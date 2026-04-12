import { ingest, search, getStatus, forget } from './daemon-client';
import { isDeniedUrl, isDeniedDomain } from '../lib/denylist';

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
}

// P1-01: capture state lives in the service worker (survives popup open/close).
let captureEnabled = true;

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
		if (isDeniedUrl(url) || isDeniedDomain(hostname)) return;
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
					}),
				)
				.catch((e) => sendResponse({ error: String(e) }));
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
