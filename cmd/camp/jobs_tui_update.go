package main

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func (m jobsTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case jobsRefreshedMsg:
		m.busy = false
		if msg.err != nil {
			m.setStatus("refresh failed: "+msg.err.Error(), true)
			return m, nil
		}
		m.entries = msg.entries
		m.superseded = msg.superseded
		if m.superseded == nil {
			m.superseded = map[string]bool{}
		}
		m.clampCursor()
		m.setStatus("refreshed", false)
		return m, nil
	case jobsActionDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			m.busy = true
			return m, refreshJobsCmd(m.ctx, m.campRoot)
		}
		m.setStatus(msg.okStatus, false)
		m.busy = true
		return m, refreshJobsCmd(m.ctx, m.campRoot)
	case tea.KeyMsg:
		if m.overlay != jobsOverlayNone {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m jobsTUIModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		switch key.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	m.status = ""
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.quitting = true
		return m, tea.Quit
	case "down", "j":
		if len(m.entries) > 0 {
			m.cursor = (m.cursor + 1) % len(m.entries)
		}
		return m, nil
	case "up", "k":
		if len(m.entries) > 0 {
			m.cursor = (m.cursor - 1 + len(m.entries)) % len(m.entries)
		}
		return m, nil
	case "u", "ctrl+r":
		m.busy = true
		m.setStatus("refreshing…", false)
		return m, refreshJobsCmd(m.ctx, m.campRoot)
	case "y":
		e, ok := m.selected()
		if !ok {
			return m, nil
		}
		if err := ui.WriteClipboard(e.ID); err != nil {
			m.setStatus("copy failed: "+err.Error(), true)
		} else {
			m.setStatus("copied "+e.ID, false)
		}
		return m, nil
	case "r":
		return m.startRetrySelected()
	case "R":
		return m.startRetryAll()
	case "d":
		return m.startDropSelected()
	case "D":
		failed, _, _, _ := jobsActionCounts(m.entries, m.superseded)
		if failed == 0 {
			m.setStatus("no failed jobs to drop", true)
			return m, nil
		}
		m.overlay = jobsOverlayConfirmDropAll
		return m, nil
	case "x":
		return m.startDropRunningSelected()
	case "s":
		return m.startServe()
	}
	return m, nil
}

func (m jobsTUIModel) updateOverlay(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q", "esc", "n":
		m.overlay = jobsOverlayNone
		m.confirmID = ""
		m.confirmIDs = nil
		m.confirmSkip = 0
		return m, nil
	case "y", "enter":
		overlay := m.overlay
		id := m.confirmID
		ids := append([]string(nil), m.confirmIDs...)
		skip := m.confirmSkip
		m.overlay = jobsOverlayNone
		m.confirmID = ""
		m.confirmIDs = nil
		m.confirmSkip = 0
		m.busy = true
		switch overlay {
		case jobsOverlayConfirmDrop:
			m.setStatus("dropping…", false)
			return m, retryOrDropCmd(m.ctx, m.campRoot, id, false, false)
		case jobsOverlayConfirmDropRunning:
			m.setStatus("stopping worker and dropping…", false)
			return m, retryOrDropCmd(m.ctx, m.campRoot, id, false, true)
		case jobsOverlayConfirmDropAll:
			m.setStatus("dropping all failed…", false)
			return m, retryOrDropCmd(m.ctx, m.campRoot, jobs.SelectorAll, false, false)
		case jobsOverlayConfirmRetryAll:
			m.setStatus("retrying failed…", false)
			return m, retryIDsCmd(m.ctx, m.campRoot, ids, skip)
		}
	}
	return m, nil
}

func (m jobsTUIModel) startRetrySelected() (tea.Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok {
		return m, nil
	}
	if e.State != "failed" {
		m.setStatus("retry only works on failed jobs", true)
		return m, nil
	}
	if m.superseded[e.ID] {
		m.setStatus("cannot retry: history moved past this job; drop it instead", true)
		return m, nil
	}
	m.busy = true
	m.setStatus("retrying…", false)
	return m, retryOrDropCmd(m.ctx, m.campRoot, e.ID, true, false)
}

func (m jobsTUIModel) startRetryAll() (tea.Model, tea.Cmd) {
	ids := retryableFailedIDs(m.entries, m.superseded)
	_, _, _, stale := jobsActionCounts(m.entries, m.superseded)
	if len(ids) == 0 {
		if stale > 0 {
			m.setStatus(fmt.Sprintf(
				"no retryable failed jobs (%d cannot retry; drop them)", stale), true)
			return m, nil
		}
		m.setStatus("no failed jobs to retry", true)
		return m, nil
	}
	m.overlay = jobsOverlayConfirmRetryAll
	m.confirmIDs = ids
	m.confirmSkip = stale
	return m, nil
}

func (m jobsTUIModel) startDropSelected() (tea.Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok {
		return m, nil
	}
	if e.State != "failed" {
		if e.State == "running" {
			m.setStatus("job is running; press x to stop its worker and drop it", true)
			return m, nil
		}
		m.setStatus("drop only works on failed jobs", true)
		return m, nil
	}
	m.overlay = jobsOverlayConfirmDrop
	m.confirmID = e.ID
	return m, nil
}

func (m jobsTUIModel) startDropRunningSelected() (tea.Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok {
		return m, nil
	}
	if e.State != "running" {
		m.setStatus("drop-running only works on running jobs", true)
		return m, nil
	}
	m.overlay = jobsOverlayConfirmDropRunning
	m.confirmID = e.ID
	return m, nil
}

