package notice

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
)

// DungeonLegacyID identifies the legacy-dungeon-layout signal.
const DungeonLegacyID = "dungeon-legacy-layout"

// DungeonLegacy reports a notice when campaignRoot still uses the visible
// "dungeon" layout that camp dungeon migrate converts.
//
// It resolves only the root dungeon rather than sweeping the campaign, which
// keeps the check at two stats. The root dungeon is the campaign's signature:
// camp init always scaffolds one, and migrate converts every dungeon at once,
// so the root spelling answers the question the notice asks.
func DungeonLegacy(ctx context.Context, campaignRoot string) (*Notice, error) {
	resolved, err := spelling.Resolve(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}
	if !resolved.Exists || resolved.Hidden() {
		return nil, nil
	}
	return &Notice{
		ID:      DungeonLegacyID,
		Message: "this campaign uses the visible " + spelling.Visible + "/ layout; new campaigns hide it as " + spelling.Hidden + "/",
		Command: spelling.MigrateCommand,
	}, nil
}
