package fresh

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// configureProjectFlag preselects the project scope the interactive configure
// TUI opens on. Empty means "detect from the working directory".
var configureProjectFlag string

// newConfigureCommand builds the non-interactive `camp fresh configure`
// subcommand group for managing follow-up command workflows stored in
// .campaign/settings/fresh.yaml.
func newConfigureCommand() *cobra.Command {
	configureCmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure the camp fresh workflow",
		Long: `Configure what camp fresh does after a merge. Configuration lives in
.campaign/settings/fresh.yaml, as campaign-wide defaults plus optional
per-project overrides.

Run without a subcommand to open the interactive setup for humans, which
groups the fresh sequence by what you can change about each step:

  Sync        checkout, pull, and safety checks; always runs
  Settings    branch, push_upstream, prune, and prune_remote
  Follow-ups  your own commands, run after a successful cycle

Press enter on a settings step to change it, and a/e/d/K/J on a follow-up to
add, edit, delete, or reorder it. prune and prune_remote are campaign-wide,
so they are changed under Global defaults rather than under a project.

The subcommands below cover follow-ups only, for scripts and agents; edit the
other keys in the interactive setup or in fresh.yaml directly.

The interactive setup opens on the project you are standing in, resolved the
same way camp fresh picks its target, so the overrides you edit are the ones
that run. Pass --project to open on a different project, and edit the global
defaults by selecting them in the left pane.

Examples:
  camp fresh configure
  camp fresh configure --project camp
  camp fresh show-workflow camp
  camp fresh configure show
  camp fresh configure add install --run "npm install"
  camp fresh configure add build --run "go build ./..." --project camp --dir cmd/camp
  camp fresh configure move build --up --project camp
  camp fresh configure remove install
  camp fresh configure remove build --project camp`,
		Args: cobra.NoArgs,
		RunE: runConfigureTUI,
	}

	configureCmd.Flags().StringVar(&configureProjectFlag, "project", "", "Open the setup on a project scope (default: detected from the current directory)")
	_ = configureCmd.RegisterFlagCompletionFunc("project", completeProjectName)

	configureCmd.AddCommand(newConfigureShowCommand())
	configureCmd.AddCommand(newConfigureAddCommand())
	configureCmd.AddCommand(newConfigureMoveCommand())
	configureCmd.AddCommand(newConfigureRemoveCommand())

	return configureCmd
}

func newShowWorkflowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show-workflow [project-name]",
		Short: "Show the fresh cycle and configured follow-up steps",
		Long: `Show the ordered steps camp fresh will use, including disabled steps
and the follow-up commands resolved for a project.

With no project name, the global defaults are shown. Pass a project name to
include its branch, pruning, and follow-up overrides.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProjectName,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}
			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}
			projectName := ""
			if len(args) == 1 {
				resolved, err := project.Resolve(ctx, campRoot, args[0])
				if err != nil {
					return err
				}
				projectName = resolved.Name
			}
			return printFreshWorkflow(cmd.OutOrStdout(), cfg, projectName)
		},
	}
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

func newConfigureMoveCommand() *cobra.Command {
	var (
		project string
		up      bool
		down    bool
	)

	cmd := &cobra.Command{
		Use:   "move <name>",
		Short: "Move a follow-up command workflow step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if up == down {
				return camperrors.NewValidation("direction", "choose exactly one of --up or --down", nil)
			}

			ctx := cmd.Context()
			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}

			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}
			name := args[0]
			delta := 1
			direction := "down"
			if up {
				delta = -1
				direction = "up"
			}
			ordered, err := config.ReorderFreshFollowUps(cfg.ResolveFreshFollowUps(project), name, delta)
			if err != nil {
				return err
			}
			if err := config.SetFreshFollowUps(ctx, campRoot, project, ordered); err != nil {
				return err
			}

			fmt.Println(ui.Success(fmt.Sprintf("Moved follow-up %q %s (%s)", name, direction, followUpScopeDescription(project))))
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Scope the move to a single project (default: global)")
	cmd.Flags().BoolVar(&up, "up", false, "Move the step earlier in the workflow")
	cmd.Flags().BoolVar(&down, "down", false, "Move the step later in the workflow")

	return cmd
}

func followUpScopeDescription(project string) string {
	if project == "" {
		return "global"
	}
	return "project " + project
}
