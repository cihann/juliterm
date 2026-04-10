package tmux

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type gitCacheEntry struct {
	branch string
	repo   string
}

type activePaneInfo struct {
	paneID  string
	pid     int
	path    string
	command string
	dead    bool
}

const (
	monitoringExcerptLines   = 6
	monitoringExcerptTimeout = 500 * time.Millisecond
)

type tmuxOutputRunner func(context.Context, string, ...string) ([]byte, error)

// Manager coordinates multiple ControlClients — one per tmux session — and
// maintains a unified SessionTree. A dedicated watcher ControlClient listens
// for %sessions-changed events to detect new/removed sessions.
type Manager struct {
	tmuxPath string
	tree     *SessionTree

	mu      sync.Mutex
	clients map[string]*ControlClient // sessionID -> client
	watcher *ControlClient            // dedicated watcher for %sessions-changed

	ctx    context.Context
	cancel context.CancelFunc

	onOutput func(paneID string, data []byte) // unified output callback
	onEvent  func(event Event)                // unified event callback

	// Excerpt stability tracking: detects when terminal output stops changing
	// (agent likely waiting for user input).
	prevExcerpts       map[string]string // session ID → last captured excerpt
	excerptStableCount map[string]int    // session ID → consecutive stable cycles
}

// NewManager creates a Manager. If tmuxPath is empty it defaults to "tmux".
func NewManager(tmuxPath string) *Manager {
	if tmuxPath == "" {
		tmuxPath = "tmux"
	}
	return &Manager{
		tmuxPath:           tmuxPath,
		tree:               NewSessionTree(),
		clients:            make(map[string]*ControlClient),
		prevExcerpts:       make(map[string]string),
		excerptStableCount: make(map[string]int),
	}
}

// Tree returns the unified SessionTree.
func (m *Manager) Tree() *SessionTree {
	return m.tree
}

// TmuxPath returns the path to the tmux binary.
func (m *Manager) TmuxPath() string {
	return m.tmuxPath
}

// Client returns the ControlClient for the given session ID.
func (m *Manager) Client(sessionID string) (*ControlClient, error) {
	m.mu.Lock()
	c, ok := m.clients[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("tmux manager: no client for session %s", sessionID)
	}
	return c, nil
}

// OnOutput sets the unified output callback. Must be called before Start.
func (m *Manager) OnOutput(fn func(paneID string, data []byte)) {
	m.mu.Lock()
	m.onOutput = fn
	m.mu.Unlock()
}

// OnEvent sets the unified event callback. Must be called before Start.
func (m *Manager) OnEvent(fn func(event Event)) {
	m.mu.Lock()
	m.onEvent = fn
	m.mu.Unlock()
}

// Start discovers all existing tmux sessions, spawns a ControlClient for
// each, and starts the watcher. The provided context controls the lifetime
// of the Manager; cancelling it is equivalent to calling Stop.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Discover existing sessions.
	sessions, err := m.listSessions()
	if err != nil {
		m.cancel()
		return fmt.Errorf("tmux manager: list sessions: %w", err)
	}

	// Spawn a ControlClient for each session and populate the tree.
	for _, s := range sessions {
		if err := m.addSession(s.ID); err != nil {
			log.Printf("tmux manager: failed to add session %s (%s): %v", s.ID, s.Name, err)
			// Continue with remaining sessions.
		}
	}

	// Start the watcher. We attach it to the first available session.
	// If there are no sessions the watcher cannot start — poll until one appears.
	if err := m.startWatcher(); err != nil {
		log.Printf("tmux manager: no sessions yet, polling for new sessions")
		go m.pollForSessions()
	}

	// Start periodic metadata enrichment (tool type, git info, status).
	go m.startMetadataPoller(5 * time.Second)

	return nil
}

// pollForSessions periodically checks for new tmux sessions when none exist
// at startup. Once a session appears, it adds all sessions and starts the watcher.
func (m *Manager) pollForSessions() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			sessions, _ := m.listSessions()
			if len(sessions) > 0 {
				for _, s := range sessions {
					if err := m.addSession(s.ID); err != nil {
						log.Printf("tmux manager: failed to add session %s: %v", s.ID, err)
					}
				}
				if err := m.startWatcher(); err != nil {
					log.Printf("tmux manager: watcher start failed: %v", err)
				}
				return
			}
		}
	}
}

