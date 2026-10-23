package tmux

import (
	"reflect"
	"testing"
)

func TestStaleSessionsAfterJoinPane(t *testing.T) {
	// The tree still has three windows; tmux joined two of them into @2.
	tree := []*Session{{
		ID: "$1",
		Windows: []*Window{
			{ID: "@1", Index: 1, Name: "zsh", Active: true, Panes: []*Pane{{ID: "%45", Active: true}}},
			{ID: "@2", Index: 2, Name: "claude", Panes: []*Pane{{ID: "%8", Active: true}, {ID: "%9"}}},
			{ID: "@3", Index: 3, Name: "claude", Panes: []*Pane{{ID: "%43", Active: true}, {ID: "%25"}}},
		},
	}, {
		ID:      "$2",
		Windows: []*Window{{ID: "@7", Index: 1, Name: "fennec", Active: true, Panes: []*Pane{{ID: "%21", Active: true}}}},
	}}
	out := "$1\t@2\t2\tclaude\t1\t0\t%8\t1\n" +
		"$1\t@2\t2\tclaude\t1\t0\t%9\t0\n" +
		"$1\t@2\t2\tclaude\t1\t0\t%43\t0\n" +
		"$1\t@2\t2\tclaude\t1\t0\t%25\t0\n" +
		"$2\t@7\t1\tfennec\t1\t0\t%21\t1\n"

	got := staleSessions(tree, tmuxStructure(out))
	if want := []string{"$1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stale = %v, want %v", got, want)
	}
}

func TestStaleSessionsNoticesActivePaneAndZoom(t *testing.T) {
	tree := []*Session{{
		ID:      "$1",
		Windows: []*Window{{ID: "@1", Index: 0, Name: "w", Active: true, Panes: []*Pane{{ID: "%1", Active: true}, {ID: "%2"}}}},
	}}
	for name, out := range map[string]string{
		"active pane": "$1\t@1\t0\tw\t1\t0\t%1\t0\n$1\t@1\t0\tw\t1\t0\t%2\t1\n",
		"zoomed":      "$1\t@1\t0\tw\t1\t1\t%1\t1\n$1\t@1\t0\tw\t1\t1\t%2\t0\n",
	} {
		if got := staleSessions(tree, tmuxStructure(out)); len(got) != 1 {
			t.Errorf("%s: stale = %v, want [$1]", name, got)
		}
	}
	same := "$1\t@1\t0\tw\t1\t0\t%2\t0\n$1\t@1\t0\tw\t1\t0\t%1\t1\n"
	if got := staleSessions(tree, tmuxStructure(same)); len(got) != 0 {
		t.Errorf("unchanged: stale = %v, want none", got)
	}
}

func TestStaleSessionsIgnoresSessionsTmuxNoLongerHas(t *testing.T) {
	tree := []*Session{{ID: "$9", Windows: []*Window{{ID: "@1", Panes: []*Pane{{ID: "%1"}}}}}}
	if got := staleSessions(tree, tmuxStructure("")); len(got) != 0 {
		t.Fatalf("stale = %v, want none (syncSessions removes vanished sessions)", got)
	}
}

func TestUpdateSessionStructureKeepsMetadata(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(&Session{
		ID: "$1", Name: "zenith", ToolType: "claude", GitRepo: "arpiq/beaconwright", GitBranch: "main",
		WorkingDir: "/w", Status: "running", LastActivity: 42,
		Windows: []*Window{{ID: "@1", Panes: []*Pane{{ID: "%1"}}}},
	})

	// A structural re-read knows windows and panes, not metadata.
	tree.UpdateSessionStructure(&Session{
		ID: "$1", Name: "zenith",
		Windows: []*Window{{ID: "@1", Panes: []*Pane{{ID: "%1"}, {ID: "%2", Active: true}}}},
	})

	got := tree.GetSession("$1")
	if got.ToolType != "claude" || got.GitRepo != "arpiq/beaconwright" || got.GitBranch != "main" ||
		got.WorkingDir != "/w" || got.Status != "running" || got.LastActivity != 42 {
		t.Errorf("metadata lost: %+v", got)
	}
	if n := len(got.Windows[0].Panes); n != 2 {
		t.Errorf("panes = %d, want 2", n)
	}
}
