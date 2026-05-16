package workitem

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/paths"
)

// Discover scans all campaign work surfaces and returns a sorted list of work items.
// Checks context cancellation between each discovery stage since each may spawn
// git subprocesses.
func Discover(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	var items []WorkItem

	intents, err := discoverIntents(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, intents...)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	designs, err := discoverDesign(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, designs...)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	explores, err := discoverExplore(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, explores...)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	customs, err := discoverCustomWorkflowTypes(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, customs...)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	festivals, err := discoverFestivals(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, err
	}
	items = append(items, festivals...)

	Sort(items)
	return items, nil
}
