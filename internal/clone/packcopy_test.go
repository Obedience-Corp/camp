package clone

import (
	"strings"
	"testing"
)

func TestPackCopyEligible(t *testing.T) {
	const root = "/home/peer/campaigns/demo"
	goodDir := root + "/.git"

	tests := []struct {
		name string
		v    RepoVerdict
		root string
		want bool
		// wantReason is a substring the refusal must explain itself with.
		wantReason string
	}{
		{
			name: "non-quiescent repo carries its reasons into the refusal",
			v: RepoVerdict{
				Repo:    "projects/camp",
				Reasons: []string{"uncommitted changes in the working tree"},
				HeadSHA: "abc", GitDir: goodDir,
			},
			root:       root,
			want:       false,
			wantReason: "not quiescent: uncommitted changes in the working tree",
		},
		{
			name:       "quiescent but no HEAD to verify against",
			v:          RepoVerdict{Repo: ".", Quiescent: true, GitDir: goodDir},
			root:       root,
			want:       false,
			wantReason: "no HEAD",
		},
		{
			name:       "quiescent but no git dir to copy from",
			v:          RepoVerdict{Repo: ".", Quiescent: true, HeadSHA: "abc"},
			root:       root,
			want:       false,
			wantReason: "no git directory",
		},
		{
			name: "git dir outside the campaign root is refused, not trusted",
			v: RepoVerdict{
				Repo: ".", Quiescent: true, HeadSHA: "abc",
				GitDir: "/etc/shadow",
			},
			root:       root,
			want:       false,
			wantReason: "outside the campaign root",
		},
		{
			name: "git dir escaping via traversal is refused",
			v: RepoVerdict{
				Repo: ".", Quiescent: true, HeadSHA: "abc",
				GitDir: root + "/../../etc",
			},
			root:       root,
			want:       false,
			wantReason: "outside the campaign root",
		},
		{
			name: "relative git dir is refused",
			v: RepoVerdict{
				Repo: ".", Quiescent: true, HeadSHA: "abc", GitDir: ".git",
			},
			root:       root,
			want:       false,
			wantReason: "outside the campaign root",
		},
		{
			name: "quiescent root repo",
			v: RepoVerdict{
				Repo: ".", Quiescent: true, HeadSHA: "abc", GitDir: goodDir,
			},
			root: root,
			want: true,
		},
		{
			name: "quiescent submodule with a module git dir",
			v: RepoVerdict{
				Repo: "projects/camp", Quiescent: true, HeadSHA: "abc",
				GitDir: root + "/.git/modules/camp",
			},
			root: root,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := packCopyEligible(tt.v, tt.root)
			if ok != tt.want {
				t.Fatalf("packCopyEligible() = %v (%q), want %v", ok, reason, tt.want)
			}
			if tt.want {
				if reason != "" {
					t.Errorf("packCopyEligible() eligible but gave reason %q", reason)
				}
				return
			}
			if !strings.Contains(reason, tt.wantReason) {
				t.Errorf("packCopyEligible() reason = %q, want it to mention %q", reason, tt.wantReason)
			}
		})
	}
}

func TestWithinRoot(t *testing.T) {
	const root = "/campaigns/demo"
	tests := []struct {
		name string
		abs  string
		want bool
	}{
		{name: "relative path is never contained", abs: "demo/.git", want: false},
		{name: "sibling with a shared prefix", abs: "/campaigns/demo-evil/.git", want: false},
		{name: "parent", abs: "/campaigns", want: false},
		{name: "traversal out", abs: "/campaigns/demo/../other", want: false},
		{name: "unrelated absolute", abs: "/etc", want: false},
		{name: "the root itself", abs: root, want: true},
		{name: "direct child", abs: root + "/.git", want: true},
		{name: "nested module dir", abs: root + "/.git/modules/projects/camp", want: true},
		{name: "traversal that lands back inside", abs: root + "/a/../.git", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withinRoot(root, tt.abs); got != tt.want {
				t.Errorf("withinRoot(%q, %q) = %v, want %v", root, tt.abs, got, tt.want)
			}
		})
	}
}

func TestLastLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "only whitespace", in: "\n  \n", want: ""},
		{name: "single line", in: "boom", want: "boom"},
		{name: "trailing newline ignored", in: "a\nboom\n", want: "boom"},
		{name: "trailing blank lines ignored", in: "a\nboom\n\n  \n", want: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastLine(tt.in); got != tt.want {
				t.Errorf("lastLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestShortSHA(t *testing.T) {
	const full = "0123456789abcdef0123456789abcdef01234567"
	if got := shortSHA(full); got != "0123456789ab" {
		t.Errorf("shortSHA() = %q, want %q", got, "0123456789ab")
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Errorf("shortSHA() on a short value = %q, want it returned unchanged", got)
	}
}

// TestPackCopyScopeIsImmutableOnly guards the class A invariant at the level it
// is actually decided: the list of things the copy is willing to touch. A live
// index, config, hooks, or worktree state appearing here would be the bug.
func TestPackCopyScopeIsImmutableOnly(t *testing.T) {
	allowed := map[string]bool{
		"objects": true, "refs": true, "packed-refs": true, "HEAD": true,
	}
	for _, name := range append(append([]string{}, packCopySubdirs...), packCopyFiles...) {
		if !allowed[name] {
			t.Errorf("pack copy would transfer %q, which is not immutable object or ref content", name)
		}
	}
	for _, forbidden := range []string{"index", "config", "hooks", "logs", "worktrees", "shallow"} {
		for _, name := range append(append([]string{}, packCopySubdirs...), packCopyFiles...) {
			if name == forbidden {
				t.Errorf("pack copy must never transfer %q", forbidden)
			}
		}
	}
}
