package initcmd

import (
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/scaffold"
)

func TestBuildRepairCommitFiles_IncludesIntentMigrations(t *testing.T) {
	result := &scaffold.InitResult{
		CampaignRoot: "/campaign",
	}
	plan := &scaffold.RepairPlan{
		IntentMigrations: []scaffold.MigrationAction{
			{
				Source: "/campaign/workflow/intents/inbox",
				Dest:   "/campaign/.campaign/intents/inbox",
				Items:  []string{"legacy.md"},
			},
		},
	}

	files := buildRepairCommitFiles(result, plan, nil)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, "workflow/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing legacy source path: %v", files)
	}
	if !strings.Contains(got, ".campaign/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing canonical destination path: %v", files)
	}
}

func TestBuildRepairCommitFiles_IncludesSkillProjections(t *testing.T) {
	result := &scaffold.InitResult{CampaignRoot: "/campaign"}
	skillPaths := []string{".claude/skills/code-review", ".agents/skills/code-review"}

	files := buildRepairCommitFiles(result, nil, skillPaths)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, ".claude/skills/code-review") {
		t.Fatalf("commit files missing projected claude skill link: %v", files)
	}
	if !strings.Contains(got, ".agents/skills/code-review") {
		t.Fatalf("commit files missing projected agents skill link: %v", files)
	}
}

func TestBuildRepairCommitMessage_IncludesIntentMigrations(t *testing.T) {
	msg := buildRepairCommitMessage(&scaffold.InitResult{}, &scaffold.RepairPlan{
		IntentMigrations: []scaffold.MigrationAction{
			{
				Source: "/campaign/workflow/intents/inbox",
				Dest:   "/campaign/.campaign/intents/inbox",
				Items:  []string{"legacy.md"},
			},
		},
	}, 0, nil)

	if !strings.Contains(msg, "Migrated 1 legacy intent item(s):") {
		t.Fatalf("commit message missing intent migration summary: %q", msg)
	}
	if !strings.Contains(msg, "/campaign/workflow/intents/inbox/legacy.md → /campaign/.campaign/intents/inbox") {
		t.Fatalf("commit message missing intent migration detail: %q", msg)
	}
}

func TestBuildRepairCommitMessage_IncludesSkillProjections(t *testing.T) {
	msg := buildRepairCommitMessage(&scaffold.InitResult{}, nil, 0,
		[]string{".claude/skills/code-review"})

	if !strings.Contains(msg, "Skill links projected:") {
		t.Fatalf("commit message missing skill projection summary: %q", msg)
	}
	if !strings.Contains(msg, ".claude/skills/code-review") {
		t.Fatalf("commit message missing projected skill path: %q", msg)
	}
}

// TestChooseWriters asserts the default writer routing.
func TestChooseWriters(t *testing.T) {
	w := ChooseWriters()
	if w.HumanOut != os.Stdout {
		t.Errorf("default mode HumanOut = %v, want os.Stdout", w.HumanOut)
	}
}
