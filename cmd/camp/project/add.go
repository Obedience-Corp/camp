package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// noOptProjectCampaign is the NoOptDefVal for the --campaign flag.
// A bare --campaign opens the shared picker in interactive terminals.
const noOptProjectCampaign = "\x00pick"

var projectAddCmd = &cobra.Command{
	Use:   "add [source]",
	Short: "Add a project to campaign",
	Long: `Add a git repository as a project in the campaign.

The project is cloned as a git submodule into the projects/ directory.
A worktree directory is also created for future parallel development.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or a bare --campaign to
select a registered target campaign.

Source can be:
  - SSH URL:   git@github.com:org/repo.git
  - HTTPS URL: https://github.com/org/repo.git
  - Local path (with --local): ./existing-repo
  - Local path (with --link):  ~/code/my-project (prefer 'camp project link')

Examples:
  camp project add git@github.com:org/api.git           # Add remote repo
  camp project add https://github.com/org/web.git       # Add via HTTPS
  camp project add --local ./my-repo --name my-project  # Add existing local repo
  camp project add --link ~/code/my-project             # Link an existing project
  camp project link ~/code/my-project                   # Preferred linked-project command
  camp project add --campaign platform --local ./my-repo # Add outside current campaign
  camp project add --campaign --link ~/code/my-project   # Pick a target campaign
  camp project add git@github.com:org/api.git --name backend  # Custom name`,
	Args: validateProjectAddArgs,
	RunE: runProjectAdd,
}

