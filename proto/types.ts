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
export type IngestMode = 'full_text' | 'llm_summary' | 'manual' | 'meta_only';

export interface IngestRequest {
	url: string;
	title: string;
	text: string;
	/** Unix milliseconds */
	visitTs: number;
	dwellMs: number;
	domain: string;
	/** User-assigned tags. */
	tags?: string[];
	/**
	 * If true, `tags` becomes the authoritative final tag list for the page
	 * (additions + removals applied). If false/absent, tags are merged.
	 */
	setTags?: boolean;
	/** How to post-process `text` before chunking. Default: full_text. */
	mode?: IngestMode;
}

// ---- Tags ----
export interface TagCount {
	tag: string;
	count: number;
}

// ---- Extraction intents (client-side only) ----
// Marks a non-default extraction strategy run by the content script. The
// daemon sees `mode: "manual"` for all these — text is fully prepared client-
// side. CR-0002. `suggest_tags` (CR-0003) extracts but doesn't ingest;
// payload is forwarded to /tags/suggest via the SW.
export type ExtractIntent =
	| 'selection'
	| 'yt_transcript'
	| 'yt_comments'
	| 'suggest_tags'
	| 'manual';

// ---- Tag suggestion (CR-0003) ----
export interface SuggestTagsRequest {
	url: string;
	title: string;
	text: string;
}

export interface SuggestTagsResponse {
	tags: string[];
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
	/** Tags currently assigned to the page (empty if exists=false). */
	tags?: string[];
}

// ---- Content script → SW signals ----
export interface DwellStartedMessage {
	type: 'dwell_started';
}
