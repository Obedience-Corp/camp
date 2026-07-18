package selector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeCampaignWithDesignWorkitem lays down a minimal campaign containing one
// adopted design work item whose .workitem marker carries the given ref, then
// returns the campaign root.
func writeCampaignWithDesignWorkitem(t *testing.T, slug, id, ref string) string {
	t.Helper()
	root := t.TempDir()

	campaignYAML := "version: campaign/v1\nid: testcampaign\nname: test\ntype: product\n"
	mustWrite(t, filepath.Join(root, ".campaign", "campaign.yaml"), campaignYAML)

	marker := "version: v1alpha6\nkind: workitem\nid: " + id +
		"\ntype: design\ntitle: " + slug + "\nref: " + ref + "\n"
	mustWrite(t, filepath.Join(root, "workflow", "design", slug, ".workitem"), marker)
	mustWrite(t, filepath.Join(root, "workflow", "design", slug, "README.md"), "# "+slug+"\n")
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_MatchesEverySelectorFormForAdoptedWorkitem(t *testing.T) {
	const (
		slug = "camp-sync-clone-transport"
		id   = "design-camp-sync-clone-transport-2026-07-17"
		ref  = "WI-541bcc"
	)
	root := writeCampaignWithDesignWorkitem(t, slug, id, ref)

	tests := []struct {
		name  string
		query string
	}{
		{"ref lowercase", ref},
		{"ref uppercase", "WI-541BCC"},
		{"stable id", id},
		{"key", "design:workflow/design/" + slug},
		{"relative path", "workflow/design/" + slug},
		{"slug", slug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wi, err := Resolve(context.Background(), root, tt.query, ResolveOptions{})
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.query, err)
			}
			if wi.StableID != id {
				t.Fatalf("Resolve(%q) matched %q, want stable id %q", tt.query, wi.StableID, id)
			}
		})
	}
}

func TestResolve_UnknownRefIsNotFound(t *testing.T) {
	root := writeCampaignWithDesignWorkitem(t, "thing", "design-thing-2026-07-17", "WI-541bcc")

	_, err := Resolve(context.Background(), root, "WI-000000", ResolveOptions{})
	if !errors.Is(err, ErrSelectorNotFound) {
		t.Fatalf("Resolve(unknown ref) error = %v, want ErrSelectorNotFound", err)
	}
}

func TestResolve_AmbiguousRefReportsBothKeys(t *testing.T) {
	// Two adopted design items hand-edited to share a ref: the ref tier must
	// surface an ambiguity error rather than silently pick one.
	root := writeCampaignWithDesignWorkitem(t, "one", "design-one-2026-07-17", "WI-541bcc")
	marker := "version: v1alpha6\nkind: workitem\nid: design-two-2026-07-17\ntype: design\ntitle: two\nref: WI-541bcc\n"
	mustWrite(t, filepath.Join(root, "workflow", "design", "two", ".workitem"), marker)
	mustWrite(t, filepath.Join(root, "workflow", "design", "two", "README.md"), "# two\n")

	_, err := Resolve(context.Background(), root, "WI-541bcc", ResolveOptions{})
	if !errors.Is(err, ErrSelectorAmbiguous) {
		t.Fatalf("Resolve(shared ref) error = %v, want ErrSelectorAmbiguous", err)
	}
}
