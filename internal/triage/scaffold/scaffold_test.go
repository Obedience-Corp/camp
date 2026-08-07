package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNamesShipsEveryFileSpecDoc05Requires is the golden list. A file silently
// dropping out of the embed is the failure this guards: `//go:embed files`
// skips `_`-prefixed names, which had already dropped types/_default.yaml once.
func TestNamesShipsEveryFileSpecDoc05Requires(t *testing.T) {
	names, err := Names()
	require.NoError(t, err)

	assert.Equal(t, []string{
		"OBEY.md",
		"profile.yaml",
		"types/_default.yaml",
		"types/design.yaml",
		"types/explore.yaml",
		"types/festival.yaml",
		"types/intent.yaml",
	}, names)
}

// TestProfileScaffoldIsCommentedNotEmpty: the point of scaffolding is that a
// user can read what triage will do before running it. An uncommented profile
// would be an empty file with extra steps.
func TestProfileScaffoldIsCommentedNotEmpty(t *testing.T) {
	body, err := File("profile.yaml")
	require.NoError(t, err)
	text := string(body)

	assert.Contains(t, text, "schema_version: triage-profile/v1alpha1")
	for _, key := range []string{
		"scope:", "runs:", "review:", "evidence:", "anchors:",
		"routing:", "apply:", "outputs:",
	} {
		assert.Contains(t, text, key, "profile must ship %q with its default", key)
	}

	// Every top-level block carries explanatory comments, not just values.
	assert.GreaterOrEqual(t, strings.Count(text, "#"), 25,
		"every key ships with a comment; that is the scaffold's whole purpose")

	// The invariant that no profile can change is stated where it would
	// otherwise be assumed.
	assert.Contains(t, text, "ALWAYS require")
	assert.Contains(t, text, "recorded human approval")
}

// TestTypePoliciesShipTheirVocabularies pins what each type may be decided
// into, including the fallback an invented type inherits with zero config.
func TestTypePoliciesShipTheirVocabularies(t *testing.T) {
	tests := []struct {
		name         string
		wantEvidence string
		wantLabels   []string
	}{
		{
			name: "types/design.yaml", wantEvidence: "evidence: deep",
			wantLabels: []string{"completed: dungeon/completed", "consolidate: split"},
		},
		{
			name: "types/explore.yaml", wantEvidence: "evidence: metadata",
			wantLabels: []string{"completed: dungeon/completed"},
		},
		{
			name: "types/intent.yaml", wantEvidence: "evidence: metadata",
			wantLabels: []string{"ready: rail/ready", "someday: dungeon/someday"},
		},
		{
			name: "types/festival.yaml", wantEvidence: "evidence: none",
			wantLabels: []string{"completed: dungeon/completed"},
		},
		{
			name: "types/_default.yaml", wantEvidence: "evidence: metadata",
			wantLabels: []string{
				"completed: dungeon/completed", "consolidate: split",
				"parked: attention/parked", "ready: rail/ready",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := File(tt.name)
			require.NoError(t, err)
			text := string(body)

			assert.Contains(t, text, "schema_version: triage-type/v1alpha1")
			assert.Contains(t, text, tt.wantEvidence)
			for _, label := range tt.wantLabels {
				assert.Contains(t, text, label)
			}
		})
	}
}

// TestOBEYCoversTheDocumentedOutline: the guide is the thing a user reads
// first, so its sections are pinned rather than left to drift.
func TestOBEYCoversTheDocumentedOutline(t *testing.T) {
	body, err := File("OBEY.md")
	require.NoError(t, err)
	text := string(body)

	for _, section := range []string{
		"camp triage start",          // 1: what it is + one command
		"The phases, and who acts",   // 2: phase diagram with who acts
		"Where everything lives",     // 3: the file map
		"Reading the review",         // 4: reading + approve forms
		"What the profile controls",  // 5: keys by phase
		"What no profile can change", // 5: the invariants
		"The incremental model",      // 6
		"Recipes",                    // 7
	} {
		assert.Contains(t, text, section, "OBEY.md must cover %q", section)
	}

	// The invariants are reproduced, not summarized away.
	for _, invariant := range []string{
		"stable workitem identity",
		"recorded provenance",
		"recoverable moves",
		"authority restrictions",
		"advisory-only evidence workers",
		"interruption-safe session state",
		"stale-inventory detection",
		"post-application verification",
	} {
		assert.Contains(t, text, invariant)
	}

	// Seven recipes, as the outline asks.
	assert.GreaterOrEqual(t, strings.Count(text, "\n**"), 6,
		"the recipes section carries the documented set")
}

