package triage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderWorkflowDocIsDeterministic: the companion doc is generated from
// run data, so regenerating it must not churn a diff.
func TestRenderWorkflowDocIsDeterministic(t *testing.T) {
	run := statusRun(t)

	first := renderWorkflowDoc(run)
	second := renderWorkflowDoc(run)

	assert.Equal(t, first, second)
}

// TestRenderWorkflowDocDescribesTheRun checks it says what the run is and who
// acts in each phase, which is the whole reason it exists.
func TestRenderWorkflowDocDescribesTheRun(t *testing.T) {
	run := statusRun(t)

	body := renderWorkflowDoc(run)

	assert.Contains(t, body, run.ID)
	assert.Contains(t, body, run.Manifest.Profile.Name)
	assert.Contains(t, body, string(run.State.Phase))
	for _, phase := range Phases() {
		if Phase(phase) == PhaseAbandoned {
			continue // reachable from anywhere; not a step in the walk
		}
		assert.Contains(t, body, phase, "the phase list should name %s", phase)
	}
	assert.Contains(t, body, "camp triage status",
		"it must point at the command that reports real state")
}

// TestScaffoldWorkflowDocWritesIntoTheRun covers the write path.
func TestScaffoldWorkflowDocWritesIntoTheRun(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	path, err := ScaffoldWorkflowDoc(ctx, run)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(run.Dir, WorkflowDocFileName), path)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(body), "# Triage run "))
	assert.Contains(t, string(body), run.ID)
}

// TestScaffoldWorkflowDocRejectsAnEmptyRun guards the nil path rather than
// panicking on it.
func TestScaffoldWorkflowDocRejectsAnEmptyRun(t *testing.T) {
	_, err := ScaffoldWorkflowDoc(context.Background(), nil)

	assert.Error(t, err)
}

// TestScaffoldWorkflowDocRespectsCancellation stops before writing.
func TestScaffoldWorkflowDocRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ScaffoldWorkflowDoc(ctx, statusRun(t))

	assert.ErrorIs(t, err, context.Canceled)
}

// TestResolveProfileReturnsAValidDefault pins the seam the profile layer will
// replace: whatever it returns must pass the validation a run applies to it.
func TestResolveProfileReturnsAValidDefault(t *testing.T) {
	profile, err := ResolveProfile(context.Background(), "/campaign")

	require.NoError(t, err)
	assert.Empty(t, profile.validate("profile"))
	assert.Equal(t, ProfileSchemaVersion, profile.SchemaVersion)
	assert.Equal(t, ProfileNameDefault, ResolvedProfileName())
	assert.Equal(t, IdentityPolicyRepair, profile.Preflight.Identity,
		"repairing identity is the default; strict is opt-in")
}

// TestResolveProfileRespectsCancellation keeps the seam honest about context.
func TestResolveProfileRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveProfile(ctx, "/campaign")

	assert.ErrorIs(t, err, context.Canceled)
}
