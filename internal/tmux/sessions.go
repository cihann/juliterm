package tmux

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Pane represents a single tmux pane within a window.
type Pane struct {
	ID             string `json:"id"` // e.g., "%0"
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	Active         bool   `json:"active"`                   // is this the active pane in its window?
	CurrentCommand string `json:"currentCommand,omitempty"` // pane_current_command
	CurrentPath    string `json:"currentPath,omitempty"`    // pane_current_path
	Pid            int    `json:"pid,omitempty"`            // pane_pid (shell process)
}

// Window represents a single tmux window within a session.
type Window struct {
	ID     string  `json:"id"` // e.g., "@0"
	Name   string  `json:"name"`
	Index  int     `json:"index"`
	Active bool    `json:"active"` // is this the active window?
	Zoomed bool    `json:"zoomed"` // is the window in zoomed state?
	Panes  []*Pane `json:"panes"`
}

// Session represents a single tmux session.
type Session struct {
	ID              string        `json:"id"` // e.g., "$0"
	Name            string        `json:"name"`
	Windows         []*Window     `json:"windows"`
	Status          string        `json:"status,omitempty"`       // "running", "idle", "error", "detached"
	LastActivity    int64         `json:"lastActivity,omitempty"` // unix epoch seconds
	ToolType        string        `json:"toolType,omitempty"`     // "claude", "codex", "opencode", "aider", "shell", "unknown"
	GitBranch       string        `json:"gitBranch,omitempty"`
	GitRepo         string        `json:"gitRepo,omitempty"`    // "owner/repo"
	WorkingDir      string        `json:"workingDir,omitempty"` // active pane cwd
	ActivityState   ActivityState `json:"activityState,omitempty"`
	ActivitySummary string        `json:"activitySummary,omitempty"`
	Attached        int           `json:"-"` // number of attached clients (internal)
}

// SessionTree is the thread-safe aggregated tree of all tmux sessions.
type SessionTree struct {
	mu       sync.RWMutex
	sessions map[string]*Session // keyed by session ID
	onChange func()              // called when tree changes (for notifying WebSocket clients)
}

// NewSessionTree creates a new empty SessionTree.
func NewSessionTree() *SessionTree {
	return &SessionTree{
		sessions: make(map[string]*Session),
	}
}

// SetOnChange sets a callback that is invoked after any mutation to the tree.
// The callback is called with the lock released.
func (t *SessionTree) SetOnChange(fn func()) {
	t.mu.Lock()
	t.onChange = fn
	t.mu.Unlock()
}

