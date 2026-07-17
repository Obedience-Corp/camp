package fresh

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// freshFlagSet captures the resolved persistent flags shared by every fresh
// invocation. It is passed to runFreshBatch so per-project settings can be
// resolved against the fresh config.
type freshFlagSet struct {
	branch     string
	noBranch   bool
	noPush     bool
	noPrune    bool
	noFollowUp bool
	dryRun     bool
}

// freshTarget is a resolved project to run the fresh cycle against.
type freshTarget struct {
	name string
	path string
}

// getFreshFlagSet extracts the fresh persistent flags from the parent command.
// These are stored on the parent (freshCmd) and inherited by subcommands.
func getFreshFlagSet(freshCmd *cobra.Command) freshFlagSet {
	branch, _ := freshCmd.PersistentFlags().GetString("branch")
	noBranch, _ := freshCmd.PersistentFlags().GetBool("no-branch")
	noPush, _ := freshCmd.PersistentFlags().GetBool("no-push")
	noPrune, _ := freshCmd.PersistentFlags().GetBool("no-prune")
	noFollowUp, _ := freshCmd.PersistentFlags().GetBool("no-follow-up")
	dryRun, _ := freshCmd.PersistentFlags().GetBool("dry-run")
	return freshFlagSet{
		branch:     branch,
		noBranch:   noBranch,
		noPush:     noPush,
		noPrune:    noPrune,
		noFollowUp: noFollowUp,
		dryRun:     dryRun,
	}
}

// cleanProjectList trims surrounding whitespace from each entry and drops
// empties, so '--list=camp, fest,' resolves to ["camp", "fest"] rather than
// failing on a blank or space-padded name.
func cleanProjectList(list []string) []string {
	cleaned := make([]string, 0, len(list))
	for _, name := range list {
		name = strings.TrimSpace(name)
		if name != "" {
			cleaned = append(cleaned, name)
		}
	}
	return cleaned
}

// resolveFreshTargets resolves a list of project names to fresh targets.
//
// All names are resolved up front so an invalid name fails before any project
// is mutated. Duplicate names collapse to a single target, preserving
// first-seen order.
func resolveFreshTargets(ctx context.Context, campRoot string, names []string) ([]freshTarget, error) {
	seen := make(map[string]struct{}, len(names))
	targets := make([]freshTarget, 0, len(names))
	for _, name := range names {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		result, err := project.Resolve(ctx, campRoot, name)
		if err != nil {
			var notFound *project.ProjectNotFoundError
			if errors.As(err, &notFound) {
				fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
			}
			return nil, err
		}

		if _, ok := seen[result.Name]; ok {
			continue
		}
		seen[result.Name] = struct{}{}
		targets = append(targets, freshTarget{name: result.Name, path: result.Path})
	}
	return targets, nil
}

// runFreshBatch runs the fresh cycle across multiple targets. It continues on
// per-project execution errors and reports an aggregate summary, returning an
// error if any target failed. This backs both `camp fresh <a> <b> ...` and
// `camp fresh all`.
func runFreshBatch(ctx context.Context, cfg *config.FreshConfig, targets []freshTarget, flags freshFlagSet, header string) error {
	green := lipgloss.NewStyle().Foreground(ui.SuccessColor)
	red := lipgloss.NewStyle().Foreground(ui.ErrorColor)

	fmt.Println(ui.Info(header))
	fmt.Println()

	var succeeded, failed int
	var failedNames []string

	for _, t := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		branch := cfg.ResolveFreshBranch(flags.branch, flags.noBranch, t.name)
		doPrune := !flags.noPrune && cfg.ResolveFreshPrune()
		doPush := !flags.noPush && cfg.ResolveFreshPushUpstream(t.name)
		followUps := resolveFreshFollowUps(cfg, t.name, flags.noFollowUp)

		err := executeFresh(ctx, t.name, t.path, freshOptions{
			branch:      branch,
			prune:       doPrune,
			pruneRemote: cfg.ResolveFreshPruneRemote(),
			push:        doPush,
			followUps:   followUps,
			dryRun:      flags.dryRun,
		})
		if err != nil {
			fmt.Printf("  %s %s: %s\n", red.Render("FAILED"), t.name, err)
			failed++
			failedNames = append(failedNames, t.name)
		} else {
			succeeded++
		}
	}

	fmt.Println()
	fmt.Println(ui.Separator(50))
	if failed == 0 {
		fmt.Printf("%s Fresh completed for %d project(s)\n", green.Render("All done!"), succeeded)
	} else {
		fmt.Printf("%s %d succeeded, %d failed\n",
			ui.Warning("Fresh completed with errors:"), succeeded, failed)
		for _, name := range failedNames {
			fmt.Printf("  %s %s\n", red.Render("-"), name)
		}
	}

	if failed > 0 {
		return camperrors.Newf("%d project(s) failed", failed)
	}
	return nil
}
