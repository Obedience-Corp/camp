//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createDesignWorkitem(t *testing.T, tc *TestContainer, campaign, slug, title, body string) {
	t.Helper()
	_, err := tc.RunCampInDir(campaign, "workitem", "create", slug, "--type", "design", "--title", title)
	require.NoError(t, err, "create design workitem %s", slug)
	require.NoError(t, tc.WriteFile(campaign+"/workflow/design/"+slug+"/README.md", body))
}

func TestGatherDesign_MovesSourcesIntoGatheredPackage(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := "/campaigns/gather-design"

	_, err := tc.InitCampaign(campaign, "gather-design", "product")
	require.NoError(t, err)

	createDesignWorkitem(t, tc, campaign, "auth-flow", "Auth Flow", "# Auth Flow\n\nLogin and session design.\n")
	createDesignWorkitem(t, tc, campaign, "auth-tokens", "Auth Tokens", "# Auth Tokens\n\nToken rotation design.\n")
	createDesignWorkitem(t, tc, campaign, "billing", "Billing", "# Billing\n\nUnrelated design.\n")
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed design workitems")

	output, err := tc.RunCampInDir(campaign, "gather", "design", "auth-flow", "auth-tokens", "--title", "Unified Auth", "--json")
	require.NoError(t, err, "gather output: %s", output)

	var result struct {
		SchemaVersion string `json:"schema_version"`
		Gathered      struct {
			ID           string `json:"id"`
			Ref          string `json:"ref"`
			Type         string `json:"type"`
			RelativePath string `json:"relative_path"`
		} `json:"gathered"`
		Sources []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"sources"`
		Committed bool     `json:"committed"`
		Warnings  []string `json:"warnings"`
	}
	jsonStart := strings.Index(output, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON in output: %s", output)
	require.NoError(t, json.Unmarshal([]byte(output[jsonStart:]), &result), "output: %s", output)

	assert.Equal(t, "workitem-gather/v1alpha1", result.SchemaVersion)
	assert.Equal(t, "design", result.Gathered.Type)
	assert.Equal(t, "workflow/design/unified-auth", result.Gathered.RelativePath)
	assert.NotEmpty(t, result.Gathered.ID)
	assert.Regexp(t, `^WI-[0-9a-f]{6}$`, result.Gathered.Ref)
	assert.Len(t, result.Sources, 2)
	assert.True(t, result.Committed, "gather should auto-commit")
	assert.Empty(t, result.Warnings, "gather should complete without warnings")

	// Source directories moved inside the gathered package.
	for _, slug := range []string{"auth-flow", "auth-tokens"} {
		moved, err := tc.CheckDirExists(campaign + "/workflow/design/unified-auth/" + slug)
		require.NoError(t, err)
		assert.True(t, moved, "%s should live inside the gathered package", slug)

		old, err := tc.CheckDirExists(campaign + "/workflow/design/" + slug)
		require.NoError(t, err)
		assert.False(t, old, "%s should no longer exist at the top level", slug)
	}

	// Generated README indexes both sources.
	readme, err := tc.ReadFile(campaign + "/workflow/design/unified-auth/README.md")
	require.NoError(t, err)
	assert.Contains(t, readme, "# Unified Auth")
	assert.Contains(t, readme, "[Auth Flow](auth-flow/README.md)")
	assert.Contains(t, readme, "[Auth Tokens](auth-tokens/README.md)")

	// Sources are stamped with gather lineage.
	marker, err := tc.ReadFile(campaign + "/workflow/design/unified-auth/auth-flow/.workitem")
	require.NoError(t, err)
	assert.Contains(t, marker, "gathered_into: "+result.Gathered.ID)
	assert.Contains(t, marker, "gathered_at:")

	// Discovery lists the gathered package but not the moved sources.
	listOutput, err := tc.RunCampInDir(campaign, "workitem", "--json", "--type", "design")
	require.NoError(t, err)
	assert.Contains(t, listOutput, "workflow/design/unified-auth")
	assert.Contains(t, listOutput, "workflow/design/billing")
	assert.NotContains(t, listOutput, `"relative_path": "workflow/design/auth-flow"`)
	assert.NotContains(t, listOutput, `"relative_path": "workflow/design/auth-tokens"`)

	// The move landed in one commit that leaves the tree clean.
	gitLog := tc.GitOutput(t, campaign, "log", "-1", "--pretty=%s")
	assert.Contains(t, gitLog, "WorkitemGather: Gather 2 design workitems into workflow/design/unified-auth")
	gitStatus := tc.GitOutput(t, campaign, "status", "--porcelain")
	assert.Empty(t, strings.TrimSpace(gitStatus), "working tree should be clean after gather commit")

	// Unchanged source docs are recorded as renames, preserving history.
	nameStatus := tc.GitOutput(t, campaign, "log", "-1", "-M", "--name-status", "--pretty=format:")
	assert.Contains(t, nameStatus, "workflow/design/unified-auth/auth-flow/README.md")
	assert.Regexp(t, `R\d+\s+workflow/design/auth-flow/README.md`, nameStatus)

	// Audit trail records the gather.
	audit, err := tc.ReadFile(campaign + "/.campaign/workitems/.workitems.jsonl")
	require.NoError(t, err)
	assert.Contains(t, audit, `"event":"gather"`)
	assert.Contains(t, audit, `"gathered_into":"`+result.Gathered.ID+`"`)
}

func TestGatherDesign_PriorityMigratesToGatheredItem(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := "/campaigns/gather-design-priority"

	_, err := tc.InitCampaign(campaign, "gather-design-priority", "product")
	require.NoError(t, err)

	createDesignWorkitem(t, tc, campaign, "cache-l1", "Cache L1", "# Cache L1\n\nDesign.\n")
	createDesignWorkitem(t, tc, campaign, "cache-l2", "Cache L2", "# Cache L2\n\nDesign.\n")

	_, err = tc.RunCampInDir(campaign, "workitem", "priority", "cache-l1", "high")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(campaign, "workitem", "priority", "cache-l2", "low")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaign, "gather", "design", "cache-l1", "cache-l2", "--title", "Cache Design")
	require.NoError(t, err, "gather output: %s", output)

	store, err := tc.ReadFile(campaign + "/.campaign/settings/workitems.json")
	require.NoError(t, err)
	assert.Contains(t, store, "design:workflow/design/cache-design")
	assert.Contains(t, store, `"priority": "high"`)
	assert.NotContains(t, store, "design:workflow/design/cache-l1")
	assert.NotContains(t, store, "design:workflow/design/cache-l2")
}

func TestGatherDesign_AuditFailureIsWarningNotError(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := "/campaigns/gather-design-audit-warn"

	_, err := tc.InitCampaign(campaign, "gather-design-audit-warn", "product")
	require.NoError(t, err)

	createDesignWorkitem(t, tc, campaign, "svc-a", "Service A", "# Service A\n\nDesign.\n")
	createDesignWorkitem(t, tc, campaign, "svc-b", "Service B", "# Service B\n\nDesign.\n")
	tc.GitOutput(t, campaign, "add", "-A")
	tc.GitOutput(t, campaign, "commit", "-m", "seed design workitems")

	// Force the post-move audit append to fail by replacing the audit log with a
	// directory, so os.OpenFile(..., O_APPEND) errors even as root. Audit runs
	// only after the sources are already moved, so the command must surface a
	// warning and still exit zero rather than stranding a mutated filesystem
	// behind a non-zero exit.
	auditPath := campaign + "/.campaign/workitems/.workitems.jsonl"
	tc.Shell(t, "rm -rf "+auditPath+" && mkdir -p "+auditPath)

	output, err := tc.RunCampInDir(campaign, "gather", "design", "svc-a", "svc-b", "--title", "Unified Svc", "--no-commit", "--json")
	require.NoError(t, err, "audit failure must not fail the command; output: %s", output)

	var result struct {
		Warnings []string `json:"warnings"`
	}
	jsonStart := strings.Index(output, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON in output: %s", output)
	require.NoError(t, json.Unmarshal([]byte(output[jsonStart:]), &result), "output: %s", output)

	// The gather still applied on disk despite the audit failure.
	moved, err := tc.CheckDirExists(campaign + "/workflow/design/unified-svc/svc-a")
	require.NoError(t, err)
	assert.True(t, moved, "sources should be moved despite the audit failure")

	// The audit write failure is reported as a warning, not swallowed or fatal.
	var auditWarn bool
	for _, w := range result.Warnings {
		if strings.Contains(w, "gather audit event") {
			auditWarn = true
			break
		}
	}
	assert.True(t, auditWarn, "audit write failure should surface as a warning, got: %v", result.Warnings)
}

func TestGatherDesign_Guards(t *testing.T) {
	tc := GetSharedContainer(t)
	campaign := "/campaigns/gather-design-guards"

	_, err := tc.InitCampaign(campaign, "gather-design-guards", "product")
	require.NoError(t, err)

	createDesignWorkitem(t, tc, campaign, "solo", "Solo", "# Solo\n\nDesign.\n")
	createDesignWorkitem(t, tc, campaign, "duo", "Duo", "# Duo\n\nDesign.\n")

	// Fewer than 2 sources is rejected.
	output, err := tc.RunCampInDir(campaign, "gather", "design", "solo", "--title", "Only One")
	require.Error(t, err)
	assert.Contains(t, output, "need at least 2")

	// Duplicate selectors collapse and are rejected.
	output, err = tc.RunCampInDir(campaign, "gather", "design", "solo", "solo", "--title", "Same Twice")
	require.Error(t, err)
	assert.Contains(t, output, "need at least 2")

	// Missing title is rejected.
	output, err = tc.RunCampInDir(campaign, "gather", "design", "solo", "duo")
	require.Error(t, err)
	assert.Contains(t, output, "--title is required")

	// Unknown selector is rejected.
	output, err = tc.RunCampInDir(campaign, "gather", "design", "solo", "missing", "--title", "Nope")
	require.Error(t, err)
	assert.Contains(t, output, "no workitem matched selector missing")

	// Existing target directory is rejected.
	output, err = tc.RunCampInDir(campaign, "gather", "design", "solo", "duo", "--title", "Solo", "--slug", "duo")
	require.Error(t, err)
	assert.Contains(t, output, "target directory already exists")

	// Dry run changes nothing.
	output, err = tc.RunCampInDir(campaign, "gather", "design", "solo", "duo", "--title", "Pair", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "dry-run: would gather 2 design workitems")
	exists, err := tc.CheckDirExists(campaign + "/workflow/design/pair")
	require.NoError(t, err)
	assert.False(t, exists, "dry-run must not create the target")

	// Non-interactive invocation without selectors is rejected.
	output, err = tc.RunCampInDir(campaign, "gather", "design", "--title", "Bare")
	require.Error(t, err)
	assert.Contains(t, output, "pass at least 2 design workitem selectors")
}
