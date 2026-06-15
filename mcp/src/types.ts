/**
 * TypeScript interfaces for the vbmd HTTP API responses.
 *
 * Field names mirror the daemon's JSON output verbatim (see
 * daemon/internal/server/routes.go and proto/types.ts). Do not rename fields —
 * these shapes must match the wire format exactly.
 */

/** A single hit from GET /search. */
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
	/** "indexed" = manual ingest; "history" = passive dwell capture */
	source: 'indexed' | 'history';
}

export interface SearchResponse {
	results: SearchResult[];
	total: number;
}

/** GET /status — note: embedder_version is snake_case on the wire. */
export interface StatusResponse {
	visited: number;
	indexed: number;
	pending: number;
	version: string;
	embedder_version: string;
}

/** GET /page?url= */
export interface PageStatusResponse {
	exists: boolean;
	indexed: boolean;
	tags: string[];
}

/** GET /tags */
export interface TagCount {
	tag: string;
	count: number;
}
export interface TagsResponse {
	tags: TagCount[];
}

/** GET /pages?tag= */
export interface TaggedPage {
	url: string;
	title: string;
	domain: string;
	/** Unix milliseconds */
	visitTs: number;
	tags: string[];
}
export interface PagesByTagResponse {
	pages: TaggedPage[];
	total: number;
}

/** GET /topics */
export interface KeywordCount {
	word: string;
	count: number;
}
export interface TopicsResponse {
	keywords: KeywordCount[];
	total_chunks: number;
	from: number;
	to: number;
}

/** GET /history */
export interface HistoryPage {
	url: string;
	title: string;
	domain: string;
	/** Unix milliseconds */
	visitTs: number;
	keywords: string[];
}
export interface HistoryResponse {
	pages: HistoryPage[];
	total: number;
	/** Map of YYYY-MM-DD -> page count, spanning the full [from,to) range. */
	daily: Record<string, number>;
}

/** GET /export */
export interface ExportChunk {
	chunkIdx: number;
	text: string;
}
export interface ExportPage {
	url: string;
	title: string;
	domain: string;
	/** Unix milliseconds */
	visitTs: number;
	dwellMs: number;
	chunks: ExportChunk[];
}
export interface ExportResponse {
	pages: ExportPage[];
	total: number;
}
