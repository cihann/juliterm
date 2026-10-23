package tmux

import (
	"fmt"
	"sync"
	"testing"
)

// ===========================================================================
// 1. UnescapeOctal
// ===========================================================================

func TestUnescapeOctal_BasicCRLF(t *testing.T) {
	got := UnescapeOctal(`\015\012`)
	want := "\r\n"
	if string(got) != want {
		t.Errorf("UnescapeOctal(`\\015\\012`) = %q, want %q", got, want)
	}
}

func TestUnescapeOctal_Backslash(t *testing.T) {
	got := UnescapeOctal(`\134`)
	want := `\`
	if string(got) != want {
		t.Errorf("UnescapeOctal(`\\134`) = %q, want %q", got, want)
	}
}

func TestUnescapeOctal_Mixed(t *testing.T) {
	got := UnescapeOctal(`hello\015\012world`)
	want := "hello\r\nworld"
	if string(got) != want {
		t.Errorf("UnescapeOctal mixed = %q, want %q", got, want)
	}
}

func TestUnescapeOctal_NoEscapes(t *testing.T) {
	input := "hello world"
	got := UnescapeOctal(input)
	if string(got) != input {
		t.Errorf("UnescapeOctal no escapes = %q, want %q", got, input)
	}
}

func TestUnescapeOctal_AllPrintable(t *testing.T) {
	input := "abcdefghijklmnopqrstuvwxyz0123456789"
	got := UnescapeOctal(input)
	if string(got) != input {
		t.Errorf("UnescapeOctal printable = %q, want %q", got, input)
	}
}

func TestUnescapeOctal_Empty(t *testing.T) {
	got := UnescapeOctal("")
	if len(got) != 0 {
		t.Errorf("UnescapeOctal empty = %q, want empty", got)
	}
}

func TestUnescapeOctal_LoneBackslashAtEnd(t *testing.T) {
	// A trailing backslash without 3 octal digits should pass through.
	got := UnescapeOctal(`hello\`)
	want := `hello\`
	if string(got) != want {
		t.Errorf("UnescapeOctal lone backslash = %q, want %q", got, want)
	}
}

func TestUnescapeOctal_BackslashFollowedByNonOctal(t *testing.T) {
	got := UnescapeOctal(`\xyz`)
	want := `\xyz`
	if string(got) != want {
		t.Errorf("UnescapeOctal non-octal = %q, want %q", got, want)
	}
}

func TestUnescapeOctal_MultiByte_UTF8(t *testing.T) {
	// ü in UTF-8 is 0xC3 0xBC → octal \303\274
	got := UnescapeOctal(`\303\274`)
	want := "ü"
	if string(got) != want {
		t.Errorf("UnescapeOctal UTF-8 = %q, want %q", got, want)
	}
}

// ===========================================================================
// 2. splitFirst helper
// ===========================================================================

func TestSplitFirst(t *testing.T) {
	tests := []struct {
		input      string
		wantBefore string
		wantAfter  string
	}{
		{"hello world", "hello", "world"},
		{"one two three", "one", "two three"},
		{"nospace", "nospace", ""},
		{"", "", ""},
		{" leading", "", "leading"},
	}
	for _, tt := range tests {
		before, after := splitFirst(tt.input)
		if before != tt.wantBefore || after != tt.wantAfter {
			t.Errorf("splitFirst(%q) = (%q, %q), want (%q, %q)",
				tt.input, before, after, tt.wantBefore, tt.wantAfter)
		}
	}
}

// ===========================================================================
// 3. ParseSessionList, ParseWindowList, ParsePaneList
// ===========================================================================

func TestParseSessionList_Typical(t *testing.T) {
	output := "$0\tmain\t1711111111\t1\n$1\tdev\t1711111222\t0\n$2\tlogs\t1711111333\t1\n"
	sessions, err := ParseSessionList(output)
	if err != nil {
		t.Fatalf("ParseSessionList error: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "$0" || sessions[0].Name != "main" {
		t.Errorf("session[0] = {%q, %q}, want {$0, main}", sessions[0].ID, sessions[0].Name)
	}
	if sessions[0].LastActivity != 1711111111 {
		t.Errorf("session[0].LastActivity = %d, want 1711111111", sessions[0].LastActivity)
	}
	if sessions[0].Attached != 1 {
		t.Errorf("session[0].Attached = %d, want 1", sessions[0].Attached)
	}
	if sessions[1].Attached != 0 {
		t.Errorf("session[1].Attached = %d, want 0", sessions[1].Attached)
	}
	if sessions[2].ID != "$2" || sessions[2].Name != "logs" {
		t.Errorf("session[2] = {%q, %q}, want {$2, logs}", sessions[2].ID, sessions[2].Name)
	}
}

func TestParseSessionList_Empty(t *testing.T) {
	sessions, err := ParseSessionList("")
	if err != nil {
		t.Fatalf("ParseSessionList error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestParseSessionList_MinimalTwoFields(t *testing.T) {
	// Only ID and name (no activity/attached) should still parse.
	output := "$0\tmain\n"
	sessions, err := ParseSessionList(output)
	if err != nil {
		t.Fatalf("ParseSessionList error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Name != "main" {
		t.Errorf("unexpected: %+v", sessions)
	}
}

func TestParseSessionList_Malformed(t *testing.T) {
	// No tab separator means only 1 part.
	_, err := ParseSessionList("malformedline")
	if err == nil {
		t.Error("expected error for malformed session line")
	}
}

func TestParseWindowList_Typical(t *testing.T) {
	output := "@0\tbash\t0\t1\t0\n@1\tvim\t1\t0\t1\n"
	windows, err := ParseWindowList(output)
	if err != nil {
		t.Fatalf("ParseWindowList error: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	// Sorted by index.
	if windows[0].ID != "@0" || windows[0].Name != "bash" || windows[0].Index != 0 || !windows[0].Active || windows[0].Zoomed {
		t.Errorf("window[0] unexpected: %+v", windows[0])
	}
	if windows[1].ID != "@1" || windows[1].Name != "vim" || windows[1].Index != 1 || windows[1].Active || !windows[1].Zoomed {
		t.Errorf("window[1] unexpected: %+v", windows[1])
	}
}

func TestParseWindowList_WithoutZoomedFlag(t *testing.T) {
	// Backward compat: 4-field format without zoomed flag.
	output := "@0\tbash\t0\t1\n"
	windows, err := ParseWindowList(output)
	if err != nil {
		t.Fatalf("ParseWindowList error: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].Zoomed {
		t.Error("expected Zoomed=false when field is missing")
	}
}

func TestParseWindowList_Empty(t *testing.T) {
	windows, err := ParseWindowList("")
	if err != nil {
		t.Fatalf("ParseWindowList error: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("expected 0 windows, got %d", len(windows))
	}
}

func TestParseWindowList_Malformed(t *testing.T) {
	_, err := ParseWindowList("@0\tbash")
	if err == nil {
		t.Error("expected error for malformed window line")
	}
}

func TestParseWindowList_BadIndex(t *testing.T) {
	_, err := ParseWindowList("@0\tbash\tabc\t1")
	if err == nil {
		t.Error("expected error for non-numeric index")
	}
}

func TestParsePaneList_Typical(t *testing.T) {
	output := "%0\t80\t24\t1\tzsh\t/home/user\t12345\n%1\t80\t24\t0\tvim\t/home/user/project\t12346\n"
	panes, err := ParsePaneList(output)
	if err != nil {
		t.Fatalf("ParsePaneList error: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}
	if panes[0].ID != "%0" || panes[0].Width != 80 || panes[0].Height != 24 || !panes[0].Active {
		t.Errorf("pane[0] unexpected: %+v", panes[0])
	}
	if panes[0].CurrentCommand != "zsh" {
		t.Errorf("pane[0].CurrentCommand = %q, want zsh", panes[0].CurrentCommand)
	}
	if panes[0].CurrentPath != "/home/user" {
		t.Errorf("pane[0].CurrentPath = %q, want /home/user", panes[0].CurrentPath)
	}
	if panes[0].Pid != 12345 {
		t.Errorf("pane[0].Pid = %d, want 12345", panes[0].Pid)
	}
	if panes[1].ID != "%1" || panes[1].Active {
		t.Errorf("pane[1] unexpected: %+v", panes[1])
	}
}

func TestParsePaneList_MinimalFourFields(t *testing.T) {
	// Only basic fields (no command/path/pid) should still parse.
	output := "%0\t80\t24\t1\n"
	panes, err := ParsePaneList(output)
	if err != nil {
		t.Fatalf("ParsePaneList error: %v", err)
	}
	if len(panes) != 1 || panes[0].ID != "%0" || panes[0].CurrentCommand != "" {
		t.Errorf("unexpected: %+v", panes[0])
	}
}

func TestParsePaneList_PathWithSpaces(t *testing.T) {
	output := "%0\t80\t24\t1\tzsh\t/home/user/my project\t12345\n"
	panes, err := ParsePaneList(output)
	if err != nil {
		t.Fatalf("ParsePaneList error: %v", err)
	}
	if panes[0].CurrentPath != "/home/user/my project" {
		t.Errorf("pane[0].CurrentPath = %q, want path with spaces", panes[0].CurrentPath)
	}
}

func TestParsePaneList_Empty(t *testing.T) {
	panes, err := ParsePaneList("")
	if err != nil {
		t.Fatalf("ParsePaneList error: %v", err)
	}
	if len(panes) != 0 {
		t.Errorf("expected 0 panes, got %d", len(panes))
	}
}

func TestParsePaneList_Malformed(t *testing.T) {
	_, err := ParsePaneList("%0\t80")
	if err == nil {
		t.Error("expected error for malformed pane line")
	}
}

func TestParsePaneList_BadWidth(t *testing.T) {
	_, err := ParsePaneList("%0\tabc\t24\t1")
	if err == nil {
		t.Error("expected error for non-numeric width")
	}
}

// ===========================================================================
// 4. SessionTree
// ===========================================================================

func makeTestSession(id, name string, paneIDs ...string) *Session {
	var panes []*Pane
	for _, pid := range paneIDs {
		panes = append(panes, &Pane{ID: pid, Width: 80, Height: 24})
	}
	return &Session{
		ID:   id,
		Name: name,
		Windows: []*Window{
			{ID: "@0", Name: "bash", Index: 0, Panes: panes},
		},
	}
}

func TestSessionTree_UpdateAndGet(t *testing.T) {
	tree := NewSessionTree()
	s := makeTestSession("$0", "main", "%0")
	tree.UpdateSession(s)

	got := tree.GetSession("$0")
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.ID != "$0" || got.Name != "main" {
		t.Errorf("session = {%q, %q}, want {$0, main}", got.ID, got.Name)
	}
}

func TestSessionTree_GetSession_NotFound(t *testing.T) {
	tree := NewSessionTree()
	if s := tree.GetSession("$99"); s != nil {
		t.Errorf("expected nil for unknown session, got %+v", s)
	}
}

func TestSessionTree_RemoveSession(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main", "%0"))
	tree.RemoveSession("$0")

	if s := tree.GetSession("$0"); s != nil {
		t.Errorf("expected nil after remove, got %+v", s)
	}
}

func TestSessionTree_RemoveSession_NonExistent(t *testing.T) {
	// Should not panic.
	tree := NewSessionTree()
	tree.RemoveSession("$99")
}

func TestSessionTree_GetSessions_SortedByName(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$2", "zebra"))
	tree.UpdateSession(makeTestSession("$0", "alpha"))
	tree.UpdateSession(makeTestSession("$1", "middle"))

	sessions := tree.GetSessions()
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	if sessions[0].Name != "alpha" || sessions[1].Name != "middle" || sessions[2].Name != "zebra" {
		t.Errorf("sessions not sorted: %s, %s, %s", sessions[0].Name, sessions[1].Name, sessions[2].Name)
	}
}

func TestSessionTree_GetSessions_ReturnsCopies(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main"))

	sessions := tree.GetSessions()
	sessions[0].Name = "mutated"

	original := tree.GetSession("$0")
	if original.Name != "main" {
		t.Error("GetSessions did not return a deep copy")
	}
}

func TestSessionTree_FindPane(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main", "%0", "%1"))
	tree.UpdateSession(makeTestSession("$1", "dev", "%2"))

	sid, found := tree.FindPane("%2")
	if !found {
		t.Fatal("expected to find pane %2")
	}
	if sid != "$1" {
		t.Errorf("FindPane(%%2) = %q, want $1", sid)
	}
}

func TestSessionTree_FindPane_NotFound(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main", "%0"))

	_, found := tree.FindPane("%99")
	if found {
		t.Error("expected pane %99 not to be found")
	}
}

func TestSessionTree_AddWindow(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main"))

	w := &Window{ID: "@5", Name: "new-win", Index: 5}
	tree.AddWindow("$0", w)

	s := tree.GetSession("$0")
	if len(s.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(s.Windows))
	}
	// Should be sorted by index: @0 (index 0) then @5 (index 5).
	if s.Windows[1].ID != "@5" || s.Windows[1].Name != "new-win" {
		t.Errorf("added window unexpected: %+v", s.Windows[1])
	}
}

func TestSessionTree_AddWindow_NoOpForUnknownSession(t *testing.T) {
	tree := NewSessionTree()
	w := &Window{ID: "@0", Name: "win", Index: 0}
	// Should not panic.
	tree.AddWindow("$99", w)
}

func TestSessionTree_RemoveWindow(t *testing.T) {
	tree := NewSessionTree()
	s := &Session{
		ID:   "$0",
		Name: "main",
		Windows: []*Window{
			{ID: "@0", Name: "bash", Index: 0},
			{ID: "@1", Name: "vim", Index: 1},
		},
	}
	tree.UpdateSession(s)
	tree.RemoveWindow("$0", "@0")

	got := tree.GetSession("$0")
	if len(got.Windows) != 1 {
		t.Fatalf("expected 1 window after remove, got %d", len(got.Windows))
	}
	if got.Windows[0].ID != "@1" {
		t.Errorf("remaining window = %q, want @1", got.Windows[0].ID)
	}
}

func TestSessionTree_RemoveWindow_NonExistent(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main"))
	// Should not panic.
	tree.RemoveWindow("$0", "@99")
}

func TestSessionTree_RenameWindow(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main"))
	tree.RenameWindow("$0", "@0", "renamed")

	s := tree.GetSession("$0")
	if s.Windows[0].Name != "renamed" {
		t.Errorf("window name = %q, want renamed", s.Windows[0].Name)
	}
}

func TestSessionTree_RenameWindow_NonExistentWindow(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "main"))
	// Should not panic.
	tree.RenameWindow("$0", "@99", "nope")
}

func TestSessionTree_OnChangeCallback(t *testing.T) {
	tree := NewSessionTree()
	callCount := 0
	tree.SetOnChange(func() { callCount++ })

	tree.UpdateSession(makeTestSession("$0", "main", "%0"))
	tree.AddWindow("$0", &Window{ID: "@1", Name: "w", Index: 1})
	tree.RenameWindow("$0", "@0", "x")
	tree.RemoveWindow("$0", "@0")
	tree.RemoveSession("$0")

	if callCount != 5 {
		t.Errorf("onChange called %d times, want 5", callCount)
	}
}

func TestSessionTree_OnChange_NotCalledForNoOp(t *testing.T) {
	tree := NewSessionTree()
	callCount := 0
	tree.SetOnChange(func() { callCount++ })

	// Remove non-existent session -- should NOT fire onChange.
	tree.RemoveSession("$99")
	if callCount != 0 {
		t.Errorf("onChange called %d times for no-op remove, want 0", callCount)
	}
}

func TestSessionTree_ConcurrentAccess(t *testing.T) {
	// Verify that concurrent UpdateSession, RemoveSession, GetSessions,
	// FindPane, and ToJSON calls do not panic. We use separate session IDs
	// per goroutine to avoid hitting the known in-place mutation race in
	// RenameWindow (which mutates a Window.Name through a pointer while
	// readers clone it under RLock).
	tree := NewSessionTree()

	var wg sync.WaitGroup
	// Concurrent writers — each operates on its own session.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("$%d", i)
			paneID := fmt.Sprintf("%%%d", i)
			tree.UpdateSession(makeTestSession(id, "sess", paneID))
			tree.RemoveSession(id)
		}(i)
	}
	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tree.GetSessions()
			tree.GetSession("$0")
			tree.FindPane("%0")
			tree.ToJSON()
		}()
	}
	wg.Wait()
	// If we get here without a panic, the test passes.
}

// ===========================================================================
// ===========================================================================
// 5. ToJSON
// ===========================================================================

func TestSessionTree_ToJSON(t *testing.T) {
	tree := NewSessionTree()
	tree.UpdateSession(makeTestSession("$0", "alpha", "%0"))

	data, err := tree.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON error: %v", err)
	}
	s := string(data)
	if len(s) == 0 {
		t.Error("ToJSON returned empty")
	}
	// Basic sanity: should contain the session name.
	if !containsStr(s, "alpha") {
		t.Errorf("JSON does not contain session name: %s", s)
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && searchStr(haystack, needle)
}

func searchStr(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// ===========================================================================
// Structural notifications re-read the session
// ===========================================================================

func TestDispatchLine_StructuralChangesEmitLayoutChanged(t *testing.T) {
	for _, line := range []string{
		"%layout-change @2 b25d,212x55,0,0{106x55,0,0,8,105x55,107,0,9} b25d,212x55,0,0{106x55,0,0,8,105x55,107,0,9} *",
		"%window-pane-changed @15 %145",
		"%session-window-changed $0 @2",
		"%unlinked-window-add @20",
		"%unlinked-window-close @20",
		"%unlinked-window-renamed @15 fennec",
	} {
		c := &ControlClient{}
		var got []Event
		c.OnEvent(func(e Event) { got = append(got, e) })
		c.dispatchLine(line)
		if len(got) != 1 || got[0].Type != EventLayoutChanged {
			t.Errorf("%q: events = %+v, want one EventLayoutChanged", line, got)
		}
	}
}
