package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenAIEmbedder calls an OpenAI-compatible embeddings endpoint.
// Works with OpenRouter, OpenAI, and Ollama (/v1/embeddings).
// Env vars: VBM_EMBED_URL, VBM_EMBED_MODEL, VBM_EMBED_API_KEY.
type OpenAIEmbedder struct {
	url    string
	model  string
	apiKey string
	dim    int
	client *http.Client
}

// NewOpenAIEmbedder creates an OpenAIEmbedder. model defaults to openai/text-embedding-3-small.
func NewOpenAIEmbedder(url, model, apiKey string) *OpenAIEmbedder {
	if model == "" {
		model = "openai/text-embedding-3-small"
	}
	return &OpenAIEmbedder{
		url:    url,
		model:  model,
		apiKey: apiKey,
		dim:    1536,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *OpenAIEmbedder) Dim() int        { return e.dim }
func (e *OpenAIEmbedder) Version() string { return "openai-v0" }

func (e *OpenAIEmbedder) Embed(text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{
		"model": e.model,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed response contained empty embedding")
	}

	vec := result.Data[0].Embedding
	if e.dim != len(vec) {
		e.dim = len(vec)
	}
	return vec, nil
}
