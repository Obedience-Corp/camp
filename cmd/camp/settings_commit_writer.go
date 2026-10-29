package main

import (
	"errors"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/spf13/cobra"
)

func newCommitWriterSettingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "commit-writer",
		Short:       "Set up or change automatic commit messages for this camp",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{"interactive": "true", "agent_allowed": "false", "agent_reason": "Interactive commit writer setup"},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := campaign.DetectCached(cmd.Context())
			if err != nil {
				return err
			}
			_, err = cmdutil.ConfigureCommitWriter(cmd, root, false)
			if errors.Is(err, git.ErrCommitCancelled) {
				return nil
			}
			return err
		},
	}
}
