package quest

import "testing"

// TestQuestSchemaVersionsAreFrozen pins the quest surfaces' published schema
// strings.
func TestQuestSchemaVersionsAreFrozen(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"camp quest list --json", QuestListJSONVersion, "quest-list/v1alpha1"},
		{"camp quest create --json", QuestCreateJSONVersion, "quest-create/v1alpha1"},
		{"camp quest show --json", QuestShowJSONVersion, "quest-show/v1alpha1"},
		{"camp quest links --json", QuestLinksJSONVersion, "quest-links/v1alpha1"},
		{"camp quest workspace --json", QuestWorkspaceJSONVersion, "quest-workspace/v1alpha1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("published schema version moved: got %q, want %q (docs/json-contracts.md)", tt.got, tt.want)
			}
		})
	}
}
