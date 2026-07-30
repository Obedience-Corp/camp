package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

func execDemote(t *testing.T, cwd string, args ...string) (string, string, error) {
	t.Helper()
	restore := chdir(t, cwd)
	defer restore()

	cmd := newDemoteCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// A resident returns to its type root, taking its content and marker with it.
func TestDemoteResidentReturnsHome(t *testing.T) {
	for _, stage := range []string{"ready", "active"} {
		t.Run(stage, func(t *testing.T) {
			root := promoteCampaign(t)
			src := addResident(t, root, stage, "design", "widget", "Widget")

			if _, _, err := execDemote(t, src, "--no-commit"); err != nil {
				t.Fatalf("demote from %s: %v", stage, err)
			}

			home := filepath.Join(root, "workflow", "design", "widget")
			if _, err := os.Stat(filepath.Join(home, ".workitem")); err != nil {
				t.Errorf("marker did not travel home: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, "README.md")); err != nil {
				t.Errorf("content did not travel home: %v", err)
			}
			if dirExists(src) {
				t.Errorf("resident should have left %s", src)
			}
		})
	}
}

// The type root comes from the marker, not from the folder the resident sat in.
func TestDemoteUsesMarkerTypeForHome(t *testing.T) {
	root := promoteCampaign(t)
	src := addResident(t, root, "active", "explore", "topic", "Topic")

	if _, _, err := execDemote(t, src, "--no-commit"); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if !dirExists(filepath.Join(root, "workflow", "explore", "topic")) {
		t.Error("explore resident should land under workflow/explore")
	}
	if dirExists(filepath.Join(root, "workflow", "design", "topic")) {
		t.Error("must not land under a type it never belonged to")
	}
}

// Demote re-homes path-keyed state, unlike a shelve, because the workitem stays
// active.
func TestDemoteRehomesPathState(t *testing.T) {
	root := promoteCampaign(t)
	src := addResident(t, root, "active", "design", "widget", "Widget")
	oldKey := "design:festivals/active/widget"
	newKey := "design:workflow/design/widget"

	storePath := priority.StorePath(root)
	if err := priority.WithLock(context.Background(), storePath, func(s *priority.Store) error {
		priority.Set(s, oldKey, priority.High)
		return nil
	}); err != nil {
		t.Fatalf("seed priority: %v", err)
	}

	if _, _, err := execDemote(t, src, "--no-commit"); err != nil {
		t.Fatalf("demote: %v", err)
	}

	store, err := priority.Load(storePath)
	if err != nil {
		t.Fatalf("reload priority: %v", err)
	}
	if _, ok := store.ManualPriorities[oldKey]; ok {
		t.Errorf("old key %q survived the demote", oldKey)
	}
	if _, ok := store.ManualPriorities[newKey]; !ok {
		t.Errorf("priority did not follow the workitem home to %q: %+v", newKey, store.ManualPriorities)
	}
}

func TestDemoteRejections(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string) string
		wantErr string
	}{
		{
			name: "already at type root",
			setup: func(t *testing.T, root string) string {
				return addWorkitem(t, root, "design", "widget", "Widget", "body")
			},
			wantErr: "already at its type root",
		},
		{
			name: "from a dungeon",
			setup: func(t *testing.T, root string) string {
				dir := filepath.Join(root, "workflow", "design", "dungeon", "completed", "2026-07-24", "widget")
				writeFile(t, filepath.Join(dir, ".workitem"),
					"version: v1alpha8\nkind: workitem\nid: design-widget-fixed\ntype: design\ntitle: Widget\n")
				return dir
			},
			wantErr: "restoring a shelved workitem is not a demote",
		},
		{
			name: "destination occupied",
			setup: func(t *testing.T, root string) string {
				addWorkitem(t, root, "design", "widget", "Occupant", "body")
				return addResident(t, root, "ready", "design", "widget", "Resident")
			},
			wantErr: "already exists",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := promoteCampaign(t)
			src := tc.setup(t, root)

			_, _, err := execDemote(t, src, "--no-commit")
			if err == nil {
				t.Fatalf("expected %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
			if !dirExists(src) {
				t.Error("a refused demote must leave the source in place")
			}
		})
	}
}

func TestDemoteDryRunChangesNothing(t *testing.T) {
	root := promoteCampaign(t)
	src := addResident(t, root, "active", "design", "widget", "Widget")

	out, _, err := execDemote(t, src, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "would demote") || !strings.Contains(out, "workflow/design/widget") {
		t.Errorf("dry-run output should name the plan and destination, got %q", out)
	}
	if !dirExists(src) {
		t.Error("dry-run must not move the resident")
	}
	if dirExists(filepath.Join(root, "workflow", "design", "widget")) {
		t.Error("dry-run must not create the destination")
	}
}

func TestDemoteIsAgentAllowed(t *testing.T) {
	cmd := newDemoteCommand()
	if cmd.Annotations["agent_allowed"] != "true" {
		t.Errorf("agent_allowed = %q, want true", cmd.Annotations["agent_allowed"])
	}
	if cmd.Annotations["agent_reason"] == "" {
		t.Error("agent_reason must explain why the command is agent-safe")
	}
}
