package chunk

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"unicode"
)

// Chunk is a text chunk with its normalized hash.
type Chunk struct {
	Index int
	Text  string
	Hash  string
}

const (
	WindowTokens  = 512
	OverlapTokens = 64
	MinTokens     = 40
)

// Tokenize splits text into tokens (words + punctuation groups).
// Simple whitespace split for bootstrap; replace with proper tokenizer later.
func Tokenize(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	return fields
}

// Normalize cleans text for hashing (lowercase, collapse whitespace).
func Normalize(text string) string {
	text = strings.ToLower(text)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// Hash returns sha1 hex of normalized text.
func Hash(text string) string {
	normalized := Normalize(text)
	return fmt.Sprintf("%x", sha1.Sum([]byte(normalized)))
}

// SplitIntoChunks splits text into overlapping windows.
// Returns nil if text is too short (< MinTokens).
func SplitIntoChunks(text string) []Chunk {
	tokens := Tokenize(text)
	if len(tokens) < MinTokens {
		return nil
	}

	var chunks []Chunk
	idx := 0
	for start := 0; start < len(tokens); start += WindowTokens - OverlapTokens {
		end := start + WindowTokens
		if end > len(tokens) {
			end = len(tokens)
		}
		window := tokens[start:end]
		if len(window) < MinTokens {
			break
		}
		chunkText := strings.Join(window, " ")
		chunks = append(chunks, Chunk{
			Index: idx,
			Text:  chunkText,
			Hash:  Hash(chunkText),
		})
		idx++
		if end == len(tokens) {
			break
		}
	}
	return chunks
}
