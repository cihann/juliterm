// Package server implements the HTTP/WebSocket server that exposes tmux
// sessions via PTY-based terminal connections. Each browser client gets its own
// PTY running "tmux attach-session", providing a native terminal experience.
package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/cihann/juliterm/internal/config"
	"github.com/cihann/juliterm/internal/tmux"
)

const (
	// sessionTTL matches the cookie MaxAge (7 days).
	sessionTTL = 7 * 24 * time.Hour
	// sweepInterval controls how often expired sessions are evicted.
	sweepInterval = 1 * time.Hour
)

// SessionManager is the interface the server uses to interact with tmux.
// *tmux.Manager satisfies this interface. Tests may substitute a mock.
type SessionManager interface {
	Tree() *tmux.SessionTree
	TmuxPath() string
	CreateSession(ctx context.Context, name string) error
	RenameSession(ctx context.Context, oldName, newName string) error
	KillSession(ctx context.Context, name string) error
	CreateWindow(ctx context.Context, sessionName string) error
	RenameWindow(ctx context.Context, sessionName string, windowIndex int, newName string) error
	KillWindow(ctx context.Context, sessionName string, windowIndex int) error
	SplitWindow(ctx context.Context, sessionName string, opts tmux.SplitOpts) error
	DetectTestCommand(ctx context.Context, sessionName string) (string, error)
	PaneCwd(ctx context.Context, sessionName string) (string, error)
	ZoomPane(ctx context.Context, sessionName string) error
	KillPane(ctx context.Context, sessionName string) error
}

// Server is the main HTTP/WebSocket server for Juli.
type Server struct {
	port    int
	token   string // shared auth token
	tmuxMgr SessionManager
	cfg     *config.Config
	fanout  *Fanout

	sessionSecret []byte          // random secret for cookie signing
	sessions      *SessionStore   // server-side session store for revocation
	spaFS         http.FileSystem // embedded SPA files; nil to skip static serving

	allowedOriginsMu sync.RWMutex
	allowedOrigins   []string // extra origins allowed for WS (e.g. tunnel URL host)
}

// AllowOrigin adds a host pattern to the WebSocket origin allowlist.
// Patterns follow the coder/websocket format (e.g. "random-words.trycloudflare.com").
// Same-origin requests are always permitted regardless of this list.
func (s *Server) AllowOrigin(pattern string) {
	s.allowedOriginsMu.Lock()
	defer s.allowedOriginsMu.Unlock()
	s.allowedOrigins = append(s.allowedOrigins, pattern)
}

// New creates a Server. The token is the shared secret that clients must provide
// via POST /api/login. If spaFS is non-nil, it is served at the root path.
func New(port int, token string, tmuxMgr SessionManager, spaFS http.FileSystem, cfg *config.Config) *Server {
	if cfg == nil {
		cfg = &config.Config{}
	}
	s := &Server{
		port:    port,
		token:   token,
		tmuxMgr: tmuxMgr,
		cfg:     cfg,
		spaFS:   spaFS,
	}
	s.fanout = NewFanout(tmuxMgr.Tree())
	return s
}

// Start initialises the server and begins listening. It blocks until the
// context is cancelled, at which point it performs a graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	s.sessionSecret = make([]byte, 32)
	if _, err := rand.Read(s.sessionSecret); err != nil {
		return fmt.Errorf("server: generate session secret: %w", err)
	}
	s.sessions = NewSessionStore(sessionTTL)
	go s.sweepSessions(ctx)

	// When the session tree changes, broadcast to all connected clients.
	s.tmuxMgr.Tree().SetOnChange(func() {
		s.fanout.BroadcastSessionTree()
	})

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		log.Println("server: shutting down...")
		srv.Shutdown(context.Background())
	}()

	log.Printf("server: listening on :%d", s.port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// StartTLS is identical to Start but serves over TLS.
func (s *Server) StartTLS(ctx context.Context, certFile, keyFile string) error {
	s.sessionSecret = make([]byte, 32)
	if _, err := rand.Read(s.sessionSecret); err != nil {
		return fmt.Errorf("server: generate session secret: %w", err)
	}
	s.sessions = NewSessionStore(sessionTTL)
	go s.sweepSessions(ctx)

	s.tmuxMgr.Tree().SetOnChange(func() {
		s.fanout.BroadcastSessionTree()
	})

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		log.Println("server: shutting down...")
		srv.Shutdown(context.Background())
	}()

	log.Printf("server: listening on :%d (TLS)", s.port)
	if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// setupRoutes registers all HTTP handlers on the given mux.
func (s *Server) setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.Handle("GET /api/sessions", s.authMiddleware(http.HandlerFunc(s.handleSessions)))
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Session CRUD
	mux.Handle("POST /api/sessions", s.authMiddleware(http.HandlerFunc(s.handleCreateSession)))
	mux.Handle("PATCH /api/sessions/{name}", s.authMiddleware(http.HandlerFunc(s.handleRenameSession)))
	mux.Handle("DELETE /api/sessions/{name}", s.authMiddleware(http.HandlerFunc(s.handleKillSession)))

	// Window CRUD
	mux.Handle("POST /api/sessions/{session}/windows", s.authMiddleware(http.HandlerFunc(s.handleCreateWindow)))
	mux.Handle("PATCH /api/sessions/{session}/windows/{index}", s.authMiddleware(http.HandlerFunc(s.handleRenameWindow)))
	mux.Handle("DELETE /api/sessions/{session}/windows/{index}", s.authMiddleware(http.HandlerFunc(s.handleKillWindow)))

	// Split pane
	mux.Handle("GET /api/sessions/{session}/presets", s.authMiddleware(http.HandlerFunc(s.handleGetPresets)))
	mux.Handle("POST /api/sessions/{session}/split", s.authMiddleware(http.HandlerFunc(s.handleSplitWindow)))
	mux.Handle("GET /api/sessions/{session}/detect-test", s.authMiddleware(http.HandlerFunc(s.handleDetectTest)))
	mux.Handle("POST /api/sessions/{session}/zoom", s.authMiddleware(http.HandlerFunc(s.handleZoomPane)))
	mux.Handle("DELETE /api/sessions/{session}/pane", s.authMiddleware(http.HandlerFunc(s.handleKillPane)))

	if s.spaFS != nil {
		fileServer := http.FileServer(s.spaFS)
		mux.Handle("/", spaHandler(s.spaFS, fileServer))
	}
}

// sweepSessions periodically evicts expired sessions and closes their WebSocket
// connections. It exits when the context is cancelled.
func (s *Server) sweepSessions(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, id := range s.sessions.Sweep() {
				s.fanout.CloseByAuthSession(id)
			}
		}
	}
}

// spaHandler serves static files and falls back to index.html for SPA routing.
func spaHandler(fsys http.FileSystem, fileServer http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		f, err := fsys.Open(path)
		if err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
