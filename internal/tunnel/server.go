package tunnel

import (
	"log"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// Allow all origins — TLS is handled by Caddy/Fly.io in front of this.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// RegisterHandler handles: WS /tunnel/register?session=<id>&token=<token>
//
// The CLI calls this on startup. We:
//  1. Validate session + token params are non-empty (reject before allocating anything)
//  2. Upgrade to WebSocket
//  3. Store the connection in the registry
//  4. Send "registered" ack to the CLI
//  5. Block until the session ends (browser disconnects and ProxyHandler cleans up)
//  6. Remove from registry and close the tunnel connection
//
// This goroutine is the SOLE READER of the tunnel WebSocket connection.
// ProxyHandler only WRITES to it — gorilla/websocket is not concurrent-read-safe.
func RegisterHandler(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.URL.Query().Get("session")
		token := r.URL.Query().Get("token")

		// Reject before any resource is allocated — arch doc gotcha #1
		if sessionID == "" || token == "" {
			http.Error(w, "missing session or token", http.StatusBadRequest)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("tunnel register upgrade error:", err)
			return
		}

		// Store tunnel connection — ProxyHandler will look this up
		session := reg.Register(sessionID, conn)

		// Acknowledge registration to CLI
		if err := conn.WriteMessage(websocket.TextMessage, []byte("registered")); err != nil {
			log.Println("tunnel register ack error:", err)
			reg.Remove(sessionID)
			conn.Close()
			return
		}

		log.Printf("tunnel: session %s registered", sessionID)

		// Block here — this goroutine owns the tunnel conn.
		// ProxyHandler signals session.done when the browser disconnects,
		// which unblocks Wait() and lets us clean up.
		session.Wait()

		reg.Remove(sessionID)
		conn.Close()
		log.Printf("tunnel: session %s closed", sessionID)
	}
}

// ProxyHandler handles: WS /s/:sessionId
//
// The browser connects here. We:
//  1. Look up the CLI's tunnel connection in the registry
//  2. Upgrade the browser connection
//  3. Start two goroutines that forward bytes in both directions
//  4. On either side closing, clean up both connections
//
// PROXY IS DUMB BY DESIGN (arch doc rule):
// We do NOT parse PTY data, resize messages, or any terminal content.
// We forward raw bytes from one WebSocket to the other. That is all.
//
// Concurrency safety:
//   - Goroutine A: reads browserConn, writes tunnelConn  (sole writer of tunnelConn)
//   - Goroutine B: reads tunnelConn,  writes browserConn (sole writer of browserConn)
//   Each connection has exactly one reader and one writer — gorilla/websocket safe.
func ProxyHandler(reg *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionId")

		session, ok := reg.Get(sessionID)
		if !ok {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		browserConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("tunnel proxy upgrade error:", err)
			return
		}

		tunnelConn := session.Conn

		var once sync.Once
		cleanup := func() {
			browserConn.Close()
			// tunnelConn.Close() is done by RegisterHandler after session.Wait() returns
			session.close() // unblocks RegisterHandler's Wait()
		}

		// Goroutine A: browser → CLI
		// Sole writer to tunnelConn.
		go func() {
			defer once.Do(cleanup)
			for {
				msgType, msg, err := browserConn.ReadMessage()
				if err != nil {
					return
				}
				if err := tunnelConn.WriteMessage(msgType, msg); err != nil {
					return
				}
			}
		}()

		// Goroutine B: CLI → browser
		// Sole reader of tunnelConn. Sole writer to browserConn.
		go func() {
			defer once.Do(cleanup)
			for {
				msgType, msg, err := tunnelConn.ReadMessage()
				if err != nil {
					return
				}
				if err := browserConn.WriteMessage(msgType, msg); err != nil {
					return
				}
			}
		}()
	}
}