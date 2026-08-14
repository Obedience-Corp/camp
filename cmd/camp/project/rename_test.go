package project

import (
	"testing"

	projectrename "github.com/Obedience-Corp/camp/internal/project/rename"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppliedProjectRenamePlanPrefersRefreshedPlan(t *testing.T) {
	refreshed := &projectrename.PlanResult{
		AutoCommitEligible:   false,
		AutoCommitSkipReason: "affected file became dirty",
		CommitFiles:          []string{".gitmodules", "workflow/design/new/.workitem"},
	}

	got, err := appliedProjectRenamePlan(&projectrename.Result{Plan: refreshed})

	require.NoError(t, err)
	require.Same(t, refreshed, got)
	assert.False(t, got.AutoCommitEligible)
	assert.Equal(t, "affected file became dirty", got.AutoCommitSkipReason)
	assert.Equal(t, refreshed.CommitFiles, got.CommitFiles)
}

func TestAppliedProjectRenamePlanRejectsMissingPlan(t *testing.T) {
	_, err := appliedProjectRenamePlan(nil)
	require.EqualError(t, err, "project rename apply returned no refreshed plan")

	_, err = appliedProjectRenamePlan(&projectrename.Result{})
	require.EqualError(t, err, "project rename apply returned no refreshed plan")
}
