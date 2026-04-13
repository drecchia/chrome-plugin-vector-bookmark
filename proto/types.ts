// ---- Page Meta (HTML meta tags) ----
export interface PageMeta {
	description?: string;
	keywords?: string;
	ogTitle?: string;
	ogDescription?: string;
	ogImage?: string;
	author?: string;
}

// ---- Visit (passive history, no text extraction) ----
export interface VisitRequest {
	url: string;
	title: string;
	/** Unix milliseconds */
	visitTs: number;
	dwellMs: number;
	domain: string;
	meta?: PageMeta;
}

// ---- Ingest (manual full-index with text) ----
export interface IngestRequest {
	url: string;
	title: string;
	text: string;
	/** Unix milliseconds */
	visitTs: number;
	dwellMs: number;
	domain: string;
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
	visited: number;
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

// ---- Page status ----
export interface PageStatusResponse {
	exists: boolean;
	indexed: boolean;
}

// ---- Content script → SW signals ----
export interface DwellStartedMessage {
	type: 'dwell_started';
}
