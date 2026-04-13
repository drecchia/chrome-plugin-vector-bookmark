package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vbm/daemon/internal/embed"
	"github.com/vbm/daemon/internal/nm"
	"github.com/vbm/daemon/internal/queue"
	"github.com/vbm/daemon/internal/store"
)

const version = "0.1.0"

// Run starts the daemon server. Blocks until SIGTERM/SIGINT.
func Run() error {
	dataDir, err := nm.DataDir()
	if err != nil {
		return fmt.Errorf("data dir: %w", err)
	}

	// Select embedder based on VBM_EMBED_URL / VBM_EMBED_FORMAT / VBM_EMBED_API_KEY.
	// VBM_EMBED_FORMAT=openai (or any non-empty VBM_EMBED_API_KEY) → OpenAI-compatible
	// (works with OpenRouter, OpenAI, Ollama /v1/embeddings).
	// Default format=ollama → Ollama native API (/api/embeddings).
	var embedder embed.Embedder = embed.NewStubEmbedder()
	if u := os.Getenv("VBM_EMBED_URL"); u != "" {
		model := os.Getenv("VBM_EMBED_MODEL")
		apiKey := os.Getenv("VBM_EMBED_API_KEY")
		format := os.Getenv("VBM_EMBED_FORMAT")
		if apiKey != "" || format == "openai" {
			embedder = embed.NewOpenAIEmbedder(u, model, apiKey)
			slog.Info("using OpenAI-compatible embedder", "url", u, "model", model)
		} else {
			embedder = embed.NewHttpEmbedder(u, model)
			slog.Info("using Ollama embedder", "url", u, "model", model)
		}
	} else {
		slog.Warn("VBM_EMBED_URL not set — using stub embedder (BM25-only, no semantic search)")
	}

	s, err := store.New(dataDir, embedder)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer s.Close()

	q := queue.New(s, 256)

	// Re-enqueue any items that were pending when the daemon last shut down or crashed.
	if pending, err := s.LoadPendingItems(); err != nil {
		slog.Warn("could not load pending queue items", "err", err)
	} else if len(pending) > 0 {
		slog.Info("re-enqueueing pending items from previous run", "count", len(pending))
		for _, item := range pending {
			q.Enqueue(item)
		}
	}

	// Default port 7532. Override via VBM_PORT env var.
	// Use VBM_BIND to override interface (Docker only — e.g. VBM_BIND=0.0.0.0).
	bind := "127.0.0.1"
	if b := os.Getenv("VBM_BIND"); b != "" {
		bind = b
		slog.Warn("binding on non-loopback interface", "bind", bind)
	}
	portStr := "7532"
	if p := os.Getenv("VBM_PORT"); p != "" {
		portStr = p
	}
	listenAddr := bind + ":" + portStr

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	slog.Info("server listening", "addr", listener.Addr().String())

	// P0-07: start periodic cleanup if VBM_TTL_DAYS is set.
	if t := os.Getenv("VBM_TTL_DAYS"); t != "" {
		if ttlDays, err := strconv.Atoi(t); err == nil && ttlDays > 0 {
			slog.Info("data retention enabled", "ttl_days", ttlDays)
			go func() {
				if n, err := s.Cleanup(ttlDays); err != nil {
					slog.Error("startup cleanup error", "err", err)
				} else if n > 0 {
					slog.Info("startup cleanup complete", "pages_removed", n)
				}
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for range ticker.C {
					if n, err := s.Cleanup(ttlDays); err != nil {
						slog.Error("cleanup error", "err", err)
					} else if n > 0 {
						slog.Info("cleanup complete", "pages_removed", n)
					}
				}
			}()
		}
	}

	// P1-07: allow external origins via VBM_CORS_ORIGIN (comma-separated).
	var extraOrigins []string
	if co := os.Getenv("VBM_CORS_ORIGIN"); co != "" {
		for _, o := range strings.Split(co, ",") {
			if o = strings.TrimSpace(o); o != "" {
				extraOrigins = append(extraOrigins, o)
			}
		}
	}

	r := newRouter(s, q, version, extraOrigins)

	srv := &http.Server{Handler: r}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// P0-04: drain queue after HTTP server shuts down.
	go func() {
		<-ctx.Done()
		slog.Info("shutting down HTTP server")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck

		slog.Info("draining ingest queue")
		q.Close()
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		done := make(chan struct{})
		go func() { q.Wait(); close(done) }()
		select {
		case <-done:
			slog.Info("queue drained")
		case <-drainCtx.Done():
			slog.Warn("queue drain timeout, some pending items may be lost")
		}
	}()

	return srv.Serve(listener)
}
