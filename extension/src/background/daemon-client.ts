import {
	type IngestRequest,
	type VisitRequest,
	type SearchResult,
	type SearchResponse,
	type ForgetRequest,
	type PageStatusResponse,
	type TagCount,
	type SuggestTagsRequest,
	type SuggestTagsResponse,
} from '../../../proto/types';
import { getDaemonConfig, getDaemonBase, authHeader } from './native-bridge';

async function checkResponse(res: Response): Promise<void> {
	if (!res.ok) {
		let msg = `${res.status} ${res.statusText}`;
		try {
			const body = await res.json();
			if (body?.error) msg = body.error;
		} catch {
			// non-JSON body — keep status text
		}
		throw new Error(msg);
	}
}

export async function recordVisit(req: VisitRequest): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/visit`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json', ...authHeader(cfg) },
		body: JSON.stringify(req),
	});
	await checkResponse(res);
}

export async function ingest(req: IngestRequest): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/ingest`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json', ...authHeader(cfg) },
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
	const res = await fetch(`${getDaemonBase(cfg)}/search?${params}`, {
		headers: authHeader(cfg),
	});
	await checkResponse(res);
	const data = (await res.json()) as SearchResponse;
	return data.results;
}

export async function forget(req: ForgetRequest): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/forget`, {
		method: 'DELETE',
		headers: { 'Content-Type': 'application/json', ...authHeader(cfg) },
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
	const res = await fetch(`${getDaemonBase(cfg)}/status`, {
		headers: authHeader(cfg),
	});
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
		const res = await fetch(`${getDaemonBase(cfg)}/healthz`, {
			headers: authHeader(cfg),
		});
		return res.ok;
	} catch {
		return false;
	}
}

export async function pageStatus(url: string): Promise<PageStatusResponse> {
	try {
		const cfg = await getDaemonConfig();
		const params = new URLSearchParams({ url });
		const res = await fetch(`${getDaemonBase(cfg)}/page?${params}`, {
			headers: authHeader(cfg),
		});
		if (!res.ok) return { exists: false, indexed: false };
		return (await res.json()) as PageStatusResponse;
	} catch {
		return { exists: false, indexed: false };
	}
}

export async function suggestTags(
	req: SuggestTagsRequest,
): Promise<SuggestTagsResponse> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/tags/suggest`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json', ...authHeader(cfg) },
		body: JSON.stringify(req),
	});
	if (!res.ok) {
		const data = (await res.json().catch(() => ({}))) as {
			error?: string;
		};
		throw new Error(data.error ?? `HTTP ${res.status}`);
	}
	return (await res.json()) as SuggestTagsResponse;
}

export async function listTags(): Promise<TagCount[]> {
	try {
		const cfg = await getDaemonConfig();
		const res = await fetch(`${getDaemonBase(cfg)}/tags`, {
			headers: authHeader(cfg),
		});
		if (!res.ok) return [];
		const data = (await res.json()) as { tags: TagCount[] };
		return data.tags ?? [];
	} catch {
		return [];
	}
}

export async function getBlacklist(): Promise<string[]> {
	try {
		const cfg = await getDaemonConfig();
		const res = await fetch(`${getDaemonBase(cfg)}/blacklist`, {
			headers: authHeader(cfg),
		});
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
		headers: { 'Content-Type': 'application/json', ...authHeader(cfg) },
		body: JSON.stringify({ pattern }),
	});
	await checkResponse(res);
}

export async function removeFromBlacklist(pattern: string): Promise<void> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/blacklist`, {
		method: 'DELETE',
		headers: { 'Content-Type': 'application/json', ...authHeader(cfg) },
		body: JSON.stringify({ pattern }),
	});
	await checkResponse(res);
}

export async function reindex(): Promise<{ started: boolean }> {
	const cfg = await getDaemonConfig();
	const res = await fetch(`${getDaemonBase(cfg)}/admin/reindex`, {
		method: 'POST',
		headers: authHeader(cfg),
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
	const res = await fetch(`${getDaemonBase(cfg)}/admin/reindex/status`, {
		headers: authHeader(cfg),
	});
	await checkResponse(res);
	return res.json() as Promise<{
		running: boolean;
		done: number;
		total: number;
	}>;
}
