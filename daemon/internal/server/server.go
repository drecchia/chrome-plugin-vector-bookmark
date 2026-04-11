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

	embedder := embed.NewStubEmbedder()
	s, err := store.New(dataDir, embedder)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer s.Close()

	q := queue.New(s, 256)

	token := uuid.New().String()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	if err := nm.WriteSession(&nm.Session{Port: port, Token: token}); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	defer os.Remove(nm.SessionPath())

	log.Printf("[vbmd] server listening on 127.0.0.1:%d", port)

	r := newRouter(s, q, token, version)

	srv := &http.Server{Handler: r}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("[vbmd] shutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	return srv.Serve(listener)
}
