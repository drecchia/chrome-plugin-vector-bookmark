package queue

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vbm/daemon/internal/chunk"
	"github.com/vbm/daemon/internal/embed"
	"github.com/vbm/daemon/internal/store"
)

// countingEmbedder returns zero vecs but counts calls — useful for verifying
// that embed is invoked per-chunk and pipeline drains.
type countingEmbedder struct {
	calls atomic.Int64
}

func (c *countingEmbedder) Embed(text string) ([]float32, error) {
	c.calls.Add(1)
	return make([]float32, 8), nil
}
func (c *countingEmbedder) Dim() int        { return 8 }
func (c *countingEmbedder) Version() string { return "test-v0" }

func newTestStore(t *testing.T, e embed.Embedder) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "d"), e)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// longText builds text guaranteed to pass chunk.MinTokens.
func longText() string {
	return strings.Repeat("word ", chunk.MinTokens+20)
}

func TestPipeline_DrainsAndWrites(t *testing.T) {
	e := &countingEmbedder{}
	s := newTestStore(t, e)
	q := New(s, 16)

	const N = 5
	for i := 0; i < N; i++ {
		url := "https://example.com/p" + string(rune('a'+i))
		q.Enqueue(store.IngestRequest{
			URL:     url,
			Title:   "t",
			Text:    longText(),
			VisitTs: time.Now().UnixMilli(),
			Domain:  "example.com",
		})
	}
	q.Close()
	q.Wait()

	visited, indexed, _, _, err := s.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if indexed != N {
		t.Fatalf("expected %d indexed, got %d (visited=%d)", N, indexed, visited)
	}
	if e.calls.Load() == 0 {
		t.Fatalf("expected embedder to be called, got 0")
	}
}

func TestPipeline_TooShortSkipped(t *testing.T) {
	e := &countingEmbedder{}
	s := newTestStore(t, e)
	q := New(s, 4)
	q.Enqueue(store.IngestRequest{
		URL:     "https://example.com/short",
		Text:    "too short",
		VisitTs: time.Now().UnixMilli(),
	})
	q.Close()
	q.Wait()
	_, indexed, _, _, _ := s.GetStatus()
	if indexed != 0 {
		t.Fatalf("expected 0 indexed for short text, got %d", indexed)
	}
}

func TestEnqueue_BackpressureDropsSilently(t *testing.T) {
	// Use a bufSize of 1 and flood past it before any worker can drain.
	// We achieve this by NOT calling Close, then re-using a closed/drained
	// approach that is hard to time — instead, assert that Enqueue never
	// blocks or panics when inbound is saturated.
	e := &countingEmbedder{}
	s := newTestStore(t, e)
	q := New(s, 1)
	defer func() {
		q.Close()
		q.Wait()
	}()

	// Fire many calls; even if workers drain most, the select-default branch
	// must ensure no call blocks longer than trivial scheduling.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			q.Enqueue(store.IngestRequest{
				URL:  "https://x/" + string(rune('a'+(i%26))),
				Text: longText(),
			})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Enqueue blocked under saturation — backpressure broken")
	}
}
