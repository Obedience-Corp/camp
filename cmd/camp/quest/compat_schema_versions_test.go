//go:build dev

package quest

import "testing"

// TestQuestSchemaVersionsAreFrozen pins the quest surfaces' published schema
// strings. They live behind the dev build profile, so `just test unit` misses
// them and only `BUILD_TAGS=dev just test unit` runs this file.
func TestQuestSchemaVersionsAreFrozen(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"camp quest list --json", QuestListJSONVersion, "quest-list/v1alpha1"},
		{"camp quest show --json", QuestShowJSONVersion, "quest-show/v1alpha1"},
		{"camp quest links --json", QuestLinksJSONVersion, "quest-links/v1alpha1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("published schema version moved: got %q, want %q (docs/json-contracts.md)", tt.got, tt.want)
			}
		})
	}
}
