package worktrees

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// WorktreesListJSONVersion is bumped to v1alpha2 because git-derived
// enumeration changed the meaning of the emitted path and name fields: path is
// now the worktree's real on-disk location (which may be outside the
// conventional projects/worktrees/<project>/ layout), and name is
// disambiguated to a campaign-relative path when two linked worktrees share a
// basename. Clients can key on this version to detect the new semantics.
const WorktreesListJSONVersion = "worktrees-list/v1alpha2"

var (
	listProject string
	listStale   bool
	listJSON    bool
)

var worktreesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees for the current project or campaign",
	Long: `List worktrees in the campaign, organized by project.

When run from a project checkout or one of its git worktrees, only that
project's worktrees are shown. Outside a project context, all worktrees are
shown unless --project is supplied.

Examples:
  # List worktrees for the current project
  camp worktrees list

  # List all worktrees from the campaign root
  cd /path/to/campaign && camp worktrees list

  # Filter from outside a project
  camp worktrees list --project my-api

  # Show only stale worktrees
  camp worktrees list --stale

  # JSON output for scripting
  camp worktrees list --json`,
	Aliases: []string{"ls"},
	RunE:    jsoncontract.RunE(WorktreesListJSONVersion, func() bool { return listJSON }, runWorktreesList),
}

func init() {
	Cmd.AddCommand(worktreesListCmd)

	worktreesListCmd.Flags().StringVarP(&listProject, "project", "p", "",
		"Filter by project name (overrides automatic project detection)")
	worktreesListCmd.Flags().BoolVar(&listStale, "stale", false,
		"Show only stale worktrees")
	worktreesListCmd.Flags().BoolVar(&listJSON, "json", false,
		"Output as JSON")
	worktreesListCmd.Flags().BoolP("interactive", "i", false,
		"Open the interactive worktree browser (prints the table when stdout is not a terminal)")
	worktreesListCmd.Flags().String("path-output", "", "Write the selected worktree path to a file (shell integration)")
	_ = worktreesListCmd.Flags().MarkHidden("path-output")
	worktreesListCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorktreesListJSONVersion, func() bool { return listJSON }))
}

