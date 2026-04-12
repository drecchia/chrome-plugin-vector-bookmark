import {
	ingest,
	search,
	getStatus,
	forget,
	pageExists,
	getBlocklist,
	addToBlocklist,
} from './daemon-client';
import { isDeniedUrl, isDeniedDomain } from '../lib/denylist';
import { normaliseBlocklistEntry } from './native-bridge';

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
	starRank?: boolean;
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

// CR-010: load blocklist from daemon on startup; refresh every 60s.
function refreshBlocklist() {
	getBlocklist()
		.then((patterns) => {
			blockedDomains = patterns;
		})
		.catch(() => {});
}
refreshBlocklist();
setInterval(refreshBlocklist, 60_000);

// CR-010: one-shot migration from chrome.storage.local → daemon.
chrome.storage.local.get('vbmBlockedDomains', (result) => {
	const legacy = result['vbmBlockedDomains'] as string[] | undefined;
	if (legacy?.length) {
		Promise.all(legacy.map((p) => addToBlocklist(p)))
			.then(() => chrome.storage.local.remove('vbmBlockedDomains'))
			.catch(() => {});
	}
});

// Default LLM chat domains — seeded once into the user blocklist so the
// user can inspect and remove them via Open UI → Blocklist.
const DEFAULT_BLOCKED_LLM_DOMAINS = [
	'chatgpt.com',
	'chat.openai.com',
	'claude.ai',
	'gemini.google.com',
	'aistudio.google.com',
	'copilot.microsoft.com',
	'perplexity.ai',
	'poe.com',
	'character.ai',
	'mistral.ai',
	'huggingface.co',
	'you.com',
	'phind.com',
	'groq.com',
	'cohere.com',
	'pi.ai',
	'together.ai',
	'replicate.com',
];

// Seed LLM domains on every startup — addToBlocklist is idempotent (PRIMARY KEY).
Promise.all(DEFAULT_BLOCKED_LLM_DOMAINS.map((d) => addToBlocklist(d)))
	.then(() => refreshBlocklist())
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

	try {
		const { hostname } = new URL(url);
		if (
			isDeniedUrl(url) ||
			isDeniedDomain(hostname) ||
			isBlockedByUser(hostname)
		)
			return;
	} catch {
		return;
	}

	// P2-10: strip tracking/session params from URL before indexing.
	const cleanUrl = sanitizeUrl(msg.url);
	await ingest({
		url: cleanUrl,
		title: msg.title,
		text: msg.text,
		visitTs: Date.now(),
		dwellMs: msg.dwellMs,
		domain: new URL(cleanUrl).hostname,
		starRank: msg.starRank,
	});

	// Update badge briefly
	const tabId = sender.tab?.id;
	if (tabId !== undefined) {
		chrome.action.setBadgeText({ text: '●', tabId });
		chrome.action.setBadgeBackgroundColor({ color: '#ef4444' });
		setTimeout(() => chrome.action.setBadgeText({ text: '', tabId }), 2000);
	}
}

chrome.runtime.onMessage.addListener(
	(
		msg: { type: string; req?: ForgetRequest; enabled?: boolean },
		sender: chrome.runtime.MessageSender,
		sendResponse: (response?: unknown) => void,
	) => {
		if (msg.type === 'page_viewed') {
			handlePageViewed(msg as PageViewedMessage, sender).catch(
				console.error,
			);
		}

		if (msg.type === 'page_sensitive') {
			console.log('[VBM] Sensitive page detected — skipping ingest');
		}

		if (msg.type === 'popup_status') {
			getStatus()
				.then((status) =>
					sendResponse({
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

		if (msg.type === 'popup_page_exists') {
			chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
				const url = tabs[0]?.url;
				if (!url) {
					sendResponse({ indexed: false });
					return;
				}
				pageExists(url)
					.then((indexed) => sendResponse({ indexed }))
					.catch(() => sendResponse({ indexed: false }));
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

		// CR-010: force-index with star_rank=1.
		if (msg.type === 'popup_force_index_star') {
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
						{ type: 'force_extract', starRank: true },
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
				const files = (chrome.runtime.getManifest().content_scripts?.[0]
					?.js ?? []) as string[];
				if (!files.length) {
					doExtract();
					return;
				}
				chrome.scripting.executeScript(
					{ target: { tabId }, files },
					() => {
						void chrome.runtime.lastError;
						doExtract();
					},
				);
			});
			return true;
		}

		// Check if current tab's domain is in the user blacklist only (not built-in denylist).
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

		// CR-010: check if current tab's domain is blocked (built-in or user).
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
					sendResponse({
						blocked:
							isDeniedDomain(hostname) ||
							isBlockedByUser(hostname),
					});
				} catch {
					sendResponse({ blocked: false });
				}
			});
			return true;
		}
	},
);

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
