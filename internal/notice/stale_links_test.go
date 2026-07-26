package notice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLinks(t *testing.T, root string, kind string, scopePaths ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("version: workitem-links/v1alpha1\nlinks:\n")
	for i, p := range scopePaths {
		b.WriteString("  - id: lnk_20260726_00000" + string(rune('a'+i)) + "\n")
		b.WriteString("    workitem_id: design-demo-2026-07-26\n")
		b.WriteString("    scope:\n      kind: " + kind + "\n      path: " + p + "\n")
		b.WriteString("    role: primary\n")
		b.WriteString("    created_at: 2026-07-26T00:00:00Z\n")
		b.WriteString("    created_by: test\n")
	}
	path := filepath.Join(root, ".campaign", "workitems", "links.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStaleLinks_QuietWhenEveryScopeExists(t *testing.T) {
	root := t.TempDir()
	live := "workflow/design/live"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(live)), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLinks(t, root, "campaign_path", live)

	got, err := StaleLinks(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected no notice, got %+v", got)
	}
}

func TestStaleLinks_QuietWithoutARegistry(t *testing.T) {
	got, err := StaleLinks(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected no notice for a campaign with no links.yaml, got %+v", got)
	}
}

func TestStaleLinks_CountsAndPluralizes(t *testing.T) {
	cases := []struct {
		name string
		gone []string
		want string
	}{
		{name: "one", gone: []string{"workflow/design/a"}, want: "1 workitem link points"},
		{name: "several", gone: []string{"workflow/design/a", "workflow/design/b"}, want: "2 workitem links point"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLinks(t, root, "campaign_path", tc.gone...)

			got, err := StaleLinks(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("expected a notice")
			}
			if !strings.Contains(got.Message, tc.want) {
				t.Fatalf("message %q missing %q", got.Message, tc.want)
			}
			if got.Command != "camp workitem doctor --fix" {
				t.Fatalf("Command = %q", got.Command)
			}
			if got.ID != StaleLinksID {
				t.Fatalf("ID = %q, want %q", got.ID, StaleLinksID)
			}
		})
	}
}

// Worktrees are gitignored as machine-local, so on a campaign synced across two
// machines every worktree the other machine owns is absent here. Counting those
// would make the notice permanent and unactionable.
func TestStaleLinks_IgnoresMachineLocalScopes(t *testing.T) {
	root := t.TempDir()
	writeLinks(t, root, "worktree", "projects/worktrees/fest/on-my-laptop")

	got, err := StaleLinks(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("a missing worktree must not raise a notice, got %+v", got)
	}
}
