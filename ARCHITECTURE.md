# Shelve — Architecture Document
### For AI Assistants: Read This Entire File Before Writing a Single Line of Code

> Every decision in this document has been deliberately chosen and reasoned.
> Do not substitute, upgrade, simplify, or add to any decision without explicit user confirmation.
> If something seems missing, re-read this document — it is intentional.

---

## What Shelve Is

Shelve is a **product** — not a script, not a demo, not a toy.

It has two distinct modes that ship as **one Go binary**:

### Mode 1 — `shelve` (CLI, installed by developers)
A developer runs `shelve` on their machine. The binary:
- Spawns a PTY-attached shell (bash/zsh)
- Starts a local WebSocket server
- Opens an outbound tunnel connection to the Shelve cloud server
- Prints two URLs to the terminal:
  - `Local:  http://192.168.x.x:7681?token=abc123` (same-WiFi access)
  - `Remote: https://shelve.sh/s/x9k2m` (globally accessible, proxied via cloud)
- Anyone with the remote URL gets a real terminal session into this machine

### Mode 2 — `shelve server` (deployed on cloud VPS)
The cloud server that:
- Serves the landing page at `shelve.sh`
- Acts as a tunnel relay — proxies WebSocket traffic between remote browsers and local `shelve` CLI instances
- Manages session IDs and routes traffic to the correct CLI instance

**This is the product story:** Run one command, get a shareable terminal link. Like ngrok, but for terminals.

> **Docker demo sessions are out of scope for V1.** The landing page links directly to the GitHub install instructions. Demo sessions via Docker containers will be added in V2 once the core tunnel system is stable and deployed.

---

## Why This Architecture, Not Alternatives

### Why not WebRTC
- WebRTC is designed for browser-to-browser P2P media. Terminal data is tiny text bytes — P2P gives zero meaningful latency benefit over a relay.
- `pion/webrtc` in Go adds 10MB+ to the binary and requires understanding ICE, SDP, DTLS, STUN, TURN — massive complexity for no user-visible gain.
- WebRTC NAT traversal fails on restrictive networks anyway and falls back to TURN relay — which you'd need to host separately.
- A signaling server (Node.js) + TURN server = two additional services to deploy and maintain. Our architecture has one.

### Why not Tailscale for remote access
- Requires the visitor to install Tailscale. Non-starter for a product where the value prop is "just click the link."
- Not controllable — you can't brand it, you can't add session timeouts, you can't build features on top of it.

### Why not ngrok
- ngrok is a dependency, not your product. You can't put ngrok on your resume as something you built.
- Building the tunnel relay yourself is the technically impressive part.

### Why no Docker demo sessions in V1
- Docker requires a VPS with Docker daemon — rules out free hosting tiers.
- The core product value (tunnel + shareable link) does not depend on demo sessions.
- Docker sandboxed sessions are the correct V2 addition once the tunnel is live and stable.
- Do not add Docker to this codebase until explicitly instructed.

---

## Full System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    shelve.sh (Cloud — any free tier)             │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Go Cloud Server                        │   │
│  │                                                          │   │
│  │  Routes:                                                 │   │
│  │  GET  /                → Landing page (embedded HTML)    │   │
│  │  WS   /tunnel/register → CLI registers tunnel here       │   │
│  │  GET  /s/:sessionId    → Session terminal UI (HTML)      │   │
│  │  WS   /s/:sessionId    → Browser connects to tunnel      │   │
│  │  GET  /install         → Redirect to install.sh          │   │
│  │                                                          │   │
│  │  Session Registry (in-memory map):                       │   │
│  │  sessionId → tunnelConn (WebSocket to CLI instance)      │   │
│  └──────────────────────────────────────────────────────────┘   │
│                           │                                      │
│                    ┌──────▼──────────────────┐                  │
│                    │   Caddy (reverse proxy)  │                  │
│                    │   - Auto TLS (HTTPS)     │                  │
│                    │   - WebSocket proxying   │                  │
│                    └─────────────────────────┘                  │
└─────────────────────────────────────────────────────────────────┘
                              ▲              ▲
                              │              │
              ┌───────────────┘              └──────────────┐
              │                                             │
