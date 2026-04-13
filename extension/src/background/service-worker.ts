import {
	ingest,
	recordVisit,
	search,
	getStatus,
	forget,
	pageStatus,
	getBlacklist,
	addToBlacklist,
} from './daemon-client';
import {
	normaliseBlacklistEntry,
	getDaemonBase,
	getDaemonConfig,
} from './native-bridge';
import defaultBlacklist from './default-blacklist.json';

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
	| 'indexed'
	| 'disconnected';
const BADGE_COLORS: Record<BadgeState, string> = {
	blocked: '#9ca3af', // grey
	tracking: '#f59e0b', // yellow
	visited: '#3b82f6', // blue — history recorded, not indexed
	indexed: '#22c55e', // green
	disconnected: '#ef4444', // red
};
const BADGE_TITLES: Record<BadgeState, string> = {
	blocked: 'Vector Bookmark: page blocked',
	tracking: 'Vector Bookmark: tracking dwell time…',
	visited: 'Vector Bookmark: visit recorded',
	indexed: 'Vector Bookmark: page indexed',
	disconnected: 'Vector Bookmark: daemon unreachable',
};
function setBadge(tabId: number, state: BadgeState | 'clear'): void {
	if (state === 'clear') {
		chrome.action.setBadgeText({ text: '', tabId });
		chrome.action.setTitle({ title: 'Vector Bookmark', tabId });
		return;
	}
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

// CR-008: user-managed domain blacklist (suffix match).
let blockedDomains: string[] = [];

function isBlockedByUser(hostname: string): boolean {
	const h = hostname.toLowerCase();
	return blockedDomains.some((entry) => {
		if (entry.startsWith('/') && entry.endsWith('/')) {
			try {
				return new RegExp(entry.slice(1, -1), 'i').test(h);
			} catch {
				return false;
			}
		}
		return h === entry || h.endsWith('.' + entry);
	});
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

// P2-10: strip common tracking/session query params before indexing.
function sanitizeUrl(url: string): string {
	try {
		const u = new URL(url);
		const trackingParams = [
			'utm_source',
			'utm_medium',
			'utm_campaign',
			'utm_term',
			'utm_content',
			'fbclid',
			'gclid',
			'msclkid',
			'ref',
			'source',
			'token',
			'access_token',
			'api_key',
			'key',
			'secret',
			'session',
			'sid',
			'sessionid',
			'session_id',
		];
		for (const p of trackingParams) u.searchParams.delete(p);
		return u.toString();
	} catch {
		return url;
	}
}

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
	try {
		await recordVisit({
			url: cleanUrl,
			title: msg.title,
			visitTs: Date.now(),
			dwellMs: msg.dwellMs,
			domain: new URL(cleanUrl).hostname,
			meta: msg.meta,
		});
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
		// Prepend meta fields so they are chunked and embedded alongside body text.
		const metaParts: string[] = [];
		const m = msg.meta ?? {};
		if (m.description) metaParts.push(`description: ${m.description}`);
		if (m.keywords) metaParts.push(`keywords: ${m.keywords}`);
		if (m.ogDescription) metaParts.push(`summary: ${m.ogDescription}`);
		if (m.author) metaParts.push(`author: ${m.author}`);
		const enrichedText = metaParts.length
			? `${metaParts.join('\n')}\n\n${msg.text}`
			: msg.text;
		await ingest({
			url: cleanUrl,
			title: msg.title,
			text: enrichedText,
			visitTs: Date.now(),
			dwellMs: msg.dwellMs,
			domain: new URL(cleanUrl).hostname,
		});
		if (tabId !== undefined) setBadge(tabId, 'indexed');
	} catch {
		if (tabId !== undefined) setBadge(tabId, 'disconnected');
	}
}

chrome.runtime.onMessage.addListener(
	(
		msg: { type: string; req?: ForgetRequest; enabled?: boolean },
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

		if (msg.type === 'page_sensitive') {
			console.log('[VBM] Sensitive page detected — skipping ingest');
		}

		if (msg.type === 'popup_status') {
			getStatus()
				.then((status) =>
					sendResponse({
						visited: status.visited,
						indexed: status.indexed,
						pending: status.pending,
						version: status.version,
						daemonPort: status.port,
						captureEnabled,
						embedderVersion: status.embedderVersion,
					}),
				)
				.catch((e) => sendResponse({ error: String(e) }));
			return true;
		}

		if (msg.type === 'popup_page_status') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const url = tabs[0]?.url;
				if (!url) {
					sendResponse({ exists: false, indexed: false });
					return;
				}
				pageStatus(url)
					.then((status) => sendResponse(status))
					.catch(() =>
						sendResponse({ exists: false, indexed: false }),
					);
			});
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
				const doExtract = () => {
					chrome.tabs.sendMessage(
						tabId,
						{ type: 'force_extract' },
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

// Update badge when a tab finishes loading a new URL.
chrome.tabs.onUpdated.addListener((tabId, info, tab) => {
	if (info.status === 'complete' && tab.url) updateTabBadge(tabId, tab.url);
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
