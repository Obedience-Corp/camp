package linked

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// NoOptCampaign is the NoOptDefVal for the --campaign flag.
// A bare --campaign opens the shared picker in interactive terminals.
const NoOptCampaign = "\x00pick"

// CampaignResolver selects a target campaign for commands that can operate
// inside or outside current campaign context.
type CampaignResolver interface {
	Resolve(ctx context.Context, targetCampaign string, targetChanged bool) (*config.CampaignConfig, string, error)
}

// CampaignResolverFactory creates a campaign resolver for a command instance.
type CampaignResolverFactory func(stderr io.Writer, usageLine string) CampaignResolver

// Add links a local directory into the selected campaign.
func Add(ctx context.Context, campaignRoot, sourcePath, name string) (*projectsvc.LinkResult, error) {
	return projectsvc.AddLinked(ctx, campaignRoot, sourcePath, projectsvc.LinkOptions{
		Name: name,
	})
}

// PrintResult renders a linked-project success result.
func PrintResult(result *projectsvc.LinkResult) {
	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Linked project: "+result.Name))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Path:", result.Path))
	fmt.Println(ui.KeyValue("  Source:", result.Source))
	if result.Type != "" {
		fmt.Println(ui.KeyValue("  Type:", result.Type))
	}
	if result.IsGit {
		fmt.Println(ui.KeyValue("  Git:", "yes"))
	} else {
		fmt.Println(ui.KeyValue("  Git:", "no"))
	}
	printWarnings(result.Warnings)
	fmt.Println()
	fmt.Println(ui.Dim("  Linked projects are tracked in campaign git history. Linked git repos can still be committed with camp project commit."))
}

func printWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Println()
	for _, warning := range warnings {
		fmt.Printf("%s %s\n", ui.WarningIcon(), ui.Warning(warning))
	}
}

// CommitLink records a linked-project add in the campaign repo.
func CommitLink(ctx context.Context, cfg *config.CampaignConfig, campaignRoot, projectPath, projectName string) commit.Result {
	return commitLinkedChange(ctx, cfg, campaignRoot, commit.ProjectLink, projectPath, projectName)
}

// CommitUnlink records a linked-project unlink in the campaign repo.
func CommitUnlink(ctx context.Context, cfg *config.CampaignConfig, campaignRoot, projectPath, projectName string) commit.Result {
	return commitLinkedChange(ctx, cfg, campaignRoot, commit.ProjectUnlink, projectPath, projectName)
}

func commitLinkedChange(
	ctx context.Context,
	cfg *config.CampaignConfig,
	campaignRoot string,
	action commit.ProjectAction,
	projectPath string,
	projectName string,
) commit.Result {
	campaignID := ""
	if cfg != nil {
		campaignID = cfg.ID
	}

	return commit.Project(ctx, commit.ProjectOptions{
		Options: commit.Options{
			CampaignRoot:  campaignRoot,
			CampaignID:    campaignID,
			Files:         commit.NormalizeFiles(campaignRoot, projectPath),
			SelectiveOnly: true,
		},
		Action:      action,
		ProjectName: projectName,
	})
}

func validateLinkArgs(cmd *cobra.Command, args []string) error {
	maxArgs := 1
	targetCampaign, _ := cmd.Flags().GetString("campaign")
	if targetCampaign == NoOptCampaign {
		maxArgs = 2
	}
	return cobra.MaximumNArgs(maxArgs)(cmd, args)
}

func validateUnlinkArgs(cmd *cobra.Command, args []string) error {
	maxArgs := 1
	targetCampaign, _ := cmd.Flags().GetString("campaign")
	if targetCampaign == NoOptCampaign {
		maxArgs = 2
	}
	return cobra.MaximumNArgs(maxArgs)(cmd, args)
}

func normalizeLinkCampaignArgs(args []string, targetCampaign string) (string, []string) {
	if targetCampaign != NoOptCampaign {
		return targetCampaign, args
	}

	switch {
	case len(args) == 0:
		return "", args
	case len(args) > 1:
		return args[0], args[1:]
	case looksLikeLinkedPathArg(args[0]):
		return "", args
	default:
		return args[0], args[1:]
	}
}

func normalizeUnlinkCampaignArgs(args []string, targetCampaign string) (string, []string) {
	if targetCampaign != NoOptCampaign {
		return targetCampaign, args
	}
	if len(args) == 0 {
		return "", args
	}
	return args[0], args[1:]
}

func resolveLinkSourcePath(campaignRoot string, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if isWithinTargetCampaign(cwd, campaignRoot) {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "link path required when current directory is already inside the target campaign")
	}
	return cwd, nil
}

func resolveUnlinkName(ctx context.Context, campaignRoot string, args []string) (string, error) {
	if len(args) > 0 {
		return strings.TrimPrefix(args[0], "projects/"), nil
	}

	resolved, err := projectsvc.Resolve(ctx, campaignRoot, "")
	if err != nil {
		return "", camperrors.Wrap(err, "could not infer linked project from current directory\n       Use 'camp project unlink <name>' to target it explicitly")
	}
	return strings.TrimPrefix(resolved.Name, "projects/"), nil
}

func looksLikeLinkedPathArg(arg string) bool {
	return strings.HasPrefix(arg, "/") ||
		strings.HasPrefix(arg, "./") ||
		strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, "~") ||
		strings.Contains(arg, string(filepath.Separator))
}

func isWithinTargetCampaign(path, campaignRoot string) bool {
	normalizedPath, err := normalizeLocalPath(path)
	if err != nil {
		return false
	}
	normalizedRoot, err := normalizeLocalPath(campaignRoot)
	if err != nil {
		return false
	}
	if normalizedPath == normalizedRoot {
		return true
	}
	rel, err := filepath.Rel(normalizedRoot, normalizedPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func normalizeLocalPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved, nil
	}
	return absPath, nil
}
