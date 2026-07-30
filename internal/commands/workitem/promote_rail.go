package workitem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/mdlinks"
	"github.com/Obedience-Corp/camp/internal/statusmove"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
)

const festivalsDir = "festivals"

// Stage names come from the lifecycle vocabulary so the promote target, the
// stage folder, and StagesByType cannot drift apart.
const (
	railStageRoot    = "root"
	railStageReady   = string(wkitem.LifecycleStageReady)
	railStageActive  = string(wkitem.LifecycleStageActive)
	railStageDungeon = "dungeon"
)

// railStageOf reports which rail stage a resolved location occupies.
func railStageOf(loc *locate.Location, root string) string {
	if loc.InDungeon {
		return railStageDungeon
	}
	rel := filepath.ToSlash(dungeoncmd.RelFromRoot(root, loc.SourcePath))
	switch {
	case strings.HasPrefix(rel, festivalsDir+"/"+railStageReady+"/"):
		return railStageReady
	case strings.HasPrefix(rel, festivalsDir+"/"+railStageActive+"/"):
		return railStageActive
	default:
		return railStageRoot
	}
}

// checkRailTransition enforces the forward-only rail: root -> ready -> active.
func checkRailTransition(from, target string) error {
	switch {
	case from == railStageDungeon:
		return camperrors.New("cannot promote to " + target +
			" from a dungeon: restoring workitems out of the dungeon is not a promote")
	case target == railStageReady && from != railStageRoot:
		return camperrors.New("cannot promote to ready from " + from +
			"; the rail is forward-only (root -> ready -> active)")
	case target == railStageActive && from == railStageActive:
		return camperrors.New("workitem is already active on the rail")
	}
	return nil
}

// doRailPromote moves a directory workitem onto festivals/<stage>/<slug> and
// repairs every reference to it. recordPromotedTo is intentionally not called:
// a rail move relocates the directory itself, so its stage is its location.
func doRailPromote(ctx context.Context, cfg *config.CampaignConfig, root string, loc *locate.Location, stage string, result *workitemPromoteResult) (*commitInputs, error) {
	if isFile, _ := promoteSourceShape(loc); isFile {
		return nil, camperrors.New(
			"cannot promote a file workitem onto the rail: a resident is a directory carrying its own " +
				wkitem.MetadataFilename + " marker")
	}

	oldRel := filepath.ToSlash(dungeoncmd.RelFromRoot(root, loc.SourcePath))
	newRel := path.Join(festivalsDir, stage, loc.Slug)
	destAbs := filepath.Join(root, filepath.FromSlash(newRel))
	if _, err := os.Lstat(destAbs); err == nil {
		return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists,
			"cannot promote to %s: destination %s already exists", stage, newRel)
	}

	// Before the move: resident resolution refuses an unstamped directory, so a
	// failed stamp must leave the workitem where it can still be resolved.
	if err := ensureResidentMarker(ctx, cfg, root, oldRel, loc.Type); err != nil {
		return nil, err
	}

	if _, err := statusmove.Move(ctx, loc.SourcePath, destAbs, statusmove.MoveOptions{BoundaryRoot: root}); err != nil {
		if errors.Is(err, statusmove.ErrAlreadyExists) {
			return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists,
				"cannot promote to %s: destination %s already exists", stage, newRel)
		}
		return nil, camperrors.Wrapf(err, "moving %s to %s", oldRel, newRel)
	}
	rewritten, err := mdlinks.RewriteForMove(ctx, root, loc.SourcePath, destAbs)
	if err != nil {
		return nil, camperrors.Wrapf(err,
			"rewriting markdown links after moving %s (move applied; recover with git status)", oldRel)
	}

	destPaths := []string{destAbs}
	if migrateRailReferences(ctx, root, loc.Type+":"+oldRel, loc.Type+":"+newRel, oldRel, newRel, result) {
		destPaths = append(destPaths, links.LinksPath(root))
	}

	result.To = newRel
	return &commitInputs{
		description: fmt.Sprintf("Promote workitem %s to %s", loc.Slug, newRel),
		sourcePaths: []string{loc.SourcePath},
		destPaths:   destPaths,
		rewritten:   rewritten,
	}, nil
}

// migrateRailReferences reuses the rename migrations: a rail move preserves the
// workitem key's type prefix and changes only the path. Returns whether
// links.yaml changed so the caller can stage it. Best-effort, since the move has
// already landed.
func migrateRailReferences(ctx context.Context, root, oldKey, newKey, oldRel, newRel string, result *workitemPromoteResult) bool {
	warn := func(format string, args ...any) {
		result.Warnings = append(result.Warnings, fmt.Sprintf(format, args...))
	}

	if _, err := migrateRenamePriority(ctx, root, oldKey, newKey); err != nil {
		warn("migrate priority entries: %v", err)
	}

	linksChanged, err := migrateRenameLinks(ctx, root, oldKey, newKey, oldRel, newRel)
	if err != nil {
		warn("re-home workitem links: %v", err)
	}

	if _, err := migrateRenameCurrent(ctx, root, oldKey, newKey); err != nil {
		warn("update current workitem selection: %v", err)
	}
	return linksChanged
}

// ensureResidentMarker makes the source's .workitem marker canonical before it
// enters the rail. Directory workitems are not uniformly stamped, but resident
// resolution reads the marker for the type, so a missing one strands the item.
//
// An existing marker is repaired, not trusted: the path type is authoritative
// because migrateRailReferences keys the move on loc.Type while resolution after
// the move reads the marker, so a disagreeing marker would leave priority,
// links, and current pointing at a key no rail command resolves. An unparseable
// marker fails the promote before anything moves.
//
// Repair goes through the doctor --fix planner and is idempotent.
func ensureResidentMarker(ctx context.Context, cfg *config.CampaignConfig, root, relPath, typeName string) error {
	plan, err := planRepair(ctx, root, cfg, relPath, typeName)
	if err != nil {
		return camperrors.Wrapf(err, "repairing %s marker for %s", wkitem.MetadataFilename, relPath)
	}
	if len(plan.changes) == 0 {
		return nil
	}
	absDir := filepath.Join(root, filepath.FromSlash(relPath))
	if err := writeMarker(absDir, plan.meta); err != nil {
		return camperrors.Wrapf(err, "writing %s marker for %s", wkitem.MetadataFilename, relPath)
	}
	return nil
}