// TestEnsureCreatesThenIsANoOp is the scaffold's core behavior.
func TestEnsureCreatesThenIsANoOp(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	first, err := Ensure(ctx, root)
	require.NoError(t, err)
	assert.True(t, first.Wrote())
	assert.Len(t, first.Created(), 7)
	assert.Empty(t, first.Diverged())

	// Every file landed where OBEY.md says it does.
	for _, file := range first.Files {
		_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(file.Path)))
		require.NoError(t, statErr, "%s must exist", file.Path)
		assert.True(t, strings.HasPrefix(file.Path, DirName+"/"))
	}

	second, err := Ensure(ctx, root)
	require.NoError(t, err)
	assert.False(t, second.Wrote(), "a re-run creates nothing")
	assert.Empty(t, second.Diverged())
	for _, file := range second.Files {
		assert.Equal(t, StatusUnchanged, file.Status)
	}
}

// TestEnsureNeverOverwritesAnEditedFile is the rule that matters most: the
// profile exists to be edited, so a scaffold that "repaired" it would be the
// most destructive thing this package could do.
func TestEnsureNeverOverwritesAnEditedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	_, err := Ensure(ctx, root)
	require.NoError(t, err)

	profile := filepath.Join(root, DirName, "profile.yaml")
	edited := "# my campaign's profile\nschema_version: triage-profile/v1alpha1\n"
	require.NoError(t, os.WriteFile(profile, []byte(edited), 0o644))

	result, err := Ensure(ctx, root)
	require.NoError(t, err)

	assert.False(t, result.Wrote())
	assert.Equal(t, []string{DirName + "/profile.yaml"}, result.Diverged(),
		"an edited file is reported, never replaced")

	got, err := os.ReadFile(profile)
	require.NoError(t, err)
	assert.Equal(t, edited, string(got), "the user's edit survives untouched")
}

// TestEnsureRestoresOnlyWhatIsMissing: deleting one file and re-running fills
// that gap without disturbing the neighbours.
func TestEnsureRestoresOnlyWhatIsMissing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	_, err := Ensure(ctx, root)
	require.NoError(t, err)

	edited := filepath.Join(root, DirName, "types", "design.yaml")
	require.NoError(t, os.WriteFile(edited, []byte("# mine\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(root, DirName, "OBEY.md")))

	result, err := Ensure(ctx, root)
	require.NoError(t, err)

	assert.Equal(t, []string{DirName + "/OBEY.md"}, result.Created())
	assert.Equal(t, []string{DirName + "/types/design.yaml"}, result.Diverged())
}

// TestEnsureHonorsCancellation is the context rule.
func TestEnsureHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Ensure(ctx, t.TempDir())
	assert.ErrorIs(t, err, context.Canceled)
}

// TestDigestIsStable pins the comparison divergence is decided by.
func TestDigestIsStable(t *testing.T) {
	body, err := File("profile.yaml")
	require.NoError(t, err)
	assert.Equal(t, Digest(body), Digest(body))
	assert.NotEqual(t, Digest(body), Digest(append(body, '\n')))
	assert.True(t, strings.HasPrefix(Digest(body), "sha256:"))
}

// TestFileRejectsAnUnknownName keeps a typo from silently returning nothing.
func TestFileRejectsAnUnknownName(t *testing.T) {
	_, err := File("types/nope.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope.yaml")
}
