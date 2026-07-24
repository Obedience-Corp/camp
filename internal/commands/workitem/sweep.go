package workitem

import (
	"path/filepath"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
)

// resolveSweepLocation resolves the locate.Location for a sweep candidate from
// its RelativePath, reusing locate.DetectFromCwd (which is generic over any
// path under the item, not literally the process cwd) so the sweep and
// interactive promote share one notion of "where this item's dungeon lives."
// Every candidate PlanSweep can produce today has a workflow/<type>/<slug>
// RelativePath, so this is currently a pass-through onto DetectFromCwd's
// existing resolution; phase 3 (rail residents in festivals/) extends
// DetectFromCwd itself, and this function needs no change when that lands.
func resolveSweepLocation(campaignRoot string, item wkitem.WorkItem) (*locate.Location, error) {
	return locate.DetectFromCwd(campaignRoot, filepath.Join(campaignRoot, item.RelativePath))
}
