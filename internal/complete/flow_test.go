package complete

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

// createTestWorkflow creates a workflow with the given name in the campaign root.
func createTestWorkflow(t *testing.T, root, name string) string {
	t.Helper()

	workflowDir := filepath.Join(root, "workflow", "design", "active", name)
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}

	svc := workflow.NewService(workflowDir)
	ctx := context.Background()
	if _, err := svc.Init(ctx, workflow.InitOptions{}); err != nil {
		t.Fatalf("failed to init workflow: %v", err)
	}

	return workflowDir
}

// createTestItem creates an item (directory) in a workflow status.
func createTestItem(t *testing.T, workflowDir, status, itemName string) {
	t.Helper()

	itemPath := filepath.Join(workflowDir, status, itemName)
	if err := os.MkdirAll(itemPath, 0755); err != nil {
		t.Fatalf("failed to create item: %v", err)
	}
}

func TestIsFlowDirectory(t *testing.T) {
	root := t.TempDir()
	createTestWorkflow(t, root, "test-flow")

	ctx := context.Background()

	tests := []struct {
		name     string
		flowName string
		want     bool
	}{
		{
			name:     "existing flow",
			flowName: "test-flow",
			want:     true,
		},
		{
			name:     "non-existent flow",
			flowName: "nonexistent",
			want:     false,
		},
		{
			name:     "empty name",
			flowName: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFlowDirectory(ctx, root, tt.flowName)
			if got != tt.want {
				t.Errorf("IsFlowDirectory(%q) = %v, want %v", tt.flowName, got, tt.want)
			}
		})
	}
}

func TestFindFlowRoot(t *testing.T) {
	root := t.TempDir()
	workflowDir := createTestWorkflow(t, root, "my-flow")

	ctx := context.Background()

	tests := []struct {
		name     string
		flowName string
		wantPath string
	}{
		{
			name:     "existing flow",
			flowName: "my-flow",
			wantPath: workflowDir,
		},
		{
			name:     "non-existent flow",
			flowName: "nonexistent",
			wantPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindFlowRoot(ctx, root, tt.flowName)
			if got != tt.wantPath {
				t.Errorf("FindFlowRoot(%q) = %q, want %q", tt.flowName, got, tt.wantPath)
			}
		})
	}
}

func TestCompleteFlowNames(t *testing.T) {
	root := t.TempDir()
	createTestWorkflow(t, root, "flow-alpha")
	createTestWorkflow(t, root, "flow-beta")
	createTestWorkflow(t, root, "other-workflow")

	ctx := context.Background()

	tests := []struct {
		name    string
		partial string
		wantLen int
	}{
		{
			name:    "empty partial matches all",
			partial: "",
			wantLen: 3,
		},
		{
			name:    "prefix match",
			partial: "flow",
			wantLen: 2,
		},
		{
			name:    "exact match",
			partial: "other",
			wantLen: 1,
		},
		{
			name:    "no match",
			partial: "xyz",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := completeFlowNames(ctx, root, tt.partial)
			if err != nil {
				t.Fatalf("completeFlowNames error: %v", err)
			}
			if len(candidates) != tt.wantLen {
				t.Errorf("got %d candidates, want %d: %v", len(candidates), tt.wantLen, candidates)
			}
			// Verify all candidates have trailing slash
			for _, c := range candidates {
				if c[len(c)-1] != '/' {
					t.Errorf("candidate %q missing trailing slash", c)
				}
			}
		})
	}
}

func TestCompleteFlowStatuses(t *testing.T) {
	root := t.TempDir()
	createTestWorkflow(t, root, "test-flow")

	ctx := context.Background()

	tests := []struct {
		name     string
		flowName string
		partial  string
		wantLen  int
	}{
		{
			name:     "empty partial matches active and ready and dungeon",
			flowName: "test-flow",
			partial:  "",
			wantLen:  3, // active, ready, dungeon
		},
		{
			name:     "prefix match active",
			flowName: "test-flow",
			partial:  "act",
			wantLen:  1,
		},
		{
			name:     "prefix match dungeon",
			flowName: "test-flow",
			partial:  "dun",
			wantLen:  1,
		},
		{
			name:     "non-existent flow",
			flowName: "nonexistent",
			partial:  "",
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := completeFlowStatuses(ctx, root, tt.flowName, tt.partial)
			if err != nil {
				t.Fatalf("completeFlowStatuses error: %v", err)
			}
			if len(candidates) != tt.wantLen {
				t.Errorf("got %d candidates, want %d: %v", len(candidates), tt.wantLen, candidates)
			}
		})
	}
}