// Stop cancels the manager context and stops all ControlClients including
// the watcher.
func (m *Manager) Stop() {
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, c := range m.clients {
		_ = c.Stop()
		delete(m.clients, id)
	}
	if m.watcher != nil {
		_ = m.watcher.Stop()
		m.watcher = nil
	}
}

// SendKeys sends keystrokes to the pane identified by paneID. The paneID
// (e.g. "%0") is looked up in the tree to determine which session's
// ControlClient should receive the command.
func (m *Manager) SendKeys(paneID string, command string) error {
	sessionID, found := m.tree.FindPane(paneID)
	if !found {
		return fmt.Errorf("tmux manager: pane %s not found", paneID)
	}

	m.mu.Lock()
	c, ok := m.clients[sessionID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("tmux manager: no client for session %s", sessionID)
	}

	// send-keys with literal flag to avoid key-name interpretation.
	return c.SendCommand(fmt.Sprintf("send-keys -t %s -l %s", paneID, escapeForTmux(command)))
}

// CapturePane runs `tmux capture-pane` and returns the textual content of
// the pane. lines specifies how many lines of scrollback to capture; if <= 0
// the entire visible area is captured.
func (m *Manager) CapturePane(paneID string, lines int) (string, error) {
	args := []string{"capture-pane", "-p", "-e", "-t", paneID}
	if lines > 0 {
		args = append(args, "-S", "-"+strconv.Itoa(lines))
	}

	out, err := exec.CommandContext(m.ctx, m.tmuxPath, args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux manager: capture-pane %s: %w", paneID, err)
	}
	// xterm.js needs \r\n line endings; capture-pane outputs \n only.
	result := strings.ReplaceAll(string(out), "\n", "\r\n")

	// Strip trailing empty lines to avoid pushing cursor to bottom.
	parts := strings.Split(result, "\r\n")
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	result = strings.Join(parts, "\r\n")
	if len(result) > 0 {
		result += "\r\n"
	}
	return result, nil
}

func (m *Manager) captureMonitoringExcerpt(paneID string) string {
	return captureMonitoringExcerptWithRunner(m.ctx, m.tmuxPath, paneID, runTmuxOutput)
}

func captureMonitoringExcerptWithRunner(parent context.Context, tmuxPath, paneID string, run tmuxOutputRunner) string {
	if strings.TrimSpace(paneID) == "" {
		return ""
	}

	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithTimeout(parent, monitoringExcerptTimeout)
	defer cancel()

	excerpt, err := run(ctx, tmuxPath,
		"capture-pane", "-p", "-t", paneID,
		"-S", "-"+strconv.Itoa(monitoringExcerptLines),
	)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(excerpt))
}

func runTmuxOutput(ctx context.Context, tmuxPath string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, tmuxPath, args...).Output()
}

func collectActivePaneInfo(output string) map[string]*activePaneInfo {
	activePanes := make(map[string]*activePaneInfo)

	for _, line := range splitLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 8 {
			continue
		}

		sessionID := parts[0]
		windowActive := parts[2] == "1"
		paneActive := parts[3] == "1"
		if _, exists := activePanes[sessionID]; !exists {
			activePanes[sessionID] = nil
		}
		if !windowActive || !paneActive {
			continue
		}

		pid, _ := strconv.Atoi(parts[4])
		activePanes[sessionID] = &activePaneInfo{
			paneID:  parts[1],
			pid:     pid,
			path:    parts[5],
			command: parts[6],
			dead:    parts[7] == "1",
		}
	}

	return activePanes
}

func buildMonitoringInputs(session *Session, info *activePaneInfo, snapshot ProcessSnapshot, excerpt string, now time.Time) MonitoringInputs {
	command := ""
	commandArgs := ""
	if info != nil {
		command = info.command
		if child, ok := snapshot[info.pid]; ok {
			commandArgs = child.Args
		}
	}

	return MonitoringInputs{
		Attached:     session.Attached,
		ToolType:     session.ToolType,
		Command:      command,
		CommandArgs:  commandArgs,
		WorkingDir:   session.WorkingDir,
		GitRepo:      session.GitRepo,
		GitBranch:    session.GitBranch,
		LastActivity: session.LastActivity,
		Now:          now,
		Excerpt:      excerpt,
	}
}

