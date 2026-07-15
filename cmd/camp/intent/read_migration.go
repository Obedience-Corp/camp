package intent

import (
	"fmt"
	"os"

	intentcore "github.com/Obedience-Corp/camp/internal/intent"
)

const legacyIntentMigrationWarning = "warning: legacy idea layout detected at workflow/intents/; run 'camp init --repair' to migrate\n"

func warnPendingLegacyMigration(svc *intentcore.IntentService) {
	pending, err := svc.PendingLegacyMigration()
	if err == nil && pending {
		fmt.Fprint(os.Stderr, legacyIntentMigrationWarning)
	}
}
