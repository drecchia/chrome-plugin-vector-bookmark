import {
	ingest,
	recordVisit,
	search,
	getStatus,
	forget,
	pageStatus,
	getBlacklist,
	addToBlacklist,
	listTags,
	suggestTags,
	retryQueueItem,
} from './daemon-client';
import type { IngestMode, ExtractIntent } from '../../../proto/types';
import {
	normaliseBlacklistEntry,
	getDaemonBase,
	getDaemonConfig,
} from './native-bridge';
import defaultBlacklist from './default-blacklist.json';
import { sanitizeUrl, matchesAnyBlacklist } from '../lib/url';

interface ForgetRequest {
	type: 'url' | 'domain' | 'timerange';
	value: string;
}

interface PageViewedMessage {
	type: 'page_viewed';
	url: string;
	title: string;
	text: string;
	dwellMs: number;
	meta?: Record<string, string>;
}

interface PageVisitedMessage {
	type: 'page_visited';
	url: string;
	title: string;
	dwellMs: number;
	meta?: Record<string, string>;
}

// Badge state per tab.
type BadgeState =
	| 'blocked'
	| 'tracking'
	| 'visited'
	| 'indexing'
	| 'error'
	| 'indexed'
	| 'disconnected';
const BADGE_COLORS: Record<BadgeState, string> = {
	blocked: '#9ca3af', // grey
	tracking: '#f59e0b', // yellow
	visited: '#3b82f6', // blue — history recorded, not indexed
	indexing: '#8b5cf6', // purple — ingest accepted, embedding in progress
	error: '#f97316', // orange — ingest failed (retriable), daemon still up
	indexed: '#22c55e', // green
	disconnected: '#ef4444', // red
};
const BADGE_TITLES: Record<BadgeState, string> = {
	blocked: 'Vector Bookmark: not tracking this site',
	tracking: 'Vector Bookmark: tracking dwell time…',
	visited: 'Vector Bookmark: visit recorded',
	indexing: 'Vector Bookmark: indexing…',
	error: 'Vector Bookmark: indexing failed — open popup to retry',
	indexed: 'Vector Bookmark: page indexed',
	disconnected: 'Vector Bookmark: daemon unreachable',
};
// Priority order — higher index wins; dwell_started must not downgrade visited/indexed.
// CR-0010: indexing sits above visited (an in-flight ingest supersedes a plain
// visit); error above indexing (the failed outcome wins over the transient
// in-progress state); indexed stays on top as the terminal success.
const BADGE_PRIORITY: Record<BadgeState, number> = {
	tracking: 0,
	disconnected: 1,
	blocked: 2,
	visited: 3,
	indexing: 4,
	error: 5,
	indexed: 6,
};
const tabBadgeState = new Map<number, BadgeState>();

// Pending ingest options stored when the popup triggers "Index this site now".
// Consumed when the matching page_viewed arrives from the content script.
// Keyed by tabId so concurrent extracts on different tabs don't clobber each other.
interface PendingIngest {
	mode: IngestMode;
	tags: string[];
	intent?: ExtractIntent;
	// CR-0006: when set, handlePageViewed appends this to the metaText built
	// from the page_viewed payload. Used by the `manual` intent flow.
	manualText?: string;
}
const pendingIngest = new Map<number, PendingIngest>();

// CR-0010: URLs with an ingest currently being confirmed (post-202 poll in
// flight). Lets the popup restore its "Indexing…" state when reopened mid-ingest
// and prevents two concurrent polls for the same URL. Keyed by clean URL.
interface InFlightIngest {
	tabId?: number;
	timer?: ReturnType<typeof setTimeout>;
}
const inFlightIngests = new Map<string, InFlightIngest>();

// Poll cadence for confirming an ingest actually landed. Starts at 1s, grows to
// a 3s ceiling, gives up after POLL_TIMEOUT_MS (embed via a remote provider can
// take several seconds per chunk).
const POLL_START_MS = 1000;
const POLL_MAX_MS = 3000;
const POLL_TIMEOUT_MS = 45_000;

// Broadcast the real ingest outcome to the popup (dropped silently if closed —
// the badge and /page queueStatus carry the state for a reopened popup).
function broadcastIngestComplete(payload: {
	ok: boolean;
	url: string;
	error?: string;
	retriable?: boolean;
}) {
	chrome.runtime
		.sendMessage({ type: 'ingest_complete', ...payload })
		.catch(() => {});
}

