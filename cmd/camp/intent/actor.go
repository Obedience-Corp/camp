package intent

import (
	"context"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
)

func resolveIntentActor(ctx context.Context) string {
	actor := strings.TrimSpace(git.GetUserName(ctx))
	if actor == "" {
		return "system"
	}
	return actor
}
