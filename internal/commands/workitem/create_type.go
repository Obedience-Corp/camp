package workitem

import (
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

const createWorkflowRoot = "workflow"

// createPlacement is the type and camp-relative parent directory of a new
// workitem. From names the workflow/<type> directory the type was inferred
// from and is empty when the type came from --type or the default.
type createPlacement struct {
	Type   string
	From   string
	Parent string
}

// planCreateDir places a directory workitem. An explicit --type wins and keeps
// workflow/<type> (or --dir). Otherwise the type is inferred from --dir when
// given, else from the camp-relative cwd, so creating from anywhere under
// workflow/<type>/ makes a sibling in workflow/<type>/.
func planCreateDir(typeFlag string, typeExplicit bool, dirOverride, cwdRel string) createPlacement {
	location := cwdRel
	if dirOverride != "" {
		location = dirOverride
	}
	typ, from := resolveCreateType(typeFlag, typeExplicit, location)
	parent := createWorkflowRoot + "/" + typ
	if dirOverride != "" {
		parent = filepath.ToSlash(filepath.Clean(dirOverride))
	}
	return createPlacement{Type: typ, From: from, Parent: parent}
}

// planCreateFile places a file workitem: the type is inferred from the
// directory of the camp-relative target file unless --type was given.
func planCreateFile(typeFlag string, typeExplicit bool, fileRel string) createPlacement {
	parent := filepath.ToSlash(filepath.Dir(filepath.Clean(fileRel)))
	typ, from := resolveCreateType(typeFlag, typeExplicit, parent)
	return createPlacement{Type: typ, From: from, Parent: parent}
}

func resolveCreateType(typeFlag string, typeExplicit bool, location string) (typ, from string) {
	if typeExplicit {
		return typeFlag, ""
	}
	if seg := workflowTypeSegment(location); seg != "" {
		return seg, createWorkflowRoot + "/" + seg
	}
	return typeFlag, ""
}

// workflowTypeSegment returns <type> when dir is workflow/<type> or below and
// <type> is a usable type name, and "" otherwise.
func workflowTypeSegment(dir string) string {
	if dir == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(dir)), "/")
	if len(parts) < 2 || parts[0] != createWorkflowRoot {
		return ""
	}
	seg := parts[1]
	if validateSlug(seg) != nil || spelling.IsDungeonName(seg) {
		return ""
	}
	return seg
}

// campRelativeCwd returns the shell's logical cwd relative to the camp root.
// Symlinks below the root are kept as the user walked them, so a linked
// directory under workflow/<type>/ still counts as that type and cd guidance
// matches the shell's logical path; a symlinked root still matches. ok is
// false when the cwd is outside the camp.
func campRelativeCwd(campaignRoot string) (rel string, ok bool) {
	cwd, err := pathutil.LogicalCwd()
	if err != nil {
		return "", false
	}
	root, err := pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		root = campaignRoot
	}
	if logical, found := pathutil.LogicalRelativeToRoot(root, cwd); found && !escapesRoot(logical) {
		return filepath.ToSlash(logical), true
	}
	rel, err = pathutil.RelativeToRoot(root, cwd)
	if err != nil || escapesRoot(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// cdTargetFromCwd rewrites a camp-relative path so it can be passed to cd
// from the user's cwd. It falls back to the camp-relative path when the cwd
// is outside the camp.
func cdTargetFromCwd(cwdRel string, cwdInCamp bool, rel string) string {
	if !cwdInCamp {
		return rel
	}
	target, err := filepath.Rel(filepath.FromSlash(cwdRel), filepath.FromSlash(rel))
	if err != nil {
		return rel
	}
	return filepath.ToSlash(target)
}