┌─────────────▼──────────────┐              ┌──────────────▼──────┐
│  Developer's Machine        │              │   Browser (anyone)   │
│                             │              │                      │
│  shelve (CLI binary)        │              │  visits shelve.sh/s/ │
│  ├── PTY + bash             │              │  :sessionId          │
│  ├── Local WS server        │              │  xterm.js renders    │
│  └── Tunnel WS client  ─────┼──────────────┼─► /s/:sessionId WS  │
│      (outbound conn to      │              │                      │
│       shelve.sh/tunnel/     │              └──────────────────────┘
│       register)             │
└─────────────────────────────┘
```

---

## Tunnel System — How It Works Exactly

This is the most novel and technically impressive part. Understand it fully.

### Step 1 — CLI registers with cloud server
```
shelve binary starts
→ generates session ID (e.g. "x9k2m") — random 5-char alphanumeric
→ generates auth token (32-byte crypto/rand hex)
→ opens outbound WebSocket to: wss://shelve.sh/tunnel/register?session=x9k2m&token=abc123
→ cloud server stores: registry["x9k2m"] = thisWebSocketConn
→ CLI prints:
    Local:  http://192.168.1.5:7681?token=abc123
    Remote: https://shelve.sh/s/x9k2m
→ CLI holds this WebSocket open — it is the tunnel
```

### Step 2 — Browser connects via tunnel URL
```
Browser visits https://shelve.sh/s/x9k2m
→ cloud server serves terminal HTML page
→ browser opens WebSocket to wss://shelve.sh/s/x9k2m
→ cloud server looks up registry["x9k2m"] → finds CLI's tunnel WebSocket
→ cloud server now has TWO WebSocket connections:
    browserConn  (browser ↔ cloud)
    tunnelConn   (cloud ↔ CLI)
→ cloud server starts proxying bytes between them:
    browserConn.Read() → tunnelConn.Write()
    tunnelConn.Read()  → browserConn.Write()
```

### Step 3 — CLI receives proxied traffic
```
CLI's tunnel WebSocket receives browser's keystrokes
→ CLI writes keystrokes to PTY master (into bash)
→ bash output comes out of PTY master
→ CLI writes PTY output to tunnel WebSocket
→ cloud server proxies it to browser
→ browser's xterm.js renders the output
```

### The proxy is dumb by design
The cloud server does NOT understand terminal protocol.
It does NOT parse PTY data, resize messages, or any content.
It is a pure byte forwarder — reads from one WebSocket, writes to the other.
All terminal logic lives in the CLI binary and the browser's xterm.js.
This is the correct design. Do not add terminal awareness to the cloud server.

---

## Demo Session System — How It Works Exactly

For anonymous visitors who want to try Shelve without installing anything.

```
Browser visits shelve.sh/demo
→ Served the demo terminal HTML page
→ Browser opens WebSocket to wss://shelve.sh/demo/ws
→ Cloud server calls Docker SDK: create new alpine container
→ Cloud server exec's bash inside container with a PTY
→ Two goroutines start:
    goroutine A: Read PTY → Write to browserConn (shell output → browser)
    goroutine B: Read browserConn → Write to PTY (keystrokes → shell)
