package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/jobs"
)

func jobsTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "jobs"}
	c.Flags().Bool("json", false, "")
	c.Flags().Bool("plain", false, "")
	c.Flags().BoolP("interactive", "i", false, "")
	return c
}

func TestJobsTUIRequested(t *testing.T) {
	t.Parallel()

	origJSON, origPlain, origInteractive := jobsOpts.json, jobsOpts.plain, jobsOpts.interactive
	t.Cleanup(func() {
		jobsOpts.json, jobsOpts.plain, jobsOpts.interactive = origJSON, origPlain, origInteractive
	})

	t.Run("tty no flags opens", func(t *testing.T) {
		jobsOpts.json, jobsOpts.plain, jobsOpts.interactive = false, false, false
		if !jobsTUIRequested(jobsTestCmd(), true) {
			t.Error("bare camp jobs in a TTY should open the browser")
		}
	})
	t.Run("piped no flags prints", func(t *testing.T) {
		jobsOpts.json, jobsOpts.plain, jobsOpts.interactive = false, false, false
		if jobsTUIRequested(jobsTestCmd(), false) {
			t.Error("piped camp jobs should print the table")
		}
	})
	t.Run("json never opens", func(t *testing.T) {
		jobsOpts.json, jobsOpts.plain, jobsOpts.interactive = true, false, false
		if jobsTUIRequested(jobsTestCmd(), true) {
			t.Error("--json must never open the browser")
		}
	})
	t.Run("plain never opens", func(t *testing.T) {
		jobsOpts.json, jobsOpts.plain, jobsOpts.interactive = false, true, false
		if jobsTUIRequested(jobsTestCmd(), true) {
			t.Error("--plain must never open the browser")
		}
	})
	t.Run("interactive forces even without tty", func(t *testing.T) {
		jobsOpts.json, jobsOpts.plain, jobsOpts.interactive = false, false, true
		if !jobsTUIRequested(jobsTestCmd(), false) {
			t.Error("-i must force the TUI request")
		}
	})
}

func newTestJobsModel(entries []jobs.Entry) jobsTUIModel {
	superseded := map[string]bool{}
	return newJobsTUIModel(context.Background(), "/tmp/camp", entries, superseded)
}

func jkey(m jobsTUIModel, s string) jobsTUIModel {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(jobsTUIModel)
}

func TestJobsTUIViewIncludesCreatedAndActions(t *testing.T) {
	t.Parallel()

	entries := []jobs.Entry{{
		Job: jobs.Job{
			ID: "job-20260728T113000Z-abcd", Seq: 1, Kind: jobs.KindCommitTree,
			CreatedAt: "2026-07-28T11:30:00.000Z", AutoWrite: true,
			LastError: "the commit message writer did not finish within 5m0s",
		},
		State: "failed", Lane: ".",
	}, {
		Job: jobs.Job{
			ID: "job-20260728T120000Z-ef01", Seq: 2, Kind: jobs.KindCommitPaths,
			CreatedAt: "2026-07-28T12:00:00.000Z", Paths: []string{"a.md"},
		},
		State: "running", Lane: "projects/camp", RunningFor: 3 * time.Second,
	}}
	m := newTestJobsModel(entries)
	m.width, m.height = 120, 30

	view := m.View()
	for _, want := range []string{
		"Jobs",
		"CREATED",
		jobs.FormatCreated("2026-07-28T11:30:00.000Z"),
		"failed",
		"running",
		"r: retry",
		"d: drop",
		"q: quit",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view omits %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "╭") && !strings.Contains(view, "┌") {
		// Rounded border uses ╭; some profiles may differ. Accept either box drawing.
		if !strings.Contains(view, "─") {
			t.Errorf("expected a bordered frame, view:\n%s", view)
		}
	}
}

