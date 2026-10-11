package explore

import (
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// invalidInput is a filter or flag value the feed cannot honor. It unwraps to
// a ValidationError so a command can give it exit 2. Error stays the bare
// sentence because the feed prints it on its status line.
type invalidInput struct {
	validation *camperrors.ValidationError
}

func (e *invalidInput) Error() string { return e.validation.Message }
func (e *invalidInput) Unwrap() error { return e.validation }

func invalid(field, message string) error {
	return &invalidInput{validation: camperrors.NewValidation(field, message, nil)}
}

func errUnknownStatus(name string) error {
	return invalid("status", fmt.Sprintf("unknown status %q; use finished, completed, done, archived, someday, killed, holding, all, or a status directory name", name))
}

func errUnknownDungeon(name string) error {
	return invalid("dungeon", fmt.Sprintf("unknown dungeon %q; use all, a dungeon label, or a camp-relative dungeon path", name))
}

func errAmbiguousDungeon(name string) error {
	return invalid("dungeon", fmt.Sprintf("dungeon %q matches more than one path; pass the camp-relative dungeon path", name))
}

func errBadDay(field, value string) error {
	return invalid(field, fmt.Sprintf("--%s must be YYYY-MM-DD, got %q", field, value))
}

func errSinceAfterUntil() error {
	return invalid("since", "--since is after --until")
}

func errUnknownImages(value string) error {
	return invalid("images", fmt.Sprintf("unknown --images %q; use auto, kitty, iterm, or off", value))
}

// ErrReplayTooLong means the GIF must not play because it is past a decode
// budget. A poster comes with it when at least one whole frame fits.
var ErrReplayTooLong = camperrors.New("replay is too long to play here")

// ErrReplayIncomplete means the file ends before its last frame does. It does
// not play; the poster is the canvas of the whole frames before the break.
var ErrReplayIncomplete = camperrors.New("replay file is incomplete")

var errNotGIF = camperrors.New("replay is not a valid GIF")

// These travel inside an *fs.PathError, so a warning can name the file by its
// camp-relative path instead of the absolute one.
var (
	errNotRegular  = camperrors.New("not a regular file")
	errFileChanged = camperrors.New("file changed while opening")
)

var (
	errFrontmatterShape = camperrors.New("frontmatter is not a list of key: value lines")
	errFrontmatterOpen  = camperrors.New("frontmatter has no closing --- line")
)
