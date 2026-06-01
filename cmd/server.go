package cmd

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"

	"github.com/abhishek-sonje/octo/internal/tunnel"
)

// RunServer is the entry point for `octo server` (cloud relay mode).
//
// Cloud server startup sequence (from ARCHITECTURE.md):
//  1. Initialise empty session registry
//  2. Register HTTP routes via chi
//  3. Start HTTP server on :port
//  4. Block until SIGINT/SIGTERM
//  5. Graceful shutdown with 10s timeout
//
// embed.FS is passed from main.go — cmd/server.go cannot embed directly
// because //go:embed paths are relative to the source file, not the project root.
func RunServer(port int, fs embed.FS) error {
	// ── Step 1: Initialise registry ──────────────────────────────────────────
	reg := tunnel.NewRegistry()

	// ── Step 2: Build chi router ─────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// GET / → landing page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile("frontend/landing/index.html")
		if err != nil {
			http.Error(w, "landing page not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	// GET /install → redirect to raw GitHub install.sh
	r.Get("/install", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r,
			"https://raw.githubusercontent.com/abhishek-sonje/octo/main/install.sh",
			http.StatusMovedPermanently,
		)
	})

	// WS /tunnel/register → CLI registers its tunnel here
	r.Get("/tunnel/register", tunnel.RegisterHandler(reg))

	// GET /s/:sessionId  → serve session terminal HTML (plain browser visit)
	// WS  /s/:sessionId  → proxy browser WebSocket to CLI tunnel
	//
	// Both share one chi route. We distinguish by checking the
	// Upgrade header — matching the arch doc route table exactly.
	r.Get("/s/{sessionId}", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			tunnel.ProxyHandler(reg)(w, r)
			return
		}
		data, err := fs.ReadFile("frontend/session/index.html")
		if err != nil {
			http.Error(w, "session page not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	// ── Step 3: Start server ─────────────────────────────────────────────────
	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Printf("octo server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "octo server: %v\n", err)
		}
	}()

	// ── Step 4: Block until signal ───────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("octo server: shutting down...")

	// ── Step 5: Graceful shutdown ────────────────────────────────────────────
	// 10s gives active tunnel sessions time to drain before the server closes.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

// isWebSocketUpgrade reports whether r is a WebSocket upgrade request.
// Lets a single chi route serve both plain GET (HTML page) and WS traffic
// for /s/{sessionId} — matching the arch doc route table.
func isWebSocketUpgrade(r *http.Request) bool {
	return websocket.IsWebSocketUpgrade(r)
}