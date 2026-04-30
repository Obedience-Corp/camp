package crawl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sharedcrawl "github.com/Obedience-Corp/camp/internal/crawl"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

// captureWriters returns AppendAudit and AppendLog implementations
// that record events into the provided slices. They are the
// in-process replacement for the real audit.AppendEvent and
// DefaultLogAppender so runner tests do not touch the filesystem.
func captureWriters(audits *[]audit.Event, logs *[]LogEntry) (AuditAppender, LogAppender) {
	auditFn := func(_ context.Context, _ string, e audit.Event) error {
		*audits = append(*audits, e)
		return nil
	}
	logFn := func(_ context.Context, _ string, e LogEntry) error {
		*logs = append(*logs, e)
		return nil
	}
	return auditFn, logFn
}

func newRunnerConfig(store IntentStore, prompt sharedcrawl.Prompt, audits *[]audit.Event, logs *[]LogEntry) Config {
	auditFn, logFn := captureWriters(audits, logs)
	return Config{
		Store:       store,
		Prompt:      prompt,
		IntentsDir:  ".campaign/intents",
		Actor:       "test",
		AppendAudit: auditFn,
		AppendLog:   logFn,
	}
}

func TestRun_NoCandidatesReturnsEmptyResult(t *testing.T) {
	store := newFakeStore()
	prompt := &sharedcrawl.FakePrompt{}
	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &[]audit.Event{}, &[]LogEntry{}), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.CandidateCount != 0 {
		t.Errorf("CandidateCount = %d, want 0", res.CandidateCount)
	}
	if res.Summary.Total() != 0 {
		t.Errorf("summary should be empty: %+v", res.Summary)
	}
}

func TestRun_KeepRecordsKeepLog(t *testing.T) {
	store := newFakeStore(&intent.Intent{
		ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0),
	})
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionKeep}},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Kept != 1 {
		t.Errorf("Kept = %d, want 1", res.Summary.Kept)
	}
	if len(audits) != 0 {
		t.Errorf("audit events for keep should be empty, got %d", len(audits))
	}
	if len(logs) != 1 || logs[0].Decision != DecisionKeep || logs[0].ID != "a" {
		t.Fatalf("log entries = %+v", logs)
	}
	if len(store.moveCalls) != 0 {
		t.Errorf("expected no Move calls, got %d", len(store.moveCalls))
	}
}

func TestRun_SkipRecordsSkipLog(t *testing.T) {
	store := newFakeStore(&intent.Intent{
		ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0),
	})
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionSkip}},
	}
	var audits []audit.Event
	var logs []LogEntry

	if _, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{}); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(logs) != 1 || logs[0].Decision != DecisionSkip {
		t.Fatalf("log entries = %+v", logs)
	}
}

func TestRun_QuitStopsImmediately(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "b", Title: "B", Status: intent.StatusInbox, CreatedAt: time.Unix(200, 0)},
	)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionQuit}},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Total() != 0 {
		t.Errorf("expected zero decisions on immediate quit, got %+v", res.Summary)
	}
	if prompt.ActionsConsumed() != 1 {
		t.Errorf("ActionsConsumed = %d, want 1", prompt.ActionsConsumed())
	}
}

func TestRun_MoveToLiveStatusNoReason(t *testing.T) {
	in := &intent.Intent{ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionMove}},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{Action: sharedcrawl.ActionMove, Target: "ready"}},
		},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Moved["ready"] != 1 {
		t.Errorf("expected one move to ready, got %+v", res.Summary)
	}
	if len(store.moveCalls) != 1 || store.moveCalls[0].Status != intent.StatusReady {
		t.Errorf("move calls = %+v, want one to ready", store.moveCalls)
	}
	if len(audits) != 1 || audits[0].Type != audit.EventMove || audits[0].To != "ready" {
		t.Errorf("audit events = %+v", audits)
	}
	if len(logs) != 1 || logs[0].Decision != DecisionMove || logs[0].To != intent.StatusReady {
		t.Errorf("log entries = %+v", logs)
	}
	if logs[0].Reason != "" {
		t.Errorf("live move should not record a reason, got %q", logs[0].Reason)
	}
	// Save should not be called for non-dungeon moves.
	if len(store.saveCalls) != 0 {
		t.Errorf("expected no Save for live move, got %d", len(store.saveCalls))
	}
}

