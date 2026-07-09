package explorer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

const (
	quickDrainTimeout      = 300 * time.Millisecond
	autoCommitDrainTimeout = 15 * time.Second
)

// autoCommitter runs best-effort intent commits in the background, serialized so
// they cannot race on the git index lock and tracked so they can be drained
// before the process exits.
type autoCommitter struct {
	mu  sync.Mutex
	wg  sync.WaitGroup
	run func(ctx context.Context, opts commit.IntentOptions)
}

func newAutoCommitter() *autoCommitter {
	return &autoCommitter{run: runAutoCommitIntent}
}

func runAutoCommitIntent(ctx context.Context, opts commit.IntentOptions) {
	if res := commit.Intent(ctx, opts); res.Err != nil {
		slog.WarnContext(ctx, "intent auto-commit failed",
			"action", opts.Action,
			"intent", opts.IntentTitle,
			"error", res.Err,
		)
	}
}

func (a *autoCommitter) start(ctx context.Context, opts commit.IntentOptions) {
	a.wg.Go(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.run(ctx, opts)
	})
}

func (a *autoCommitter) wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

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
	m.autoCommit.start(ctx, commit.IntentOptions{
		Options: commit.Options{
			CampaignRoot:  m.campaignRoot,
			CampaignID:    m.campaignID,
			Files:         m.autoCommitFiles(files...),
			SelectiveOnly: true,
		},
		Action:      action,
		IntentTitle: title,
		Description: description,
	})
}

// DrainAutoCommits waits for background intent commits to finish before the
// process exits, printing a notice to w if the wait is not immediate and a
// warning if it exceeds the bounded timeout.
func (m Model) DrainAutoCommits(w io.Writer) {
	if m.autoCommit.wait(quickDrainTimeout) {
		return
	}
	_, _ = fmt.Fprintln(w, "Finalizing intent commits...")
	if !m.autoCommit.wait(autoCommitDrainTimeout) {
		_, _ = fmt.Fprintln(w, "warning: some intent auto-commits did not finish; run 'camp status' to check for uncommitted intent changes")
	}
}
