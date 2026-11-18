package skills

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ObeyManagedMarkerFile marks a tool skills directory that Obey projects into.
// Obey writes it and keeps the directory in step with .campaign/skills/.
const ObeyManagedMarkerFile = ".obey-managed-skills"

// LinkResult values for one tool directory or one bundle link.
const (
	LinkLinked        = "linked"
	LinkAlreadyLinked = "already-linked"
	LinkLeftToObey    = "left-to-obey"
	LinkNoFolder      = "no-folder"
	LinkLeftUnmanaged = "left-unmanaged"
	LinkConflict      = "conflict"
)

// BundleLink is what linking did in one tool directory: one row per bundle
// when camp linked there, or one row for the directory when it left it alone.
type BundleLink struct {
	Tool   string
	Path   string
	Result string
}

// LinkBundles makes slugs discoverable in each camp-root tool directory that
// already holds camp's links. It never creates a tool directory: a missing
// one, one Obey manages, and one camp has never linked into are left alone,
// so the camp's other skills keep reaching every tool that reads there.
func LinkBundles(root, skillsDir string, slugs []string, dryRun bool) ([]BundleLink, error) {
	for _, slug := range slugs {
		if !validBundleSegment(slug) {
			return nil, camperrors.Newf("cannot link %q: a skill bundle is one folder name", slug)
		}
	}
	var links []BundleLink
	for _, tool := range ToolNames() {
		rel := toolPaths[tool]
		dest := filepath.Join(root, rel)
		result, err := linkFolderState(root, dest, skillsDir)
		if err != nil {
			return links, err
		}
		if result != "" {
			links = append(links, BundleLink{Tool: tool, Path: rel, Result: result})
			continue
		}
		for _, slug := range slugs {
			result, err := linkOneBundle(dest, skillsDir, slug, dryRun)
			if err != nil {
				return links, err
			}
			links = append(links, BundleLink{Tool: tool, Path: rel + "/" + slug, Result: result})
		}
	}
	return links, nil
}

// linkFolderState returns the result for a tool directory camp leaves alone,
// or "" when it already holds camp's links.
func linkFolderState(root, dest, skillsDir string) (string, error) {
	pathType, err := CheckPathType(dest)
	if err != nil {
		return "", err
	}
	switch {
	case pathType == TypeMissing:
		return LinkNoFolder, nil
	case pathType != TypeDirectory:
		return LinkLeftUnmanaged, nil
	case HasObeyMarker(dest):
		return LinkLeftToObey, nil
	case ValidateDestination(dest, root) != nil:
		return LinkLeftUnmanaged, nil
	}
	held, err := holdsCampLinks(dest, skillsDir)
	if err != nil || !held {
		return LinkLeftUnmanaged, err
	}
	return "", nil
}

func holdsCampLinks(dest, skillsDir string) (bool, error) {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return false, nil
	}
	for _, entry := range entries {
		linkPath := filepath.Join(dest, entry.Name())
		managed, err := IsManagedSkillEntryLink(linkPath, filepath.Join(skillsDir, entry.Name()), skillsDir)
		if err != nil {
			return false, err
		}
		if managed {
			return true, nil
		}
	}
	return false, nil
}

func linkOneBundle(dest, skillsDir, slug string, dryRun bool) (string, error) {
	summary, err := ProjectSkillEntries(dest, skillsDir, []string{slug}, dryRun, false)
	switch {
	case err != nil:
		return "", err
	case summary.Conflicts > 0:
		return LinkConflict, nil
	case summary.Created > 0 || summary.Replaced > 0:
		return LinkLinked, nil
	default:
		return LinkAlreadyLinked, nil
	}
}

// HasObeyMarker reports whether dir carries Obey's ownership marker, read the
// way Obey reads it: key: value lines naming obey and a skills projection.
func HasObeyMarker(dir string) bool {
	f, err := os.Open(filepath.Join(dir, ObeyManagedMarkerFile))
	if err != nil {
		return false
	}
	data, readErr := io.ReadAll(io.LimitReader(f, 4096))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil {
		return false
	}
	return obeyMarkerValid(string(data))
}

func obeyMarkerValid(content string) bool {
	fields := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return fields["managed_by"] == "obey" && fields["kind"] == "skills_projection"
}
