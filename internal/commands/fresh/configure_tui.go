package fresh

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

const globalFollowUpScope = "__global__"

// runConfigureTUI is the human-facing entry point for `camp fresh configure`.
// The child commands remain available for scripts and agents that need a
// non-interactive interface.
func runConfigureTUI(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if !ui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput,
			"fresh configure requires an interactive terminal; use configure show|add|remove for automation")
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "listing campaign projects")
	}

	return runFollowUpMenu(ctx, campRoot, projects)
}

func runFollowUpMenu(ctx context.Context, campRoot string, projects []project.Project) error {
	for {
		var action string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Fresh follow-ups").
				Description("Commands run after a successful camp fresh cycle.").
				Options(
					huh.NewOption("Add a follow-up command", "add"),
					huh.NewOption("Remove a follow-up command", "remove"),
					huh.NewOption("Review configured commands", "review"),
					huh.NewOption("Done", "done"),
				).
				Value(&action),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch action {
		case "add":
			if err := addFollowUpTUI(ctx, campRoot, projects); err != nil {
				if errors.Is(err, errFollowUpTUIBack) {
					continue
				}
				return err
			}
		case "remove":
			if err := removeFollowUpTUI(ctx, campRoot, projects); err != nil {
				if errors.Is(err, errFollowUpTUIBack) {
					continue
				}
				return err
			}
		case "review":
			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}
			fmt.Println()
			printFollowUpSection("Global default", cfg.FollowUp)
			for _, name := range configuredProjectNames(cfg) {
				fmt.Println()
				printFollowUpSection("Project: "+name, cfg.Projects[name].FollowUp)
			}
			fmt.Println()
		case "done", "":
			return nil
		}
	}
}

var errFollowUpTUIBack = errors.New("follow-up TUI back")

func addFollowUpTUI(ctx context.Context, campRoot string, projects []project.Project) error {
	scope, err := selectFollowUpScope(ctx, projects, nil)
	if err != nil {
		if theme.IsCancelled(err) {
			return errFollowUpTUIBack
		}
		return err
	}

	var name, run, dir string
	continueOnError := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Command name").
			Description("A short label, such as install or build.").
			Placeholder("install").
			Value(&name).
			Validate(requiredField("command name")),
		huh.NewInput().
			Title("Command").
			Description("Runs after camp fresh finishes syncing.").
			Placeholder("npm install").
			Value(&run).
			Validate(requiredField("command")),
		huh.NewInput().
			Title("Working directory (optional)").
			Description("Relative to the project root; leave blank for the root.").
			Placeholder("cmd/camp").
			Value(&dir),
		huh.NewConfirm().
			Title("Continue if this command fails?").
			Description("Choose No to stop the fresh cycle on failure.").
			Affirmative("Continue").
			Negative("Stop").
			Value(&continueOnError),
	).Title("Add follow-up").Description(scopeDescription(scope)))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return errFollowUpTUIBack
		}
		return err
	}

	entry := config.FollowUpConfig{
		Name:            strings.TrimSpace(name),
		Run:             strings.TrimSpace(run),
		Dir:             strings.TrimSpace(dir),
		ContinueOnError: continueOnError,
	}
	if err := config.AddFreshFollowUp(ctx, campRoot, scopeProjectName(scope), entry); err != nil {
		return err
	}

	fmt.Println(ui.Success(fmt.Sprintf("Added %q to %s.", entry.Name, scopeDescription(scope))))
	return nil
}

func removeFollowUpTUI(ctx context.Context, campRoot string, projects []project.Project) error {
	cfg, err := config.LoadFreshConfig(ctx, campRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading fresh config")
	}

	scope, err := selectFollowUpScope(ctx, projects, cfg)
	if err != nil {
		if theme.IsCancelled(err) {
			return errFollowUpTUIBack
		}
		return err
	}

	steps := followUpsForScope(cfg, scope)
	if len(steps) == 0 {
		fmt.Println(ui.Warning(fmt.Sprintf("No follow-up commands are configured for %s.", scopeDescription(scope))))
		return errFollowUpTUIBack
	}

	options := make([]huh.Option[string], 0, len(steps))
	for _, step := range steps {
		label := fmt.Sprintf("%-16s %s", step.Name, step.Run)
		if step.Dir != "" {
			label += "  (dir: " + step.Dir + ")"
		}
		options = append(options, huh.NewOption(label, step.Name))
	}

	var name string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Remove follow-up").
			Description(scopeDescription(scope)).
			Options(options...).
			Value(&name),
	))
	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return errFollowUpTUIBack
		}
		return err
	}

	var confirm bool
	confirmForm := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Remove %q?", name)).
			Affirmative("Remove").
			Negative("Keep").
			Value(&confirm),
	))
	if err := theme.RunForm(ctx, confirmForm); err != nil {
		if theme.IsCancelled(err) {
			return errFollowUpTUIBack
		}
		return err
	}
	if !confirm {
		return errFollowUpTUIBack
	}

	if err := config.RemoveFreshFollowUp(ctx, campRoot, scopeProjectName(scope), name); err != nil {
		return err
	}
	fmt.Println(ui.Success(fmt.Sprintf("Removed %q from %s.", name, scopeDescription(scope))))
	return nil
}

func selectFollowUpScope(ctx context.Context, projects []project.Project, cfg *config.FreshConfig) (string, error) {
	options := []huh.Option[string]{huh.NewOption("Global default (all projects)", globalFollowUpScope)}

	names := make(map[string]bool)
	for _, p := range projects {
		names[p.Name] = true
	}
	if cfg != nil {
		for name, pc := range cfg.Projects {
			if pc.FollowUp != nil {
				names[name] = true
			}
		}
	}
	projectNames := make([]string, 0, len(names))
	for name := range names {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)
	for _, name := range projectNames {
		label := name
		if cfg != nil {
			if steps := cfg.Projects[name].FollowUp; steps != nil {
				label = fmt.Sprintf("%s (%d configured)", name, len(steps))
			}
		}
		options = append(options, huh.NewOption(label, name))
	}

	var scope string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Where should this apply?").
			Description("Global commands apply to projects without their own override.").
			Options(options...).
			Value(&scope),
	))
	if err := theme.RunForm(ctx, form); err != nil {
		return "", err
	}
	return scope, nil
}

func requiredField(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

func scopeProjectName(scope string) string {
	if scope == globalFollowUpScope {
		return ""
	}
	return scope
}

func scopeDescription(scope string) string {
	if scope == globalFollowUpScope {
		return "the global default"
	}
	return "project " + scope
}

func followUpsForScope(cfg *config.FreshConfig, scope string) []config.FollowUpConfig {
	if scope == globalFollowUpScope {
		return cfg.FollowUp
	}
	return cfg.Projects[scope].FollowUp
}

func configuredProjectNames(cfg *config.FreshConfig) []string {
	names := make([]string, 0, len(cfg.Projects))
	for name, pc := range cfg.Projects {
		if pc.FollowUp != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
