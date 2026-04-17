package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/git"
	tuistatus "github.com/Obedience-Corp/camp/internal/tui/status"
	"github.com/Obedience-Corp/camp/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

var statusAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Show git status of all submodules",
	Long: `Show a visual overview of git status for all submodules in the campaign.

Displays a table with each submodule's name, branch, clean/dirty state,
and push status. Results are cached for quick subsequent lookups.

Examples:
  camp status all               # Show all submodule statuses
  camp status all --remote-url  # Show remote URLs instead of names
  camp status all --json        # Output as JSON
  camp status all --no-cache    # Skip cache, refresh all`,
	RunE: runStatusAll,
}

var (
	statusAllJSON      bool
	statusAllNoCache   bool
	statusAllView      bool
	statusAllNoRecurse bool
	statusAllRemoteURL bool
)

func init() {
	statusAllCmd.Flags().BoolVar(&statusAllJSON, "json", false, "Output as JSON")
	statusAllCmd.Flags().BoolVar(&statusAllNoCache, "no-cache", false, "Skip cache and refresh")
	statusAllCmd.Flags().BoolVar(&statusAllView, "view", false, "Open interactive TUI viewer")
	statusAllCmd.Flags().BoolVar(&statusAllNoRecurse, "no-recurse", false, "Only list top-level submodules")
	statusAllCmd.Flags().BoolVar(&statusAllRemoteURL, "remote-url", false, "Show remote URLs instead of remote names")

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
	Unmerged    int    `json:"unmerged"`
	StaleRefs   int    `json:"stale_refs"`
	Remote      string `json:"remote"`
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
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Enumerate submodules (including nested monorepo submodules)
	var paths []string
	if statusAllNoRecurse {
		paths, err = git.ListSubmodulePathsFiltered(ctx, campRoot, "projects/")
	} else {
		paths, err = git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
	}
	if err != nil {
		return camperrors.Wrap(err, "failed to list submodules")
	}

	if len(paths) == 0 {
		fmt.Println(ui.Info("No submodules found in this campaign"))
		return nil
	}

	// Collect status for each
	statuses := collectStatuses(ctx, campRoot, paths)

	// Add campaign root itself (with ref filtering)
	rootStatus := getRepoStatus(ctx, campRoot, "campaign root", true, statusAllRemoteURL)
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
			Unmerged:  s.Unmerged,
			StaleRefs: s.StaleRefs,
			Clean:     s.Clean,
			Error:     s.Error,
		}
	}

	p := tea.NewProgram(tuistatus.New(repos), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return camperrors.Wrap(err, "TUI error")
	}
	return nil
}

func collectStatuses(ctx context.Context, campRoot string, paths []string) []repoStatus {
	statuses := make([]repoStatus, 0, len(paths))

	for _, p := range paths {
		fullPath := filepath.Join(campRoot, p)
		name := git.SubmoduleDisplayName(p)
		status := getRepoStatus(ctx, fullPath, name, false, statusAllRemoteURL)
		status.Path = p
		statuses = append(statuses, status)
	}

	return statuses
}

func getRepoStatus(ctx context.Context, repoPath, name string, isCampaignRoot bool, showRemoteURL bool) repoStatus {
	rs := repoStatus{
		Name: name,
		Path: repoPath,
	}

	// Get current branch
	branch, err := git.Output(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		rs.Error = "not a git repo"
		return rs
	}
	rs.Branch = branch

	// Get remote info
	if showRemoteURL {
		if remote, err := git.Output(ctx, repoPath, "remote", "get-url", "origin"); err == nil {
			rs.Remote = shortenRemoteURL(remote)
		}
	} else {
		if remote, err := git.Output(ctx, repoPath, "remote"); err == nil && remote != "" {
			names := strings.Split(remote, "\n")
			rs.Remote = strings.Join(names, ", ")
		}
	}

	// Get porcelain status — at campaign root, ignore submodule refs so they
	// don't pollute the dirty state.
	statusArgs := []string{"status", "--porcelain=v1"}
	if isCampaignRoot {
		statusArgs = append(statusArgs, "--ignore-submodules=all")
	}
	output, err := git.Output(ctx, repoPath, statusArgs...)
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

	// Count stale submodule refs at campaign root
	if isCampaignRoot {
		rs.StaleRefs = countStaleRefs(ctx, repoPath)
	}

	// Get ahead/behind — also determines if upstream tracking is configured
	abOutput, err := git.Output(ctx, repoPath, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err == nil {
		rs.HasUpstream = true
		parts := strings.Fields(abOutput)
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &rs.Ahead)
			fmt.Sscanf(parts[1], "%d", &rs.Behind)
		}
	}

	// Count unmerged branches
	rs.Unmerged = git.UnmergedBranchCount(ctx, repoPath)

	return rs
}

