package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cihann/juliterm/internal/protocol"
	"github.com/cihann/juliterm/internal/tmux"
	"github.com/coder/websocket"
)

// dialWS attempts a WebSocket connection to the given httptest.Server,
// optionally setting the Origin header. It returns the connection or an error.
func dialWS(t *testing.T, ts *httptest.Server, cookie, origin string) (*websocket.Conn, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{},
	}
	if cookie != "" {
		opts.HTTPHeader.Set("Cookie", cookieName+"="+cookie)
	}
	if origin != "" {
		opts.HTTPHeader.Set("Origin", origin)
	}
	conn, _, err := websocket.Dial(context.Background(), url, opts)
	return conn, err
}

func TestWebSocket_SameOriginAccepted(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	cookie := doLogin(t, s, "test")

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Same-origin: Origin matches the test server's host.
	conn, err := dialWS(t, ts, cookie, ts.URL)
	if err != nil {
		t.Fatalf("same-origin WS dial failed: %v", err)
	}
	conn.CloseNow()
}

func TestWebSocket_CrossOriginRejected(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	cookie := doLogin(t, s, "test")

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Cross-origin: Origin does NOT match the test server's host.
	conn, err := dialWS(t, ts, cookie, "http://evil.com")
	if err == nil {
		conn.CloseNow()
		t.Fatal("cross-origin WS dial should have been rejected, but succeeded")
	}
}

func TestWebSocket_AllowOriginAccepted(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	cookie := doLogin(t, s, "test")

	// Allow a specific tunnel origin.
	s.AllowOrigin("tunnel.trycloudflare.com")

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Allowed origin should be accepted.
	conn, err := dialWS(t, ts, cookie, "https://tunnel.trycloudflare.com")
	if err != nil {
		t.Fatalf("allowed-origin WS dial failed: %v", err)
	}
	conn.CloseNow()
}

func TestWebSocket_AllowOriginStillRejectsCrossOrigin(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	cookie := doLogin(t, s, "test")

	// Allow a specific tunnel origin.
	s.AllowOrigin("tunnel.trycloudflare.com")

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// A different cross-origin should still be rejected.
	conn, err := dialWS(t, ts, cookie, "http://evil.com")
	if err == nil {
		conn.CloseNow()
		t.Fatal("cross-origin WS dial should have been rejected even with AllowOrigin set")
	}
}

func TestWebSocket_NoAuthRejected(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// No cookie — should be rejected before origin check.
	conn, err := dialWS(t, ts, "", ts.URL)
	if err == nil {
		conn.CloseNow()
		t.Fatal("unauthenticated WS dial should have been rejected")
	}
}

// openTestWS starts a test server, logs in and dials /ws. The caller must call
// the returned cleanup function.
func openTestWS(t *testing.T) (*Server, *websocket.Conn, func()) {
	t.Helper()
	mgr := newMockManager()
	s := newTestServer(mgr)
	cookie := doLogin(t, s, "test")

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	ts := httptest.NewServer(mux)

	conn, err := dialWS(t, ts, cookie, ts.URL)
	if err != nil {
		ts.Close()
		t.Fatalf("WS dial failed: %v", err)
	}
	return s, conn, func() {
		conn.CloseNow()
		ts.Close()
	}
}

// drainWS reads and discards frames until the connection or ctx ends, so
// control frames such as pongs are processed.
func drainWS(ctx context.Context, conn *websocket.Conn) {
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

func TestWebSocket_LargeInputFrameKeepsConnection(t *testing.T) {
	_, conn, cleanup := openTestWS(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A 100 KiB paste arrives as one input frame — above coder/websocket's
	// default 32 KiB read limit.
	paste := make([]byte, 100*1024)
	for i := range paste {
		paste[i] = 'a'
	}
	if err := conn.Write(ctx, websocket.MessageBinary, protocol.NewMessage(protocol.MsgInput, paste)); err != nil {
		t.Fatalf("write large frame: %v", err)
	}

	// Ping needs a concurrent reader on our side; the server must still answer.
	go drainWS(ctx, conn)
	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("connection closed after large frame: %v", err)
	}
}

func TestWebSocket_SendsHeartbeat(t *testing.T) {
	defer func(interval time.Duration) { keepAliveInterval = interval }(keepAliveInterval)
	keepAliveInterval = 20 * time.Millisecond

	_, conn, cleanup := openTestWS(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no heartbeat before error: %v", err)
		}
		if msgType, _, _ := protocol.ParseMessage(data); msgType == protocol.MsgHeartbeat {
			return
		}
	}
}

func TestHandleResize_RemembersSizeWithoutPTY(t *testing.T) {
	s := newTestServer(newMockManager())
	wc := &wsConn{id: "test"}

	s.handleResize(wc, []byte(`{"cols":132,"rows":43}`))
	if wc.cols != 132 || wc.rows != 43 {
		t.Fatalf("size = %dx%d, want 132x43", wc.cols, wc.rows)
	}

	// Invalid sizes are ignored and keep the last good one.
	for _, payload := range []string{`{"cols":0,"rows":10}`, `{"cols":10,"rows":-1}`, `{"cols":70000,"rows":10}`, `not json`} {
		s.handleResize(wc, []byte(payload))
		if wc.cols != 132 || wc.rows != 43 {
			t.Fatalf("after %s: size = %dx%d, want 132x43", payload, wc.cols, wc.rows)
		}
	}
}

func TestWebSocket_InputWithoutSessionReportsNotAttached(t *testing.T) {
	_, conn, cleanup := openTestWS(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Two keystrokes in quick succession: only one error (rate limited).
	for _, key := range []string{"a", "b"} {
		if err := conn.Write(ctx, websocket.MessageBinary, protocol.NewMessage(protocol.MsgInput, []byte(key))); err != nil {
			t.Fatalf("write input: %v", err)
		}
	}
	// A resize afterwards proves the server processed both inputs.
	if err := conn.Write(ctx, websocket.MessageBinary, protocol.NewMessage(protocol.MsgResize, []byte(`{"cols":80,"rows":24}`))); err != nil {
		t.Fatalf("write resize: %v", err)
	}

	errors := 0
	readCtx, readCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer readCancel()
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			break
		}
		msgType, payload, _ := protocol.ParseMessage(data)
		if msgType != protocol.MsgError {
			continue
		}
		var ep protocol.ErrorPayload
		if err := json.Unmarshal(payload, &ep); err != nil || ep.Code != "not_attached" {
			t.Fatalf("unexpected error frame: %s", payload)
		}
		errors++
	}
	if errors != 1 {
		t.Fatalf("not_attached errors = %d, want 1", errors)
	}
}

func TestWebSocket_ReceivesBroadcastTree(t *testing.T) {
	s, conn, cleanup := openTestWS(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The initial tree arrives synchronously on connect.
	if _, data, err := conn.Read(ctx); err != nil {
		t.Fatalf("read initial tree: %v", err)
	} else if msgType, _, _ := protocol.ParseMessage(data); msgType != protocol.MsgSessionTree {
		t.Fatalf("first frame type = 0x%02x, want session tree", msgType)
	}

	s.tmuxMgr.Tree().UpdateSession(&tmux.Session{ID: "$9", Name: "later"})
	s.fanout.BroadcastSessionTree()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no broadcast tree: %v", err)
		}
		msgType, payload, _ := protocol.ParseMessage(data)
		if msgType == protocol.MsgSessionTree && strings.Contains(string(payload), `"later"`) {
			return
		}
	}
}
