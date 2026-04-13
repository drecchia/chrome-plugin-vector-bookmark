export const DEFAULT_HOST = '127.0.0.1';
export const DEFAULT_PORT = 7532;

export interface DaemonConfig {
	host: string;
	port: number;
}

export async function getDaemonConfig(): Promise<DaemonConfig> {
	const result = await chrome.storage.local.get(['vbmHost', 'vbmPort']);
	return {
		host: (result.vbmHost as string) || DEFAULT_HOST,
		port: (result.vbmPort as number) || DEFAULT_PORT,
	};
}

export async function saveDaemonConfig(
	config: Partial<DaemonConfig>,
): Promise<void> {
	const update: Record<string, unknown> = {};
	if (config.host !== undefined) update.vbmHost = config.host;
	if (config.port !== undefined) update.vbmPort = config.port;
	await chrome.storage.local.set(update);
}

export function getDaemonBase(config: DaemonConfig): string {
	return `http://${config.host}:${config.port}`;
}

export const DEFAULT_DWELL_MS = 10_000;

export async function getDwellThreshold(): Promise<number> {
	const result = await chrome.storage.local.get('vbmDwellMs');
	const v = result.vbmDwellMs as number | undefined;
	return v && v > 0 ? v : DEFAULT_DWELL_MS;
}

export async function saveDwellThreshold(ms: number): Promise<void> {
	await chrome.storage.local.set({ vbmDwellMs: ms });
}

/** Normalise a domain/pattern for the daemon blacklist.
 *  - Regex entries (/pattern/) stored as-is.
 *  - Plain domains: strip leading "*." and lowercase.
 */
export function normaliseBlacklistEntry(raw: string): string {
	const trimmed = raw.trim();
	// Treat as regex if it starts with / and contains a closing / (e.g. /pattern/ or /pattern/i).
	if (trimmed.startsWith('/') && trimmed.lastIndexOf('/') > 0) return trimmed;
	return trimmed.replace(/^\*\./, '').toLowerCase();
}
