package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateV1ToV2RejectsRootCollision(t *testing.T) {
	root, svc := testV1Workflow(t)
	writeWorkflowFile(t, filepath.Join(root, "active", "foo.md"), "active")
	writeWorkflowFile(t, filepath.Join(root, "foo.md"), "root")

	_, err := svc.MigrateV1ToV2(context.Background(), false)
	if err == nil {
		t.Fatal("expected root collision error")
	}
	if !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("error missing destination collision: %v", err)
	}

	assertWorkflowFile(t, filepath.Join(root, "active", "foo.md"), "active")
	assertWorkflowFile(t, filepath.Join(root, "foo.md"), "root")
	assertWorkflowSchemaVersion(t, root, 1)
}

func TestMigrateV1ToV2RejectsActiveReadyNameCollision(t *testing.T) {
	root, svc := testV1Workflow(t)
	writeWorkflowFile(t, filepath.Join(root, "active", "foo.md"), "from-active")
	writeWorkflowFile(t, filepath.Join(root, "ready", "foo.md"), "from-ready")

	_, err := svc.MigrateV1ToV2(context.Background(), false)
	if err == nil {
		t.Fatal("expected duplicate destination error")
	}
	if !strings.Contains(err.Error(), "duplicate destination") {
		t.Fatalf("error missing duplicate destination: %v", err)
	}

	assertWorkflowFile(t, filepath.Join(root, "active", "foo.md"), "from-active")
	assertWorkflowFile(t, filepath.Join(root, "ready", "foo.md"), "from-ready")
	assertWorkflowMissing(t, filepath.Join(root, "foo.md"))
	assertWorkflowSchemaVersion(t, root, 1)
}

func TestMigrateV1ToV2RollbackOnMoveFailure(t *testing.T) {
	root, svc := testV1Workflow(t)
	writeWorkflowFile(t, filepath.Join(root, "active", "a.md"), "from-active")
	writeWorkflowFile(t, filepath.Join(root, "ready", "b.md"), "from-ready")

	calls := 0
	_, err := svc.migrateV1ToV2(context.Background(), false, func(src, dst string) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		return executeWorkflowMigrationMove(src, dst)
	})
	if err == nil {
		t.Fatal("expected injected move failure")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("error missing rollback context: %v", err)
	}

	assertWorkflowFile(t, filepath.Join(root, "active", "a.md"), "from-active")
	assertWorkflowFile(t, filepath.Join(root, "ready", "b.md"), "from-ready")
	assertWorkflowMissing(t, filepath.Join(root, "a.md"))
	assertWorkflowMissing(t, filepath.Join(root, "b.md"))
	assertWorkflowSchemaVersion(t, root, 1)
}

func testV1Workflow(t *testing.T) (string, *Service) {
	t.Helper()
	root, _ := testWorkflow(t)
	createTestSchema(t, root)
	return root, NewService(root)
}

func writeWorkflowFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertWorkflowFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}

func assertWorkflowMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got err=%v", path, err)
	}
}

func assertWorkflowSchemaVersion(t *testing.T, root string, want int) {
	t.Helper()
	schema, err := LoadSchema(context.Background(), filepath.Join(root, SchemaFileName))
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}
	if schema.Version != want {
		t.Fatalf("schema version = %d, want %d", schema.Version, want)
	}
}
