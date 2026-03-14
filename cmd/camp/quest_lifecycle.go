//go:build dev

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/quest"
)

var (
	questPauseCmd = &cobra.Command{
		Use:   "pause <quest>",
		Short: "Pause an active quest",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive quest lifecycle transition",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestLifecycle(cmd, args[0], "pause")
		},
	}
	questResumeCmd = &cobra.Command{
		Use:   "resume <quest>",
		Short: "Resume a paused quest",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive quest lifecycle transition",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestLifecycle(cmd, args[0], "resume")
		},
	}
	questCompleteCmd = &cobra.Command{
		Use:   "complete <quest>",
		Short: "Mark a quest completed",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive quest lifecycle transition",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestLifecycle(cmd, args[0], "complete")
		},
	}
	questArchiveCmd = &cobra.Command{
		Use:   "archive <quest>",
		Short: "Archive a quest",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive quest lifecycle transition",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestLifecycle(cmd, args[0], "archive")
		},
	}
	questRestoreCmd = &cobra.Command{
		Use:   "restore <quest>",
		Short: "Restore a quest from the dungeon",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Non-interactive quest lifecycle transition",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuestLifecycle(cmd, args[0], "restore")
		},
	}
)

func init() {
	for _, sub := range []*cobra.Command{
		questPauseCmd,
		questResumeCmd,
		questCompleteCmd,
		questArchiveCmd,
		questRestoreCmd,
	} {
		questCmd.AddCommand(sub)
		sub.Flags().Bool("no-commit", false, "Don't create a git commit")
		sub.ValidArgsFunction = completeQuestSelector
	}
}

func runQuestLifecycle(cmd *cobra.Command, selector, action string) error {
	ctx := cmd.Context()
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	var (
		result     *quest.MutationResult
		commitType commit.QuestAction
		desc       string
	)
	switch action {
	case "pause":
		result, err = qctx.service.Pause(ctx, selector)
		commitType = commit.QuestPause
		desc = "Paused quest"
	case "resume":
		result, err = qctx.service.Resume(ctx, selector)
		commitType = commit.QuestResume
		desc = "Resumed quest"
	case "complete":
		result, err = qctx.service.Complete(ctx, selector)
		commitType = commit.QuestComplete
		desc = "Completed quest"
	case "archive":
		result, err = qctx.service.Archive(ctx, selector)
		commitType = commit.QuestArchive
		desc = "Archived quest"
	case "restore":
		result, err = qctx.service.Restore(ctx, selector)
		commitType = commit.QuestRestore
		desc = "Restored quest"
	default:
		return fmt.Errorf("unknown quest action: %s", action)
	}
	if err != nil {
		return err
	}

	fmt.Printf("✓ Quest %s: %s (%s)\n", action, result.Quest.Name, result.Quest.Status)
	if !noCommit {
		if err := autoCommitQuest(ctx, qctx, commitType, result, desc); err != nil {
			return camperrors.Wrapf(err, "quest %s succeeded, but auto-commit failed", action)
		}
	}
	return nil
}
