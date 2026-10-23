package pty

import (
	"reflect"
	"testing"
)

func TestWithoutTmuxEnv(t *testing.T) {
	env := []string{"HOME=/h", "TMUX=/tmp/tmux-501/default,1,0", "TMUX_PANE=%3", "TMUXINATOR=1", "TERM=screen"}
	got := withoutTmuxEnv(env)
	want := []string{"HOME=/h", "TMUXINATOR=1", "TERM=screen"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withoutTmuxEnv = %v, want %v", got, want)
	}
}
