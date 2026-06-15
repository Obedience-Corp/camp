package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/workflow"
)

func TestItemsJSONOutput(t *testing.T) {
	root := setupFlowItemsWorkflow(t)
	restore := chdirFlowItems(t, root)
	defer restore()

	if err := os.WriteFile(filepath.Join(root, "active", "launch.md"), []byte("ship it\n"), 0644); err != nil {
		t.Fatalf("write active item: %v", err)
	}

	cmd := newItemsCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("flow items --json: %v\nstderr=%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var payload FlowItemsPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal flow items payload: %v\nraw=%s", err, stdout.String())
	}
	if payload.SchemaVersion != WorkflowItemsSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", payload.SchemaVersion, WorkflowItemsSchemaVersion)
	}
	if payload.GeneratedAt.IsZero() {
		t.Fatal("generated_at is zero")
	}
	if len(payload.Items) != 1 {
		t.Fatalf("items groups = %d, want 1: %#v", len(payload.Items), payload.Items)
	}
	group := payload.Items[0]
	if group.Status != "active" {
		t.Fatalf("status = %q, want active", group.Status)
	}
	if len(group.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(group.Entries), group.Entries)
	}
	if got := group.Entries[0].Name; got != "launch.md" {
		t.Fatalf("entry name = %q, want launch.md", got)
	}
}

func TestItemsJSONErrorEnvelopeOnListingFailure(t *testing.T) {
	root := setupFlowItemsWorkflow(t)
	restore := chdirFlowItems(t, root)
	defer restore()

	if err := os.RemoveAll(filepath.Join(root, "active")); err != nil {
		t.Fatalf("remove active dir: %v", err)
	}

	cmd := newItemsCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--json"})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("flow items --json error = nil, want non-zero error")
	}
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %T %v, want *CommandError", err, err)
	}
	if cmdErr.ExitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on error", stdout.String())
	}

	var envelope jsoncontract.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not error envelope JSON: %v\nraw=%s", err, stderr.String())
	}
	if envelope.SchemaVersion != WorkflowItemsSchemaVersion {
		t.Fatalf("error schema_version = %q, want %q", envelope.SchemaVersion, WorkflowItemsSchemaVersion)
	}
	if envelope.Error.ExitCode == 0 {
		t.Fatalf("error envelope exit_code = 0, want non-zero")
	}
	if envelope.Error.Message == "" {
		t.Fatal("error envelope message is empty")
	}
}

func setupFlowItemsWorkflow(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	svc := workflow.NewService(root)
	if _, err := svc.Init(context.Background(), workflow.InitOptions{}); err != nil {
		t.Fatalf("init workflow: %v", err)
	}
	return root
}

func chdirFlowItems(t *testing.T, dir string) func() {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd %s: %v", old, err)
		}
	}
}
