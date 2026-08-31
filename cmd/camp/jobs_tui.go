package main

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"
)

type jobsOverlay int

const (
	jobsOverlayNone jobsOverlay = iota
	jobsOverlayConfirmDrop
	jobsOverlayConfirmDropRunning
	jobsOverlayConfirmDropAll
	jobsOverlayConfirmRetryAll
)

// jobsTUIRequested decides whether bare `camp jobs` opens the browser.
// --json and --plain never do; -i forces; otherwise an interactive terminal
// opens it.
func jobsTUIRequested(cmd *cobra.Command, isTTY bool) bool {
	if jobsOpts.json || jobsOpts.plain {
		return false
	}
	if jobsOpts.interactive {
		return true
	}
	return isTTY
}

func runJobsTUI(cmd *cobra.Command, campRoot string, entries []jobs.Entry, superseded map[string]bool) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		renderJobsHuman(cmd, entries, superseded)
		return nil
	}
	model := newJobsTUIModel(cmd.Context(), campRoot, entries, superseded)
	prog := tea.NewProgram(model, tea.WithContext(cmd.Context()), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return camperrors.Wrap(err, "running jobs browser")
	}
	return nil
}

type jobsTUIModel struct {
	ctx         context.Context
	campRoot    string
	entries     []jobs.Entry
	superseded  map[string]bool
	cursor      int
	overlay     jobsOverlay
	confirmID   string
	status      string
	statusErr   bool
	width       int
	height      int
	quitting    bool
	busy        bool
}

func newJobsTUIModel(ctx context.Context, campRoot string, entries []jobs.Entry, superseded map[string]bool) jobsTUIModel {
	if superseded == nil {
		superseded = map[string]bool{}
	}
	m := jobsTUIModel{
		ctx:        ctx,
		campRoot:   campRoot,
		entries:    entries,
		superseded: superseded,
	}
	m.clampCursor()
	return m
}

func (m *jobsTUIModel) clampCursor() {
	if len(m.entries) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
}

func (m jobsTUIModel) selected() (jobs.Entry, bool) {
	if len(m.entries) == 0 || m.cursor < 0 || m.cursor >= len(m.entries) {
		return jobs.Entry{}, false
	}
	return m.entries[m.cursor], true
}

func (m *jobsTUIModel) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

func (m jobsTUIModel) Init() tea.Cmd { return nil }

type jobsRefreshedMsg struct {
	entries    []jobs.Entry
	superseded map[string]bool
	err        error
}

type jobsActionDoneMsg struct {
	okStatus string
	err      error
}

func refreshJobsCmd(ctx context.Context, campRoot string) tea.Cmd {
	return func() tea.Msg {
		entries, err := jobs.Snapshot(ctx, campRoot)
		if err != nil {
			return jobsRefreshedMsg{err: err}
		}
		return jobsRefreshedMsg{
			entries:    entries,
			superseded: supersededIDs(ctx, campRoot, entries),
		}
	}
}