func (m jobsTUIModel) startServe() (tea.Model, tea.Cmd) {
	m.busy = true
	m.setStatus("serving lanes…", false)
	campRoot := m.campRoot
	ctx := m.ctx
	return m, func() tea.Msg {
		if err := jobs.Run(ctx, campRoot); err != nil {
			return jobsActionDoneMsg{err: err}
		}
		return jobsActionDoneMsg{okStatus: "served pending lanes"}
	}
}

// retryOrDropCmd performs retry (retry=true) or drop (retry=false). When
// running is true, drop also stops the worker holding the job.
func retryOrDropCmd(ctx context.Context, campRoot, selector string, retry, running bool) tea.Cmd {
	return func() tea.Msg {
		if retry {
			return retrySelectorAction(ctx, campRoot, selector, 0)
		}
		if running {
			return dropRunningAction(ctx, campRoot, selector)
		}
		dropped, err := jobs.Drop(ctx, campRoot, selector)
		if err != nil {
			return jobsActionDoneMsg{err: err}
		}
		if len(dropped) == 0 {
			return jobsActionDoneMsg{okStatus: "no failed jobs to drop"}
		}
		return jobsActionDoneMsg{
			okStatus: fmt.Sprintf("dropped %s; files remain for your next commit", jobCountPhrase(len(dropped))),
		}
	}
}

// retryIDsCmd requeues each id and reports how many superseded jobs were left
// alone. Ids are already filtered; skip is only for the status line.
func retryIDsCmd(ctx context.Context, campRoot string, ids []string, skip int) tea.Cmd {
	return func() tea.Msg {
		if len(ids) == 0 {
			if skip > 0 {
				return jobsActionDoneMsg{okStatus: fmt.Sprintf(
					"no retryable failed jobs (%d cannot retry)", skip)}
			}
			return jobsActionDoneMsg{okStatus: "no failed jobs to retry"}
		}
		var requeued []jobs.Job
		for _, id := range ids {
			got, err := jobs.Retry(ctx, campRoot, id)
			if err != nil {
				return jobsActionDoneMsg{err: err}
			}
			requeued = append(requeued, got...)
		}
		return retryDoneStatus(campRoot, ctx, requeued, skip)
	}
}

func retrySelectorAction(ctx context.Context, campRoot, selector string, skip int) tea.Msg {
	requeued, err := jobs.Retry(ctx, campRoot, selector)
	if err != nil {
		return jobsActionDoneMsg{err: err}
	}
	return retryDoneStatus(campRoot, ctx, requeued, skip)
}

func retryDoneStatus(campRoot string, ctx context.Context, requeued []jobs.Job, skip int) tea.Msg {
	if len(requeued) == 0 {
		if skip > 0 {
			return jobsActionDoneMsg{okStatus: fmt.Sprintf(
				"no retryable failed jobs (%d cannot retry)", skip)}
		}
		return jobsActionDoneMsg{okStatus: "no failed jobs to retry"}
	}
	for _, lane := range distinctRepos(requeued) {
		jobs.SpawnIfNeeded(ctx, campRoot, lane)
	}
	msg := fmt.Sprintf("requeued %s", jobCountPhrase(len(requeued)))
	if skip > 0 {
		msg += fmt.Sprintf(" (%d cannot retry; left alone)", skip)
	}
	return jobsActionDoneMsg{okStatus: msg}
}

func dropRunningAction(ctx context.Context, campRoot, selector string) tea.Msg {
	dropped, failedErr := jobs.Drop(ctx, campRoot, selector)
	var noMatch *jobs.NoMatchError
	if failedErr != nil && !errors.As(failedErr, &noMatch) {
		return jobsActionDoneMsg{err: failedErr}
	}
	running, stops, err := jobs.DropRunning(ctx, campRoot, selector)
	dropped = append(dropped, running...)
	status := formatWorkerStops(stops)
	if err != nil {
		if status != "" {
			return jobsActionDoneMsg{err: camperrors.Wrap(err, status)}
		}
		return jobsActionDoneMsg{err: err}
	}
	if len(dropped) == 0 {
		if failedErr != nil {
			return jobsActionDoneMsg{err: camperrors.Newf("no failed or running job with id %q", selector)}
		}
		return jobsActionDoneMsg{okStatus: "no failed or running jobs to drop"}
	}
	if blocking, err := jobs.OutstandingAll(campRoot); err == nil {
		jobs.EnsureServed(ctx, campRoot, blocking)
	}
	msg := fmt.Sprintf("dropped %s; files remain for your next commit", jobCountPhrase(len(dropped)))
	if status != "" {
		msg = status + "; " + msg
	}
	return jobsActionDoneMsg{okStatus: msg}
}

func formatWorkerStops(stops []jobs.WorkerStop) string {
	if len(stops) == 0 {
		return ""
	}
	parts := make([]string, 0, len(stops))
	for _, stop := range stops {
		switch {
		case stop.PID == 0:
			parts = append(parts, fmt.Sprintf("lane %s had no live worker", stop.Lane))
		case stop.Killed:
			parts = append(parts, fmt.Sprintf("killed worker %d on lane %s", stop.PID, stop.Lane))
		default:
			parts = append(parts, fmt.Sprintf("stopped worker %d on lane %s", stop.PID, stop.Lane))
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return fmt.Sprintf("%d workers stopped", len(parts))
}