→ 10-minute session timer starts
→ On disconnect OR timeout: container is force-stopped and removed
```

Container spec:
- Image: `alpine:latest` with bash, vim, git, curl pre-installed
- No network access (--network none)
- Read-only filesystem except /tmp
- CPU limit: 0.5 cores
- Memory limit: 64MB
- Auto-removed on stop (--rm equivalent)

---

## Tech Stack — Final Decisions

### Backend (both CLI and cloud server — same binary, different subcommands)

| Decision | Choice | Reason |
|---|---|---|
| Language | Go (latest stable) | Single binary, goroutines for I/O, low memory, stable PTY library |
| PTY | `github.com/creack/pty` | Most stable Go PTY library, used by ttyd and others |
| WebSocket | `github.com/gorilla/websocket` | Battle-tested, explicit control over read/write |
| Docker SDK | `github.com/docker/docker/client` | Official Go SDK for Docker — programmatic container management |
| HTTP router | Go stdlib `net/http` + `github.com/go-chi/chi` | Chi adds route params (/s/:sessionId) without framework overhead |
| Config/flags | Go stdlib `flag` package | No external dependency needed |
| Session IDs | `crypto/rand` | Cryptographically random, not guessable |

**Why Chi router over stdlib mux:**
- Stdlib mux cannot do `/s/:sessionId` pattern matching with named params cleanly.
- Chi is minimal — it's just a router, no ORM, no middleware stack, no magic.
- Zero learning curve — it uses stdlib `http.Handler` interface exactly.

**Why not Gin, Echo, Fiber:**
- Overkill. This is not a REST API with 50 routes. Chi is enough.

### Frontend

| Decision | Choice | Reason |
|---|---|---|
| Framework | None — Vanilla JS | Single page, no state management needed, no build step |
| Terminal emulator | xterm.js v5 | VSCode uses it, handles all ANSI codes, has FitAddon |
| Styling | CSS custom properties + no framework | Full control, fast, no Tailwind build step needed |
| Served by | Go `embed.FS` | Everything ships in the binary |

Pages to build:
- `frontend/landing/index.html` — marketing landing page with install instructions
- `frontend/session/index.html` — tunnel session terminal (connects to /s/:id)

Both are embedded into the binary. No static file server needed at runtime.

### Infrastructure

| Decision | Choice | Reason |
|---|---|---|
| Hosting | Fly.io free tier OR Render free tier | No Docker needed — Go binary runs directly, persistent WebSocket supported |
| Reverse proxy | Caddy | Auto HTTPS with Let's Encrypt, WebSocket proxying, 3-line config |
| Process manager | systemd (VPS) or platform-native (Fly/Render) | Auto-restart on crash |
| Domain | shelve.sh (or chosen name) | Short, memorable, developer-facing TLD |
| CI/CD | GitHub Actions | Cross-compile + upload to GitHub Releases on git tag push |

**Free hosting options (no Docker required):**
- **Fly.io** — free tier supports persistent long-running processes and WebSocket connections. Deploy with `flyctl deploy`. Recommended.
- **Render** — free tier with always-on web services. WebSocket supported. Slightly slower cold start than Fly.
- **Railway** — free trial, then paid. Good DX but not permanently free.

**If budget is available later (V2 with Docker demo sessions):**
- Hetzner CX21 (€5.83/month) — cheapest VPS with enough RAM for Docker session isolation.

**Why Caddy over Nginx:**
- Nginx requires manual certbot setup, cron jobs for renewal, and 30+ lines of config for WebSocket proxying.
- Caddy config for this entire project is 4 lines:
  ```
  shelve.sh {
      reverse_proxy localhost:7681
  }
  ```
  That's it. TLS, HTTP/2, WebSocket proxying — all automatic.

---

## Project File Structure

```
shelve/
├── main.go                    # Entry point — subcommand routing (shelve vs shelve server)
├── cmd/
│   ├── local.go               # `shelve` subcommand — local PTY + tunnel client
│   └── server.go              # `shelve server` subcommand — cloud server entrypoint
├── internal/
│   ├── pty/
│   │   └── pty.go             # PTY creation, bash spawn, resize — pure PTY logic only
│   ├── tunnel/
│   │   ├── registry.go        # In-memory session registry (map + mutex)
│   │   ├── server.go          # Cloud-side tunnel: register + proxy handlers
│   │   └── client.go          # CLI-side tunnel: outbound WS connection to cloud
│   └── ws/
│       └── bridge.go          # Reusable WebSocket ↔ PTY bridge
├── frontend/
│   ├── landing/
│   │   └── index.html         # shelve.sh homepage with install instructions
│   └── session/
│       └── index.html         # Tunnel session terminal page
├── install.sh                 # curl | sh install script
├── Caddyfile                  # Production Caddy config (for VPS deployment)
├── shelve.service             # systemd service file (for VPS deployment)
├── fly.toml                   # Fly.io deployment config
├── .github/
│   └── workflows/
│       └── release.yml        # Cross-compile + upload to GitHub Releases on tag
└── go.mod
```

**Separation rules — enforce strictly:**
- `internal/pty/pty.go` — zero WebSocket imports. PTY only.
- `internal/tunnel/registry.go` — zero PTY imports. Just a thread-safe map of sessionId → WebSocket conn.
- `internal/tunnel/server.go` — tunnel registration and proxy handlers only.
- `internal/tunnel/client.go` — outbound tunnel WebSocket connection only.
- `internal/ws/bridge.go` — the reusable goroutine pair that bridges any PTY to any WebSocket.
- `cmd/local.go` and `cmd/server.go` wire these internals together. They are the only files allowed to import multiple internal packages.
- **No Docker imports anywhere in the codebase for V1.**

---

## The Two Goroutine Bridge — Core Pattern

Used everywhere in this project. Understand it once, apply it everywhere.

```go
// bridge.go — reusable WebSocket ↔ PTY bridge
// ptmx = PTY master file descriptor
// conn = gorilla WebSocket connection

