import { Readability } from '@mozilla/readability';

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

function extract(starRank = false): { ok: boolean; error?: string } {
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
		starRank,
	});
	return { ok: true };
}

// Guard: prevent double initialization when injected programmatically into
// tabs that were already open (chrome.scripting.executeScript path).
if (!window.__vbm_cs) {
	window.__vbm_cs = true;

	let dwellMs = 0;
	let lastVisible =
		document.visibilityState === 'visible' ? Date.now() : null;
	let sent = false;

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

	const timer = setInterval(() => {
		// Stop cleanly if extension was reloaded — avoids the TypeError.
		if (!chrome.runtime?.id) {
			clearInterval(timer);
			return;
		}
		if (sent) return;
		accumulateDwell();
		if (dwellMs >= 30_000) {
			if (hasSensitiveInputs()) {
				runtimeSend({ type: 'page_sensitive' });
				return;
			}
			const article = new Readability(
				document.cloneNode(true) as Document,
			).parse();
			if (!article?.textContent || article.textContent.length < 200)
				return;
			sent = true;
			runtimeSend({
				type: 'page_viewed',
				url: location.href,
				title: article.title || document.title,
				text: article.textContent,
				dwellMs,
			});
		}
	}, 5_000);

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
		(msg: { type: string; starRank?: boolean }, _sender, sendResponse) => {
			if (msg.type !== 'force_extract') return;
			const result = extract(msg.starRank ?? false);
			if (result.ok) sent = true;
			sendResponse(result);
		},
	);
}
