package initcmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/scaffold"
)

func TestReportNothingToApply_UpToDateWithoutNotices(t *testing.T) {
	var out bytes.Buffer
	reportNothingToApply(&scaffold.RepairPlan{Changes: []scaffold.RepairChange{
		{Type: scaffold.RepairPreserve, Category: "shortcut", Key: "pp", Description: "my shortcut"},
	}}, Writers{HumanOut: &out})
	if !strings.Contains(out.String(), "nothing to repair") {
		t.Fatalf("a plan without skill notices must report nothing to repair:\n%s", out.String())
	}
}

func TestReportNothingToApply_PrintsPreservedSkillNotices(t *testing.T) {
	const remedy = "modified locally, preserved; run camp skills update --force fest-execution to replace"
	var out bytes.Buffer
	reportNothingToApply(&scaffold.RepairPlan{Changes: []scaffold.RepairChange{
		{Type: scaffold.RepairPreserve, Category: "shortcut", Key: "pp", Description: "my shortcut"},
		{Type: scaffold.RepairPreserve, Category: scaffold.RepairSkillCategory, Key: ".campaign/skills/fest-execution/SKILL.md", Description: remedy},
	}}, Writers{HumanOut: &out})
	got := out.String()
	for _, want := range []string{".campaign/skills/fest-execution/SKILL.md", remedy, "(user-defined, preserved)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "nothing to repair") {
		t.Errorf("preserved skill notices must not be reported as nothing to repair:\n%s", got)
	}
}
