/**
 * Zod input shapes for every tool. The MCP SDK's registerTool expects a
 * ZodRawShape (a plain map of field -> ZodType), so each schema below is the
 * shape object itself. Constraints mirror the daemon's own clamps (search limit
 * 1-1000, history 1-500, topics 1-50, min_confidence 0-1) so the agent gets
 * validation feedback before a request is even sent.
 *
 * Cross-field rules (e.g. `to` > `from`) cannot be expressed on a raw shape and
 * are enforced inside the relevant tool handlers.
 */

import { z } from 'zod';
import { ResponseFormat } from './format.js';

const responseFormat = z
	.nativeEnum(ResponseFormat)
	.default(ResponseFormat.MARKDOWN)
	.describe(
		"Output format: 'markdown' (human-readable) or 'json' (structured)",
	);

export const SearchInputShape = {
	q: z
		.string()
		.min(1, 'Query must not be empty')
		.max(500, 'Query must not exceed 500 characters')
		.describe('Free-text query. Matched via hybrid BM25 + vector search.'),
	limit: z
		.number()
		.int()
		.min(1)
		.max(1000)
		.default(20)
		.describe('Maximum results to return (1-1000, default 20)'),
	tags: z
		.array(z.string())
		.optional()
		.describe(
			'Only return pages carrying ALL of these tags (include filter).',
		),
	neg_tags: z
		.array(z.string())
		.optional()
		.describe('Exclude pages carrying ANY of these tags (exclude filter).'),
	min_confidence: z
		.number()
		.min(0)
		.max(1)
		.optional()
		.describe(
			'Relative score floor 0-1. Higher = stricter. Omit for no floor.',
		),
	source: z
		.enum(['indexed', 'history'])
		.optional()
		.describe(
			"Restrict to 'indexed' (manually ingested) or 'history' (passive dwell capture). Omit for both.",
		),
	response_format: responseFormat,
};
export type SearchInput = z.infer<z.ZodObject<typeof SearchInputShape>>;

export const StatusInputShape = { response_format: responseFormat };
export type StatusInput = z.infer<z.ZodObject<typeof StatusInputShape>>;

export const ListTagsInputShape = { response_format: responseFormat };
export type ListTagsInput = z.infer<z.ZodObject<typeof ListTagsInputShape>>;

export const PagesByTagInputShape = {
	tag: z.string().min(1, 'tag is required').describe('The tag to filter by.'),
	limit: z
		.number()
		.int()
		.min(1)
		.default(100)
		.describe('Maximum pages to return (default 100)'),
	response_format: responseFormat,
};
export type PagesByTagInput = z.infer<z.ZodObject<typeof PagesByTagInputShape>>;

const from = z
	.number()
	.int()
	.describe('Start of the window, Unix milliseconds (inclusive).');
const to = z
	.number()
	.int()
	.describe('End of the window, Unix milliseconds (exclusive).');

export const HistoryInputShape = {
	from,
	to,
	limit: z
		.number()
		.int()
		.min(1)
		.max(500)
		.default(100)
		.describe('Maximum pages to return (1-500, default 100)'),
	response_format: responseFormat,
};
export type HistoryInput = z.infer<z.ZodObject<typeof HistoryInputShape>>;

export const TopicsInputShape = {
	from,
	to,
	limit: z
		.number()
		.int()
		.min(1)
		.max(50)
		.default(20)
		.describe('Maximum keywords to return (1-50, default 20)'),
	response_format: responseFormat,
};
export type TopicsInput = z.infer<z.ZodObject<typeof TopicsInputShape>>;

export const GetPageInputShape = {
	url: z
		.string()
		.url('Must be a valid URL')
		.describe('The exact page URL to look up.'),
	response_format: responseFormat,
};
export type GetPageInput = z.infer<z.ZodObject<typeof GetPageInputShape>>;

export const ExportInputShape = { response_format: responseFormat };
export type ExportInput = z.infer<z.ZodObject<typeof ExportInputShape>>;