// pollIngestOutcome watches /page for a URL until the ingest resolves: indexed
// (success), queueStatus='failed' (embed failed after retries), or timeout.
// CR-0010: replaces the old "202 == indexed" assumption so the badge, toast,
// and counters only report success when the daemon actually wrote the page.
function pollIngestOutcome(url: string, tabId: number | undefined): void {
	const existing = inFlightIngests.get(url);
	if (existing?.timer) clearTimeout(existing.timer);
	const startedTs = Date.now();
	const entry: InFlightIngest = { tabId };
	inFlightIngests.set(url, entry);

	let delay = POLL_START_MS;
	const tick = async () => {
		let status;
		let fetched = false;
		try {
			status = await pageStatus(url);
			fetched = true;
			markHealthy();
		} catch {
			// Transient fetch failure — keep polling until the timeout.
			status = undefined;
		}

		if (status?.indexed) {
			inFlightIngests.delete(url);
			if (tabId !== undefined) setBadge(tabId, 'indexed', { force: true });
			broadcastIngestComplete({ ok: true, url });
			return;
		}
		// Metadata-only success: the queue row is gone (processed) but the page
		// has no searchable chunks (too-short text / meta_only). A popup ingest
		// always carries a tag delta, so a removed row means the page + tags
		// landed — report success instead of waiting out the timeout.
		if (fetched && status?.exists && !status.queueStatus) {
			inFlightIngests.delete(url);
			if (tabId !== undefined) setBadge(tabId, 'indexed', { force: true });
			broadcastIngestComplete({ ok: true, url });
			return;
		}
		if (status?.queueStatus === 'failed') {
			inFlightIngests.delete(url);
			if (tabId !== undefined) setBadge(tabId, 'error', { force: true });
			broadcastIngestComplete({
				ok: false,
				url,
				error: status.lastError || 'Indexing failed',
				retriable: true,
			});
			return;
		}
		if (Date.now() - startedTs >= POLL_TIMEOUT_MS) {
			inFlightIngests.delete(url);
			// Leave the badge on 'indexing' — the row may still be pending; the
			// popup surfaces a soft timeout the user can retry.
			broadcastIngestComplete({
				ok: false,
				url,
				error: 'Still indexing — taking longer than expected.',
				retriable: true,
			});
			return;
		}
		delay = Math.min(delay + 500, POLL_MAX_MS);
		entry.timer = setTimeout(tick, delay);
	};
	entry.timer = setTimeout(tick, delay);
}

function setBadge(
	tabId: number,
	state: BadgeState | 'clear',
	opts?: { force?: boolean },
): void {
	if (state === 'clear') {
		tabBadgeState.delete(tabId);
		chrome.action.setBadgeText({ text: '', tabId });
		chrome.action.setTitle({ title: 'Vector Bookmark', tabId });
		return;
	}
	// Never downgrade a higher-priority state (e.g. visited → tracking on revisits).
	// CR-0010: force=true bypasses the guard for deterministic ingest outcomes
	// (error → indexing on retry, indexed → indexing on re-index) that must apply
	// even though they lower the priority.
	const current = tabBadgeState.get(tabId);
	if (!opts?.force && current && BADGE_PRIORITY[current] > BADGE_PRIORITY[state])
		return;
	tabBadgeState.set(tabId, state);
	chrome.action.setBadgeText({ text: '●', tabId });
	chrome.action.setBadgeBackgroundColor({
		color: BADGE_COLORS[state],
		tabId,
	});
	chrome.action.setTitle({ title: BADGE_TITLES[state], tabId });
}