func TestJobsTUINavigationAndConfirm(t *testing.T) {
	t.Parallel()

	entries := []jobs.Entry{{
		Job:   jobs.Job{ID: "job-a", Seq: 1, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T11:30:00.000Z"},
		State: "failed", Lane: ".",
	}, {
		Job:   jobs.Job{ID: "job-b", Seq: 2, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T12:00:00.000Z"},
		State: "failed", Lane: ".",
	}}
	m := newTestJobsModel(entries)
	m = jkey(m, "j")
	if m.cursor != 1 {
		t.Fatalf("cursor after j = %d, want 1", m.cursor)
	}
	m = jkey(m, "d")
	if m.overlay != jobsOverlayConfirmDrop {
		t.Fatalf("overlay after d = %v, want confirm drop", m.overlay)
	}
	if m.confirmID != "job-b" {
		t.Fatalf("confirmID = %q, want job-b", m.confirmID)
	}
	view := m.View()
	if !strings.Contains(view, "Drop job") || !strings.Contains(view, "job-b") {
		t.Errorf("confirm overlay missing drop copy:\n%s", view)
	}
	m = jkey(m, "n")
	if m.overlay != jobsOverlayNone {
		t.Fatalf("overlay after n = %v, want none", m.overlay)
	}
	m = jkey(m, "q")
	if !m.quitting {
		t.Fatal("q should quit")
	}
}

func TestJobsTUIHelpFollowsSelection(t *testing.T) {
	t.Parallel()

	entries := []jobs.Entry{{
		Job:   jobs.Job{ID: "job-fail", Seq: 1, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T11:30:00.000Z"},
		State: "failed", Lane: ".",
	}, {
		Job: jobs.Job{ID: "job-run", Seq: 2, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T12:00:00.000Z"},
		State: "running", Lane: ".", Stuck: true, Stalled: true, StalledReason: "no live worker",
	}}
	m := newTestJobsModel(entries)
	m.width, m.height = 120, 24
	if help := m.helpText(); !strings.Contains(help, "r: retry") || !strings.Contains(help, "d: drop") {
		t.Fatalf("failed row help = %q", help)
	}
	m = jkey(m, "j")
	help := m.helpText()
	if !strings.Contains(help, "s: serve") || !strings.Contains(help, "x: drop running") {
		t.Fatalf("stalled row help = %q", help)
	}
	if strings.Contains(help, "r: retry") {
		t.Fatalf("stalled row must not offer retry: %q", help)
	}
}

func TestRetryableFailedIDsSkipsSuperseded(t *testing.T) {
	t.Parallel()

	entries := []jobs.Entry{
		{Job: jobs.Job{ID: "ok"}, State: "failed"},
		{Job: jobs.Job{ID: "stale"}, State: "failed"},
		{Job: jobs.Job{ID: "run"}, State: "running"},
	}
	superseded := map[string]bool{"stale": true}
	got := retryableFailedIDs(entries, superseded)
	if len(got) != 1 || got[0] != "ok" {
		t.Fatalf("retryableFailedIDs = %v, want [ok]", got)
	}
}

func TestJobsTUIBulkRetrySkipsSuperseded(t *testing.T) {
	t.Parallel()

	entries := []jobs.Entry{{
		Job:   jobs.Job{ID: "job-ok", Seq: 1, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T11:30:00.000Z"},
		State: "failed", Lane: ".",
	}, {
		Job:   jobs.Job{ID: "job-stale", Seq: 2, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T12:00:00.000Z"},
		State: "failed", Lane: ".",
	}}
	m := newJobsTUIModel(t.Context(), "/tmp/camp", entries, map[string]bool{"job-stale": true})
	m.width, m.height = 100, 24

	// Selected on a superseded row: single retry refuses, bulk still offers
	// only the retryable ids.
	m.cursor = 1
	m = jkey(m, "r")
	if !m.statusErr || !strings.Contains(m.status, "cannot retry") {
		t.Fatalf("single retry on superseded = %q (err=%v)", m.status, m.statusErr)
	}
	help := m.helpText()
	if !strings.Contains(help, "R: retry all") {
		t.Fatalf("help should still offer bulk retry when other jobs are retryable: %q", help)
	}

	m = jkey(m, "R")
	if m.overlay != jobsOverlayConfirmRetryAll {
		t.Fatalf("overlay after R = %v, want confirm retry", m.overlay)
	}
	if len(m.confirmIDs) != 1 || m.confirmIDs[0] != "job-ok" {
		t.Fatalf("confirmIDs = %v, want [job-ok]", m.confirmIDs)
	}
	if m.confirmSkip != 1 {
		t.Fatalf("confirmSkip = %d, want 1", m.confirmSkip)
	}
	view := m.View()
	if !strings.Contains(view, "cannot-retry") || !strings.Contains(view, "1 job") {
		t.Fatalf("confirm overlay should name the retryable count and skipped jobs:\n%s", view)
	}
}

func TestJobsTUIBulkRetryRefusesWhenAllSuperseded(t *testing.T) {
	t.Parallel()

	entries := []jobs.Entry{{
		Job:   jobs.Job{ID: "job-stale", Seq: 1, Kind: jobs.KindCommitTree, CreatedAt: "2026-07-28T11:30:00.000Z"},
		State: "failed", Lane: ".",
	}}
	m := newJobsTUIModel(t.Context(), "/tmp/camp", entries, map[string]bool{"job-stale": true})
	m = jkey(m, "R")
	if m.overlay != jobsOverlayNone {
		t.Fatalf("overlay = %v, want none when nothing is retryable", m.overlay)
	}
	if !m.statusErr || !strings.Contains(m.status, "cannot retry") {
		t.Fatalf("status = %q (err=%v), want cannot-retry refusal", m.status, m.statusErr)
	}
	if strings.Contains(m.helpText(), "R: retry all") {
		t.Fatalf("help must not offer R when every failed job is superseded: %q", m.helpText())
	}
}