func TestRun_MoveToDungeonRequiresReasonAndDecisionRecord(t *testing.T) {
	in := &intent.Intent{ID: "a", Title: "A", Status: intent.StatusReady, CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionMove}},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{
				Action: sharedcrawl.ActionMove, Target: "dungeon/archived", RequiresReason: true,
			}},
		},
		ReasonScript: []sharedcrawl.ReasonResponse{{Reason: "superseded"}},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Moved["dungeon/archived"] != 1 {
		t.Errorf("dungeon move not recorded: %+v", res.Summary)
	}
	if len(store.saveCalls) != 1 {
		t.Fatalf("expected exactly one save (decision record), got %d", len(store.saveCalls))
	}
	saved := store.saveCalls[0]
	if !contains(saved.Content, "Decision Record") || !contains(saved.Content, "superseded") {
		t.Errorf("decision record content missing in saved intent: %q", saved.Content)
	}
	if len(audits) != 1 || audits[0].Reason != "superseded" {
		t.Errorf("audit reason missing: %+v", audits)
	}
	if logs[0].Reason != "superseded" {
		t.Errorf("log reason = %q, want superseded", logs[0].Reason)
	}
}

func TestRun_MoveToDungeonEmptyReasonReturnsToFirstStep(t *testing.T) {
	in := &intent.Intent{ID: "a", Title: "A", Status: intent.StatusReady, CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{
			{Action: sharedcrawl.ActionMove},
			{Action: sharedcrawl.ActionSkip},
		},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{
				Action: sharedcrawl.ActionMove, Target: "dungeon/archived", RequiresReason: true,
			}},
		},
		ReasonScript: []sharedcrawl.ReasonResponse{{Reason: ""}},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Skipped != 1 {
		t.Errorf("expected skip after empty-reason cancel, got %+v", res.Summary)
	}
	if len(store.moveCalls) != 0 {
		t.Errorf("no move should occur after empty-reason cancel")
	}
}

func TestRun_DestinationBackoutReturnsToFirstStep(t *testing.T) {
	in := &intent.Intent{ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{
			{Action: sharedcrawl.ActionMove},
			{Action: sharedcrawl.ActionKeep},
		},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{}}, // back gesture
		},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Kept != 1 {
		t.Errorf("expected keep after esc-and-keep, got %+v", res.Summary)
	}
}

func TestRun_MoveToCurrentStatusTreatedAsKeep(t *testing.T) {
	in := &intent.Intent{ID: "a", Title: "A", Status: intent.StatusReady, CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionMove}},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{Action: sharedcrawl.ActionMove, Target: "ready"}},
		},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if res.Summary.Kept != 1 {
		t.Errorf("expected keep for redundant move, got %+v", res.Summary)
	}
	if len(store.moveCalls) != 0 {
		t.Errorf("no move should occur for redundant move")
	}
}

func TestRun_AbortPropagatesPartialSummary(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "b", Title: "B", Status: intent.StatusInbox, CreatedAt: time.Unix(200, 0)},
	)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{
			{Action: sharedcrawl.ActionKeep},
			{Err: sharedcrawl.ErrAborted},
		},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if !errors.Is(err, sharedcrawl.ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if res.Summary.Kept != 1 {
		t.Errorf("partial summary should retain kept=1, got %+v", res.Summary)
	}
}

