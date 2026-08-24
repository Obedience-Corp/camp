package tokens

import (
	"context"
	"os"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// AnnotateItems counts tokens for each work item's primary document and sets
// TokenCount on the item. Items without a readable primary doc get a zero
// count (omitted from JSON via omitempty). Cache failures are silent: the
// count is still correct, only the cache optimization is at risk.
func AnnotateItems(ctx context.Context, c *Counter, campaignRoot string, items []wkitem.WorkItem) {
	for i := range items {
		if err := ctx.Err(); err != nil {
			return
		}
		path := items[i].AbsPrimaryDoc(campaignRoot)
		if path == "" {
			path = items[i].AbsPath(campaignRoot)
		}
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		count, err := c.CountFile(ctx, path)
		if err != nil {
			continue
		}
		items[i].TokenCount = count
	}
}
