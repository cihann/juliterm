package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cihann/juliterm/internal/config"
	"github.com/cihann/juliterm/internal/tmux"
)

// mockManager implements SessionManager for handler tests.
type mockManager struct {
	tree         *tmux.SessionTree
	createErr    error
	renameErr    error
	killErr      error
	createWinErr error
	renameWinErr error
	killWinErr   error

	lastCreateName    string
	lastRenameOld     string
	lastRenameNew     string
	lastKillName      string
	lastCreateWinSess string
	lastRenameWinSess string
	lastRenameWinIdx  int
	lastRenameWinName string
	lastKillWinSess   string
	lastKillWinIdx    int

	splitErr         error
	lastSplitSession string
	lastSplitOpts    tmux.SplitOpts

	detectTestCmd     string
	detectTestErr     error
	lastDetectSession string

	paneCwd     string
	paneCwdErr  error

	zoomErr         error
	lastZoomSession string

	killPaneErr         error
	lastKillPaneSession string
}

func newMockManager() *mockManager {
	return &mockManager{tree: tmux.NewSessionTree()}
}

func (m *mockManager) Tree() *tmux.SessionTree    { return m.tree }
func (m *mockManager) TmuxPath() string            { return "tmux" }

func (m *mockManager) CreateSession(_ context.Context, name string) error {
	m.lastCreateName = name
	return m.createErr
}

func (m *mockManager) RenameSession(_ context.Context, oldName, newName string) error {
	m.lastRenameOld = oldName
	m.lastRenameNew = newName
	return m.renameErr
}

func (m *mockManager) KillSession(_ context.Context, name string) error {
	m.lastKillName = name
	return m.killErr
}

func (m *mockManager) CreateWindow(_ context.Context, sessionName string) error {
	m.lastCreateWinSess = sessionName
	return m.createWinErr
}

func (m *mockManager) RenameWindow(_ context.Context, sessionName string, windowIndex int, newName string) error {
	m.lastRenameWinSess = sessionName
	m.lastRenameWinIdx = windowIndex
	m.lastRenameWinName = newName
	return m.renameWinErr
}

func (m *mockManager) KillWindow(_ context.Context, sessionName string, windowIndex int) error {
	m.lastKillWinSess = sessionName
	m.lastKillWinIdx = windowIndex
	return m.killWinErr
}

func (m *mockManager) SplitWindow(_ context.Context, sessionName string, opts tmux.SplitOpts) error {
	m.lastSplitSession = sessionName
	m.lastSplitOpts = opts
	return m.splitErr
}

func (m *mockManager) DetectTestCommand(_ context.Context, sessionName string) (string, error) {
	m.lastDetectSession = sessionName
	return m.detectTestCmd, m.detectTestErr
}

func (m *mockManager) PaneCwd(_ context.Context, _ string) (string, error) {
	return m.paneCwd, m.paneCwdErr
}

func (m *mockManager) ZoomPane(_ context.Context, sessionName string) error {
	m.lastZoomSession = sessionName
	return m.zoomErr
}

func (m *mockManager) KillPane(_ context.Context, sessionName string) error {
	m.lastKillPaneSession = sessionName
	return m.killPaneErr
}

func newTestServer(mgr *mockManager) *Server {
	return newTestServerWithConfig(mgr, &config.Config{})
}

func newTestServerWithConfig(mgr *mockManager, cfg *config.Config) *Server {
	return &Server{
		port:          0,
		token:         "test",
		tmuxMgr:       mgr,
		cfg:           cfg,
		fanout:        NewFanout(mgr.tree),
		sessionSecret: []byte("testsecret0123456789abcdef012345"),
		sessions:      NewSessionStore(sessionTTL),
	}
}

// --- Session handler tests ---

func TestCreateSession_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"name":"dev"}`
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.handleCreateSession(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if mgr.lastCreateName != "dev" {
		t.Errorf("createName = %q, want %q", mgr.lastCreateName, "dev")
	}
}

func TestCreateSession_MissingName(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()

	s.handleCreateSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateSession_ValidationError(t *testing.T) {
	mgr := newMockManager()
	mgr.createErr = &tmux.ValidationError{Msg: "name must not contain '.', ':', or '/'"}
	s := newTestServer(mgr)

	body := `{"name":"my.session"}`
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.handleCreateSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateSession_Conflict(t *testing.T) {
	mgr := newMockManager()
	mgr.createErr = fmt.Errorf("tmux new-session: duplicate session: dev")
	s := newTestServer(mgr)

	body := `{"name":"dev"}`
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.handleCreateSession(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestRenameSession_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"name":"new-name"}`
	req := httptest.NewRequest("PATCH", "/api/sessions/old-name", bytes.NewBufferString(body))
	req.SetPathValue("name", "old-name")
	w := httptest.NewRecorder()

	s.handleRenameSession(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastRenameOld != "old-name" || mgr.lastRenameNew != "new-name" {
		t.Errorf("rename(%q, %q), want (old-name, new-name)", mgr.lastRenameOld, mgr.lastRenameNew)
	}
}

