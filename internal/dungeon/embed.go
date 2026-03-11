package dungeon

import (
	"embed"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
)

//go:embed templates/archived_README.md
var templatesFS embed.FS

// GetOBEYTemplate returns the content of the OBEY.md template.
func GetOBEYTemplate() ([]byte, error) {
	return dungeonscaffold.GetOBEYTemplate()
}

// GetArchivedREADME returns the content of the archived README template.
func GetArchivedREADME() ([]byte, error) {
	return templatesFS.ReadFile("templates/archived_README.md")
}
