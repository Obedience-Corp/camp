package scaffold

import "embed"

//go:embed templates/*
var templatesFS embed.FS

// GetOBEYTemplate returns the content of the standard dungeon OBEY.md template.
func GetOBEYTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/OBEY.md")
}
