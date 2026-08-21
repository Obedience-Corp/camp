package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	campgit "github.com/Obedience-Corp/camp/internal/git"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// loadProjectRenameMap returns old projects/<name> → new projects/<name> for
// directory renames git recorded under projects/, excluding worktrees. Destinations
// that no longer exist on disk are dropped so --fix never writes a second miss.
// Not-a-repo and git failures yield an empty map: doctor still warns, it does not guess.
func loadProjectRenameMap(ctx context.Context, root string) map[string]string {
	if err := ctx.Err(); err != nil {
		return nil
	}
	out, err := campgit.RunGitCmd(ctx, root,
		"log", "-M", "--diff-filter=R", "--name-status", "--pretty=format:", "--", "projects/")
	if err != nil {
		return nil
	}
	return parseProjectRenames(out, func(p string) bool {
		_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		return statErr == nil
	})
}

// parseProjectRenames reads `git log --name-status` rename lines (R100\told\tnew)
// and collapses file-level renames to projects/<name> directory mappings.
func parseProjectRenames(output string, destExists func(string) bool) map[string]string {
	proposed := map[string]string{}
	conflict := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != 'R' {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		from := projectRootPath(fields[1])
		to := projectRootPath(fields[2])
		if from == "" || to == "" || from == to {
			continue
		}
		if prev, ok := proposed[from]; ok && prev != to {
			conflict[from] = true
			continue
		}
		proposed[from] = to
	}
	out := map[string]string{}
	for from, to := range proposed {
		if conflict[from] {
			continue
		}
		if destExists != nil && !destExists(to) {
			continue
		}
		out[from] = to
	}
	return out
}

func projectRootPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 || parts[0] != "projects" || parts[1] == "" || parts[1] == "worktrees" {
		return ""
	}
	return "projects/" + parts[1]
}

func mappedProjectPath(oldPath, newRoot string) string {
	oldRoot := projectRootPath(oldPath)
	if oldRoot == "" {
		return newRoot
	}
	rest := strings.TrimPrefix(filepath.ToSlash(oldPath), oldRoot)
	return newRoot + rest
}

func rewriteWorkitemProjectPath(ctx context.Context, root, rel, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	marker := filepath.Join(root, filepath.FromSlash(rel), wkitem.MetadataFilename)
	if _, err := os.Stat(marker); err == nil {
		return rewriteDirectoryProjects(marker, from, to)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	return rewriteFrontmatterProjects(ctx, abs, from, to)
}

func rewriteDirectoryProjects(marker, from, to string) error {
	raw, err := os.ReadFile(marker)
	if err != nil {
		return camperrors.Wrapf(err, "reading %s", marker)
	}
	var meta wkitem.Metadata
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return camperrors.Wrapf(err, "parsing %s", marker)
	}
	next, ok := replaceProjectPath(meta.Projects, from, to)
	if !ok {
		return camperrors.NewValidation("projects",
			"workitem has no projects entry "+from+" to rewrite", nil)
	}
	meta.Projects = next
	out, err := yaml.Marshal(&meta)
	if err != nil {
		return camperrors.Wrap(err, "marshal updated .workitem")
	}
	return fsutil.WriteFileAtomically(marker, out, 0o644)
}

func rewriteFrontmatterProjects(ctx context.Context, abs, from, to string) error {
	existing, err := wkitem.LoadFrontmatterMetadata(abs)
	if err != nil {
		return camperrors.Wrapf(err, "reading frontmatter %s", abs)
	}
	if existing == nil {
		return camperrors.NewValidation("frontmatter",
			"no kind: workitem frontmatter to rewrite at "+abs, nil)
	}
	next, ok := replaceProjectPath(existing.Projects, from, to)
	if !ok {
		return camperrors.NewValidation("projects",
			"workitem has no projects entry "+from+" to rewrite", nil)
	}
	return wkitem.StampFrontmatterFields(ctx, abs,
		[]wkitem.FrontmatterField{{After: frontmatterProjectsAnchor(existing), Key: "projects", Values: next}})
}

// replaceProjectPath rewrites from→to and drops a resulting duplicate.
// ok is false when from was not present.
func replaceProjectPath(projects []string, from, to string) ([]string, bool) {
	found := false
	seen := make(map[string]bool, len(projects))
	out := make([]string, 0, len(projects))
	for _, p := range projects {
		next := p
		if p == from {
			next = to
			found = true
		}
		if seen[next] {
			continue
		}
		seen[next] = true
		out = append(out, next)
	}
	return out, found
}
