/**
 * vbm_export — full dump of indexed pages and their chunk text. Potentially
 * large, so the payload is truncated to stay within the character budget.
 */

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { daemonGet, handleApiError } from '../client.js';
import { CHARACTER_LIMIT } from '../constants.js';
import { ResponseFormat, errorResult, toolResult, tsToISO } from '../format.js';
import { ExportInputShape, type ExportInput } from '../schemas.js';
import type { ExportPage, ExportResponse } from '../types.js';

const DESCRIPTION = `Export the full set of indexed pages with their chunked text. This is a bulk dump — prefer vbm_search for targeted retrieval. Large exports are truncated to fit a response budget; the response reports total vs. returned counts.

Args:
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json):
  {
    "pages": [ { "url": string, "title": string, "domain": string, "visitTs": number, "dwellMs": number, "chunks": [ { "chunkIdx": number, "text": string } ] } ],
    "total": number,             // total indexed pages on the daemon
    "returned": number,          // pages included in this response
    "truncated": boolean,
    "truncation_message"?: string
  }

Examples:
  - "Dump everything in my browsing memory" (small datasets)
  - "Export my saved pages so I can review them"`;

export function registerExportTool(server: McpServer): void {
	server.registerTool(
		'vbm_export',
		{
			title: 'Export Indexed Pages',
			description: DESCRIPTION,
			inputSchema: ExportInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: ExportInput) => {
			try {
				const data = await daemonGet<ExportResponse>('/export');
				const total = data.total;

				// Trim pages until the serialized payload fits the character budget.
				let pages: ExportPage[] = data.pages;
				let truncated = false;
				while (
					pages.length > 0 &&
					JSON.stringify(pages).length > CHARACTER_LIMIT
				) {
					pages = pages.slice(
						0,
						Math.max(1, Math.floor(pages.length / 2)),
					);
					truncated = true;
					if (pages.length === 1) break;
				}

				const structured: Record<string, unknown> = {
					pages,
					total,
					returned: pages.length,
					truncated,
					...(truncated
						? {
								truncation_message: `Returned ${pages.length} of ${total} pages to stay within the response budget. Use vbm_search to retrieve specific content instead.`,
							}
						: {}),
				};

				if (!total) {
					return toolResult(
						'The browsing memory is empty.',
						structured,
					);
				}

				let text: string;
				if (input.response_format === ResponseFormat.JSON) {
					text = JSON.stringify(structured, null, 2);
				} else {
					const lines = [
						`# Export — ${pages.length} of ${total} pages`,
						...(truncated
							? [
									'',
									`_${structured.truncation_message as string}_`,
								]
							: []),
						'',
					];
					for (const p of pages) {
						lines.push(`## ${p.title || p.url}`);
						lines.push(`- **URL**: ${p.url}`);
						lines.push(
							`- **Visited**: ${tsToISO(p.visitTs)} · **Chunks**: ${p.chunks.length}`,
						);
						lines.push('');
					}
					text = lines.join('\n');
				}
				return toolResult(text, structured);
			} catch (error) {
				return errorResult(handleApiError(error));
			}
		},
	);
}
