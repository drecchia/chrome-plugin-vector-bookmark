// Package llm provides a minimal OpenAI-compatible chat-completions client used
// to summarize page content before indexing. Reuses the same provider/credentials
// as the embedder via VBM_EMBED_URL + VBM_EMBED_API_KEY; the chat URL is derived
// by replacing the trailing "/embeddings" with "/chat/completions".
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// maxInputChars caps the page text sent to the LLM. ~32k chars is roughly 8k
// tokens — well under the context window of every modern small/cheap model.
const maxInputChars = 32_000

// suggestTagsMaxChars is a tighter cap for the tag-suggestion path: tags care
// about the topic, not every detail. 8k chars is enough to capture the gist.
const suggestTagsMaxChars = 8_000

// suggestTagsMaxDefault is the fallback upper bound on tags returned by
// SuggestTags when VBM_LLM_SUGGEST_TAGS_MAX is unset/invalid. Allowed range
// is 1–10; values outside are clamped at startup.
const suggestTagsMaxDefault = 3
const suggestTagsMaxFloor = 1
const suggestTagsMaxCeil = 25

// maxPromptFileBytes caps the size of an external prompt markdown file.
// Anything bigger almost certainly isn't a prompt.
const maxPromptFileBytes = 16 * 1024

// defaultSummarizePrompt is used when VBM_LLM_PROMPT_SUMMARIZE_FILE is unset
// or unreadable. CR-0006.
const defaultSummarizePrompt = `You are summarizing a single web page so it can be retrieved later by semantic and keyword search.

STEP 1 — Identify the subject. A page is primarily ABOUT one canonical thing: a software project, a library, a product, a video, an article, a paper, a tool, a person. Identify it before writing anything. The HTML title and the top of the body usually carry this. Lead the summary with one or two sentences that state what the page IS and what it exists to present (e.g., "X is a free open-source SVG icon set with N icons" / "Y is a YouTube video by Z explaining ..." / "owner/repo is a TypeScript library for ...").

STEP 2 — Describe around the subject. Cover, when present: purpose / problem solved, distinctive features, technologies and stack, key facts (numbers, names, versions, dates, authors), and intended audience. Preserve technical terms, proper nouns, and any code/commands verbatim.

IGNORE noise that is NOT about the subject: user comments, reactions, related/recommended links, navigation, footers, cookie banners, ads, "you might also like" blocks, and platform chrome. Do not let audience-generated text shape the summary.

The platform hosting the page (GitHub, YouTube, npm, Medium, etc.) is the HOST, not the subject. Do not describe the platform.

Focus on what the subject IS and DOES. Mention build, packaging, deployment, OS support, CI, or distribution channels only when they are central to the subject; do not let how-to-install-and-build sections dominate the summary of a library whose purpose is something else.

Do not invent facts that are not in the text. Respond in the page's primary language.

Target 200-400 words; shorter is fine for genuinely thin pages. Output plain prose only — no headers, no bullet lists, no markdown.`

