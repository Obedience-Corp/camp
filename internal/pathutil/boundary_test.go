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

func TestValidateBoundary_Symlinks(t *testing.T) {
	tmp := resolvePath(t, t.TempDir())
	outside := resolvePath(t, t.TempDir())

	realInside := filepath.Join(tmp, "projects", "real")
	if err := os.MkdirAll(realInside, 0o755); err != nil {
		t.Fatal(err)
	}

	// Symlink inside root pointing OUTSIDE root — must be rejected.
	escapeLink := filepath.Join(tmp, "escape-link")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Skipf("symlink creation failed (may lack permission): %v", err)
	}

	err := ValidateBoundary(tmp, escapeLink)
	if err == nil {
		t.Error("expected boundary violation for symlink pointing outside root, got nil")
	}
	if !errors.Is(err, ErrOutsideBoundary) {
		t.Errorf("expected ErrOutsideBoundary, got: %v", err)
	}

	// Symlink inside root pointing to another directory INSIDE root — must be allowed.
	internalLink := filepath.Join(tmp, "internal-link")
	if err := os.Symlink(realInside, internalLink); err != nil {
		t.Skipf("symlink creation failed: %v", err)
	}

	err = ValidateBoundary(tmp, internalLink)
	if err != nil {
		t.Errorf("expected no error for symlink pointing inside root, got: %v", err)
	}

	// Symlink simulating .claude pointing outside root.
	claudeLink := filepath.Join(tmp, ".claude")
	if err := os.Symlink(outside, claudeLink); err != nil {
		t.Skipf("symlink creation failed: %v", err)
	}

	err = ValidateBoundary(tmp, claudeLink)
	if err == nil {
		t.Error("expected boundary violation for .claude symlink pointing outside root, got nil")
	}
	if !errors.Is(err, ErrOutsideBoundary) {
		t.Errorf("expected ErrOutsideBoundary for .claude escape, got: %v", err)
	}
}

func TestValidateBoundary_macOS_VarSymlink(t *testing.T) {
	tmp := t.TempDir() // may be /var/... on macOS (unresolved)
	resolvedTmp := resolvePath(t, tmp)

	inside := filepath.Join(resolvedTmp, "subdir")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}

	// Using the raw (possibly unresolved) tmp as root should still work because
	// ValidateBoundary resolves root via EvalSymlinks internally.
	err := ValidateBoundary(tmp, inside)
	if err != nil {
		t.Errorf("macOS symlink test: expected no error, got: %v", err)
	}
}