// ResizePane resizes a tmux pane to the given dimensions.
func (m *Manager) ResizePane(paneID string, cols, rows int) error {
	return exec.CommandContext(m.ctx, m.tmuxPath,
		"resize-pane", "-t", paneID,
		"-x", strconv.Itoa(cols),
		"-y", strconv.Itoa(rows),
	).Run()
}

// ---------------------------------------------------------------------------
// CRUD operations
// ---------------------------------------------------------------------------

// ValidationError is returned when a session or window name is invalid.
// It is exported so callers (e.g. HTTP handlers) can distinguish validation
// failures from tmux process errors.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// validateName checks that a session or window name is safe for tmux and URL
// routing. It rejects empty names, names containing tmux target specials
// (. : /) and names longer than 256 characters.
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return &ValidationError{"name must not be empty"}
	}
	if len(name) > 256 {
		return &ValidationError{"name must not exceed 256 characters"}
	}
	if strings.ContainsAny(name, ".:/") {
		return &ValidationError{"name must not contain '.', ':', or '/'"}
	}
	return nil
}

// CreateSession creates a new detached tmux session with the given name.
func (m *Manager) CreateSession(ctx context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "new-session", "-d", "-s", name).CombinedOutput(); err != nil {
		return tmuxError("tmux new-session", out, err)
	}
	m.syncSessions()
	return nil
}

// RenameSession renames an existing tmux session.
func (m *Manager) RenameSession(ctx context.Context, oldName, newName string) error {
	if err := validateName(newName); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "rename-session", "-t", oldName, newName).CombinedOutput(); err != nil {
		return tmuxError("tmux rename-session", out, err)
	}
	m.syncSessions()
	return nil
}

// KillSession kills a tmux session.
func (m *Manager) KillSession(ctx context.Context, name string) error {
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "kill-session", "-t", name).CombinedOutput(); err != nil {
		return tmuxError("tmux kill-session", out, err)
	}
	m.syncSessions()
	return nil
}

// SplitOpts configures a split-window operation.
type SplitOpts struct {
	Horizontal bool   // true = split right (-h), false = split down (-v)
	Percent    int    // size of new pane (1-90)
	Command    string // command to run in new pane (empty = shell)
	Detach     bool   // true = focus stays on original pane (-d)
}

// SplitWindow splits the active pane in the given session and optionally runs
// a command in the new pane. The new pane inherits the active pane's working
// directory via #{pane_current_path}. Fire-and-forget: Juli does not track the
// resulting pane.
func (m *Manager) SplitWindow(ctx context.Context, sessionName string, opts SplitOpts) error {
	args := []string{"split-window", "-t", sessionName, "-PF", "#{pane_id}",
		"-c", "#{pane_current_path}"}

	if opts.Horizontal {
		args = append(args, "-h")
	} else {
		args = append(args, "-v")
	}
	if opts.Detach {
		args = append(args, "-d")
	}
	if opts.Percent > 0 && opts.Percent <= 90 {
		args = append(args, "-p", strconv.Itoa(opts.Percent))
	}

	out, err := exec.CommandContext(ctx, m.tmuxPath, args...).CombinedOutput()
	if err != nil {
		return tmuxError("tmux split-window", out, err)
	}

	paneID := strings.TrimSpace(string(out))

	if opts.Command != "" {
		// Pass the raw command as an argv element to send-keys -l. Do NOT use
		// escapeForTmux — that helper is for control-mode stdin strings, not
		// exec.CommandContext argv elements.
		sendArgs := []string{"send-keys", "-t", paneID, "-l", opts.Command}
		if sout, serr := exec.CommandContext(ctx, m.tmuxPath, sendArgs...).CombinedOutput(); serr != nil {
			_ = exec.Command(m.tmuxPath, "kill-pane", "-t", paneID).Run() // best-effort cleanup
			return tmuxError("tmux send-keys", sout, serr)
		}
		// Press Enter without -l so tmux interprets it as the key name.
		if sout, serr := exec.CommandContext(ctx, m.tmuxPath, "send-keys", "-t", paneID, "Enter").CombinedOutput(); serr != nil {
			_ = exec.Command(m.tmuxPath, "kill-pane", "-t", paneID).Run() // best-effort cleanup
			return tmuxError("tmux send-keys Enter", sout, serr)
		}
	}

	m.syncSessions()
	return nil
}