// defaultSuggestTagsPromptTemplate is the system message for the SuggestTags
// call. %d is replaced with the configured upper bound at client construction.
// The user message is dynamically built with the existing taxonomy + title +
// content; only this fixed system prompt is externalisable.
const defaultSuggestTagsPromptTemplate = `You generate retrieval tags for a single web page. Output up to %d tags. Use as many as the page genuinely warrants — do NOT pad to reach the limit; returning 3 well-chosen tags is better than 10 weak ones.

Before emitting tags, mentally fill three layers in this order:

1. IDENTITY — the canonical name of the subject the page is about: a repo name, library name, product name, video creator or distinct video topic, paper title, author. The HTML title almost always carries this. If a canonical name exists, it MUST appear as a tag.

2. CATEGORY — what KIND of thing the page is: icon-library, web-framework, cli-tool, tutorial, documentation, paper, video-essay, landing-page, blog-post, course, dataset, etc.

3. TECHNOLOGY / TOPIC — only technologies, languages, frameworks, methods, or domains that are INTRINSIC to the subject: what it is written in, what it primarily targets, what domain it operates in, what topic it teaches.

   Apply this test before tagging any technology: "If this technology were removed, would this still be the same project / video / article?" If yes, it is INCIDENTAL — skip it.

   INTRINSIC (tag these): the language a library is written in (node.js, typescript, rust); the format it produces (svg, json, pdf); the framework it extends (react, django); the domain it teaches or implements (machine-learning, cryptography, ray-tracing).

   INCIDENTAL — DO NOT tag these even when mentioned prominently:
   - build / compile / packaging tools used only to ship the subject: docker, make, cmake, gcc, webpack, vite, rollup, esbuild
   - CI / release / hosting infrastructure: github-actions, ci, cd, vercel, netlify, heroku
   - operating systems or environments the subject happens to run on: linux, windows, macos, wsl, android, ios — UNLESS the subject is OS-specific (e.g., a Windows-only tool)
   - shells, terminals, editors used in demos: bash, zsh, powershell, vscode, vim
   - package managers used purely for distribution: npm, pip, cargo, brew, apt — UNLESS the package manager itself is the subject
   - cross-compilation targets mentioned as "also builds for X"
   - languages mentioned only because the docs include a code block in them

DO NOT tag, in general:
- the hosting platform (github, youtube, npm-website, medium, x, reddit) — that is the host, not the subject
- terms that come from user comments, reactions, recommended/related items, ads, navigation, or "you might also like" blocks
- empty generic words: article, blog, website, page, video, content, post, project, library, software

REUSE a tag from the existing list when it already fits the concept; only create a new tag when no existing tag covers it, and match the style of the existing taxonomy.

Format: lowercase kebab-case, 1-3 words per tag joined by hyphens (e.g., react, machine-learning, icon-library). No leading '#'. No spaces inside a tag.

Language: if the existing tag list is non-empty, match its primary language. Otherwise, use the page's primary language.

Output STRICT JSON only, exactly in this shape: {"tags":["tag-one","tag-two"]}. No prose, no markdown fences, no explanation.`

// defaultTagMergePrompt is the system message for SuggestTagMerges. Used when
// VBM_LLM_PROMPT_TAG_MERGE_FILE is unset or unreadable. CR-0007.
const defaultTagMergePrompt = `You are cleaning up a tag taxonomy. You receive a list of existing tags, each with the number of pages it is attached to, in the form "tag (count)".

Your job: find groups of tags that are NEAR-DUPLICATES of one another — different spellings of the SAME concept — and propose merging each group into a single canonical tag.

Treat as near-duplicates ONLY:
- spelling / spacing / punctuation variants: "machine learning" vs "machine-learning" vs "machinelearning"
- singular / plural: "icon" vs "icons"
- common abbreviations and their expansion: "ml" vs "machine-learning", "k8s" vs "kubernetes", "js" vs "javascript"
- obvious synonyms for the exact same thing: "frontend" vs "front-end"

DO NOT merge tags that are merely related or hierarchical but distinct concepts:
- "react" and "javascript" (one is a framework, the other a language) — KEEP SEPARATE
- "machine-learning" and "deep-learning" — KEEP SEPARATE
- "python" and "django" — KEEP SEPARATE
- a broad topic and a specific instance of it — KEEP SEPARATE

When in doubt, DO NOT merge. A wrong merge destroys information; a missed merge is harmless.

For each group, pick the canonical tag: prefer the variant that is the clearest, most standard spelling (kebab-case, expanded over abbreviated when the expansion is unambiguous), and — as a tiebreaker — the one with the higher page count. The canonical MUST be one of the tags in the group.

Every variant you list MUST appear verbatim in the provided tag list. Do not invent tags.

Output STRICT JSON only, exactly in this shape:
{"groups":[{"canonical":"machine-learning","variants":["ml","machine learning"]}]}

The "variants" array lists the OTHER tags in the group (excluding the canonical). Omit groups with no variants. If there are no near-duplicates at all, output {"groups":[]}. No prose, no markdown fences, no explanation.`

// loadPromptOrDefault returns the contents of the file pointed to by envVar
// (when set, readable, non-empty, and ≤ maxPromptFileBytes). Otherwise logs a
// slog.Warn and returns the embedded fallback. CR-0006.
func loadPromptOrDefault(envVar, fallback string) string {
	path := os.Getenv(envVar)
	if path == "" {
		return fallback
	}
	info, err := os.Stat(path)
	if err != nil {
		slog.Warn("prompt file unreadable, using default", "env", envVar, "path", path, "err", err)
		return fallback
	}
	if info.Size() > maxPromptFileBytes {
		slog.Warn("prompt file exceeds size cap, using default", "env", envVar, "path", path, "size", info.Size(), "max", maxPromptFileBytes)
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("prompt file read failed, using default", "env", envVar, "path", path, "err", err)
		return fallback
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		slog.Warn("prompt file empty, using default", "env", envVar, "path", path)
		return fallback
	}
	slog.Info("loaded external prompt", "env", envVar, "path", path, "bytes", len(s))
	return s
}