func (t *SessionTree) notifyChange() {
	t.mu.RLock()
	fn := t.onChange
	t.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// UpdateSession adds or fully replaces a session in the tree.
func (t *SessionTree) UpdateSession(s *Session) {
	t.mu.Lock()
	t.sessions[s.ID] = cloneSession(s)
	t.mu.Unlock()
	t.notifyChange()
}

// UpdateSessionStructure replaces a session's name, windows and panes from
// a fresh tmux read, keeping the metadata the enrichment poller fills in
// (tool, git info, status, activity) when the fresh read leaves it empty.
// Re-reading a session's structure must not blank that metadata until the
// next poll, or the sidebar flickers and shifts on every pane change.
func (t *SessionTree) UpdateSessionStructure(s *Session) {
	t.mu.Lock()
	fresh := cloneSession(s)
	if old, ok := t.sessions[s.ID]; ok {
		keep := func(dst *string, prev string) {
			if *dst == "" {
				*dst = prev
			}
		}
		keep(&fresh.Status, old.Status)
		keep(&fresh.ToolType, old.ToolType)
		keep(&fresh.GitBranch, old.GitBranch)
		keep(&fresh.GitRepo, old.GitRepo)
		keep(&fresh.WorkingDir, old.WorkingDir)
		keep(&fresh.ActivitySummary, old.ActivitySummary)
		if fresh.ActivityState == "" {
			fresh.ActivityState = old.ActivityState
		}
		if fresh.LastActivity == 0 {
			fresh.LastActivity = old.LastActivity
		}
	}
	t.sessions[s.ID] = fresh
	t.mu.Unlock()
	t.notifyChange()
}

// UpdateSessionMetadata applies a mutation function to a session's metadata
// fields without replacing its windows or panes. The onChange callback is NOT
// called — the caller is responsible for batching notifications.
func (t *SessionTree) UpdateSessionMetadata(id string, fn func(*Session)) {
	t.mu.Lock()
	s, ok := t.sessions[id]
	if ok {
		fn(s)
	}
	t.mu.Unlock()
}

// RemoveSession removes a session from the tree by its ID.
func (t *SessionTree) RemoveSession(id string) {
	t.mu.Lock()
	_, existed := t.sessions[id]
	delete(t.sessions, id)
	t.mu.Unlock()
	if existed {
		t.notifyChange()
	}
}

// GetSession returns a deep copy of the session with the given ID, or nil if not found.
func (t *SessionTree) GetSession(id string) *Session {
	t.mu.RLock()
	s, ok := t.sessions[id]
	t.mu.RUnlock()
	if !ok {
		return nil
	}
	return cloneSession(s)
}

// GetSessions returns deep copies of all sessions, sorted by name.
func (t *SessionTree) GetSessions() []*Session {
	t.mu.RLock()
	result := make([]*Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		result = append(result, cloneSession(s))
	}
	t.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// FindPane searches all sessions for a pane with the given ID and returns the
// session ID it belongs to.
func (t *SessionTree) FindPane(paneID string) (sessionID string, found bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, s := range t.sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.ID == paneID {
					return s.ID, true
				}
			}
		}
	}
	return "", false
}

// ToJSON serializes the full tree to JSON. Sessions are sorted by name for stable output.
func (t *SessionTree) ToJSON() ([]byte, error) {
	sessions := t.GetSessions()
	return json.Marshal(sessions)
}

// AddWindow adds a window to the specified session. If the session does not
// exist, AddWindow is a no-op.
func (t *SessionTree) AddWindow(sessionID string, w *Window) {
	t.mu.Lock()
	s, ok := t.sessions[sessionID]
	if ok {
		s.Windows = append(s.Windows, cloneWindow(w))
		sortWindows(s.Windows)
	}
	t.mu.Unlock()
	if ok {
		t.notifyChange()
	}
}

// RemoveWindow removes a window from the specified session.
func (t *SessionTree) RemoveWindow(sessionID, windowID string) {
	t.mu.Lock()
	s, ok := t.sessions[sessionID]
	changed := false
	if ok {
		for i, w := range s.Windows {
			if w.ID == windowID {
				s.Windows = append(s.Windows[:i], s.Windows[i+1:]...)
				changed = true
				break
			}
		}
	}
	t.mu.Unlock()
	if changed {
		t.notifyChange()
	}
}

// RenameSession renames a session by its ID.
func (t *SessionTree) RenameSession(sessionID, newName string) {
	t.mu.Lock()
	s, ok := t.sessions[sessionID]
	changed := false
	if ok && s.Name != newName {
		s.Name = newName
		changed = true
	}
	t.mu.Unlock()
	if changed {
		t.notifyChange()
	}
}

// RenameWindow renames a window within the specified session.
func (t *SessionTree) RenameWindow(sessionID, windowID, newName string) {
	t.mu.Lock()
	s, ok := t.sessions[sessionID]
	changed := false
	if ok {
		for _, w := range s.Windows {
			if w.ID == windowID {
				w.Name = newName
				changed = true
				break
			}
		}
	}
	t.mu.Unlock()
	if changed {
		t.notifyChange()
	}
}

// --- Parse helpers ---

