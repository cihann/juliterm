package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os/exec"
	"sync"
	"time"

	julipty "github.com/cihann/juliterm/internal/pty"
	"github.com/cihann/juliterm/internal/protocol"
	"github.com/coder/websocket"
)

// maxReadBytes caps a single client frame (large pastes arrive as one frame).
const maxReadBytes = 4 << 20

// Keepalive timing. A ping detects half-open connections (laptop sleep,
// network switch) that would otherwise hang forever, and the heartbeat frame
// lets the browser detect the same thing, since it cannot see pings. Both also
// keep proxies such as Cloudflare from dropping an idle connection.
var (
	keepAliveInterval = 25 * time.Second
	pingTimeout       = 10 * time.Second
)

// wsConn wraps a single WebSocket connection with its PTY session.
type wsConn struct {
	id            string
	conn          *websocket.Conn
	authSessionID string             // auth session ID from cookie (for revocation)
	sessionID     string             // tmux session this conn is attached to
	pty           *julipty.Session
	ptyCancel     context.CancelFunc // cancels forwardPTYOutput goroutine
	ptyDone       chan struct{}      // closed when forwardPTYOutput has exited
	cols, rows    uint16             // last size the client reported, 0 if none yet
	notAttachedAt time.Time          // last not_attached error sent (rate limit)
	treeCh        chan []byte        // latest pending session tree frame (size 1)
	writeMu       sync.Mutex
}

// newWSConn creates a wsConn for an accepted connection.
func newWSConn(id string, conn *websocket.Conn, authSessionID string) *wsConn {
	return &wsConn{
		id:            id,
		conn:          conn,
		authSessionID: authSessionID,
		treeCh:        make(chan []byte, 1),
	}
}

// offerTree queues a session tree frame for this connection without ever
// blocking: a pending frame that hasn't been written yet is replaced, since
// only the latest tree matters.
func (wc *wsConn) offerTree(msg []byte) {
	for {
		select {
		case wc.treeCh <- msg:
			return
		default:
		}
		// Full: drop the stale pending frame and try again.
		select {
		case <-wc.treeCh:
		default:
		}
	}
}

// treeWriter writes queued session tree frames until ctx ends or a write
// fails. It runs in its own goroutine so a slow client only delays its own
// updates, never the broadcaster.
func (wc *wsConn) treeWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-wc.treeCh:
			if err := wc.writeMessage(msg); err != nil {
				return
			}
		}
	}
}

// writeMessage sends a binary message over the WebSocket.
func (wc *wsConn) writeMessage(data []byte) error {
	wc.writeMu.Lock()
	defer wc.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return wc.conn.Write(ctx, websocket.MessageBinary, data)
}

// wsAcceptOptions returns the WebSocket accept options with the current origin
// allowlist. When no extra origins are configured, it returns nil which makes
// coder/websocket enforce same-origin (Origin must match Host). When extra
// origins are set (e.g. a Cloudflare tunnel host), those are allowed alongside
// same-origin.
func (s *Server) wsAcceptOptions() *websocket.AcceptOptions {
	s.allowedOriginsMu.RLock()
	defer s.allowedOriginsMu.RUnlock()
	if len(s.allowedOrigins) == 0 {
		return nil
	}
	origins := make([]string, len(s.allowedOrigins))
	copy(origins, s.allowedOrigins)
	return &websocket.AcceptOptions{OriginPatterns: origins}
}

// handleWebSocket upgrades an HTTP request to a WebSocket connection,
// waits for a session selection, spawns a PTY, and bridges I/O.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	authID, ok := s.validateSession(r)
	if !ok {
		log.Printf("ws: rejected unauthorized connection")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, s.wsAcceptOptions())
	if err != nil {
		log.Printf("ws: accept failed: %v", err)
		return
	}
	// coder/websocket defaults to a 32 KiB read limit; xterm sends a paste as a
	// single input frame, so anything larger would kill the connection.
	conn.SetReadLimit(maxReadBytes)

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		conn.Close(websocket.StatusInternalError, "failed to generate connection id")
		return
	}

	wc := newWSConn(hex.EncodeToString(idBytes), conn, authID)

	s.fanout.RegisterConn(wc)
	log.Printf("ws[%s]: connected", wc.id)
	defer func() {
		log.Printf("ws[%s]: disconnected", wc.id)
		s.fanout.UnregisterConn(wc)
		wc.detachPTY()
		conn.CloseNow()
	}()

	// Send the initial session tree.
	if treeJSON, err := s.tmuxMgr.Tree().ToJSON(); err == nil {
		log.Printf("ws[%s]: sent initial session tree (%d sessions)", wc.id, len(s.tmuxMgr.Tree().GetSessions()))
		_ = wc.writeMessage(protocol.NewSessionTreeMessage(treeJSON))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wc.keepAlive(ctx)
	go wc.treeWriter(ctx)

	// Read loop — handle messages from the browser.
	s.readLoop(ctx, wc)
}

