package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/cihann/juliterm/internal/protocol"
	"github.com/cihann/juliterm/internal/tmux"
)

// A client whose writer is stuck (no treeWriter draining it, as with a slow
// or half-open connection) must neither block the broadcaster nor keep other
// clients from getting the update.
func TestBroadcastSessionTree_StuckClientDoesNotBlock(t *testing.T) {
	tree := tmux.NewSessionTree()
	f := NewFanout(tree)

	stuck := newWSConn("stuck", nil, "")
	healthy := newWSConn("healthy", nil, "")
	f.RegisterConn(stuck)
	f.RegisterConn(healthy)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, name := range []string{"one", "two", "three"} {
			tree.UpdateSession(&tmux.Session{ID: "$" + name, Name: name})
			f.BroadcastSessionTree()
			// The healthy client keeps up: drain it each time.
			<-healthy.treeCh
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("BroadcastSessionTree blocked on a stuck client")
	}

	// The stuck client holds only the latest tree, not a backlog.
	if n := len(stuck.treeCh); n != 1 {
		t.Fatalf("stuck client has %d pending frames, want 1", n)
	}
	msgType, payload, _ := protocol.ParseMessage(<-stuck.treeCh)
	if msgType != protocol.MsgSessionTree {
		t.Fatalf("pending frame type = 0x%02x, want session tree", msgType)
	}
	if !bytes.Contains(payload, []byte(`"three"`)) {
		t.Fatalf("pending frame is not the latest tree: %s", payload)
	}
}