// ZoomPane toggles the zoom state of the active pane in the given session.
// When zoomed, the active pane fills the entire window; other panes are hidden
// but their processes continue running.
func (m *Manager) ZoomPane(ctx context.Context, sessionName string) error {
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "resize-pane", "-Z", "-t", sessionName).CombinedOutput(); err != nil {
		return tmuxError("tmux resize-pane", out, err)
	}
	m.syncSessions()
	return nil
}

// reTestTarget matches a Makefile target named "test" at the start of a line.
var reTestTarget = regexp.MustCompile(`(?m)^test:`)

// PaneCwd resolves the active pane's current working directory for the given
// session via tmux display-message. Returns "" if the cwd cannot be determined.
func (m *Manager) PaneCwd(ctx context.Context, sessionName string) (string, error) {
	out, err := exec.CommandContext(ctx, m.tmuxPath,
		"display-message", "-t", sessionName, "-p", "#{pane_current_path}",
	).CombinedOutput()
	if err != nil {
		return "", nil // tmux unavailable or session gone — graceful fallback
	}
	dir := strings.TrimSpace(string(out))
	return dir, nil
}

// DetectTestCommand resolves the active pane's current working directory via
// tmux and checks it for common project markers. Returns "" if no marker is
// found or the cwd cannot be determined.
func (m *Manager) DetectTestCommand(ctx context.Context, sessionName string) (string, error) {
	dir, _ := m.PaneCwd(ctx, sessionName)
	if dir == "" {
		return "", nil
	}

	checks := []struct {
		marker  string
		command string
	}{
		{"go.mod", "go test ./..."},
		{"Cargo.toml", "cargo test"},
		{"package.json", "npm test"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.marker)); err == nil {
			return c.command, nil
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "Makefile")); err == nil {
		if reTestTarget.Match(data) {
			return "make test", nil
		}
	}
	return "", nil
}

// CreateWindow creates a new window in the given session.
func (m *Manager) CreateWindow(ctx context.Context, sessionName string) error {
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "new-window", "-t", sessionName).CombinedOutput(); err != nil {
		return tmuxError("tmux new-window", out, err)
	}
	m.syncSessions()
	return nil
}

// RenameWindow renames a window in the given session by index.
func (m *Manager) RenameWindow(ctx context.Context, sessionName string, windowIndex int, newName string) error {
	if err := validateName(newName); err != nil {
		return err
	}
	target := fmt.Sprintf("%s:%d", sessionName, windowIndex)
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "rename-window", "-t", target, newName).CombinedOutput(); err != nil {
		return tmuxError("tmux rename-window", out, err)
	}
	m.syncSessions()
	return nil
}

// KillWindow kills a window in the given session by index.
func (m *Manager) KillWindow(ctx context.Context, sessionName string, windowIndex int) error {
	target := fmt.Sprintf("%s:%d", sessionName, windowIndex)
	if out, err := exec.CommandContext(ctx, m.tmuxPath, "kill-window", "-t", target).CombinedOutput(); err != nil {
		return tmuxError("tmux kill-window", out, err)
	}
	m.syncSessions()
	return nil
}

