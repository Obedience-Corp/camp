package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

const (
	testWorkitemKey = "design:workflow/design/example"
	testWorkitemID  = "design-example-2026-05-24"
)

func runPriorityCmd(t *testing.T, selectorArg, level string, jsonOut bool) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	err := runPriority(context.Background(), cmd, selectorArg, level, jsonOut)
	return stdout.String(), err
}

func loadPriority(t *testing.T, root, key string) (priority.ManualPriority, bool) {
	t.Helper()
	store, err := priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load priority store: %v", err)
	}
	entry, ok := store.ManualPriorities[key]
	return entry.Priority, ok
}

func TestPriority_SetLevels(t *testing.T) {
	cases := []struct {
		name     string
		selector string
		level    string
		want     priority.ManualPriority
	}{
		{"high by id", testWorkitemID, "high", priority.High},
		{"medium by key", testWorkitemKey, "medium", priority.Medium},
		{"low by slug", "example", "low", priority.Low},
		{"medium alias", testWorkitemID, "med", priority.Medium},
		{"case insensitive", testWorkitemID, "HIGH", priority.High},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := linkTestCampaign(t)
			restore := chdir(t, root)
			defer restore()

			if _, err := runPriorityCmd(t, tc.selector, tc.level, false); err != nil {
				t.Fatalf("runPriority: %v", err)
			}

			got, ok := loadPriority(t, root, testWorkitemKey)
			if !ok {
				t.Fatalf("no priority entry stored for %s", testWorkitemKey)
			}
			if got != tc.want {
				t.Fatalf("priority = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrioritySetPrunesStaleEntries(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	store := priority.NewStore()
	priority.Set(store, "design:workflow/design/stale", priority.Low)
	if err := priority.Save(priority.StorePath(root), store); err != nil {
		t.Fatalf("save stale priority: %v", err)
	}

	if _, err := runPriorityCmd(t, testWorkitemID, "high", false); err != nil {
		t.Fatalf("runPriority: %v", err)
	}

	store, err := priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load priority store: %v", err)
	}
	if _, ok := store.ManualPriorities["design:workflow/design/stale"]; ok {
		t.Fatal("expected stale priority entry to be pruned")
	}
	entry, ok := store.ManualPriorities[testWorkitemKey]
	if !ok {
		t.Fatalf("expected priority entry for %s", testWorkitemKey)
	}
	if entry.Priority != priority.High {
		t.Fatalf("priority = %q, want %q", entry.Priority, priority.High)
	}
}

func TestPriority_ClearRemovesEntryAndDeletesEmptyStore(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if _, err := runPriorityCmd(t, testWorkitemID, "high", false); err != nil {
		t.Fatalf("seed priority: %v", err)
	}
	if _, ok := loadPriority(t, root, testWorkitemKey); !ok {
		t.Fatal("expected a stored priority before clearing")
	}

	if _, err := runPriorityCmd(t, testWorkitemID, "clear", false); err != nil {
		t.Fatalf("clear priority: %v", err)
	}

	if _, ok := loadPriority(t, root, testWorkitemKey); ok {
		t.Fatal("expected priority entry to be removed after clear")
	}
	if _, err := os.Stat(priority.StorePath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected empty store file to be deleted, stat err = %v", err)
	}
}

func TestPriority_JSONShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runPriorityCmd(t, testWorkitemID, "high", true)
	if err != nil {
		t.Fatalf("runPriority --json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, out)
	}
	if payload["schema_version"] != WorkitemPriorityJSONVersion {
		t.Fatalf("schema_version = %v, want %s", payload["schema_version"], WorkitemPriorityJSONVersion)
	}
	if payload["key"] != testWorkitemKey {
		t.Fatalf("key = %v, want %s", payload["key"], testWorkitemKey)
	}
	if payload["priority"] != "high" {
		t.Fatalf("priority = %v, want high", payload["priority"])
	}
	if payload["cleared"] != false {
		t.Fatalf("cleared = %v, want false", payload["cleared"])
	}
}

func TestPriority_JSONClearedShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runPriorityCmd(t, testWorkitemID, "clear", true)
	if err != nil {
		t.Fatalf("runPriority --json clear: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, out)
	}
	if payload["cleared"] != true {
		t.Fatalf("cleared = %v, want true", payload["cleared"])
	}
	if payload["priority"] != "" {
		t.Fatalf("priority = %v, want empty", payload["priority"])
	}
}

func TestPriority_InvalidLevelIsValidationError(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	_, err := runPriorityCmd(t, testWorkitemID, "urgent", false)
	if err == nil {
		t.Fatal("expected an error for an unknown priority level")
	}
	if _, statErr := os.Stat(priority.StorePath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("invalid level must not write the store, stat err = %v", statErr)
	}
}

func TestPriority_UnknownSelectorIsError(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	_, err := runPriorityCmd(t, "does-not-exist", "high", false)
	if err == nil {
		t.Fatal("expected an error for an unresolvable selector")
	}
}

func TestPriority_ContextCancellationDoesNotMutateStore(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runPriority(ctx, cmd, testWorkitemID, "high", false); err == nil {
		t.Fatal("expected a cancellation error")
	}
	if _, statErr := os.Stat(priority.StorePath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled run must not write the store, stat err = %v", statErr)
	}
}
