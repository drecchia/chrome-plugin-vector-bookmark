/**
 * vbm_list_tags + vbm_list_pages_by_tag — tag taxonomy and tag-scoped page lists.
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
	ListTagsInputShape,
	PagesByTagInputShape,
	type ListTagsInput,
	type PagesByTagInput,
} from '../schemas.js';
import type { PagesByTagResponse, TagsResponse } from '../types.js';

const LIST_TAGS_DESCRIPTION = `List every tag in the user's browsing memory with how many pages carry each, sorted by the daemon. Use this to discover the taxonomy before filtering a search or listing pages by tag.

Args:
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json): { "tags": [ { "tag": string, "count": number } ] }

Examples:
  - "What topics/tags do I have saved?"
  - "How many pages are tagged 'go'?"`;

const PAGES_BY_TAG_DESCRIPTION = `List pages carrying a specific tag, newest first.

Args:
  - tag (string, required): the tag to filter by
  - limit (number): max pages (default 100)
  - response_format ('markdown' | 'json'): default 'markdown'

Returns (json):
  {
    "pages": [ { "url": string, "title": string, "domain": string, "visitTs": number, "tags": string[] } ],
    "total": number
  }

Examples:
  - "Show my pages tagged 'rust'" -> tag="rust"
  - "List the 10 most recent pages tagged 'work'" -> tag="work", limit=10`;

export function registerTagTools(server: McpServer): void {
	server.registerTool(
		'vbm_list_tags',
		{
			title: 'List Tags',
			description: LIST_TAGS_DESCRIPTION,
			inputSchema: ListTagsInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: ListTagsInput) => {
			try {
				const data = await daemonGet<TagsResponse>('/tags');
				const structured = data as unknown as Record<string, unknown>;
				if (!data.tags.length) {
					return toolResult('No tags found.', structured);
				}
				const markdown = [
					`# Tags (${data.tags.length})`,
					'',
					...data.tags.map((t) => `- **${t.tag}** (${t.count})`),
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

	server.registerTool(
		'vbm_list_pages_by_tag',
		{
			title: 'List Pages by Tag',
			description: PAGES_BY_TAG_DESCRIPTION,
			inputSchema: PagesByTagInputShape,
			annotations: {
				readOnlyHint: true,
				destructiveHint: false,
				idempotentHint: true,
				openWorldHint: true,
			},
		},
		async (input: PagesByTagInput) => {
			try {
				const data = await daemonGet<PagesByTagResponse>('/pages', {
					tag: input.tag,
					limit: input.limit,
				});
				const structured = data as unknown as Record<string, unknown>;
				if (!data.pages.length) {
					return toolResult(
						`No pages found tagged '${input.tag}'.`,
						structured,
					);
				}
				const lines = [
					`# Pages tagged '${input.tag}' (${data.total})`,
					'',
				];
				for (const p of data.pages) {
					lines.push(`## ${p.title || p.url}`);
					lines.push(`- **URL**: ${p.url}`);
					lines.push(`- **Visited**: ${tsToISO(p.visitTs)}`);
					if (p.tags.length)
						lines.push(`- **Tags**: ${p.tags.join(', ')}`);
					lines.push('');
				}
				const text = render(
					input.response_format as ResponseFormat,
					lines.join('\n'),
					structured,
				);
				return toolResult(
					capText(text, "Lower 'limit' to reduce the result set."),
					structured,
				);
			} catch (error) {
				return errorResult(handleApiError(error));
			}
		},
	);
}
