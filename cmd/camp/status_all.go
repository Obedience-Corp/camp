package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	statuspkg "github.com/Obedience-Corp/camp/internal/status"
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
and push status.

Examples:
  camp status all               # Show all submodule statuses
  camp status all --remote-url  # Show remote URLs instead of names
  camp status all --json        # Output as JSON`,
	RunE: jsoncontract.RunE(StatusAllJSONVersion, func() bool { return statusAllJSON }, runStatusAll),
}

const StatusAllJSONVersion = "status-all/v1alpha1"

var (
	statusAllJSON      bool
	statusAllView      bool
	statusAllNoRecurse bool
	statusAllRemoteURL bool
)

func init() {
	statusAllCmd.Flags().BoolVar(&statusAllJSON, "json", false, "Output as JSON")
	statusAllCmd.Flags().BoolVar(&statusAllView, "view", false, "Open interactive TUI viewer")
	statusAllCmd.Flags().BoolVar(&statusAllNoRecurse, "no-recurse", false, "Only list top-level submodules")
	statusAllCmd.Flags().BoolVar(&statusAllRemoteURL, "remote-url", false, "Show remote URLs instead of remote names")

	statusCmd.AddCommand(statusAllCmd)
	statusAllCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(StatusAllJSONVersion, func() bool { return statusAllJSON }))
}

type statusAllOutput struct {
	SchemaVersion string                 `json:"schema_version"`
	Timestamp     string                 `json:"timestamp"`
	CampaignRoot  string                 `json:"campaign_root,omitempty"`
	Repos         []statuspkg.RepoStatus `json:"repos"`
}

func runStatusAll(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	campRoot, err = pathutil.ResolveRoot(campRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolving campaign root")
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
		if statusAllJSON {
			return outputStatusJSON("", []statuspkg.RepoStatus{})
		}
		fmt.Fprintln(os.Stderr, ui.Info("No submodules found in this campaign"))
		return nil
	}

	statusOpts := statuspkg.Options{ShowRemoteURL: statusAllRemoteURL}
	statuses := statuspkg.Collect(ctx, campRoot, paths, statusOpts)

	rootStatus := statuspkg.GetRepoStatus(ctx, campRoot, "campaign root", true, statusOpts)
	rootStatus.Path = "."
	allStatuses := append([]statuspkg.RepoStatus{rootStatus}, statuses...)

	// Status cache write removed: no read path existed, and read-style polling
	// commands were dirtying git status by creating .campaign/cache files.

	// Output
	if statusAllJSON {
		return outputStatusJSON(campRoot, allStatuses)
	}

	if statusAllView {
		return runStatusTUI(campRoot, allStatuses)
	}

	renderStatusTable(allStatuses)
	return nil
}

func runStatusTUI(campRoot string, statuses []statuspkg.RepoStatus) error {
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

func renderStatusTable(statuses []statuspkg.RepoStatus) {
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

func outputStatusJSON(campaignRoot string, statuses []statuspkg.RepoStatus) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if statuses == nil {
		statuses = []statuspkg.RepoStatus{}
	}
	return enc.Encode(statusAllOutput{
		SchemaVersion: StatusAllJSONVersion,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		CampaignRoot:  campaignRoot,
		Repos:         statuses,
	})
}
