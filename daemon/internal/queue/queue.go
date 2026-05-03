package queue

import (
	"log/slog"
	"runtime"
	"sync"

	"github.com/vbm/daemon/internal/chunk"
	"github.com/vbm/daemon/internal/store"
)

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

// Enqueue adds a request to the inbound buffer. Drops silently if full.
func (q *Queue) Enqueue(req store.IngestRequest) {
	select {
	case q.inbound <- req:
	default:
		slog.Warn("dropped ingest, buffer full", "url", req.URL)
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
		prepared := make([]store.PreparedChunk, 0, len(chunks))
		failed := false
		for _, c := range chunks {
			vec, err := e.Embed(c.Text)
			if err != nil {
				slog.Error("embed chunk error", "url", req.URL, "chunk", c.Index, "err", err)
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
		if failed {
			// Leave queue row as 'pending' — retried on restart.
			continue
		}
		q.writeCh <- store.PreparedIngest{Req: req, Chunks: prepared}
	}
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
