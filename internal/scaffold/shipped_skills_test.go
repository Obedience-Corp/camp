package scaffold

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

func renderShippedForTest(t *testing.T, vars map[string]any) map[string][]byte {
	t.Helper()
	files, err := intskills.RenderShipped(t.Context(), embeddedScaffoldFS(), campaignRecipePath, vars)
	if err != nil {
		t.Fatalf("RenderShipped() error = %v", err)
	}
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		out[f.RelPath()] = f.Content
	}
	return out
}

func TestRenderShipped_CoversEverySkillTemplate(t *testing.T) {
	var templates []string
	err := fs.WalkDir(CampaignScaffoldFS, "campaign/templates/.campaign/skills", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			templates = append(templates, strings.TrimSuffix(strings.TrimPrefix(p, "campaign/templates/"), ".tmpl"))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderShippedForTest(t, map[string]any{"Profile": "stable"})
	if len(rendered) != len(templates) || len(templates) == 0 {
		t.Fatalf("rendered %d shipped files for %d skill templates", len(rendered), len(templates))
	}
	for _, rel := range templates {
		content, ok := rendered[rel]
		if !ok {
			t.Errorf("skill template %s was not rendered", rel)
			continue
		}
		if bytes.Contains(content, []byte("<no value>")) {
			t.Errorf("%s rendered a missing template var", rel)
		}
	}
}

func TestRenderShipped_ProfileChangesOnlyProfileGatedBundles(t *testing.T) {
	stable := renderShippedForTest(t, map[string]any{"Profile": "stable"})
	dev := renderShippedForTest(t, map[string]any{"Profile": "dev"})

	const gated = ".campaign/skills/campaign-workflows/SKILL.md"
	if bytes.Equal(stable[gated], dev[gated]) {
		t.Fatalf("%s rendered identically for dev and stable; its hash must follow the profile", gated)
	}
	for rel, content := range stable {
		if rel != gated && !bytes.Equal(content, dev[rel]) {
			t.Errorf("%s differs between profiles but has no profile gate", rel)
		}
	}
}

func TestRenderShipped_IsDeterministicAcrossRenderTimes(t *testing.T) {
	first := renderShippedForTest(t, map[string]any{"Profile": "stable", "now": "2001-01-01T00:00:00Z"})
	second := renderShippedForTest(t, map[string]any{"Profile": "stable", "now": "2030-06-15T12:00:00Z"})
	for rel, content := range first {
		if !bytes.Equal(content, second[rel]) {
			t.Errorf("%s depends on the render time, so its shipped hash could never match", rel)
		}
	}
}

func TestRepairPlan_HasSkillNoticesOnlyForSkillPreserves(t *testing.T) {
	plan := &RepairPlan{Changes: []RepairChange{
		{Type: RepairPreserve, Category: "shortcut", Key: "x"},
	}}
	if plan.HasSkillNotices() {
		t.Fatal("a shortcut preserve is not a skill notice")
	}
	plan.Changes = append(plan.Changes, RepairChange{Type: RepairPreserve, Category: RepairSkillCategory, Key: ".campaign/skills/a/SKILL.md"})
	if !plan.HasSkillNotices() {
		t.Fatal("a preserved skill file must count as a skill notice")
	}
	if plan.HasChanges() {
		t.Fatal("preserved files alone are not changes to apply")
	}
}
