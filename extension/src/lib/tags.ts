// Pure helpers for the comma-separated tag input. Extracted from popup/App.tsx
// so they are unit-testable without React.

export function parseTagsCSV(csv: string): string[] {
	return csv
		.split(',')
		.map((t) => t.trim())
		.filter((t) => t.length > 0);
}

// CR-0003: merge generated tags into the existing CSV string, preserving
// order and dropping case-insensitive duplicates.
export function mergeTagsCSV(existing: string, generated: string[]): string {
	const seen = new Set<string>();
	const out: string[] = [];
	for (const raw of existing.split(',')) {
		const t = raw.trim();
		if (!t) continue;
		const k = t.toLowerCase();
		if (seen.has(k)) continue;
		seen.add(k);
		out.push(t);
	}
	for (const t of generated) {
		const k = t.trim().toLowerCase();
		if (!k || seen.has(k)) continue;
		seen.add(k);
		out.push(t.trim());
	}
	return out.join(', ');
}
