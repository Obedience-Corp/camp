package explorer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

const autoCommitDrainTimeout = 30 * time.Second

// pendingOp is one filesystem mutation recorded during an explorer session.
// Ops are only committed when the session ends (see DrainAutoCommits), matching
// dungeon/intent crawl: mutations apply immediately on disk; git is batched once.
type pendingOp struct {
	Action      commit.IntentAction
	Title       string
	Description string
}

// autoCommitter accumulates intent filesystem paths and op descriptions during a
// TUI session and runs a single selective git commit when drained. Recording is
// O(paths) and never touches git, so move/edit/archive stay responsive.
type autoCommitter struct {
	mu    sync.Mutex
	files map[string]struct{}
	ops   []pendingOp
	// run is the commit sink; tests replace it. Nil uses runAutoCommitIntent.
	run func(ctx context.Context, opts commit.IntentOptions)
}

func newAutoCommitter() *autoCommitter {
	return &autoCommitter{
		files: make(map[string]struct{}),
		run:   runAutoCommitIntent,
	}
}

func runAutoCommitIntent(ctx context.Context, opts commit.IntentOptions) {
	res := commit.Intent(ctx, opts)
	if res.Err != nil {
		slog.WarnContext(ctx, "intent auto-commit failed",
			"action", opts.Action,
			"intent", opts.IntentTitle,
			"error", res.Err,
		)
		return
	}
	if res.Skipped {
		slog.WarnContext(ctx, "intent auto-commit skipped",
			"action", opts.Action,
			"intent", opts.IntentTitle,
			"reason", res.SkipReason,
		)
	}
}

// slogWarnWriter adapts an io.Writer to slog.WarnContext so best-effort
// ambient-context resolution warnings (ref backfill, etc.) reach the TUI's
// redirected log file instead of the live terminal. Writing raw text to
// stderr here would corrupt the bubbletea alt-screen; quietSlogDuringTUI
// already routes the default slog handler to a log file for the same
// reason, so this keeps warnings on that same path.
type slogWarnWriter struct {
	ctx context.Context
}

func (w slogWarnWriter) Write(p []byte) (int, error) {
	slog.WarnContext(w.ctx, "intent auto-commit context resolution warning", "message", strings.TrimSpace(string(p)))
	return len(p), nil
}

func (a *autoCommitter) record(action commit.IntentAction, title, description string, files []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.files == nil {
		a.files = make(map[string]struct{})
	}
	for _, f := range files {
		if f == "" {
			continue
		}
		a.files[f] = struct{}{}
	}
	a.ops = append(a.ops, pendingOp{
		Action:      action,
		Title:       title,
		Description: description,
	})
}

func (a *autoCommitter) pending() (files []string, ops []pendingOp) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.ops) == 0 && len(a.files) == 0 {
		return nil, nil
	}
	files = make([]string, 0, len(a.files))
	for f := range a.files {
		files = append(files, f)
	}
	sort.Strings(files)
	ops = append([]pendingOp(nil), a.ops...)
	return files, ops
}

func (a *autoCommitter) clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.files = make(map[string]struct{})
	a.ops = nil
}

func buildSessionCommit(files []string, ops []pendingOp) commit.IntentOptions {
	// Single-op sessions keep the original action/title for familiar git log.
	if len(ops) == 1 {
		return commit.IntentOptions{
			Options: commit.Options{
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      ops[0].Action,
			IntentTitle: ops[0].Title,
			Description: ops[0].Description,
		}
	}

	var b strings.Builder
	for _, op := range ops {
		line := fmt.Sprintf("%s: %s", op.Action, op.Title)
		if op.Description != "" {
			line += " — " + op.Description
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return commit.IntentOptions{
		Options: commit.Options{
			Files:         files,
			SelectiveOnly: true,
		},
		Action:      commit.IntentUpdate,
		IntentTitle: "intent explorer session",
		Description: strings.TrimRight(b.String(), "\n"),
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

// autoCommitIntent records paths for a session-end batch commit. It does not
// invoke git. Disk + domain audit already happened; durability of "what
// happened" is the ledger/audit log. Git history is flushed on DrainAutoCommits.
func (m *Model) autoCommitIntent(action commit.IntentAction, title, description string, files ...string) {
	if m.campaignRoot == "" || m.campaignID == "" {
		ctx := m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		slog.WarnContext(ctx, "intent auto-commit skipped",
			"action", action,
			"intent", title,
			"reason", "missing campaign context: campaignRoot or campaignID is empty",
		)
		return
	}
	m.autoCommit.record(action, title, description, m.autoCommitFiles(files...))
}

// DrainAutoCommits runs one selective git commit for every mutation recorded
// during the explorer session (crawl-style). No-op when nothing changed.
func (m Model) DrainAutoCommits(w io.Writer) {
	files, ops := m.autoCommit.pending()
	if len(ops) == 0 {
		return
	}

	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	_, _ = fmt.Fprintln(w, "Finalizing intent commits...")

	opts := buildSessionCommit(files, ops)
	ambient := wkcmd.AmbientCommitOptions(ctx, m.campaignRoot, m.campaignID, slogWarnWriter{ctx: ctx})
	opts.CampaignRoot = ambient.CampaignRoot
	opts.CampaignID = ambient.CampaignID
	opts.QuestID = ambient.QuestID
	opts.FestivalRef = ambient.FestivalRef
	opts.WorkitemRef = ambient.WorkitemRef

	run := m.autoCommit.run
	if run == nil {
		run = runAutoCommitIntent
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, opts)
	}()

	select {
	case <-done:
		m.autoCommit.clear()
	case <-time.After(autoCommitDrainTimeout):
		_, _ = fmt.Fprintln(w, "warning: intent auto-commit did not finish; run 'camp status' to check for uncommitted intent changes")
	}
}
