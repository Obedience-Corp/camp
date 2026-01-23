package dungeon

import "embed"

//go:embed templates/*
var templatesFS embed.FS

// GetOBEYTemplate returns the content of the OBEY.md template.
func GetOBEYTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/OBEY.md")
}

// GetArchivedREADME returns the content of the archived README template.
func GetArchivedREADME() ([]byte, error) {
	return templatesFS.ReadFile("templates/archived_README.md")
}
