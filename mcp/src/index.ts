#!/usr/bin/env node
/**
 * Vector Bookmark MCP server.
 *
 * Exposes the local vbmd daemon's read-only browsing-memory API as MCP tools so
 * an LLM agent can search, browse, and summarize what the user has read.
 *
 * Transport: stdio (single-user, local subprocess). All logging goes to stderr;
 * stdout is reserved for the MCP protocol stream.
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { isDaemonHealthy } from './client.js';
import { DAEMON_BASE_URL } from './constants.js';
import { registerExportTool } from './tools/export.js';
import { registerPageTool } from './tools/page.js';
import { registerSearchTool } from './tools/search.js';
import { registerStatusTool } from './tools/status.js';
import { registerTagTools } from './tools/tags.js';
import { registerTimelineTools } from './tools/timeline.js';

const server = new McpServer({
	name: 'vector-bookmark-mcp-server',
	version: '1.0.0',
});

registerSearchTool(server);
registerStatusTool(server);
registerTagTools(server);
registerTimelineTools(server);
registerPageTool(server);
registerExportTool(server);

async function main(): Promise<void> {
	// Non-fatal startup probe — the daemon may be started after this server.
	const healthy = await isDaemonHealthy();
	if (!healthy) {
		console.error(
			`[vector-bookmark-mcp] WARNING: vbmd daemon not reachable at ${DAEMON_BASE_URL}. ` +
				`Tools will return connection errors until it is running.`,
		);
	}

	const transport = new StdioServerTransport();
	await server.connect(transport);
	console.error(
		`[vector-bookmark-mcp] running (stdio) → daemon ${DAEMON_BASE_URL}`,
	);
}

main().catch((error) => {
	console.error('[vector-bookmark-mcp] fatal:', error);
	process.exit(1);
});