// Client is an OpenAI-compatible chat completions client.
type Client struct {
	url               string
	model             string
	apiKey            string
	http              *http.Client
	summarizePrompt   string
	suggestTagsPrompt string
	tagMergePrompt    string
	suggestTagsMax    int
}

// New builds a client. embedURL is the embeddings URL (e.g.
// https://openrouter.ai/api/v1/embeddings); the chat URL is derived by
// swapping "/embeddings" → "/chat/completions". apiKey may be empty (for
// providers like Ollama). model defaults to "gpt-4o-mini".
func New(embedURL, model, apiKey string) (*Client, error) {
	if embedURL == "" {
		return nil, errors.New("empty embed URL")
	}
	chatURL := deriveChatURL(embedURL)
	if model == "" {
		model = "gpt-4o-mini"
	}
	maxTags := resolveSuggestTagsMax()
	defaultPrompt := fmt.Sprintf(defaultSuggestTagsPromptTemplate, maxTags)
	return &Client{
		url:    chatURL,
		model:  model,
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
		summarizePrompt: loadPromptOrDefault(
			"VBM_LLM_PROMPT_SUMMARIZE_FILE", defaultSummarizePrompt,
		),
		suggestTagsPrompt: loadPromptOrDefault(
			"VBM_LLM_PROMPT_SUGGEST_TAGS_FILE", defaultPrompt,
		),
		tagMergePrompt: loadPromptOrDefault(
			"VBM_LLM_PROMPT_TAG_MERGE_FILE", defaultTagMergePrompt,
		),
		suggestTagsMax: maxTags,
	}, nil
}

// SuggestTagsMax returns the configured upper bound on tags from SuggestTags.
// Exposed so HTTP handlers can size buffers / apply the same cap consistently.
func (c *Client) SuggestTagsMax() int {
	if c == nil || c.suggestTagsMax <= 0 {
		return suggestTagsMaxDefault
	}
	return c.suggestTagsMax
}

// resolveSuggestTagsMax reads VBM_LLM_SUGGEST_TAGS_MAX, parses it as an int,
// clamps to [suggestTagsMaxFloor, suggestTagsMaxCeil], and falls back to the
// default on parse error or when unset.
func resolveSuggestTagsMax() int {
	raw := strings.TrimSpace(os.Getenv("VBM_LLM_SUGGEST_TAGS_MAX"))
	if raw == "" {
		return suggestTagsMaxDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("VBM_LLM_SUGGEST_TAGS_MAX not a number, using default",
			"value", raw, "default", suggestTagsMaxDefault)
		return suggestTagsMaxDefault
	}
	if n < suggestTagsMaxFloor {
		slog.Warn("VBM_LLM_SUGGEST_TAGS_MAX below floor, clamping",
			"value", n, "floor", suggestTagsMaxFloor)
		return suggestTagsMaxFloor
	}
	if n > suggestTagsMaxCeil {
		slog.Warn("VBM_LLM_SUGGEST_TAGS_MAX above ceil, clamping",
			"value", n, "ceil", suggestTagsMaxCeil)
		return suggestTagsMaxCeil
	}
	return n
}

func deriveChatURL(embedURL string) string {
	if i := strings.LastIndex(embedURL, "/embeddings"); i >= 0 {
		return embedURL[:i] + "/chat/completions"
	}
	// Fallback: append /chat/completions to the base.
	return strings.TrimRight(embedURL, "/") + "/chat/completions"
}

// Summarize returns a retrieval-friendly prose summary of text. Retries once
// on 5xx or transport errors.
func (c *Client) Summarize(ctx context.Context, text string) (string, error) {
	if c == nil {
		return "", errors.New("llm client not configured")
	}
	if len(text) > maxInputChars {
		text = text[:maxInputChars]
	}

	body, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": c.summarizePrompt},
			{"role": "user", "content": text},
		},
		"temperature": 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("transport: %w", err)
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("llm endpoint returned %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}
		if len(result.Choices) == 0 {
			return "", errors.New("llm response has no choices")
		}
		out := strings.TrimSpace(result.Choices[0].Message.Content)
		if out == "" {
			return "", errors.New("llm returned empty summary")
		}
		return out, nil
	}
	return "", fmt.Errorf("llm call failed after retry: %w", lastErr)
}