// tmuxError builds an error from a tmux command, preferring stderr/stdout
// output over the generic "exit status 1" so mapTmuxError can match it.
func tmuxError(prefix string, out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%s: %s", prefix, msg)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// ---------------------------------------------------------------------------
// Internal: session lifecycle
// ---------------------------------------------------------------------------

// addSession creates a ControlClient for the given session, discovers its
// windows and panes, updates the tree, and starts the client.
func (m *Manager) addSession(sessionID string) error {
	session, err := m.discoverSessionDetails(sessionID)
	if err != nil {
		return err
	}

	client := NewControlClient(m.ctx, sessionID, m.tmuxPath)
	m.wireClientCallbacks(client, sessionID)

	if err := client.Start(); err != nil {
		return fmt.Errorf("start control client for %s: %w", sessionID, err)
	}

	m.mu.Lock()
	m.clients[sessionID] = client
	m.mu.Unlock()

	m.tree.UpdateSession(session)
	return nil
}

// removeSession stops the ControlClient for the given session and removes it
// from the tree.
func (m *Manager) removeSession(sessionID string) {
	m.mu.Lock()
	c, ok := m.clients[sessionID]
	if ok {
		delete(m.clients, sessionID)
	}
	m.mu.Unlock()

	if ok {
		_ = c.Stop()
	}
	m.tree.RemoveSession(sessionID)

	// Clean up excerpt stability state for this session.
	delete(m.prevExcerpts, sessionID)
	delete(m.excerptStableCount, sessionID)
}

// syncSessions compares the set of currently running tmux sessions against
// the known sessions in the tree and adds/removes as needed.
func (m *Manager) syncSessions() {
	sessions, err := m.listSessions()
	if err != nil {
		log.Printf("tmux manager: syncSessions list failed (keeping existing tree): %v", err)
		return
	}

	current := make(map[string]*Session, len(sessions))
	for _, s := range sessions {
		current[s.ID] = s
	}

	// Determine which sessions to add and which to remove.
	known := m.tree.GetSessions()
	knownIDs := make(map[string]bool, len(known))
	for _, s := range known {
		knownIDs[s.ID] = true
	}

	// When tmux legitimately has zero sessions (all killed), remove
	// everything from the tree so the UI doesn't show stale entries.
	if len(current) == 0 && len(known) > 0 {
		log.Printf("tmux manager: all tmux sessions gone — clearing tree (%d removed)", len(known))
		for _, s := range known {
			m.removeSession(s.ID)
		}
		return
	}

	// Add new sessions.
	for id := range current {
		if !knownIDs[id] {
			if err := m.addSession(id); err != nil {
				log.Printf("tmux manager: failed to add new session %s: %v", id, err)
			}
		}
	}

	// Remove disappeared sessions.
	for _, s := range known {
		if _, exists := current[s.ID]; !exists {
			log.Printf("tmux manager: removing session %s (%s) — no longer in tmux", s.ID, s.Name)
			m.removeSession(s.ID)
		}
	}

	// Detect renames: same ID but different name.
	for _, s := range known {
		if cur, ok := current[s.ID]; ok && cur.Name != s.Name {
			m.tree.RenameSession(s.ID, cur.Name)
		}
	}

	// Ensure the watcher is running — it may have been deferred if there
	// were no sessions at startup.
	m.mu.Lock()
	hasWatcher := m.watcher != nil
	m.mu.Unlock()
	if !hasWatcher && len(sessions) > 0 {
		if err := m.startWatcher(); err != nil {
			log.Printf("tmux manager: watcher start failed: %v", err)
		}
	}
}

// discoverSessionDetails queries tmux for the session's name, windows, and
// panes and returns a fully populated Session struct.
func (m *Manager) discoverSessionDetails(sessionID string) (*Session, error) {
	// Get session name.
	sessions, err := m.listSessions()
	if err != nil {
		return nil, err
	}

	var session *Session
	for _, s := range sessions {
		if s.ID == sessionID {
			session = s
			break
		}
	}
	if session == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	// Query windows.
	winOut, err := exec.CommandContext(m.ctx, m.tmuxPath,
		"list-windows", "-t", sessionID,
		"-F", "#{window_id}\t#{window_name}\t#{window_index}\t#{window_active}",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("list-windows for %s: %w", sessionID, err)
	}
	windows, err := ParseWindowList(string(winOut))
	if err != nil {
		return nil, fmt.Errorf("parse windows for %s: %w", sessionID, err)
	}

	// Query panes for each window.
	for _, w := range windows {
		paneOut, err := exec.CommandContext(m.ctx, m.tmuxPath,
			"list-panes", "-t", w.ID,
			"-F", "#{pane_id}\t#{pane_width}\t#{pane_height}\t#{pane_active}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_pid}",
		).Output()
		if err != nil {
			return nil, fmt.Errorf("list-panes for window %s: %w", w.ID, err)
		}
		panes, err := ParsePaneList(string(paneOut))
		if err != nil {
			return nil, fmt.Errorf("parse panes for window %s: %w", w.ID, err)
		}
		w.Panes = panes
	}

	session.Windows = windows
	return session, nil
}

// ---------------------------------------------------------------------------
// Internal: watcher
// ---------------------------------------------------------------------------

// startWatcher creates a ControlClient attached to the first available
// session whose sole purpose is to receive %sessions-changed events.
func (m *Manager) startWatcher() error {
	m.mu.Lock()
	if m.watcher != nil {
		m.mu.Unlock()
		return nil // already running
	}
	m.mu.Unlock()

	// Pick any session to attach the watcher to.
	sessions := m.tree.GetSessions()
	if len(sessions) == 0 {
		return fmt.Errorf("no sessions available for watcher")
	}

	targetSession := sessions[0].ID
	w := NewControlClient(m.ctx, targetSession, m.tmuxPath)
	w.OnEvent(func(event Event) {
		if event.Type == EventSessionsChanged {
			m.syncSessions()
		}
	})

	if err := w.Start(); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}

	m.mu.Lock()
	m.watcher = w
	m.mu.Unlock()

	// Monitor watcher liveness: if the watcher's host session is killed,
	// nil out m.watcher so syncSessions can restart it. If no sessions
	// remain, fall back to pollForSessions.
	go func() {
		w.Wait()

		// Don't act if the manager is shutting down.
		if m.ctx.Err() != nil {
			return
		}

		m.mu.Lock()
		if m.watcher == w {
			m.watcher = nil
		}
		m.mu.Unlock()

		sessions, _ := m.listSessions()
		if len(sessions) > 0 {
			m.syncSessions()
		} else {
			// All sessions are gone — clear stale entries from the tree.
			m.syncSessions()
			go m.pollForSessions()
		}
	}()

	return nil
}

