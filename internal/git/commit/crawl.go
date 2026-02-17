package commit

import "context"

// CrawlOptions configures a dungeon crawl commit.
type CrawlOptions struct {
	Options
	Description string // Body text describing moved items
}

// Crawl stages all changes and commits for a dungeon crawl operation.
func Crawl(ctx context.Context, opts CrawlOptions) Result {
	return doCommit(ctx, opts.Options, "Crawl", "dungeon crawl completed", opts.Description)
}
