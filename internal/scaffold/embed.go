package scaffold

import (
	"embed"
	"io/fs"
	"strings"
)

// CampaignScaffoldFS contains the embedded scaffold definitions and templates.
//
//go:embed all:campaign
var CampaignScaffoldFS embed.FS

// guild-scaffold joins template paths with filepath.Join. On Windows those
// paths contain backslashes, while embed.FS accepts slash-separated names.
// Keep the conversion at the embedded filesystem boundary for both init and
// repair, including validation and rendering inside guild-scaffold.
type slashPathFS struct{ fs.FS }

func (f slashPathFS) Open(name string) (fs.File, error) {
	return f.FS.Open(strings.ReplaceAll(name, `\`, "/"))
}

func embeddedScaffoldFS() fs.FS {
	return slashPathFS{CampaignScaffoldFS}
}
