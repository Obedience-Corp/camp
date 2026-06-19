package promote

import (
	"context"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

func promotablePool(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]workitem.WorkItem, error) {
	items, err := workitem.Discover(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering promotable items")
	}
	out := items[:0:0]
	for _, it := range items {
		if isPromotable(it) {
			out = append(out, it)
		}
	}
	return out, nil
}

func isPromotable(it workitem.WorkItem) bool {
	switch it.WorkflowType {
	case workitem.WorkflowTypeIntent:
		return it.LifecycleStage == workitem.LifecycleStageInbox || it.LifecycleStage == workitem.LifecycleStageReady
	case workitem.WorkflowTypeFestival:
		return it.LifecycleStage == workitem.LifecycleStagePlanning ||
			it.LifecycleStage == workitem.LifecycleStageReady ||
			it.LifecycleStage == workitem.LifecycleStageActive
	default:
		return true
	}
}
