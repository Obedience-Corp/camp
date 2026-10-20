package scaffold

import (
	"context"
	"io/fs"
	"os"
	"path"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/quest"
)

const defaultQuestTemplatePath = "campaign/templates/" + quest.RootDirName + "/" + quest.DefaultDirName + "/" + quest.FileName + ".tmpl"

// campaignScaffoldFS returns the embedded campaign scaffold that init and the
// repair plan render into absDir. Completing or archiving the default quest
// moves it into the quest dungeon under its fixed qst_default identity, so the
// default quest template is withheld while that quest exists anywhere else.
func campaignScaffoldFS(ctx context.Context, absDir string) (fs.FS, error) {
	base := embeddedScaffoldFS()
	if _, err := os.Stat(quest.DefaultQuestPath(absDir)); err == nil {
		return base, nil
	}
	exists, err := quest.DefaultQuestExists(ctx, absDir)
	if err != nil {
		return nil, camperrors.Wrap(err, "check for an existing default quest")
	}
	if !exists {
		return base, nil
	}
	return withoutFileFS{FS: base, hidden: defaultQuestTemplatePath}, nil
}

// withoutFileFS hides one file from both Open and ReadDir, because
// guild-scaffold discovers templates with fs.WalkDir and renders them by Open.
type withoutFileFS struct {
	fs.FS
	hidden string
}

func (f withoutFileFS) Open(name string) (fs.File, error) {
	if f.hides(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f.FS.Open(name)
}

func (f withoutFileFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.FS, name)
	if err != nil {
		return nil, err
	}
	kept := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !f.hides(path.Join(slashName(name), entry.Name())) {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

func (f withoutFileFS) hides(name string) bool {
	return path.Clean(slashName(name)) == f.hidden
}

func slashName(name string) string {
	return strings.ReplaceAll(name, `\`, "/")
}
