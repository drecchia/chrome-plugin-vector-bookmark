import { describe, it, expect } from 'vitest';
import { normaliseBlacklistEntry } from './native-bridge';

describe('normaliseBlacklistEntry', () => {
	it('lowercases plain domains', () => {
		expect(normaliseBlacklistEntry('Example.COM')).toBe('example.com');
	});

	it('strips a leading wildcard "*."', () => {
		expect(normaliseBlacklistEntry('*.foo.com')).toBe('foo.com');
		expect(normaliseBlacklistEntry('*.SUB.bar.io')).toBe('sub.bar.io');
	});

	it('only strips wildcard at the start (not in the middle)', () => {
		expect(normaliseBlacklistEntry('a.*.b.com')).toBe('a.*.b.com');
	});

	it('trims surrounding whitespace', () => {
		expect(normaliseBlacklistEntry('   foo.com  ')).toBe('foo.com');
	});

	it('passes through /regex/ entries unchanged', () => {
		expect(normaliseBlacklistEntry('/^chat\\./i')).toBe('/^chat\\./i');
		// Casing must be preserved inside regexes.
		expect(normaliseBlacklistEntry('/MixedCase/')).toBe('/MixedCase/');
	});

	it('treats a single "/" as a plain string, not a regex', () => {
		// startsWith('/') is true but lastIndexOf('/') > 0 is false → plain path.
		expect(normaliseBlacklistEntry('/')).toBe('/');
	});
});
