package ws

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// resizeMsg is the JSON control message sent by the browser on window resize.
type resizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Bridge pipes data between a PTY master fd and a WebSocket connection.
//
// Two goroutines run concurrently:
//   Goroutine A — PTY master → WebSocket (shell output → browser)
//   Goroutine B — WebSocket  → PTY master (keystrokes → shell)
//
// sync.Once guarantees ptmx and conn are closed exactly once,
// even if both goroutines error at the same time.
//
// onDone is an optional callback called when the session ends (used for --once).
//
// Message type contract (must match frontend):
//   websocket.BinaryMessage → raw keystrokes, written directly to PTY
//   websocket.TextMessage   → JSON control message (resize only)
func Bridge(ptmx *os.File, conn *websocket.Conn, onDone ...func()) {
	var once sync.Once
	cleanup := func() {
		ptmx.Close()
		conn.Close()
		if len(onDone) > 0 && onDone[0] != nil {
			onDone[0]()
		}
	}

	// Goroutine A: PTY output → Browser
	// This is the ONLY goroutine that calls conn.WriteMessage.
	// gorilla/websocket is NOT concurrent-write-safe — never write from goroutine B.
	go func() {
		defer once.Do(cleanup)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// Goroutine B: Browser → PTY
	// Reads keystrokes and resize events. Never calls conn.WriteMessage.
	go func() {
		defer once.Do(cleanup)
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				// Control message — only resize is supported in V1
				var r resizeMsg
				if err := json.Unmarshal(msg, &r); err == nil && r.Type == "resize" {
					pty.Setsize(ptmx, &pty.Winsize{Rows: r.Rows, Cols: r.Cols})
				}
			} else {
				// Binary message — raw keystrokes, write straight to shell
				ptmx.Write(msg)
			}
		}
	}()
}