// ---------------------------------------------------------------------------
// Internal: callbacks
// ---------------------------------------------------------------------------

// wireClientCallbacks sets up the OnOutput and OnEvent callbacks on a
// ControlClient so that output is forwarded to the unified callbacks and
// lifecycle events update the tree.
func (m *Manager) wireClientCallbacks(client *ControlClient, sessionID string) {
	client.OnOutput(func(paneID string, data []byte) {
		m.mu.Lock()
		fn := m.onOutput
		m.mu.Unlock()
		if fn != nil {
			fn(paneID, data)
		}
	})

	client.OnEvent(func(event Event) {
		// Forward to unified event callback.
		m.mu.Lock()
		fn := m.onEvent
		m.mu.Unlock()
		if fn != nil {
			fn(event)
		}

		// Handle tree-mutating events.
		switch event.Type {
		case EventWindowAdd:
			// Re-discover the full session to pick up the new window and
			// its panes, since %window-add only gives us the window ID.
			s, err := m.discoverSessionDetails(sessionID)
			if err != nil {
				log.Printf("tmux manager: rediscover session %s after window-add: %v", sessionID, err)
				return
			}
			m.tree.UpdateSession(s)

		case EventWindowClose:
			m.tree.RemoveWindow(sessionID, event.WindowID)

		case EventWindowRenamed:
			m.tree.RenameWindow(sessionID, event.WindowID, event.Name)
		}
	})
}

// ---------------------------------------------------------------------------
// Internal: metadata enrichment
// ---------------------------------------------------------------------------

// startMetadataPoller periodically enriches session metadata (tool type, git
// info, status) and broadcasts changes to WebSocket clients.
func (m *Manager) startMetadataPoller(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.enrichMetadata()
		}
	}
}

