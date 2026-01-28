package workflow

// DefaultSchema returns the default workflow schema.
// This schema provides a standard structure for organizing work:
// - active: Work in progress
// - ready: Prepared for action
// - dungeon: Archive area with nested subdirectories
func DefaultSchema() *Schema {
	return &Schema{
		Version:       CurrentSchemaVersion,
		Type:          SchemaType,
		Name:          "Workflow",
		Description:   "Status workflow for organizing work",
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
