import { describe, it, expect } from 'vitest';
import { parseTagsCSV, mergeTagsCSV } from './tags';

describe('parseTagsCSV', () => {
	it('splits, trims, and drops empties', () => {
		expect(parseTagsCSV('a, b ,  ,c')).toEqual(['a', 'b', 'c']);
	});

	it('returns [] for empty input', () => {
		expect(parseTagsCSV('')).toEqual([]);
		expect(parseTagsCSV('   ')).toEqual([]);
		expect(parseTagsCSV(',,,')).toEqual([]);
	});

	it('preserves duplicates (caller may dedupe)', () => {
		expect(parseTagsCSV('x, x, X')).toEqual(['x', 'x', 'X']);
	});
});

describe('mergeTagsCSV', () => {
	it('appends new generated tags after existing ones', () => {
		expect(mergeTagsCSV('a, b', ['c', 'd'])).toBe('a, b, c, d');
	});

	it('drops generated tags that match existing case-insensitively', () => {
		expect(mergeTagsCSV('AI, work', ['ai', 'reading'])).toBe(
			'AI, work, reading',
		);
	});

	it('dedupes within the existing CSV first', () => {
		expect(mergeTagsCSV('a, A, b', [])).toBe('a, b');
	});

	it('preserves the original casing of the first occurrence', () => {
		expect(mergeTagsCSV('Rust', ['RUST', 'go'])).toBe('Rust, go');
	});

	it('handles all-empty inputs gracefully', () => {
		expect(mergeTagsCSV('', [])).toBe('');
		expect(mergeTagsCSV('  ,  ', ['  '])).toBe('');
	});

	it('trims whitespace on the generated side too', () => {
		expect(mergeTagsCSV('a', ['  b  '])).toBe('a, b');
	});
});
