package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/approvals"
	"github.com/Obedience-Corp/camp/pkg/approvalkit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// approvalCamp is a throwaway camp root holding the fixture's source directory,
// so the directory the browser re-resolves before running a command exists.
func approvalCamp(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "workflow", "explore", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return root, dir
}

func approvalFixture(id string, actionable bool) approvalRow {
	row := approvalRow{Pending: approvalkit.Pending{
		ID: id, EventID: id + "-event", Key: id, Source: id,
		SourceKind: approvalkit.SourceStandaloneCheckpoint, Step: 2, StepName: "REVIEW",
		Title: "demo: REVIEW", Body: "Step 2 of 2 needs a decision.\n\nGoal: Get sign-off.",
		Cwd: "workflow/explore/demo", Doc: "workflow/explore/demo/WORKFLOW.md",
		RequestedAt: ledgerkit.NowUTC(time.Now().Add(-5 * time.Minute)),
		RequestedBy: ledgerkit.Actor{Type: ledgerkit.ActorAgent, Name: "bot"},
		Writer:      "wabc",
	}}
	if actionable {
		row.ready = approvals.Readiness{Actionable: true, Dir: "/camp/workflow/explore/demo"}
	} else {
		row.ready = approvals.Readiness{Reason: "its directory is not on this machine"}
	}
	return row
}