async function updateTabBadge(tabId: number, url: string): Promise<void> {
	if (
		!url ||
		url.startsWith('chrome://') ||
		url.startsWith('chrome-extension://') ||
		url.startsWith('about:')
	) {
		setBadge(tabId, 'clear');
		return;
	}
	let hostname: string;
	try {
		hostname = new URL(url).hostname;
	} catch {
		setBadge(tabId, 'clear');
		return;
	}
	if (isBlockedByUser(hostname)) {
		setBadge(tabId, 'blocked');
		return;
	}
	try {
		const base = getDaemonBase(await getDaemonConfig());
		const resp = await fetch(`${base}/healthz`, {
			signal: AbortSignal.timeout(2000),
		});
		if (!resp.ok) throw new Error('not ok');
		markHealthy();
	} catch {
		setBadge(tabId, 'disconnected');
		return;
	}
	try {
		const status = await pageStatus(url);
		if (status.indexed) {
			setBadge(tabId, 'indexed');
			return;
		}
		// CR-0010: reflect an unfinished/failed queue row so switching tabs
		// during an ingest doesn't drop the indexing/error badge back to visited.
		if (status.queueStatus === 'failed') {
			setBadge(tabId, 'error');
			return;
		}
		if (status.queueStatus === 'pending') {
			setBadge(tabId, 'indexing');
			return;
		}
		if (status.exists) {
			setBadge(tabId, 'visited');
			return;
		}
	} catch {
		// fall through
	}
	setBadge(tabId, 'tracking');
}

// P1-01: capture state lives in the service worker (survives popup open/close).
let captureEnabled = true;

// Track last successful daemon contact so the popup can show an offline banner
// without every call hitting /healthz. Updated from any successful daemon call
// (healthz, status, visit, ingest). `connected` = lastHealthyTs within window.
let lastHealthyTs: number | null = null;
const HEALTHY_WINDOW_MS = 15_000;
function markHealthy() {
	lastHealthyTs = Date.now();
}
function isConnected(): boolean {
	return (
		lastHealthyTs !== null && Date.now() - lastHealthyTs < HEALTHY_WINDOW_MS
	);
}

// Debounce repeated /visit calls for the same URL within this window —
// prevents the timeline from showing the same page over and over when the
// user idles or refreshes. Different URLs (even on the same domain) bypass
// the debounce, so subpage navigation still records.
const RECORD_DEBOUNCE_MS = 30 * 60 * 1000;
const recentVisits = new Map<string, number>();
function rememberVisit(url: string) {
	const now = Date.now();
	// Sweep expired entries on every insert — keeps the Map bounded by the
	// number of distinct URLs visited within the last RECORD_DEBOUNCE_MS,
	// not by the full history of the SW lifetime.
	for (const [u, ts] of recentVisits) {
		if (now - ts >= RECORD_DEBOUNCE_MS) recentVisits.delete(u);
	}
	recentVisits.set(url, now);
}

// CR-008: user-managed domain blacklist (suffix match).
let blockedDomains: string[] = [];

function isBlockedByUser(hostname: string): boolean {
	return matchesAnyBlacklist(hostname, blockedDomains);
}

// CR-010: load blacklist from daemon on startup; refresh every 60s.
function refreshBlacklist() {
	getBlacklist()
		.then((patterns) => {
			blockedDomains = patterns;
		})
		.catch(() => {});
}
refreshBlacklist();
setInterval(refreshBlacklist, 60_000);

// CR-010: one-shot migration from chrome.storage.local → daemon.
chrome.storage.local.get('vbmBlockedDomains', (result) => {
	const legacy = result['vbmBlockedDomains'] as string[] | undefined;
	if (legacy?.length) {
		Promise.all(legacy.map((p) => addToBlacklist(p)))
			.then(() => chrome.storage.local.remove('vbmBlockedDomains'))
			.catch(() => {});
	}
});

// Default LLM chat domains — seeded once into the user blacklist so the
// user can inspect and remove them via Open UI → Blacklist.
// Default blocked entries loaded from default-blacklist.json at build time.
// To add/remove entries, edit that file and rebuild — no TypeScript changes needed.
const DEFAULT_BLOCKED_ALL = [
	...defaultBlacklist.local,
	...defaultBlacklist.llm,
	...defaultBlacklist.social,
	...defaultBlacklist.adult,
];

// Seed all default entries on every startup — addToBlacklist is idempotent (PRIMARY KEY).
Promise.all(DEFAULT_BLOCKED_ALL.map((d) => addToBlacklist(d)))
	.then(() => refreshBlacklist())
	.catch(() => {});

// P2-04: debounce omnibox to avoid a fetch on every keystroke.
let omniboxTimer: ReturnType<typeof setTimeout> | null = null;

function escapeXml(s: string): string {
	return s
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;');
}

