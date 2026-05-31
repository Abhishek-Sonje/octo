package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"github.com/abhishek-sonje/octo/cmd"
)

// frontendFS embeds both HTML pages at compile time.
// Path is relative to this file (project root) — frontend/ is right here.
//
//go:embed frontend/landing/index.html frontend/session/index.html
var frontendFS embed.FS

func main() {
	// ── octo server ──────────────────────────────────────────────────────────
	if len(os.Args) > 1 && os.Args[1] == "server" {
		serverFlags := flag.NewFlagSet("server", flag.ExitOnError)
		port := serverFlags.Int("port", 7681, "port to listen on")
		serverFlags.Parse(os.Args[2:])

		if err := cmd.RunServer(*port, frontendFS); err != nil {
			fmt.Fprintln(os.Stderr, "octo server error:", err)
			os.Exit(1)
		}
		return
	}

	// ── octo (CLI mode) ──────────────────────────────────────────────────────
	port     := flag.Int("port",      7681,  "local port to listen on")
	shell    := flag.String("shell",  "",    "shell to spawn (default: $SHELL → /bin/bash)")
	noTunnel := flag.Bool("no-tunnel", false, "disable tunnel, local access only")
	once     := flag.Bool("once",     false, "exit after first session disconnects")
	flag.Parse()

	opts := cmd.LocalOpts{
		Port:     *port,
		Shell:    *shell,
		NoTunnel: *noTunnel,
		Once:     *once,
		FS:       frontendFS,
	}

	if err := cmd.RunLocal(opts); err != nil {
		fmt.Fprintln(os.Stderr, "octo error:", err)
		os.Exit(1)
	}
}