// SuggestTags asks the LLM for up to 3 tags describing the page. It receives
// the page title + meta-aware context + body excerpt, plus the list of tags
// already in use so the model can reuse the existing taxonomy when relevant.
// Returns at most suggestTagsMax tags, already trimmed and lowercased.
func (c *Client) SuggestTags(
	ctx context.Context,
	title, text string,
	existing []string,
) ([]string, error) {
	if c == nil {
		return nil, errors.New("llm client not configured")
	}
	if len(text) > suggestTagsMaxChars {
		text = text[:suggestTagsMaxChars]
	}

	existingLine := "(none yet)"
	if len(existing) > 0 {
		existingLine = strings.Join(existing, ", ")
	}

	system := c.suggestTagsPrompt

	userMsg := fmt.Sprintf(
		"Existing tags: %s\n\nTitle: %s\n\nContent:\n%s",
		existingLine, title, text,
	)

	body, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": userMsg},
		},
		"temperature": 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("transport: %w", err)
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("llm endpoint returned %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(result.Choices) == 0 {
			return nil, errors.New("llm response has no choices")
		}
		raw := strings.TrimSpace(result.Choices[0].Message.Content)
		tags, err := parseTagsJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("parse tags: %w", err)
		}
		if c.suggestTagsMax > 0 && len(tags) > c.suggestTagsMax {
			tags = tags[:c.suggestTagsMax]
		}
		return tags, nil
	}
	return nil, fmt.Errorf("llm call failed after retry: %w", lastErr)
}

// parseTagsJSON tolerates markdown fences (```json ... ```) around the JSON
// payload and extracts the tags array.
func parseTagsJSON(raw string) ([]string, error) {
	// Strip ```json ... ``` or ``` ... ``` fences if present.
	if strings.HasPrefix(raw, "```") {
		// drop first fence line
		if i := strings.Index(raw, "\n"); i >= 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j >= 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}

	var out struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return out.Tags, nil
}

// TagStat is a tag plus its page count, fed to SuggestTagMerges. Defined here to
// avoid importing the store package (keeps llm dependency-free).
type TagStat struct {
	Tag   string
	Count int
}

// MergeGroup is one proposed near-duplicate cluster: Canonical is the suggested
// winner, Variants are the other tags that should be merged into it.
type MergeGroup struct {
	Canonical string   `json:"canonical"`
	Variants  []string `json:"variants"`
}

// SuggestTagMerges asks the LLM to cluster near-duplicate tags and propose a
// canonical name per cluster. Retries once on 5xx. The model only sees tag names
// and counts — never page content. CR-0007.
func (c *Client) SuggestTagMerges(ctx context.Context, tags []TagStat) ([]MergeGroup, error) {
	if c == nil {
		return nil, errors.New("llm client not configured")
	}
	if len(tags) == 0 {
		return []MergeGroup{}, nil
	}

	lines := make([]string, 0, len(tags))
	for _, t := range tags {
		lines = append(lines, fmt.Sprintf("%s (%d)", t.Tag, t.Count))
	}
	userMsg := "Tags:\n" + strings.Join(lines, "\n")

	body, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": c.tagMergePrompt},
			{"role": "user", "content": userMsg},
		},
		"temperature": 0.1,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("transport: %w", err)
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("llm endpoint returned %d", resp.StatusCode)
		}
		defer resp.Body.Close()

		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(result.Choices) == 0 {
			return nil, errors.New("llm response has no choices")
		}
		raw := strings.TrimSpace(result.Choices[0].Message.Content)
		groups, err := parseMergeGroupsJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("parse merge groups: %w", err)
		}
		return groups, nil
	}
	return nil, fmt.Errorf("llm call failed after retry: %w", lastErr)
}

// parseMergeGroupsJSON tolerates markdown fences around the JSON payload and
// extracts the groups array, dropping groups with no variants.
func parseMergeGroupsJSON(raw string) ([]MergeGroup, error) {
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i >= 0 {
			raw = raw[i+1:]
		}
		if j := strings.LastIndex(raw, "```"); j >= 0 {
			raw = raw[:j]
		}
		raw = strings.TrimSpace(raw)
	}
	var out struct {
		Groups []MergeGroup `json:"groups"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	groups := make([]MergeGroup, 0, len(out.Groups))
	for _, g := range out.Groups {
		if g.Canonical == "" || len(g.Variants) == 0 {
			continue
		}
		groups = append(groups, g)
	}
	return groups, nil
}
