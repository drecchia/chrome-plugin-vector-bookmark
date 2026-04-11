import { Readability } from '@mozilla/readability';

let dwellMs = 0;
let lastVisible = document.visibilityState === 'visible' ? Date.now() : null;
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

function hasSensitiveInputs(): boolean {
	return !!document.querySelector(
		'input[type="password"], input[autocomplete*="cc-"], input[autocomplete="one-time-code"], input[autocomplete="current-password"], input[autocomplete="new-password"]',
	);
}

function sendPage() {
	if (sent) return;
	sent = true;

	if (hasSensitiveInputs()) {
		chrome.runtime.sendMessage({ type: 'page_sensitive' });
		return;
	}

	const docClone = document.cloneNode(true) as Document;
	const article = new Readability(docClone).parse();

	if (!article || !article.textContent || article.textContent.length < 200)
		return;

	chrome.runtime.sendMessage({
		type: 'page_viewed',
		url: location.href,
		title: article.title || document.title,
		text: article.textContent || '',
		dwellMs,
	});
}

setInterval(() => {
	if (sent) return;
	accumulateDwell();
	if (dwellMs >= 30_000) {
		sendPage();
	}
}, 5_000);

document.addEventListener('focusin', (e) => {
	const el = e.target as HTMLElement;
	if (
		el.tagName === 'INPUT' &&
		(el as HTMLInputElement).type === 'password'
	) {
		chrome.runtime.sendMessage({ type: 'page_sensitive' });
	}
});
