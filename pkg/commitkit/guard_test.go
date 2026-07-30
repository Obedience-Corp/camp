package commitkit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/stageguard"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// The re-exports must be aliases, not distinct named types, so a consumer
// passes and receives exactly the types the guard uses internally and camp
// never converts between them. These declarations fail to compile if any
// re-export becomes a separate type, which is the whole assertion: the check
// is the build, not a runtime comparison.
var (
	_ stageguard.GuardLimits    = commitkit.GuardLimits{}
	_ stageguard.GuardViolation = commitkit.GuardViolation{}
	_ stageguard.ViolationKind  = commitkit.OverThreshold
	_ stageguard.Mode           = commitkit.ModeAuto

	// And the reverse direction, so neither side is merely assignable to the
	// other through an implicit conversion.
	_ commitkit.GuardLimits    = stageguard.GuardLimits{}
	_ commitkit.GuardViolation = stageguard.GuardViolation{}
)

// StageOptions must alias the internal override type the same way: the
// CommitLarge a consumer sets is the CommitLarge the guard reads.
var (
	_ git.StageOptions       = commitkit.StageOptions{CommitLarge: true}
	_ commitkit.StageOptions = git.StageOptions{}
)

func TestGuardConstantsMatchStageguard(t *testing.T) {
	assert.Equal(t, stageguard.Bulk, commitkit.Bulk)
	assert.Equal(t, stageguard.TrackedGrowth, commitkit.TrackedGrowth)
	assert.Equal(t, stageguard.ModeBlock, commitkit.ModeBlock)
	assert.Equal(t, stageguard.ModeOff, commitkit.ModeOff)
}

// nonexistentRepo is a path that is never created. Every use below is guarded
// by an already-cancelled context, so nothing should ever reach the disk.
const nonexistentRepo = "/nonexistent/stageguard-ctx-check"

func TestCheckStagingHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A cancelled context short-circuits before any filesystem or git access,
	// so the path deliberately does not exist: if this ever returns something
	// other than context.Canceled, the short-circuit regressed.
	_, err := commitkit.CheckStaging(ctx, nonexistentRepo, commitkit.GuardLimits{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestResolveLimitsHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := commitkit.ResolveLimits(ctx, nonexistentRepo)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestStageAllWithOptionsHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := commitkit.StageAllWithOptions(ctx, nonexistentRepo, commitkit.StageOptions{CommitLarge: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestStageAllHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := commitkit.StageAll(ctx, nonexistentRepo)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
