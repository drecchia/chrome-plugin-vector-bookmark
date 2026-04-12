package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vbm/daemon/internal/nm"
	"github.com/vbm/daemon/internal/server"
)

func main() {
	// On Windows, load %APPDATA%\vbm\env before anything else so env vars
	// set there (VBM_PORT, VBM_EMBED_URL, etc.) are visible to the server.
	// On Linux this is handled by systemd EnvironmentFile= in vbmd.service.
	if runtime.GOOS == "windows" {
		if dir, err := nm.DataDir(); err == nil {
			loadEnvFile(filepath.Join(dir, "env"))
		}
	}

	mode := "server"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "server":
		// P1-13: structured JSON logs. Level controlled by VBM_LOG_LEVEL.
		logLevel := slog.LevelInfo
		switch strings.ToLower(os.Getenv("VBM_LOG_LEVEL")) {
		case "debug":
			logLevel = slog.LevelDebug
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
		if err := server.Run(); err != nil {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Usage: vbmd [server]\n")
		os.Exit(1)
	}
}

// loadEnvFile reads a KEY=value file and sets missing env vars.
// Lines starting with # and blank lines are ignored. Errors are silently
// swallowed — the file is optional (hence the leading "-" in systemd's
// EnvironmentFile syntax).
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Only set if not already present in environment — allows caller
		// to override via explicit env vars (e.g. VBM_PORT=7532 make run).
		if k != "" && os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