async function handlePageVisited(
	msg: PageVisitedMessage,
	sender: chrome.runtime.MessageSender,
): Promise<void> {
	if (!captureEnabled) return;
	if (!sender.tab) return;
	const url = msg.url;
	if (
		!url ||
		url.startsWith('chrome://') ||
		url.startsWith('chrome-extension://')
	)
		return;

	const tabId = sender.tab?.id;

	try {
		const { hostname } = new URL(url);
		if (isBlockedByUser(hostname)) {
			if (tabId !== undefined) setBadge(tabId, 'blocked');
			return;
		}
	} catch {
		return;
	}

	const cleanUrl = sanitizeUrl(msg.url);
	// Skip the network round-trip if the same URL was recorded recently.
	// Badge still goes to 'visited' so the user sees we acknowledged the page.
	const lastTs = recentVisits.get(cleanUrl);
	if (lastTs !== undefined && Date.now() - lastTs < RECORD_DEBOUNCE_MS) {
		if (tabId !== undefined) setBadge(tabId, 'visited');
		return;
	}
	try {
		await recordVisit({
			url: cleanUrl,
			title: msg.title,
			visitTs: Date.now(),
			dwellMs: msg.dwellMs,
			domain: new URL(cleanUrl).hostname,
			meta: msg.meta,
		});
		rememberVisit(cleanUrl);
		if (tabId !== undefined) setBadge(tabId, 'visited');
	} catch {
		if (tabId !== undefined) setBadge(tabId, 'disconnected');
	}
}

async function handlePageViewed(
	msg: PageViewedMessage,
	sender: chrome.runtime.MessageSender,
): Promise<void> {
	// P1-01: respect pause state.
	if (!captureEnabled) return;

	if (!sender.tab) return;
	const url = msg.url;
	if (
		!url ||
		url.startsWith('chrome://') ||
		url.startsWith('chrome-extension://')
	)
		return;

	const tabId = sender.tab?.id;

	try {
		const { hostname } = new URL(url);
		if (isBlockedByUser(hostname)) {
			if (tabId !== undefined) setBadge(tabId, 'blocked');
			return;
		}
	} catch {
		return;
	}

	// P2-10: strip tracking/session params from URL before indexing.
	const cleanUrl = sanitizeUrl(msg.url);
	try {
		// Build a meta block (title + description/keywords/og/author).
		const metaParts: string[] = [];
		const m = msg.meta ?? {};
		if (msg.title) metaParts.push(msg.title);
		if (m.description) metaParts.push(`description: ${m.description}`);
		if (m.keywords) metaParts.push(`keywords: ${m.keywords}`);
		if (m.ogDescription) metaParts.push(`summary: ${m.ogDescription}`);
		if (m.author) metaParts.push(`author: ${m.author}`);
		const metaText = metaParts.join('\n');

		const pending =
			tabId !== undefined ? pendingIngest.get(tabId) : undefined;
		if (tabId !== undefined) pendingIngest.delete(tabId);
		const mode: IngestMode = pending?.mode ?? 'full_text';
		const tags = pending?.tags ?? [];

		// CR-0006: manualText takes precedence — concatenated to metaText,
		// the page body (msg.text) is ignored because we never extracted it.
		let text: string;
		if (pending?.manualText) {
			text = metaText
				? `${metaText}\n\n${pending.manualText}`
				: pending.manualText;
		} else if (mode === 'meta_only') {
			text = metaText;
		} else {
			text = metaText ? `${metaText}\n\n${msg.text}` : msg.text;
		}

		await ingest({
			url: cleanUrl,
			title: msg.title,
			text,
			visitTs: Date.now(),
			dwellMs: msg.dwellMs,
			domain: new URL(cleanUrl).hostname,
			tags,
			setTags: pending !== undefined,
			mode,
		});
		// CR-0010: the 202 only means "accepted into the queue" — the embed
		// still runs asynchronously and can fail. Show the in-progress state and
		// poll /page for the real outcome instead of claiming success now.
		markHealthy();
		if (tabId !== undefined) setBadge(tabId, 'indexing', { force: true });
		pollIngestOutcome(cleanUrl, tabId);
	} catch (e) {
		// Synchronous /ingest failure (e.g. 503 queue full, 502 llm_summary,
		// network down). Distinguish a reachable-but-rejecting daemon (show the
		// real error, retriable) from an unreachable one (disconnected badge).
		const message = String((e as Error)?.message ?? e);
		const reachable = await isDaemonReachable();
		if (tabId !== undefined) {
			setBadge(tabId, reachable ? 'error' : 'disconnected', {
				force: true,
			});
		}
		broadcastIngestComplete({
			ok: false,
			url: cleanUrl,
			error: message,
			retriable: reachable,
		});
	}
}

