// Premium Theme Config
const term = new Terminal({
  cursorBlink: true,
  fontSize: 14,
  fontFamily: 'JetBrains Mono, Menlo, Monaco, monospace',
  theme: { 
    background: '#191816', 
    foreground: '#fbfaf7',
    cursor: '#d97706',
    cursorAccent: '#191816',
    selectionBackground: 'rgba(217, 119, 6, 0.3)'
  }
});

const fitAddon = new FitAddon.FitAddon();
term.loadAddon(fitAddon);
term.open(document.getElementById('terminal'));
requestAnimationFrame(() => fitAddon.fit());

// Status UI Elements
const statusDot = document.getElementById('status-dot');
const statusText = document.getElementById('status-text');

// Link Copier
function copySessionLink() {
  navigator.clipboard.writeText(window.location.href).then(() => {
    const btn = document.getElementById('copy-btn');
    const originalText = btn.innerText;
    btn.innerText = 'Copied';
    btn.style.borderColor = 'var(--accent)';
    btn.style.color = 'var(--accent)';
    setTimeout(() => {
      btn.innerText = originalText;
      btn.style.borderColor = '';
      btn.style.color = '';
    }, 2000);
  });
}

// ── WebSocket Endpoint Logic ──────────────────────────
const proto = location.protocol === 'https:' ? 'wss' : 'ws';
const config = window.OCTO_SESSION || {};

let wsURL;
if (config.sessionId && config.sessionId !== "" && config.sessionId !== "local") {
  wsURL = `${proto}://${location.host}/s/${config.sessionId}`;
  document.title = `octo — ${config.sessionId}`;
} else {
  const token = config.token || '';
  wsURL = `${proto}://${location.host}/ws?token=${token}`;
  document.title = `octo — local`;
}

const ws = new WebSocket(wsURL);
ws.binaryType = 'arraybuffer'; // CRITICAL: Required for xterm

ws.onopen = () => {
  statusDot.className = 'status-dot connected';
  statusText.innerText = 'Connected';
  ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
};

ws.onmessage = (event) => {
  term.write(new Uint8Array(event.data));
};

ws.onclose = () => {
  statusDot.className = 'status-dot disconnected';
  statusText.innerText = 'Session Closed';
  term.writeln('\r\n\x1b[38;5;208m[octo] Session disconnected cleanly.\x1b[0m');
};

ws.onerror = () => {
  statusDot.className = 'status-dot disconnected';
  statusText.innerText = 'Connection Error';
  term.writeln('\r\n\x1b[31m[octo] Connection error occurred.\x1b[0m');
};

term.onData((data) => {
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(new TextEncoder().encode(data)); // Send as binary (Uint8Array)
  }
});

window.addEventListener('resize', () => {
  fitAddon.fit();
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
  }
});
