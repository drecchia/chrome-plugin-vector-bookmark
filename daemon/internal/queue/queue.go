package queue

import (
	"log"

	"github.com/vbm/daemon/internal/store"
)

// Queue is a buffered in-memory work queue backed by a store.
type Queue struct {
	ch    chan store.IngestRequest
	store *store.Store
}

// New creates a Queue with a given buffer size and starts the background worker.
func New(s *store.Store, bufSize int) *Queue {
	q := &Queue{ch: make(chan store.IngestRequest, bufSize), store: s}
	go q.worker()
	return q
}

// Enqueue adds a request to the queue. Drops if buffer is full.
func (q *Queue) Enqueue(req store.IngestRequest) {
	select {
	case q.ch <- req:
	default:
		log.Printf("[queue] dropped ingest for %s: buffer full", req.URL)
	}
}

func (q *Queue) worker() {
	for req := range q.ch {
		if err := q.store.Ingest(req); err != nil {
			log.Printf("[queue] ingest error for %s: %v", req.URL, err)
		}
	}
}