// isDaemonReachable does a fast /healthz probe used on the ingest error path to
// pick between the 'error' (daemon up, ingest rejected) and 'disconnected'
// (daemon down) badges. CR-0010.
async function isDaemonReachable(): Promise<boolean> {
	try {
		const base = getDaemonBase(await getDaemonConfig());
		const resp = await fetch(`${base}/healthz`, {
			signal: AbortSignal.timeout(2000),
		});
		if (resp.ok) markHealthy();
		return resp.ok;
	} catch {
		return false;
	}
}

chrome.runtime.onMessage.addListener(
	(
		msg: {
			type: string;
			req?: ForgetRequest;
			enabled?: boolean;
			tags?: string[];
			mode?: IngestMode;
			manualText?: string;
			intent?: ExtractIntent;
			url?: string;
		},
		sender: chrome.runtime.MessageSender,
		sendResponse: (response?: unknown) => void,
	) => {
		if (msg.type === 'page_visited') {
			handlePageVisited(msg as PageVisitedMessage, sender).catch(
				console.error,
			);
		}

		if (msg.type === 'page_viewed') {
			handlePageViewed(msg as PageViewedMessage, sender).catch(
				console.error,
			);
		}

		if (msg.type === 'dwell_started' && sender.tab?.id !== undefined) {
			setBadge(sender.tab.id, 'tracking');
		}

		if (msg.type === 'url_changed' && sender.tab?.id !== undefined) {
			// SPA navigation: reset badge so dwell tracking restarts for new URL.
			tabBadgeState.delete(sender.tab.id);
			setBadge(sender.tab.id, 'tracking');
		}

		if (msg.type === 'page_sensitive') {
			console.log('[VBM] Sensitive page detected — skipping ingest');
		}

		if (msg.type === 'popup_status') {
			getStatus()
				.then((status) => {
					markHealthy();
					sendResponse({
						visited: status.visited,
						indexed: status.indexed,
						pending: status.pending,
						version: status.version,
						daemonPort: status.port,
						captureEnabled,
						embedderVersion: status.embedderVersion,
						connected: true,
						lastSeenTs: lastHealthyTs,
					});
				})
				.catch((e) =>
					sendResponse({
						error: String(e),
						connected: isConnected(),
						lastSeenTs: lastHealthyTs,
					}),
				);
			return true;
		}

		if (msg.type === 'popup_page_status') {
			// Popup may pass the URL it queried directly so we always look up
			// the same tab the user is staring at (avoids a race where SW's
			// own chrome.tabs.query sees a different active tab). Falls back
			// to active-tab query when missing for backwards compatibility.
			const lookup = (raw: string | undefined) => {
				if (!raw) {
					sendResponse({ exists: false, indexed: false });
					return;
				}
				pageStatus(sanitizeUrl(raw))
					.then((status) => sendResponse(status))
					.catch(() =>
						sendResponse({ exists: false, indexed: false }),
					);
			};
			if (typeof msg.url === 'string' && msg.url) {
				lookup(msg.url);
			} else {
				chrome.tabs.query(
					{ active: true, currentWindow: true },
					(tabs) => lookup(tabs[0]?.url),
				);
			}
			return true;
		}

		// P1-01: popup toggles capture on/off.
		if (msg.type === 'popup_set_capture') {
			captureEnabled = msg.enabled ?? true;
			sendResponse({ ok: true, captureEnabled });
			return true;
		}

		if (msg.type === 'popup_forget') {
			forget(msg.req as ForgetRequest)
				.then(() => sendResponse({ ok: true }))
				.catch((e) => sendResponse({ ok: false, error: String(e) }));
			return true;
		}

		if (msg.type === 'popup_list_tags') {
			listTags()
				.then((tags) => sendResponse({ tags }))
				.catch(() => sendResponse({ tags: [] }));
			return true;
		}

		// CR-0010: popup asks, on open, whether an ingest for this URL is still
		// being confirmed so it can restore the "Indexing…" state.
		if (msg.type === 'popup_ingest_state') {
			const url = msg.url ? sanitizeUrl(msg.url) : '';
			sendResponse({ inFlight: url ? inFlightIngests.has(url) : false });
			return true;
		}

		// CR-0010: manual retry of a failed ingest. Re-enqueues the failed queue
		// row on the daemon (no re-extraction needed) and restarts the poll.
		if (msg.type === 'popup_retry_ingest') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const rawUrl = msg.url ?? tabs[0]?.url;
				const tabId = tabs[0]?.id;
				if (!rawUrl) {
					sendResponse({ ok: false, error: 'No URL to retry' });
					return;
				}
				const url = sanitizeUrl(rawUrl);
				retryQueueItem(url)
					.then(() => {
						if (tabId !== undefined)
							setBadge(tabId, 'indexing', { force: true });
						pollIngestOutcome(url, tabId);
						sendResponse({ ok: true });
					})
					.catch((e) =>
						sendResponse({
							ok: false,
							error: String((e as Error)?.message ?? e),
						}),
					);
			});
			return true;
		}

		// CR-0003: ask the content script for the page payload, then forward it
		// to /tags/suggest. Doesn't touch ingest, badge, or pendingIngest.
		if (msg.type === 'popup_suggest_tags') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const tab = tabs[0];
				if (
					!tab?.id ||
					!tab.url ||
					tab.url.startsWith('chrome://') ||
					tab.url.startsWith('chrome-extension://')
				) {
					sendResponse({
						ok: false,
						error: 'Cannot suggest tags for this page type',
					});
					return;
				}
				const tabId = tab.id;
				const ask = () => {
					chrome.tabs.sendMessage(
						tabId,
						{ type: 'force_extract', intent: 'suggest_tags' },
						(res) => {
							if (chrome.runtime.lastError) {
								sendResponse({
									ok: false,
									error: 'Failed — refresh the page and try again',
								});
								return;
							}
							if (!res?.ok || !res.payload) {
								sendResponse({
									ok: false,
									error:
										res?.error ?? 'Could not extract page',
								});
								return;
							}
							suggestTags(res.payload)
								.then((data) => {
									if (!data.tags || data.tags.length === 0) {
										sendResponse({
											ok: false,
											error: 'Could not suggest tags — try again',
										});
										return;
									}
									sendResponse({
										ok: true,
										tags: data.tags,
									});
								})
								.catch((e) =>
									sendResponse({
										ok: false,
										error: String(
											(e as Error).message ?? e,
										),
									}),
								);
						},
					);
				};
				const files = (chrome.runtime.getManifest().content_scripts?.[0]
					?.js ?? []) as string[];
				if (!files.length) {
					ask();
					return;
				}
				chrome.scripting.executeScript(
					{ target: { tabId }, files },
					() => {
						void chrome.runtime.lastError;
						ask();
					},
				);
			});
			return true;
		}

		if (msg.type === 'popup_force_index') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const tab = tabs[0];
				if (
					!tab?.id ||
					!tab.url ||
					tab.url.startsWith('chrome://') ||
					tab.url.startsWith('chrome-extension://')
				) {
					sendResponse({
						ok: false,
						error: 'Cannot index this page type',
					});
					return;
				}
				const tabId = tab.id;
				let intent = msg.intent;
				let manualText: string | undefined;

				// CR-0006: manual mode goes through the content script via
				// intent='manual' so we can grab title + meta. The popup's
				// manualText is stashed in pendingIngest and concatenated in
				// handlePageViewed.
				if (!intent && msg.mode === 'manual') {
					const t = (msg.manualText ?? '').trim();
					if (!t) {
						sendResponse({
							ok: false,
							error: 'Manual text is empty',
						});
						return;
					}
					intent = 'manual';
					manualText = t;
				}

				// CR-0002: when an extraction intent is provided, the content
				// script does the heavy lifting and the daemon sees mode=manual
				// (stores text as-is, no LLM/Readability post-processing).
				const mode: IngestMode = intent
					? 'manual'
					: (msg.mode ?? 'full_text');
				const tags = msg.tags ?? [];

				pendingIngest.set(tabId, { mode, tags, intent, manualText });
				const doExtract = () => {
					chrome.tabs.sendMessage(
						tabId,
						{ type: 'force_extract', intent },
						(res) => {
							if (chrome.runtime.lastError) {
								sendResponse({
									ok: false,
									error: 'Failed — refresh the page and try again',
								});
							} else {
								sendResponse(res ?? { ok: false });
							}
						},
					);
				};
				// Inject content script if tab was open before extension loaded.
				// Guard in extract.ts (window.__vbm_cs) prevents double-init.
				const files = (chrome.runtime.getManifest().content_scripts?.[0]
					?.js ?? []) as string[];
				if (!files.length) {
					doExtract();
					return;
				}
				chrome.scripting.executeScript(
					{ target: { tabId }, files },
					() => {
						void chrome.runtime.lastError; // suppress unchecked error warning
						doExtract();
					},
				);
			});
			return true;
		}

		// Check if current tab's domain is in the user blacklist.
		if (msg.type === 'popup_is_user_blocked') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const url = tabs[0]?.url;
				if (
					!url ||
					url.startsWith('chrome://') ||
					url.startsWith('chrome-extension://')
				) {
					sendResponse({ blocked: false });
					return;
				}
				try {
					const { hostname } = new URL(url);
					sendResponse({ blocked: isBlockedByUser(hostname) });
				} catch {
					sendResponse({ blocked: false });
				}
			});
			return true;
		}

		// Check if current tab's domain is blocked (user blacklist).
		if (msg.type === 'popup_is_blocked') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const url = tabs[0]?.url;
				if (
					!url ||
					url.startsWith('chrome://') ||
					url.startsWith('chrome-extension://')
				) {
					sendResponse({ blocked: false });
					return;
				}
				try {
					const { hostname } = new URL(url);
					sendResponse({ blocked: isBlockedByUser(hostname) });
				} catch {
					sendResponse({ blocked: false });
				}
			});
			return true;
		}

		// Add current tab's domain to the user blacklist.
		if (msg.type === 'popup_ignore_domain') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const url = tabs[0]?.url;
				const tabId = tabs[0]?.id;
				if (
					!url ||
					url.startsWith('chrome://') ||
					url.startsWith('chrome-extension://')
				) {
					sendResponse({
						ok: false,
						error: 'Cannot ignore this page type',
					});
					return;
				}
				let hostname: string;
				try {
					hostname = new URL(url).hostname;
				} catch {
					sendResponse({ ok: false, error: 'Invalid URL' });
					return;
				}
				const pattern = normaliseBlacklistEntry(hostname);
				addToBlacklist(pattern)
					.then(() => {
						blockedDomains = [...blockedDomains, pattern];
						if (tabId !== undefined) setBadge(tabId, 'blocked');
						sendResponse({ ok: true, domain: hostname });
					})
					.catch((e) =>
						sendResponse({ ok: false, error: String(e) }),
					);
			});
			return true;
		}
	},
);

