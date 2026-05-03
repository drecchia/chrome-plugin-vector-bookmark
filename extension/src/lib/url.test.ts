import { describe, it, expect } from 'vitest';
import {
	sanitizeUrl,
	matchesBlacklistEntry,
	matchesAnyBlacklist,
	STRIPPED_PARAMS,
} from './url';

describe('sanitizeUrl', () => {
	it('strips every tracking param in the deny list', () => {
		const u = new URL('https://example.com/path');
		for (const p of STRIPPED_PARAMS) u.searchParams.set(p, 'x');
		u.searchParams.set('keep', '1');
		const cleaned = new URL(sanitizeUrl(u.toString()));
		for (const p of STRIPPED_PARAMS) {
			expect(cleaned.searchParams.has(p)).toBe(false);
		}
		expect(cleaned.searchParams.get('keep')).toBe('1');
	});

	it('preserves non-tracking query params (id, q, page)', () => {
		const out = sanitizeUrl(
			'https://example.com/?id=42&q=hello&page=3&utm_source=ads',
		);
		expect(out).toBe('https://example.com/?id=42&q=hello&page=3');
	});

	it('preserves the path and fragment', () => {
		expect(sanitizeUrl('https://example.com/a/b#frag')).toBe(
			'https://example.com/a/b#frag',
		);
	});

	it('returns the original string when URL is invalid', () => {
		expect(sanitizeUrl('not a url')).toBe('not a url');
	});

	it('is idempotent', () => {
		const dirty = 'https://example.com/?utm_source=x&fbclid=y&q=keep';
		const clean = sanitizeUrl(dirty);
		expect(sanitizeUrl(clean)).toBe(clean);
	});
});

describe('matchesBlacklistEntry', () => {
	it('exact match on plain domain', () => {
		expect(matchesBlacklistEntry('example.com', 'example.com')).toBe(true);
	});

	it('matches subdomains via suffix', () => {
		expect(matchesBlacklistEntry('foo.example.com', 'example.com')).toBe(
			true,
		);
		expect(matchesBlacklistEntry('a.b.example.com', 'example.com')).toBe(
			true,
		);
	});

	it('does NOT match a sibling domain that ends in the same string', () => {
		// "barexample.com" must not match the entry "example.com".
		expect(matchesBlacklistEntry('barexample.com', 'example.com')).toBe(
			false,
		);
	});

	it('lowercases the hostname before comparing', () => {
		expect(matchesBlacklistEntry('EXAMPLE.COM', 'example.com')).toBe(true);
	});

	it('supports /regex/ entries with default i flag', () => {
		expect(matchesBlacklistEntry('CHATGPT.com', '/^chat/')).toBe(true);
	});

	it('supports /regex/flags entries — explicit flags drop the default i', () => {
		// Hostname is lowercased before the regex runs, so case sensitivity
		// only matters for the *pattern* side. With default (no flags) the
		// helper applies i, so an uppercase pattern still matches.
		expect(matchesBlacklistEntry('chat.openai.com', '/^CHAT\\./')).toBe(
			true,
		);
		// Once the caller supplies any flags string, the helper does NOT
		// inject i — the same uppercase pattern stops matching.
		expect(matchesBlacklistEntry('chat.openai.com', '/^CHAT\\./g')).toBe(
			false,
		);
	});

	it('returns false on a malformed regex', () => {
		expect(matchesBlacklistEntry('foo.com', '/[unclosed/')).toBe(false);
	});
});

describe('matchesAnyBlacklist', () => {
	it('returns true if any pattern matches', () => {
		expect(
			matchesAnyBlacklist('chat.openai.com', [
				'example.com',
				'/^chat\\./',
			]),
		).toBe(true);
	});

	it('returns false when none match', () => {
		expect(matchesAnyBlacklist('foo.dev', ['example.com', 'bar.dev'])).toBe(
			false,
		);
	});

	it('handles an empty pattern list', () => {
		expect(matchesAnyBlacklist('anything.com', [])).toBe(false);
	});
});
