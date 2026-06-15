/**
 * vbm_get_history + vbm_get_topics — time-windowed browsing timeline and the
 * top keywords for a period (interest evolution).
 */

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { daemonGet, handleApiError } from '../client.js';
import {
	ResponseFormat,
	capText,
	errorResult,
	render,
	toolResult,
	tsToISO,
} from '../format.js';
import {
	HistoryInputShape,
	TopicsInputShape,
	type HistoryInput,
	type TopicsInput,
} from '../schemas.js';
import type { HistoryResponse, TopicsResponse } from '../types.js';

const HISTORY_DESCRIPTION = `Get the user's chronological browsing history within a time window, newest first, with per-page keywords and a daily activity histogram.

\`from\` and \`to\` are REQUIRED Unix-millisecond timestamps. Compute them client-side (e.g. last 7 days = now-604800000 .. now).

Args:
  - from (number, required): window start, Unix ms (inclusive)
  - to (number, required): window end, Unix ms (exclusive)
  - limit (number): max pages, 1-500 (default 100)
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json):
  {
    "pages": [ { "url": string, "title": string, "domain": string, "visitTs": number, "keywords": string[] } ],
    "total": number,
    "daily": { "YYYY-MM-DD": number }   // page count per day over the FULL window
  }

Note: "daily" covers the entire [from,to) range regardless of "limit"; the page list is capped by "limit".

Examples:
  - "What did I browse yesterday?" -> from/to bounding yesterday
  - "Show my activity over the last month"`;

const TOPICS_DESCRIPTION = `Get the top keywords across all indexed page text within a time window — a snapshot of what the user was focused on during that period.

\`from\` and \`to\` are REQUIRED Unix-millisecond timestamps.

Args:
  - from (number, required): window start, Unix ms (inclusive)
  - to (number, required): window end, Unix ms (exclusive)
  - limit (number): max keywords, 1-50 (default 20)
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json):
  {
    "keywords": [ { "word": string, "count": number } ],
    "total_chunks": number,
    "from": number,
    "to": number
  }

Examples:
  - "What were my main topics last week?"
  - "Summarize my interests this month" -> inspect the keyword counts`;

export function registerTimelineTools(server: McpServer): void {
	server.registerTool(
		'vbm_get_history',
		{
			title: 'Get Browsing History',
			description: HISTORY_DESCRIPTION,
			inputSchema: HistoryInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: HistoryInput) => {
			if (input.to <= input.from) {
				return errorResult('Error: `to` must be greater than `from`.');
			}
			try {
				const data = await daemonGet<HistoryResponse>('/history', {
					from: input.from,
					to: input.to,
					limit: input.limit,
				});
				const structured = data as unknown as Record<string, unknown>;
				if (!data.pages.length) {
					return toolResult(
						`No browsing history between ${tsToISO(input.from)} and ${tsToISO(input.to)}.`,
						structured,
					);
				}
				const lines = [
					`# Browsing history ${tsToISO(input.from)} → ${tsToISO(input.to)} (${data.total})`,
					'',
				];
				for (const p of data.pages) {
					lines.push(`## ${p.title || p.url}`);
					lines.push(`- **URL**: ${p.url}`);
					lines.push(`- **Visited**: ${tsToISO(p.visitTs)}`);
					if (p.keywords.length)
						lines.push(`- **Keywords**: ${p.keywords.join(', ')}`);
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
						"Narrow the from/to window or lower 'limit' to reduce results.",
					),
					structured,
				);
			} catch (error) {
				return errorResult(handleApiError(error));
			}
		},
	);

	server.registerTool(
		'vbm_get_topics',
		{
			title: 'Get Top Topics',
			description: TOPICS_DESCRIPTION,
			inputSchema: TopicsInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: TopicsInput) => {
			if (input.to <= input.from) {
				return errorResult('Error: `to` must be greater than `from`.');
			}
			try {
				const data = await daemonGet<TopicsResponse>('/topics', {
					from: input.from,
					to: input.to,
					limit: input.limit,
				});
				const structured = data as unknown as Record<string, unknown>;
				if (!data.keywords.length) {
					return toolResult(
						`No indexed content between ${tsToISO(input.from)} and ${tsToISO(input.to)}.`,
						structured,
					);
				}
				const markdown = [
					`# Top topics ${tsToISO(input.from)} → ${tsToISO(input.to)}`,
					`(${data.total_chunks} chunks analyzed)`,
					'',
					...data.keywords.map((k) => `- **${k.word}** (${k.count})`),
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
