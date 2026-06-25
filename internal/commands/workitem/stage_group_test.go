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

func runStageCmd(t *testing.T, selectorArg, stage string, jsonOut bool) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	err := runStage(context.Background(), cmd, selectorArg, stage, jsonOut)
	return stdout.String(), err
}

func runGroupCmd(t *testing.T, selectorArg, group string, jsonOut bool) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	err := runGroup(context.Background(), cmd, selectorArg, group, jsonOut)
	return stdout.String(), err
}

func TestStage_SetAndClear(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if _, err := runStageCmd(t, testWorkitemID, "staged", false); err != nil {
		t.Fatalf("stage set: %v", err)
	}
	store, err := priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	entry, ok := store.Attention[testWorkitemKey]
	if !ok {
		t.Fatalf("missing attention entry for %s", testWorkitemKey)
	}
	if entry.Stage != priority.AttentionStaged {
		t.Fatalf("stage = %q, want staged", entry.Stage)
	}

	if _, err := runStageCmd(t, testWorkitemID, "clear", false); err != nil {
		t.Fatalf("stage clear: %v", err)
	}
	store, err = priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load store after clear: %v", err)
	}
	if _, ok := store.Attention[testWorkitemKey]; ok {
		t.Fatalf("attention entry should be removed after clearing only stage")
	}
}

func TestGroup_SetAndClear(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if _, err := runGroupCmd(t, testWorkitemID, "camp-workflow", false); err != nil {
		t.Fatalf("group set: %v", err)
	}
	store, err := priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	entry, ok := store.Attention[testWorkitemKey]
	if !ok {
		t.Fatalf("missing attention entry for %s", testWorkitemKey)
	}
	if entry.Group != "camp-workflow" {
		t.Fatalf("group = %q, want camp-workflow", entry.Group)
	}

	if _, err := runGroupCmd(t, testWorkitemID, "clear", false); err != nil {
		t.Fatalf("group clear: %v", err)
	}
	store, err = priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load store after clear: %v", err)
	}
	if _, ok := store.Attention[testWorkitemKey]; ok {
		t.Fatalf("attention entry should be removed after clearing only group")
	}
}

func TestStage_JSONShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	out, err := runStageCmd(t, testWorkitemID, "current", true)
	if err != nil {
		t.Fatalf("stage --json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, out)
	}
	if payload["schema_version"] != WorkitemStageJSONVersion {
		t.Fatalf("schema_version = %v, want %s", payload["schema_version"], WorkitemStageJSONVersion)
	}
	if payload["attention_stage"] != "current" {
		t.Fatalf("attention_stage = %v, want current", payload["attention_stage"])
	}
}

func TestGroup_InvalidDoesNotMutateStore(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if _, err := runGroupCmd(t, testWorkitemID, "Bad Group", false); err == nil {
		t.Fatal("expected invalid group error")
	}
	if _, err := os.Stat(priority.StorePath(root)); !os.IsNotExist(err) {
		t.Fatalf("invalid group must not write store, stat err = %v", err)
	}
}
