package flow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

func TestHistoryCommandDisplaysEntries(t *testing.T) {
	root := setupFlowItemsWorkflow(t)
	chdirFlowItems(t, root)

	if err := os.WriteFile(filepath.Join(root, "active", "launch.md"), []byte("ship it\n"), 0644); err != nil {
		t.Fatalf("write active item: %v", err)
	}
	svc := workflow.NewService(root)
	if _, err := svc.Move(context.Background(), "launch.md", "ready", workflow.MoveOptions{Reason: "ready"}); err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	cmd := newHistoryCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--item", "launch.md", "--limit", "1"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("flow history: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "launch.md: active -> ready") {
		t.Fatalf("history output missing transition: %q", output)
	}
	if !strings.Contains(output, "(ready)") {
		t.Fatalf("history output missing reason: %q", output)
	}
}

func TestHistoryCommandNoHistory(t *testing.T) {
	root := setupFlowItemsWorkflow(t)
	chdirFlowItems(t, root)

	cmd := newHistoryCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("flow history: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "no history" {
		t.Fatalf("output = %q, want no history", got)
	}
}
