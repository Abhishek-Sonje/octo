package cmd

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	internalty "github.com/abhishek-sonje/octo/internal/pty"
	"github.com/abhishek-sonje/octo/internal/tunnel"
	"github.com/abhishek-sonje/octo/internal/ws"
)

// LocalOpts holds all flags parsed in main.go for the CLI mode.
type LocalOpts struct {
	Port     int
	Shell    string
	NoTunnel bool
	Once     bool
	FS       embed.FS // passed from main.go — root-level embed
}

var upgrader = websocket.Upgrader{
	// Allow all origins for local network access
	CheckOrigin: func(r *http.Request) bool { return true },
}

// RunLocal is the entry point for `octo` (CLI mode).
//
// CLI startup sequence (from ARCHITECTURE.md):
//  1. Parse flags — done by main.go, passed via LocalOpts
//  2. Generate sessionId (5-char alphanumeric, crypto/rand)
//  3. Generate token (32-byte hex, crypto/rand)
//  4. Start local HTTP/WS server on --port
//  5. Detect local network IP
//  6. If not --no-tunnel: connect outbound tunnel + start ping goroutine
//  7. Print URLs
//  8. Block until Ctrl+C (or session end if --once)
//  9. Graceful shutdown
func RunLocal(opts LocalOpts) error {
	// ── Step 2 & 3: Credentials ──────────────────────────────────────────────
	sessionID := randomAlphanumeric(5)
	token := randomHex(32)

	// ── Step 4: HTTP mux ─────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Serve xterm.js session page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, _ := opts.FS.ReadFile("frontend/session/index.html")
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	// sessionDone is closed when the first session ends — used for --once
	sessionDone := make(chan struct{})
	var sessionDoneOnce sync.Once

	// WebSocket handler — one PTY per connection
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Validate token before allocating any resource — arch doc gotcha #1
		if r.URL.Query().Get("token") != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		p, err := internalty.Spawn(opts.Shell)
		if err != nil {
			conn.Close()
			return
		}

		var onDone func()
		if opts.Once {
			onDone = func() {
				sessionDoneOnce.Do(func() { close(sessionDone) })
			}
		}

		ws.Bridge(p.Master, conn, onDone)
	})

	addr := fmt.Sprintf(":%d", opts.Port)
	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "octo: server error: %v\n", err)
		}
	}()

	// ── Step 5: Detect local network IP ─────────────────────────────────────
	localIP := localNetworkIP()

	// ── Step 6: Connect tunnel ───────────────────────────────────────────────
	if !opts.NoTunnel {
		// Spawn a dedicated PTY for the tunnel session.
		// This is separate from any /ws local session — each gets its own shell.
		tunnelPTY, err := internalty.Spawn(opts.Shell)
		if err != nil {
			fmt.Fprintf(os.Stderr, "octo: tunnel pty error: %v\n", err)
		} else {
			go func() {
				if err := tunnel.Connect(sessionID, token, tunnelPTY.Master); err != nil {
					fmt.Fprintf(os.Stderr, "octo: tunnel closed: %v\n", err)
				}
			}()
		}
	}

	// ── Step 7: Print URLs ───────────────────────────────────────────────────
	fmt.Printf("\n✓ octo started\n\n")
	fmt.Printf("  Local:   http://%s:%d?token=%s\n", localIP, opts.Port, token)
	if !opts.NoTunnel {
		fmt.Printf("  Remote:  https://octo.sh/s/%s\n", sessionID)
	}
	fmt.Printf("\n  Ctrl+C to stop\n\n")

	// ── Step 8: Block ────────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		fmt.Println("\nshutting down...")
	case <-sessionDone:
		fmt.Println("\nsession ended — shutting down (--once)")
	}

	// ── Step 9: Graceful shutdown ────────────────────────────────────────────
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

// localNetworkIP returns the first non-loopback IPv4 address found on
// active network interfaces — this is the LAN IP printed to the terminal.
func localNetworkIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "localhost"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}
	return "localhost"
}

// randomAlphanumeric returns a crypto/rand n-char lowercase alphanumeric string.
func randomAlphanumeric(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// randomHex returns a crypto/rand n-byte hex string.
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}