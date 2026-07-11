package workitem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// workflowDirCandidate is a directory under workflow/ that validate and repair
// treat as a work item. RelPath is campaign-relative in slash form; PathType is
// the workflow type inferred from the path segment after the workflow root.
type workflowDirCandidate struct {
	RelPath  string
	PathType string
}

// parseWorkflowTarget normalizes an operator-supplied target (absolute or
// relative) into the campaign-relative work item directory it names and the
// workflow type inferred from its path. It refuses anything that is not of the
// form workflow/<type>/<dir>, satisfying the "refuse ambiguous paths" contract.
func parseWorkflowTarget(root string, resolver *paths.Resolver, target string) (relPath, pathType string, err error) {
	if strings.TrimSpace(target) == "" {
		return "", "", camperrors.NewValidation("path", "target path is required", nil)
	}
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, target)
	}
	rel, rerr := filepath.Rel(root, abs)
	if rerr != nil {
		return "", "", camperrors.Wrap(rerr, "resolve target relative to campaign root")
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "", camperrors.NewValidation("path", "target must be inside the campaign root: "+target, nil)
	}

	prefix := filepath.ToSlash(filepath.Clean(resolver.RelativeWorkflow())) + "/"
	if !strings.HasPrefix(rel, prefix) {
		return "", "", camperrors.NewValidation("path",
			"target must be a workflow work item directory of the form "+prefix+"<type>/<dir>; got "+rel, nil)
	}
	sub := strings.Split(strings.TrimPrefix(rel, prefix), "/")
	if len(sub) != 2 || sub[0] == "" || sub[1] == "" {
		return "", "", camperrors.NewValidation("path",
			"ambiguous target; expected exactly "+prefix+"<type>/<dir>, got "+rel, nil)
	}
	pathType = sub[0]
	if pathType == "dungeon" || wkitem.IsBuiltinType(wkitem.WorkflowType(pathType)) && !wkitem.IsBuiltinDocType(wkitem.WorkflowType(pathType)) {
		return "", "", camperrors.NewValidation("path", pathType+" directories are not workflow work items", nil)
	}
	return rel, pathType, nil
}

// scanWorkflowWorkitemDirs enumerates work item directories under workflow/,
// mirroring discovery semantics: builtin doc types (design, explore) treat every
// child directory as a work item, custom types surface only marker-bearing
// children, and intent/festival/dungeon control areas are skipped.
func scanWorkflowWorkitemDirs(ctx context.Context, root string, resolver *paths.Resolver) ([]workflowDirCandidate, error) {
	workflowRoot := resolver.Workflow()
	typeEntries, err := os.ReadDir(workflowRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", workflowRoot)
	}

	var out []workflowDirCandidate
	for _, typeEntry := range typeEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		typeName := typeEntry.Name()
		if !typeEntry.IsDir() || strings.HasPrefix(typeName, ".") || typeName == "dungeon" {
			continue
		}
		wt := wkitem.WorkflowType(typeName)
		if wkitem.IsBuiltinType(wt) && !wkitem.IsBuiltinDocType(wt) {
			continue
		}
		docType := wkitem.IsBuiltinDocType(wt)

		typeDir := filepath.Join(workflowRoot, typeName)
		children, err := os.ReadDir(typeDir)
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading %s", typeDir)
		}
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			childName := child.Name()
			if !child.IsDir() || strings.HasPrefix(childName, ".") || childName == "dungeon" {
				continue
			}
			childAbs := filepath.Join(typeDir, childName)
			if !docType {
				if _, statErr := os.Stat(filepath.Join(childAbs, wkitem.MetadataFilename)); statErr != nil {
					continue
				}
			}
			rel, rerr := filepath.Rel(root, childAbs)
			if rerr != nil {
				continue
			}
			out = append(out, workflowDirCandidate{RelPath: filepath.ToSlash(rel), PathType: typeName})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}
