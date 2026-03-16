package quest

import (
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var (
	ErrDuplicateLink = camperrors.Wrap(camperrors.ErrInvalidInput, "link already exists")
	ErrLinkNotFound  = camperrors.Wrap(camperrors.ErrNotFound, "link not found")
)

// DetectLinkType infers the link type from its campaign-relative path.
func DetectLinkType(path string) string {
	normalized := filepath.ToSlash(path)

	switch {
	case strings.HasPrefix(normalized, "workflow/intents/"):
		return "intent"
	case strings.HasPrefix(normalized, "workflow/design/"):
		return "design"
	case strings.HasPrefix(normalized, "workflow/explore/"):
		return "explore"
	case strings.HasPrefix(normalized, "festivals/"):
		return "festival"
	case strings.HasPrefix(normalized, "projects/"):
		return "project"
	default:
		return "document"
	}
}

// ValidateLinkPath confirms the path exists relative to the campaign root.
func ValidateLinkPath(campaignRoot, path string) error {
	abs := filepath.Join(campaignRoot, path)
	if _, err := os.Stat(abs); err != nil {
		return camperrors.Wrapf(camperrors.ErrNotFound, "link target does not exist: %s", path)
	}
	return nil
}

// AddLink appends a link to the quest, returning an error if a link with the
// same path already exists.
func AddLink(q *Quest, link Link) error {
	for _, existing := range q.Links {
		if existing.Path == link.Path {
			return camperrors.Wrapf(ErrDuplicateLink, "%s", link.Path)
		}
	}
	q.Links = append(q.Links, link)
	return nil
}

// RemoveLink removes a link by path, returning an error if not found.
func RemoveLink(q *Quest, path string) error {
	for i, link := range q.Links {
		if link.Path == path {
			q.Links = append(q.Links[:i], q.Links[i+1:]...)
			return nil
		}
	}
	return camperrors.Wrapf(ErrLinkNotFound, "%s", path)
}
