/**
 * HTTP client for the vbmd daemon.
 *
 * All wrapped endpoints are GET, so a single helper covers them. Auth is an
 * optional bearer token; when VBM_AUTH_TOKEN is unset the daemon allows
 * unauthenticated loopback requests.
 */

import axios, { AxiosError } from 'axios';
import { AUTH_TOKEN, DAEMON_BASE_URL, HEALTH_HINT } from './constants.js';

/** Query param value: string, number, boolean, or repeated (multi-value). */
type QueryValue = string | number | boolean | string[] | undefined;
export type Query = Record<string, QueryValue>;

/**
 * Perform a GET request against the daemon and return the parsed JSON body.
 * Throws on network/HTTP errors; callers convert via {@link handleApiError}.
 */
export async function daemonGet<T>(path: string, query?: Query): Promise<T> {
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (AUTH_TOKEN) {
		headers.Authorization = `Bearer ${AUTH_TOKEN}`;
	}
	const response = await axios.get<T>(`${DAEMON_BASE_URL}${path}`, {
		params: query,
		// axios serializes string[] as repeated params (tag=a&tag=b) by default.
		paramsSerializer: { indexes: null },
		timeout: 30000,
		headers,
	});
	return response.data;
}

/** Extract the daemon's {"error":"..."} message when present. */
function daemonErrorMessage(error: AxiosError): string | undefined {
	const data = error.response?.data as { error?: string } | undefined;
	return data && typeof data.error === 'string' ? data.error : undefined;
}

/**
 * Convert any thrown error into an actionable, agent-facing message. Never
 * leaks daemon internals — only status-derived guidance.
 */
export function handleApiError(error: unknown): string {
	if (axios.isAxiosError(error)) {
		if (error.code === 'ECONNREFUSED' || error.code === 'ECONNRESET') {
			return `Error: cannot reach the Vector Bookmark daemon at ${DAEMON_BASE_URL}. ${HEALTH_HINT}`;
		}
		if (error.code === 'ECONNABORTED') {
			return 'Error: request to the daemon timed out. The daemon may be busy reindexing — try again shortly.';
		}
		const detail = daemonErrorMessage(error);
		switch (error.response?.status) {
			case 400:
				return `Error: bad request${detail ? ` (${detail})` : ''}. Check the tool arguments.`;
			case 401:
				return "Error: unauthorized. The daemon requires VBM_AUTH_TOKEN — set it in this MCP server's environment to the same value the daemon uses.";
			case 404:
				return `Error: not found${detail ? ` (${detail})` : ''}.`;
			case 503:
				return `Error: feature unavailable${detail ? ` (${detail})` : ''}. The daemon may be missing optional config (e.g. an embedder/LLM).`;
			default:
				if (error.response) {
					return `Error: daemon returned status ${error.response.status}${detail ? ` (${detail})` : ''}.`;
				}
				return `Error: network failure contacting the daemon at ${DAEMON_BASE_URL}. ${HEALTH_HINT}`;
		}
	}
	return `Error: unexpected error: ${error instanceof Error ? error.message : String(error)}`;
}

/** Lightweight startup probe — true if GET /healthz reports ok. */
export async function isDaemonHealthy(): Promise<boolean> {
	try {
		const data = await daemonGet<{ ok?: boolean }>('/healthz');
		return data.ok === true;
	} catch {
		return false;
	}
}
