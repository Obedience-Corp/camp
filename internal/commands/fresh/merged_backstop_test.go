package fresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func TestBackstopPromoteCommand(t *testing.T) {
	got := backstopPromoteCommand(wkitem.WorkItem{StableID: "design-foo-01", Key: "design:workflow/design/foo"})
	if want := "camp workitem promote design-foo-01 --target completed"; got != want {
		t.Errorf("promote command = %q, want %q", got, want)
	}
	// Falls back to Key when there is no StableID.
	got = backstopPromoteCommand(wkitem.WorkItem{Key: "design:foo"})
	if want := "camp workitem promote design:foo --target completed"; got != want {
		t.Errorf("promote command (no StableID) = %q, want %q", got, want)
	}
}

// TestResolveBackstopMode_DryRunNeverPrompts is the regression guard for the
// dry-run mutation bug: a dry-run must downgrade to "report", so the prompt path
// (the only path that can reach PromoteMergedWorkitem) is unreachable on a
// dry-run regardless of TTY.
func TestResolveBackstopMode_DryRunNeverPrompts(t *testing.T) {
	tests := []struct {
		configured string
		dryRun     bool
		want       string
	}{
		{"prompt", true, "report"},  // the bug: dry-run must NOT prompt/promote
		{"prompt", false, "prompt"}, // normal run keeps prompt
		{"report", true, "report"},
		{"off", true, "off"}, // dry-run does not resurrect an opted-out backstop
		{"off", false, "off"},
	}
	for _, tc := range tests {
		if got := resolveBackstopMode(tc.configured, tc.dryRun); got != tc.want {
			t.Errorf("resolveBackstopMode(%q, %v) = %q, want %q", tc.configured, tc.dryRun, got, tc.want)
		}
	}
	// The prompt path is the sole caller of PromoteMergedWorkitem, and it only
	// runs when the resolved mode is "prompt"; since dry-run never yields
	// "prompt", a dry-run can never reach a promote.
	if resolveBackstopMode("prompt", true) == "prompt" {
		t.Fatal("dry-run resolved to prompt: a dry-run could reach PromoteMergedWorkitem")
	}
}

func TestBackstopPromptTitle(t *testing.T) {
	title := backstopPromptTitle(MergedBackstopMatch{Workitem: wkitem.WorkItem{Title: "Fix login", StableID: "design-x"}})
	if !strings.Contains(title, `"Fix login"`) || !strings.Contains(title, "Promote to completed?") {
		t.Errorf("prompt title missing quoted label or question: %q", title)
	}
}

func TestBackstopPromptDescription(t *testing.T) {
	wi := wkitem.WorkItem{Title: "Fix login", StableID: "design-foo-01", RelativePath: "workflow/design/foo"}

	t.Run("worktree-link match shows id, directory, and merged branch", func(t *testing.T) {
		desc := backstopPromptDescription(MergedBackstopMatch{Workitem: wi, Branch: "myfeature", Signal: SignalWorktreeLink})
		for _, want := range []string{"design-foo-01", "workflow/design/foo", "myfeature (merged)"} {
			if !strings.Contains(desc, want) {
				t.Errorf("description missing %q:\n%s", want, desc)
			}
		}
	})

	t.Run("commit-tag match shows evidence instead of a branch", func(t *testing.T) {
		desc := backstopPromptDescription(MergedBackstopMatch{Workitem: wi, Signal: SignalCommitTag})
		if strings.Contains(desc, "Branch:") {
			t.Errorf("commit-tag match must not claim a branch:\n%s", desc)
		}
		if !strings.Contains(desc, "workitem-tagged commits merged") {
			t.Errorf("commit-tag match missing evidence line:\n%s", desc)
		}
	})

	t.Run("missing directory omits the Directory line", func(t *testing.T) {
		desc := backstopPromptDescription(MergedBackstopMatch{Workitem: wkitem.WorkItem{StableID: "design-x"}, Branch: "b"})
		if strings.Contains(desc, "Directory:") {
			t.Errorf("description must omit Directory when RelativePath is empty:\n%s", desc)
		}
	})
}

func TestBackstopWorkitemContext(t *testing.T) {
	got := backstopWorkitemContext(wkitem.WorkItem{StableID: "design-foo-01", RelativePath: "workflow/design/foo"})
	if want := "design-foo-01, workflow/design/foo"; got != want {
		t.Errorf("context = %q, want %q", got, want)
	}
	if got := backstopWorkitemContext(wkitem.WorkItem{StableID: "design-foo-01"}); got != "design-foo-01" {
		t.Errorf("context without path = %q, want bare id", got)
	}
}

