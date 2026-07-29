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

// festivalsDir is the campaign-relative folder the rail moves workitems into.
const festivalsDir = "festivals"

// Rail stages. "root" is a workitem still living under workflow/<type>/; ready
// and active are the physical festival stages and take their names from the
// lifecycle vocabulary so the promote target, the stage folder, and
// StagesByType cannot drift apart; dungeon covers any shelved source, which the
// rail never accepts as an origin.
const (
	railStageRoot    = "root"
	railStageReady   = string(wkitem.LifecycleStageReady)
	railStageActive  = string(wkitem.LifecycleStageActive)
	railStageDungeon = "dungeon"
)

// railStageOf reports which rail stage a resolved location currently occupies.
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
// The dungeon case preserves the semantics of the old blanket "active" rejection
// this replaced: moving a workitem back out of a dungeon is a restore, not a
// promote, and the rail does not become a loophole for it.
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
// repairs every reference to it, returning commitInputs so the shared promote
// tail supplies the audit event, ledger emit, and auto-commit unchanged.
//
// recordPromotedTo is deliberately not called here. promoted_to records where a
// shelved source was copied to (the festival and doc targets); a rail move
// relocates the directory itself, so its stage is its location and a
// promoted_to pointer would only be able to describe where it already is.
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

	// Stamp before moving, not after. Resident resolution refuses an unstamped
	// directory in a lifecycle folder, so a marker written after the move would
	// strand the workitem where nothing can resolve it if that write failed.
	// Stamping first leaves every failure recoverable at the original location.
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

// migrateRailReferences repairs the priority store, link registry, and current
// pointer for the resident's new path by reusing the rename migrations verbatim.
// A rail move is a path change that preserves the type prefix of the workitem
// key (design:workflow/design/x becomes design:festivals/ready/x), which is
// exactly the shape those helpers already handle.
//
// Returns whether links.yaml changed so the caller can stage it. Each store is
// best-effort: the move has already landed, so a bookkeeping failure is a
// warning rather than a reason to strand the moved workitem.
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

// ensureResidentMarker guarantees the workitem carries a canonical .workitem
// marker before it enters the rail.
//
// Directory workitems are not uniformly stamped: promote tolerates an unmarked
// directory today (recordPromotedTo skips the stamp when the marker is absent,
// and the ledger falls back to the slug for the same reason). Resident
// resolution is stricter, because a resident's parent segment is a lifecycle
// stage rather than a workflow type and so cannot supply the type. Minting the
// marker at rail entry is what reconciles the two.
//
// An existing marker is repaired, not trusted. The path type is authoritative
// here: migrateRailReferences keys the moved workitem on loc.Type, while
// resident resolution after the move reads the marker, so a marker carrying a
// different type would leave priority, links, and current pointing at a key no
// rail command resolves. A marker that cannot be parsed fails the promote
// before anything moves, so the workitem stays where every repair tool can
// still reach it.
//
// It reuses the camp workitem repair planner so a marker minted or repaired
// here is byte-identical to the one doctor --fix would write, including id
// generation and unique-ref derivation. Repair is idempotent: an already
// canonical marker plans no changes and is not rewritten.
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
