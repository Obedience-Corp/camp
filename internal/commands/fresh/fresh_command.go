package fresh

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// NewFreshCommand creates and returns the fresh cobra command with all subcommands.
func NewFreshCommand() *cobra.Command {
	var (
		freshBranch             string
		freshNoBranch           bool
		freshNoPush             bool
		freshNoPrune            bool
		freshNoFollowUp         bool
		freshDryRun             bool
		freshProjectFlag        string
		freshList               []string
		freshNoDrain            bool
		freshCleanupStack       bool
		freshAllowDefaultTarget bool
		freshNoLink             bool
		freshCampaign           string
	)

	freshCmd := &cobra.Command{
		Use:   "fresh [project-name]",
		Short: "Post-merge branch cycling: sync to default branch and optionally create a new working branch",
		Long: `Reset one or more projects to a fresh state after merging a PR.

Performs the post-merge cycle: checkout the default branch, fetch origin,
sync to the remote default while preserving local-only commits, prune merged
branches, and optionally create a new working branch.

Auto-detects the current project from your working directory, or accepts a
single project name. Use --list to cycle a specific set of projects in one
run, or 'camp fresh all' to cycle every project submodule in the camp.

OUTSIDE A CAMP

Run with no project name inside a git repository that is not part of a camp,
and camp fresh links that repository into a camp as a project, says so in one
line, and runs the normal cycle. The repository stays where it is: the link is
a shortcut at projects/<name> in the camp and a .camp attachment marker in the
repository, kept out of git status. 'camp project unlink' reverses it.

The repository is linked into the camp named festival. With no registered
camps, that starter camp is created first, the way 'camp setup' creates it.
With registered camps and none named festival, camp fresh stops and asks for
--campaign rather than guessing. From a secondary git worktree, the main
checkout is the project that gets linked.

--dry-run never links and never creates a camp: it prints what would be
linked, then the plan. --no-link turns linking off, so an unlinked repository
is an error.

Without configuration, syncs to the default branch and prunes.
Configure .campaign/settings/fresh.yaml to set a default working branch, or
follow-up command workflows (install, build, bootstrap, ...) to run once the
cycle succeeds. Manage those with 'camp fresh configure'. Inspect the resolved
sequence with 'camp fresh show-workflow [project-name]'.

WORKITEM COMPLETION

Two fresh.yaml settings decide what happens to workitems whose work looks done:

  completed_runs     Tier 1, once per run, camp-root scoped. "prompt"
                     (default) asks per workitem on a TTY and reports otherwise,
                     "report" prints a read-only banner and the reason for every
                     non-move, "sweep" promotes automatically (the pre-2026-08
                     behavior), "off" skips it. This is the same work
                     'camp workitem sweep' performs.
  merged_workitems   Tier 2, per project. A workitem whose branch merged is
                     inferred evidence, so it never auto-promotes: "prompt"
                     asks on a TTY and reports otherwise, "report" prints the
                     exact promote commands to run, "off" skips it.

completed_runs asks rather than sweeps because a completed workflow run is not
the same claim for every kind of work. For a bug or a chore the loop was the
work. For explore and research it means findings now exist and need a home, so
the prompt offers to route them into docs/ instead of burying them. For a design
it means a specification exists that nobody has built yet, so a completed run
never promotes one; a design waits for a merged branch or a completed festival.

Both settings are configured in fresh.yaml, not by flags. See
'camp fresh configure'.

Examples:
  camp fresh                            # Sync current project (checkout default, fetch, prune)
  camp fresh --branch develop           # Sync and create develop branch
  camp fresh camp -b feat/new-thing     # Sync camp project, create feature branch
  camp fresh --list camp,fest,festival  # Sync a specific set of projects
  camp fresh --no-prune                 # Sync without pruning
  camp fresh --no-follow-up             # Sync without running configured follow-ups
  camp fresh --dry-run                  # Preview what would happen (follow-ups listed, not run)
  camp fresh --campaign work            # In an unlinked repository: link it into the camp named work, then sync
  camp fresh --no-link                  # In an unlinked repository: fail instead of linking

  camp fresh --branch feat/aggregate-CW0003 --cleanup-stack
                                        # Switch to an existing aggregate branch and
                                        # remove child worktrees merged into it
                                        # (ancestry or squash/patch-id). Refuses
                                        # main/master unless --allow-default-target.
                                        # Worktrees whose branches also landed on
                                        # the default branch are skipped (not stack
                                        # children). The default-branch prune still
                                        # runs first; pass --no-prune to skip it.
  camp fresh --branch feat/aggregate-CW0003 --cleanup-stack --dry-run
                                        # Preview the stack cleanup plan without removing anything`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProjectName,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Outside a camp, a git repository is linked into one instead of
			// failing. Planning writes nothing, so every flag check below still
			// runs before the link is made.
			var autoLink *autoLinkPlan
			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				autoLink, err = planAutoLink(ctx, err, autoLinkRequest{
					explicitTarget: len(args) > 0 || freshProjectFlag != "" || len(cleanProjectList(freshList)) > 0,
					noLink:         freshNoLink,
					campaign:       freshCampaign,
					errOut:         cmd.ErrOrStderr(),
				})
				if err != nil {
					return err
				}
				campRoot = autoLink.campRoot
			}

			// Load fresh config. A starter camp that does not exist yet has no
			// fresh.yaml, which loads as the defaults.
			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}

			flags := freshFlagSet{
				branch:             freshBranch,
				noBranch:           freshNoBranch,
				noPush:             freshNoPush,
				noPrune:            freshNoPrune,
				noFollowUp:         freshNoFollowUp,
				dryRun:             freshDryRun,
				cleanupStack:       freshCleanupStack,
				allowDefaultTarget: freshAllowDefaultTarget,
			}

			if freshCleanupStack && freshBranch == "" {
				return camperrors.New("--cleanup-stack requires --branch <existing-branch>")
			}
			if freshCleanupStack && freshNoBranch {
				return camperrors.New("--cleanup-stack cannot be used with --no-branch")
			}
			if freshAllowDefaultTarget && !freshCleanupStack {
				return camperrors.New("--allow-default-target requires --cleanup-stack")
			}

			// --list runs a batch across an explicit set of projects. It is
			// mutually exclusive with a positional project name (the -p/--project
			// flag is guarded by MarkFlagsMutuallyExclusive below).
			list := cleanProjectList(freshList)
			if len(list) > 0 {
				if len(args) > 0 {
					return camperrors.New("specify a project with a positional name or --list, not both")
				}

				// Resolve all names up front (fail fast on a bad name), then
				// run the batch cycle with an aggregate summary.
				targets, err := resolveFreshTargets(ctx, campRoot, list)
				if err != nil {
					return err
				}
				if err := drainFreshTargets(ctx, targets, freshNoDrain, freshDryRun); err != nil {
					return err
				}
				header := fmt.Sprintf("Running fresh across %d project(s)...", len(targets))
				return runFreshBatch(ctx, cfg, targets, flags, header)
			}

			// Single project: positional name > --project > cwd auto-detect.
			projectName := freshProjectFlag
			if len(args) > 0 {
				projectName = args[0]
			}

			var target freshTarget
			if autoLink != nil {
				// Only a preview whose camp does not exist runs without camp
				// reports. An existing target camp is safe to read without linking.
				previewOnly, err := runAutoLink(ctx, autoLink, flags, cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				if previewOnly {
					campRoot = ""
				} else {
					campRoot = autoLink.campRoot
					// The camp may have been created just now; read its settings.
					if cfg, err = config.LoadFreshConfig(ctx, campRoot); err != nil {
						return camperrors.Wrap(err, "loading fresh config")
					}
				}
				target = freshTarget{name: autoLink.name, path: autoLink.repo.top}
			} else {
				result, err := project.Resolve(ctx, campRoot, projectName)
				if err != nil {
					var notFound *project.ProjectNotFoundError
					if errors.As(err, &notFound) {
						fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
					}
					return err
				}
				target = freshTarget{name: result.Name, path: result.Path}
			}
			if err := drainFreshTargets(ctx, []freshTarget{target}, freshNoDrain, freshDryRun); err != nil {
				return err
			}

			// Resolve settings
			branch := cfg.ResolveFreshBranch(freshBranch, freshNoBranch, target.name)
			doPrune := !freshNoPrune && cfg.ResolveFreshPrune()
			doPush := !freshNoPush && cfg.ResolveFreshPushUpstream(target.name)
			followUps := resolveFreshFollowUps(cfg, target.name, freshNoFollowUp)

			var summary string
			if err := executeFresh(ctx, target.name, target.path, freshOptions{
				branch:             branch,
				prune:              doPrune,
				pruneRemote:        cfg.ResolveFreshPruneRemote(),
				push:               doPush,
				followUps:          followUps,
				dryRun:             freshDryRun,
				campRoot:           campRoot,
				mergedWorkitems:    cfg.ResolveFreshMergedWorkitems(),
				cleanupStack:       freshCleanupStack,
				allowDefaultTarget: freshAllowDefaultTarget,
				holdSummary:        &summary,
			}); err != nil {
				return err
			}

			// Campaign-root workitem sweep runs once, after the project's
			// git-hygiene cycle, never inside executeFresh. The closing line
			// follows it so "Fresh!" is the last thing on the screen. A preview
			// that would create a new camp has no camp to sweep.
			if campRoot != "" {
				runCampaignWorkitemSweep(ctx, cfg, freshDryRun)
			}
			printFreshSummary(summary)
			return nil
		},
	}

	freshCmd.Flags().Bool("interaction-json", false, "Stream Fresh output and native UI questions as NDJSON; read answers from stdin")
	wrapInteraction(freshCmd)

	// Persistent flags are inherited by subcommands (e.g. fresh all --dry-run)
	freshCmd.PersistentFlags().StringVarP(&freshBranch, "branch", "b", "", "Branch to create after syncing (overrides config)")
	freshCmd.PersistentFlags().BoolVar(&freshNoBranch, "no-branch", false, "Skip branch creation even if configured")
	freshCmd.PersistentFlags().BoolVar(&freshNoPush, "no-push", false, "Skip pushing the new branch upstream")
	freshCmd.PersistentFlags().BoolVar(&freshNoPrune, "no-prune", false, "Skip pruning merged branches")
	freshCmd.PersistentFlags().BoolVar(&freshNoFollowUp, "no-follow-up", false, "Skip configured follow-up command workflows")
	freshCmd.PersistentFlags().BoolVarP(&freshDryRun, "dry-run", "n", false, "Preview without making changes")
	freshCmd.PersistentFlags().BoolVar(&freshCleanupStack, "cleanup-stack", false,
		"Target an existing aggregate branch and remove child worktrees merged into it by ancestry or squash (requires --branch; refuses the default branch unless --allow-default-target)")
	freshCmd.PersistentFlags().BoolVar(&freshAllowDefaultTarget, "allow-default-target", false,
		"Permit --cleanup-stack against the default branch (main/master). Without this, cleanup-stack refuses default-branch targets because every merged feature worktree would look like a stack child")
	freshCmd.Flags().StringVarP(&freshProjectFlag, "project", "p", "", "Project name (auto-detected from cwd)")
	if err := freshCmd.RegisterFlagCompletionFunc("project", completeProjectName); err != nil {
		panic(err)
	}
	freshCmd.Flags().StringSliceVar(&freshList, "list", nil, "Comma-separated set of projects to cycle in one run")
	freshCmd.Flags().BoolVar(&freshNoDrain, "no-drain", false, "Do not wait for camp's queued commits first")
	freshCmd.Flags().BoolVar(&freshNoLink, "no-link", false, "Never link an unlinked repository into a camp; fail with 'not in a camp' instead")
	freshCmd.Flags().StringVarP(&freshCampaign, "campaign", "c", "", "Camp to link an unlinked repository into, by name or ID (ignored when already linked or inside a camp)")
	_ = freshCmd.RegisterFlagCompletionFunc("list", completeProjectName)
	freshCmd.MarkFlagsMutuallyExclusive("project", "list")

	// Add subcommands
	freshCmd.AddCommand(newAllCommand(freshCmd))
	freshCmd.AddCommand(newConfigureCommand())
	freshCmd.AddCommand(newShowWorkflowCommand())

	return freshCmd
}
