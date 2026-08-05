package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

const (
	intentTestID    = "intents-cannot-be-primary-linked-20260803-184438"
	intentTestStage = "inbox"
)

// intentTestCampaign extends linkTestCampaign with one intent whose frontmatter
// carries intentTestID, plus a worktree directory to link it to.
func intentTestCampaign(t *testing.T) (root, relPath string) {
	t.Helper()
	root = linkTestCampaign(t)

	relPath = ".campaign/intents/" + intentTestStage + "/" + intentTestID + ".md"
	body := "---\nid: " + intentTestID +
		"\ntitle: Intents cannot be primary-linked to a worktree\nstatus: " + intentTestStage +
		"\ncreated_at: 2026-08-03T18:44:38Z\ntype: bug\n---\n\n# Intents cannot be primary-linked\n"
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relPath)), body)

	if err := os.MkdirAll(filepath.Join(root, "projects", "worktrees", "camp", "intent-links"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, relPath
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLink_IntentRejectsUnknownSelector keeps the error case first: a selector
// that names no intent must fail before any link row is written.
func TestLink_IntentRejectsUnknownSelector(t *testing.T) {
	root, _ := intentTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "no-such-intent-20260101-000000",
		Worktree: "camp/intent-links",
	})
	if err == nil {
		t.Fatal("runLink with an unknown intent selector must fail")
	}
	registry, lerr := links.Load(context.Background(), root)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(registry.Links) != 0 {
		t.Fatalf("failed link must not write a row, got %d", len(registry.Links))
	}
}

// TestLink_UnadoptedDocTypeAsksForAdoptionInsteadOfLeakingTheValidator covers
// the neighbouring path-keyed case: a design/explore directory with no
// .workitem marker has no single-segment id anywhere on disk, so linking it
// must point at `camp workitem adopt` rather than reporting a workitem_id the
// user never chose.
func TestLink_UnadoptedDocTypeAsksForAdoptionInsteadOfLeakingTheValidator(t *testing.T) {
	root, _ := intentTestCampaign(t)
	unadopted := filepath.Join(root, "workflow", "explore", "unadopted-topic")
	if err := os.MkdirAll(unadopted, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(unadopted, "README.md"), "# Unadopted\n")
	restore := chdir(t, root)
	defer restore()

	err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "unadopted-topic",
		Worktree: "camp/intent-links",
	})
	if err == nil {
		t.Fatal("linking an unadopted explore directory must fail")
	}
	if !strings.Contains(err.Error(), "camp workitem adopt") {
		t.Fatalf("error should name the adopt command, got: %v", err)
	}
	if strings.Contains(err.Error(), "must not contain") {
		t.Fatalf("error should not leak the path-segment validator, got: %v", err)
	}
}

// TestLink_IntentPrimaryLinksByEverySelectorForm is the regression for the
// intent-link defect: every form that names the intent must produce a link whose
// workitem_id is the bare frontmatter id and whose workitem_key is the key form.
func TestLink_IntentPrimaryLinksByEverySelectorForm(t *testing.T) {
	_, relPath := intentTestCampaign(t)
	wantKey := "intent:" + relPath

	tests := []struct {
		name     string
		selector string
	}{
		{"frontmatter id", intentTestID},
		{"key form", wantKey},
		{"relative path", relPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _ := intentTestCampaign(t)
			restore := chdir(t, root)
			defer restore()

			if err := runLink(context.Background(), newCmd(), linkOptions{
				Selector: tt.selector,
				Worktree: "camp/intent-links",
			}); err != nil {
				t.Fatalf("runLink(%q): %v", tt.selector, err)
			}

			registry, err := links.Load(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.Links) != 1 {
				t.Fatalf("expected 1 link, got %d", len(registry.Links))
			}
			link := registry.Links[0]
			if link.WorkitemID != intentTestID {
				t.Errorf("workitem_id = %q, want the bare frontmatter id %q", link.WorkitemID, intentTestID)
			}
			if link.WorkitemKey != wantKey {
				t.Errorf("workitem_key = %q, want %q", link.WorkitemKey, wantKey)
			}
			if link.Scope.Kind != links.ScopeWorktree ||
				link.Scope.Path != "projects/worktrees/camp/intent-links" {
				t.Errorf("scope = %#v", link.Scope)
			}
			if link.Role != links.RolePrimary {
				t.Errorf("role = %s, want primary", link.Role)
			}
		})
	}
}

// TestLinks_IntentLinkIsListedBySelector covers the read-back half: once an
// intent is linked, `camp workitem links <intent>` must find its own row.
func TestLinks_IntentLinkIsListedBySelector(t *testing.T) {
	root, _ := intentTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	ctx := context.Background()
	if err := runLink(ctx, newCmd(), linkOptions{
		Selector: intentTestID,
		Worktree: "camp/intent-links",
	}); err != nil {
		t.Fatalf("runLink: %v", err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runLinks(ctx, cmd, intentTestID, false); err != nil {
		t.Fatalf("runLinks: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, intentTestID) {
		t.Fatalf("camp workitem links %s did not list the intent's own link:\n%s", intentTestID, got)
	}
}

// TestUnlink_IntentRemovesItsOwnLink covers the teardown half: an intent must be
// able to drop the link it was just able to create.
func TestUnlink_IntentRemovesItsOwnLink(t *testing.T) {
	root, _ := intentTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	ctx := context.Background()
	if err := runLink(ctx, newCmd(), linkOptions{
		Selector: intentTestID,
		Worktree: "camp/intent-links",
	}); err != nil {
		t.Fatalf("runLink: %v", err)
	}
	if err := runUnlink(ctx, newCmd(), unlinkOptions{
		Selector: intentTestID,
		Worktree: "camp/intent-links",
	}); err != nil {
		t.Fatalf("runUnlink: %v", err)
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 0 {
		t.Fatalf("expected the intent link to be removed, got %d rows", len(registry.Links))
	}
}
