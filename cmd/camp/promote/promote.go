package promote

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	wicmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
)

var Cmd = &cobra.Command{
	Use:    "shelve <status>",
	Short:  "Deprecated: use camp workitem promote --target <status>",
	Hidden: true,
	Long: `Deprecated alias for camp workitem promote --target <status>.

Shelve the directory-style workitem containing the current working directory
to a named dungeon status (completed, archived, someday). Run from anywhere
inside workflow/<type>/<slug>/. Prefer camp workitem promote --target <status>
or camp promote.`,
	Example: `  camp shelve completed   # use: camp workitem promote --target completed`,
	Args:    cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive shelving of the workitem at cwd to a dungeon status",
	},
	RunE: runShelveAlias,
}

func init() {
	flags := Cmd.Flags()
	flags.Bool("no-commit", false, "Skip auto-commit after shelving")
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

func runShelveAlias(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	status := args[0]

	noCommit, _ := cmd.Flags().GetBool("no-commit")
	jsonOut, _ := cmd.Flags().GetBool("json")

	if status == "active" {
		return camperrors.New(fmt.Sprintf("shelve cannot target %q: %q is not a dungeon status (outside the dungeon a workitem is already active); restoring workitems out of dungeon is not supported by shelve", status, status))
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "getting current directory")
	}

	loc, err := locate.DetectFromCwd(campaignRoot, cwd)
	if err != nil {
		return err
	}

	// Read .workitem metadata before the move: shelve relocates loc.SourcePath,
	// so the marker will not be readable from its original path afterward.
	// camp shelve is a promote alias (see command Long text), so it records
	// the same "promote" event type camp workitem promote uses for the
	// completed/archived/someday targets.
	ledgerID, ledgerRef, ledgerTitle := loc.Slug, "", ""
	if meta, metaErr := wkitem.LoadMetadata(ctx, loc.SourcePath); metaErr == nil && meta != nil {
		ledgerID, ledgerRef, ledgerTitle = meta.ID, meta.Ref, meta.Title
	}

	move, err := wicmd.MoveToDungeon(ctx, campaignRoot, loc, status)
	if err != nil {
		return err
	}

	wkaudit.AppendBestEffort(ctx, cmd.ErrOrStderr(), campaignRoot, wkaudit.Event{
		Event:  wkaudit.EventPromote,
		ID:     ledgerID,
		Ref:    ledgerRef,
		Title:  ledgerTitle,
		Type:   loc.Type,
		From:   move.FromRel,
		To:     move.ToRel,
		Target: status,
	})

	result := promoteResult{
		Slug:   loc.Slug,
		Type:   loc.Type,
		Status: status,
		From:   move.FromRel,
		To:     move.ToRel,
	}

	stdout := cmd.OutOrStdout()
	textOut := stdout
	if jsonOut {
		textOut = io.Discard
	}

	if !jsonOut {
		if _, err := fmt.Fprintf(stdout, "%s Shelved %s (%s → %s)\n", ui.SuccessIcon(), loc.Slug, result.From, result.To); err != nil {
			return err
		}
	}

	if navErr := navindex.Delete(campaignRoot); navErr != nil {
		msg := fmt.Sprintf("failed to invalidate navigation cache: %v", navErr)
		if jsonOut {
			result.Warnings = append(result.Warnings, msg)
		} else {
			_, _ = fmt.Fprintf(stdout, "%s %s\n", ui.WarningIcon(), msg)
		}
	}

	var commitErr error
	if !noCommit {
		destinationPaths := append([]string{}, move.CreatedFiles...)
		destinationPaths = append(destinationPaths, move.TargetPath)
		destinationPaths = append(destinationPaths, filepath.Join(campaignRoot, ".campaign", "workitems", wkaudit.AuditFile))
		description := fmt.Sprintf("Shelve workitem %s → %s", loc.Slug, result.To)

		outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
			Config:           cfg,
			CampaignRoot:     campaignRoot,
			Description:      description,
			SourcePaths:      []string{loc.SourcePath},
			DestinationPaths: destinationPaths,
			RewrittenFiles:   move.Svc.RewrittenLinkFiles(),
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
		return commitErr
	}

	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "camp shelve is deprecated; use camp workitem promote --target "+status+" (or camp promote).")
	return commitErr
}
