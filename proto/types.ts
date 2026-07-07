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
	'selection' | 'yt_transcript' | 'yt_comments' | 'suggest_tags' | 'manual';

// ---- Tag suggestion (CR-0003) ----
export interface SuggestTagsRequest {
	url: string;
	title: string;
	text: string;
}

export interface SuggestTagsResponse {
	tags: string[];
}

// ---- Tag merge / dedup maintenance (CR-0007) ----
/** One LLM-proposed near-duplicate cluster. `canonical` is the suggested winner. */
export interface MergeGroup {
	canonical: string;
	variants: string[];
}

/** POST /tags/merge/suggest response — clusters of near-duplicate tags. */
export interface SuggestTagMergesResponse {
	groups: MergeGroup[];
}

/** POST /tags/merge — rename every `from` tag to `to` across all pages. */
export interface MergeTagsRequest {
	from: string[];
	to: string;
}

export interface MergeTagsResponse {
	tag: string;
	pagesAffected: number;
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
	snippets: string[];
	/** Unix milliseconds */
	visitTs: number;
	score: number;
	domain: string;
	tags: string[];
	/** "indexed" = manual popup ingest; "history" = passive dwell capture */
	source: 'indexed' | 'history';
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
	/** CR-0010: queue rows whose embed failed after all retries. */
	failed?: number;
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
	/**
	 * CR-0010: state of the page's queue row, if one exists.
	 * 'pending' = ingest accepted, still embedding; 'failed' = embed failed
	 * after all retries (retriable via /queue/retry). Absent when there is no
	 * queue row (already indexed and cleaned up, or never queued).
	 */
	queueStatus?: 'pending' | 'failed';
	/** Populated when queueStatus === 'failed': the final embed error. */
	lastError?: string;
}

// ---- Content script → SW signals ----
export interface DwellStartedMessage {
	type: 'dwell_started';
}
