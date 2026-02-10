package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

var leverageSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Capture current leverage state as a snapshot",
	Long: `Capture the current leverage state for all projects (or a specific project)
and save as JSON snapshots for historical tracking.

Each snapshot includes scc metrics, computed leverage scores, and per-author
LOC attribution from git blame.

Snapshots are stored in .campaign/leverage/snapshots/<project>/<date>.json.
Re-running on the same date overwrites the previous snapshot.

Examples:
  camp leverage snapshot                  Snapshot all projects
  camp leverage snapshot --project camp   Snapshot specific project`,
	RunE: runLeverageSnapshot,
}

func init() {
	leverageSnapshotCmd.Flags().StringP("project", "p", "", "snapshot a specific project only")
	leverageCmd.AddCommand(leverageSnapshotCmd)
}

func runLeverageSnapshot(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	configPath := leverage.DefaultConfigPath(root)
	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.ProjectStart.IsZero() {
		detected, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return fmt.Errorf("auto-detecting config: %w", err)
		}
		cfg = detected
	}

	runner := sccRunner
	if runner == nil {
		r, err := leverage.NewSCCRunner(cfg.COCOMOProjectType)
		if err != nil {
			return err
		}
		runner = r
	}

	resolved, err := leverage.ResolveProjects(ctx, root, cfg)
	if err != nil {
		return fmt.Errorf("resolving projects: %w", err)
	}

	projectFilter, _ := cmd.Flags().GetString("project")
	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(root))

	elapsed := leverage.ElapsedMonths(cfg.ProjectStart, time.Now())

	var count int
	for _, proj := range resolved {
		if err := ctx.Err(); err != nil {
			return err
		}

		if projectFilter != "" && proj.Name != projectFilter {
			continue
		}

		// Get HEAD commit info
		hash, commitDate, err := getHeadCommit(ctx, proj.GitDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", proj.Name, err)
			continue
		}

		// Run scc
		result, err := runner.Run(ctx, proj.SCCDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s (scc): %v\n", proj.Name, err)
			continue
		}

		// Compute leverage score
		score := leverage.ComputeScore(result, cfg.ActualPeople, elapsed)
		score.ProjectName = proj.Name

		// Get author contributions via git blame
		authors, err := leverage.GetAuthorLOC(ctx, proj.SCCDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s author attribution failed: %v\n", proj.Name, err)
			// Non-fatal: continue without author data
		}

		// Build snapshot
		snapshot := &leverage.Snapshot{
			Project:    proj.Name,
			CommitHash: hash,
			CommitDate: commitDate,
			SampledAt:  time.Now(),
			SCC:        leverage.SCCResultToSnapshotSCC(result),
			Leverage:   score,
			Authors:    authors,
			TotalLines: result.LanguageSummary[0].Lines, // Will be aggregated below
		}

		// Aggregate total lines from all languages
		var totalLines int
		for _, lang := range result.LanguageSummary {
			totalLines += lang.Lines
		}
		snapshot.TotalLines = totalLines

		if err := store.Save(ctx, snapshot); err != nil {
			return fmt.Errorf("saving snapshot for %s: %w", proj.Name, err)
		}
		count++
	}

	if projectFilter != "" && count == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Saved %d snapshots to .campaign/leverage/snapshots/\n", count)
	return nil
}

// getHeadCommit returns the HEAD commit hash and date for a git directory.
func getHeadCommit(ctx context.Context, gitDir string) (string, time.Time, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "log", "-1", "--format=%H%n%cI")
	out, err := cmd.Output()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("git log: %w", err)
	}

	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) != 2 {
		return "", time.Time{}, fmt.Errorf("unexpected git log output")
	}

	date, err := time.Parse(time.RFC3339, lines[1])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parsing commit date: %w", err)
	}

	return lines[0], date, nil
}

