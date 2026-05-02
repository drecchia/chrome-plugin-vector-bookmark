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
	"strings"
	"time"
)

// maxInputChars caps the page text sent to the LLM. ~32k chars is roughly 8k
// tokens — well under the context window of every modern small/cheap model.
const maxInputChars = 32_000

// suggestTagsMaxChars is a tighter cap for the tag-suggestion path: tags care
// about the topic, not every detail. 8k chars is enough to capture the gist.
const suggestTagsMaxChars = 8_000

// suggestTagsMax is the hard upper bound on tags returned by SuggestTags.
const suggestTagsMax = 3

// maxPromptFileBytes caps the size of an external prompt markdown file.
// Anything bigger almost certainly isn't a prompt.
const maxPromptFileBytes = 16 * 1024

// defaultSummarizePrompt is used when VBM_LLM_PROMPT_SUMMARIZE_FILE is unset
// or unreadable. CR-0006.
const defaultSummarizePrompt = "Summarize the following web page content for retrieval and search purposes. " +
	"Target 200-400 words. Preserve technical terms, names, dates, key claims, and any code or commands mentioned. " +
	"Output plain prose with no headers, no bullet lists, and no markdown."

// defaultSuggestTagsPrompt is the system message for the SuggestTags call.
// The user message is dynamically built with the existing taxonomy + title +
// content; only this fixed system prompt is externalisable.
const defaultSuggestTagsPrompt = "You generate tags for retrieval and curation of saved web pages. " +
	"Output between 1 and 3 short tags (1-2 words each, lowercase, kebab-case, " +
	"no leading '#'). " +
	"Reuse from the existing tag list when applicable; otherwise create new tags " +
	"that match the same style. " +
	"Output STRICT JSON only, exactly in this shape: {\"tags\":[\"tag-one\",\"tag-two\"]}. " +
	"No prose, no markdown fences, no explanation."

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
	return &Client{
		url:    chatURL,
		model:  model,
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
		summarizePrompt: loadPromptOrDefault(
			"VBM_LLM_PROMPT_SUMMARIZE_FILE", defaultSummarizePrompt,
		),
		suggestTagsPrompt: loadPromptOrDefault(
			"VBM_LLM_PROMPT_SUGGEST_TAGS_FILE", defaultSuggestTagsPrompt,
		),
	}, nil
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
		if len(tags) > suggestTagsMax {
			tags = tags[:suggestTagsMax]
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