func Bridge(ptmx *os.File, conn *websocket.Conn) {
    var once sync.Once
    cleanup := func() {
        ptmx.Close()
        conn.Close()
    }

    // Goroutine A: PTY output → Browser
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

    // Goroutine B: Browser keystrokes → PTY
    go func() {
        defer once.Do(cleanup)
        for {
            msgType, msg, err := conn.ReadMessage()
            if err != nil {
                return
            }
            if msgType == websocket.TextMessage {
                // Text messages are control messages (resize only)
                handleControlMessage(ptmx, msg)
            } else {
                // Binary messages are raw keystrokes
                ptmx.Write(msg)
            }
        }
    }()
}
```

**Non-negotiable rules for this pattern:**
- Use `sync.Once` for cleanup — ensures PTY and WebSocket are closed exactly once even if both goroutines error simultaneously.
- Use `websocket.TextMessage` for resize/control, `websocket.BinaryMessage` for raw PTY data. Never mix them.
- Buffer size 4096 bytes for PTY reads — standard, do not change.
- `gorilla/websocket` is NOT concurrent-safe for writes. Only one goroutine (A) ever calls `conn.WriteMessage`. Never call WriteMessage from goroutine B.
- Both goroutines must be started as goroutines (non-blocking). The caller does not wait for them.

---

## Session Registry — Tunnel Routing

```go
// registry.go
type Registry struct {
    mu       sync.RWMutex
    sessions map[string]*websocket.Conn // sessionId → CLI's tunnel WebSocket
}

func (r *Registry) Register(sessionId string, conn *websocket.Conn) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.sessions[sessionId] = conn
}

func (r *Registry) Get(sessionId string) (*websocket.Conn, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    conn, ok := r.sessions[sessionId]
    return conn, ok
}

func (r *Registry) Remove(sessionId string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.sessions, sessionId)
}
```

**Rules:**
- This is an in-memory map. It does NOT persist to disk or a database. If the server restarts, all active tunnel sessions are dropped — this is acceptable.
- Always use RWMutex (read lock for Get, write lock for Register/Remove).
- The registry is a singleton initialized once in server startup and passed to handlers. Do not use global variables.
- Session IDs are generated by the CLI, not the server. The server trusts the CLI's session ID.

---

## CLI Startup Sequence

```
1. Parse flags (--port, --shell, --no-tunnel)
2. Generate sessionId = random 5-char alphanumeric (crypto/rand)
3. Generate token = random 32-byte hex (crypto/rand)
4. Start local HTTP/WS server on --port (default 7681)
5. Detect local network IP (enumerate network interfaces, pick non-loopback IPv4)
6. If --no-tunnel flag NOT set:
   a. Connect outbound WebSocket to wss://shelve.sh/tunnel/register?session=<id>&token=<token>
   b. Wait for acknowledgement from server ("registered")
   c. Start goroutine to keep tunnel alive (ping every 30s)
7. Print to terminal:
   ✓ Shelve started

   Local:   http://192.168.1.5:7681?token=<token>
   Remote:  https://shelve.sh/s/<sessionId>

   Ctrl+C to stop
8. Block until Ctrl+C
9. On Ctrl+C: close tunnel WS, close local server, exit
```

---

## Cloud Server Startup Sequence

```
1. Initialize empty session registry
2. Initialize Docker client (connects to local Docker daemon via unix socket)
3. Pull alpine image if not present (docker pull alpine:latest)
4. Register HTTP routes:
   GET  /              → serve landing/index.html
   GET  /demo          → serve demo/index.html
   WS   /demo/ws       → demo session handler
   WS   /tunnel/register → tunnel registration handler
   GET  /s/:sessionId  → serve session/index.html
   WS   /s/:sessionId  → tunnel proxy handler
   GET  /install       → redirect to raw GitHub install.sh
5. Start HTTP server on :7681
6. Caddy handles TLS in front of this
```

---

## Frontend — xterm.js Rules

```javascript
// Correct xterm.js initialization — do not deviate from this
const term = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'JetBrains Mono, Menlo, Monaco, monospace',
    theme: {
        background: '#0d1117',  // GitHub dark background
        foreground: '#e6edf3',
    }
});

const fitAddon = new FitAddon.FitAddon();
term.loadAddon(fitAddon);
term.open(document.getElementById('terminal'));

// MUST call fit AFTER open AND after a layout tick
requestAnimationFrame(() => fitAddon.fit());

const ws = new WebSocket(`wss://${location.host}/demo/ws`);
ws.binaryType = 'arraybuffer'; // CRITICAL — never omit this

