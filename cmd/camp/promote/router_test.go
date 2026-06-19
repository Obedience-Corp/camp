package promote

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

type recordingRunner struct {
	bin  string
	dir  string
	args []string
}

func (r *recordingRunner) run(_ context.Context, dir, bin string, args []string) error {
	r.dir, r.bin, r.args = dir, bin, args
	return nil
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func promoteFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, ".campaign", "campaign.yaml"), "id: test-campaign\nname: Test\ntype: product\n")
	dir := filepath.Join(root, "workflow", "design", "sample")
	writeFixtureFile(t, filepath.Join(dir, ".workitem"), "version: v1alpha6\nkind: workitem\nid: design-sample-fixed\ntype: design\ntitle: Sample\n")
	writeFixtureFile(t, filepath.Join(dir, "README.md"), "# Sample\n\nbody\n")
	return root
}

func newRouterCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "promote [id]", Args: cobra.MaximumNArgs(1), RunE: runRouter}
	addPromoteRouterFlags(cmd)
	cmd.ValidArgsFunction = completePromotable
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetContext(context.Background())
	return cmd
}

func TestDispatchArgv(t *testing.T) {
	t.Run("intent forwards id and target", func(t *testing.T) {
		rec := &recordingRunner{}
		orig := runner
		runner = rec
		t.Cleanup(func() { runner = orig })

		if err := dispatchIntent(context.Background(), "add-dark", "festival"); err != nil {
			t.Fatalf("dispatchIntent: %v", err)
		}
		want := []string{"intent", "promote", "add-dark", "--target", "festival"}
		if !reflect.DeepEqual(rec.args, want) {
			t.Fatalf("args = %v, want %v", rec.args, want)
		}
	})

	t.Run("workitem dispatches via cwd dir without id", func(t *testing.T) {
		rec := &recordingRunner{}
		orig := runner
		runner = rec
		t.Cleanup(func() { runner = orig })

		dir := filepath.FromSlash("/camp/workflow/design/x")
		if err := dispatchWorkitem(context.Background(), dir, "doc", []string{"--no-commit"}); err != nil {
			t.Fatalf("dispatchWorkitem: %v", err)
		}
		want := []string{"workitem", "promote", "--target", "doc", "--no-commit"}
		if !reflect.DeepEqual(rec.args, want) {
			t.Fatalf("args = %v, want %v", rec.args, want)
		}
		if rec.dir != dir {
			t.Fatalf("dir = %q, want %q", rec.dir, dir)
		}
	})

	t.Run("festival dungeon target maps to --dungeon", func(t *testing.T) {
		rec := &recordingRunner{}
		origR := runner
		runner = rec
		origF := festLookup
		festLookup = func() (string, error) { return "fest", nil }
		t.Cleanup(func() { runner = origR; festLookup = origF })

		dir := filepath.FromSlash("/camp/festivals/planning/f")
		if err := dispatchFestival(context.Background(), dir, festPassthrough("archived", nil)); err != nil {
			t.Fatalf("dispatchFestival: %v", err)
		}
		want := []string{"promote", "--dungeon", "archived"}
		if !reflect.DeepEqual(rec.args, want) {
			t.Fatalf("args = %v, want %v", rec.args, want)
		}
		if rec.dir != dir || rec.bin != "fest" {
			t.Fatalf("dir/bin = %q/%q, want %q/fest", rec.dir, rec.bin, dir)
		}
	})

	t.Run("festival next stage forwards bare promote", func(t *testing.T) {
		rec := &recordingRunner{}
		origR := runner
		runner = rec
		origF := festLookup
		festLookup = func() (string, error) { return "fest", nil }
		t.Cleanup(func() { runner = origR; festLookup = origF })

		if err := dispatchFestival(context.Background(), "/x", festPassthrough("", nil)); err != nil {
			t.Fatalf("dispatchFestival: %v", err)
		}
		want := []string{"promote"}
		if !reflect.DeepEqual(rec.args, want) {
			t.Fatalf("args = %v, want %v", rec.args, want)
		}
	})
}

