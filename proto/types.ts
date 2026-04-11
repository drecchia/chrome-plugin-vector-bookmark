// ---- Ingest ----
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
	indexed: number;
	pending: number;
	version: string;
}

// ---- WebSocket push ----
export interface WsStatusMessage {
	type: 'status';
	indexed: number;
	pending: number;
}

// ---- Native Messaging ----
export interface NMHandshakeRequest {
	type: 'handshake';
	extensionId: string;
}

export interface NMHandshakeOk {
	type: 'handshake_ok';
	port: number;
	token: string;
}

export interface NMHandshakeError {
	type: 'handshake_error';
	error: string;
}

export type NMResponse = NMHandshakeOk | NMHandshakeError;

// ---- Daemon connection state ----
export interface DaemonState {
	port: number | null;
	token: string | null;
}
