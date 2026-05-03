package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── deriveChatURL ────────────────────────────────────────────────────────────

func TestDeriveChatURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://api.openai.com/v1/embeddings", "https://api.openai.com/v1/chat/completions"},
		{"https://openrouter.ai/api/v1/embeddings", "https://openrouter.ai/api/v1/chat/completions"},
		{"http://127.0.0.1:11434/v1/embeddings", "http://127.0.0.1:11434/v1/chat/completions"},
		// No /embeddings suffix → fallback appends /chat/completions to base.
		{"https://example.test/api/", "https://example.test/api/chat/completions"},
		{"https://example.test/api", "https://example.test/api/chat/completions"},
	}
	for _, c := range cases {
		if got := deriveChatURL(c.in); got != c.want {
			t.Errorf("deriveChatURL(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

// ── parseTagsJSON ────────────────────────────────────────────────────────────

func TestParseTagsJSON_PlainAndFenced(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", `{"tags":["go","sqlite"]}`, []string{"go", "sqlite"}},
		{
			"fenced-json",
			"```json\n{\"tags\":[\"a\",\"b\"]}\n```",
			[]string{"a", "b"},
		},
		{
			"fenced-bare",
			"```\n{\"tags\":[\"x\"]}\n```",
			[]string{"x"},
		},
		{"empty-array", `{"tags":[]}`, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseTagsJSON(c.in)
			if err != nil {
				t.Fatalf("parseTagsJSON: %v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(c.want), got)
			}
			for i, v := range c.want {
				if got[i] != v {
					t.Errorf("[%d] = %q; want %q", i, got[i], v)
				}
			}
		})
	}
}

func TestParseTagsJSON_RejectsInvalid(t *testing.T) {
	_, err := parseTagsJSON("not json")
	if err == nil {
		t.Error("expected error for non-JSON input")
	}
}

// ── Summarize via httptest mock ──────────────────────────────────────────────

// fakeChatServer returns a *httptest.Server that mimics the OpenAI
// chat/completions API. The handler is invoked for each request so tests can
// assert on the body or simulate failures by closing and re-creating.
func fakeChatServer(t *testing.T, handler func(req map[string]any) (status int, content string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		status, content := handler(parsed)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		})
	}))
}

func newTestClient(t *testing.T, embedURL string) *Client {
	t.Helper()
	c, err := New(embedURL, "test-model", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestSummarize_HappyPath(t *testing.T) {
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		if model := req["model"]; model != "test-model" {
			t.Errorf("model in body = %v, want test-model", model)
		}
		return http.StatusOK, "  summary text  "
	})
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/embeddings")
	out, err := c.Summarize(context.Background(), "the page text")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out != "summary text" {
		t.Errorf("want trimmed 'summary text', got %q", out)
	}
}

func TestSummarize_RejectsEmptyContent(t *testing.T) {
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		return http.StatusOK, "" // model returns blank
	})
	defer srv.Close()
	_, err := newTestClient(t, srv.URL+"/embeddings").
		Summarize(context.Background(), "in")
	if err == nil {
		t.Error("expected error on empty summary")
	}
}

func TestSummarize_5xxIsRetried(t *testing.T) {
	calls := 0
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		calls++
		if calls == 1 {
			return http.StatusBadGateway, ""
		}
		return http.StatusOK, "ok after retry"
	})
	defer srv.Close()
	out, err := newTestClient(t, srv.URL+"/embeddings").
		Summarize(context.Background(), "in")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 retry), got %d", calls)
	}
	if out != "ok after retry" {
		t.Errorf("got %q", out)
	}
}

func TestSummarize_4xxNotRetried(t *testing.T) {
	calls := 0
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		calls++
		return http.StatusBadRequest, ""
	})
	defer srv.Close()
	_, err := newTestClient(t, srv.URL+"/embeddings").
		Summarize(context.Background(), "in")
	if err == nil {
		t.Error("expected error on 400")
	}
	if calls != 1 {
		t.Errorf("4xx must not retry; got %d calls", calls)
	}
}

func TestSummarize_NilClientFailsCleanly(t *testing.T) {
	var c *Client
	if _, err := c.Summarize(context.Background(), "x"); err == nil {
		t.Error("expected error for nil client")
	}
}

// ── SuggestTags via httptest mock ────────────────────────────────────────────

