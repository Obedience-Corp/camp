//go:build dev

package quest

func init() {
	frozenQuestSchemaVersions = append(frozenQuestSchemaVersions,
		questSchemaVersion{"camp quest workspace --json", QuestWorkspaceJSONVersion, "quest-workspace/v1alpha1"})
}
