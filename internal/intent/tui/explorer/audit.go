package explorer

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

var runAutoCommitIntent = func(ctx context.Context, opts commit.IntentOptions) {
	if res := commit.Intent(ctx, opts); res.Err != nil {
		slog.WarnContext(ctx, "intent auto-commit failed",
			"action", opts.Action,
			"intent", opts.IntentTitle,
			"error", res.Err,
		)
	}
}

// autoCommitIntentMu serializes background auto-commits so concurrent goroutines
// cannot race on the git index lock. autoCommitIntentWg tracks in-flight commits
// so they can be drained before the process exits (see WaitForAutoCommits).
var (
	autoCommitIntentMu sync.Mutex
	autoCommitIntentWg sync.WaitGroup
)

func (m *Model) auditActor() string {
	actor := strings.TrimSpace(m.author)
	if actor == "" {
		return "system"
	}
	return actor
}

func (m *Model) appendAuditEvent(event audit.Event) error {
	if event.Actor == "" {
		event.Actor = m.auditActor()
	}
	return audit.AppendEvent(m.ctx, m.intentsDir, event)
}

func (m *Model) autoCommitFiles(files ...string) []string {
	if m.intentsDir != "" {
		files = append(files, audit.FilePath(m.intentsDir))
	}
	return commit.NormalizeFiles(m.campaignRoot, files...)
}

// autoCommitIntent starts a best-effort intent commit if campaign context is available.
func (m *Model) autoCommitIntent(action commit.IntentAction, title, description string, files ...string) {
	if m.campaignRoot == "" || m.campaignID == "" {
		return
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	opts := commit.IntentOptions{
		Options: commit.Options{
			CampaignRoot:  m.campaignRoot,
			CampaignID:    m.campaignID,
			Files:         m.autoCommitFiles(files...),
			SelectiveOnly: true,
		},
		Action:      action,
		IntentTitle: title,
		Description: description,
	}
	autoCommitIntentWg.Go(func() {
		autoCommitIntentMu.Lock()
		defer autoCommitIntentMu.Unlock()
		runAutoCommitIntent(ctx, opts)
	})
}

// WaitForAutoCommits blocks until every background intent auto-commit started by
// autoCommitIntent has finished, or until timeout elapses. The explorer fires
// commits asynchronously to keep the UI responsive, so the process must drain
// them on exit; otherwise an intent change already written to disk could be lost
// without ever being committed. It returns true if all commits drained, or false
// if the timeout was hit first. The timeout bounds shutdown so a wedged git lock
// cannot hang the exit indefinitely.
func WaitForAutoCommits(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		autoCommitIntentWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
