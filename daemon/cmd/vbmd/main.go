package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/vbm/daemon/internal/nm"
	"github.com/vbm/daemon/internal/server"
)

func main() {
	mode := "server"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "nm-host", "--nm-host":
		if err := nm.RunHost(); err != nil {
			fmt.Fprintf(os.Stderr, "nm-host error: %v\n", err)
			os.Exit(1)
		}
	case "server":
		// P1-13: structured JSON logs. Level controlled by VBM_LOG_LEVEL=debug.
		logLevel := slog.LevelInfo
		if os.Getenv("VBM_LOG_LEVEL") == "debug" {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
		if err := server.Run(); err != nil {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Usage: vbmd [server|nm-host]\n")
		os.Exit(1)
	}
}
