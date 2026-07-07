package queue

import (
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/vbm/daemon/internal/chunk"
	"github.com/vbm/daemon/internal/store"
)

// embedRetryDelays defines the backoff schedule for a single request's embed
// attempts (CR-0010). len+1 total attempts: the first is immediate, then one
// wait per entry. Tuned for transient OpenRouter 429/5xx on a local daemon.
var embedRetryDelays = []time.Duration{1 * time.Second, 4 * time.Second, 10 * time.Second}

// Queue is a two-stage pipeline:
//
//	Enqueue → inbound (cap bufSize) → N embedder workers (chunk+embed) →
//	writeCh (cap 64) → 1 DB-writer goroutine → SQLite
//
// Splitting embed (CPU-bound) from write (SQLite single-writer) lets
// embedding run in parallel without producing lock contention on the DB.
type Queue struct {
	inbound chan store.IngestRequest
	writeCh chan store.PreparedIngest
	store   *store.Store

	embedWg sync.WaitGroup // tracks embed workers; when all exit, writeCh closes
	allWg   sync.WaitGroup // tracks every goroutine (embed + writer + coordinator)
}

// workerCount caps embed workers at 4 — more threads compete for CPU with the
// HTTP server and the DB-writer without adding throughput for a local daemon.
func workerCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return n
}

// New creates a Queue with the given inbound buffer size and starts workers.
func New(s *store.Store, bufSize int) *Queue {
	n := workerCount()
	q := &Queue{
		inbound: make(chan store.IngestRequest, bufSize),
		writeCh: make(chan store.PreparedIngest, 64),
		store:   s,
	}
	for i := 0; i < n; i++ {
		q.embedWg.Add(1)
		q.allWg.Add(1)
		go q.embedWorker()
	}
	q.allWg.Add(1)
	go q.writer()
	// Coordinator: when all embed workers have drained and exited, close writeCh
	// so the writer knows no more prepared ingests will arrive.
	q.allWg.Add(1)
	go func() {
		defer q.allWg.Done()
		q.embedWg.Wait()
		close(q.writeCh)
	}()
	slog.Info("ingest pipeline started", "embed_workers", n, "inbound_cap", bufSize, "write_cap", 64)
	return q
}

// Enqueue adds a request to the inbound buffer. Returns false if the buffer is
// full (the request was NOT accepted) so the caller can surface backpressure
// instead of reporting a false success. CR-0010.
func (q *Queue) Enqueue(req store.IngestRequest) bool {
	select {
	case q.inbound <- req:
		return true
	default:
		slog.Warn("dropped ingest, buffer full", "url", req.URL)
		return false
	}
}

// Close signals the pipeline to drain and stop. Must be called exactly once.
func (q *Queue) Close() { close(q.inbound) }

// Wait blocks until every goroutine (workers + writer + coordinator) has exited.
func (q *Queue) Wait() { q.allWg.Wait() }

func (q *Queue) embedWorker() {
	defer q.embedWg.Done()
	defer q.allWg.Done()
	e := q.store.Embedder()
	for req := range q.inbound {
		chunks := chunk.SplitIntoChunks(req.Text)
		if len(chunks) == 0 {
			// Text too short for chunking. Skip embedding entirely, but still
			// flush the request to the writer when there's a tag delta — tags
			// are metadata that must persist regardless of the body length
			// (otherwise llm_summary/meta_only/manual modes silently drop tags).
			hasTagDelta := req.SetTags || len(req.Tags) > 0
			if !hasTagDelta {
				if err := q.store.RemoveQueueItem(req.URL); err != nil {
					slog.Warn("remove queue item (too-short) error", "url", req.URL, "err", err)
				}
				continue
			}
			q.writeCh <- store.PreparedIngest{Req: req, Chunks: nil}
			continue
		}
		prepared, attempts, lastErr := q.embedChunks(e, req, chunks)
		if lastErr != nil {
			// CR-0010: definitive failure after all retries. Flip the queue row
			// to 'failed' with the error so the popup can show it and offer a
			// manual retry — instead of leaving a 'pending' row only a restart
			// would ever touch.
			slog.Error("embed failed after retries", "url", req.URL, "attempts", attempts, "err", lastErr)
			if err := q.store.MarkQueueItemFailed(req.URL, lastErr.Error(), attempts); err != nil {
				slog.Warn("mark queue item failed error", "url", req.URL, "err", err)
			}
			continue
		}
		q.writeCh <- store.PreparedIngest{Req: req, Chunks: prepared}
	}
}

// embedChunks embeds every chunk of a request, retrying the whole request on
// the first embed error using embedRetryDelays. Returns the prepared chunks on
// success, or the number of attempts made and the last error on failure.
func (q *Queue) embedChunks(e embedder, req store.IngestRequest, chunks []chunk.Chunk) ([]store.PreparedChunk, int, error) {
	var lastErr error
	for attempt := 0; attempt <= len(embedRetryDelays); attempt++ {
		if attempt > 0 {
			delay := embedRetryDelays[attempt-1]
			slog.Warn("retrying embed after error", "url", req.URL, "attempt", attempt, "delay", delay, "err", lastErr)
			time.Sleep(delay)
		}
		prepared := make([]store.PreparedChunk, 0, len(chunks))
		failed := false
		for _, c := range chunks {
			vec, err := e.Embed(c.Text)
			if err != nil {
				lastErr = err
				failed = true
				break
			}
			prepared = append(prepared, store.PreparedChunk{
				Index: c.Index,
				Text:  c.Text,
				Hash:  c.Hash,
				Vec:   vec,
			})
		}
		if !failed {
			return prepared, attempt + 1, nil
		}
	}
	return nil, len(embedRetryDelays) + 1, lastErr
}

// embedder is the subset of embed.Embedder the queue needs — declared locally
// to avoid importing the embed package just for the parameter type.
type embedder interface {
	Embed(text string) ([]float32, error)
}

func (q *Queue) writer() {
	defer q.allWg.Done()
	for p := range q.writeCh {
		if err := q.store.IngestPrepared(p); err != nil {
			slog.Error("db write error", "url", p.Req.URL, "err", err)
			continue
		}
		if err := q.store.RemoveQueueItem(p.Req.URL); err != nil {
			slog.Warn("remove queue item error", "url", p.Req.URL, "err", err)
		}
	}
}
