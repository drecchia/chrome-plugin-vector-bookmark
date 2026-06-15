/**
 * vbm_get_status — daemon health/index counters.
 */

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { daemonGet, handleApiError } from '../client.js';
import { ResponseFormat, errorResult, render, toolResult } from '../format.js';
import { StatusInputShape, type StatusInput } from '../schemas.js';
import type { StatusResponse } from '../types.js';

const DESCRIPTION = `Get the current status of the Vector Bookmark daemon: how many pages are captured/indexed, the queue backlog, daemon version, and the active embedder.

Use this to confirm the daemon is alive and to understand the size of the memory before searching, or to check whether semantic search is active.

Args:
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json):
  {
    "visited": number,            // pages captured (history)
    "indexed": number,            // pages with indexed text/embeddings
    "pending": number,            // items still in the ingest queue
    "version": string,            // daemon version
    "embedder_version": string    // 'stub-v0' => semantic search disabled (BM25-only)
  }

Examples:
  - "Is my browsing memory daemon running and how big is it?"
  - "Is semantic search enabled?" -> check embedder_version != 'stub-v0'`;

export function registerStatusTool(server: McpServer): void {
	server.registerTool(
		'vbm_get_status',
		{
			title: 'Get Daemon Status',
			description: DESCRIPTION,
			inputSchema: StatusInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: StatusInput) => {
			try {
				const data = await daemonGet<StatusResponse>('/status');
				const structured = data as unknown as Record<string, unknown>;
				const semantic =
					data.embedder_version === 'stub-v0'
						? 'disabled (BM25-only)'
						: `enabled (${data.embedder_version})`;
				const markdown = [
					'# Vector Bookmark daemon status',
					'',
					`- **Version**: ${data.version}`,
					`- **Visited (history)**: ${data.visited}`,
					`- **Indexed**: ${data.indexed}`,
					`- **Pending in queue**: ${data.pending}`,
					`- **Semantic search**: ${semantic}`,
				].join('\n');
				return toolResult(
					render(
						input.response_format as ResponseFormat,
						markdown,
						structured,
					),
					structured,
				);
			} catch (error) {
				return errorResult(handleApiError(error));
			}
		},
	);
}
