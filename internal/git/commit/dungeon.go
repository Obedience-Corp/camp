package commit

import "context"

// DungeonMigrateOptions configures the commit for a dungeon spelling migration.
type DungeonMigrateOptions struct {
	Options
	Description string // Body text listing the migrated dungeons
}

// DungeonMigrate commits a dungeon spelling migration. The renames are already
// staged, so Options.Files carries the new hidden paths and Options.PreStaged
// carries the old visible paths whose staged removal the commit must include.
func DungeonMigrate(ctx context.Context, opts DungeonMigrateOptions) Result {
	return doCommit(ctx, opts.Options, "Migrate", "dungeon directories to .dungeon", opts.Description)
}
