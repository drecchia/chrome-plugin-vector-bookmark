/**
 * vbm_search — hybrid semantic + keyword search over the user's browsing memory.
 */

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { daemonGet, handleApiError, type Query } from '../client.js';
import {
	ResponseFormat,
	capText,
	errorResult,
	render,
	toolResult,
	tsToISO,
} from '../format.js';
import { SearchInputShape, type SearchInput } from '../schemas.js';
import type { SearchResponse } from '../types.js';

const DESCRIPTION = `Search the user's personal browsing memory (pages they visited or manually indexed) by free-text query.

Uses the daemon's hybrid retrieval: BM25 full-text + vector cosine similarity fused with Reciprocal Rank Fusion. Returns the most relevant pages with matching text snippets. This is the primary tool for answering "what did I read/see about X".

Args:
  - q (string, required): the free-text query
  - limit (number): max results, 1-1000 (default 20)
  - tags (string[]): include filter — only pages carrying ALL these tags
  - neg_tags (string[]): exclude filter — drop pages carrying ANY of these tags
  - min_confidence (number 0-1): relative score floor; higher is stricter
  - source ('indexed' | 'history'): 'indexed' = manually saved pages, 'history' = passively captured. Omit for both.
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json):
  {
    "results": [
      {
        "url": string, "title": string, "domain": string,
        "snippet": string,          // best matching excerpt
        "snippets": string[],       // additional excerpts
        "score": number,            // RRF score (higher = better)
        "visitTs": number,          // Unix ms of the visit
        "tags": string[],
        "source": "indexed" | "history"
      }
    ],
    "total": number
  }

Examples:
  - "What did I read about rust async runtimes?" -> q="rust async runtime"
  - "Find my saved pages tagged 'go' about sqlite" -> q="sqlite", tags=["go"], source="indexed"
  - "Pages about kubernetes but not helm" -> q="kubernetes", neg_tags=["helm"]

Errors:
  - Returns "No results found for '<q>'" when the memory has no match.
  - Returns a connection error with a healthz hint if the daemon is unreachable.`;

export function registerSearchTool(server: McpServer): void {
	server.registerTool(
		'vbm_search',
		{
			title: 'Search Browsing Memory',
			description: DESCRIPTION,
			inputSchema: SearchInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: SearchInput) => {
			try {
				const query: Query = {
					q: input.q,
					limit: input.limit,
					tag: input.tags,
					neg_tag: input.neg_tags,
					min_confidence: input.min_confidence,
					source: input.source,
				};
				const data = await daemonGet<SearchResponse>('/search', query);

				if (!data.results.length) {
					return toolResult(`No results found for '${input.q}'.`, {
						results: [],
						total: 0,
					});
				}

				const structured = data as unknown as Record<string, unknown>;
				const lines = [
					`# Search results for '${input.q}' (${data.total})`,
					'',
				];
				for (const r of data.results) {
					lines.push(`## ${r.title || r.url}`);
					lines.push(`- **URL**: ${r.url}`);
					lines.push(
						`- **Score**: ${r.score.toFixed(4)} · **Source**: ${r.source} · **Visited**: ${tsToISO(r.visitTs)}`,
					);
					if (r.tags.length)
						lines.push(`- **Tags**: ${r.tags.join(', ')}`);
					if (r.snippet) lines.push(`- ${r.snippet}`);
					lines.push('');
				}
				const text = render(
					input.response_format as ResponseFormat,
					lines.join('\n'),
					structured,
				);
				return toolResult(
					capText(
						text,
						"Lower 'limit' or add 'tags'/'min_confidence' filters to narrow results.",
					),
					structured,
				);
			} catch (error) {
				return errorResult(handleApiError(error));
			}
		},
	);
}
