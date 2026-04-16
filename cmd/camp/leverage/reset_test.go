package leverage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/project"
)

func executeReset(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("CAMP_CACHE_DISABLE", "1")

	resetFlagSet(leverageResetCmd.Flags())

	var buf bytes.Buffer
	root := newTestRootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"leverage", "reset"}, args...))

	err := root.Execute()
	return buf.String(), err
}

func setupSnapshotDir(t *testing.T, projects map[string][]string) string {
	t.Helper()

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}

	snapshotDir := intleverage.DefaultSnapshotDir(tmpDir)
	for proj, dates := range projects {
		projDir := filepath.Join(snapshotDir, proj)
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, date := range dates {
			path := filepath.Join(projDir, date+".json")
			if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	return tmpDir
}

func TestLeverageReset_ClearsAllSnapshots(t *testing.T) {
	root := setupSnapshotDir(t, map[string][]string{
		"camp": {"2025-06-01", "2025-06-08"},
		"fest": {"2025-06-01"},
	})
	t.Setenv("CAMP_ROOT", root)

	output, err := executeReset(t)
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, "Cleared all cached leverage data") {
		t.Errorf("unexpected output: %s", output)
	}
	if !strings.Contains(output, "camp leverage backfill") {
		t.Errorf("output should remind user to re-backfill: %s", output)
	}

	snapshotDir := intleverage.DefaultSnapshotDir(root)
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Errorf("snapshot directory should not exist after reset, got err: %v", err)
	}
}

func TestLeverageReset_ClearsProjectOnly(t *testing.T) {
	root := setupSnapshotDir(t, map[string][]string{
		"camp": {"2025-06-01", "2025-06-08"},
		"fest": {"2025-06-01", "2025-06-15"},
	})
	t.Setenv("CAMP_ROOT", root)

	output, err := executeReset(t, "--project", "camp")
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `Cleared cached data for project "camp"`) {
		t.Errorf("unexpected output: %s", output)
	}

	campDir := filepath.Join(intleverage.DefaultSnapshotDir(root), "camp")
	if _, err := os.Stat(campDir); !os.IsNotExist(err) {
		t.Errorf("camp snapshot directory should not exist, got err: %v", err)
	}

	festDir := filepath.Join(intleverage.DefaultSnapshotDir(root), "fest")
	entries, err := os.ReadDir(festDir)
	if err != nil {
		t.Fatalf("fest dir should still exist: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("fest should have 2 snapshots, got %d", len(entries))
	}
}

func TestLeverageReset_NoSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_ROOT", tmpDir)

	output, err := executeReset(t)
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, "No cached data to clear") {
		t.Errorf("expected 'No cached data to clear', got: %s", output)
	}
}

func TestLeverageReset_ProjectFlagValidation(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_ROOT", tmpDir)

	tests := []struct {
		name          string
		projectFilter string
		wantErr       bool
	}{
		{name: "traversal attempt", projectFilter: "../etc", wantErr: true},
		{name: "dotdot alone", projectFilter: "..", wantErr: true},
		{name: "absolute path", projectFilter: "/etc/passwd", wantErr: true},
		{name: "forward slash", projectFilter: "proj/evil", wantErr: true},
		{name: "backslash", projectFilter: `proj\evil`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeReset(t, "--project", tc.projectFilter)
			if !tc.wantErr && err == nil {
				return
			}
			if tc.wantErr && err == nil {
				t.Errorf("expected error for --project=%q, got nil", tc.projectFilter)
				return
			}
			if !errors.Is(err, project.ErrInvalidProjectName) {
				t.Errorf("expected ErrInvalidProjectName for --project=%q, got: %v", tc.projectFilter, err)
			}
		})
	}
}

func TestLeverageReset_BoundaryEnforcement(t *testing.T) {
	tmp := t.TempDir()
	snapshotDir := filepath.Join(tmp, ".campaign", "leverage", "snapshots")
	cacheDir := filepath.Join(tmp, ".campaign", "leverage", "cache")

	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	safeFilter := "myproject"
	if err := pathutil.ValidateBoundary(snapshotDir, filepath.Join(snapshotDir, safeFilter)); err != nil {
		t.Errorf("expected no boundary error for safe snapshot path: %v", err)
	}
	if err := pathutil.ValidateBoundary(cacheDir, filepath.Join(cacheDir, safeFilter+".json")); err != nil {
		t.Errorf("expected no boundary error for safe cache path: %v", err)
	}

	if err := pathutil.ValidateBoundary(snapshotDir, filepath.Join(snapshotDir, "..", "..", "escape")); err == nil {
		t.Error("expected boundary error for escaping snapshot target, got nil")
	}
	if err := pathutil.ValidateBoundary(cacheDir, filepath.Join(cacheDir, "..", "..", "escape.json")); err == nil {
		t.Error("expected boundary error for escaping cache target, got nil")
	}
}