func init() {
	Cmd.AddCommand(projectAddCmd)

	flags := projectAddCmd.Flags()
	flags.StringP("name", "n", "", "Override project name (defaults to repo name)")
	flags.StringP("path", "p", "", "Override destination path (defaults to projects/<name>)")
	flags.StringP("local", "l", "", "Add existing local repository instead of cloning")
	flags.String("link", "", "Link an existing local directory without cloning (prefer 'camp project link')")
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	flags.Bool("no-commit", false, "Skip automatic git commit")
	flags.Lookup("campaign").NoOptDefVal = noOptProjectCampaign
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	name, _ := cmd.Flags().GetString("name")
	path, _ := cmd.Flags().GetString("path")
	local, _ := cmd.Flags().GetString("local")
	link, _ := cmd.Flags().GetString("link")
	targetCampaign, _ := cmd.Flags().GetString("campaign")
	noCommit, _ := cmd.Flags().GetBool("no-commit")
	targetCampaign, args = normalizeProjectAddCampaignArgs(args, targetCampaign, local, link)

	if local != "" && link != "" {
		return fmt.Errorf("use either --local or --link, not both")
	}

	source := ""
	switch {
	case link != "":
		// Linked adds take their source from --link.
	case local != "":
		source = local
	case len(args) == 0:
		return fmt.Errorf("source URL is required")
	default:
		source = args[0]
	}

	campaignResolver := newProjectCampaignResolver(cmd.ErrOrStderr(), "camp project add --campaign <name> [source]")
	cfg, root, err := campaignResolver.resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
	}

	if link != "" {
		result, err := linkProject(ctx, root, link, name, path)
		if err != nil {
			return err
		}
		printLinkedProjectResult(result)
		return nil
	}

	opts := projectsvc.AddOptions{
		Name:  name,
		Path:  path,
		Local: local,
	}

	result, err := projectsvc.Add(ctx, root, source, opts)
	if err != nil {
		// Check if it's a GitError and format it nicely
		var gitErr *projectsvc.GitError
		if errors.As(err, &gitErr) {
			return formatGitError(gitErr)
		}
		return err
	}

	// Print result
	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Added project: "+result.Name))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Path:", result.Path))
	fmt.Println(ui.KeyValue("  Source:", result.Source))
	if result.Type != "" {
		fmt.Println(ui.KeyValue("  Type:", result.Type))
	}

	// Auto-commit if not disabled
	if !noCommit {
		campaignID := ""
		if cfg != nil {
			campaignID = cfg.ID
		}
		files := commit.NormalizeFiles(root, ".gitmodules", result.Path)
		commitResult := commit.Project(ctx, commit.ProjectOptions{
			Options: commit.Options{
				CampaignRoot:  root,
				CampaignID:    campaignID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.ProjectAdd,
			ProjectName: result.Name,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}

func validateProjectAddArgs(cmd *cobra.Command, args []string) error {
	maxArgs := 1

	targetCampaign, _ := cmd.Flags().GetString("campaign")
	local, _ := cmd.Flags().GetString("local")
	link, _ := cmd.Flags().GetString("link")
	if targetCampaign == noOptProjectCampaign && local == "" && link == "" {
		maxArgs = 2
	}

	return cobra.MaximumNArgs(maxArgs)(cmd, args)
}

func normalizeProjectAddCampaignArgs(args []string, targetCampaign, local, link string) (string, []string) {
	if targetCampaign != noOptProjectCampaign {
		return targetCampaign, args
	}

	switch {
	case len(args) == 0:
		return "", args
	case local != "" || link != "":
		return args[0], args[1:]
	case len(args) > 1:
		return args[0], args[1:]
	case looksLikeProjectAddSource(args[0]):
		return "", args
	default:
		return args[0], args[1:]
	}
}

func looksLikeProjectAddSource(arg string) bool {
	return strings.HasPrefix(arg, "git@") ||
		strings.Contains(arg, "://") ||
		strings.HasPrefix(arg, "/") ||
		strings.HasPrefix(arg, "./") ||
		strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, "~")
}

type projectCampaignResolver struct {
	stderr        io.Writer
	usageLine     string
	isInteractive func() bool
	loadCurrent   func(context.Context) (*config.CampaignConfig, string, error)
	loadRegistry  func(context.Context) (*config.Registry, error)
	loadCampaign  func(context.Context, string) (*config.CampaignConfig, error)
	saveRegistry  func(context.Context, *config.Registry) error
	pickCampaign  func(context.Context, *config.Registry) (config.RegisteredCampaign, error)
}

func newProjectCampaignResolver(stderr io.Writer, usageLine string) projectCampaignResolver {
	return projectCampaignResolver{
		stderr:        stderr,
		usageLine:     usageLine,
		isInteractive: navtui.IsTerminal,
		loadCurrent:   config.LoadCampaignConfigFromCwd,
		loadRegistry:  config.LoadRegistry,
		loadCampaign:  config.LoadCampaignConfig,
		saveRegistry:  config.SaveRegistry,
		pickCampaign:  cmdutil.PickCampaign,
	}
}

func (r projectCampaignResolver) resolve(ctx context.Context, targetCampaign string, targetChanged bool) (*config.CampaignConfig, string, error) {
	if targetCampaign == noOptProjectCampaign {
		targetCampaign = ""
	}

	if !targetChanged {
		cfg, campaignRoot, err := r.loadCurrent(ctx)
		if err == nil {
			reg, regErr := r.loadRegistry(ctx)
			if regErr != nil {
				return nil, "", camperrors.Wrap(regErr, "load registry")
			}
			if err := ensureProjectCampaignRegistered(reg, cfg, campaignRoot); err != nil {
				return nil, "", err
			}
			return cfg, campaignRoot, nil
		}
	}

	reg, err := r.loadRegistry(ctx)
	if err != nil {
		return nil, "", camperrors.Wrap(err, "load registry")
	}
	if reg.Len() == 0 {
		return nil, "", fmt.Errorf("no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign
	switch {
	case targetCampaign == "":
		if !r.isInteractive() {
			return nil, "", fmt.Errorf("campaign name required in non-interactive mode\n       Usage: %s", r.usage())
		}
		selected, err = r.pickCampaign(ctx, reg)
		if err != nil {
			return nil, "", err
		}
	default:
		selected, err = cmdutil.ResolveCampaignSelection(targetCampaign, reg, r.stderr)
		if err != nil {
			return nil, "", err
		}
	}

	cfg, err := r.loadCampaign(ctx, selected.Path)
	if err != nil {
		return nil, "", camperrors.Wrapf(err, "load target campaign %s", selected.Path)
	}
	if err := ensureProjectCampaignRegistered(reg, cfg, selected.Path); err != nil {
		return nil, "", err
	}

	reg.UpdateLastAccess(selected.ID)
	if r.saveRegistry != nil {
		_ = r.saveRegistry(ctx, reg)
	}

	return cfg, selected.Path, nil
}

func (r projectCampaignResolver) usage() string {
	if strings.TrimSpace(r.usageLine) == "" {
		return "camp project add --campaign <name> [source]"
	}
	return r.usageLine
}

func ensureProjectCampaignRegistered(reg *config.Registry, cfg *config.CampaignConfig, campaignRoot string) error {
	if cfg == nil {
		return fmt.Errorf("target campaign config could not be loaded")
	}

	normalizedRoot, err := normalizeProjectCampaignRoot(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolve target campaign root")
	}

	for _, entry := range reg.ListAll() {
		if entry.ID != cfg.ID {
			continue
		}

		normalizedEntryPath, err := normalizeProjectCampaignRoot(entry.Path)
		if err != nil {
			continue
		}
		if normalizedEntryPath == normalizedRoot {
			return nil
		}
	}

	name := cfg.Name
	if strings.TrimSpace(name) == "" {
		name = normalizedRoot
	}
	return fmt.Errorf("target campaign %q is not registered\n       Run 'camp register %s' before adding projects", name, normalizedRoot)
}

func normalizeProjectCampaignRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolvedRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		return resolvedRoot, nil
	}
	return absRoot, nil
}

// formatGitError formats a GitError with nice visual indicators.
func formatGitError(gitErr *projectsvc.GitError) error {
	var b strings.Builder

	// Header with X indicator
	b.WriteString(ui.ErrorIcon())
	b.WriteString(" ")
	b.WriteString(ui.Error(gitErr.Diagnosis))
	b.WriteString("\n")

	// Fix instructions if present
	if gitErr.Fix != "" {
		b.WriteString("\n")
		b.WriteString(ui.Info(gitErr.Fix))
		b.WriteString("\n")
	}

	// Documentation link if present
	if gitErr.DocLink != "" {
		b.WriteString("\n")
		b.WriteString(ui.Label("Documentation:"))
		b.WriteString(" ")
		b.WriteString(ui.Accent(gitErr.DocLink))
		b.WriteString("\n")
	}

	return fmt.Errorf("%s", b.String())
}