func TestSuggestTags_ParsesAndCapsAtMax(t *testing.T) {
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		// Confirm we forward the existing tags so the model can reuse them.
		messages, _ := req["messages"].([]any)
		userMsg, _ := messages[1].(map[string]any)
		content, _ := userMsg["content"].(string)
		if !strings.Contains(content, "rust") {
			t.Errorf("expected existing tag 'rust' in user msg, got %q", content)
		}
		// LLM returns 5 tags; client must cap at suggestTagsMax (3).
		return http.StatusOK, `{"tags":["a","b","c","d","e"]}`
	})
	defer srv.Close()

	got, err := newTestClient(t, srv.URL+"/embeddings").
		SuggestTags(context.Background(), "title", "body", []string{"rust", "go"})
	if err != nil {
		t.Fatalf("SuggestTags: %v", err)
	}
	if len(got) != suggestTagsMax {
		t.Errorf("expected cap at %d, got %d (%v)", suggestTagsMax, len(got), got)
	}
}

func TestSuggestTags_HandlesMarkdownFences(t *testing.T) {
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		return http.StatusOK, "```json\n{\"tags\":[\"web\",\"docs\"]}\n```"
	})
	defer srv.Close()
	got, err := newTestClient(t, srv.URL+"/embeddings").
		SuggestTags(context.Background(), "t", "b", nil)
	if err != nil {
		t.Fatalf("SuggestTags: %v", err)
	}
	if len(got) != 2 || got[0] != "web" || got[1] != "docs" {
		t.Errorf("got %v", got)
	}
}

func TestSuggestTags_InvalidJSONReturnsError(t *testing.T) {
	srv := fakeChatServer(t, func(req map[string]any) (int, string) {
		return http.StatusOK, "not even close to JSON"
	})
	defer srv.Close()
	if _, err := newTestClient(t, srv.URL+"/embeddings").
		SuggestTags(context.Background(), "t", "b", nil); err == nil {
		t.Error("expected JSON parse error")
	}
}

// ── Prompt loading (env file vs default fallback) ────────────────────────────

func TestLoadPromptOrDefault_FallbackWhenUnset(t *testing.T) {
	os.Unsetenv("VBM_LLM_PROMPT_TEST")
	if got := loadPromptOrDefault("VBM_LLM_PROMPT_TEST", "FALLBACK"); got != "FALLBACK" {
		t.Errorf("got %q, want FALLBACK", got)
	}
}

func TestLoadPromptOrDefault_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(path, []byte("  custom prompt  "), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("VBM_LLM_PROMPT_TEST", path)
	if got := loadPromptOrDefault("VBM_LLM_PROMPT_TEST", "FALLBACK"); got != "custom prompt" {
		t.Errorf("got %q, want trimmed custom prompt", got)
	}
}

func TestLoadPromptOrDefault_EmptyFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(path, []byte("   \n  "), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("VBM_LLM_PROMPT_TEST", path)
	if got := loadPromptOrDefault("VBM_LLM_PROMPT_TEST", "FALLBACK"); got != "FALLBACK" {
		t.Errorf("expected fallback when file is whitespace-only, got %q", got)
	}
}

func TestLoadPromptOrDefault_MissingFileFallsBack(t *testing.T) {
	t.Setenv("VBM_LLM_PROMPT_TEST", "/no/such/path-here-please-12345.md")
	if got := loadPromptOrDefault("VBM_LLM_PROMPT_TEST", "FALLBACK"); got != "FALLBACK" {
		t.Errorf("expected fallback when file missing, got %q", got)
	}
}

func TestLoadPromptOrDefault_OversizedFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	big := strings.Repeat("x", maxPromptFileBytes+1)
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("VBM_LLM_PROMPT_TEST", path)
	if got := loadPromptOrDefault("VBM_LLM_PROMPT_TEST", "FALLBACK"); got != "FALLBACK" {
		t.Errorf("expected fallback for oversize file, got %q", got)
	}
}

// ── New() validation ─────────────────────────────────────────────────────────

func TestNew_RejectsEmptyEmbedURL(t *testing.T) {
	if _, err := New("", "m", "k"); err == nil {
		t.Error("expected error on empty embed URL")
	}
}

func TestNew_DefaultsModelWhenEmpty(t *testing.T) {
	c, err := New("https://x.test/v1/embeddings", "", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.model != "gpt-4o-mini" {
		t.Errorf("default model = %q", c.model)
	}
}
