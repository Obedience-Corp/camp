package main

import (
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

	files := buildRepairCommitFiles(result, plan)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, "workflow/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing legacy source path: %v", files)
	}
	if !strings.Contains(got, ".campaign/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing canonical destination path: %v", files)
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
	}, 0)

	if !strings.Contains(msg, "Migrated 1 legacy intent item(s):") {
		t.Fatalf("commit message missing intent migration summary: %q", msg)
	}
	if !strings.Contains(msg, "/campaign/workflow/intents/inbox/legacy.md → /campaign/.campaign/intents/inbox") {
		t.Fatalf("commit message missing intent migration detail: %q", msg)
	}
}
