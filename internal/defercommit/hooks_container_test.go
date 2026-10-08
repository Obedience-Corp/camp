//go:build container_fs

package defercommit

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

// Bookkeeping jobs execute real hooks without a captured direction context,
// so all executable hooks keep them foreground. Captured-tree jobs can still
// reproduce the supported shim.
func TestAllowedForPathsKeepsHooksForeground(t *testing.T) {
	t.Setenv(EnvNoDefer, "")
	paths := []string{"note.md"}

	const stock = "#!/bin/sh\n" +
		"# direction-hook v1 — appends Festival-Direction trailers; see docs/anchoring.md\n" +
		"here=\"$(dirname \"$0\")\"\n" +
		"if [ -x \"$here/commit-msg.before-direction\" ]; then \"$here/commit-msg.before-direction\" \"$@\" || exit $?; fi\n" +
		"exec fest-direction hook commit-msg \"$1\"\n"
	const older = "#!/bin/sh\n# direction-hook v1\nexec direction hook commit-msg \"$@\"\n"

	tests := []struct {
		name  string
		setup func(t *testing.T, repo string)
		want  Refusal
		shim  bool
	}{
		{
			name: "only the direction shim",
			want: RefusedHooks,
			shim: true,
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", stock, 0o755)
			},
		},
		{
			name: "modified shim validates before exec",
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", strings.Replace(stock, "exec fest-direction", "./validate-message \"$1\" || exit 1\nexec fest-direction", 1), 0o755)
			},
			want: RefusedHooks,
		},
		{
			name: "older direction shim",
			want: RefusedHooks,
			shim: true,
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", older, 0o755)
			},
		},
		{
			name: "shim plus another executable hook",
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", stock, 0o755)
				writeHook(t, repo, "pre-commit", "#!/bin/sh\nexit 0\n", 0o755)
			},
			want: RefusedHooks,
		},
		{
			name: "shim plus the hook it would chain",
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", stock, 0o755)
				writeHook(t, repo, "commit-msg.before-direction", "#!/bin/sh\nexit 0\n", 0o755)
			},
			want: RefusedHooks,
		},
		{
			name:  "no hooks",
			setup: func(t *testing.T, repo string) {},
		},
		{
			name: "a commit-msg hook that is not the shim",
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", "#!/bin/sh\nexit 0\n", 0o755)
			},
			want: RefusedHooks,
		},
		{
			name: "the marker without the direction exec is still a user's hook",
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", "#!/bin/sh\n# direction-hook v1\nexit 0\n", 0o755)
			},
			want: RefusedHooks,
		},
		{
			name: "a non-executable shim is not a hook git would run",
			setup: func(t *testing.T, repo string) {
				writeHook(t, repo, "commit-msg", stock, 0o644)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initGitRepo(t)
			tt.setup(t, repo)
			if tt.shim && git.HasCommitHooks(context.Background(), repo) {
				t.Fatal("supported shim must still allow captured-tree deferral")
			}
			allowed, why := AllowedForPaths(context.Background(), repo, repo, paths, nil)
			if tt.want == "" {
				if !allowed || why != "" {
					t.Fatalf("AllowedForPaths() = %v, %q; want allowed", allowed, why)
				}
				return
			}
			if allowed || why != tt.want {
				t.Fatalf("AllowedForPaths() = %v, %q; want refusal %q", allowed, why, tt.want)
			}
		})
	}
}

// rev-parse failing is the fail-safe: camp cannot tell, so it assumes a hook
// is there and does not defer.
func TestAllowedForPathsUnreadableHooksDirectoryRefuses(t *testing.T) {
	t.Setenv(EnvNoDefer, "")
	repo := t.TempDir()
	allowed, why := AllowedForPaths(context.Background(), repo, repo, []string{"note.md"}, nil)
	if allowed || why != RefusedHooks {
		t.Fatalf("AllowedForPaths() = %v, %q; an unreadable hooks directory must refuse", allowed, why)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if _, err := git.Output(context.Background(), repo, "init", "-q", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	return repo
}

func writeHook(t *testing.T, repo, name, body string, mode fs.FileMode) {
	t.Helper()
	path := filepath.Join(repo, ".git", "hooks", name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write hook %s: %v", name, err)
	}
}
