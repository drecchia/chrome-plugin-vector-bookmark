package chunk

import (
	"strings"
	"testing"
)

func TestSplitIntoChunks_TooShort(t *testing.T) {
	if got := SplitIntoChunks("one two three"); got != nil {
		t.Fatalf("expected nil for <MinTokens text, got %d chunks", len(got))
	}
}

func TestSplitIntoChunks_SingleWindow(t *testing.T) {
	text := strings.Repeat("word ", MinTokens+5)
	chunks := SplitIntoChunks(text)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Index != 0 {
		t.Fatalf("expected index 0, got %d", chunks[0].Index)
	}
	if chunks[0].Hash == "" {
		t.Fatalf("expected non-empty hash")
	}
}

func TestSplitIntoChunks_OverlapAndMin(t *testing.T) {
	// Build text of exactly WindowTokens + (WindowTokens - OverlapTokens) + MinTokens
	// so we get exactly 3 chunks: [0..W), [W-O..2W-O), and a final >= MinTokens.
	total := WindowTokens + (WindowTokens - OverlapTokens) + MinTokens + 10
	tokens := make([]string, total)
	for i := range tokens {
		tokens[i] = "t"
	}
	chunks := SplitIntoChunks(strings.Join(tokens, " "))
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks for long text, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Index != i {
			t.Fatalf("chunk %d has wrong index %d", i, c.Index)
		}
		if got := len(Tokenize(c.Text)); got < MinTokens {
			t.Fatalf("chunk %d has %d tokens, below MinTokens=%d", i, got, MinTokens)
		}
	}
}

func TestHash_Normalization(t *testing.T) {
	a := Hash("  Hello   WORLD  ")
	b := Hash("hello world")
	if a != b {
		t.Fatalf("hash should be normalization-invariant: %s vs %s", a, b)
	}
}

func TestTokenize_Whitespace(t *testing.T) {
	got := Tokenize("a\tb  c\nd")
	if len(got) != 4 {
		t.Fatalf("expected 4 tokens, got %v", got)
	}
}
