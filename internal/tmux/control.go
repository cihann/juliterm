// Package tmux provides a client for tmux's control mode protocol.
// A single ControlClient manages one tmux control-mode subprocess attached
// to a specific session, multiplexing commands and notifications over
// stdin/stdout.
package tmux

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Event types
// ---------------------------------------------------------------------------

// EventType classifies asynchronous notifications from tmux control mode.
type EventType int

const (
	EventSessionsChanged EventType = iota
	EventWindowAdd
	EventWindowClose
	EventWindowRenamed
	EventPaneOutput
	EventPanePause
	EventPaneContinue
	EventPaneModeChanged
	// EventLayoutChanged reports a structural change the event itself doesn't
	// describe (panes split, closed or moved, active window or pane changed,
	// windows changing in other sessions); the session should be re-read.
	EventLayoutChanged
)

// Event represents a single asynchronous notification from tmux.
type Event struct {
	Type     EventType
	PaneID   string // e.g. "%0"
	WindowID string // e.g. "@0"
	Name     string // window name for EventWindowRenamed
	Data     []byte // unescaped output bytes for EventPaneOutput
	Delay    int    // milliseconds, only set for %extended-output
}

// ---------------------------------------------------------------------------
// ControlClient
// ---------------------------------------------------------------------------

// ControlClient manages a single tmux control-mode subprocess. There should
// be exactly one ControlClient per tmux session. All methods are safe for
// concurrent use.
type ControlClient struct {
	sessionID string
	tmuxPath  string

	cmd   *exec.Cmd
	stdin io.WriteCloser

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex // serialises stdin writes
	onOutput func(paneID string, data []byte)
	onEvent  func(event Event)

	wg      sync.WaitGroup // tracks the read-loop goroutine
	started bool
}

// NewControlClient creates a ControlClient for the given tmux session.
// tmuxPath is the path to the tmux binary; if empty it defaults to "tmux".
// The provided context controls the lifetime of the subprocess: cancelling
// it is equivalent to calling Stop.
func NewControlClient(ctx context.Context, sessionID string, tmuxPath string) *ControlClient {
	if tmuxPath == "" {
		tmuxPath = "tmux"
	}
	childCtx, cancel := context.WithCancel(ctx)
	return &ControlClient{
		sessionID: sessionID,
		tmuxPath:  tmuxPath,
		ctx:       childCtx,
		cancel:    cancel,
	}
}

// OnOutput sets the callback invoked for every pane output event (%output
// and %extended-output). The data slice is already unescaped. The callback
// is called from the read-loop goroutine; it must not block.
func (c *ControlClient) OnOutput(fn func(paneID string, data []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onOutput = fn
}

// OnEvent sets the callback invoked for every non-output notification.
// Output events are also delivered here (as EventPaneOutput) in addition
// to the OnOutput callback. The callback is called from the read-loop
// goroutine; it must not block.
func (c *ControlClient) OnEvent(fn func(event Event)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvent = fn
}

// Start spawns the tmux control-mode subprocess and begins reading its
// stdout in a background goroutine. It returns an error if the subprocess
// cannot be started.
func (c *ControlClient) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("tmux: control client already started")
	}

	// -u keeps tmux from replacing non-ASCII characters (including the
	// separators in our format strings) with "_" when there is no UTF-8
	// locale, as under launchd.
	// -f no-output: juli only uses control mode for structure events
	// (windows, sessions); terminal output reaches the browser through its
	// own PTY. Without it tmux streams every pane's output here as %output,
	// which nothing consumes.
	c.cmd = exec.CommandContext(c.ctx, c.tmuxPath, "-u", "-C", "attach-session", "-f", "no-output", "-t", c.sessionID)
	c.cmd.Env = withoutTmuxEnv(os.Environ())

	stdinPipe, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("tmux: stdin pipe: %w", err)
	}
	c.stdin = stdinPipe

	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return fmt.Errorf("tmux: stdout pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		stdinPipe.Close()
		return fmt.Errorf("tmux: start: %w", err)
	}

	c.started = true
	c.wg.Add(1)
	go c.readLoop(stdoutPipe)

	return nil
}

