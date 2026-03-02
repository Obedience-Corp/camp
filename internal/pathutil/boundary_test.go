package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func resolvePath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func TestValidateBoundary(t *testing.T) {
	tmp := resolvePath(t, t.TempDir())

	existingDir := filepath.Join(tmp, "projects", "myproj")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		root        string
		target      string
		wantErr     bool
		wantOOB     bool
		wantBadRoot bool
	}{
		{
			name:   "path inside root (existing)",
			root:   tmp,
			target: existingDir,
		},
		{
			name:   "path at root",
			root:   tmp,
			target: tmp,
		},
		{
			name:    "traversal via ../",
			root:    tmp,
			target:  filepath.Join(tmp, "projects", "..", "..", "etc"),
			wantErr: true,
			wantOOB: true,
		},
		{
			name:    "absolute path outside root",
			root:    tmp,
			target:  "/etc/passwd",
			wantErr: true,
			wantOOB: true,
		},
		{
			name:   "non-existent target inside root",
			root:   tmp,
			target: filepath.Join(tmp, "projects", "new-proj"),
		},
		{
			name:    "non-existent target outside root",
			root:    tmp,
			target:  filepath.Join(tmp, "..", "outside"),
			wantErr: true,
			wantOOB: true,
		},
		{
			name:        "empty root",
			root:        "",
			target:      existingDir,
			wantErr:     true,
			wantBadRoot: true,
		},
		{
			name:    "empty target resolves to cwd (outside root)",
			root:    tmp,
			target:  "",
			wantErr: true,
			wantOOB: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBoundary(tc.root, tc.target)

			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tc.wantOOB && !errors.Is(err, ErrOutsideBoundary) {
				t.Errorf("expected ErrOutsideBoundary, got: %v", err)
			}
			if tc.wantBadRoot && !errors.Is(err, ErrBoundaryRootInvalid) {
				t.Errorf("expected ErrBoundaryRootInvalid, got: %v", err)
			}
		})
	}
}

func TestBoundaryError_Unwrap(t *testing.T) {
	err := &BoundaryError{Root: "/root", Target: "/outside", Cause: ErrOutsideBoundary}
	if !errors.Is(err, ErrOutsideBoundary) {
		t.Errorf("errors.Is did not find ErrOutsideBoundary through BoundaryError")
	}
}
