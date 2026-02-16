package workflow

import "embed"

//go:embed templates/*
var templatesFS embed.FS

// GetActiveOBEYTemplate returns the content of the active OBEY.md template.
func GetActiveOBEYTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/active_obey.md")
}

// GetReadyOBEYTemplate returns the content of the ready OBEY.md template.
func GetReadyOBEYTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/ready_obey.md")
}

// GetDungeonOBEYTemplate returns the content of the dungeon OBEY.md template.
func GetDungeonOBEYTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/dungeon_obey.md")
}

// GetFlowRootOBEYTemplate returns the content of the root OBEY.md Go template.
func GetFlowRootOBEYTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/flow_root_obey.md")
}
