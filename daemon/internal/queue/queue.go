package queue

import (
	"log"
	"sync"

	"github.com/vbm/daemon/internal/store"
)

// Queue is a buffered in-memory work queue backed by a store.
type Queue struct {
	ch    chan store.IngestRequest
	store *store.Store
	wg    sync.WaitGroup
}

// New creates a Queue with a given buffer size and starts the background worker.
func New(s *store.Store, bufSize int) *Queue {
	q := &Queue{ch: make(chan store.IngestRequest, bufSize), store: s}
	q.wg.Add(1)
	go q.worker()
	return q
}

// Enqueue adds a request to the queue. Drops silently if buffer is full.
func (q *Queue) Enqueue(req store.IngestRequest) {
	select {
	case q.ch <- req:
	default:
		log.Printf("[queue] dropped ingest for %s: buffer full", req.URL)
	}
}

// Close signals the worker to stop after draining remaining items.
// Must be called exactly once. After Close, Enqueue must not be called.
func (q *Queue) Close() {
	close(q.ch)
}

// Wait blocks until the worker goroutine has finished processing all items.
func (q *Queue) Wait() {
	q.wg.Wait()
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for req := range q.ch {
		if err := q.store.Ingest(req); err != nil {
			log.Printf("[queue] ingest error for %s: %v", req.URL, err)
			continue
		}
		// P2-02: remove from persistent queue after successful ingest.
		if err := q.store.RemoveQueueItem(req.URL); err != nil {
			log.Printf("[queue] remove queue item error for %s: %v", req.URL, err)
		}
	}
}
