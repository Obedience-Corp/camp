// Package notice surfaces campaign-state advisories on commands the user
// already runs, so a fix they don't know about becomes discoverable.
//
// This is deliberately the smallest thing that works: detect, render, move on.
// The full design (workflow/design/need-to-reduce-bloat-in-20260709-215845/
// 04-repair-and-template-update-notifications.md) adds per-signature dismissal
// persisted in campaign state and folds in the camp init --repair drift
// signal. Neither is built yet. Notice carries an ID so dismissal has a key to
// hang off when it is, and Detect takes a detector list so the repair signal
// plugs in as one more entry.
//
// Detectors run on high-traffic commands, so they must stay stat-level. No
// detector may scan the campaign tree.
package notice

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/ui"
)

// Notice is one advisory about campaign state.
type Notice struct {
	// ID identifies the signal. It is the key per-signature dismissal will
	// use, so it must stay stable across runs.
	ID string
	// Message states what is drifted, in one line.
	Message string
	// Command fixes it.
	Command string
}

// Detector reports a Notice for campaignRoot, or nil when it has nothing to
// say. Detectors must be stat-level cheap.
type Detector func(ctx context.Context, campaignRoot string) (*Notice, error)

// Detect runs detectors against campaignRoot and collects what they report.
//
// A detector that fails is skipped rather than surfaced: a notice is an
// advisory on the side of a command that has its own job to do, and failing
// that command because an advisory could not be computed would be worse than
// staying quiet.
func Detect(ctx context.Context, campaignRoot string, detectors ...Detector) []Notice {
	var notices []Notice
	for _, detect := range detectors {
		if ctx.Err() != nil {
			return notices
		}
		n, err := detect(ctx, campaignRoot)
		if err != nil || n == nil {
			continue
		}
		notices = append(notices, *n)
	}
	return notices
}

// Render writes notices to w.
//
// A notice shares stderr with the command's own output, so it is styled like
// the rest of camp's advisories: a warning icon and label carry the severity,
// and the fix is accented so the line a user should act on stands out from the
// line that explains why. Styling collapses to plain text under --no-color and
// on non-terminal writers, which keeps the notice greppable in scripts and in
// the integration tests that assert on its text.
func Render(w io.Writer, notices []Notice) {
	for _, n := range notices {
		_, _ = fmt.Fprintf(w, "%s %s %s\n", ui.WarningIcon(), ui.Warning("notice:"), n.Message)
		_, _ = fmt.Fprintf(w, "  %s %s\n", ui.Dim("run:"), ui.Accent(n.Command))
	}
}