func taggedSubject(ref, msg string) string {
	return commitkit.PrependContextTagsFullNamed("obey-campaign", "8deed8b4", "", "", ref, msg)
}

func TestWorkitemRefsFromSubjects(t *testing.T) {
	subjects := []string{
		taggedSubject("WI-abc123", "fix a thing"),
		"chore: no tag here",
		"",
		taggedSubject("WI-def456", "another fix"),
		taggedSubject("WI-abc123", "same ref again"),
	}
	got := workitemRefsFromSubjects(subjects)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct refs, got %d: %v", len(got), got)
	}
	if !got["WI-abc123"] || !got["WI-def456"] {
		t.Errorf("unexpected ref set: %v", got)
	}
}

func TestMatchWorktreeLinkBranch(t *testing.T) {
	active := []wkitem.WorkItem{
		{Key: "design:workflow/design/foo", StableID: "design-foo-01", RelativePath: "workflow/design/foo"},
	}
	linkList := []links.Link{
		// Worktree link whose dir basename matches the branch name.
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/foo"}},
		// Repo-scope link with the same basename must be ignored (no branch identity).
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeRepo, Path: "projects/other/foo"}},
	}

	wi, _, ok := matchWorktreeLinkBranch(linkList, active, "foo")
	if !ok || wi.Key != "design:workflow/design/foo" {
		t.Fatalf("expected worktree-link match for branch foo, got ok=%v wi=%+v", ok, wi)
	}

	if _, _, ok := matchWorktreeLinkBranch(linkList, active, "unrelated"); ok {
		t.Errorf("expected no match for a branch with no worktree link")
	}

	// Repo-scope only (no worktree link) must not match by basename.
	repoOnly := []links.Link{{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeRepo, Path: "projects/other/foo"}}}
	if _, _, ok := matchWorktreeLinkBranch(repoOnly, active, "foo"); ok {
		t.Errorf("repo-scope link must not match a branch by basename")
	}
}