func TestCompleteFlowItems(t *testing.T) {
	root := t.TempDir()
	workflowDir := createTestWorkflow(t, root, "test-flow")

	// Add some items to active status
	createTestItem(t, workflowDir, "active", "feature-auth")
	createTestItem(t, workflowDir, "active", "feature-payments")
	createTestItem(t, workflowDir, "active", "bugfix-login")

	ctx := context.Background()

	tests := []struct {
		name    string
		parts   []string
		wantLen int
	}{
		{
			name:    "all items in active",
			parts:   []string{"test-flow", "active", ""},
			wantLen: 3,
		},
		{
			name:    "prefix match feature",
			parts:   []string{"test-flow", "active", "feature"},
			wantLen: 2,
		},
		{
			name:    "exact prefix bugfix",
			parts:   []string{"test-flow", "active", "bugfix"},
			wantLen: 1,
		},
		{
			name:    "no match",
			parts:   []string{"test-flow", "active", "xyz"},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := completeFlowItems(ctx, root, tt.parts)
			if err != nil {
				t.Fatalf("completeFlowItems error: %v", err)
			}
			if len(candidates) != tt.wantLen {
				t.Errorf("got %d candidates, want %d: %v", len(candidates), tt.wantLen, candidates)
			}
		})
	}
}

func TestCompleteFlowItems_NestedDungeon(t *testing.T) {
	root := t.TempDir()
	workflowDir := createTestWorkflow(t, root, "test-flow")

	// Add items to dungeon/completed
	createTestItem(t, workflowDir, "dungeon/completed", "old-feature")
	createTestItem(t, workflowDir, "dungeon/completed", "archive-task")

	ctx := context.Background()

	// Test completion in nested dungeon path
	parts := []string{"test-flow", "dungeon", "completed", ""}
	candidates, err := completeFlowItems(ctx, root, parts)
	if err != nil {
		t.Fatalf("completeFlowItems error: %v", err)
	}
	if len(candidates) != 2 {
		t.Errorf("got %d candidates, want 2: %v", len(candidates), candidates)
	}
}

func TestCompleteFlow_Integration(t *testing.T) {
	root := t.TempDir()
	workflowDir := createTestWorkflow(t, root, "my-workflow")
	createTestItem(t, workflowDir, "active", "task-one")
	createTestItem(t, workflowDir, "active", "task-two")

	ctx := context.Background()

	tests := []struct {
		name    string
		partial string
		wantLen int
	}{
		{
			name:    "complete flow names",
			partial: "my",
			wantLen: 1,
		},
		{
			name:    "complete statuses",
			partial: "my-workflow/",
			wantLen: 3, // active, ready, dungeon
		},
		{
			name:    "complete items in active",
			partial: "my-workflow/active/",
			wantLen: 2,
		},
		{
			name:    "complete items with prefix",
			partial: "my-workflow/active/task-o",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := CompleteFlow(ctx, root, tt.partial)
			if err != nil {
				t.Fatalf("CompleteFlow error: %v", err)
			}
			if len(candidates) != tt.wantLen {
				t.Errorf("CompleteFlow(%q) got %d candidates, want %d: %v",
					tt.partial, len(candidates), tt.wantLen, candidates)
			}
		})
	}
}

func TestCompleteFlow_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	createTestWorkflow(t, root, "test-flow")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := CompleteFlow(ctx, root, "test")
	if err == nil {
		t.Error("expected context error, got nil")
	}
}

func TestCompleteFlowInCategory(t *testing.T) {
	root := t.TempDir()

	// Don't create any workflows - this tests the detection logic
	ctx := context.Background()

	// Non-flow path should not be handled
	_, handled, _ := CompleteFlowInCategory(ctx, "design", root, "regular-query")
	if handled {
		t.Error("expected non-flow path to not be handled")
	}

	// Path without slash should not be handled
	_, handled, _ = CompleteFlowInCategory(ctx, "design", root, "noslash")
	if handled {
		t.Error("expected path without slash to not be handled")
	}
}
