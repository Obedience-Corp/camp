package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	tuistatus "github.com/obediencecorp/camp/internal/tui/status"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var statusAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Show git status of all submodules",
	Long: `Show a visual overview of git status for all submodules in the campaign.

Displays a table with each submodule's name, branch, clean/dirty state,
and push status. Results are cached for quick subsequent lookups.

Examples:
  camp status all           # Show all submodule statuses
  camp status all --json    # Output as JSON
  camp status all --no-cache  # Skip cache, refresh all`,
	RunE: runStatusAll,
}

var (
	statusAllJSON    bool
	statusAllNoCache bool
	statusAllView    bool
)

func init() {
	statusAllCmd.Flags().BoolVar(&statusAllJSON, "json", false, "Output as JSON")
	statusAllCmd.Flags().BoolVar(&statusAllNoCache, "no-cache", false, "Skip cache and refresh")
	statusAllCmd.Flags().BoolVar(&statusAllView, "view", false, "Open interactive TUI viewer")

	statusCmd.AddCommand(statusAllCmd)
}

// repoStatus holds the status of a single repository.
type repoStatus struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Clean       bool   `json:"clean"`
	HasUpstream bool   `json:"has_upstream"`
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	Staged      int    `json:"staged"`
	Modified    int    `json:"modified"`
	Untracked   int    `json:"untracked"`
	Error       string `json:"error,omitempty"`
}

// statusAllCache is the JSON cache format.
type statusAllCache struct {
	Timestamp time.Time    `json:"timestamp"`
	Repos     []repoStatus `json:"repos"`
}

func runStatusAll(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Enumerate submodules
	paths, err := git.ListSubmodulePathsFiltered(ctx, campRoot, "projects/")
	if err != nil {
		return fmt.Errorf("failed to list submodules: %w", err)
	}

	if len(paths) == 0 {
		fmt.Println(ui.Info("No submodules found in this campaign"))
		return nil
	}

	// Collect status for each
	statuses := collectStatuses(ctx, campRoot, paths)

	// Add campaign root itself
	rootStatus := getRepoStatus(ctx, campRoot, "campaign root")
	allStatuses := append([]repoStatus{rootStatus}, statuses...)

	// Cache results
	writeStatusCache(campRoot, allStatuses)

	// Output
	if statusAllJSON {
		return outputStatusJSON(allStatuses)
	}

	if statusAllView {
		return runStatusTUI(campRoot, allStatuses)
	}

	renderStatusTable(allStatuses)
	return nil
}

func runStatusTUI(campRoot string, statuses []repoStatus) error {
	repos := make([]tuistatus.RepoInfo, len(statuses))
	for i, s := range statuses {
		path := s.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(campRoot, path)
		}
		repos[i] = tuistatus.RepoInfo{
			Name:      s.Name,
			Path:      path,
			Branch:    s.Branch,
			Staged:    s.Staged,
			Modified:  s.Modified,
			Untracked: s.Untracked,
			Ahead:     s.Ahead,
			Behind:    s.Behind,
			Clean:     s.Clean,
			Error:     s.Error,
		}
	}

	p := tea.NewProgram(tuistatus.New(repos), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

func collectStatuses(ctx context.Context, campRoot string, paths []string) []repoStatus {
	statuses := make([]repoStatus, 0, len(paths))

	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		name := filepath.Base(p)
		status := getRepoStatus(ctx, fullPath, name)
		status.Path = p
		statuses = append(statuses, status)
	}

	return statuses
}

func getRepoStatus(ctx context.Context, repoPath, name string) repoStatus {
	rs := repoStatus{
		Name: name,
		Path: repoPath,
	}

	// Get current branch
	branch, err := gitOutput(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		rs.Error = "not a git repo"
		return rs
	}
	rs.Branch = branch

	// Get porcelain status
	output, err := gitOutput(ctx, repoPath, "status", "--porcelain=v1")
	if err != nil {
		rs.Error = "status failed"
		return rs
	}

	rs.Clean = output == ""
	if !rs.Clean {
		for _, line := range strings.Split(output, "\n") {
			if len(line) < 2 {
				continue
			}
			x, y := line[0], line[1]
			if x != ' ' && x != '?' {
				rs.Staged++
			}
			if y != ' ' && y != '?' {
				rs.Modified++
			}
			if x == '?' && y == '?' {
				rs.Untracked++
			}
		}
	}

	// Get ahead/behind — also determines if upstream tracking is configured
	abOutput, err := gitOutput(ctx, repoPath, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err == nil {
		rs.HasUpstream = true
		parts := strings.Fields(abOutput)
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &rs.Ahead)
			fmt.Sscanf(parts[1], "%d", &rs.Behind)
		}
	}

	return rs
}

func gitOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func renderStatusTable(statuses []repoStatus) {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)
	yellow := lipgloss.NewStyle().Foreground(ui.WarningColor)
	dim := lipgloss.NewStyle().Foreground(ui.DimColor)
	header := lipgloss.NewStyle().Foreground(ui.BrightColor).Bold(true)

	// Header
	fmt.Printf("  %-20s %-14s %-8s %-10s %s\n",
		header.Render("Name"),
		header.Render("Branch"),
		header.Render("Status"),
		header.Render("Push"),
		header.Render("Changes"))
	fmt.Printf("  %s\n", dim.Render(strings.Repeat("─", 70)))

	for _, s := range statuses {
		if s.Error != "" {
			fmt.Printf("  %-20s %s\n", s.Name, red.Render(s.Error))
			continue
		}

		// Status indicator
		statusStr := green.Render("clean")
		if !s.Clean {
			statusStr = red.Render("dirty")
		}

		// Push status
		var pushStr string
		if !s.HasUpstream {
			pushStr = red.Render("no track")
		} else if s.Ahead > 0 && s.Behind > 0 {
			pushStr = yellow.Render(fmt.Sprintf("↑%d ↓%d", s.Ahead, s.Behind))
		} else if s.Ahead > 0 {
			pushStr = yellow.Render(fmt.Sprintf("↑%d", s.Ahead))
		} else if s.Behind > 0 {
			pushStr = yellow.Render(fmt.Sprintf("↓%d", s.Behind))
		} else {
			pushStr = green.Render("ok")
		}

		// Changes detail
		var changes []string
		if s.Staged > 0 {
			changes = append(changes, green.Render(fmt.Sprintf("+%d", s.Staged)))
		}
		if s.Modified > 0 {
			changes = append(changes, red.Render(fmt.Sprintf("~%d", s.Modified)))
		}
		if s.Untracked > 0 {
			changes = append(changes, dim.Render(fmt.Sprintf("?%d", s.Untracked)))
		}
		changeStr := ""
		if len(changes) > 0 {
			changeStr = strings.Join(changes, " ")
		}

		// Branch (truncate if needed)
		branch := s.Branch
		if len(branch) > 12 {
			branch = branch[:12] + "…"
		}

		fmt.Printf("  %-20s %-14s %-8s %-10s %s\n",
			s.Name, dim.Render(branch), statusStr, pushStr, changeStr)
	}

	fmt.Println()
}

func outputStatusJSON(statuses []repoStatus) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(statuses)
}

func writeStatusCache(campRoot string, statuses []repoStatus) {
	cacheDir := filepath.Join(campRoot, ".campaign", "cache", "gitstatus")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}

	cache := statusAllCache{
		Timestamp: time.Now(),
		Repos:     statuses,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}

	// Atomic write
	tmpFile := filepath.Join(cacheDir, "status.json.tmp")
	finalFile := filepath.Join(cacheDir, "status.json")
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return
	}
	os.Rename(tmpFile, finalFile)
}
