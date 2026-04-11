package main

import (
	"fmt"
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
		if err := server.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Usage: vbmd [server|nm-host]\n")
		os.Exit(1)
	}
}
