package tmux

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Tree reconciliation: a safety net under the event-driven updates. tmux
// control mode doesn't describe every structural change precisely (panes
// moved between windows, active pane changes), and an event can be missed,
// so every metadata tick compares tmux's real windows and panes with the
// tree and re-reads any session that differs. The tree then can't stay
// wrong for longer than one tick.

// structureFormat lists, for every pane, what the sidebar shows about it.
const structureFormat = "#{session_id}\t#{window_id}\t#{window_index}\t#{window_name}\t#{window_active}\t#{window_zoomed_flag}\t#{pane_id}\t#{pane_active}"

// tmuxStructure parses `list-panes -a -F structureFormat` output into a
// per-session signature comparable with sessionSignature.
func tmuxStructure(output string) map[string]string {
	lines := make(map[string][]string)
	for _, line := range splitLines(output) {
		p := strings.Split(line, "\t")
		if len(p) < 8 {
			continue
		}
		lines[p[0]] = append(lines[p[0]], signatureLine(p[1], p[2], p[3], p[4] == "1", p[5] == "1", p[6], p[7] == "1"))
	}
	sigs := make(map[string]string, len(lines))
	for id, l := range lines {
		sort.Strings(l)
		sigs[id] = strings.Join(l, "\n")
	}
	return sigs
}

// sessionSignature is the tree's view of a session in the same form.
func sessionSignature(s *Session) string {
	var l []string
	for _, w := range s.Windows {
		for _, p := range w.Panes {
			l = append(l, signatureLine(w.ID, fmt.Sprint(w.Index), w.Name, w.Active, w.Zoomed, p.ID, p.Active))
		}
	}
	sort.Strings(l)
	return strings.Join(l, "\n")
}

func signatureLine(windowID, index, name string, windowActive, zoomed bool, paneID string, paneActive bool) string {
	return fmt.Sprintf("%s|%s|%s|%t|%t|%s|%t", windowID, index, name, windowActive, zoomed, paneID, paneActive)
}

// staleSessions returns the IDs of tree sessions whose windows or panes
// differ from tmux's.
func staleSessions(tree []*Session, actual map[string]string) []string {
	var stale []string
	for _, s := range tree {
		if want, ok := actual[s.ID]; ok && want != sessionSignature(s) {
			stale = append(stale, s.ID)
		}
	}
	return stale
}

// reconcileTree re-reads every session whose structure has drifted from tmux.
// Sessions that appeared or vanished are handled by syncSessions.
func (m *Manager) reconcileTree() {
	out, err := exec.CommandContext(m.ctx, m.tmuxPath, "-u", "list-panes", "-a", "-F", structureFormat).Output()
	if err != nil {
		return
	}
	for _, id := range staleSessions(m.tree.GetSessions(), tmuxStructure(string(out))) {
		m.scheduleRediscover(id, true)
	}
}
