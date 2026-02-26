package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

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
	RunE:    runWorktreesList,
}

func init() {
	worktreesCmd.AddCommand(worktreesListCmd)

	worktreesListCmd.Flags().StringVarP(&listProject, "project", "p", "",
		"Filter by project name")
	worktreesListCmd.Flags().BoolVar(&listStale, "stale", false,
		"Show only stale worktrees")
	worktreesListCmd.Flags().BoolVar(&listJSON, "json", false,
		"Output as JSON")
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
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return fmt.Errorf("failed to load campaign config: %w", err)
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	pathManager := worktree.NewPathManager(resolver)

	result, err := listWorktrees(ctx, pathManager, cfg, listProject, listStale)
	if err != nil {
		return err
	}

	if listJSON {
		return outputListJSON(result)
	}

	return outputListTable(result)
}

func listWorktrees(_ context.Context, pm *worktree.PathManager, _ *config.CampaignConfig, filterProject string, staleOnly bool) (*WorktreeListResult, error) {
	var allWorktrees []WorktreeListItem

	// Get projects to scan
	var projectNames []string
	if filterProject != "" {
		projectNames = []string{filterProject}
	} else {
		projects, err := pm.ListAllProjects()
		if err != nil {
			return nil, err
		}
		projectNames = projects
	}

	// Scan each project
	for _, projectName := range projectNames {
		fsWorktrees, err := pm.ListProjectWorktrees(projectName)
		if err != nil {
			continue // Skip projects with errors
		}

		for _, name := range fsWorktrees {
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	currentProject := ""
	for _, wt := range result.Worktrees {
		if wt.Project != currentProject {
			if currentProject != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s/\n", wt.Project)
			currentProject = wt.Project
		}

		status := ""
		if wt.Stale {
			status = " [stale]"
		}

		fmt.Fprintf(w, "  ├── %s\t%s\t%s%s\n",
			wt.Name,
			wt.Branch,
			wt.LastAccessed,
			status,
		)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Total: %d worktrees", result.Total)
	if result.StaleCount > 0 {
		fmt.Fprintf(w, " (%d stale)", result.StaleCount)
	}
	fmt.Fprintln(w)

	return w.Flush()
}
