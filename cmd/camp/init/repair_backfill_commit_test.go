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

func TestBuildRepairCommitFiles_IncludesModifiedGitignore(t *testing.T) {
	result := &scaffold.InitResult{
		CampaignRoot:  "/campaign",
		FilesModified: []string{"/campaign/.gitignore"},
	}

	files := buildRepairCommitFiles(result, nil, nil)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, ".gitignore") {
		t.Fatalf("commit files missing modified .gitignore: %v", files)
	}
}

func TestBuildRepairCommitMessage_IncludesFilesModified(t *testing.T) {
	msg := buildRepairCommitMessage(&scaffold.InitResult{
		FilesModified: []string{"/campaign/.gitignore"},
	}, nil, 0, nil)

	if !strings.Contains(msg, "Files updated:") {
		t.Fatalf("commit message missing updated-files summary: %q", msg)
	}
	if !strings.Contains(msg, ".gitignore") {
		t.Fatalf("commit message missing modified path: %q", msg)
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
