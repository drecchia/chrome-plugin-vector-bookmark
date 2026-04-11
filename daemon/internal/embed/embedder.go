package embed

import (
	"encoding/binary"
	"math"
)

// Embedder produces a fixed-dimension float32 embedding for a text chunk.
type Embedder interface {
	Embed(text string) ([]float32, error)
	Dim() int
	Version() string
}

// StubEmbedder returns zero vectors. Replace with ONNX implementation.
type StubEmbedder struct{}

func NewStubEmbedder() *StubEmbedder { return &StubEmbedder{} }

func (s *StubEmbedder) Embed(text string) ([]float32, error) {
	// Returns a zero vector — dense search will degrade to BM25-only.
	// TODO: replace with onnxruntime-go + Snowflake/snowflake-arctic-embed-xs
	return make([]float32, s.Dim()), nil
}

func (s *StubEmbedder) Dim() int        { return 384 }
func (s *StubEmbedder) Version() string { return "stub-v0" }

// EncodeEmbedding serializes []float32 to bytes (little-endian).
func EncodeEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeEmbedding deserializes bytes to []float32.
func DecodeEmbedding(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// CosineSimilarity computes cosine similarity between two equal-length vectors.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
