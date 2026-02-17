package workflow

// DefaultSchemaV2 returns the default workflow schema version 2.
// V2 uses a dungeon-centric model where:
//   - Root directory (`.`) = active work (default status)
//   - All other statuses live under `dungeon/`
//
// This eliminates the need for separate `active/` and `ready/` directories.
func DefaultSchemaV2() *Schema {
	return DefaultSchemaV2WithInfo("", "")
}

// DefaultSchemaV2WithInfo returns a v2 schema with custom name and description.
// Empty values fall back to defaults.
func DefaultSchemaV2WithInfo(name, description string) *Schema {
	if name == "" {
		name = "Workflow"
	}
	if description == "" {
		description = "Dungeon-centric workflow for organizing work"
	}
	return &Schema{
		Version:       2,
		Type:          SchemaType,
		Name:          name,
		Description:   description,
		DefaultStatus: ".",
		TrackHistory:  true,
		HistoryFile:   DefaultHistoryFile,
		Directories: map[string]Directory{
			".": {
				Description:    "Active work in progress",
				Order:          1,
				TransitionOpts: []string{"dungeon"},
			},
			"dungeon": {
				Description: "All non-active statuses",
				Order:       2,
				Nested:      true,
				Children: map[string]Directory{
					"completed": {
						Description: "Successfully finished work",
						Order:       1,
					},
					"archived": {
						Description: "Preserved but no longer active",
						Order:       2,
					},
					"someday": {
						Description: "Maybe later, low priority",
						Order:       3,
					},
				},
			},
		},
	}
}

// DefaultSchema returns the default workflow schema.
// This schema provides a standard structure for organizing work:
// - active: Work in progress
// - ready: Prepared for action
// - dungeon: Archive area with nested subdirectories
func DefaultSchema() *Schema {
	return DefaultSchemaWithInfo("", "")
}

// DefaultSchemaWithInfo returns a v1 schema with custom name and description.
// Empty values fall back to defaults.
func DefaultSchemaWithInfo(name, description string) *Schema {
	if name == "" {
		name = "Workflow"
	}
	if description == "" {
		description = "Status workflow for organizing work"
	}
	return &Schema{
		Version:       CurrentSchemaVersion,
		Type:          SchemaType,
		Name:          name,
		Description:   description,
		DefaultStatus: "active",
		TrackHistory:  true,
		HistoryFile:   DefaultHistoryFile,
		Directories: map[string]Directory{
			"active": {
				Description:    "Work actively being done",
				Order:          1,
				TransitionOpts: []string{"ready", "dungeon"},
			},
			"ready": {
				Description:    "Prepared and ready for action",
				Order:          2,
				TransitionOpts: []string{"active", "dungeon"},
			},
			"dungeon": {
				Description: "Archive area for completed, archived, or deferred work",
				Order:       3,
				Nested:      true,
				Children: map[string]Directory{
					"completed": {
						Description: "Successfully finished work",
						Order:       1,
					},
					"archived": {
						Description: "Preserved but no longer active",
						Order:       2,
					},
					"someday": {
						Description: "Maybe later, low priority",
						Order:       3,
					},
				},
			},
		},
	}
}
