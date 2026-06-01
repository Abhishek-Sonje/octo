package tunnel

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CloudServer is the URL of the octo relay server.
// Override this before calling Connect for local development:
//
//	tunnel.CloudServer = "ws://localhost:8080"
var CloudServer = getEnvOrDefault("OCTO_CLOUD_URL", "wss://octo.sh")

func getEnvOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// Connect dials the cloud relay server, registers the session, then
// bridges the tunnel WebSocket ↔ PTY master (ptmx).
//
// This runs the CLI side of the tunnel:
//  1. Dial wss://octo.sh/tunnel/register?session=<id>&token=<token>
//  2. Wait for "registered" acknowledgement from server
//  3. Start ping goroutine — sends a ping every 30s to prevent Fly.io
//     from closing idle WebSocket connections (arch doc gotcha #9)
//  4. Bridge: PTY output → tunnel WS, tunnel WS input → PTY
//
// Connect blocks until the tunnel closes (browser disconnects or server dies).
// The caller should run it in a goroutine.
//
// ptmx is a stdlib *os.File — client.go does NOT import internal/pty.
// The caller (cmd/local.go) spawns the PTY and passes the master fd here.
func Connect(sessionID, token string, ptmx *os.File) error {
	u, err := buildURL(sessionID, token)
	if err != nil {
		return fmt.Errorf("tunnel: invalid server URL: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		return fmt.Errorf("tunnel: dial %s: %w", u, err)
	}
	defer conn.Close()

	// ── Step 2: Wait for "registered" ack ───────────────────────────────────
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("tunnel: waiting for ack: %w", err)
	}
	if msgType != websocket.TextMessage || string(msg) != "registered" {
		return fmt.Errorf("tunnel: unexpected ack: %q", msg)
	}
	log.Printf("tunnel: registered as session %s", sessionID)

	// ── Step 3: Ping goroutine — keep Fly.io connection alive ────────────────
	// gorilla/websocket requires serialised writes on a single connection.
	// The ping goroutine and the PTY→WS goroutine both write to conn,
	// so we protect all writes with writeMu.
	var writeMu sync.Mutex

	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteControl(
					websocket.PingMessage,
					[]byte{},
					time.Now().Add(10*time.Second),
				)
				writeMu.Unlock()
				if err != nil {
					log.Println("tunnel: ping error:", err)
					return
				}
			case <-done:
				return
			}
		}
	}()

	// ── Step 4a: PTY output → tunnel WebSocket (shell output → browser) ──────
	// Sole writer to conn (serialised via writeMu alongside ping goroutine).
	go func() {
		defer close(done) // stop ping goroutine when PTY closes
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return // PTY closed — shell exited
			}
			writeMu.Lock()
			err = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// ── Step 4b: tunnel WebSocket → PTY (browser keystrokes → shell) ─────────
	// Sole reader of conn.
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("tunnel: connection closed: %w", err)
		}
		if _, err := ptmx.Write(msg); err != nil {
			return fmt.Errorf("tunnel: pty write: %w", err)
		}
	}
}

// buildURL constructs the WebSocket registration URL.
func buildURL(sessionID, token string) (string, error) {
	base, err := url.Parse(CloudServer)
	if err != nil {
		return "", err
	}
	base.Path = "/tunnel/register"
	q := base.Query()
	q.Set("session", sessionID)
	q.Set("token", token)
	base.RawQuery = q.Encode()
	return base.String(), nil
}