ws.onopen = () => {
    // Send initial size immediately on connect
    ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
};

ws.onmessage = (event) => {
    term.write(new Uint8Array(event.data)); // CRITICAL — always Uint8Array, never string
};

term.onData((data) => {
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(data); // raw binary — gorilla receives as BinaryMessage
    }
});

window.addEventListener('resize', () => {
    fitAddon.fit();
    if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
    }
});
```

**Non-negotiable frontend rules:**
- `ws.binaryType = 'arraybuffer'` — always, without exception.
- Write to xterm.js as `new Uint8Array(event.data)` — never as a string.
- Resize messages are JSON TextMessages. Keystrokes are binary BinaryMessages. Never mix.
- Always check `ws.readyState === WebSocket.OPEN` before sending.
- Call `fitAddon.fit()` inside `requestAnimationFrame` after `term.open()` — the div needs a layout pass first.
- Send initial resize on `ws.onopen` — the PTY needs to know the terminal dimensions immediately.

---

## Go embed — Frontend Embedding

```go
//go:embed frontend/landing/index.html frontend/session/index.html
var frontendFS embed.FS

// Serve landing page
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    f, _ := frontendFS.ReadFile("frontend/landing/index.html")
    w.Header().Set("Content-Type", "text/html")
    w.Write(f)
})
```

**Rules:**
- Both HTML files are embedded at compile time. Zero runtime file I/O.
- The `//go:embed` directive path is relative to the `.go` source file location.
- Never use `os.ReadFile` or relative paths to serve frontend files.

---

## CLI Flags

```
shelve [flags]
  --port     int     Local port to listen on (default: 7681)
  --shell    string  Shell to spawn (default: $SHELL, fallback: /bin/bash)
  --no-tunnel        Disable tunnel, local access only
  --once             Exit after first session disconnects

shelve server [flags]
  --port     int     Port to listen on (default: 7681)
```

---

## Install Script

```bash
#!/bin/sh
# install.sh — hosted at shelve.sh/install or raw GitHub
set -e

REPO="yourgithubusername/shelve"
BINARY="shelve"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

LATEST=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | cut -d'"' -f4)

URL="https://github.com/$REPO/releases/download/$LATEST/${BINARY}-${OS}-${ARCH}"
DEST="/usr/local/bin/$BINARY"

echo "Installing shelve $LATEST..."
curl -fsSL "$URL" -o "$DEST"
chmod +x "$DEST"
echo "✓ shelve installed to $DEST"
echo "  Run: shelve"
```

---

## GitHub Actions — Release Pipeline

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: 'stable'

      - name: Build all platforms
        run: |
          GOOS=linux  GOARCH=amd64 go build -o shelve-linux-amd64  .
          GOOS=linux  GOARCH=arm64 go build -o shelve-linux-arm64  .
          GOOS=darwin GOARCH=amd64 go build -o shelve-darwin-amd64 .
          GOOS=darwin GOARCH=arm64 go build -o shelve-darwin-arm64 .

      - name: Upload to GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: shelve-*
```

---

## Deployment Sequence — Fly.io (Free, Recommended)

```bash
# Install flyctl
curl -L https://fly.io/install.sh | sh

# From project root
fly launch           # creates fly.toml, picks region, sets up app
fly deploy           # builds and deploys the Go binary

# Done. shelve.sh (or your fly subdomain) is live with auto-HTTPS.
```

```toml
# fly.toml
app = "shelve"
primary_region = "sin"   # Singapore — closest to India

[build]
  builder = "paketobuildpacks/builder:base"

[http_service]
  internal_port = 7681
  force_https = true
  [http_service.concurrency]
    type = "connections"
    hard_limit = 100

[[vm]]
  memory = "256mb"
  cpu_kind = "shared"
  cpus = 1
```

**Optional: VPS deployment (if moving to paid/Docker in V2)**
```bash
# On any Ubuntu 22.04 VPS
apt install caddy
echo 'shelve.sh { reverse_proxy localhost:7681 }' > /etc/caddy/Caddyfile
systemctl restart caddy
scp shelve-linux-amd64 root@server:/usr/local/bin/shelve
scp shelve.service root@server:/etc/systemd/system/
systemctl enable --now shelve-server
```

```ini
# shelve.service
[Unit]
Description=Shelve Server
After=network.target

[Service]
ExecStart=/usr/local/bin/shelve server
Restart=always
RestartSec=5
User=shelve

