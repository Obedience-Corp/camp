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
func Render(w io.Writer, notices []Notice) {
	for _, n := range notices {
		_, _ = fmt.Fprintf(w, "notice: %s\n  run: %s\n", n.Message, n.Command)
	}
}
