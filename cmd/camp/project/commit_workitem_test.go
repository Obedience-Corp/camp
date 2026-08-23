package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"gopkg.in/yaml.v3"
)

// writeFestivalCampaign creates a campaign root with a festival directory
// containing a fest.yaml with the given festival ID, plus a links.yaml entry
// that scopes a primary link to that festival.
func writeFestivalCampaign(t *testing.T, festID string) (root, festDir string) {
	t.Helper()
	root = t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	festDirName := "test-fest-" + festID
	festDir = filepath.Join(root, "festivals", "active", festDirName)
	if err := os.MkdirAll(festDir, 0o755); err != nil {
		t.Fatal(err)
	}
	festYAML := "version: fest/v1\nmetadata:\n  id: " + festID + "\n  name: " + festDirName + "\n  festival_type: standard\n"
	if err := os.WriteFile(filepath.Join(festDir, "fest.yaml"), []byte(festYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(festDir, "FESTIVAL_GOAL.md"), []byte("# goal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a links.yaml with a ScopeFestival primary link pointing to the
	// festival's workitem id. This is the link the resolver's SourceFestival
	// tier matches when FestivalID is passed.
	linksDir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(linksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := links.Empty()
	reg.Links = append(reg.Links, links.Link{
		ID:         "lnk_20260101_000001",
		WorkitemID: festID,
		Scope:      links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/" + festDirName},
		Role:       links.RolePrimary,
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:  "test",
	})
	data, err := yaml.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linksDir, "links.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	return root, festDir
}

func writeFestivalLinkedProjectCampaign(t *testing.T, festID string) (root, projectDir string) {
	t.Helper()
	root = t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir = filepath.Join(root, "projects", "alpha")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	designDir := filepath.Join(root, "workflow", "design", "linked")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "version: v1alpha8\nkind: workitem\nid: design-linked\ntype: design\ntitle: Linked\nref: WI-aabbcc\n"
	if err := os.WriteFile(filepath.Join(designDir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "README.md"), []byte("# Linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	festDirName := "linked-" + festID
	festDir := filepath.Join(root, "festivals", "active", festDirName)
	if err := os.MkdirAll(festDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(festDir, "fest.yaml"),
		[]byte("version: fest/v1\nmetadata:\n  id: "+festID+"\n  name: Linked\n  festival_type: standard\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := links.Empty()
	reg.Links = append(reg.Links,
		links.Link{
			ID: "lnk_20260101_000001", WorkitemID: "design-linked",
			Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/alpha"},
			Role:  links.RolePrimary, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), CreatedBy: "test",
		},
		links.Link{
			ID: "lnk_20260101_000002", WorkitemID: "design-linked",
			Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/" + festDirName},
			Role:  links.RolePrimary, CreatedAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), CreatedBy: "test",
		},
	)
	data, err := yaml.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "workitems", "links.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, projectDir
}

// TestResolveProjectCommitContext_InfersFestivalIDFromCwd verifies the core
// fix for camp#306: camp p commit's context resolver infers the festival ID
// from cwd (when cwd is inside a festival directory) and passes it to the
// resolver so the SourceFestival tier fires. Without the fix, FestivalID was
// always empty and the festival tier never ran from the project-commit path.
func TestResolveProjectCommitContext_InfersFestivalIDFromCwd(t *testing.T) {
	const festID = "CF9999"
	root, festDir := writeFestivalCampaign(t, festID)

	questID, festivalRef, workitemRef := resolveProjectCommitContext(
		context.Background(), root, festDir, "")

	if festivalRef != festID {
		t.Errorf("festivalRef = %q, want %q (the festival id inferred from cwd)", festivalRef, festID)
	}
	if workitemRef != "" {
		t.Errorf("workitemRef = %q, want empty (festival has no WI- ref)", workitemRef)
	}
	if questID != "" {
		t.Errorf("questID = %q, want empty", questID)
	}
}

func TestResolveProjectCommitContext_ProjectLinkCarriesFestivalContext(t *testing.T) {
	const festID = "CF9999"
	root, projectDir := writeFestivalLinkedProjectCampaign(t, festID)

	questID, festivalRef, workitemRef := resolveProjectCommitContext(
		context.Background(), root, projectDir, "")

	if festivalRef != festID {
		t.Errorf("festivalRef = %q, want %q from the resolved workitem's festival link", festivalRef, festID)
	}
	if workitemRef != "WI-aabbcc" {
		t.Errorf("workitemRef = %q, want WI-aabbcc", workitemRef)
	}
	if questID != "" {
		t.Errorf("questID = %q, want empty", questID)
	}
}

// TestResolveProjectCommitContext_NoFestivalContextIsEmpty verifies graceful
// degradation: when cwd is not inside a festival and no workitem resolves, the
// returned values are all empty so the caller produces a bare campaign tag.
func TestResolveProjectCommitContext_NoFestivalContextIsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	questID, festivalRef, workitemRef := resolveProjectCommitContext(
		context.Background(), root, root, "")

	if questID != "" || festivalRef != "" || workitemRef != "" {
		t.Errorf("expected all-empty when no context resolves, got questID=%q festivalRef=%q workitemRef=%q",
			questID, festivalRef, workitemRef)
	}
}
