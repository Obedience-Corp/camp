package approvals

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/pkg/approvalkit"
)

func pending(kind string, step int, cwd string) approvalkit.Pending {
	return approvalkit.Pending{ID: "ap-test", SourceKind: kind, Step: step, Cwd: cwd, Title: "t"}
}

func TestCommand(t *testing.T) {
	p := pending(approvalkit.SourceStandaloneCheckpoint, 3, "workflow/explore/x")

	argv, err := Command(p, Approve, "ignored for approvals")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); got != "fest workflow approve --expect-step 3" {
		t.Fatalf("approve = %q", got)
	}

	argv, err = Command(p, Reject, "  missing evidence  ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fest", "workflow", "reject", "--expect-step", "3", "--reason", "missing evidence"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("reject = %q", argv)
	}

	for _, kind := range []string{approvalkit.SourceFestivalCheckpoint, approvalkit.SourcePhaseGate} {
		if _, err := Command(pending(kind, 1, "x"), Approve, ""); err != nil {
			t.Errorf("kind %s must be supported: %v", kind, err)
		}
	}
}

func TestCommandRefuses(t *testing.T) {
	ok := pending(approvalkit.SourceStandaloneCheckpoint, 2, "x")
	cases := map[string]func() error{
		"unknown kind":     func() error { _, err := Command(pending("shell", 1, "x"), Approve, ""); return err },
		"empty kind":       func() error { _, err := Command(pending("", 1, "x"), Approve, ""); return err },
		"no step":          func() error { _, err := Command(pending(approvalkit.SourcePhaseGate, 0, "x"), Approve, ""); return err },
		"reject no reason": func() error { _, err := Command(ok, Reject, "  "); return err },
		"unknown decision": func() error { _, err := Command(ok, Decision("delete"), ""); return err },
	}
	for name, run := range cases {
		if run() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestCommandIgnoresRequestText pins the authority rule: nothing a request
// says, other than its step number, reaches the command line. A forged request
// cannot choose what runs.
func TestCommandIgnoresRequestText(t *testing.T) {
	p := approvalkit.Pending{
		SourceKind: approvalkit.SourceFestivalCheckpoint,
		Step:       4,
		Title:      "; rm -rf /",
		Body:       "$(curl evil)",
		StepName:   "--override-judge",
		Cwd:        "x",
		Doc:        "--auto",
		Source:     "--judge-command=evil",
		Key:        "k",
	}
	argv, err := Command(p, Approve, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); got != "fest workflow approve --expect-step 4" {
		t.Fatalf("request text reached the command: %q", got)
	}
}

func withStatus(t *testing.T, out string, err error) {
	t.Helper()
	orig := statusRunner
	statusRunner = func(context.Context, string) ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() { statusRunner = orig })
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflow", "explore", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	here := pending(approvalkit.SourceStandaloneCheckpoint, 2, "workflow/explore/x")

	t.Run("waiting at the recorded step", func(t *testing.T) {
		withStatus(t, `{"current_step":2,"complete":false}`, nil)
		r := Check(ctx, root, here)
		if !r.Actionable || r.Dir == "" || r.Reason != "" {
			t.Fatalf("readiness = %+v", r)
		}
	})
	t.Run("gate waiting at the recorded step", func(t *testing.T) {
		withStatus(t, `{"mode":"gate","current_step":2,"complete":false}`, nil)
		if r := Check(ctx, root, pending(approvalkit.SourcePhaseGate, 2, "workflow/explore/x")); !r.Actionable {
			t.Fatalf("readiness = %+v", r)
		}
	})
	gate := pending(approvalkit.SourcePhaseGate, 2, "workflow/explore/x")
	cases := []struct {
		name   string
		p      approvalkit.Pending
		status string
		err    error
		reason string
		over   bool
	}{
		{"unknown kind", pending("shell", 2, "workflow/explore/x"), `{"current_step":2}`, nil, "cannot act", false},
		{"outside the camp", pending(approvalkit.SourceStandaloneCheckpoint, 2, "../elsewhere"), `{"current_step":2}`, nil, "outside this camp", false},
		{"absolute path", pending(approvalkit.SourceStandaloneCheckpoint, 2, "/etc"), `{"current_step":2}`, nil, "outside this camp", false},
		{"not on this machine", pending(approvalkit.SourceStandaloneCheckpoint, 2, "projects/worktrees/a/b"), `{"current_step":2}`, nil, "not on this machine", false},
		{"source past the step is over", here, `{"current_step":3,"complete":false}`, nil, "at step 3, not step 2", true},
		// A checkout that has not caught up after a pull reports an older
		// step. The request is live elsewhere and must not be cleared.
		{"source behind the step is not over", here, `{"current_step":1,"complete":false}`, nil, "at step 1, not step 2", false},
		{"source finished is over", here, `{"complete":true}`, nil, "has finished", true},
		{"source at no step is unknown", here, `{"complete":false}`, nil, "not at any step", false},
		{"workflow request while the gate is current is over", here, `{"mode":"gate","current_step":2}`, nil, "phase gate is current", true},
		{"gate request before the phase reaches the gate is not over", gate, `{"mode":"workflow","current_step":2}`, nil, "has not reached the gate", false},
		{"status unreadable", here, ``, errors.New("fest: not found\nmore"), "could not be read: fest: not found", false},
		{"status not json", here, `not json`, nil, "could not be parsed", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withStatus(t, c.status, c.err)
			r := Check(ctx, root, c.p)
			if r.Actionable || r.Dir != "" || !strings.Contains(r.Reason, c.reason) {
				t.Fatalf("readiness = %+v, want reason containing %q", r, c.reason)
			}
			if r.Over != c.over {
				t.Fatalf("Over = %v, want %v (%s)", r.Over, c.over, r.Reason)
			}
		})
	}
}

func TestCheckStripsControlCharactersFromSourceErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "w"), 0o755); err != nil {
		t.Fatal(err)
	}
	withStatus(t, "", errors.New("boom\x1b[2J\x07 here"))
	r := Check(context.Background(), root, pending(approvalkit.SourceStandaloneCheckpoint, 1, "w"))
	if strings.ContainsAny(r.Reason, "\x1b\x07") || !strings.Contains(r.Reason, "boom[2J here") {
		t.Fatalf("reason = %q", r.Reason)
	}
}

func TestCheckAllKeepsOrder(t *testing.T) {
	root := t.TempDir()
	var list []approvalkit.Pending
	for i := 1; i <= 9; i++ {
		// Unknown kinds answer without starting a process, with the kind in
		// the reason, so order is observable.
		list = append(list, pending("kind-"+string(rune('0'+i)), i, "x"))
	}
	got := CheckAll(context.Background(), root, list)
	if len(got) != len(list) {
		t.Fatalf("results = %d, want %d", len(got), len(list))
	}
	for i, r := range got {
		if !strings.Contains(r.Reason, list[i].SourceKind) {
			t.Fatalf("result %d is out of order: %q", i, r.Reason)
		}
	}
	if got := CheckAll(context.Background(), root, nil); len(got) != 0 {
		t.Fatalf("empty list: %v", got)
	}
}
