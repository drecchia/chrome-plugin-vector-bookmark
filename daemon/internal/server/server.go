package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/vbm/daemon/internal/embed"
	"github.com/vbm/daemon/internal/nm"
	"github.com/vbm/daemon/internal/queue"
	"github.com/vbm/daemon/internal/store"
)

const version = "0.1.0"

// Run starts the daemon server. Blocks until SIGTERM/SIGINT.
func Run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	dataDir := filepath.Join(home, ".local", "share", "vbm")

	// P0-02: select embedder based on VBM_EMBED_URL.
	var embedder embed.Embedder = embed.NewStubEmbedder()
	if u := os.Getenv("VBM_EMBED_URL"); u != "" {
		model := os.Getenv("VBM_EMBED_MODEL")
		embedder = embed.NewHttpEmbedder(u, model)
		log.Printf("[vbmd] using HTTP embedder: %s (model: %s)", u, model)
	} else {
		log.Printf("[vbmd] WARNING: VBM_EMBED_URL not set — using stub embedder (BM25-only, no semantic search)")
	}

	s, err := store.New(dataDir, embedder)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer s.Close()

	q := queue.New(s, 256)

	token := uuid.New().String()

	// P0-06: VBM_PORT always binds on loopback.
	// Use VBM_BIND to override interface (Docker only — e.g. VBM_BIND=0.0.0.0).
	listenAddr := "127.0.0.1:0"
	if p := os.Getenv("VBM_PORT"); p != "" {
		bind := "127.0.0.1"
		if b := os.Getenv("VBM_BIND"); b != "" {
			bind = b
			log.Printf("[vbmd] WARNING: binding on %s — ensure firewall restricts access", bind)
		}
		listenAddr = bind + ":" + p
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	if err := nm.WriteSession(&nm.Session{Port: port, Token: token}); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	if sessionPath, err := nm.SessionPath(); err == nil {
		defer os.Remove(sessionPath)
	}

	log.Printf("[vbmd] server listening on %s", listener.Addr().String())

	// P0-07: start periodic cleanup if VBM_TTL_DAYS is set.
	if t := os.Getenv("VBM_TTL_DAYS"); t != "" {
		if ttlDays, err := strconv.Atoi(t); err == nil && ttlDays > 0 {
			log.Printf("[vbmd] data retention: %d days", ttlDays)
			go func() {
				if n, err := s.Cleanup(ttlDays); err != nil {
					log.Printf("[vbmd] cleanup error: %v", err)
				} else if n > 0 {
					log.Printf("[vbmd] startup cleanup: removed %d pages", n)
				}
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for range ticker.C {
					if n, err := s.Cleanup(ttlDays); err != nil {
						log.Printf("[vbmd] cleanup error: %v", err)
					} else if n > 0 {
						log.Printf("[vbmd] cleanup: removed %d pages", n)
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

	r := newRouter(s, q, token, version, extraOrigins)

	srv := &http.Server{Handler: r}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// P0-04: drain queue after HTTP server shuts down.
	go func() {
		<-ctx.Done()
		log.Println("[vbmd] shutting down HTTP server...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck

		log.Println("[vbmd] draining ingest queue...")
		q.Close()
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		done := make(chan struct{})
		go func() { q.Wait(); close(done) }()
		select {
		case <-done:
			log.Println("[vbmd] queue drained")
		case <-drainCtx.Done():
			log.Println("[vbmd] queue drain timeout — some pending items may be lost")
		}
	}()

	return srv.Serve(listener)
}
