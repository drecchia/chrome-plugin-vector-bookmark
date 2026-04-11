import {
	type IngestRequest,
	type SearchResult,
	type SearchResponse,
	type StatusResponse,
	type ForgetRequest,
} from '../../../proto/types';
import {
	connectDaemon,
	getDaemonBase,
	getAuthHeader,
	resetDaemon,
} from './native-bridge';

function authHeaders(): Record<string, string> {
	return {
		'Content-Type': 'application/json',
		Authorization: getAuthHeader(),
		Origin: chrome.runtime.getURL(''),
	};
}

async function checkResponse(res: Response): Promise<void> {
	if (res.status === 401) {
		// P2-09: daemon restarted — token rotated. Force re-handshake on next call.
		resetDaemon();
	}
	if (!res.ok) {
		throw new Error(`Daemon HTTP error ${res.status}: ${res.statusText}`);
	}
}

export async function ingest(req: IngestRequest): Promise<void> {
	await connectDaemon();
	const res = await fetch(`${getDaemonBase()}/ingest`, {
		method: 'POST',
		headers: authHeaders(),
		body: JSON.stringify(req),
	});
	await checkResponse(res);
}

export async function search(
	query: string,
	limit = 5,
): Promise<SearchResult[]> {
	await connectDaemon();
	const params = new URLSearchParams({ q: query, limit: String(limit) });
	const res = await fetch(`${getDaemonBase()}/search?${params}`, {
		method: 'GET',
		headers: authHeaders(),
	});
	await checkResponse(res);
	// P0-01: daemon returns {results: SearchResult[], total: number}
	const data = (await res.json()) as SearchResponse;
	return data.results;
}

export async function forget(req: ForgetRequest): Promise<void> {
	await connectDaemon();
	const res = await fetch(`${getDaemonBase()}/forget`, {
		method: 'DELETE',
		headers: authHeaders(),
		body: JSON.stringify(req),
	});
	await checkResponse(res);
}

export async function getStatus(): Promise<StatusResponse> {
	await connectDaemon();
	const res = await fetch(`${getDaemonBase()}/status`, {
		method: 'GET',
		headers: authHeaders(),
	});
	await checkResponse(res);
	return res.json() as Promise<StatusResponse>;
}

export async function healthz(): Promise<boolean> {
	try {
		const res = await fetch(`${getDaemonBase()}/healthz`, {
			method: 'GET',
		});
		return res.ok;
	} catch {
		return false;
	}
}
