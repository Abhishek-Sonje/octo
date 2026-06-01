package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"

	"github.com/abhishek-sonje/octo/cmd"
)

// frontendFS embeds the frontend directory at compile time.
// Path is relative to this file (project root) — frontend/ is right here.
//
//go:embed frontend
var frontendFS embed.FS

func main() {
	// Attempt to load .env file, ignoring errors if it doesn't exist
	_ = godotenv.Load()

	defaultPort := 7681
	if portStr := os.Getenv("OCTO_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			defaultPort = p
		}
	}

	// ── octo server ──────────────────────────────────────────────────────────
	if len(os.Args) > 1 && os.Args[1] == "server" {
		serverFlags := flag.NewFlagSet("server", flag.ExitOnError)
		port := serverFlags.Int("port", defaultPort, "port to listen on")
		serverFlags.Parse(os.Args[2:])

		if err := cmd.RunServer(*port, frontendFS); err != nil {
			fmt.Fprintln(os.Stderr, "octo server error:", err)
			os.Exit(1)
		}
		return
	}

	// ── octo (CLI mode) ──────────────────────────────────────────────────────
	port     := flag.Int("port",      defaultPort,  "local port to listen on")
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