import { Readability } from '@mozilla/readability';
import type { PageMeta } from '../../../proto/types';
import { DEFAULT_DWELL_MS } from '../background/native-bridge';

declare global {
	interface Window {
		__vbm_cs?: boolean;
	}
}

// Safe wrapper — chrome.runtime becomes undefined when the extension is
// reloaded while this content script is still alive in an existing tab.
function runtimeSend(msg: object): boolean {
	try {
		if (!chrome.runtime?.id) return false;
		chrome.runtime.sendMessage(msg);
		return true;
	} catch {
		return false;
	}
}

function hasSensitiveInputs(): boolean {
	return !!document.querySelector(
		'input[type="password"], input[autocomplete*="cc-"], input[autocomplete="one-time-code"], input[autocomplete="current-password"], input[autocomplete="new-password"]',
	);
}

function getMeta(nameOrProp: string, isProperty = false): string {
	const selector = isProperty
		? `meta[property="${nameOrProp}"]`
		: `meta[name="${nameOrProp}"]`;
	return (
		document.querySelector(selector)?.getAttribute('content')?.trim() ?? ''
	);
}

function extractMeta(): PageMeta {
	const meta: PageMeta = {};
	const desc = getMeta('description');
	if (desc) meta.description = desc;
	const kw = getMeta('keywords');
	if (kw) meta.keywords = kw;
	const ogTitle = getMeta('og:title', true);
	if (ogTitle) meta.ogTitle = ogTitle;
	const ogDesc = getMeta('og:description', true);
	if (ogDesc) meta.ogDescription = ogDesc;
	const ogImg = getMeta('og:image', true);
	if (ogImg) meta.ogImage = ogImg;
	const author = getMeta('author');
	if (author) meta.author = author;
	return meta;
}

// Force-extract: used by manual index (popup_force_index / force_extract message).
function extract(): { ok: boolean; error?: string } {
	if (hasSensitiveInputs()) return { ok: false, error: 'sensitive page' };
	const article = new Readability(
		document.cloneNode(true) as Document,
	).parse();
	if (!article?.textContent || article.textContent.length < 200)
		return { ok: false, error: 'not enough text' };
	runtimeSend({
		type: 'page_viewed',
		url: location.href,
		title: article.title || document.title,
		text: article.textContent,
		dwellMs: 0,
		meta: extractMeta(),
	});
	return { ok: true };
}

// Guard: prevent double initialization when injected programmatically into
// tabs that were already open (chrome.scripting.executeScript path).
if (!window.__vbm_cs) {
	window.__vbm_cs = true;

	const TICK_MS = 500;

	let dwellThreshold = DEFAULT_DWELL_MS;
	// Read user-configured threshold from storage; falls back to default if unavailable.
	chrome.storage.local
		.get('vbmDwellMs')
		.then((r) => {
			const v = r.vbmDwellMs as number | undefined;
			if (v && v > 0) dwellThreshold = v;
		})
		.catch(() => {});

	let dwellMs = 0;
	let lastVisible =
		document.visibilityState === 'visible' ? Date.now() : null;
	let sent = false;
	let dwellStartedSent = false;

	function accumulateDwell() {
		if (lastVisible !== null) {
			dwellMs += Date.now() - lastVisible;
			lastVisible = Date.now();
		}
	}

	document.addEventListener('visibilitychange', () => {
		if (document.visibilityState === 'visible') {
			lastVisible = Date.now();
		} else {
			if (lastVisible !== null) {
				dwellMs += Date.now() - lastVisible;
				lastVisible = null;
			}
		}
	});

	// Hot-update threshold when the user changes it in the popup.
	chrome.storage.onChanged.addListener((changes) => {
		if (changes.vbmDwellMs) {
			const v = changes.vbmDwellMs.newValue as number | undefined;
			dwellThreshold = v && v > 0 ? v : DEFAULT_DWELL_MS;
		}
	});

	const timer = setInterval(() => {
		// Stop cleanly if extension was reloaded — avoids the TypeError.
		if (!chrome.runtime?.id) {
			clearInterval(timer);
			return;
		}
		if (sent) return;
		accumulateDwell();
		if (!dwellStartedSent && dwellMs > 0) {
			dwellStartedSent = true;
			runtimeSend({ type: 'dwell_started' });
		}
		if (dwellMs >= dwellThreshold) {
			if (hasSensitiveInputs()) {
				runtimeSend({ type: 'page_sensitive' });
				return;
			}
			sent = true;
			// Record visit: URL + meta tags only — no deep indexing.
			runtimeSend({
				type: 'page_visited',
				url: location.href,
				title: document.title,
				dwellMs,
				meta: extractMeta(),
			});
		}
	}, TICK_MS);

	document.addEventListener('focusin', (e) => {
		const el = e.target as HTMLElement;
		if (
			el.tagName === 'INPUT' &&
			(el as HTMLInputElement).type === 'password'
		) {
			runtimeSend({ type: 'page_sensitive' });
		}
	});

	// Force index: message from service worker bypasses dwell timer.
	chrome.runtime.onMessage.addListener(
		(msg: { type: string }, _sender, sendResponse) => {
			if (msg.type !== 'force_extract') return;
			const result = extract();
			if (result.ok) sent = true;
			sendResponse(result);
		},
	);
}