func TestApprovalsTUIRequested(t *testing.T) {
	orig := approvalsOpts
	t.Cleanup(func() { approvalsOpts = orig })
	cases := []struct {
		json, plain, interactive, tty, want bool
	}{
		{tty: true, want: true},
		{tty: false, want: false},
		{plain: true, tty: true, want: false},
		{json: true, tty: true, want: false},
		{interactive: true, tty: false, want: true},
		{json: true, interactive: true, tty: true, want: false},
	}
	for _, c := range cases {
		approvalsOpts.json, approvalsOpts.plain, approvalsOpts.interactive = c.json, c.plain, c.interactive
		if got := approvalsTUIRequested(c.tty); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

func TestWrapApprovalText(t *testing.T) {
	if got := wrapApprovalText("short", 0); len(got) != 1 || got[0] != "short" {
		t.Fatalf("no width: %q", got)
	}
	got := wrapApprovalText("  1. one two three four five", 14)
	want := []string{"  1. one two", "  three four", "  five"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrap = %q, want %q", got, want)
	}
	for _, line := range got {
		if len([]rune(line)) > 14 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	// A single word longer than the width is kept whole rather than dropped.
	if got := wrapApprovalText("supercalifragilistic", 5); len(got) != 1 || got[0] != "supercalifragilistic" {
		t.Fatalf("long word: %q", got)
	}
}

func TestShellQuoteArgv(t *testing.T) {
	got := shellQuoteArgv([]string{"fest", "workflow", "reject", "--reason", "it's not ready", ""})
	want := `fest workflow reject --reason 'it'\''s not ready' ''`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestApprovalToJSON(t *testing.T) {
	now := time.Now()
	ready := approvalToJSON(approvalFixture("ap-1", true), now)
	if !ready.Actionable || ready.Dir == "" || strings.Join(ready.ApproveCommand, " ") != "fest workflow approve --expect-step 2" {
		t.Fatalf("actionable row: %+v", ready)
	}
	if ready.AgeMs < int64(4*time.Minute/time.Millisecond) {
		t.Fatalf("age = %dms", ready.AgeMs)
	}
	stuck := approvalToJSON(approvalFixture("ap-2", false), now)
	if stuck.Actionable || stuck.Dir != "" || stuck.ApproveCommand != nil || stuck.NotActionableReason == "" {
		t.Fatalf("non-actionable row must carry a reason and no command: %+v", stuck)
	}
}

func TestApprovalsNotices(t *testing.T) {
	if notes := approvalsNotices(approvalsSnapshot{}); len(notes) != 0 {
		t.Fatalf("clean camp: %q", notes)
	}
	// No upstream is unknown, not "behind".
	if notes := approvalsNotices(approvalsSnapshot{behind: 3}); len(notes) != 0 {
		t.Fatalf("unknown upstream must not warn: %q", notes)
	}
	notes := approvalsNotices(approvalsSnapshot{behind: 3, behindKnown: true, uncommitted: 1})
	if len(notes) != 2 || !strings.Contains(notes[0], "3 commits behind") || !strings.Contains(notes[1], "not committed") {
		t.Fatalf("notices = %q", notes)
	}
}

func approvalKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func approvalsModel(rows ...approvalRow) approvalsTUIModel {
	return newApprovalsTUIModel(context.Background(), approvalsSnapshot{campRoot: "/camp", rows: rows})
}

func press(t *testing.T, m approvalsTUIModel, keys ...string) (approvalsTUIModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		next, c := m.Update(approvalKey(k))
		m, cmd = next.(approvalsTUIModel), c
	}
	return m, cmd
}

func TestApprovalsTUI_ApproveShowsTheCommandAndWaits(t *testing.T) {
	m, cmd := press(t, approvalsModel(approvalFixture("ap-1", true)), "a")
	if m.mode != approvalsConfirm || cmd != nil {
		t.Fatalf("approve must open a confirm and run nothing: mode=%v cmd=%v", m.mode, cmd)
	}
	view := m.View()
	if !strings.Contains(view, "fest workflow approve --expect-step 2") || !strings.Contains(view, "/camp/workflow/explore/demo") {
		t.Fatalf("confirm must show the exact command and directory:\n%s", view)
	}

	cancelled, cmd := press(t, m, "n")
	if cancelled.mode != approvalsBrowse || cmd != nil {
		t.Fatalf("n must cancel without running anything")
	}

	// The fixture's directory does not exist, so confirming must refuse: the
	// directory is resolved again at the moment of running.
	refused, cmd := press(t, m, "y")
	if cmd != nil || !refused.statusErr || !strings.Contains(refused.status, "directory changed") {
		t.Fatalf("y with an unresolvable directory must run nothing: status=%q", refused.status)
	}
}

func TestApprovalsTUI_ConfirmRunsOnlyInTheDirectoryThatWasShown(t *testing.T) {
	root, dir := approvalCamp(t)
	row := approvalFixture("ap-1", true)
	row.ready.Dir = dir
	m := newApprovalsTUIModel(context.Background(), approvalsSnapshot{campRoot: root, rows: []approvalRow{row}})

	m, _ = press(t, m, "a")
	if _, cmd := press(t, m, "y"); cmd == nil {
		t.Fatal("y must hand off to the source's command when the directory still resolves to what was shown")
	}

	// The same path now resolves somewhere else.
	moved := m
	moved.pendingRow.ready.Dir = filepath.Join(root, "elsewhere")
	after, cmd := press(t, moved, "y")
	if cmd != nil || !strings.Contains(after.status, "directory changed") {
		t.Fatalf("a directory that resolves differently must run nothing: status=%q", after.status)
	}
}

func TestApprovalIsStale(t *testing.T) {
	cases := []struct {
		r    approvals.Readiness
		want bool
	}{
		{approvals.Readiness{Actionable: true, Dir: "/x"}, false},
		{approvals.Readiness{Reason: "its source has finished", Over: true}, true},
		{approvals.Readiness{Reason: "its source is at step 1, not step 2"}, false},
		{approvals.Readiness{Reason: "its directory is not on this machine"}, false},
		{approvals.Readiness{Reason: "its source could not be read: boom"}, false},
	}
	for _, c := range cases {
		if got := approvalIsStale(c.r); got != c.want {
			t.Errorf("approvalIsStale(%+v) = %v", c.r, got)
		}
	}
}

func TestApprovalsTUI_NotActionableRunsNothing(t *testing.T) {
	for _, k := range []string{"a", "x"} {
		m, cmd := press(t, approvalsModel(approvalFixture("ap-1", false)), k)
		if m.mode != approvalsBrowse || cmd != nil || !m.statusErr || !strings.Contains(m.status, "not actionable here") {
			t.Fatalf("%s on a non-actionable request: mode=%v status=%q", k, m.mode, m.status)
		}
	}
}

func TestApprovalsTUI_RejectCollectsAReasonFirst(t *testing.T) {
	m, _ := press(t, approvalsModel(approvalFixture("ap-1", true)), "x")
	if m.mode != approvalsReason {
		t.Fatalf("x must ask for a reason, mode=%v", m.mode)
	}
	// Enter with no reason stays put.
	if m, _ = press(t, m, "enter"); m.mode != approvalsReason {
		t.Fatal("an empty reason must not continue")
	}
	m, _ = press(t, m, "n", "o", "space", "p", "r", "o", "o", "f", "!", "backspace", "enter")
	if m.mode != approvalsConfirm {
		t.Fatalf("a reason must lead to the confirm, mode=%v", m.mode)
	}
	if got := strings.Join(m.pendingArgv, " "); got != "fest workflow reject --expect-step 2 --reason no proof" {
		t.Fatalf("argv = %q", got)
	}
	// Letters typed into the reason must not act as browse keys ("q" quits).
	typed, cmd := press(t, approvalsModel(approvalFixture("ap-1", true)), "x", "q", "a")
	if typed.mode != approvalsReason || typed.reason != "qa" || cmd != nil {
		t.Fatalf("typing a reason triggered a browse key: mode=%v reason=%q", typed.mode, typed.reason)
	}
	if back, _ := press(t, typed, "esc"); back.mode != approvalsBrowse || back.reason != "" {
		t.Fatal("esc must cancel the reason")
	}
}

// TestApprovalsTUI_ConfirmedRequestIsFixed pins that the request a confirm acts
// on is the one that was shown, even if the list changes underneath it.
func TestApprovalsTUI_ConfirmedRequestIsFixed(t *testing.T) {
	m, _ := press(t, approvalsModel(approvalFixture("ap-1", true), approvalFixture("ap-2", true)), "a")
	other := approvalFixture("ap-9", true)
	other.Step = 7
	next, _ := m.Update(approvalsRefreshedMsg{snap: approvalsSnapshot{campRoot: "/camp", rows: []approvalRow{other}}})
	m = next.(approvalsTUIModel)
	if m.pendingRow.ID != "ap-1" || strings.Join(m.pendingArgv, " ") != "fest workflow approve --expect-step 2" {
		t.Fatalf("a refresh changed the confirmed request: %s %q", m.pendingRow.ID, m.pendingArgv)
	}
}

func TestApprovalsTUI_EmptyAndNavigation(t *testing.T) {
	empty := approvalsModel()
	if !strings.Contains(empty.View(), "Nothing is waiting on your approval.") {
		t.Fatalf("empty view:\n%s", empty.View())
	}
	if m, cmd := press(t, empty, "a", "x", "o", "j", "k"); m.mode != approvalsBrowse || cmd != nil {
		t.Fatal("keys on an empty list must do nothing")
	}
	m, _ := press(t, approvalsModel(approvalFixture("ap-1", true), approvalFixture("ap-2", false)), "j", "j", "j")
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want it clamped to the last row", m.cursor)
	}
	if m, _ = press(t, m, "k", "k"); m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}
