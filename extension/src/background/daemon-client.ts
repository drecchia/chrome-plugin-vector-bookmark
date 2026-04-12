import {
	type IngestRequest,
	type SearchResult,
	type SearchResponse,
	type ForgetRequest,
} from '../../../proto/types';
import { getDaemonConfig, getDaemonBase } from './native-bridge';

async function checkResponse(res: Response): Promise<void> {
	if (!res.ok) {
		throw new Error(`Daemon HTTP error ${res.status}: ${res.statusText}`);
	}
}

export async function ingest(req: IngestRequest): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/ingest`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(req),
	});
	await checkResponse(res);
}

export async function search(
	query: string,
	limit = 5,
): Promise<SearchResult[]> {
	const cfg = await getDaemonConfig();
	const params = new URLSearchParams({ q: query, limit: String(limit) });
	const res = await fetch(`${getDaemonBase(cfg)}/search?${params}`);
	await checkResponse(res);
	const data = (await res.json()) as SearchResponse;
	return data.results;
}

export async function forget(req: ForgetRequest): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/forget`, {
		method: 'DELETE',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(req),
	});
	await checkResponse(res);
}

export async function getStatus(): Promise<{
	indexed: number;
	pending: number;
	version: string;
	port: number;
}> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/status`);
	await checkResponse(res);
	const data = (await res.json()) as {
		indexed: number;
		pending: number;
		version: string;
	};
	return { ...data, port: cfg.port };
}

export async function healthz(): Promise<boolean> {
	try {
		const cfg = await getDaemonConfig();
		const res = await fetch(`${getDaemonBase(cfg)}/healthz`);
		return res.ok;
	} catch {
		return false;
	}
}
