package workitem

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
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

// TestStatusDeprecationListExampleAvoidsStatusFlag verifies operator examples
// teach --stage / --attention-stage instead of the deprecated --status flag.
func TestStatusDeprecationListExampleAvoidsStatusFlag(t *testing.T) {
	cmd := newListCommand()
	if strings.Contains(cmd.Long, "--status") {
		t.Fatalf("list command Long still steers to --status:\n%s", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "--stage ready") {
		t.Fatalf("list command Long should show --stage, got:\n%s", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "--attention-stage") {
		t.Fatalf("list command Long should show --attention-stage, got:\n%s", cmd.Long)
	}
}

// TestStatusDeprecationUnknownFilterGuidesToReplacementFlags verifies unknown
// positional filters name the two real axes, not --status.
func TestStatusDeprecationUnknownFilterGuidesToReplacementFlags(t *testing.T) {
	state := &discoveredWorkitems{cfg: &config.CampaignConfig{}}
	err := applyPositionalFilter("does-not-exist", state, &listOptions{})
	if err == nil {
		t.Fatal("applyPositionalFilter(does-not-exist) error = nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "--status") {
		t.Fatalf("unknown filter error still steers to --status: %s", msg)
	}
	for _, want := range []string{"--type", "--stage", "--attention-stage", "--category"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("unknown filter error missing %s: %s", want, msg)
		}
	}
}

// TestStatusDeprecationAmbiguousFilterGuidesToReplacementFlags verifies
// ambiguous positional filters pin --type/--category and the two real axes.
func TestStatusDeprecationAmbiguousFilterGuidesToReplacementFlags(t *testing.T) {
	state := &discoveredWorkitems{
		cfg:   &config.CampaignConfig{},
		items: []wkitem.WorkItem{{WorkflowType: wkitem.WorkflowType("plan"), WorkflowCategory: "plan"}},
	}
	err := applyPositionalFilter("plan", state, &listOptions{})
	if err == nil {
		t.Fatal("applyPositionalFilter(plan) error = nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "--status") {
		t.Fatalf("ambiguous filter error still steers to --status: %s", msg)
	}
	for _, want := range []string{"--type plan", "--category plan", "--stage plan", "--attention-stage plan"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("ambiguous filter error missing %s: %s", want, msg)
		}
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
