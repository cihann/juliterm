package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTrackExcerptStability(t *testing.T) {
	m := NewManager("")

	if m.trackExcerpt("$1", "prompt>") {
		t.Fatal("first sighting should not be stable")
	}
	if m.trackExcerpt("$1", "prompt>") {
		t.Fatal("one repeat should not be stable yet")
	}
	if !m.trackExcerpt("$1", "prompt>") {
		t.Fatal("two repeats should be stable")
	}
	if m.trackExcerpt("$1", "new output") {
		t.Fatal("changed excerpt should reset stability")
	}
	if m.trackExcerpt("$2", "") || m.trackExcerpt("$2", "") || m.trackExcerpt("$2", "") {
		t.Fatal("empty excerpt should never be stable")
	}
}

// The poller tracks excerpts while removeSession can run from the watcher or
// HTTP handlers; run both concurrently under -race.
func TestTrackExcerptConcurrentWithRemove(t *testing.T) {
	m := NewManager("")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			m.trackExcerpt("$1", "same")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			m.removeSession("$1")
		}
	}()
	wg.Wait()
}

// fakeTmux writes a stand-in tmux script that answers the queries used by
// discoverSessionDetails for one session ($1, one window, one pane), and
// counts list-windows calls in a file. Each list-windows call takes 100ms.
func fakeTmux(t *testing.T) (path, countFile string) {
	t.Helper()
	dir := t.TempDir()
	countFile = filepath.Join(dir, "count")
	path = filepath.Join(dir, "tmux")
	script := `#!/bin/sh
[ "$1" = "-u" ] && shift
case "$1" in
list-sessions) printf '$1\tmain\t0\t0\n' ;;
list-windows) echo x >> "` + countFile + `"; sleep 0.1; printf '@1\tshell\t0\t1\t0\n' ;;
list-panes) printf '%%1\t80\t24\t1\tzsh\t/tmp\t123\n' ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, countFile
}

func countLines(t *testing.T, file string) int {
	t.Helper()
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

func TestScheduleRediscoverCoalesces(t *testing.T) {
	tmuxPath, countFile := fakeTmux(t)
	m := NewManager(tmuxPath)
	m.ctx, m.cancel = context.WithCancel(context.Background())
	defer m.cancel()
	m.clients["$1"] = NewControlClient(m.ctx, "$1", tmuxPath)

	// A burst of window events while the first rediscovery runs: they
	// collapse into a single rerun.
	for i := 0; i < 5; i++ {
		m.scheduleRediscover("$1", true)
	}
	m.scheduleRediscover("$1", false)

	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		_, running := m.rediscover["$1"]
		m.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rediscovery did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if n := countLines(t, countFile); n != 2 {
		t.Fatalf("rediscovery ran %d times, want 2 (one run plus one coalesced rerun)", n)
	}
	if s := m.tree.GetSession("$1"); s == nil || len(s.Windows) != 1 {
		t.Fatalf("tree not updated: %+v", s)
	}
}

func TestScheduleRediscoverWithoutStartIsNoOp(t *testing.T) {
	tmuxPath, countFile := fakeTmux(t)
	m := NewManager(tmuxPath)
	m.ctx, m.cancel = context.WithCancel(context.Background())
	defer m.cancel()

	// Window close/rename only ask a running rediscovery to rerun.
	m.scheduleRediscover("$1", false)
	time.Sleep(50 * time.Millisecond)
	if n := countLines(t, countFile); n != 0 {
		t.Fatalf("rediscovery ran %d times, want 0", n)
	}
}

func TestScheduleRediscoverSkipsRemovedSession(t *testing.T) {
	tmuxPath, _ := fakeTmux(t)
	m := NewManager(tmuxPath)
	m.ctx, m.cancel = context.WithCancel(context.Background())
	defer m.cancel()

	// No control client for $1: the session was removed, so the result of
	// the rediscovery must not put it back into the tree.
	m.scheduleRediscover("$1", true)
	time.Sleep(300 * time.Millisecond)
	if s := m.tree.GetSession("$1"); s != nil {
		t.Fatalf("removed session reappeared: %+v", s)
	}
}
