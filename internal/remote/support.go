package remote

import (
	"runtime"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ErrWindowsPeerUnsupported identifies a remote Camp operation that this
// release cannot safely execute from Windows. SSH login-shell commands and
// rsync paths assume a POSIX peer protocol that has no native Windows proof.
var ErrWindowsPeerUnsupported = camperrors.New("remote Camp peer operations are unavailable on Windows in this release; use local Camp commands or run the peer operation from macOS/Linux")

// RequirePeerSupport fails before a peer command can start a copy, stage a
// partial file, or fall back to an origin-only result that looks successful.
func RequirePeerSupport() error {
	return requirePeerSupportFor(runtime.GOOS)
}

func requirePeerSupportFor(goos string) error {
	if goos == "windows" {
		return ErrWindowsPeerUnsupported
	}
	return nil
}