func TestIsPromotable(t *testing.T) {
	tests := []struct {
		name string
		item workitem.WorkItem
		want bool
	}{
		{"intent active excluded", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeIntent, LifecycleStage: workitem.LifecycleStageActive}, false},
		{"intent inbox", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeIntent, LifecycleStage: workitem.LifecycleStageInbox}, true},
		{"intent ready", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeIntent, LifecycleStage: workitem.LifecycleStageReady}, true},
		{"festival planning", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeFestival, LifecycleStage: workitem.LifecycleStagePlanning}, true},
		{"festival active", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeFestival, LifecycleStage: workitem.LifecycleStageActive}, true},
		{"festival none excluded", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeFestival, LifecycleStage: workitem.LifecycleStageNone}, false},
		{"design none kept", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeDesign, LifecycleStage: workitem.LifecycleStageNone}, true},
		{"explore none kept", workitem.WorkItem{WorkflowType: workitem.WorkflowTypeExplore, LifecycleStage: workitem.LifecycleStageNone}, true},
		{"custom type kept", workitem.WorkItem{WorkflowType: workitem.WorkflowType("feature"), LifecycleStage: workitem.LifecycleStageNone}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPromotable(tt.item); got != tt.want {
				t.Fatalf("isPromotable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRouterNonInteractiveGuard(t *testing.T) {
	root := promoteFixture(t)
	t.Chdir(root)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"json no id", []string{"--json"}, "no item in context"},
		{"id no target json", []string{"design:workflow/design/sample", "--json"}, "target is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRouterCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestRouterDryRunDoesNotDispatch(t *testing.T) {
	root := promoteFixture(t)
	t.Chdir(root)

	rec := &recordingRunner{}
	orig := runner
	runner = rec
	t.Cleanup(func() { runner = orig })

	cmd := newRouterCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"design:workflow/design/sample", "--target", "doc", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if rec.args != nil {
		t.Fatalf("dry-run must not dispatch, got args %v", rec.args)
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Fatalf("stdout = %q, want dry-run notice", out.String())
	}
}

func TestResolveItemDisambiguatesByWorkflowType(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, ".campaign", "campaign.yaml"), "id: t\nname: T\ntype: product\n")
	for _, wt := range []string{"design", "explore"} {
		dir := filepath.Join(root, "workflow", wt, "dup")
		writeFixtureFile(t, filepath.Join(dir, ".workitem"), "version: v1alpha6\nkind: workitem\nid: "+wt+"-dup\ntype: "+wt+"\ntitle: Dup\n")
		writeFixtureFile(t, filepath.Join(dir, "README.md"), "# Dup\n\nbody\n")
	}
	t.Chdir(filepath.Join(root, "workflow", "explore", "dup"))

	ctx := context.Background()
	cfg, cRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	resolver := paths.NewResolverFromConfig(cRoot, cfg)

	item, resolved, err := resolveItem(ctx, cRoot, resolver, nil)
	if err != nil {
		t.Fatalf("resolveItem: %v", err)
	}
	if !resolved {
		t.Fatal("expected cwd resolution to find the explore workitem")
	}
	if string(item.WorkflowType) != "explore" {
		t.Fatalf("resolved %s/dup, want explore/dup (basename collision must disambiguate by type)", item.WorkflowType)
	}
}

func TestCompletePromotable(t *testing.T) {
	root := promoteFixture(t)
	t.Chdir(root)

	cmd := newRouterCmd()
	got, directive := completePromotable(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v", directive)
	}
	found := false
	for _, id := range got {
		if id == "design:workflow/design/sample" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the design workitem id in completions, got %v", got)
	}

	none, _ := completePromotable(cmd, nil, "zzz-no-match")
	if len(none) != 0 {
		t.Fatalf("prefix should match nothing, got %v", none)
	}
}
