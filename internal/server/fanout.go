package server

import (
	"log"
	"sync"

	"github.com/cihann/juliterm/internal/protocol"
	"github.com/cihann/juliterm/internal/tmux"
	"github.com/coder/websocket"
)

// Fanout tracks connected WebSocket clients and broadcasts session tree
// updates to all of them. With the PTY-based architecture, per-pane output
// routing is no longer needed — each client has its own PTY.
type Fanout struct {
	mu    sync.RWMutex
	conns map[string]*wsConn // all connected clients
	tree  *tmux.SessionTree
}

// NewFanout creates a Fanout bound to the given SessionTree.
func NewFanout(tree *tmux.SessionTree) *Fanout {
	return &Fanout{
		conns: make(map[string]*wsConn),
		tree:  tree,
	}
}

// RegisterConn adds a WebSocket connection to the global connection map.
func (f *Fanout) RegisterConn(wc *wsConn) {
	f.mu.Lock()
	f.conns[wc.id] = wc
	f.mu.Unlock()
}

// UnregisterConn removes a connection.
func (f *Fanout) UnregisterConn(wc *wsConn) {
	f.mu.Lock()
	delete(f.conns, wc.id)
	f.mu.Unlock()
}

// BroadcastSessionTree queues the current session tree for every connected
// client and returns without waiting for any write. It is called from tmux
// control-mode read loops, the metadata poller and HTTP handlers, none of
// which may stall on a slow or half-open client.
func (f *Fanout) BroadcastSessionTree() {
	treeJSON, err := f.tree.ToJSON()
	if err != nil {
		log.Printf("fanout: failed to serialize session tree: %v", err)
		return
	}
	msg := protocol.NewSessionTreeMessage(treeJSON)

	f.mu.RLock()
	conns := make([]*wsConn, 0, len(f.conns))
	for _, wc := range f.conns {
		conns = append(conns, wc)
	}
	f.mu.RUnlock()

	for _, wc := range conns {
		wc.offerTree(msg)
	}
}

// CloseByAuthSession closes all WebSocket connections that belong to the given
// authentication session ID. It sends close code 4001 ("session_revoked") so
// the frontend can distinguish revocation from network errors.
func (f *Fanout) CloseByAuthSession(authSessionID string) {
	f.mu.RLock()
	var targets []*wsConn
	for _, wc := range f.conns {
		if wc.authSessionID == authSessionID {
			targets = append(targets, wc)
		}
	}
	f.mu.RUnlock()

	for _, wc := range targets {
		log.Printf("fanout: closing ws[%s] (auth session revoked)", wc.id)
		wc.conn.Close(websocket.StatusCode(4001), "session_revoked")
	}
}
