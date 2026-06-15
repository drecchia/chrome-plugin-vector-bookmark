/**
 * vbm_get_page — look up whether a specific URL is in the memory and its tags.
 */

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { daemonGet, handleApiError } from '../client.js';
import { ResponseFormat, errorResult, render, toolResult } from '../format.js';
import { GetPageInputShape, type GetPageInput } from '../schemas.js';
import type { PageStatusResponse } from '../types.js';

const DESCRIPTION = `Check whether a specific URL exists in the user's browsing memory, whether its full text is indexed, and which tags it carries.

Args:
  - url (string, required): the exact page URL
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json): { "exists": boolean, "indexed": boolean, "tags": string[] }

Examples:
  - "Have I saved https://example.com/article ?" -> url="https://example.com/article"
  - "What tags are on this page?" -> inspect "tags"`;

export function registerPageTool(server: McpServer): void {
	server.registerTool(
		'vbm_get_page',
		{
			title: 'Get Page Status',
			description: DESCRIPTION,
			inputSchema: GetPageInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: GetPageInput) => {
			try {
				const data = await daemonGet<PageStatusResponse>('/page', {
					url: input.url,
				});
				const structured = data as unknown as Record<string, unknown>;
				const markdown = data.exists
					? [
							`# ${input.url}`,
							'',
							`- **In memory**: yes`,
							`- **Indexed**: ${data.indexed ? 'yes' : 'no (history only)'}`,
							`- **Tags**: ${data.tags.length ? data.tags.join(', ') : '(none)'}`,
						].join('\n')
					: `${input.url} is not in the browsing memory.`;
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
