package links

import (
	"path/filepath"
	"strings"
)

// Default campaign-relative directories a scope path is read against. They
// mirror config.DefaultCampaignPaths and are the fallback for a caller that
// has no loaded campaign config.
const (
	DefaultProjectsDir  = "projects/"
	DefaultWorktreesDir = "projects/worktrees/"
)

// ScopeLayout names the campaign-relative directories a scope path is read
// against. It is injected rather than read from disk, so this package stays
// free of configuration I/O: command code passes the campaign's configured
// paths and every other caller gets the camp defaults from the zero value.
type ScopeLayout struct {
	ProjectsDir  string
	WorktreesDir string
}

// LayoutFor builds a ScopeLayout from configured campaign paths. An empty
// value falls back to the camp default, so LayoutFor("", "") is the default
// layout.
func LayoutFor(projectsDir, worktreesDir string) ScopeLayout {
	return ScopeLayout{
		ProjectsDir:  normalizeDir(projectsDir, DefaultProjectsDir),
		WorktreesDir: normalizeDir(worktreesDir, DefaultWorktreesDir),
	}
}

func normalizeDir(value, fallback string) string {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(value)), "/")
	if trimmed == "" {
		return fallback
	}
	return trimmed + "/"
}

func (l ScopeLayout) projects() string {
	if l.ProjectsDir == "" {
		return DefaultProjectsDir
	}
	return l.ProjectsDir
}

func (l ScopeLayout) worktrees() string {
	if l.WorktreesDir == "" {
		return DefaultWorktreesDir
	}
	return l.WorktreesDir
}

// ProjectPath returns the campaign-relative path of the project named name, or
// "" when name is not a single usable path segment.
func (l ScopeLayout) ProjectPath(name string) string {
	name = strings.Trim(filepath.ToSlash(name), "/")
	if !usableSegment(name) || strings.Contains(name, "/") {
		return ""
	}
	return l.projects() + name
}

// ProjectsDirPath and WorktreesDirPath return the configured directories with a
// trailing slash, for callers that build a path rather than test one. The zero
// layout returns the camp defaults.
func (l ScopeLayout) ProjectsDirPath() string { return l.projects() }

// WorktreesDirPath is the worktrees half of ProjectsDirPath.
func (l ScopeLayout) WorktreesDirPath() string { return l.worktrees() }

// UnderProjects reports whether a campaign-relative path sits inside the
// projects directory.
func (l ScopeLayout) UnderProjects(path string) bool {
	_, ok := underDir(path, l.projects())
	return ok
}

// UnderWorktrees reports whether a campaign-relative path sits inside the
// worktrees directory.
func (l ScopeLayout) UnderWorktrees(path string) bool {
	_, ok := underDir(path, l.worktrees())
	return ok
}

// ProjectName is the inverse of ProjectPath: it returns the project's name for
// a campaign-relative projects/<name> path, or "" for anything else.
func (l ScopeLayout) ProjectName(projectPath string) string {
	rest, ok := underDir(projectPath, l.projects())
	if !ok || strings.Contains(rest, "/") || !usableSegment(rest) {
		return ""
	}
	return rest
}

