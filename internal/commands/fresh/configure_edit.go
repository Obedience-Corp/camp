package fresh

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func newConfigureEditCommand() *cobra.Command {
	var (
		run             string
		dir             string
		continueOnError bool
		newName         string
		projectName     string
	)

	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a follow-up command workflow step",
		Long: `Update a follow-up in place, keeping its position in the sequence.

On a project that still inherits the global list, editing forks that list
into a project override the same way the interactive setup does.

Pass --name to rename the step. --run is required so the command being
saved is explicit rather than inferred from a previous value.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a camp")
			}
			if strings.TrimSpace(projectName) != "" {
				resolved, err := project.Resolve(ctx, campRoot, projectName)
				if err != nil {
					return err
				}
				projectName = resolved.Name
			}

			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}

			oldName := args[0]
			entry := config.FollowUpConfig{
				Name:            oldName,
				Run:             run,
				Dir:             dir,
				ContinueOnError: continueOnError,
			}
			if strings.TrimSpace(newName) != "" {
				entry.Name = strings.TrimSpace(newName)
			}
			if err := replaceFreshFollowUp(ctx, cfg, campRoot, projectName, oldName, entry); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), ui.Success(fmt.Sprintf("Updated follow-up %q (%s)", entry.Name, followUpScopeDescription(projectName))))
			return nil
		},
	}

	cmd.Flags().StringVar(&run, "run", "", "Command to run for this follow-up step (required)")
	cmd.Flags().StringVar(&dir, "dir", "", "Directory relative to the project root to run the command in")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Keep running later follow-ups if this step fails")
	cmd.Flags().StringVar(&newName, "name", "", "Rename the follow-up")
	cmd.Flags().StringVar(&projectName, "project", "", "Scope this follow-up to a single project (default: global)")
	_ = cmd.MarkFlagRequired("run")
	_ = cmd.RegisterFlagCompletionFunc("project", completeProjectName)

	return cmd
}

func replaceFreshFollowUp(ctx context.Context, cfg *config.FreshConfig, campRoot, projectName, oldName string, entry config.FollowUpConfig) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	entries := append([]config.FollowUpConfig(nil), cfg.ResolveFreshFollowUps(projectName)...)
	found := false
	for i := range entries {
		if entries[i].Name != oldName {
			continue
		}
		for j, existing := range entries {
			if j != i && existing.Name == entry.Name {
				return camperrors.NewValidation("name", fmt.Sprintf("follow-up %q already exists", entry.Name), nil)
			}
		}
		entries[i] = entry
		found = true
		break
	}
	if !found {
		return camperrors.NewValidation("name", fmt.Sprintf("follow-up %q is not configured in %s", oldName, followUpScopeDescription(projectName)), nil)
	}
	return config.SetFreshFollowUps(ctx, campRoot, projectName, entries)
}
