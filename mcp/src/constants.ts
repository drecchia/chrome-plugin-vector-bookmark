/**
 * Shared constants for the Vector Bookmark MCP server.
 *
 * Connection target is the local vbmd daemon. Host/port/token are resolved from
 * env at startup, mirroring the daemon's own VBM_* configuration variables.
 */

const host = process.env.VBM_HOST || '127.0.0.1';
const port = process.env.VBM_PORT || '7532';

/** Base URL of the running vbmd daemon, e.g. http://127.0.0.1:7532 */
export const DAEMON_BASE_URL = `http://${host}:${port}`;

/** Optional bearer token. When set, the daemon requires it on every request. */
export const AUTH_TOKEN = process.env.VBM_AUTH_TOKEN || '';

/** Maximum response size in characters before a tool truncates its payload. */
export const CHARACTER_LIMIT = 25000;

/** Display strings reused in error hints. */
export const HEALTH_HINT = `Is vbmd running? Check with: curl ${DAEMON_BASE_URL}/healthz`;
