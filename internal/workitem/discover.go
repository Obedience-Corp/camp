package workitem

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/paths"
)

// Discover scans all campaign work surfaces and returns a sorted list of work items.
func Discover(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	var items []WorkItem

	intents, err := discoverIntents(ctx, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, intents...)

	designs, err := discoverDesign(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, designs...)

	explores, err := discoverExplore(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, explores...)

	festivals, err := discoverFestivals(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, festivals...)

	Sort(items)
	return items, nil
}