[Install]
WantedBy=multi-user.target
```

---

## Security Threat Model

| Threat | Mitigation |
|---|---|
| Unauthorized tunnel access | Session token in WS URL, validated on register — no token = rejected before registry entry |
| Session ID enumeration | Session IDs are crypto/rand generated — 5-char alphanumeric = 916 million combinations |
| Tunnel traffic interception | All traffic goes through TLS (WSS via Caddy/Fly.io) — encrypted in transit |
| Server-side token logging | Token is in WS URL — ensure access logs are disabled or URL params stripped |
| MITM on CLI tunnel connection | CLI connects to wss:// (TLS) — certificate pinning is out of scope for V1 |
| Tunnel server crash | Fly.io auto-restarts the process — active sessions drop but reconnect is possible |

---

## Platform Support

| Platform | Mode | Status |
|---|---|---|
| Linux (amd64, arm64) | CLI + Server | ✅ Full support |
| macOS (amd64, arm64 M1/M2) | CLI only | ✅ Full support |
| Windows | CLI | ❌ v1 out of scope — requires ConPTY (`github.com/UserExistsError/conpty`) |

Windows support is architecturally possible via Go build tags (`//go:build !windows` / `//go:build windows`) but is deferred to a future release.

---

## Go Modules

```
module github.com/yourusername/shelve

go 1.22

require (
    github.com/creack/pty v1.1.21
    github.com/gorilla/websocket v1.5.1
    github.com/go-chi/chi/v5 v5.0.12
)
```

Run `go mod tidy` after initial setup.

---

## Known Gotchas — Every AI Must Read This

1. **Never create PTY before validating the auth token.** Token check happens on the HTTP upgrade request handler — before any resource is allocated.

2. **`io.Copy` cannot be used between WebSocket and PTY.** `gorilla/websocket` connections are not `io.Reader`/`io.Writer`. Use explicit `ReadMessage`/`WriteMessage` loops.

3. **`gorilla/websocket` is not concurrent-write-safe.** Only one goroutine may call `WriteMessage` on a connection at any time. In the bridge, goroutine A (PTY→WS) is the only writer. Never write from goroutine B.

4. **PTY output is raw bytes, not lines.** Do not buffer by newlines. Read chunks of up to 4096 bytes and forward immediately.

5. **`fitAddon.fit()` must run after a layout pass.** Call it inside `requestAnimationFrame()` after `term.open()`, not immediately.

6. **Always send initial resize after WebSocket connect.** The PTY starts with a default size (80x24). If the browser terminal is larger, content wraps incorrectly until the first resize message is sent.

7. **Tunnel proxy is a dumb byte forwarder.** Do not add terminal logic to the cloud server's proxy handler. It reads bytes from one WebSocket and writes them to another. That is all it does.

8. **Use `sync.Once` for bridge cleanup.** Both goroutines may error simultaneously. `sync.Once` ensures `ptmx.Close()` and `conn.Close()` are called exactly once.

9. **Fly.io WebSocket connections stay alive** but Fly may impose a 1-hour idle timeout. The CLI tunnel client must send WebSocket ping frames every 30 seconds to keep the connection alive.

10. **Session registry is in-memory only.** If the cloud server restarts, all registered tunnel sessions are lost and the CLI must reconnect. This is acceptable — the CLI detects the dropped connection and can reconnect automatically.

---

## What Is Explicitly Out of Scope for V1

Do not implement these. Do not suggest these. Do not ask about these.

- ❌ Docker demo sessions (V2 feature — requires paid VPS)
- ❌ Session recording or replay
- ❌ Read-only shareable view of a tunnel session
- ❌ Multiple users / authentication accounts
- ❌ Database of any kind (SQLite, PostgreSQL, Redis)
- ❌ Windows support (ConPTY)
- ❌ Mobile virtual keyboard row (Ctrl, Tab, arrows as touch buttons)
- ❌ Shell picker (only bash/zsh from $SHELL env var)
- ❌ Process picker (attach to existing process)
- ❌ TLS termination inside the Go binary (Caddy/Fly.io handles this)
- ❌ Rate limiting
- ❌ Metrics / observability
- ❌ React, Vue, Svelte, or any JS framework
- ❌ Tailwind CSS or any CSS framework

---

*End of architecture document.*
*Build in this order: (1) local PTY + WS bridge, (2) tunnel system, (3) landing page + session UI, (4) install script + CI, (5) deploy to Fly.io.*
