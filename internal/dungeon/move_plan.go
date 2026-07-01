package dungeon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/statusmove"
)

type MoveKind string

const (
	MoveKindStatus       MoveKind = "status"
	MoveKindTriageRoot   MoveKind = "triage_root"
	MoveKindTriageStatus MoveKind = "triage_status"
	MoveKindDocs         MoveKind = "docs"
)

type MovePlan struct {
	Kind        MoveKind
	ItemName    string
	Source      string
	Destination string
	Status      string

	plan          statusmove.Plan
	alreadyExists string
	moveContext   string
}

func (mp *MovePlan) translateErr(err error) error {
	switch {
	case errors.Is(err, camperrors.ErrNotFound):
		return camperrors.Wrap(ErrNotFound, mp.ItemName)
	case errors.Is(err, statusmove.ErrAlreadyExists):
		return camperrors.Wrap(ErrAlreadyExists, mp.alreadyExists)
	default:
		return camperrors.Wrap(err, mp.moveContext)
	}
}

func (mp *MovePlan) resolve(ctx context.Context, srcPath, dstRoot string, opts statusmove.MoveOptions) error {
	sp, err := statusmove.PlanMove(ctx, srcPath, dstRoot, opts)
	if err != nil {
		return mp.translateErr(err)
	}
	if !opts.DatedBucket {
		if _, statErr := os.Lstat(sp.FinalDst); statErr == nil {
			return mp.translateErr(statusmove.ErrAlreadyExists)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return camperrors.Wrapf(statErr, "checking destination %s", sp.FinalDst)
		}
	}
	mp.plan = sp
	mp.Source = sp.Src
	mp.Destination = sp.FinalDst
	return nil
}

func (s *Service) PlanMoveToStatus(ctx context.Context, itemName, status string) (*MovePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	if err := validateStatusName(status); err != nil {
		return nil, err
	}
	validName, err := validateDirectChildItemName(itemName)
	if err != nil {
		return nil, err
	}
	itemName = validName

	srcPath := filepath.Join(s.dungeonPath, itemName)
	if err := ensureSourceInDungeonRoot(srcPath, s.dungeonPath, itemName); err != nil {
		return nil, err
	}

	mp := &MovePlan{
		Kind:          MoveKindStatus,
		ItemName:      itemName,
		Status:        status,
		alreadyExists: fmt.Sprintf("%s already in %s/", itemName, status),
		moveContext:   fmt.Sprintf("moving %s to %s", itemName, status),
	}
	if err := mp.resolve(ctx, srcPath, filepath.Join(s.dungeonPath, status), statusmove.MoveOptions{
		DatedBucket:  true,
		BoundaryRoot: s.campaignRoot,
	}); err != nil {
		return nil, err
	}
	return mp, nil
}

func (s *Service) PlanMoveToDungeon(ctx context.Context, itemName, parentPath string) (*MovePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	validName, err := validateDirectChildItemName(itemName)
	if err != nil {
		return nil, err
	}
	itemName = validName
	if err := s.validateParentMoveCandidate(ctx, parentPath, itemName); err != nil {
		return nil, err
	}

	targetPath := filepath.Join(s.dungeonPath, itemName)
	if err := pathutil.ValidateBoundary(s.campaignRoot, targetPath); err != nil {
		return nil, camperrors.Wrap(ErrNotInDungeon, "target outside campaign root")
	}
	if _, err := os.Stat(s.dungeonPath); err != nil {
		return nil, camperrors.Wrap(err, "dungeon directory does not exist")
	}

	mp := &MovePlan{
		Kind:          MoveKindTriageRoot,
		ItemName:      itemName,
		alreadyExists: fmt.Sprintf("%s already in dungeon", itemName),
		moveContext:   fmt.Sprintf("moving %s to dungeon", itemName),
	}
	if err := mp.resolve(ctx, filepath.Join(parentPath, itemName), targetPath, statusmove.MoveOptions{
		BoundaryRoot: s.campaignRoot,
	}); err != nil {
		return nil, err
	}
	return mp, nil
}

func (s *Service) PlanMoveToDungeonStatus(ctx context.Context, itemName, parentPath, status string) (*MovePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	if err := validateStatusName(status); err != nil {
		return nil, err
	}
	validName, err := validateDirectChildItemName(itemName)
	if err != nil {
		return nil, err
	}
	itemName = validName
	if err := s.validateParentMoveCandidate(ctx, parentPath, itemName); err != nil {
		return nil, err
	}

	sourcePath := filepath.Join(parentPath, itemName)
	if err := pathutil.ValidateBoundary(s.campaignRoot, sourcePath); err != nil {
		return nil, camperrors.Wrap(ErrNotInDungeon, "source outside campaign root")
	}

	mp := &MovePlan{
		Kind:          MoveKindTriageStatus,
		ItemName:      itemName,
		Status:        status,
		alreadyExists: fmt.Sprintf("%s already in %s/", itemName, status),
		moveContext:   fmt.Sprintf("moving %s to dungeon/%s", itemName, status),
	}
	if err := mp.resolve(ctx, sourcePath, filepath.Join(s.dungeonPath, status), statusmove.MoveOptions{
		DatedBucket:  true,
		BoundaryRoot: s.campaignRoot,
	}); err != nil {
		return nil, err
	}
	return mp, nil
}

func (s *Service) ApplyMove(ctx context.Context, mp *MovePlan) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled")
	}
	dst, err := mp.plan.Apply(ctx)
	if err != nil {
		return "", mp.translateErr(err)
	}
	if err := s.rewriteLinksAfterMove(ctx, mp.Source, dst); err != nil {
		return "", camperrors.Wrapf(err, "rewriting markdown links after moving %s", mp.ItemName)
	}
	return dst, nil
}

func ensureSourceInDungeonRoot(srcPath, dungeonPath, itemName string) error {
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return camperrors.Wrap(ErrNotFound, itemName)
	}
	absSource, err := filepath.Abs(srcPath)
	if err != nil {
		return camperrors.Wrap(err, "resolving source path")
	}
	absDungeon, err := filepath.Abs(dungeonPath)
	if err != nil {
		return camperrors.Wrap(err, "resolving dungeon path")
	}
	if filepath.Dir(absSource) != absDungeon {
		return camperrors.Wrap(ErrNotInDungeon, itemName)
	}
	return nil
}