func TestKillSession_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("DELETE", "/api/sessions/dev", nil)
	req.SetPathValue("name", "dev")
	w := httptest.NewRecorder()

	s.handleKillSession(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastKillName != "dev" {
		t.Errorf("killName = %q, want %q", mgr.lastKillName, "dev")
	}
}

func TestKillSession_NotFound(t *testing.T) {
	mgr := newMockManager()
	mgr.killErr = fmt.Errorf("tmux kill-session: can't find session: nope")
	s := newTestServer(mgr)

	req := httptest.NewRequest("DELETE", "/api/sessions/nope", nil)
	req.SetPathValue("name", "nope")
	w := httptest.NewRecorder()

	s.handleKillSession(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- Window handler tests ---

func TestCreateWindow_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("POST", "/api/sessions/dev/windows", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleCreateWindow(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if mgr.lastCreateWinSess != "dev" {
		t.Errorf("createWinSess = %q, want %q", mgr.lastCreateWinSess, "dev")
	}
}

func TestRenameWindow_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"name":"new-win"}`
	req := httptest.NewRequest("PATCH", "/api/sessions/dev/windows/2", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	req.SetPathValue("index", "2")
	w := httptest.NewRecorder()

	s.handleRenameWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastRenameWinSess != "dev" || mgr.lastRenameWinIdx != 2 || mgr.lastRenameWinName != "new-win" {
		t.Errorf("renameWin(%q, %d, %q), want (dev, 2, new-win)",
			mgr.lastRenameWinSess, mgr.lastRenameWinIdx, mgr.lastRenameWinName)
	}
}

func TestRenameWindow_BadIndex(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"name":"new-win"}`
	req := httptest.NewRequest("PATCH", "/api/sessions/dev/windows/abc", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	req.SetPathValue("index", "abc")
	w := httptest.NewRecorder()

	s.handleRenameWindow(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestKillWindow_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("DELETE", "/api/sessions/dev/windows/0", nil)
	req.SetPathValue("session", "dev")
	req.SetPathValue("index", "0")
	w := httptest.NewRecorder()

	s.handleKillWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastKillWinSess != "dev" || mgr.lastKillWinIdx != 0 {
		t.Errorf("killWin(%q, %d), want (dev, 0)", mgr.lastKillWinSess, mgr.lastKillWinIdx)
	}
}

// --- Split window handler tests ---

func TestSplitWindow_Lazygit(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"preset":"lazygit"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastSplitSession != "dev" {
		t.Errorf("splitSession = %q, want %q", mgr.lastSplitSession, "dev")
	}
	want := tmux.SplitOpts{Horizontal: true, Percent: 50, Command: "lazygit", Detach: false}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_TestRunner(t *testing.T) {
	mgr := newMockManager()
	mgr.detectTestCmd = "go test ./..."
	s := newTestServer(mgr)

	body := `{"preset":"test-runner"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	want := tmux.SplitOpts{Horizontal: false, Percent: 30, Command: "go test ./...", Detach: true}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_TestRunner_NoDetect(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"preset":"test-runner"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// No detected command — empty shell with detach.
	want := tmux.SplitOpts{Horizontal: false, Percent: 30, Command: "", Detach: true}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_Custom(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"preset":"custom","command":"htop"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	want := tmux.SplitOpts{Horizontal: true, Percent: 50, Command: "htop", Detach: false}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_CustomEmpty(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"preset":"custom","command":""}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	want := tmux.SplitOpts{Horizontal: true, Percent: 50, Command: "", Detach: false}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_CustomVertical(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"preset":"custom","direction":"vertical"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	want := tmux.SplitOpts{Horizontal: false, Percent: 50, Command: "", Detach: false}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_UnknownPreset(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	body := `{"preset":"foo"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSplitWindow_TmuxError(t *testing.T) {
	mgr := newMockManager()
	mgr.splitErr = fmt.Errorf("tmux split-window: pane too small")
	s := newTestServer(mgr)

	body := `{"preset":"lazygit"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestSplitWindow_NotFound(t *testing.T) {
	mgr := newMockManager()
	mgr.splitErr = fmt.Errorf("tmux split-window: can't find session: nope")
	s := newTestServer(mgr)

	body := `{"preset":"lazygit"}`
	req := httptest.NewRequest("POST", "/api/sessions/nope/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "nope")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSplitWindow_ConfigPreset(t *testing.T) {
	mgr := newMockManager()
	cfg := &config.Config{
		Splits: []config.SplitPreset{
			{Name: "htop", Command: "htop", Direction: "horizontal", Percent: 30, Detach: false},
		},
	}
	s := newTestServerWithConfig(mgr, cfg)

	body := `{"preset":"htop"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	want := tmux.SplitOpts{Horizontal: true, Percent: 30, Command: "htop", Detach: false}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestSplitWindow_ConfigPresetVertical(t *testing.T) {
	mgr := newMockManager()
	cfg := &config.Config{
		Splits: []config.SplitPreset{
			{Name: "logs", Command: "tail -f app.log", Direction: "vertical", Percent: 25, Detach: true},
		},
	}
	s := newTestServerWithConfig(mgr, cfg)

	body := `{"preset":"logs"}`
	req := httptest.NewRequest("POST", "/api/sessions/dev/split", bytes.NewBufferString(body))
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleSplitWindow(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	want := tmux.SplitOpts{Horizontal: false, Percent: 25, Command: "tail -f app.log", Detach: true}
	if mgr.lastSplitOpts != want {
		t.Errorf("splitOpts = %+v, want %+v", mgr.lastSplitOpts, want)
	}
}

func TestGetPresets_Empty(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("GET", "/api/sessions/dev/presets", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleGetPresets(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	builtIn, ok := resp["builtIn"].([]any)
	if !ok || len(builtIn) != 2 {
		t.Errorf("builtIn count = %d, want 2", len(builtIn))
	}
	if resp["global"] != nil {
		t.Errorf("global = %v, want nil", resp["global"])
	}
	if resp["project"] != nil {
		t.Errorf("project = %v, want nil", resp["project"])
	}
}

func TestGetPresets_WithGlobalConfig(t *testing.T) {
	mgr := newMockManager()
	cfg := &config.Config{
		Splits: []config.SplitPreset{
			{Name: "htop", Command: "htop", Direction: "horizontal", Percent: 30},
			{Name: "k9s", Command: "k9s", Direction: "vertical", Percent: 50},
		},
	}
	s := newTestServerWithConfig(mgr, cfg)

	req := httptest.NewRequest("GET", "/api/sessions/dev/presets", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleGetPresets(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	global, ok := resp["global"].([]any)
	if !ok || len(global) != 2 {
		t.Errorf("global count = %d, want 2", len(global))
	}
}

// --- Detect test handler tests ---

func TestDetectTest_WithCommand(t *testing.T) {
	mgr := newMockManager()
	mgr.detectTestCmd = "go test ./..."
	s := newTestServer(mgr)

	req := httptest.NewRequest("GET", "/api/sessions/dev/detect-test", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleDetectTest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["command"] != "go test ./..." {
		t.Errorf("command = %v, want %q", resp["command"], "go test ./...")
	}
}

func TestDetectTest_NoCommand(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("GET", "/api/sessions/dev/detect-test", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleDetectTest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["command"] != nil {
		t.Errorf("command = %v, want nil", resp["command"])
	}
}

// --- Zoom pane handler tests ---

func TestZoomPane_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("POST", "/api/sessions/dev/zoom", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleZoomPane(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastZoomSession != "dev" {
		t.Errorf("zoomSession = %q, want %q", mgr.lastZoomSession, "dev")
	}
}

func TestZoomPane_NotFound(t *testing.T) {
	mgr := newMockManager()
	mgr.zoomErr = fmt.Errorf("tmux resize-pane: can't find session: nope")
	s := newTestServer(mgr)

	req := httptest.NewRequest("POST", "/api/sessions/nope/zoom", nil)
	req.SetPathValue("session", "nope")
	w := httptest.NewRecorder()

	s.handleZoomPane(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- Kill pane handler tests ---

func TestKillPane_Success(t *testing.T) {
	mgr := newMockManager()
	s := newTestServer(mgr)

	req := httptest.NewRequest("DELETE", "/api/sessions/dev/pane", nil)
	req.SetPathValue("session", "dev")
	w := httptest.NewRecorder()

	s.handleKillPane(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if mgr.lastKillPaneSession != "dev" {
		t.Errorf("killPaneSession = %q, want %q", mgr.lastKillPaneSession, "dev")
	}
}

func TestKillPane_NotFound(t *testing.T) {
	mgr := newMockManager()
	mgr.killPaneErr = fmt.Errorf("tmux kill-pane: can't find session: nope")
	s := newTestServer(mgr)

	req := httptest.NewRequest("DELETE", "/api/sessions/nope/pane", nil)
	req.SetPathValue("session", "nope")
	w := httptest.NewRecorder()

	s.handleKillPane(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- Error response format ---

func TestErrorResponseFormat(t *testing.T) {
	mgr := newMockManager()
	mgr.killErr = fmt.Errorf("tmux kill-session: can't find session: nope")
	s := newTestServer(mgr)

	req := httptest.NewRequest("DELETE", "/api/sessions/nope", nil)
	req.SetPathValue("name", "nope")
	w := httptest.NewRecorder()

	s.handleKillSession(w, req)

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("error field is empty in JSON response")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
