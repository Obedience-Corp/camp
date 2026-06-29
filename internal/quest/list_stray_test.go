package quest

import (
	"os"
	"strings"
	"testing"
)

func TestListSkipsStrayDirWithoutWarning(t *testing.T) {
	ctx, root, _ := setupQuestCampaign(t)

	strayDir := QuestDir(root, "straydir")
	if err := os.MkdirAll(strayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strayDir+"/ui-state.json", []byte(`{"ui":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	malformedDir := QuestDir(root, "broken")
	if err := os.MkdirAll(malformedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(QuestPathForDir(malformedDir), []byte(":\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var quests []*Quest
	warnings := captureStderr(t, func() {
		var listErr error
		quests, listErr = List(ctx, root, false)
		if listErr != nil {
			t.Fatalf("List() error = %v", listErr)
		}
	})

	for _, q := range quests {
		if q.Slug == "straydir" || q.Slug == "broken" {
			t.Fatalf("non-quest directory leaked into List result: %#v", q)
		}
	}

	foundDefault := false
	for _, q := range quests {
		if q.IsDefault() {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Fatalf("valid default quest was not listed: %#v", quests)
	}

	if strings.Contains(warnings, "straydir") {
		t.Fatalf("stray directory must not produce a warning:\n%s", warnings)
	}
	if !strings.Contains(warnings, `warning: skipping unreadable quest "broken"`) {
		t.Fatalf("malformed quest.yaml must still warn:\n%s", warnings)
	}
}
