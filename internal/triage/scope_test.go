package triage

import (
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- error cases first -------------------------------------------------

// TestScopeExpressionsRejectMalformedInput: a mistyped scope silently matching
// nothing would look like an empty campaign, so it is refused with the keys
// that would have worked.
func TestScopeExpressionsRejectMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"no separator", "design"},
		{"no key", ":design"},
		{"no value", "type:"},
		{"unknown key", "colour:blue"},
		{"empty", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope := NewScope(DefaultProfile())

			err := scope.ApplyExpressions([]string{tc.expr})

			require.Error(t, err)
			require.ErrorIs(t, err, camperrors.ErrInvalidInput)
			assert.Equal(t, []string{"scope[0]"}, violatedFields(err))
			assert.Contains(t, err.Error(), "type")
		})
	}
}

// TestScopeExpressionsReportEveryProblem keeps the one-submission-one-list
// contract the rest of triage follows.
func TestScopeExpressionsReportEveryProblem(t *testing.T) {
	scope := NewScope(DefaultProfile())

	err := scope.ApplyExpressions([]string{"type:design", "nonsense", "colour:blue"})

	require.Error(t, err)
	assert.ElementsMatch(t, []string{"scope[1]", "scope[2]"}, violatedFields(err))
}

// --- filters -----------------------------------------------------------

// TestScopeExpressionsMapToWorkitemFilters is the anti-drift check: scope must
// populate the same FilterOptions camp workitem uses, not a parallel matcher,
// so the two commands cannot disagree about what a campaign contains.
func TestScopeExpressionsMapToWorkitemFilters(t *testing.T) {
	scope := NewScope(DefaultProfile())

	require.NoError(t, scope.ApplyExpressions([]string{
		"type:design", "type:intent", "category:product", "status:active",
		"stage:ready", "attention-stage:next", "group:launch", "tag:urgent",
		"project:projects/camp", "query:hub",
	}))

	assert.Equal(t, []string{"design", "intent"}, scope.Filter.Types)
	assert.Equal(t, []string{"product"}, scope.Filter.Categories)
	assert.Equal(t, []string{"active"}, scope.Filter.Statuses)
	assert.Equal(t, []string{"ready"}, scope.Filter.LifecycleStages)
	assert.Equal(t, []string{"next"}, scope.Filter.AttentionStages)
	assert.Equal(t, []string{"launch"}, scope.Filter.Groups)
	assert.Equal(t, []string{"urgent"}, scope.Filter.Tags)
	assert.Equal(t, []string{"projects/camp"}, scope.Filter.Projects)
	assert.Equal(t, "hub", scope.Filter.Query)
}

// TestScopeAcceptsUnderscoreAttentionStage: the JSON field is attention_stage
// and the workitem flag is --attention-stage, so both spellings work.
func TestScopeAcceptsUnderscoreAttentionStage(t *testing.T) {
	scope := NewScope(DefaultProfile())

	require.NoError(t, scope.ApplyExpressions([]string{"attention_stage:parked"}))

	assert.Equal(t, []string{"parked"}, scope.Filter.AttentionStages)
}

// TestScopeAppliesTypeFilter proves the filter actually narrows the set.
func TestScopeAppliesTypeFilter(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("intent-a", "intent", "intent:a", "inbox", "next"),
	}
	scope := NewScope(DefaultProfile())
	require.NoError(t, scope.ApplyExpressions([]string{"type:design"}))

	got := scope.Apply(items)

	require.Len(t, got, 1)
	assert.Equal(t, "design:a", got[0].Key)
}

// TestProfileIncludeParkedControlsParkedRows: parked is a decision to revisit,
// not to forget, so the default keeps it in scope.
func TestProfileIncludeParkedControlsParkedRows(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("design-p", "design", "design:p", "active", "parked"),
	}

	included := NewScope(DefaultProfile()).Apply(items)
	assert.Len(t, included, 2, "the default profile reviews parked items")

	profile := DefaultProfile()
	profile.Scope.IncludeParked = false
	excluded := NewScope(profile).Apply(items)
	require.Len(t, excluded, 1)
	assert.Equal(t, "design:a", excluded[0].Key)
}

// TestProfileExcludeTypesDropsWholeTypes covers `scope.exclude_types`, the
// "leave festivals to fest" case the profile documents.
func TestProfileExcludeTypesDropsWholeTypes(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("festival-a", "festival", "festival:a", "active", "active"),
	}
	profile := DefaultProfile()
	profile.Scope.ExcludeTypes = []string{"festival"}

	got := NewScope(profile).Apply(items)

	require.Len(t, got, 1)
	assert.Equal(t, "design:a", got[0].Key)
}

// --- path globs --------------------------------------------------------

// TestPathGlobsMatchPrefixesAndPatterns: a bare directory name excludes
// everything beneath it, so a user does not have to know to write `dir/**`.
func TestPathGlobsMatchPrefixesAndPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"exact", "workflow/design/a", "workflow/design/a", true},
		{"directory prefix", "projects", "projects/camp/thing", true},
		{"directory prefix with slash", "projects/", "projects/camp", true},
		{"single-segment wildcard", "workflow/*", "workflow/design", true},
		{"wildcard covers children one level", "workflow/design/*", "workflow/design/a", true},
		{"non-match", "projects", "workflow/design/a", false},
		{"partial name is not a prefix", "proj", "projects/camp", false},
		{"empty pattern is ignored", "", "anything", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchesAnyGlob(tc.path, []string{tc.pattern}))
		})
	}
}

// TestScopePathIncludeNarrowsToASubtree covers `--scope path:...`.
func TestScopePathIncludeNarrowsToASubtree(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("intent-a", "intent", "intent:a", "inbox", "next"),
	}
	scope := NewScope(DefaultProfile())
	require.NoError(t, scope.ApplyExpressions([]string{"path:workflow/design"}))

	got := scope.Apply(items)

	require.Len(t, got, 1)
	assert.Equal(t, "design:a", got[0].Key)
}

// TestProfileExcludePathsDropsSubtrees covers `scope.exclude_paths`.
func TestProfileExcludePathsDropsSubtrees(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("intent-a", "intent", "intent:a", "inbox", "next"),
	}
	profile := DefaultProfile()
	profile.Scope.ExcludePaths = []string{"workflow/intent"}

	got := NewScope(profile).Apply(items)

	require.Len(t, got, 1)
	assert.Equal(t, "design:a", got[0].Key)
}

// TestEmptyScopeKeepsEverything: the default profile with no expressions is
// the whole campaign.
func TestEmptyScopeKeepsEverything(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("intent-a", "intent", "intent:a", "inbox", ""),
		item("festival-a", "festival", "festival:a", "active", "parked"),
	}

	got := NewScope(DefaultProfile()).Apply(items)

	assert.Len(t, got, 3)
}
