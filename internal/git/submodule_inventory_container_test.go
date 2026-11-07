//go:build container_fs

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventorySubmodulesOwnerRelative(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	child := filepath.Join(base, "child")
	leaf := filepath.Join(base, "leaf")
	for _, dir := range []string{root, child, leaf} {
		inventoryGit(t, base, "init", "-q", "-b", "main", dir)
		if err := os.WriteFile(filepath.Join(dir, "README"), []byte("fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		inventoryGit(t, dir, "add", "README")
		inventoryGit(t, dir, "commit", "-qm", "initial")
	}
	inventoryGit(t, child, "submodule", "add", "-q", "--name", "alias-leaf", leaf, "nested/leaf")
	inventoryGit(t, child, "commit", "-qam", "add nested leaf")
	inventoryGit(t, root, "submodule", "add", "-q", "--name", "alias-child", child, "projects/child")
	inventoryGit(t, root, "commit", "-qam", "add child")
	inventoryGit(t, root, "submodule", "update", "--init", "--recursive")

	childSHA := strings.TrimSpace(inventoryGit(t, child, "rev-parse", "HEAD"))
	inventoryGit(t, root, "config", "-f", ".gitmodules", "submodule.declaration.path", "projects/declaration")
	inventoryGit(t, root, "config", "-f", ".gitmodules", "submodule.declaration.url", child)
	inventoryGit(t, root, "config", "-f", ".gitmodules", "submodule.uninit.path", "projects/uninit")
	inventoryGit(t, root, "config", "-f", ".gitmodules", "submodule.uninit.url", child)
	inventoryGit(t, root, "config", "-f", ".gitmodules", "submodule.invalid.path", "../escape")
	inventoryGit(t, root, "config", "-f", ".gitmodules", "submodule.invalid.url", child)
	inventoryGit(t, root, "add", ".gitmodules")
	inventoryGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+childSHA+",projects/orphan")
	inventoryGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+childSHA+",projects/uninit")
	inventoryGit(t, root, "commit", "-qm", "add inventory edge cases")

	childCheckout := filepath.Join(root, "projects/child")
	remoteBase := filepath.Join(base, "remotes")
	inventoryGit(t, childCheckout, "remote", "set-url", "origin", filepath.Join(remoteBase, "child.git"))
	inventoryGit(t, childCheckout, "config", "-f", ".gitmodules", "submodule.alias-leaf.url", "../leaf.git")

	inventory := InventorySubmodules(context.Background(), root)
	byPath := make(map[string]SubmoduleInventoryEntry, len(inventory.Entries))
	for _, entry := range inventory.Entries {
		byPath[entry.Path] = entry
	}
	if len(byPath) != 6 {
		t.Fatalf("entries = %+v, want six distinct owner-relative paths", inventory.Entries)
	}
	check := func(path string, state InventoryState, owner, name string) {
		t.Helper()
		entry, ok := byPath[path]
		if !ok || entry.State != state || entry.Owner != owner || entry.Name != name {
			t.Fatalf("entry %s = %+v (present %v), want state %s owner %s name %s", path, entry, ok, state, owner, name)
		}
	}
	check("projects/child", InventoryMatched, ".", "alias-child")
	check("projects/child/nested/leaf", InventoryMatched, "projects/child", "alias-leaf")
	check("projects/declaration", InventoryDeclarationOnly, ".", "declaration")
	check("projects/orphan", InventoryGitlinkOnly, ".", "")
	check("projects/uninit", InventoryUninitialized, ".", "uninit")
	if entry := byPath["../escape"]; entry.State != InventoryInvalidPath || entry.Error == nil {
		t.Fatalf("invalid path = %+v, want classified error", entry)
	}
	if got := byPath["projects/child/nested/leaf"].ResolvedURL; got != filepath.Join(remoteBase, "leaf.git") {
		t.Fatalf("nested resolved URL = %q, want owner-relative %q", got, filepath.Join(remoteBase, "leaf.git"))
	}
	if len(inventory.Errors) != 1 {
		t.Fatalf("inventory errors = %v, want invalid path only", inventory.Errors)
	}
	ctx := context.Background()
	matched, err := SelectedCommitMatches(ctx, root, byPath["projects/child"])
	if err != nil || !matched {
		t.Fatalf("exact selected commit: matched=%v err=%v", matched, err)
	}
	matched, active, err := InventoryURLMatches(ctx, root, byPath["projects/child"])
	if err != nil || !matched || active != child {
		t.Fatalf("matching active URL: matched=%v active=%q err=%v", matched, active, err)
	}
	matched, active, err = InventoryURLMatches(ctx, root, byPath["projects/child/nested/leaf"])
	if err != nil || matched || active == "" {
		t.Fatalf("mismatched nested URL: matched=%v active=%q err=%v", matched, active, err)
	}
	inventoryGit(t, childCheckout, "commit", "--allow-empty", "-qm", "selected branch advance")
	advanced := byPath["projects/child"]
	advanced.ActualCommit = strings.TrimSpace(inventoryGit(t, childCheckout, "rev-parse", "HEAD"))
	matched, err = SelectedCommitMatches(ctx, root, advanced)
	if err != nil || !matched {
		t.Fatalf("descendant selected commit: matched=%v err=%v", matched, err)
	}
	tree := strings.TrimSpace(inventoryGit(t, childCheckout, "write-tree"))
	advanced.ActualCommit = strings.TrimSpace(inventoryGit(t, childCheckout, "commit-tree", tree, "-m", "unrelated checkout"))
	matched, err = SelectedCommitMatches(ctx, root, advanced)
	if err != nil || matched {
		t.Fatalf("divergent selected commit: matched=%v err=%v", matched, err)
	}
	advanced.RecordedCommit = strings.Repeat("0", 40)
	if matched, err = SelectedCommitMatches(ctx, root, advanced); err == nil || matched {
		t.Fatalf("unavailable recorded commit: matched=%v err=%v", matched, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if matched, err = SelectedCommitMatches(cancelled, root, byPath["projects/child"]); err == nil || matched {
		t.Fatalf("cancelled exact commit check: matched=%v err=%v", matched, err)
	}
}

func inventoryGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
	return string(output)
}

func TestResolveRelativeURLMatchesGitSubmoduleInit(t *testing.T) {
	base := t.TempDir()
	parentRemote := filepath.Join(base, "parent.git")
	childRemote := filepath.Join(parentRemote, "child")
	inventoryGit(t, base, "init", "-q", "--bare", "-b", "main", parentRemote)
	inventoryGit(t, base, "init", "-q", "-b", "main", childRemote)
	if err := os.WriteFile(filepath.Join(childRemote, "README"), []byte("child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inventoryGit(t, childRemote, "add", "README")
	inventoryGit(t, childRemote, "commit", "-qm", "child")
	source := filepath.Join(base, "source")
	inventoryGit(t, base, "clone", parentRemote, source)
	inventoryGit(t, source, "submodule", "add", childRemote, "projects/child")
	inventoryGit(t, source, "config", "-f", ".gitmodules", "submodule.projects/child.url", "./child")
	inventoryGit(t, source, "add", ".gitmodules", "projects/child")
	inventoryGit(t, source, "commit", "-qm", "add relative child")
	inventoryGit(t, source, "push", "origin", "main")
	checkout := filepath.Join(base, "checkout")
	inventoryGit(t, base, "clone", parentRemote, checkout)
	inventoryGit(t, checkout, "submodule", "init")
	active, err := GetActiveURL(context.Background(), checkout, "projects/child")
	if err != nil {
		t.Fatal(err)
	}
	want := resolveRelativeSubmoduleURL(parentRemote, "./child")
	if active != want {
		t.Fatalf("Git initialized URL %q; Camp resolved %q", active, want)
	}
	// An scp-style remote can be relative to the remote user's home.
	// Git does not insert a slash after the host when ../ reaches that root.
	inventoryGit(t, checkout, "config", "--unset", "submodule.projects/child.url")
	inventoryGit(t, checkout, "config", "-f", ".gitmodules", "submodule.projects/child.url", "../child.git")
	inventoryGit(t, checkout, "remote", "set-url", "origin", "git@example.com:parent.git")
	inventoryGit(t, checkout, "submodule", "init")
	active, err = GetActiveURL(context.Background(), checkout, "projects/child")
	if err != nil {
		t.Fatal(err)
	}
	want = resolveRelativeSubmoduleURL("git@example.com:parent.git", "../child.git")
	if active != want {
		t.Fatalf("Git initialized home-relative URL %q; Camp resolved %q", active, want)
	}
}
