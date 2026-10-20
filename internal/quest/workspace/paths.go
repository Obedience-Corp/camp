package workspace

import (
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// UIStatePath returns the canonical ui-state.json path for a quest metadata file.
func UIStatePath(questYAMLPath string) string {
	return filepath.Join(filepath.Dir(questYAMLPath), FileName)
}

// LockPath returns the ui-state.lock path beside the layout file.
func LockPath(uiStatePath string) string {
	return filepath.Join(filepath.Dir(uiStatePath), LockFileName)
}

// CanonicalizeCampPath turns a camp-relative or absolute-within-camp path into
// a slash-separated camp-relative path. Empty paths are rejected. Paths that
// escape the camp root are rejected.
func CanonicalizeCampPath(campaignRoot, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "path is empty")
	}
	root, err := pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "resolve camp root")
	}

	var abs string
	if filepath.IsAbs(input) {
		abs = filepath.Clean(input)
	} else {
		cleaned := filepath.Clean(input)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "path escapes camp: %s", input)
		}
		abs = filepath.Join(root, cleaned)
	}

	if err := pathutil.ValidateBoundary(root, abs); err != nil {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "path outside camp: %s", input)
	}
	rel, err := pathutil.RelativeToRoot(root, abs)
	if err != nil {
		return "", camperrors.Wrap(err, "relativize camp path")
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "path resolves to camp root")
	}
	return rel, nil
}
