/**
 * Shared response-formatting helpers so every tool returns a consistent shape:
 * a text block (markdown or JSON) plus structuredContent for programmatic use.
 */

import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js';
import { CHARACTER_LIMIT } from './constants.js';

export enum ResponseFormat {
	MARKDOWN = 'markdown',
	JSON = 'json',
}

/** Build a successful tool result carrying both text and structured data. */
export function toolResult(
	text: string,
	structured: Record<string, unknown>,
): CallToolResult {
	return {
		content: [{ type: 'text', text }],
		structuredContent: structured,
	};
}

/** Build an error tool result from a pre-formatted message. */
export function errorResult(message: string): CallToolResult {
	return { content: [{ type: 'text', text: message }], isError: true };
}

/** Render either markdown (already built) or pretty JSON of the structured payload. */
export function render(
	format: ResponseFormat,
	markdown: string,
	structured: Record<string, unknown>,
): string {
	return format === ResponseFormat.JSON
		? JSON.stringify(structured, null, 2)
		: markdown;
}

/** Convert Unix milliseconds to a human-readable ISO string (UTC). */
export function tsToISO(ms: number): string {
	return new Date(ms).toISOString();
}

/**
 * Guard against oversized payloads. If the rendered text exceeds the character
 * limit, return a truncated notice instead of flooding the agent's context.
 * Callers that can paginate should prefer narrowing via limit/filters.
 */
export function capText(text: string, hint: string): string {
	if (text.length <= CHARACTER_LIMIT) return text;
	return (
		text.slice(0, CHARACTER_LIMIT) +
		`\n\n...[truncated: response exceeded ${CHARACTER_LIMIT} characters. ${hint}]`
	);
}
