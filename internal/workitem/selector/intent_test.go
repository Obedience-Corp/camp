package selector

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

const (
	testIntentID    = "fest-phase-gate-templates-give-20260727-123326"
	testIntentStage = "active"
)

// writeCampaignWithIntent lays down a minimal campaign containing one intent in
// the given stage whose frontmatter carries id, and returns the campaign root.
func writeCampaignWithIntent(t *testing.T, stage, id string) string {
	t.Helper()
	root := t.TempDir()

	campaignYAML := "version: campaign/v1\nid: testcampaign\nname: test\ntype: product\n"
	mustWrite(t, filepath.Join(root, ".campaign", "campaign.yaml"), campaignYAML)

	body := "---\nid: " + id + "\ntitle: Fest phase gate templates\nstatus: " + stage +
		"\ncreated_at: 2026-07-27T12:33:26Z\ntype: bug\n---\n\n# Fest phase gate templates\n"
	mustWrite(t, filepath.Join(root, ".campaign", "intents", stage, id+".md"), body)
	return root
}

func TestResolve_IntentMatchesEverySelectorForm(t *testing.T) {
	root := writeCampaignWithIntent(t, testIntentStage, testIntentID)
	relPath := ".campaign/intents/" + testIntentStage + "/" + testIntentID + ".md"

	tests := []struct {
		name  string
		query string
	}{
		{"frontmatter id", testIntentID},
		{"frontmatter id uppercase", "FEST-PHASE-GATE-TEMPLATES-GIVE-20260727-123326"},
		{"key", "intent:" + relPath},
		{"relative path", relPath},
		{"filename slug", testIntentID + ".md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wi, err := Resolve(context.Background(), root, tt.query, ResolveOptions{})
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.query, err)
			}
			if wi.SourceID != testIntentID {
				t.Fatalf("Resolve(%q) matched source_id %q, want %q", tt.query, wi.SourceID, testIntentID)
			}
		})
	}
}

func TestResolve_UnknownIntentIDIsNotFound(t *testing.T) {
	root := writeCampaignWithIntent(t, testIntentStage, testIntentID)

	_, err := Resolve(context.Background(), root, "no-such-intent-20260101-000000", ResolveOptions{})
	if !errors.Is(err, ErrSelectorNotFound) {
		t.Fatalf("Resolve(unknown intent id) error = %v, want ErrSelectorNotFound", err)
	}
}

// TestResolve_AmbiguousIntentIDReportsBothKeys guards the tier semantics: two
// intents hand-edited to share an id must report ambiguity rather than silently
// picking whichever the directory walk saw first.
func TestResolve_AmbiguousIntentIDReportsBothKeys(t *testing.T) {
	root := writeCampaignWithIntent(t, testIntentStage, testIntentID)
	body := "---\nid: " + testIntentID + "\ntitle: Duplicate\nstatus: inbox\n" +
		"created_at: 2026-07-27T12:33:26Z\ntype: bug\n---\n\n# Duplicate\n"
	mustWrite(t, filepath.Join(root, ".campaign", "intents", "inbox", "duplicate.md"), body)

	_, err := Resolve(context.Background(), root, testIntentID, ResolveOptions{})
	if !errors.Is(err, ErrSelectorAmbiguous) {
		t.Fatalf("Resolve(duplicate intent id) error = %v, want ErrSelectorAmbiguous", err)
	}
}