// Update badge when the user switches tabs.
chrome.tabs.onActivated.addListener(async ({ tabId }) => {
	try {
		const tab = await chrome.tabs.get(tabId);
		if (tab.url) updateTabBadge(tabId, tab.url);
	} catch {
		// tab may have been closed
	}
});

// Reset per-tab state on new navigation so priority guard doesn't carry over.
chrome.tabs.onUpdated.addListener((tabId, info, tab) => {
	if (info.status === 'loading' && info.url) tabBadgeState.delete(tabId);
	if (info.status === 'complete' && tab.url) updateTabBadge(tabId, tab.url);
});

// Clean up state when a tab is closed.
chrome.tabs.onRemoved.addListener((tabId) => {
	tabBadgeState.delete(tabId);
	pendingIngest.delete(tabId);
});

chrome.omnibox.onInputChanged.addListener(
	(
		text: string,
		suggest: (suggestions: chrome.omnibox.SuggestResult[]) => void,
	) => {
		if (text.length < 2) return;
		// P2-04: debounce — wait 200ms after last keystroke before fetching.
		if (omniboxTimer !== null) clearTimeout(omniboxTimer);
		omniboxTimer = setTimeout(async () => {
			omniboxTimer = null;
			try {
				const results = await search(text, 5);
				suggest(
					results.map((r) => ({
						content: r.url,
						description: `<url>${escapeXml(r.domain)}</url> — <match>${escapeXml(r.snippet.slice(0, 80))}</match>`,
					})),
				);
			} catch {
				// daemon not connected — silently skip
			}
		}, 200);
	},
);

chrome.omnibox.onInputEntered.addListener(
	(text: string, disposition: chrome.omnibox.OnInputEnteredDisposition) => {
		if (text.startsWith('http')) {
			const url = text;
			if (disposition === 'currentTab') {
				chrome.tabs.update({ url });
			} else {
				chrome.tabs.create({ url });
			}
		}
	},
);
