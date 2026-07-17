package sync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// fakePRChecker is the gh boundary fake: Plan's decision table is exercised
// entirely against this in-memory map, so these tests never shell out.
type fakePRChecker struct {
	states map[string]PRState
	errs   map[string]error
}

func (f *fakePRChecker) State(ctx context.Context, url string) (PRState, error) {
	if err, ok := f.errs[url]; ok {
		return "", err
	}
	if state, ok := f.states[url]; ok {
		return state, nil
	}
	return "", errors.New("fakePRChecker: no fixture for " + url)
}

func TestPRURLFromRefs(t *testing.T) {
	tests := []struct {
		name string
		refs []string
		want string
	}{
		{name: "nil refs", refs: nil, want: ""},
		{name: "no PR URL among refs", refs: []string{"branch/feature-x", "festivals/planning/CA0002"}, want: ""},
		{
			name: "finds a PR URL among non-PR refs",
			refs: []string{"branch/feature-x", "https://github.com/Obedience-Corp/camp/pull/123"},
			want: "https://github.com/Obedience-Corp/camp/pull/123",
		},
		{
			name: "trims surrounding whitespace before matching",
			refs: []string{"  https://github.com/Obedience-Corp/camp/pull/123  "},
			want: "https://github.com/Obedience-Corp/camp/pull/123",
		},
		{
			name: "rejects a non-PR github URL",
			refs: []string{"https://github.com/Obedience-Corp/camp/issues/123"},
			want: "",
		},
		{
			name: "returns the first PR URL when multiple are present",
			refs: []string{"https://github.com/o/r/pull/1", "https://github.com/o/r/pull/2"},
			want: "https://github.com/o/r/pull/1",
		},
		{
			name: "normalizes a /files suffix down to the canonical PR URL",
			refs: []string{"https://github.com/o/r/pull/123/files"},
			want: "https://github.com/o/r/pull/123",
		},
		{
			name: "normalizes a review-comment fragment down to the canonical PR URL",
			refs: []string{"https://github.com/o/r/pull/123#pullrequestreview-456"},
			want: "https://github.com/o/r/pull/123",
		},
		{
			name: "normalizes a diff-view query string down to the canonical PR URL",
			refs: []string{"https://github.com/o/r/pull/123?diff=split"},
			want: "https://github.com/o/r/pull/123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PRURLFromRefs(tt.refs); got != tt.want {
				t.Errorf("PRURLFromRefs(%#v) = %q, want %q", tt.refs, got, tt.want)
			}
		})
	}
}

func TestPlan(t *testing.T) {
	const mergedURL = "https://github.com/o/r/pull/1"
	const closedURL = "https://github.com/o/r/pull/2"
	const openURL = "https://github.com/o/r/pull/3"
	const failURL = "https://github.com/o/r/pull/4"

	checker := &fakePRChecker{
		states: map[string]PRState{
			mergedURL: PRStateMerged,
			closedURL: PRStateClosed,
			openURL:   PRStateOpen,
		},
		errs: map[string]error{
			failURL: errors.New("boom"),
		},
	}

	candidates := []*intent.Intent{
		{ID: "1", Title: "Merged one", WorkRef: []string{mergedURL}},
		{ID: "2", Title: "Closed one", WorkRef: []string{closedURL}},
		{ID: "3", Title: "Open one", WorkRef: []string{openURL}},
		{ID: "4", Title: "gh failed", WorkRef: []string{failURL}},
	}

	decisions, err := Plan(context.Background(), checker, candidates)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(decisions) != len(candidates) {
		t.Fatalf("Plan() returned %d decisions, want %d", len(decisions), len(candidates))
	}

	want := map[string]Outcome{
		"1": OutcomeMerged,
		"2": OutcomeClosed,
		"3": OutcomeOpen,
		"4": OutcomeCheckFailed,
	}
	for _, d := range decisions {
		if d.Outcome != want[d.IntentID] {
			t.Errorf("decision[%s].Outcome = %q, want %q", d.IntentID, d.Outcome, want[d.IntentID])
		}
	}

	failDecision := decisions[3]
	if failDecision.Err == nil {
		t.Error("check_failed decision should carry the underlying error")
	}
}

func TestPlan_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := &fakePRChecker{states: map[string]PRState{}}
	candidates := []*intent.Intent{{ID: "1", WorkRef: []string{"https://github.com/o/r/pull/1"}}}

	if _, err := Plan(ctx, checker, candidates); !errors.Is(err, context.Canceled) {
		t.Fatalf("Plan() with cancelled context error = %v, want context.Canceled", err)
	}
}

func TestApply_OnlyActsOnMerged(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := intent.NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{Title: "Not merged", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	for _, outcome := range []Outcome{OutcomeClosed, OutcomeOpen, OutcomeCheckFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			result, err := Apply(ctx, svc, Decision{IntentID: created.ID, Outcome: outcome})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if result != nil {
				t.Fatalf("Apply() with outcome %q returned a result, want nil (no-op)", outcome)
			}
		})
	}

	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reloaded.Status != intent.StatusInbox {
		t.Fatalf("intent status = %q, want unchanged %q", reloaded.Status, intent.StatusInbox)
	}
}

func TestApply_MovesMergedIntentToDone(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	svc := intent.NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{Title: "Merged intent", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	prURL := "https://github.com/o/r/pull/42"
	result, err := Apply(ctx, svc, Decision{IntentID: created.ID, Title: created.Title, PRURL: prURL, Outcome: OutcomeMerged})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil {
		t.Fatal("Apply() returned nil result for a merged decision")
	}
	if result.Status != intent.StatusDone {
		t.Fatalf("Status = %q, want %q", result.Status, intent.StatusDone)
	}
	if !strings.Contains(result.Content, prURL) {
		t.Errorf("decision record should reference the merged PR URL, content = %q", result.Content)
	}

	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after Apply error = %v", err)
	}
	if reloaded.Status != intent.StatusDone {
		t.Fatalf("reloaded status = %q, want %q", reloaded.Status, intent.StatusDone)
	}
}
