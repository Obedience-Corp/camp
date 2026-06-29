package initcmd

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/scaffold"
)

func TestBuildRepairCommitFiles_IncludesQuestDateBackfill(t *testing.T) {
	result := &scaffold.InitResult{CampaignRoot: "/campaign"}
	plan := &scaffold.RepairPlan{
		QuestDateBackfill: &scaffold.QuestDateBackfill{
			Path: "/campaign/.campaign/quests/default/quest.yaml",
		},
	}

	files := buildRepairCommitFiles(result, plan, nil)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, ".campaign/quests/default/quest.yaml") {
		t.Fatalf("commit files missing backfilled quest path: %v", files)
	}
}

func TestBuildRepairCommitMessage_IncludesQuestDateBackfill(t *testing.T) {
	msg := buildRepairCommitMessage(&scaffold.InitResult{}, &scaffold.RepairPlan{
		QuestDateBackfill: &scaffold.QuestDateBackfill{
			Path: "/campaign/.campaign/quests/default/quest.yaml",
		},
	}, 0, nil)

	if !strings.Contains(msg, "Backfilled default quest timestamp:") {
		t.Fatalf("commit message missing backfill summary: %q", msg)
	}
	if !strings.Contains(msg, ".campaign/quests/default/quest.yaml") {
		t.Fatalf("commit message missing backfill path: %q", msg)
	}
}
