// ---- Ingest ----
export interface IngestRequest {
	url: string;
	title: string;
	text: string;
	/** Unix milliseconds */
	visitTs: number;
	dwellMs: number;
	domain: string;
	/** If true, page is marked as a reference (1.5× search score boost). */
	starRank?: boolean;
}

// ---- Search ----
export interface SearchRequest {
	q: string;
	limit?: number;
}

export interface SearchResult {
	url: string;
	title: string;
	snippet: string;
	/** Unix milliseconds */
	visitTs: number;
	score: number;
	domain: string;
}

export interface SearchResponse {
	results: SearchResult[];
	total: number;
}

// ---- Forget ----
export type ForgetType = 'url' | 'domain' | 'timerange';

export interface ForgetRequest {
	type: ForgetType;
	/** URL string, domain string, or "fromMs:toMs" for timerange */
	value: string;
}

// ---- Status ----
export interface StatusResponse {
	indexed: number;
	pending: number;
	version: string;
	daemonPort: number | null;
	captureEnabled: boolean;
	/** 'stub-v0' means semantic search is disabled (BM25-only) */
	embedderVersion: string;
}

// ---- WebSocket push ----
export interface WsStatusMessage {
	type: 'status';
	indexed: number;
	pending: number;
}

// ---- Daemon connection config ----
export interface DaemonState {
	host: string;
	port: number;
}
