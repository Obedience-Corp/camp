package promote

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var Cmd = &cobra.Command{
	Use:     "promote <status>",
	Short:   "Promote the workitem at cwd to a dungeon status",
	GroupID: "planning",
	Long: `Promote the directory-style workitem containing the current working
directory to a named status. Status directories live under the workitem
type's local dungeon (workflow/<type>/dungeon/<status>/); outside the
dungeon a workitem is treated as active.

Run this from anywhere inside workflow/<type>/<slug>/. The workitem
boundary is detected from cwd. The status argument is the destination
directory name (e.g., completed, archived, someday) - no need to spell
out "dungeon/".

Examples:
  camp promote completed   Shelve the workitem to its local dungeon/completed
  camp promote archived    Move to dungeon/archived
  camp promote someday     Move to dungeon/someday`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive promotion of the workitem at cwd to a dungeon status",
	},
	RunE: runPromote,
}

func init() {
	flags := Cmd.Flags()
	flags.Bool("no-commit", false, "Skip auto-commit after promotion")
	flags.Bool("json", false, "Output result as JSON")
}

type promoteResult struct {
	Slug          string   `json:"slug"`
	Type          string   `json:"type"`
	Status        string   `json:"status"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Committed     bool     `json:"committed"`
	CommitMessage string   `json:"commit_message,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

func runPromote(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	status := args[0]

	noCommit, _ := cmd.Flags().GetBool("no-commit")
	jsonOut, _ := cmd.Flags().GetBool("json")

	if status == "active" {
		return fmt.Errorf("promote cannot target %q: %q is not a dungeon status (outside the dungeon a workitem is already active); restoring workitems out of dungeon is not supported by promote", status, status)
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "getting current directory")
	}

	loc, err := detectWorkitemFromCwd(campaignRoot, cwd)
	if err != nil {
		return err
	}

	info, err := os.Stat(loc.SourcePath)
	if err != nil {
		return camperrors.Wrapf(err, "stat workitem %s", loc.SourcePath)
	}
	if !info.IsDir() {
		return fmt.Errorf("workitem %s is not a directory; promote only handles directory-style workitems", dungeoncmd.RelFromRoot(campaignRoot, loc.SourcePath))
	}

	if loc.InDungeon && loc.Status == status {
		return fmt.Errorf("workitem %q is already at status %q", loc.Slug, status)
	}

	svc := intdungeon.NewService(campaignRoot, loc.DungeonPath)
	initResult, err := svc.Init(ctx, intdungeon.InitOptions{})
	if err != nil {
		return camperrors.Wrap(err, "initializing workitem dungeon")
	}

	targetPath, err := svc.MoveToDungeonStatus(ctx, loc.Slug, loc.ParentPath, status)
	if err != nil {
		return dungeoncmd.WrapDungeonMoveError(err, loc.Slug, status)
	}

	result := promoteResult{
		Slug:   loc.Slug,
		Type:   loc.Type,
		Status: status,
		From:   filepath.ToSlash(dungeoncmd.RelFromRoot(campaignRoot, loc.SourcePath)),
		To:     filepath.ToSlash(dungeoncmd.RelFromRoot(campaignRoot, targetPath)),
	}

	stdout := cmd.OutOrStdout()
	textOut := stdout
	if jsonOut {
		textOut = io.Discard
	}

	if !jsonOut {
		fmt.Fprintf(stdout, "%s Promoted %s (%s → %s)\n", ui.SuccessIcon(), loc.Slug, result.From, result.To)
	}

	if navErr := navindex.Delete(campaignRoot); navErr != nil {
		msg := fmt.Sprintf("failed to invalidate navigation cache: %v", navErr)
		if jsonOut {
			result.Warnings = append(result.Warnings, msg)
		} else {
			fmt.Fprintf(stdout, "%s %s\n", ui.WarningIcon(), msg)
		}
	}

	var commitErr error
	if !noCommit {
		destinationPaths := append([]string{}, initResult.CreatedFiles...)
		destinationPaths = append(destinationPaths, targetPath)
		description := fmt.Sprintf("Promote workitem %s → %s", loc.Slug, result.To)

		outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
			Config:           cfg,
			CampaignRoot:     campaignRoot,
			Description:      description,
			SourcePaths:      []string{loc.SourcePath},
			DestinationPaths: destinationPaths,
		})
		dungeoncmd.PrintDungeonMoveOutcome(textOut, outcome)
		result.Committed = outcome.Committed
		result.CommitMessage = outcome.Message
		commitErr = outcome.Err()
	}

	if jsonOut {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return camperrors.Wrap(err, "encoding JSON output")
		}
	}

	return commitErr
}