// ParseSessionList parses the output of:
//
//	tmux list-sessions -F '#{session_id}\t#{session_name}\t#{session_activity}\t#{session_attached}'
func ParseSessionList(output string) ([]*Session, error) {
	var sessions []*Session
	for _, line := range splitLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid session line: %q", line)
		}
		s := &Session{
			ID:   parts[0],
			Name: parts[1],
		}
		if len(parts) >= 3 {
			if v, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				s.LastActivity = v
			}
		}
		if len(parts) >= 4 {
			if v, err := strconv.Atoi(parts[3]); err == nil {
				s.Attached = v
			}
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// ParseWindowList parses the output of:
//
//	tmux list-windows -F '#{window_id}\t#{window_name}\t#{window_index}\t#{window_active}\t#{window_zoomed_flag}'
func ParseWindowList(output string) ([]*Window, error) {
	var windows []*Window
	for _, line := range splitLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			return nil, fmt.Errorf("invalid window line: %q", line)
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid window index %q: %w", parts[2], err)
		}
		active, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("invalid window active flag %q: %w", parts[3], err)
		}
		var zoomed bool
		if len(parts) >= 5 {
			if z, err := strconv.Atoi(parts[4]); err == nil {
				zoomed = z == 1
			}
		}
		windows = append(windows, &Window{
			ID:     parts[0],
			Name:   parts[1],
			Index:  idx,
			Active: active == 1,
			Zoomed: zoomed,
		})
	}
	sortWindows(windows)
	return windows, nil
}

// ParsePaneList parses the output of:
//
//	tmux list-panes -F '#{pane_id}\t#{pane_width}\t#{pane_height}\t#{pane_active}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_pid}'
func ParsePaneList(output string) ([]*Pane, error) {
	var panes []*Pane
	for _, line := range splitLines(output) {
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			return nil, fmt.Errorf("invalid pane line: %q", line)
		}
		width, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid pane width %q: %w", parts[1], err)
		}
		height, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid pane height %q: %w", parts[2], err)
		}
		active, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("invalid pane active flag %q: %w", parts[3], err)
		}
		p := &Pane{
			ID:     parts[0],
			Width:  width,
			Height: height,
			Active: active == 1,
		}
		if len(parts) >= 5 {
			p.CurrentCommand = parts[4]
		}
		if len(parts) >= 6 {
			p.CurrentPath = parts[5]
		}
		if len(parts) >= 7 {
			if v, err := strconv.Atoi(parts[6]); err == nil {
				p.Pid = v
			}
		}
		panes = append(panes, p)
	}
	return panes, nil
}

// --- internal helpers ---

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func sortWindows(windows []*Window) {
	sort.Slice(windows, func(i, j int) bool {
		return windows[i].Index < windows[j].Index
	})
}

func cloneSession(s *Session) *Session {
	c := &Session{
		ID:              s.ID,
		Name:            s.Name,
		Windows:         make([]*Window, len(s.Windows)),
		Status:          s.Status,
		LastActivity:    s.LastActivity,
		ToolType:        s.ToolType,
		GitBranch:       s.GitBranch,
		GitRepo:         s.GitRepo,
		WorkingDir:      s.WorkingDir,
		ActivityState:   s.ActivityState,
		ActivitySummary: s.ActivitySummary,
		Attached:        s.Attached,
	}
	for i, w := range s.Windows {
		c.Windows[i] = cloneWindow(w)
	}
	return c
}

func cloneWindow(w *Window) *Window {
	c := &Window{
		ID:     w.ID,
		Name:   w.Name,
		Index:  w.Index,
		Active: w.Active,
		Zoomed: w.Zoomed,
		Panes:  make([]*Pane, len(w.Panes)),
	}
	for i, p := range w.Panes {
		c.Panes[i] = clonePane(p)
	}
	return c
}

func clonePane(p *Pane) *Pane {
	return &Pane{
		ID:             p.ID,
		Width:          p.Width,
		Height:         p.Height,
		Active:         p.Active,
		CurrentCommand: p.CurrentCommand,
		CurrentPath:    p.CurrentPath,
		Pid:            p.Pid,
	}
}
