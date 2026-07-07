package worktrees

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

const WorktreesListJSONVersion = "worktrees-list/v1alpha1"

var (
	listProject string
	listStale   bool
	listJSON    bool
)

var worktreesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all worktrees",
	Long: `List all worktrees in the campaign, organized by project.

Examples:
  # List all worktrees
  camp worktrees list

  # List worktrees for specific project
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
		"Filter by project name")
	worktreesListCmd.Flags().BoolVar(&listStale, "stale", false,
		"Show only stale worktrees")
	worktreesListCmd.Flags().BoolVar(&listJSON, "json", false,
		"Output as JSON")
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

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	pathManager := worktree.NewPathManager(resolver)

	result, err := listWorktrees(ctx, campRoot, pathManager, listProject, listStale)
	if err != nil {
		return err
	}

	if listJSON {
		return outputListJSON(result)
	}

	return outputListTable(result)
}

type listProjectTarget struct {
	name string
	path string
}

func listWorktrees(ctx context.Context, campRoot string, pm *worktree.PathManager, filterProject string, staleOnly bool) (*WorktreeListResult, error) {
	var allWorktrees []WorktreeListItem

	projectTargets, err := listWorktreeProjectTargets(ctx, campRoot, filterProject)
	if err != nil {
		return nil, err
	}

	// Scan each project
	for _, target := range projectTargets {
		gitEntries, err := worktree.NewGitWorktree(target.path).List(ctx)
		if err != nil {
			if filterProject != "" {
				return nil, camperrors.Wrapf(err, "failed to list git worktrees for project %q", target.name)
			}
			continue
		}

		projectName := target.name
		fsWorktrees, err := pm.ListProjectWorktrees(projectName)
		if err != nil {
			continue // Skip projects with errors
		}

		for _, name := range filterRegisteredWorktreeNames(projectName, fsWorktrees, gitEntries, pm) {
			wtPath := pm.WorktreePath(projectName, name)
			info := buildWorktreeListItem(projectName, name, wtPath)
			allWorktrees = append(allWorktrees, info)
		}
	}

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

func listWorktreeProjectTargets(ctx context.Context, campRoot, filterProject string) ([]listProjectTarget, error) {
	if filterProject != "" {
		resolved, err := project.Resolve(ctx, campRoot, filterProject)
		if err != nil {
			return nil, err
		}
		return []listProjectTarget{{
			name: resolved.Name,
			path: resolved.Path,
		}}, nil
	}

	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list projects")
	}

	targets := make([]listProjectTarget, 0, len(projects))
	for _, proj := range projects {
		targets = append(targets, listProjectTarget{
			name: proj.Name,
			path: project.ResolveProjectPath(campRoot, proj),
		})
	}
	return targets, nil
}

func filterRegisteredWorktreeNames(projectName string, names []string, entries []worktree.GitWorktreeEntry, pm *worktree.PathManager) []string {
	registered := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		registered[filepath.Clean(entry.Path)] = struct{}{}
	}

	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := registered[filepath.Clean(pm.WorktreePath(projectName, name))]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
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
