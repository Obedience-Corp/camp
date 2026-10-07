package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	tuinotify "github.com/Obedience-Corp/camp/internal/tui/notify"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// notifyPayload is the --json document. Both lists are always arrays.
type notifyPayload struct {
	SchemaVersion string                `json:"schema_version"`
	CampaignRoot  string                `json:"campaign_root"`
	Live          []notifyLiveJSON      `json:"live"`
	Dismissed     []notifyDismissedJSON `json:"dismissed"`
}

// notifyLiveJSON is one live notice. Subject is what it is about, such as an
// artifact root, and is empty for a notice about the camp as a whole.
type notifyLiveJSON struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Message string `json:"message"`
	Command string `json:"command"`
}

// notifyDismissedJSON carries message and command only while a detector still
// reports the id: the dismissal file records an id and a time, nothing more.
// Summary is what the id says the notice was about, so a dismissal nothing
// reports still reads as more than an id.
type notifyDismissedJSON struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	DismissedAt string `json:"dismissed_at"`
	Summary     string `json:"summary,omitempty"`
	Message     string `json:"message,omitempty"`
	Command     string `json:"command,omitempty"`
}

func runNotify(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}

	inv, err := notice.TakeInventory(ctx, campRoot, notice.Detectors()...)
	if err != nil {
		return err
	}

	if notifyOpts.json {
		root, err := pathutil.ResolveRoot(campRoot)
		if err != nil {
			return camperrors.Wrap(err, "resolving camp root")
		}
		return emitNotifyJSON(cmd.OutOrStdout(), root, inv)
	}
	if notifyTUIRequested(ui.IsTerminal()) {
		return runNotifyTUI(cmd, campRoot, inv)
	}
	renderNotifyPlain(cmd.OutOrStdout(), inv)
	return nil
}

func notifyTUIRequested(isTTY bool) bool {
	if notifyOpts.json || notifyOpts.plain {
		return false
	}
	return isTTY
}

func runNotifyTUI(cmd *cobra.Command, campRoot string, inv notice.Inventory) error {
	model := tuinotify.New(cmd.Context(), inv, tuinotify.Options{
		Store:     tuinotify.DiskStore{Root: campRoot, Detectors: notice.Detectors(), Now: time.Now},
		Clipboard: ui.WriteClipboard,
	})
	final, err := tea.NewProgram(model, tea.WithContext(cmd.Context()), tea.WithAltScreen()).Run()
	if err != nil {
		return camperrors.Wrap(err, "running notice browser")
	}
	if m, ok := final.(tuinotify.Model); ok {
		reportNotifyChanges(cmd.OutOrStdout(), m.Changes())
	}
	return nil
}

// reportNotifyChanges repeats what the session changed once the alternate
// screen is gone, so the record of a dismissal and its undo outlives the
// browser.
func reportNotifyChanges(w io.Writer, changes []tuinotify.Change) {
	for _, c := range changes {
		if c.Dismissed {
			_, _ = fmt.Fprintf(w, "%s Dismissed %s. Undo: camp notify restore %s\n", ui.SuccessIcon(), c.ID, c.ID)
			continue
		}
		_, _ = fmt.Fprintf(w, "%s Restored %s. Undo: camp notify dismiss %s\n", ui.SuccessIcon(), c.ID, c.ID)
	}
	if len(changes) > 0 {
		_, _ = fmt.Fprintf(w, "  Recorded in %s.\n", notice.DismissalRelPath)
	}
}

func emitNotifyJSON(w io.Writer, campRoot string, inv notice.Inventory) error {
	payload := notifyPayload{
		SchemaVersion: NotifyJSONVersion,
		CampaignRoot:  campRoot,
		Live:          make([]notifyLiveJSON, 0, len(inv.Live)),
		Dismissed:     make([]notifyDismissedJSON, 0, len(inv.Dismissed)),
	}
	for _, n := range inv.Live {
		payload.Live = append(payload.Live, notifyLiveJSON{ID: n.ID, Subject: n.Subject, Message: n.Message, Command: n.Command})
	}
	for _, d := range inv.Dismissed {
		row := notifyDismissedJSON{
			ID:          d.ID,
			Subject:     d.Subject,
			DismissedAt: d.At.UTC().Format(time.RFC3339),
			Summary:     d.Summary,
		}
		if d.Notice != nil {
			row.Message, row.Command = d.Notice.Message, d.Notice.Command
		}
		payload.Dismissed = append(payload.Dismissed, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return camperrors.Wrap(err, "encode notices")
	}
	return nil
}

func renderNotifyPlain(w io.Writer, inv notice.Inventory) {
	if inv.Empty() {
		_, _ = fmt.Fprintln(w, "No notices")
		return
	}

	_, _ = fmt.Fprintf(w, "LIVE NOTICES (%d)\n", len(inv.Live))
	if len(inv.Live) == 0 {
		_, _ = fmt.Fprintln(w, "  none")
	}
	for _, n := range inv.Live {
		_, _ = fmt.Fprintf(w, "  %s\n    %s\n    fix: %s\n", withSubject(n.ID, n.Subject), n.Message, n.Command)
	}

	_, _ = fmt.Fprintf(w, "\nDISMISSED NOTICES (%d)\n", len(inv.Dismissed))
	if len(inv.Dismissed) == 0 {
		_, _ = fmt.Fprintln(w, "  none")
	}
	for _, d := range inv.Dismissed {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", d.At.Format("2006-01-02"), withSubject(d.ID, d.Subject))
		switch {
		case d.Notice != nil:
			_, _ = fmt.Fprintf(w, "    %s\n", d.Notice.Message)
		case d.Summary != "":
			_, _ = fmt.Fprintf(w, "    %s\n", d.Summary)
		}
	}

	_, _ = fmt.Fprintln(w, "\nDismiss one: camp notify dismiss <id>   Restore one: camp notify restore <id>")
}

func withSubject(id, subject string) string {
	if subject == "" {
		return id
	}
	return id + "  " + subject
}
