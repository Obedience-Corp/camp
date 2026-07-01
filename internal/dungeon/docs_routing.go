package dungeon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/statusmove"
)

const docsDirName = "docs"

// ResolveDocsDestination resolves a docs destination to an absolute directory path
// under campaign-root docs/. The destination must identify a docs subdirectory.
func ResolveDocsDestination(campaignRoot, destination string) (string, error) {
	docsRoot := filepath.Join(campaignRoot, docsDirName)

	subpath, err := normalizeDocsDestinationSubpath(destination)
	if err != nil {
		return "", err
	}

	targetDir := filepath.Join(docsRoot, subpath)
	if err := pathutil.ValidateBoundary(campaignRoot, targetDir); err != nil {
		return "", camperrors.Wrapf(
			ErrInvalidDocsDestination,
			"%q resolves outside campaign root docs/",
			destination,
		)
	}

	rel, err := filepath.Rel(docsRoot, targetDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", camperrors.Wrapf(
			ErrInvalidDocsDestination,
			"%q must resolve to a docs subdirectory under docs/",
			destination,
		)
	}

	return targetDir, nil
}

// MoveToDocs routes an item from a parent directory into campaign-root docs.
// destination must resolve to a docs subdirectory (for example: "api/reference").
func (s *Service) MoveToDocs(ctx context.Context, itemName, parentPath, destination string) (string, error) {
	mp, err := s.PlanMoveToDocs(ctx, itemName, parentPath, destination)
	if err != nil {
		return "", err
	}
	return s.ApplyMove(ctx, mp)
}

func (s *Service) PlanMoveToDocs(ctx context.Context, itemName, parentPath, destination string) (*MovePlan, error) {
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

	sourcePath := filepath.Join(parentPath, itemName)
	if err := pathutil.ValidateBoundary(parentPath, sourcePath); err != nil {
		return nil, camperrors.Wrapf(
			ErrInvalidItemPath,
			"%q is not a direct child item name in the resolved triage context",
			itemName,
		)
	}
	if err := pathutil.ValidateBoundary(s.campaignRoot, sourcePath); err != nil {
		return nil, camperrors.Wrap(ErrNotInDungeon, "source outside campaign root")
	}

	targetPath, err := s.resolveDocsTargetPath(itemName, destination)
	if err != nil {
		return nil, err
	}

	mp := &MovePlan{
		Kind:          MoveKindDocs,
		ItemName:      itemName,
		Status:        destination,
		alreadyExists: fmt.Sprintf("%s already exists in docs destination", itemName),
		moveContext:   fmt.Sprintf("moving %s to docs/%s", itemName, destination),
	}
	if err := mp.resolve(ctx, sourcePath, targetPath, statusmove.MoveOptions{
		BoundaryRoot: filepath.Join(s.campaignRoot, docsDirName),
	}); err != nil {
		return nil, err
	}
	return mp, nil
}

func (s *Service) resolveDocsTargetPath(itemName, destination string) (string, error) {
	targetDir, err := ResolveDocsDestination(s.campaignRoot, destination)
	if err != nil {
		return "", err
	}

	docsRoot := filepath.Join(s.campaignRoot, docsDirName)
	docsRootInfo, err := os.Stat(docsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", camperrors.Wrap(
				ErrInvalidDocsDestination,
				"campaign-root docs/ directory does not exist",
			)
		}
		return "", camperrors.Wrap(err, "reading campaign docs directory")
	}
	if !docsRootInfo.IsDir() {
		return "", camperrors.Wrap(
			ErrInvalidDocsDestination,
			"campaign-root docs/ path is not a directory",
		)
	}

	targetDirInfo, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", camperrors.Wrapf(
				ErrInvalidDocsDestination,
				"%q does not exist under campaign-root docs/; choose an existing docs subdirectory",
				destination,
			)
		}
		return "", camperrors.Wrapf(err, "reading docs destination %s", destination)
	}
	if !targetDirInfo.IsDir() {
		return "", camperrors.Wrapf(
			ErrInvalidDocsDestination,
			"%q must resolve to an existing docs subdirectory",
			destination,
		)
	}

	targetPath := filepath.Join(targetDir, itemName)
	if err := pathutil.ValidateBoundary(docsRoot, targetPath); err != nil {
		return "", camperrors.Wrapf(
			ErrInvalidDocsDestination,
			"%q resolves outside campaign root docs/",
			destination,
		)
	}
	return targetPath, nil
}

func normalizeDocsDestinationSubpath(destination string) (string, error) {
	dest := strings.TrimSpace(destination)
	if dest == "" {
		return "", camperrors.Wrap(
			ErrInvalidDocsDestination,
			"docs destination is required (example: --to-docs architecture/api)",
		)
	}
	if filepath.IsAbs(dest) {
		return "", camperrors.Wrapf(ErrInvalidDocsDestination, "%q must be relative to docs/", destination)
	}

	dest = filepath.Clean(dest)

	if dest == docsDirName {
		return "", camperrors.Wrap(
			ErrInvalidDocsDestination,
			"docs destination must be a subdirectory under docs/ (example: --to-docs architecture)",
		)
	}

	docsPrefix := docsDirName + string(filepath.Separator)
	dest = strings.TrimPrefix(dest, docsPrefix)

	if dest == "." || dest == "" || dest == ".." || strings.HasPrefix(dest, ".."+string(filepath.Separator)) {
		return "", camperrors.Wrapf(ErrInvalidDocsDestination, "%q escapes docs/", destination)
	}

	return dest, nil
}