// WorktreeListItem contains information about a worktree for listing.
type WorktreeListItem struct {
	Project      string `json:"project"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Branch       string `json:"branch"`
	LastAccessed string `json:"lastAccessed"`
	Stale        bool   `json:"stale"`
	StaleReason  string `json:"staleReason,omitempty"`
}

// WorktreeListResult contains the list operation result.
type WorktreeListResult struct {
	Worktrees  []WorktreeListItem `json:"worktrees"`
	Total      int                `json:"total"`
	StaleCount int                `json:"stale"`
}

func runWorktreesList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// In a terminal, a bare `camp worktrees list` opens the interactive browser
	// (the same pattern as `camp list`). The browser holds every worktree and
	// applies --stale as a live, toggleable filter, so load unfiltered for it;
	// --json and the shaping flags still print the table/JSON.
	openTUI := worktreesListTUIRequested(cmd, stdoutIsTTY())
	loadStale := listStale
	if openTUI {
		loadStale = false
	}

	result, err := listWorktrees(ctx, campRoot, listProject, loadStale)
	if err != nil {
		return err
	}

	if openTUI {
		return runWorktreesListTUI(cmd, result)
	}

	if listJSON {
		return outputListJSON(result)
	}

	return outputListTable(result)
}

func listWorktrees(ctx context.Context, campRoot string, filterProject string, staleOnly bool) (*WorktreeListResult, error) {
	var allWorktrees []WorktreeListItem

	projectTargets, err := listWorktreeProjectTargets(ctx, campRoot, filterProject)
	if err != nil {
		return nil, err
	}

	// Scan each project. git worktree list is the source of truth: it finds
	// every linked worktree regardless of where it lives on disk, not just
	// those under the conventional projects/worktrees/<project>/ layout.
	for _, target := range projectTargets {
		gitEntries, err := worktree.NewGitWorktree(target.path).List(ctx)
		if err != nil {
			if filterProject != "" {
				return nil, camperrors.Wrapf(err, "failed to list git worktrees for project %q", target.name)
			}
			continue
		}

		projectName := target.name
		for _, entry := range gitEntries {
			if !worktree.IsLinkedWorktree(target.path, entry) {
				continue
			}
			name := filepath.Base(filepath.Clean(entry.Path))
			info := buildWorktreeListItem(projectName, name, entry.Path)
			allWorktrees = append(allWorktrees, info)
		}
	}

	// git-derived enumeration can surface two linked worktrees for the same
	// project whose directory basenames match (a preferred
	// projects/worktrees/<project>/foo and a loose /elsewhere/foo). Basename
	// alone would collapse them to one name in the table/JSON, so rewrite
	// colliding names to a unique, path-derived form.
	disambiguateWorktreeNames(campRoot, allWorktrees)

	// Filter if needed
	var result []WorktreeListItem
	staleCount := 0
	for _, wt := range allWorktrees {
		if wt.Stale {
			staleCount++
		}
		if staleOnly && !wt.Stale {
			continue
		}
		result = append(result, wt)
	}

	return &WorktreeListResult{
		Worktrees:  result,
		Total:      len(allWorktrees),
		StaleCount: staleCount,
	}, nil
}

// listWorktreeProjectTargets resolves the scan scope for the list command.
// An explicit --project always wins. Without it, a project checkout or one of
// its linked worktrees scopes the list to that project; otherwise the command
// retains its campaign-wide behavior.
func listWorktreeProjectTargets(ctx context.Context, campRoot, filterProject string) ([]worktreeProjectTarget, error) {
	if filterProject != "" {
		return worktreeProjectTargets(ctx, campRoot, filterProject)
	}

	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list projects")
	}

	cwd, err := os.Getwd()
	if err == nil {
		if resolvedCwd, resolveErr := filepath.EvalSymlinks(cwd); resolveErr == nil {
			cwd = resolvedCwd
		}
	}

	// Match registered project paths before consulting git worktree entries.
	// This avoids generic project resolution fallbacks (for example, treating a
	// submodule worktree as an unregistered projects/worktrees project).
	var (
		match     *worktreeProjectTarget
		matchPath string
	)
	for _, proj := range projects {
		target := worktreeProjectTarget{
			name: proj.Name,
			path: project.ResolveProjectPath(campRoot, proj),
		}
		logicalPath := filepath.Join(campRoot, proj.Path)
		if pathWithin(cwd, target.path) || pathWithin(cwd, logicalPath) {
			if match == nil || len(target.path) > len(matchPath) {
				candidate := target
				match = &candidate
				matchPath = target.path
			}
		}
	}
	if match != nil {
		return []worktreeProjectTarget{*match}, nil
	}

	// A linked git worktree lives beside the registered project path rather
	// than underneath it. Match the current directory against git's worktree
	// entries so projects/worktrees/<project>/<name> remains project-aware too.
	for _, proj := range projects {
		target := worktreeProjectTarget{
			name: proj.Name,
			path: project.ResolveProjectPath(campRoot, proj),
		}
		entries, listErr := worktree.NewGitWorktree(target.path).List(ctx)
		if listErr != nil {
			continue
		}
		for _, entry := range entries {
			entryPath := filepath.Clean(entry.Path)
			if entryPath == "" || !pathWithin(cwd, entryPath) {
				continue
			}
			if match == nil || len(entryPath) > len(matchPath) {
				candidate := target
				match = &candidate
				matchPath = entryPath
			}
		}
	}
	if match != nil {
		return []worktreeProjectTarget{*match}, nil
	}

	return worktreeProjectTargets(ctx, campRoot, "")
}

func pathWithin(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	if child == parent {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// worktreeProjectTarget is a registered campaign project used as a scan root
// for worktree list/clean enumeration.
type worktreeProjectTarget struct {
	name string
	path string
}

// worktreeProjectTargets resolves the set of projects whose git worktrees
// should be enumerated. Shared by list and clean so the project set cannot
// drift between commands (how clean lagged list after git-as-source-of-truth).
func worktreeProjectTargets(ctx context.Context, campRoot, filterProject string) ([]worktreeProjectTarget, error) {
	if filterProject != "" {
		resolved, err := project.Resolve(ctx, campRoot, filterProject)
		if err != nil {
			return nil, err
		}
		return []worktreeProjectTarget{{
			name: resolved.Name,
			path: resolved.Path,
		}}, nil
	}

	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list projects")
	}

	targets := make([]worktreeProjectTarget, 0, len(projects))
	for _, proj := range projects {
		targets = append(targets, worktreeProjectTarget{
			name: proj.Name,
			path: project.ResolveProjectPath(campRoot, proj),
		})
	}
	return targets, nil
}

// disambiguateWorktreeNames rewrites the Name of any worktrees that share a
// (project, basename) so every table row and JSON entry stays uniquely
// identifiable. A colliding name becomes the campaign-relative path (or the
// cleaned absolute path when the worktree lives outside the campaign tree),
// which is unique because git worktree paths are unique.
func disambiguateWorktreeNames(campRoot string, worktrees []WorktreeListItem) {
	counts := make(map[string]int, len(worktrees))
	for _, wt := range worktrees {
		counts[wt.Project+"\x00"+wt.Name]++
	}
	for i := range worktrees {
		if counts[worktrees[i].Project+"\x00"+worktrees[i].Name] > 1 {
			worktrees[i].Name = worktreeUniqueName(campRoot, worktrees[i].Path)
		}
	}
}

// worktreeUniqueName returns a stable, unique display name for a worktree whose
// basename collides with another: the campaign-relative path when the worktree
// is inside the campaign, otherwise the cleaned absolute path.
func worktreeUniqueName(campRoot, path string) string {
	clean := filepath.Clean(path)
	if campRoot != "" {
		if rel, err := filepath.Rel(campRoot, clean); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return clean
}

func buildWorktreeListItem(project, name, path string) WorktreeListItem {
	info := WorktreeListItem{
		Project: project,
		Name:    name,
		Path:    path,
	}

	// Check if worktree is valid by looking for .git file
	gitPath := path + "/.git"
	fileInfo, err := os.Stat(gitPath)
	if err != nil {
		info.Stale = true
		info.StaleReason = "missing .git"
		info.Branch = "unknown"
	} else if fileInfo.IsDir() {
		info.Stale = true
		info.StaleReason = "not a worktree (.git is directory)"
		info.Branch = "unknown"
	} else {
		// Read branch from HEAD
		info.Branch = readWorktreeBranch(path)
	}

	// Get last accessed time from directory
	if stat, err := os.Stat(path); err == nil {
		info.LastAccessed = formatTimeAgo(stat.ModTime())
	}

	return info
}

func readWorktreeBranch(wtPath string) string {
	gitPath := wtPath + "/.git"
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "unknown"
	}

	// Parse gitdir: path
	line := string(content)
	if !startsWithGitdir(line) {
		return "unknown"
	}

	gitdir := extractGitdir(line)
	headPath := gitdir + "/HEAD"
	headContent, err := os.ReadFile(headPath)
	if err != nil {
		return "unknown"
	}

	ref := string(headContent)
	if hasRefPrefix(ref) {
		return extractBranchName(ref)
	}

	// Detached HEAD
	if len(ref) >= 7 {
		return ref[:7] + " (detached)"
	}
	return "HEAD"
}

func startsWithGitdir(s string) bool {
	return len(s) > 8 && s[:8] == "gitdir: "
}

func extractGitdir(s string) string {
	return trimSpace(s[8:])
}

func hasRefPrefix(s string) bool {
	return len(s) > 16 && s[:16] == "ref: refs/heads/"
}

func extractBranchName(s string) string {
	return trimSpace(s[16:])
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	if d < 7*24*time.Hour {
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	return t.Format("Jan 2, 2006")
}

func outputListJSON(result *WorktreeListResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func outputListTable(result *WorktreeListResult) error {
	if len(result.Worktrees) == 0 {
		fmt.Println("No worktrees found")
		return nil
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	staleStyle := lipgloss.NewStyle().Foreground(ui.WarningColor)

	headers := []string{"PROJECT", "NAME", "BRANCH", "LAST ACCESSED", "STATUS"}
	rows := make([][]string, 0, len(result.Worktrees))

	for _, wt := range result.Worktrees {
		status := ""
		if wt.Stale {
			status = staleStyle.Render("stale")
			if wt.StaleReason != "" {
				status = staleStyle.Render("stale: " + wt.StaleReason)
			}
		}
		rows = append(rows, []string{
			wt.Project,
			wt.Name,
			wt.Branch,
			wt.LastAccessed,
			status,
		})
	}

	t := table.New().
		Border(lipgloss.HiddenBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		})

	fmt.Println(t)
	summary := fmt.Sprintf("\n%d worktree(s)", result.Total)
	if result.StaleCount > 0 {
		summary += fmt.Sprintf(" (%d stale)", result.StaleCount)
	}
	fmt.Println(summary)
	return nil
}
