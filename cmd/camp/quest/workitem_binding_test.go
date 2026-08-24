//go:build dev

package quest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/quest"
)

func setupEnrichmentCampaign(t *testing.T) (context.Context, *questCommandContext, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYAML := "id: camp1234\nname: test-campaign\ntype: product\n"
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := quest.EnsureScaffold(ctx, root); err != nil {
		t.Fatalf("EnsureScaffold: %v", err)
	}

	wiRel := filepath.ToSlash(filepath.Join("workflow", "design", "billing-redesign"))
	wiDir := filepath.Join(root, filepath.FromSlash(wiRel))
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Title-only marker, no README: discovery returns a title and empty summary
	// so a pre-set Purpose is a complete enrichment no-op.
	marker := "version: v1alpha8\nkind: workitem\nid: design-billing-redesign\ntype: design\ntitle: Billing Redesign\n"
	if err := os.WriteFile(filepath.Join(wiDir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}

	qctx := &questCommandContext{
		cfg:          &config.CampaignConfig{ID: "camp1234", Name: "test-campaign"},
		campaignRoot: root,
		service:      quest.NewService(root),
	}
	title, _ := resolveWorkitemEnrichment(ctx, root, wiRel)
	if title == "" {
		t.Fatal("fixture workitem was not discoverable; enrichment would skip and hide the regression")
	}
	return ctx, qctx, wiRel
}

func containsFile(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

// TestCreateLink_PreSetPurposeKeepsQuestFileStaged is the camp#583 auto-commit
// regression: enrichFromLinkedWorkitem used to replace the post-Link
// MutationResult with EnrichFromWorkitem's no-op (Files: nil) when Purpose was
// already set, so autoCommitQuest saw SelectiveOnly=false with empty Files.
func TestCreateLink_PreSetPurposeKeepsQuestFileStaged(t *testing.T) {
	t.Run("create via bindWorkitem", func(t *testing.T) {
		ctx, qctx, wiRel := setupEnrichmentCampaign(t)

		created, err := qctx.service.Create(ctx, "Launch", "User purpose", "", nil)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := bindWorkitem(ctx, qctx, created, wiRel)
		if err != nil {
			t.Fatalf("bindWorkitem() error = %v", err)
		}
		if got.Quest.Purpose != "User purpose" {
			t.Fatalf("Purpose = %q, want user-supplied value", got.Quest.Purpose)
		}
		if !containsFile(got.Files, created.Quest.Path) {
			t.Fatalf("Files = %#v, want quest path %q still staged after no-op enrichment", got.Files, created.Quest.Path)
		}
	})

	t.Run("link via enrichFromLinkedWorkitem", func(t *testing.T) {
		ctx, qctx, wiRel := setupEnrichmentCampaign(t)

		created, err := qctx.service.Create(ctx, "Launch", "User purpose", "", nil)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		linked, err := qctx.service.Link(ctx, created.Quest.ID, wiRel, "")
		if err != nil {
			t.Fatalf("Link() error = %v", err)
		}
		if !containsFile(linked.Files, created.Quest.Path) {
			t.Fatalf("pre-enrich Link Files = %#v, want %q", linked.Files, created.Quest.Path)
		}

		got, err := enrichFromLinkedWorkitem(ctx, qctx, linked, wiRel)
		if err != nil {
			t.Fatalf("enrichFromLinkedWorkitem() error = %v", err)
		}
		if got != linked {
			t.Fatal("no-op enrichment must return the prior MutationResult unchanged")
		}
		if !containsFile(got.Files, created.Quest.Path) {
			t.Fatalf("Files = %#v, want quest path %q still staged after no-op enrichment", got.Files, created.Quest.Path)
		}
	})
}

func TestEnrichFromLinkedWorkitem_MergesFilesWhenFieldsFilled(t *testing.T) {
	ctx, qctx, wiRel := setupEnrichmentCampaign(t)

	created, err := qctx.service.Create(ctx, "Empty", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	extra := filepath.Join(qctx.campaignRoot, "also-staged.txt")
	prior := &quest.MutationResult{
		Quest: created.Quest,
		Files: []string{created.Quest.Path, extra},
	}

	got, err := enrichFromLinkedWorkitem(ctx, qctx, prior, wiRel)
	if err != nil {
		t.Fatalf("enrichFromLinkedWorkitem() error = %v", err)
	}
	if got.Quest.Purpose != "Billing Redesign" {
		t.Fatalf("Purpose = %q, want workitem title", got.Quest.Purpose)
	}
	if !containsFile(got.Files, created.Quest.Path) {
		t.Fatalf("Files = %#v, want quest path %q", got.Files, created.Quest.Path)
	}
	if !containsFile(got.Files, extra) {
		t.Fatalf("Files = %#v, want union to keep prior extra path %q", got.Files, extra)
	}
}