// countStaleRefs counts submodules whose checked-out commit differs from
// the commit recorded in the superproject index. Lines starting with '+' in
// `git submodule status` indicate such drift.
func countStaleRefs(ctx context.Context, repoPath string) int {
	output, err := git.Output(ctx, repoPath, "submodule", "status")
	if err != nil || output == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if len(line) > 0 && line[0] == '+' {
			count++
		}
	}
	return count
}

func shortenRemoteURL(url string) string {
	// Handle HTTPS: https://github.com/Org/repo.git → Org/repo
	url = strings.TrimSuffix(url, ".git")
	if strings.HasPrefix(url, "https://github.com/") {
		return strings.TrimPrefix(url, "https://github.com/")
	}
	// Handle SSH: git@github.com:Org/repo.git → Org/repo
	if strings.HasPrefix(url, "git@github.com:") {
		return strings.TrimPrefix(url, "git@github.com:")
	}
	return url
}

func renderStatusTable(statuses []repoStatus) {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)
	yellow := lipgloss.NewStyle().Foreground(ui.WarningColor)
	dim := lipgloss.NewStyle().Foreground(ui.DimColor)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.BrightColor)

	headers := []string{"NAME", "REMOTE", "BRANCH", "STATUS", "PUSH", "CHANGES", "REFS", "BRANCHES"}
	var rows [][]string

	for _, s := range statuses {
		if s.Error != "" {
			rows = append(rows, []string{s.Name, "", "", red.Render(s.Error), "", "", "", ""})
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
		var changeParts []string
		if s.Staged > 0 {
			changeParts = append(changeParts, green.Render(fmt.Sprintf("+%d", s.Staged)))
		}
		if s.Modified > 0 {
			changeParts = append(changeParts, red.Render(fmt.Sprintf("~%d", s.Modified)))
		}
		if s.Untracked > 0 {
			changeParts = append(changeParts, dim.Render(fmt.Sprintf("?%d", s.Untracked)))
		}
		changeStr := strings.Join(changeParts, " ")

		// Branch (truncate if needed)
		branch := s.Branch
		if len(branch) > 12 {
			branch = branch[:12] + "…"
		}

		// Stale refs
		var refsStr string
		if s.StaleRefs > 0 {
			refsStr = yellow.Render(fmt.Sprintf("%d stale", s.StaleRefs))
		} else {
			refsStr = dim.Render("-")
		}

		// Unmerged branches
		var branchesStr string
		if s.Unmerged > 0 {
			branchesStr = yellow.Render(fmt.Sprintf("%d unmerged", s.Unmerged))
		} else {
			branchesStr = dim.Render("-")
		}

		// Remote (truncate if needed)
		remote := s.Remote
		if len(remote) > 30 {
			remote = remote[:30] + "…"
		}

		rows = append(rows, []string{s.Name, remote, branch, statusStr, pushStr, changeStr, refsStr, branchesStr})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 1, 2: // REMOTE, BRANCH
				return lipgloss.NewStyle().Foreground(ui.DimColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Println(t)
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

	finalFile := filepath.Join(cacheDir, "status.json")
	if err := fsutil.WriteFileAtomically(finalFile, data, 0o644); err != nil {
		return
	}
}
