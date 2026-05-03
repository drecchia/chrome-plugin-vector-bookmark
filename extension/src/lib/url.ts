// Pure URL/blacklist helpers. Extracted from background/service-worker.ts so
// they can be unit-tested without loading the SW (which has top-level chrome.*
// calls that would explode under vitest).

// Tracking / session-y query params stripped before indexing or recording a
// visit. Keep this list narrow — only params known to be irrelevant to the
// page identity. Anything that selects content (id, q, page) must NOT be
// included or revisits will fail to dedupe.
export const STRIPPED_PARAMS = [
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
] as const;

export function sanitizeUrl(url: string): string {
	try {
		const u = new URL(url);
		for (const p of STRIPPED_PARAMS) u.searchParams.delete(p);
		return u.toString();
	} catch {
		return url;
	}
}

// Match a single blacklist entry against a hostname.
// Entries can be either:
//   - `/regex/flags` — JavaScript RegExp (default flag is `i`).
//   - plain domain — exact or suffix match (`foo.com` blocks `foo.com` and
//     `bar.foo.com`, but not `barfoo.com`).
export function matchesBlacklistEntry(
	hostname: string,
	entry: string,
): boolean {
	const h = hostname.toLowerCase();
	if (entry.startsWith('/')) {
		const lastSlash = entry.lastIndexOf('/');
		if (lastSlash > 0) {
			const pattern = entry.slice(1, lastSlash);
			const flags = entry.slice(lastSlash + 1);
			try {
				return new RegExp(pattern, flags || 'i').test(h);
			} catch {
				return false;
			}
		}
	}
	return h === entry || h.endsWith('.' + entry);
}

export function matchesAnyBlacklist(
	hostname: string,
	patterns: string[],
): boolean {
	return patterns.some((p) => matchesBlacklistEntry(hostname, p));
}
