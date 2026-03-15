package intent

import (
	"context"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

func appendIntentAuditEvent(ctx context.Context, intentsDir string, event audit.Event) error {
	if event.Actor == "" {
		event.Actor = resolveIntentActor(ctx)
	}
	if err := audit.AppendEvent(ctx, intentsDir, event); err != nil {
		return camperrors.Wrap(err, "writing intent audit event")
	}
	return nil
}
