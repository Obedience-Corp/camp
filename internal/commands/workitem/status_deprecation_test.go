package workitem

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestStatusDeprecationNoticeOnBareWorkitem verifies that --status on the bare
// `camp workitem` command emits a deprecation notice to stderr while still
// filtering correctly (issue #605).
func TestStatusDeprecationNoticeOnBareWorkitem(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	var stderr bytes.Buffer
	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"--list", "--status", "active"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("workitem --list --status active: %v", err)
	}
	got := stderr.String()
	if !strings.Contains(got, "deprecated") {
		t.Fatalf("expected deprecation notice on stderr, got:\n%s", got)
	}
	if !strings.Contains(got, "--stage") || !strings.Contains(got, "--attention-stage") {
		t.Fatalf("deprecation notice should name --stage and --attention-stage, got:\n%s", got)
	}
}

// TestStatusDeprecationNoticeOnListSubcommand verifies the same deprecation
// notice fires on `camp workitem list`.
func TestStatusDeprecationNoticeOnListSubcommand(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	var stderr bytes.Buffer
	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"list", "--status", "active"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("workitem list --status active: %v", err)
	}
	got := stderr.String()
	if !strings.Contains(got, "deprecated") {
		t.Fatalf("expected deprecation notice on stderr, got:\n%s", got)
	}
}

// TestStatusDeprecationNoNoticeWhenUnused verifies the deprecation notice does
// not fire when --status is not passed.
func TestStatusDeprecationNoNoticeWhenUnused(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	var stderr bytes.Buffer
	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"--list", "--type", "design"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("workitem --list --type design: %v", err)
	}
	if got := stderr.String(); strings.Contains(strings.ToLower(got), "deprecated") {
		t.Fatalf("deprecation notice should not fire without --status, got:\n%s", got)
	}
}

// TestStatusStillFilters verifies --status still filters correctly after
// deprecation (behavior is unchanged).
func TestStatusStillFilters(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	var stderr bytes.Buffer
	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"--json", "--status", "active"})
	cmd.SetErr(&stderr)
	stdout, err := captureStdout(func() error {
		return cmd.ExecuteContext(context.Background())
	})
	if err != nil {
		t.Fatalf("workitem --json --status active: %v", err)
	}
	// The design workitem in linkTestCampaign has attention_stage "active"
	// (derived), so filtering by --status active should match it.
	if !strings.Contains(stdout, "design:workflow/design/example") {
		t.Fatalf("--status active should match the design workitem, got:\n%s", stdout)
	}
	if !strings.Contains(stderr.String(), "deprecated") {
		t.Fatalf("deprecation notice should fire on --json --status too, got stderr:\n%s", stderr.String())
	}
}

// TestStatusDeprecationJSONUnaffected verifies that --json output is not
// corrupted by the deprecation notice (the notice goes to stderr only).
func TestStatusDeprecationJSONUnaffected(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	var stderr bytes.Buffer
	cmd := NewWorkitemCommand()
	cmd.SetArgs([]string{"--json", "--status", "none"})
	cmd.SetErr(&stderr)
	stdout, err := captureStdout(func() error {
		return cmd.ExecuteContext(context.Background())
	})
	if err != nil {
		t.Fatalf("workitem --json --status none: %v", err)
	}
	if !strings.Contains(stdout, "schema_version") {
		t.Fatalf("JSON output missing schema_version, got:\n%s", stdout)
	}
	if !strings.Contains(stderr.String(), "deprecated") {
		t.Fatalf("deprecation notice should be on stderr, got:\n%s", stderr.String())
	}
	if strings.Contains(strings.ToLower(stdout), "deprecated") {
		t.Fatalf("deprecation notice leaked into JSON stdout:\n%s", stdout)
	}
}
