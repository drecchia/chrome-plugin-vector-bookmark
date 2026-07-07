package server

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/vbm/daemon/internal/store"
)

// serverMetrics holds atomic counters exposed at /metrics in Prometheus text format.
// P2-08: enables Prometheus scraping without an external client library dependency.
type serverMetrics struct {
	ingestTotal atomic.Int64
	searchTotal atomic.Int64
	forgetTotal atomic.Int64
	wsActive    atomic.Int64
}

func (m *serverMetrics) handler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, indexed, pending, _, _ := s.GetStatus()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# HELP vbm_ingest_total Total ingest requests accepted into queue\n")
		fmt.Fprintf(w, "# TYPE vbm_ingest_total counter\n")
		fmt.Fprintf(w, "vbm_ingest_total %d\n", m.ingestTotal.Load())
		fmt.Fprintf(w, "# HELP vbm_search_total Total search requests served\n")
		fmt.Fprintf(w, "# TYPE vbm_search_total counter\n")
		fmt.Fprintf(w, "vbm_search_total %d\n", m.searchTotal.Load())
		fmt.Fprintf(w, "# HELP vbm_forget_total Total forget requests served\n")
		fmt.Fprintf(w, "# TYPE vbm_forget_total counter\n")
		fmt.Fprintf(w, "vbm_forget_total %d\n", m.forgetTotal.Load())
		fmt.Fprintf(w, "# HELP vbm_ws_connections_active Active WebSocket connections\n")
		fmt.Fprintf(w, "# TYPE vbm_ws_connections_active gauge\n")
		fmt.Fprintf(w, "vbm_ws_connections_active %d\n", m.wsActive.Load())
		fmt.Fprintf(w, "# HELP vbm_pages_indexed Total pages indexed in database\n")
		fmt.Fprintf(w, "# TYPE vbm_pages_indexed gauge\n")
		fmt.Fprintf(w, "vbm_pages_indexed %d\n", indexed)
		fmt.Fprintf(w, "# HELP vbm_queue_pending Items currently pending in ingest queue\n")
		fmt.Fprintf(w, "# TYPE vbm_queue_pending gauge\n")
		fmt.Fprintf(w, "vbm_queue_pending %d\n", pending)
	}
}