// keepAlive pings the client and sends a heartbeat frame every
// keepAliveInterval until ctx ends. A failed ping closes the connection, which
// ends the read loop.
func (wc *wsConn) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
		err := wc.conn.Ping(pingCtx)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("ws[%s]: ping failed, closing: %v", wc.id, err)
				wc.conn.CloseNow()
			}
			return
		}
		if err := wc.writeMessage(protocol.NewHeartbeatMessage()); err != nil {
			return
		}
	}
}

// readLoop reads binary messages from the WebSocket and dispatches them.
func (s *Server) readLoop(ctx context.Context, wc *wsConn) {
	for {
		_, data, err := wc.conn.Read(ctx)
		if err != nil {
			return
		}

		msgType, payload, err := protocol.ParseMessage(data)
		if err != nil {
			log.Printf("ws[%s]: parse error: %v", wc.id, err)
			continue
		}

		switch msgType {
		case protocol.MsgInput:
			s.handleInput(wc, payload)
		case protocol.MsgResize:
			s.handleResize(wc, payload)
		case protocol.MsgSessionTree:
			// Client sends session tree message to select/switch session.
			s.handleSessionSelect(wc, payload)
		case protocol.MsgSelectWindow:
			s.handleSelectWindow(wc, payload)
		default:
			log.Printf("ws[%s]: unknown message type 0x%02x", wc.id, msgType)
		}
	}
}

// handleSessionSelect processes a session selection from the client.
// The client sends MsgSessionTree with JSON {"sessionName": "...", "cols": N,
// "rows": N}; cols and rows are optional.
func (s *Server) handleSessionSelect(wc *wsConn, payload []byte) {
	var sel struct {
		SessionName string `json:"sessionName"`
		Cols        int    `json:"cols"`
		Rows        int    `json:"rows"`
	}
	if err := json.Unmarshal(payload, &sel); err != nil || sel.SessionName == "" {
		_ = wc.writeMessage(protocol.NewErrorMessage("bad_select", "invalid session selection"))
		return
	}

	// Detach from the current session first. detachPTY waits for the old
	// output forwarder to exit, so none of the old session's output can reach
	// the browser after the new session starts.
	wc.detachPTY()

	// Spawn a PTY attached to the requested tmux session at the client's real
	// size: attaching at a placeholder size and resizing a moment later makes
	// tmux reflow the shared window twice, garbling full-screen apps.
	if validSize(sel.Cols, sel.Rows) {
		wc.cols, wc.rows = uint16(sel.Cols), uint16(sel.Rows)
	}
	cols, rows := wc.cols, wc.rows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	log.Printf("ws[%s]: attaching to session %q at %dx%d", wc.id, sel.SessionName, cols, rows)
	ps, err := julipty.Attach(s.tmuxMgr.TmuxPath(), sel.SessionName, cols, rows)
	if err != nil {
		log.Printf("ws[%s]: pty attach to %q failed: %v", wc.id, sel.SessionName, err)
		_ = wc.writeMessage(protocol.NewErrorMessage("attach_error", err.Error()))
		return
	}
	log.Printf("ws[%s]: attached to session %q, streaming output", wc.id, sel.SessionName)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	wc.pty = ps
	wc.ptyCancel = cancel
	wc.ptyDone = done
	wc.sessionID = sel.SessionName

	// Start forwarding PTY output to WebSocket.
	go func() {
		defer close(done)
		s.forwardPTYOutput(ctx, wc, ps)
	}()
}

