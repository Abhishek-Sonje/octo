package cmd

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	iofs "io/fs"
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
func RunServer(port int, embedFS embed.FS) error {
	// ── Step 1: Initialise registry ──────────────────────────────────────────
	reg := tunnel.NewRegistry()

	// ── Step 1.5: Parse templates and static FS ──────────────────────────────
	tmpl, err := template.ParseFS(embedFS, "frontend/session/index.html")
	if err != nil {
		return fmt.Errorf("failed to parse session template: %w", err)
	}

	subFS, err := iofs.Sub(embedFS, "frontend")
	if err != nil {
		return fmt.Errorf("failed to sub frontend FS: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))

	// ── Step 2: Build chi router ─────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Serve static assets for landing and session pages
	r.Handle("/landing/*", fileServer)
	r.Handle("/session/*", fileServer)

	// GET / → landing page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data, err := embedFS.ReadFile("frontend/landing/index.html")
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

	// GET /status → active session count
	r.Get("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"active_sessions": reg.Count(),
		})
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

		sessionId := chi.URLParam(r, "sessionId")
		_, ok := reg.Get(sessionId)
		if !ok {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			errorData, err := embedFS.ReadFile("frontend/error.html")
			if err != nil {
				w.Write([]byte("Session not found"))
				return
			}
			w.Write(errorData)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		err = tmpl.Execute(w, struct {
			SessionID string
			Token     string
		}{
			SessionID: sessionId,
			Token:     "",
		})
		if err != nil {
			log.Println("session template execution error:", err)
		}
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

	// Close all active sessions in the registry to disconnect them cleanly
	reg.Close()

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