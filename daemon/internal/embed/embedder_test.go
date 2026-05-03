package embed

import (
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	v := []float32{1, 2, 3, 4}
	if got := CosineSimilarity(v, v); math.Abs(float64(got)-1.0) > 1e-6 {
		t.Fatalf("identical vectors should have cosine 1.0, got %v", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := CosineSimilarity(a, b); math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("orthogonal vectors should have cosine 0, got %v", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	if got := CosineSimilarity(a, b); math.Abs(float64(got)+1.0) > 1e-6 {
		t.Fatalf("opposite vectors should have cosine -1.0, got %v", got)
	}
}

func TestCosineSimilarity_Degenerate(t *testing.T) {
	if got := CosineSimilarity([]float32{0, 0}, []float32{1, 1}); got != 0 {
		t.Fatalf("zero vector should return 0, got %v", got)
	}
	if got := CosineSimilarity([]float32{1}, []float32{1, 1}); got != 0 {
		t.Fatalf("mismatched dims should return 0, got %v", got)
	}
}

func TestEncodeDecode_RoundTrip(t *testing.T) {
	v := []float32{0.1, -0.2, 3.14, 1e-9, -1e9}
	decoded := DecodeEmbedding(EncodeEmbedding(v))
	if len(decoded) != len(v) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded), len(v))
	}
	for i := range v {
		if v[i] != decoded[i] {
			t.Fatalf("roundtrip mismatch at %d: %v vs %v", i, v[i], decoded[i])
		}
	}
}

func TestDecodeEmbedding_InvalidLength(t *testing.T) {
	if got := DecodeEmbedding([]byte{1, 2, 3}); got != nil {
		t.Fatalf("expected nil for non-4-aligned input, got %v", got)
	}
}

func TestStubEmbedder(t *testing.T) {
	e := NewStubEmbedder()
	if e.Dim() != 384 {
		t.Fatalf("stub dim should be 384, got %d", e.Dim())
	}
	if e.Version() != "stub-v0" {
		t.Fatalf("stub version should be stub-v0, got %s", e.Version())
	}
	v, err := e.Embed("anything")
	if err != nil {
		t.Fatalf("stub embed should not error, got %v", err)
	}
	if len(v) != 384 {
		t.Fatalf("stub vec len should be 384, got %d", len(v))
	}
	for _, f := range v {
		if f != 0 {
			t.Fatalf("stub vec should be all zeros, got %v", f)
		}
	}
}
