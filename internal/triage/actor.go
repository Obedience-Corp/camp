package triage

import (
	"context"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
)

// ActorUnknown is recorded when git has no configured identity.
//
// A missing identity does not block a verdict. Losing the attribution is bad;
// losing the judgment because someone had not run `git config user.name` would
// be worse, and the run still records that the verdict was made.
const ActorUnknown = "unknown"

// ResolveActor returns who is recording a verdict.
//
// Verdicts are attributed because the whole model is recorded judgment: a
// decision nobody is named on cannot be questioned later. It reads the same
// git identity camp's commit and intent paths use, so a verdict and the commit
// that carries it name the same person.
func ResolveActor(ctx context.Context) string {
	if actor := strings.TrimSpace(git.GetUserName(ctx)); actor != "" {
		return actor
	}
	return ActorUnknown
}
