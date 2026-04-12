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

/** Normalise a domain/pattern for the daemon blocklist.
 *  - Regex entries (/pattern/) stored as-is.
 *  - Plain domains: strip leading "*." and lowercase.
 */
export function normaliseBlocklistEntry(raw: string): string {
	const trimmed = raw.trim();
	if (trimmed.startsWith('/') && trimmed.endsWith('/')) return trimmed;
	return trimmed.replace(/^\*\./, '').toLowerCase();
}
