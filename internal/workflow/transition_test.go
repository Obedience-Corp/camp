package workflow

import "testing"

func TestTransitionCommitMessage(t *testing.T) {
	tr := Transition{Item: "my-project", From: "inbox", To: "active"}
	want := "flow: move my-project from inbox to active"
	if got := tr.CommitMessage(); got != want {
		t.Errorf("CommitMessage() = %q, want %q", got, want)
	}
}
