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

// CR-0002: extract whatever the user has highlighted.
function extractSelection(): { ok: boolean; error?: string } {
	const sel = window.getSelection()?.toString().trim() ?? '';
	if (!sel) return { ok: false, error: 'Nothing selected' };
	runtimeSend({
		type: 'page_viewed',
		url: location.href,
		title: document.title,
		text: sel,
		dwellMs: 0,
		meta: extractMeta(),
	});
	return { ok: true };
}

// CR-0002: scrape the YouTube transcript panel. Tries to open the panel if
// it isn't already; returns an error when CC isn't available for the video.
async function extractYouTubeTranscript(): Promise<{
	ok: boolean;
	error?: string;
}> {
	const SEGMENT_SEL = 'ytd-transcript-segment-renderer .segment-text';

	function readSegments(): string[] {
		return Array.from(document.querySelectorAll(SEGMENT_SEL))
			.map((el) => (el.textContent ?? '').trim())
			.filter((s) => s.length > 0);
	}

	let segments = readSegments();
	if (segments.length === 0) {
		// Try clicking the "Show transcript" button — selector varies by locale.
		const btn = Array.from(
			document.querySelectorAll<HTMLElement>('button, [role="button"]'),
		).find((el) => {
			const label = (
				el.getAttribute('aria-label') ??
				el.textContent ??
				''
			).toLowerCase();
			return (
				label.includes('transcript') ||
				label.includes('transcrição') ||
				label.includes('transcricao')
			);
		});
		if (!btn)
			return {
				ok: false,
				error: 'Transcript not available for this video',
			};
		btn.click();

		// Wait briefly for the panel to render.
		const start = Date.now();
		while (Date.now() - start < 3000) {
			await new Promise((r) => setTimeout(r, 200));
			segments = readSegments();
			if (segments.length > 0) break;
		}
		if (segments.length === 0) {
			return {
				ok: false,
				error: 'Transcript not available for this video',
			};
		}
	}

	const text = segments.join(' ');
	const titleEl = document.querySelector(
		'h1.ytd-watch-metadata, h1.title yt-formatted-string',
	);
	const title = (titleEl?.textContent ?? document.title).trim();
	runtimeSend({
		type: 'page_viewed',
		url: location.href,
		title,
		text,
		dwellMs: 0,
		meta: extractMeta(),
	});
	return { ok: true };
}

// CR-0003: extract just the payload (title + body) for tag suggestion. Does
// NOT emit page_viewed — the SW gets the payload back via sendResponse and
// forwards it to /tags/suggest.
function extractForSuggest(): {
	ok: boolean;
	error?: string;
	payload?: { url: string; title: string; text: string };
} {
	if (hasSensitiveInputs()) return { ok: false, error: 'sensitive page' };
	const article = new Readability(
		document.cloneNode(true) as Document,
	).parse();
	const text = article?.textContent?.trim() ?? '';
	if (text.length < 100) {
		return { ok: false, error: 'Not enough content to suggest tags' };
	}
	return {
		ok: true,
		payload: {
			url: location.href,
			title: article?.title || document.title,
			text,
		},
	};
}

// CR-0002: read the comment threads currently rendered in the DOM (no scroll).
function extractYouTubeComments(maxN = 50): { ok: boolean; error?: string } {
	const nodes = Array.from(
		document.querySelectorAll('ytd-comment-thread-renderer #content-text'),
	).slice(0, maxN);
	const comments = nodes
		.map((el) => (el.textContent ?? '').trim())
		.filter((s) => s.length > 0);
	if (comments.length === 0) {
		return {
			ok: false,
			error: 'No comments visible — scroll to load them',
		};
	}
	const text = comments.join('\n\n---\n\n');
	const titleEl = document.querySelector(
		'h1.ytd-watch-metadata, h1.title yt-formatted-string',
	);
	const title = (titleEl?.textContent ?? document.title).trim();
	runtimeSend({
		type: 'page_viewed',
		url: location.href,
		title: `${title} — comments`,
		text,
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
	let currentUrl = location.href;

	function resetDwell() {
		dwellMs = 0;
		sent = false;
		dwellStartedSent = false;
		lastVisible =
			document.visibilityState === 'visible' ? Date.now() : null;
		currentUrl = location.href;
		try {
			runtimeSend({ type: 'url_changed', url: currentUrl });
		} catch {
			// chrome.runtime may be unavailable (extension reload or sandboxed frame).
		}
	}

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

	// SPA navigation: intercept history API and popstate to reset dwell state.
	// Wrapped in try-catch because some pages (iframes, sandboxed contexts) may
	// override history before chrome.runtime is ready or restrict property access.
	try {
		const _pushState = history.pushState.bind(history);
		const _replaceState = history.replaceState.bind(history);
		history.pushState = (...args) => {
			_pushState(...args);
			if (location.href !== currentUrl) resetDwell();
		};
		history.replaceState = (...args) => {
			_replaceState(...args);
			if (location.href !== currentUrl) resetDwell();
		};
	} catch {
		// history API not patchable in this context — skip SPA intercept.
	}
	window.addEventListener('popstate', () => {
		if (location.href !== currentUrl) resetDwell();
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
	// CR-0002: optional `intent` switches the extraction strategy.
	chrome.runtime.onMessage.addListener(
		(
			msg: {
				type: string;
				intent?:
					| 'selection'
					| 'yt_transcript'
					| 'yt_comments'
					| 'suggest_tags';
			},
			_sender,
			sendResponse,
		) => {
			if (msg.type !== 'force_extract') return;
			switch (msg.intent) {
				case 'selection': {
					const r = extractSelection();
					if (r.ok) sent = true;
					sendResponse(r);
					return;
				}
				case 'yt_transcript': {
					extractYouTubeTranscript().then((r) => {
						if (r.ok) sent = true;
						sendResponse(r);
					});
					return true; // async response
				}
				case 'yt_comments': {
					const r = extractYouTubeComments();
					if (r.ok) sent = true;
					sendResponse(r);
					return;
				}
				case 'suggest_tags': {
					// CR-0003: extracts but does NOT emit page_viewed.
					// SW receives payload via sendResponse and forwards to /tags/suggest.
					const r = extractForSuggest();
					sendResponse(r);
					return;
				}
				default: {
					const r = extract();
					if (r.ok) sent = true;
					sendResponse(r);
					return;
				}
			}
		},
	);
}
