import { ingest, search, getStatus, forget } from './daemon-client';
import { daemonState } from './native-bridge';
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

	await ingest({
		url: msg.url,
		title: msg.title,
		text: msg.text,
		visitTs: Date.now(),
		dwellMs: msg.dwellMs,
		domain: new URL(msg.url).hostname,
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
					// P1-02: include daemon port so popup can build correct UI URL.
					sendResponse({
						...status,
						daemonPort: daemonState.port,
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
	async (
		text: string,
		suggest: (suggestions: chrome.omnibox.SuggestResult[]) => void,
	) => {
		if (text.length < 2) return;
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
