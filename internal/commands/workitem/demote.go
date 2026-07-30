package workitem

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/spf13/cobra"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// demoteTarget is the audit/ledger target recorded for a demote, so the event
// stream distinguishes leaving the rail from moving along it.
const demoteTarget = "home"

type runWorkitemDemoteOptions struct {
	ID       string
	DryRun   bool
	NoCommit bool
	JSON     bool
}

func newDemoteCommand() *cobra.Command {
	var (
		dryRun   bool
		noCommit bool
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "demote [id]",
		Short: "Move a rail resident back to its type root",
		Long: `Take the workitem identified by [id], by cwd, or by the current pointer off the
festival rail and back to its original workflow type root.

A resident in festivals/ready or festivals/active returns to
workflow/<type>/<slug>, where the type comes from its own .workitem marker. Every
reference is repaired in the same commit, exactly as the rail move does.

This is the escape hatch from the rail, not backward motion along it. It is
rejected from a dungeon, because restoring a shelved workitem is not a demote,
and from a workitem already at its type root.`,
		Args: cobra.RangeArgs(0, 1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by flags; no interactive selection",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			return runWorkitemDemote(cmd, runWorkitemDemoteOptions{
				ID: id, DryRun: dryRun, NoCommit: noCommit, JSON: jsonOut,
			})
		},
	}

	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "Print the planned move, change nothing")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

func runWorkitemDemote(cmd *cobra.Command, opts runWorkitemDemoteOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	loc, err := resolveWorkitem(ctx, root, opts.ID)
	if err != nil {
		return err
	}
	if err := checkDemotable(railStageOf(loc, root)); err != nil {
		return err
	}

	ledgerID, ledgerRef, ledgerTitle := loc.Slug, "", ""
	if meta, metaErr := wkitem.LoadMetadata(ctx, loc.SourcePath); metaErr == nil && meta != nil {
		ledgerID, ledgerRef, ledgerTitle = meta.ID, meta.Ref, meta.Title
	}

	oldRel := filepath.ToSlash(dungeoncmd.RelFromRoot(root, loc.SourcePath))
	newRel := path.Join("workflow", loc.Type, loc.Slug)
	result := workitemPromoteResult{
		ID:     loc.Slug,
		Type:   loc.Type,
		Target: demoteTarget,
		From:   oldRel,
	}

	if opts.DryRun {
		result.To = newRel
		if opts.JSON {
			return emitPromoteJSON(cmd, result)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"dry-run: would demote workitem %s (%s) from %s to %s\n", loc.Slug, loc.Type, oldRel, newRel)
		return err
	}

	destAbs := filepath.Join(root, filepath.FromSlash(newRel))
	if _, statErr := os.Lstat(destAbs); statErr == nil {
		return camperrors.Wrapf(camperrors.ErrAlreadyExists,
			"cannot demote %s: destination %s already exists", loc.Slug, newRel)
	}

	ci, err := applyWorkitemMove(ctx, root, workitemMove{
		SourcePath:  loc.SourcePath,
		DestPath:    destAbs,
		OldRel:      oldRel,
		NewRel:      newRel,
		OldKey:      loc.Type + ":" + oldRel,
		NewKey:      loc.Type + ":" + newRel,
		Description: fmt.Sprintf("Demote workitem %s to %s", loc.Slug, newRel),
	}, &result)
	if err != nil {
		return err
	}

	return finishWorkitemMove(ctx, cmd, cfg, root, ci, &result, moveTail{
		LedgerID:    ledgerID,
		LedgerRef:   ledgerRef,
		LedgerTitle: ledgerTitle,
		Why:         "demote to home",
		SuccessVerb: "Demoted",
		Options:     moveTailOptions{NoCommit: opts.NoCommit, JSON: opts.JSON},
	})
}

// checkDemotable allows a demote only from a rail stage. Leaving a dungeon is a
// restore, and a workitem at its type root is already home.
func checkDemotable(from string) error {
	switch from {
	case railStageReady, railStageActive:
		return nil
	case railStageDungeon:
		return camperrors.New("cannot demote from a dungeon: restoring a shelved workitem is not a demote")
	default:
		return camperrors.New("cannot demote: workitem is already at its type root, not on the festival rail")
	}
}