// enrichMetadata gathers pane-level process/git info and updates session
// metadata. Only triggers a tree change notification if something changed.
func (m *Manager) enrichMetadata() {
	// Get all pane info in one tmux call.
	out, err := exec.CommandContext(m.ctx, m.tmuxPath,
		"list-panes", "-a",
		"-F", "#{session_id}\t#{pane_id}\t#{window_active}\t#{pane_active}\t#{pane_pid}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_dead}",
	).Output()
	if err != nil {
		return
	}

	activePanes := collectActivePaneInfo(string(out))

	// Also refresh session-level metadata (attached count, activity).
	sessionMeta, _ := m.listSessions()
	sessionMap := make(map[string]*Session, len(sessionMeta))
	for _, s := range sessionMeta {
		sessionMap[s.ID] = s
	}

	// Snapshot process table once for all sessions.
	psSnapshot := SnapshotProcesses(m.ctx)

	// Per-tick git cache — deduplicates calls for sessions sharing a cwd.
	// Rebuilt each tick so branch changes (git checkout) are picked up within 5s.
	gitCache := make(map[string]gitCacheEntry)
	now := time.Now()

	// Enrich each session.
	changed := false
	sessions := m.tree.GetSessions()
	for _, s := range sessions {
		oldStatus := s.Status
		oldTool := s.ToolType
		oldBranch := s.GitBranch
		oldRepo := s.GitRepo
		oldDir := s.WorkingDir
		oldActivity := s.LastActivity
		oldActivityState := s.ActivityState
		oldActivitySummary := s.ActivitySummary

		// Update activity/attached from tmux.
		if meta, ok := sessionMap[s.ID]; ok {
			s.LastActivity = meta.LastActivity
			s.Attached = meta.Attached
		}

		info := activePanes[s.ID]
		if info != nil {
			toolType := DetectToolType(psSnapshot, info.pid)
			s.ToolType = toolType
			s.WorkingDir = info.path

			if cached, ok := gitCache[info.path]; ok {
				s.GitBranch = cached.branch
				s.GitRepo = cached.repo
			} else {
				branch, repo := GetGitInfo(m.ctx, info.path)
				s.GitBranch = branch
				s.GitRepo = repo
				gitCache[info.path] = gitCacheEntry{branch: branch, repo: repo}
			}

			hasChild := toolType != "shell"
			s.Status = DeriveSessionStatus(s.Attached, info.dead, hasChild)
		} else {
			// No active pane info — likely detached or empty.
			s.ToolType = ""
			s.WorkingDir = ""
			s.GitBranch = ""
			s.GitRepo = ""
			s.Status = DeriveSessionStatus(s.Attached, false, false)
		}

		excerpt := ""
		if info != nil {
			excerpt = m.captureMonitoringExcerpt(info.paneID)
		}

		// Track excerpt stability: if the terminal output hasn't changed for
		// ≥2 consecutive cycles, the agent is likely waiting for user input.
		excerptStable := false
		if prev, ok := m.prevExcerpts[s.ID]; ok && excerpt != "" && excerpt == prev {
			m.excerptStableCount[s.ID]++
			if m.excerptStableCount[s.ID] >= 2 {
				excerptStable = true
			}
		} else {
			m.excerptStableCount[s.ID] = 0
		}
		m.prevExcerpts[s.ID] = excerpt

		inputs := buildMonitoringInputs(s, info, psSnapshot, excerpt, now)
		inputs.ExcerptStable = excerptStable
		monitoring := DeriveMonitoringResult(inputs)
		s.ActivityState = monitoring.State
		s.ActivitySummary = monitoring.Summary

		if s.Status != oldStatus || s.ToolType != oldTool || s.GitBranch != oldBranch ||
			s.GitRepo != oldRepo || s.WorkingDir != oldDir || s.LastActivity != oldActivity ||
			s.ActivityState != oldActivityState || s.ActivitySummary != oldActivitySummary {
			changed = true
			// Update the tree with new metadata (preserves windows/panes).
			m.tree.UpdateSessionMetadata(s.ID, func(existing *Session) {
				existing.Status = s.Status
				existing.LastActivity = s.LastActivity
				existing.ToolType = s.ToolType
				existing.GitBranch = s.GitBranch
				existing.GitRepo = s.GitRepo
				existing.WorkingDir = s.WorkingDir
				existing.ActivityState = s.ActivityState
				existing.ActivitySummary = s.ActivitySummary
				existing.Attached = s.Attached
			})
		}
	}

	if changed {
		m.tree.notifyChange()
	}
}

// ---------------------------------------------------------------------------
// Internal: tmux commands
// ---------------------------------------------------------------------------

// listSessions runs `tmux list-sessions` and returns the parsed result.
// It returns an error for unexpected failures. An exit code 1 with "no server
// running" or "no sessions" output is treated as an empty list (not an error),
// because tmux uses exit 1 for both "no sessions" and real failures.
func (m *Manager) listSessions() ([]*Session, error) {
	out, err := exec.CommandContext(m.ctx, m.tmuxPath,
		"list-sessions", "-F", "#{session_id}\t#{session_name}\t#{session_activity}\t#{session_attached}",
	).Output()
	if err != nil {
		// tmux returns exit 1 when there are no sessions — that's fine.
		// But also returns exit 1 for real errors (socket gone, etc.).
		// Distinguish by checking stderr: "no server running" or empty
		// output with exit 1 means genuinely no sessions.
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "no server running") ||
				strings.Contains(stderr, "no sessions") ||
				strings.Contains(stderr, "error connecting to") ||
				len(strings.TrimSpace(stderr)) == 0 {
				return []*Session{}, nil
			}
		}
		return nil, fmt.Errorf("tmux list-sessions: %w", err)
	}
	return ParseSessionList(string(out))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// escapeForTmux wraps a string in double quotes, escaping embedded
// double-quotes, backslashes, and dollar signs so it can be passed as a
// single argument to tmux send-keys -l.
func escapeForTmux(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\', '$':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}
