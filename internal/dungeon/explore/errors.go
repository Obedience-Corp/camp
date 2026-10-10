package explore

import camperrors "github.com/Obedience-Corp/camp/internal/errors"

func errUnknownStatus(name string) error {
	return camperrors.Newf("unknown status %q; use finished, completed, done, archived, someday, killed, holding, all, or a status directory name", name)
}

func errUnknownDungeon(name string) error {
	return camperrors.Newf("unknown dungeon %q; use all, a dungeon label, or a camp-relative dungeon path", name)
}

func errAmbiguousDungeon(name string) error {
	return camperrors.Newf("dungeon %q matches more than one path; pass the camp-relative dungeon path", name)
}

func errBadDay(flag, value string) error {
	return camperrors.Newf("%s must be YYYY-MM-DD, got %q", flag, value)
}

func errSinceAfterUntil() error {
	return camperrors.New("--since is after --until")
}

// ErrReplayTooLong means the GIF can contribute a poster but must not play.
var ErrReplayTooLong = camperrors.New("replay is too long to play here")
