// Package pty manages PTY sessions that run "tmux attach-session" for native
// terminal I/O. Each browser connection gets its own PTY, giving it the exact
// same byte stream a real terminal emulator would receive.
package pty

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
)

// Session wraps a PTY running "tmux attach-session -t <session>".
type Session struct {
	cmd       *exec.Cmd
	file      *os.File // PTY master fd
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
	closed    bool // guarded by mu, for Write/Resize guards
}

// Attach spawns "tmux attach-session -t <sessionName>" in a new PTY with the
// given initial dimensions. The caller owns the returned Session and must call
// Close when finished.
func Attach(tmuxPath, sessionName string, cols, rows uint16) (*Session, error) {
	if tmuxPath == "" {
		tmuxPath = "tmux"
	}

	// -u: tmux assumes a UTF-8 terminal whatever the locale. Under launchd
	// there is no LANG, and tmux would otherwise draw line characters and
	// non-ASCII text with ASCII fallbacks or underscores.
	cmd := exec.Command(tmuxPath, "-u", "attach-session", "-t", sessionName)
	cmd.Env = append(withoutTmuxEnv(os.Environ()), "TERM=xterm-256color")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, fmt.Errorf("pty: start tmux attach: %w", err)
	}

	s := &Session{
		cmd:  cmd,
		file: f,
		done: make(chan struct{}),
	}

	// Wait for process exit in background.
	go func() {
		_ = cmd.Wait()
		close(s.done)
	}()

	return s, nil
}

// Read reads raw terminal output from the PTY.
func (s *Session) Read(buf []byte) (int, error) {
	return s.file.Read(buf)
}

// Write sends raw keyboard input to the PTY.
func (s *Session) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, fmt.Errorf("pty: session closed")
	}
	return s.file.Write(data)
}

// Resize changes the PTY window size, which tmux picks up immediately.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("pty: session closed")
	}

	ws := struct {
		Row, Col, Xpixel, Ypixel uint16
	}{rows, cols, 0, 0}

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		s.file.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return fmt.Errorf("pty: resize: %v", errno)
	}
	return nil
}

// Done returns a channel that closes when the underlying process exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Close terminates the PTY session. tmux detaches cleanly when the client
// PTY closes, so the tmux session itself survives. Close is idempotent and
// safe to call from multiple goroutines.
func (s *Session) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		// Close PTY fd — this causes tmux to detach and unblocks
		// any goroutine blocked on Read().
		closeErr = s.file.Close()

		// If the process hasn't exited yet, signal it.
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Signal(syscall.SIGHUP)
		}
	})
	return closeErr
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
