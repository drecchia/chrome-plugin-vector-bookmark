package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultModel = "nomic-embed-text"
const defaultDim = 768

// HttpEmbedder calls an Ollama-compatible embeddings endpoint.
// Set VBM_EMBED_URL (e.g. http://localhost:11434/api/embeddings) and
// optionally VBM_EMBED_MODEL (default: nomic-embed-text).
type HttpEmbedder struct {
	url    string
	model  string
	dim    int
	client *http.Client
}

// NewHttpEmbedder creates an HttpEmbedder. model defaults to nomic-embed-text if empty.
func NewHttpEmbedder(url, model string) *HttpEmbedder {
	if model == "" {
		model = defaultModel
	}
	return &HttpEmbedder{
		url:   url,
		model: model,
		dim:   defaultDim,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (e *HttpEmbedder) Dim() int        { return e.dim }
func (e *HttpEmbedder) Version() string { return "http-v0" }

func (e *HttpEmbedder) Embed(text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{
		"model":  e.model,
		"prompt": text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	resp, err := e.client.Post(e.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("embed response contained empty embedding")
	}

	// Update dim dynamically from first response.
	if e.dim != len(result.Embedding) {
		e.dim = len(result.Embedding)
	}

	return result.Embedding, nil
}