// Stop cancels the context, kills the subprocess, and waits for the
// read-loop goroutine to finish. It is safe to call multiple times.
func (c *ControlClient) Stop() error {
	c.cancel()

	c.mu.Lock()
	started := c.started
	c.mu.Unlock()

	if !started {
		return nil
	}

	// Close stdin so tmux sees EOF.
	c.stdin.Close()

	// Wait for the read loop to drain and exit.
	c.wg.Wait()

	// Wait collects the exit status; ignore errors from signal kills.
	_ = c.cmd.Wait()

	return nil
}

// Wait blocks until the read-loop goroutine exits. It is safe to call
// concurrently with Stop. If the client was never started, Wait returns
// immediately.
func (c *ControlClient) Wait() {
	c.mu.Lock()
	started := c.started
	c.mu.Unlock()
	if !started {
		return
	}
	c.wg.Wait()
}

// SendCommand writes a tmux command to the control-mode subprocess stdin.
// A trailing newline is appended automatically. The caller must not send
// an empty string — an empty line detaches the control client.
func (c *ControlClient) SendCommand(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("tmux: refusing to send empty command (would detach)")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("tmux: control client not started")
	}

	_, err := io.WriteString(c.stdin, cmd+"\n")
	if err != nil {
		return fmt.Errorf("tmux: send command: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Read loop
// ---------------------------------------------------------------------------

func (c *ControlClient) readLoop(r io.Reader) {
	defer c.wg.Done()

	scanner := bufio.NewScanner(r)
	// Raise the buffer size — pane output lines can be large.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		if line[0] != '%' {
			// Non-notification line (should not happen in control mode).
			continue
		}
		c.dispatchLine(line)
	}
	if err := scanner.Err(); err != nil {
		// Context cancellation is expected; only log real errors.
		if c.ctx.Err() == nil {
			log.Printf("tmux: read loop error: %v", err)
		}
	}
}

func (c *ControlClient) dispatchLine(line string) {
	switch {
	case strings.HasPrefix(line, "%output "):
		c.handleOutput(line[len("%output "):], 0)

	case strings.HasPrefix(line, "%extended-output "):
		c.handleExtendedOutput(line[len("%extended-output "):])

	case line == "%sessions-changed":
		c.emit(Event{Type: EventSessionsChanged})

	case strings.HasPrefix(line, "%window-add "):
		wid := strings.TrimPrefix(line, "%window-add ")
		c.emit(Event{Type: EventWindowAdd, WindowID: wid})

	case strings.HasPrefix(line, "%window-close "):
		wid := strings.TrimPrefix(line, "%window-close ")
		c.emit(Event{Type: EventWindowClose, WindowID: wid})

	case strings.HasPrefix(line, "%window-renamed "):
		// Format: %window-renamed @WINDOW_ID NAME
		rest := line[len("%window-renamed "):]
		wid, name := splitFirst(rest)
		c.emit(Event{Type: EventWindowRenamed, WindowID: wid, Name: name})

	case strings.HasPrefix(line, "%pause "):
		pid := strings.TrimPrefix(line, "%pause ")
		c.emit(Event{Type: EventPanePause, PaneID: pid})

	case strings.HasPrefix(line, "%continue "):
		pid := strings.TrimPrefix(line, "%continue ")
		c.emit(Event{Type: EventPaneContinue, PaneID: pid})

	case strings.HasPrefix(line, "%pane-mode-changed "):
		pid := strings.TrimPrefix(line, "%pane-mode-changed ")
		c.emit(Event{Type: EventPaneModeChanged, PaneID: pid})

	case line == "%layout-change",
		strings.HasPrefix(line, "%layout-change "),
		strings.HasPrefix(line, "%window-pane-changed "),
		strings.HasPrefix(line, "%session-window-changed "),
		strings.HasPrefix(line, "%unlinked-window-add "),
		strings.HasPrefix(line, "%unlinked-window-close "),
		strings.HasPrefix(line, "%unlinked-window-renamed "):
		// Panes split, closed or moved between windows (join-pane), the
		// active window or pane changed, or windows changed outside this
		// session. None carries enough to patch the tree, so re-read it.
		c.emit(Event{Type: EventLayoutChanged})

	case strings.HasPrefix(line, "%begin "), strings.HasPrefix(line, "%end "):
		// Command response delimiters — ignored for now.

	case strings.HasPrefix(line, "%session-changed "),
		strings.HasPrefix(line, "%client-session-changed "),
		strings.HasPrefix(line, "%client-detached "),
		strings.HasPrefix(line, "%config-error "),
		strings.HasPrefix(line, "%subscription-changed "),
		strings.HasPrefix(line, "%exit"):
		// Known but unneeded notifications — silently ignore.

	default:
		// Unknown % notification; log and move on.
		log.Printf("tmux: unhandled notification: %s", line)
	}
}