// TestCollectWorktreeLinkMatches_DedupesSameWorkitem is the regression for the
// camp fresh multi-promote bug: one design workitem with four stacked worktree
// links (p0..p3) used to produce four promote attempts — first succeeds, the
// rest fail with "stat workitem ... no such file" because the source was already
// moved. Mapping must collapse to one match per workitem key.
func TestCollectWorktreeLinkMatches_DedupesSameWorkitem(t *testing.T) {
	active := []wkitem.WorkItem{
		{
			Key:          "design:workflow/design/samantha-provider-harness-sessions",
			StableID:     "design-samantha-provider-harness-sessions-2026-07-26",
			Title:        "Samantha provider split",
			RelativePath: "workflow/design/samantha-provider-harness-sessions",
		},
		{
			Key:          "design:workflow/design/other",
			StableID:     "design-other-01",
			Title:        "Other design",
			RelativePath: "workflow/design/other",
		},
	}
	linkList := []links.Link{
		{WorkitemID: "design-samantha-provider-harness-sessions-2026-07-26", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/samantha/p0-session-warn-default"}},
		{WorkitemID: "design-samantha-provider-harness-sessions-2026-07-26", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/samantha/p1-slash-clear"}},
		{WorkitemID: "design-samantha-provider-harness-sessions-2026-07-26", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/samantha/p2-unified-compact"}},
		{WorkitemID: "design-samantha-provider-harness-sessions-2026-07-26", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/samantha/p3-grok-resume"}},
		{WorkitemID: "design-other-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/samantha/other-feature"}},
	}
	pruned := []string{
		"p0-session-warn-default",
		"p1-slash-clear",
		"p2-unified-compact",
		"p3-grok-resume",
		"other-feature",
		"unrelated-merged-branch",
	}

	matches, unmatched, matchedKeys := collectWorktreeLinkMatches(linkList, active, pruned)

	if len(matches) != 2 {
		t.Fatalf("expected 2 unique workitem matches, got %d: %+v", len(matches), matches)
	}
	if matches[0].Workitem.Key != active[0].Key {
		t.Errorf("first match key = %q, want samantha provider workitem", matches[0].Workitem.Key)
	}
	if matches[0].Branch != "p0-session-warn-default" {
		t.Errorf("first match should keep the first pruned branch, got %q", matches[0].Branch)
	}
	if matches[0].Signal != SignalWorktreeLink {
		t.Errorf("signal = %q, want %q", matches[0].Signal, SignalWorktreeLink)
	}
	if matches[1].Workitem.Key != active[1].Key {
		t.Errorf("second match key = %q, want other design", matches[1].Workitem.Key)
	}
	if !matchedKeys[active[0].Key] || !matchedKeys[active[1].Key] {
		t.Errorf("matchedKeys incomplete: %v", matchedKeys)
	}
	// Duplicate worktree hits must not spill into the commit-tag unmatched set.
	if len(unmatched) != 1 || unmatched[0] != "unrelated-merged-branch" {
		t.Errorf("unmatched = %v, want only unrelated-merged-branch", unmatched)
	}
}

func TestRefMatchesActiveItem(t *testing.T) {
	item := wkitem.WorkItem{
		Key:            "design:workflow/design/foo",
		StableID:       "design-foo-01",
		RelativePath:   "workflow/design/foo",
		SourceMetadata: map[string]any{"ref": "WI-abc123"},
	}
	if !refMatchesActiveItem(map[string]bool{"WI-abc123": true}, item) {
		t.Errorf("expected match by WI- ref in SourceMetadata")
	}
	if !refMatchesActiveItem(map[string]bool{"design-foo-01": true}, item) {
		t.Errorf("expected match by StableID alias")
	}
	if refMatchesActiveItem(map[string]bool{"WI-999999": true}, item) {
		t.Errorf("expected no match for an unrelated ref")
	}
}

func TestActiveBackstopItems_ExcludesFestivalsAndIntents(t *testing.T) {
	items := []wkitem.WorkItem{
		{Key: "design:a", WorkflowType: wkitem.WorkflowTypeDesign},
		{Key: "festival:b", WorkflowType: wkitem.WorkflowTypeFestival},
		{Key: "intent:c", WorkflowType: wkitem.WorkflowTypeIntent},
		{Key: "explore:d", WorkflowType: wkitem.WorkflowTypeExplore},
	}
	got := activeBackstopItems(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items (design, explore), got %d", len(got))
	}
	for _, wi := range got {
		if wi.WorkflowType == wkitem.WorkflowTypeFestival || wi.WorkflowType == wkitem.WorkflowTypeIntent {
			t.Errorf("festival/intent leaked into backstop set: %+v", wi)
		}
	}
}

func TestHasOpenWork(t *testing.T) {
	wi := wkitem.WorkItem{Key: "design:foo", StableID: "design-foo-01"}
	const mergedPath = "projects/camp"

	t.Run("single scope, only the merged project link, is not open", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
		}}
		if HasOpenWork("/root", reg, wi, mergedPath, mergedPath) {
			t.Errorf("single-scope workitem should not be suppressed")
		}
	})

	t.Run("two distinct project links (WI-ca06e1 shape) is open", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/festival-app"}},
		}}
		if !HasOpenWork("/root", reg, wi, mergedPath, mergedPath) {
			t.Errorf("workitem linked to a second project must be suppressed (open work elsewhere)")
		}
	})

	t.Run("stale worktree link with a missing dir does not count as open", func(t *testing.T) {
		root := t.TempDir()
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/gone"}},
		}}
		if HasOpenWork(root, reg, wi, mergedPath, mergedPath) {
			t.Errorf("a worktree link whose directory no longer exists must not count as open")
		}
	})

	t.Run("existing other worktree is open", func(t *testing.T) {
		root := t.TempDir()
		wtRel := filepath.Join("projects", "worktrees", "camp", "live")
		if err := os.MkdirAll(filepath.Join(root, wtRel), 0o755); err != nil {
			t.Fatal(err)
		}
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: filepath.ToSlash(wtRel)}},
		}}
		if !HasOpenWork(root, reg, wi, mergedPath, mergedPath) {
			t.Errorf("an existing other worktree must count as open work")
		}
	})

	// Regression (review #496): a workitem whose only links are the just-merged
	// project AND its own (still-present) worktree must NOT read as open. Before
	// the fix, the project link's path was compared against the worktree scope
	// path and never matched, so the report never fired.
	t.Run("project link + just-merged worktree link is not open", func(t *testing.T) {
		root := t.TempDir()
		wtRel := filepath.ToSlash(filepath.Join("projects", "worktrees", "camp", "myfeature"))
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(wtRel)), 0o755); err != nil {
			t.Fatal(err)
		}
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: wtRel}},
		}}
		// merged scope = the worktree that just merged; project = mergedPath.
		if HasOpenWork(root, reg, wi, wtRel, mergedPath) {
			t.Errorf("project link + its own just-merged worktree must NOT count as open work")
		}
	})
}
