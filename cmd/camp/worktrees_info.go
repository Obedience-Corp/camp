package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	infoPath string
	infoJSON bool
)

var worktreesInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show worktree information",
	Long: `Show information about the current worktree or a specified worktree.

When run without --path, automatically detects if you're inside a worktree.

Examples:
  # Show info for current worktree (when inside one)
  camp worktrees info

  # Show info for specific worktree
  camp worktrees info --path projects/worktrees/my-api/feature

  # JSON output
  camp worktrees info --json`,
	RunE: runWorktreesInfo,
}

func init() {
	worktreesCmd.AddCommand(worktreesInfoCmd)

	worktreesInfoCmd.Flags().StringVarP(&infoPath, "path", "p", "",
		"Worktree path (defaults to current directory)")
	worktreesInfoCmd.Flags().BoolVar(&infoJSON, "json", false,
		"Output as JSON")
}

// WorktreeInfoResult contains detailed worktree information.
type WorktreeInfoResult struct {
	Name       string            `json:"name"`
	Project    string            `json:"project"`
	Branch     string            `json:"branch"`
	Path       string            `json:"path"`
	Status     string            `json:"status"`
	Created    string            `json:"created,omitempty"`
	LastCommit *WorktreeCommitInfo `json:"lastCommit,omitempty"`
}

// WorktreeCommitInfo contains last commit information.
type WorktreeCommitInfo struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Time    string `json:"time"`
}

func runWorktreesInfo(cmd *cobra.Command, args []string) error {
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
	detector := worktree.NewDetector(resolver)

	// Detect context
	var wtCtx *worktree.Context
	if infoPath != "" {
		wtCtx, err = detector.DetectFromPath(infoPath)
	} else {
		wtCtx, err = detector.DetectFromCwd()
	}

	if err != nil {
		if infoPath == "" {
			return fmt.Errorf("not inside a worktree (use --path to specify)")
		}
		return fmt.Errorf("not a valid worktree: %s", infoPath)
	}

	// Build detailed info
	info := buildWorktreeDetailedInfo(ctx, wtCtx)

	if infoJSON {
		return outputInfoJSON(info)
	}

	return outputInfoPretty(info)
}

func buildWorktreeDetailedInfo(ctx context.Context, wtCtx *worktree.Context) *WorktreeInfoResult {
	info := &WorktreeInfoResult{
		Name:    wtCtx.WorktreeName,
		Project: wtCtx.Project,
		Branch:  wtCtx.Branch,
		Path:    wtCtx.WorktreePath,
	}

	// Get working directory status
	info.Status = getWorktreeWorkingStatus(ctx, wtCtx.WorktreePath)

	// Get creation time from directory
	if stat, err := os.Stat(wtCtx.WorktreePath); err == nil {
		info.Created = stat.ModTime().Format(time.RFC3339)
	}

	// Get last commit info
	info.LastCommit = getWorktreeLastCommit(ctx, wtCtx.WorktreePath)

	return info
}

func getWorktreeWorkingStatus(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	trimmed := strings.TrimSpace(string(output))
	if len(trimmed) == 0 {
		return "clean"
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) == 1 {
		return "1 uncommitted change"
	}
	return fmt.Sprintf("%d uncommitted changes", len(lines))
}

func getWorktreeLastCommit(ctx context.Context, path string) *WorktreeCommitInfo {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Get commit info
	cmd := exec.CommandContext(ctx, "git", "log", "-1",
		"--format=%h|%s|%ar")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	parts := strings.SplitN(strings.TrimSpace(string(output)), "|", 3)
	if len(parts) != 3 {
		return nil
	}

	return &WorktreeCommitInfo{
		Hash:    parts[0],
		Message: parts[1],
		Time:    parts[2],
	}
}

func outputInfoJSON(info *WorktreeInfoResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

func outputInfoPretty(info *WorktreeInfoResult) error {
	fmt.Printf("Worktree: %s\n", ui.Value(info.Name))
	fmt.Printf("Project:  %s\n", ui.Value(info.Project))
	fmt.Printf("Branch:   %s\n", ui.Value(info.Branch))
	fmt.Printf("Path:     %s\n", ui.Value(info.Path))
	fmt.Printf("Status:   %s\n", info.Status)

	if info.LastCommit != nil {
		fmt.Printf("\nLast Commit:\n")
		fmt.Printf("  %s - %s (%s)\n",
			ui.Value(info.LastCommit.Hash),
			info.LastCommit.Message,
			ui.Dim(info.LastCommit.Time),
		)
	}

	return nil
}
