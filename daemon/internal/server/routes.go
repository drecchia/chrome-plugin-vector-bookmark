package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/vbm/daemon/internal/queue"
	"github.com/vbm/daemon/internal/store"
)

type ingestRequest struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	VisitTs int64  `json:"visitTs"`
	DwellMs int64  `json:"dwellMs"`
	Domain  string `json:"domain"`
}

type forgetRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Validated by auth middleware already
		return true
	},
}

const uiHTML = `<!DOCTYPE html>
<html>
<head><title>Vector Bookmark</title></head>
<body>
<h1>Vector Bookmark — Local UI</h1>
<p>Daemon running. Full UI coming in v0.2.</p>
<form id="search">
  <input name="q" placeholder="Search your reading history..." style="width:400px;font-size:16px;padding:8px">
  <button type="submit">Search</button>
</form>
<div id="results"></div>
<script>
document.getElementById('search').onsubmit = async e => {
  e.preventDefault()
  const q = e.target.q.value
  const r = await fetch('/search?q=' + encodeURIComponent(q) + '&limit=10', {
    headers: {'Authorization': 'Bearer REPLACE_TOKEN'}
  })
  const data = await r.json()
  document.getElementById('results').innerHTML = data.results?.map(r =>
    '<div style="margin:8px 0"><a href="' + r.url + '">' + (r.title||r.url) + '</a><br><small>' + r.snippet + '</small></div>'
  ).join('') || 'No results'
}
</script>
</body>
</html>`