// handleOutput parses: %PANE_ID DATA
func (c *ControlClient) handleOutput(rest string, delay int) {
	paneID, escaped := splitFirst(rest)
	if paneID == "" {
		return
	}
	data := UnescapeOctal(escaped)

	c.mu.Lock()
	outFn := c.onOutput
	evFn := c.onEvent
	c.mu.Unlock()

	if outFn != nil {
		outFn(paneID, data)
	}
	if evFn != nil {
		evFn(Event{
			Type:   EventPaneOutput,
			PaneID: paneID,
			Data:   data,
			Delay:  delay,
		})
	}
}

// handleExtendedOutput parses: %PANE_ID DELAY : DATA
func (c *ControlClient) handleExtendedOutput(rest string) {
	paneID, afterPane := splitFirst(rest)
	if paneID == "" {
		return
	}
	delayStr, afterDelay := splitFirst(afterPane)
	delay, _ := strconv.Atoi(delayStr)

	// After delay there should be a colon separator then the data.
	data := afterDelay
	if strings.HasPrefix(data, ": ") {
		data = data[2:]
	} else if strings.HasPrefix(data, ":") {
		data = data[1:]
	}

	c.handleOutput(paneID+" "+data, delay)
}

func (c *ControlClient) emit(e Event) {
	c.mu.Lock()
	fn := c.onEvent
	c.mu.Unlock()

	if fn != nil {
		fn(e)
	}
}

// ---------------------------------------------------------------------------
// Octal unescaping
// ---------------------------------------------------------------------------

// UnescapeOctal decodes tmux's octal escape encoding. Characters below
// 0x20 and backslash itself are represented as \NNN (3-digit octal).
// For example \015\012 decodes to \r\n and \134 decodes to \.
func UnescapeOctal(s string) []byte {
	// Fast path: if there are no backslashes, return as-is.
	if strings.IndexByte(s, '\\') < 0 {
		return []byte(s)
	}

	buf := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+3 < len(s) {
			o1, ok1 := octalDigit(s[i+1])
			o2, ok2 := octalDigit(s[i+2])
			o3, ok3 := octalDigit(s[i+3])
			if ok1 && ok2 && ok3 {
				buf = append(buf, byte(o1*64+o2*8+o3))
				i += 4
				continue
			}
		}
		buf = append(buf, s[i])
		i++
	}
	return buf
}

func octalDigit(b byte) (int, bool) {
	if b >= '0' && b <= '7' {
		return int(b - '0'), true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// splitFirst splits s on the first space, returning (before, after).
// If there is no space, after is empty.
func splitFirst(s string) (string, string) {
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// withoutTmuxEnv returns env minus TMUX and TMUX_PANE. When juli itself runs
// inside tmux, an inherited TMUX makes tmux refuse to nest the attach.
func withoutTmuxEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