// detachPTY stops the output forwarder, closes the PTY and waits for the
// forwarder to exit. It is a no-op when nothing is attached. Only the read
// loop goroutine (and the connection's cleanup) may call it.
func (wc *wsConn) detachPTY() {
	if wc.ptyCancel != nil {
		wc.ptyCancel()
	}
	if wc.pty != nil {
		wc.pty.Close()
	}
	if wc.ptyDone != nil {
		<-wc.ptyDone
	}
	wc.pty = nil
	wc.ptyCancel = nil
	wc.ptyDone = nil
	wc.sessionID = ""
}

// handleSelectWindow switches the active window in the attached tmux session.
// The client sends MsgSelectWindow with JSON {"sessionName": "...", "windowIndex": N}.
func (s *Server) handleSelectWindow(wc *wsConn, payload []byte) {
	var sel struct {
		SessionName string `json:"sessionName"`
		WindowIndex int    `json:"windowIndex"`
	}
	if err := json.Unmarshal(payload, &sel); err != nil || sel.SessionName == "" {
		return
	}

	target := fmt.Sprintf("%s:%d", sel.SessionName, sel.WindowIndex)
	cmd := exec.Command(s.tmuxMgr.TmuxPath(), "select-window", "-t", target)
	if err := cmd.Run(); err != nil {
		log.Printf("ws[%s]: select-window %q failed: %v", wc.id, target, err)
	}
}

// forwardPTYOutput reads from the PTY and sends MsgOutput frames to the browser.
// It exits when the PTY read returns an error (fd closed) or the context is cancelled.
func (s *Server) forwardPTYOutput(ctx context.Context, wc *wsConn, ps *julipty.Session) {
	buf := make([]byte, 32*1024)
	for {
		n, err := ps.Read(buf)
		// A chunk read just before a session switch belongs to the old
		// session; drop it rather than draw it into the new one.
		if ctx.Err() != nil {
			return
		}
		if n > 0 {
			msg := protocol.NewMessage(protocol.MsgOutput, buf[:n])
			if writeErr := wc.writeMessage(msg); writeErr != nil {
				return
			}
		}
		if err != nil {
			// Only log unexpected errors — not expected closes or EOF
			if err != io.EOF && ctx.Err() == nil {
				log.Printf("ws[%s]: pty read error: %v", wc.id, err)
			}
			// tmux exited on its own (session killed or detached): release the
			// PTY fd now rather than at the next switch or disconnect.
			_ = ps.Close()
			// Only notify client if this wasn't an intentional close (session switch)
			if ctx.Err() == nil {
				_ = wc.writeMessage(protocol.NewErrorMessage("session_ended", "tmux session ended"))
			}
			return
		}
	}
}

// handleInput writes raw keyboard bytes directly to the PTY. Input that
// arrives with no PTY attached gets a not_attached error, at most once a
// second, so the client can re-select its session instead of typing into
// nothing.
func (s *Server) handleInput(wc *wsConn, payload []byte) {
	if len(payload) == 0 {
		return
	}
	if wc.pty == nil {
		if time.Since(wc.notAttachedAt) >= time.Second {
			wc.notAttachedAt = time.Now()
			_ = wc.writeMessage(protocol.NewErrorMessage("not_attached", "no session attached"))
		}
		return
	}
	if _, err := wc.pty.Write(payload); err != nil {
		log.Printf("ws[%s]: pty write error: %v", wc.id, err)
	}
}

// handleResize changes the PTY window size. The size is remembered even
// before a PTY exists, so the next attach starts at the right size.
func (s *Server) handleResize(wc *wsConn, payload []byte) {
	var r protocol.ResizePayload
	if err := json.Unmarshal(payload, &r); err != nil {
		return
	}
	if !validSize(r.Cols, r.Rows) {
		return
	}
	wc.cols, wc.rows = uint16(r.Cols), uint16(r.Rows)
	if wc.pty == nil {
		return
	}
	if err := wc.pty.Resize(uint16(r.Cols), uint16(r.Rows)); err != nil {
		log.Printf("ws[%s]: pty resize to %dx%d: %v", wc.id, r.Cols, r.Rows, err)
	}
}

// validSize reports whether cols and rows are a usable terminal size.
func validSize(cols, rows int) bool {
	return cols > 0 && rows > 0 && cols <= math.MaxUint16 && rows <= math.MaxUint16
}