func TestRun_AuditWriteFailureReturnsError(t *testing.T) {
	store := newFakeStore(&intent.Intent{
		ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0),
	})
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionMove}},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{Action: sharedcrawl.ActionMove, Target: "ready"}},
		},
	}
	var logs []LogEntry
	cfg := Config{
		Store:      store,
		Prompt:     prompt,
		IntentsDir: ".campaign/intents",
		AppendAudit: func(_ context.Context, _ string, _ audit.Event) error {
			return errors.New("audit write fail")
		},
		AppendLog: func(_ context.Context, _ string, e LogEntry) error {
			logs = append(logs, e)
			return nil
		},
	}
	if _, err := Run(context.Background(), cfg, Options{}); err == nil {
		t.Fatal("expected error when audit write fails")
	}
}

func TestRun_LogWriteFailureReturnsError(t *testing.T) {
	store := newFakeStore(&intent.Intent{
		ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0),
	})
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionKeep}},
	}
	cfg := Config{
		Store:      store,
		Prompt:     prompt,
		IntentsDir: ".campaign/intents",
		AppendAudit: func(_ context.Context, _ string, _ audit.Event) error {
			return nil
		},
		AppendLog: func(_ context.Context, _ string, _ LogEntry) error {
			return errors.New("log write fail")
		},
	}
	if _, err := Run(context.Background(), cfg, Options{}); err == nil {
		t.Fatal("expected error when log write fails")
	}
}

func TestRun_RejectsInvalidOptions(t *testing.T) {
	store := newFakeStore()
	prompt := &sharedcrawl.FakePrompt{}
	if _, err := Run(context.Background(), newRunnerConfig(store, prompt, &[]audit.Event{}, &[]LogEntry{}),
		Options{Limit: -5}); err == nil {
		t.Fatal("expected validation error for negative limit")
	}
}

func TestRun_RequiresStoreAndPrompt(t *testing.T) {
	if _, err := Run(context.Background(), Config{}, Options{}); err == nil {
		t.Fatal("expected error when store and prompt missing")
	}
	if _, err := Run(context.Background(), Config{Store: newFakeStore()}, Options{}); err == nil {
		t.Fatal("expected error when prompt missing")
	}
}

func TestRun_CommitPathsFlattenAfterMoves(t *testing.T) {
	in := &intent.Intent{ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	prompt := &sharedcrawl.FakePrompt{
		ActionScript: []sharedcrawl.ActionResponse{{Action: sharedcrawl.ActionMove}},
		DestinationScript: []sharedcrawl.DestinationResponse{
			{Option: sharedcrawl.Option{Action: sharedcrawl.ActionMove, Target: "ready"}},
		},
	}
	var audits []audit.Event
	var logs []LogEntry

	res, err := Run(context.Background(), newRunnerConfig(store, prompt, &audits, &logs), Options{})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if !contains(joinAll(res.CommitPaths.Files), "ready") {
		t.Errorf("commit Files missing destination: %v", res.CommitPaths.Files)
	}
	if !contains(joinAll(res.CommitPaths.Files), "crawl.jsonl") {
		t.Errorf("commit Files missing crawl log: %v", res.CommitPaths.Files)
	}
	if !contains(joinAll(res.CommitPaths.Files), ".intents.jsonl") {
		t.Errorf("commit Files missing audit log: %v", res.CommitPaths.Files)
	}
}

func TestRun_PromotedToBlocksLiveBackEntry(t *testing.T) {
	in := &intent.Intent{
		ID: "a", Title: "A", Status: intent.StatusActive,
		PromotedTo: "festivals/active/foo", CreatedAt: time.Unix(100, 0),
	}
	// The runner builds destinationOptions internally, which omits
	// inbox/ready when promoted_to is set. We assert via direct call
	// to destinationOptions to keep the test focused.
	opts := destinationOptions(in, nil)
	for _, o := range opts {
		if o.Target == "inbox" || o.Target == "ready" {
			t.Errorf("promoted_to gate failed: %v", o.Target)
		}
	}
}

// helpers ----------------------------------------------------------

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

func joinAll(s []string) string { return strings.Join(s, " ") }