// newRouter builds the HTTP router. extraOrigins are additional CORS-allowed origins
// beyond chrome-extension:// (e.g. a local dashboard, configured via VBM_CORS_ORIGIN).
func newRouter(s *store.Store, q *queue.Queue, token, ver string, extraOrigins []string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// P2-08: metrics counters — no external dependency, plain Prometheus text format.
	m := &serverMetrics{}

	// Healthz — no auth, but checks DB connectivity (P1-04).
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := s.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ok":false,"error":"database unavailable"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})

	// P2-08: /metrics in Prometheus text format — no auth required for scraping.
	r.Get("/metrics", m.handler(s))

	// Auth-protected routes
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(token))
		r.Use(corsMiddleware(extraOrigins))

		r.Post("/ingest", func(w http.ResponseWriter, req *http.Request) {
			var ir ingestRequest
			if err := json.NewDecoder(req.Body).Decode(&ir); err != nil {
				http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
				return
			}
			ireq := store.IngestRequest{
				URL:     ir.URL,
				Title:   ir.Title,
				Text:    ir.Text,
				VisitTs: ir.VisitTs,
				DwellMs: ir.DwellMs,
				Domain:  ir.Domain,
			}
			q.Enqueue(ireq)
			// P2-02: persist to queue table so pending count is accurate and processed items get cleaned up.
			if err := s.AddQueueItem(ireq); err != nil {
				log.Printf("[ingest] queue persist error for %s: %v", ir.URL, err)
			}
			m.ingestTotal.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"queued":true}`))
		})

		r.Get("/search", func(w http.ResponseWriter, req *http.Request) {
			query := req.URL.Query().Get("q")
			if query == "" {
				http.Error(w, `{"error":"q required"}`, http.StatusBadRequest)
				return
			}
			limitStr := req.URL.Query().Get("limit")
			limit := 5
			if limitStr != "" {
				if n, err := strconv.Atoi(limitStr); err == nil {
					limit = n
				}
			}
			if limit > 20 {
				limit = 20
			}
			results, err := s.Search(query, limit)
			if err != nil {
				http.Error(w, `{"error":"search failed"}`, http.StatusInternalServerError)
				return
			}
			m.searchTotal.Add(1)
			type searchResultJSON struct {
				URL     string  `json:"url"`
				Title   string  `json:"title"`
				Snippet string  `json:"snippet"`
				VisitTs int64   `json:"visitTs"`
				Score   float64 `json:"score"`
				Domain  string  `json:"domain"`
			}
			type searchResponse struct {
				Results []searchResultJSON `json:"results"`
				Total   int                `json:"total"`
			}
			resp := searchResponse{Results: make([]searchResultJSON, 0, len(results)), Total: len(results)}
			for _, res := range results {
				resp.Results = append(resp.Results, searchResultJSON{
					URL:     res.URL,
					Title:   res.Title,
					Snippet: res.Snippet,
					VisitTs: res.VisitTs,
					Score:   res.Score,
					Domain:  res.Domain,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})

		r.Delete("/forget", func(w http.ResponseWriter, req *http.Request) {
			var fr forgetRequest
			if err := json.NewDecoder(req.Body).Decode(&fr); err != nil {
				http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
				return
			}
			if err := s.Forget(store.ForgetRequest{Type: fr.Type, Value: fr.Value}); err != nil {
				http.Error(w, `{"error":"forget failed"}`, http.StatusInternalServerError)
				return
			}
			m.forgetTotal.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"forgotten":true}`))
		})

		r.Get("/status", func(w http.ResponseWriter, req *http.Request) {
			indexed, pending, err := s.GetStatus()
			if err != nil {
				http.Error(w, `{"error":"status failed"}`, http.StatusInternalServerError)
				return
			}
			type statusResponse struct {
				Indexed int    `json:"indexed"`
				Pending int    `json:"pending"`
				Version string `json:"version"`
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(statusResponse{
				Indexed: indexed,
				Pending: pending,
				Version: ver,
			})
		})

		// P1-15: export endpoint for LGPD portability.
		r.Get("/export", func(w http.ResponseWriter, req *http.Request) {
			pages, err := s.Export()
			if err != nil {
				http.Error(w, `{"error":"export failed"}`, http.StatusInternalServerError)
				return
			}
			type exportResponse struct {
				Pages []store.ExportPage `json:"pages"`
				Total int                `json:"total"`
			}
			if pages == nil {
				pages = []store.ExportPage{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(exportResponse{Pages: pages, Total: len(pages)})
		})

		// P1-10: WebSocket with ping/pong and write deadlines.
		r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
			conn, err := upgrader.Upgrade(w, req, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			m.wsActive.Add(1)
			defer m.wsActive.Add(-1)

			const readDeadline = 60 * time.Second
			const writeDeadline = 10 * time.Second
			const pingInterval = 30 * time.Second

			conn.SetReadDeadline(time.Now().Add(readDeadline))
			conn.SetPongHandler(func(string) error {
				conn.SetReadDeadline(time.Now().Add(readDeadline))
				return nil
			})

			// Read goroutine required by gorilla/websocket to process control frames.
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}()

			statusTicker := time.NewTicker(5 * time.Second)
			pingTicker := time.NewTicker(pingInterval)
			defer statusTicker.Stop()
			defer pingTicker.Stop()

			for {
				select {
				case <-readDone:
					return
				case <-statusTicker.C:
					indexed, pending, err := s.GetStatus()
					if err != nil {
						return
					}
					type wsStatus struct {
						Type    string `json:"type"`
						Indexed int    `json:"indexed"`
						Pending int    `json:"pending"`
					}
					conn.SetWriteDeadline(time.Now().Add(writeDeadline))
					msg := wsStatus{Type: "status", Indexed: indexed, Pending: pending}
					if err := conn.WriteJSON(msg); err != nil {
						return
					}
				case <-pingTicker.C:
					conn.SetWriteDeadline(time.Now().Add(writeDeadline))
					if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						return
					}
				}
			}
		})

		// P2-01: inject actual session token so the embedded UI search form works.
		uiWithToken := strings.ReplaceAll(uiHTML, "REPLACE_TOKEN", token)
		r.Get("/ui", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(uiWithToken))
		})
		r.Get("/ui/*", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(uiWithToken))
		})
	})

	return r
}

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle CORS preflight without auth check
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Check Origin header
			origin := r.Header.Get("Origin")
			if origin != "" && !strings.HasPrefix(origin, "chrome-extension://") {
				http.Error(w, `{"error":"forbidden origin"}`, http.StatusUnauthorized)
				return
			}

			// Check Authorization header
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + token
			if authHeader != expected {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// corsMiddleware sets CORS headers for chrome-extension:// origins and any extraOrigins.
// P1-07: extraOrigins allows external dashboards (e.g. VBM_CORS_ORIGIN=http://localhost:3000).
func corsMiddleware(extraOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(extraOrigins))
	for _, o := range extraOrigins {
		if o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if strings.HasPrefix(origin, "chrome-extension://") || allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
