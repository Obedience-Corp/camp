package workflow

import (
	"testing"
)

func TestDefaultSchema(t *testing.T) {
	schema := DefaultSchema()

	t.Run("has correct version", func(t *testing.T) {
		if schema.Version != CurrentSchemaVersion {
			t.Errorf("Version = %d, want %d", schema.Version, CurrentSchemaVersion)
		}
	})

	t.Run("has correct type", func(t *testing.T) {
		if schema.Type != SchemaType {
			t.Errorf("Type = %q, want %q", schema.Type, SchemaType)
		}
	})

	t.Run("has required directories", func(t *testing.T) {
		required := []string{"active", "ready", "dungeon"}
		for _, name := range required {
			if _, ok := schema.Directories[name]; !ok {
				t.Errorf("missing required directory: %s", name)
			}
		}
	})

	t.Run("dungeon is nested", func(t *testing.T) {
		dungeon := schema.Directories["dungeon"]
		if !dungeon.Nested {
			t.Error("dungeon should be nested")
		}
		if len(dungeon.Children) == 0 {
			t.Error("dungeon should have children")
		}
	})

	t.Run("has default status", func(t *testing.T) {
		if schema.DefaultStatus == "" {
			t.Error("DefaultStatus should not be empty")
		}
		if _, ok := schema.Directories[schema.DefaultStatus]; !ok {
			t.Errorf("DefaultStatus %q is not a valid directory", schema.DefaultStatus)
		}
	})

	t.Run("has history tracking", func(t *testing.T) {
		if !schema.TrackHistory {
			t.Error("TrackHistory should be true")
		}
		if schema.HistoryFile == "" {
			t.Error("HistoryFile should not be empty")
		}
	})

	t.Run("validates successfully", func(t *testing.T) {
		if err := schema.Validate(); err != nil {
			t.Errorf("DefaultSchema should validate: %v", err)
		}
	})

	t.Run("has proper transition opts", func(t *testing.T) {
		active := schema.Directories["active"]
		if len(active.TransitionOpts) == 0 {
			t.Error("active should have transition_opts defined")
		}

		// Should be able to transition to ready and dungeon
		found := make(map[string]bool)
		for _, opt := range active.TransitionOpts {
			found[opt] = true
		}
		if !found["ready"] {
			t.Error("active should allow transition to ready")
		}
		if !found["dungeon"] {
			t.Error("active should allow transition to dungeon")
		}
	})

	t.Run("directories have descriptions", func(t *testing.T) {
		for name, dir := range schema.Directories {
			if dir.Description == "" {
				t.Errorf("directory %q should have a description", name)
			}
		}
	})
}
