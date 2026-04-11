package server

import (
	"encoding/json"
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

func newRouter(s *store.Store, q *queue.Queue, token, ver string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Healthz — no auth
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	// Auth-protected routes
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(token))
		r.Use(corsMiddleware)

		r.Post("/ingest", func(w http.ResponseWriter, req *http.Request) {
			var ir ingestRequest
			if err := json.NewDecoder(req.Body).Decode(&ir); err != nil {
				http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
				return
			}
			q.Enqueue(store.IngestRequest{
				URL:     ir.URL,
				Title:   ir.Title,
				Text:    ir.Text,
				VisitTs: ir.VisitTs,
				DwellMs: ir.DwellMs,
				Domain:  ir.Domain,
			})
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
			}
			resp := searchResponse{Results: make([]searchResultJSON, 0, len(results))}
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

		r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
			conn, err := upgrader.Upgrade(w, req, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					indexed, pending, err := s.GetStatus()
					if err != nil {
						return
					}
					type wsStatus struct {
						Type    string `json:"type"`
						Indexed int    `json:"indexed"`
						Pending int    `json:"pending"`
					}
					msg := wsStatus{Type: "status", Indexed: indexed, Pending: pending}
					if err := conn.WriteJSON(msg); err != nil {
						return
					}
				}
			}
		})

		r.Get("/ui", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(uiHTML))
		})
		r.Get("/ui/*", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(uiHTML))
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "chrome-extension://") {
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
