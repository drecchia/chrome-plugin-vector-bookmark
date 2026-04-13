import {
	type IngestRequest,
	type VisitRequest,
	type SearchResult,
	type SearchResponse,
	type ForgetRequest,
	type PageStatusResponse,
} from '../../../proto/types';
import { getDaemonConfig, getDaemonBase } from './native-bridge';

async function checkResponse(res: Response): Promise<void> {
	if (!res.ok) {
		throw new Error(`Daemon HTTP error ${res.status}: ${res.statusText}`);
	}
}

export async function recordVisit(req: VisitRequest): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/visit`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(req),
	});
	await checkResponse(res);
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
	visited: number;
	indexed: number;
	pending: number;
	version: string;
	port: number;
	embedderVersion: string;
}> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/status`);
	await checkResponse(res);
	const data = (await res.json()) as {
		visited: number;
		indexed: number;
		pending: number;
		version: string;
		embedder_version: string;
	};
	return {
		...data,
		port: cfg.port,
		embedderVersion: data.embedder_version ?? 'stub-v0',
	};
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

export async function pageStatus(url: string): Promise<PageStatusResponse> {
	try {
		const cfg = await getDaemonConfig();
		const params = new URLSearchParams({ url });
		const res = await fetch(`${getDaemonBase(cfg)}/page?${params}`);
		if (!res.ok) return { exists: false, indexed: false };
		return (await res.json()) as PageStatusResponse;
	} catch {
		return { exists: false, indexed: false };
	}
}

export async function getBlacklist(): Promise<string[]> {
	try {
		const cfg = await getDaemonConfig();
		const res = await fetch(`${getDaemonBase(cfg)}/blacklist`);
		if (!res.ok) return [];
		const data = (await res.json()) as { patterns: string[] };
		return data.patterns ?? [];
	} catch {
		return [];
	}
}

export async function addToBlacklist(pattern: string): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/blacklist`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ pattern }),
	});
	await checkResponse(res);
}

export async function removeFromBlacklist(pattern: string): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/blacklist`, {
		method: 'DELETE',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ pattern }),
	});
	await checkResponse(res);
}

export async function reindex(): Promise<{ started: boolean }> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/admin/reindex`, {
		method: 'POST',
	});
	if (res.status === 409) return { started: false }; // already running
	await checkResponse(res);
	return res.json() as Promise<{ started: boolean }>;
}

export async function getReindexStatus(): Promise<{
	running: boolean;
	done: number;
	total: number;
}> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/admin/reindex/status`);
	await checkResponse(res);
	return res.json() as Promise<{
		running: boolean;
		done: number;
		total: number;
	}>;
}