// SplitWorktreePath splits a worktree path of the form
// <worktrees>/<project>/<name>[/...] into its project and worktree names. Both
// are empty when the path is not under the worktrees directory or does not
// carry both segments.
func (l ScopeLayout) SplitWorktreePath(worktreePath string) (project, name string) {
	rest, ok := underDir(worktreePath, l.worktrees())
	if !ok {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || !usableSegment(parts[0]) || !usableSegment(parts[1]) {
		return "", ""
	}
	return parts[0], parts[1]
}

// WorktreeProject returns the campaign-relative project path that owns the
// worktree at worktreePath, or "" when the path does not name one.
func (l ScopeLayout) WorktreeProject(worktreePath string) string {
	project, _ := l.SplitWorktreePath(worktreePath)
	return l.ProjectPath(project)
}

// ProjectRoot returns the campaign-relative project path containing a path
// under the projects directory, so a deeper scope such as
// projects/camp/internal still answers projects/camp. Worktree paths and paths
// outside the projects directory return "".
func (l ScopeLayout) ProjectRoot(path string) string {
	if _, under := underDir(path, l.worktrees()); under {
		return ""
	}
	rest, ok := underDir(path, l.projects())
	if !ok {
		return ""
	}
	candidate := l.ProjectPath(strings.SplitN(rest, "/", 2)[0])
	// The worktrees holder can sit inside the projects directory, which is the
	// camp default. It is a container for checkouts, never a project itself.
	if candidate == "" || candidate+"/" == l.worktrees() {
		return ""
	}
	return candidate
}

// underDir reports whether path sits inside dir (which ends in "/") and
// returns the remainder with no surrounding separators.
func underDir(path, dir string) (string, bool) {
	clean := strings.Trim(filepath.ToSlash(path), "/")
	if clean == "" {
		return "", false
	}
	if !strings.HasPrefix(clean+"/", dir) {
		return "", false
	}
	rest := strings.Trim(strings.TrimPrefix(clean+"/", dir), "/")
	if rest == "" {
		return "", false
	}
	return rest, true
}

func usableSegment(segment string) bool {
	return segment != "" && segment != "." && segment != ".."
}

// ProjectFor returns the campaign-relative project path a scope belongs to.
//
// A worktree is a checkout of a project, not a project of its own, so a
// worktree scope answers with the project that owns it: the recorded project
// when the row has one, and otherwise the project derived from the
// <worktrees>/<project>/<name> path convention. The derivation is what keeps a
// registry written before the project field existed readable, and it is why
// removing the worktree directory does not cost the workitem its project.
//
// Scopes that name no project return "".
func (s LinkScope) ProjectFor(layout ScopeLayout) string {
	// The recorded project is only consulted on the kinds allowed to carry one.
	// links.Load does not validate, so a hand-edited or foreign registry can put
	// a project on a festival or campaign_path row; honouring it there would
	// show a thing that is not a project as one, which is the bug this whole
	// change exists to fix, pointing the other way.
	switch s.Kind {
	case ScopeProject, ScopeRepo, ScopeWorktree:
		if s.Project != "" {
			return s.Project
		}
	default:
		return ""
	}
	switch s.Kind {
	case ScopeProject:
		return layout.ProjectRoot(s.Path)
	case ScopeWorktree:
		return layout.WorktreeProject(s.Path)
	}
	return ""
}

// WorktreeName returns the worktree's own name for a worktree scope, or "".
func (s LinkScope) WorktreeName(layout ScopeLayout) string {
	if s.Kind != ScopeWorktree {
		return ""
	}
	_, name := layout.SplitWorktreePath(s.Path)
	return name
}

// NormalizeScope fills the derived fields a writer can compute for itself. A
// worktree scope gains the project that owns it, so the workitem stays attached
// to that project once the worktree is removed. A caller that already knows the
// project, as worktree creation does, keeps the value it passed.
func NormalizeScope(layout ScopeLayout, scope LinkScope) LinkScope {
	if scope.Kind == ScopeWorktree && scope.Project == "" {
		scope.Project = layout.WorktreeProject(scope.Path)
	}
	return scope
}

// BackfillProjects records the owning project on every worktree scope that has
// none and whose path names one. It returns the ids of the rows it changed so a
// caller can report exactly what it wrote, and it never overwrites a project
// already on a row.
func BackfillProjects(layout ScopeLayout, l *Links) []string {
	if l == nil {
		return nil
	}
	var changed []string
	for i := range l.Links {
		scope := l.Links[i].Scope
		if scope.Kind != ScopeWorktree || scope.Project != "" {
			continue
		}
		project := layout.WorktreeProject(scope.Path)
		if project == "" {
			continue
		}
		l.Links[i].Scope.Project = project
		changed = append(changed, l.Links[i].ID)
	}
	return changed
}
