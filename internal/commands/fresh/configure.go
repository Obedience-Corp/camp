package fresh

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// newConfigureCommand builds the non-interactive `camp fresh configure`
// subcommand group for managing follow-up command workflows stored in
// .campaign/settings/fresh.yaml.
func newConfigureCommand() *cobra.Command {
	configureCmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure camp fresh follow-up commands",
		Long: `Manage the follow-up command workflows camp fresh runs after a
successful sync/prune/branch cycle. Configuration lives in
.campaign/settings/fresh.yaml: a global default list, plus optional
per-project override lists that replace the global list entirely.

Run without a subcommand to open the interactive setup for humans. Use
show, add, and remove for scripts and agents.

Examples:
  camp fresh configure
  camp fresh configure show
  camp fresh configure add install --run "npm install"
  camp fresh configure add build --run "go build ./..." --project camp --dir cmd/camp
  camp fresh configure remove install
  camp fresh configure remove build --project camp`,
		Args: cobra.NoArgs,
		RunE: runConfigureTUI,
	}

	configureCmd.AddCommand(newConfigureShowCommand())
	configureCmd.AddCommand(newConfigureAddCommand())
	configureCmd.AddCommand(newConfigureRemoveCommand())

	return configureCmd
}

func newConfigureShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show configured follow-up workflows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}

			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}

			printFollowUpSection("Global", cfg.FollowUp)

			names := make([]string, 0, len(cfg.Projects))
			for name, pc := range cfg.Projects {
				if len(pc.FollowUp) > 0 {
					names = append(names, name)
				}
			}
			sort.Strings(names)

			for _, name := range names {
				fmt.Println()
				printFollowUpSection("Project: "+name, cfg.Projects[name].FollowUp)
			}

			return nil
		},
	}
}

func printFollowUpSection(label string, steps []config.FollowUpConfig) {
	fmt.Println(ui.Header(label))
	if len(steps) == 0 {
		fmt.Println(ui.Dim("  (none configured)"))
		return
	}
	for _, step := range steps {
		line := fmt.Sprintf("  %s  %s", ui.Value(step.Name), step.Run)
		if step.Dir != "" {
			line += " " + ui.Dim(fmt.Sprintf("(dir: %s)", step.Dir))
		}
		if step.ContinueOnError {
			line += " " + ui.Dim("[continue-on-error]")
		}
		fmt.Println(line)
	}
}

func newConfigureAddCommand() *cobra.Command {
	var (
		run             string
		project         string
		dir             string
		continueOnError bool
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a follow-up command workflow step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}

			entry := config.FollowUpConfig{
				Name:            name,
				Run:             run,
				Dir:             dir,
				ContinueOnError: continueOnError,
			}

			if err := config.AddFreshFollowUp(ctx, campRoot, project, entry); err != nil {
				return err
			}

			fmt.Println(ui.Success(fmt.Sprintf("Added follow-up %q (%s)", name, followUpScopeDescription(project))))
			return nil
		},
	}

	cmd.Flags().StringVar(&run, "run", "", "Command to run for this follow-up step (required)")
	cmd.Flags().StringVar(&project, "project", "", "Scope this follow-up to a single project (default: global)")
	cmd.Flags().StringVar(&dir, "dir", "", "Directory relative to the project root to run the command in")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Keep running later follow-ups if this step fails")
	_ = cmd.MarkFlagRequired("run")

	return cmd
}

func newConfigureRemoveCommand() *cobra.Command {
	var project string

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a follow-up command workflow step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}

			if err := config.RemoveFreshFollowUp(ctx, campRoot, project, name); err != nil {
				return err
			}

			fmt.Println(ui.Success(fmt.Sprintf("Removed follow-up %q (%s)", name, followUpScopeDescription(project))))
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Scope removal to a single project (default: global)")

	return cmd
}

func followUpScopeDescription(project string) string {
	if project == "" {
		return "global"
	}
	return "project " + project
}
