package linked

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
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
func Add(ctx context.Context, campaignRoot, sourcePath, name, destPath string) (*projectsvc.LinkResult, error) {
	return projectsvc.AddLinked(ctx, campaignRoot, sourcePath, projectsvc.LinkOptions{
		Name: name,
		Path: destPath,
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
	fmt.Println()
	fmt.Println(ui.Dim("  Linked projects are tracked in campaign git history. Linked git repos can still be committed with camp project commit."))
}

// CommitAdd records a linked-project add in the campaign repo.
func CommitAdd(ctx context.Context, cfg *config.CampaignConfig, campaignRoot, projectPath, projectName string) commit.Result {
	return commitLinkedChange(ctx, cfg, campaignRoot, commit.ProjectAdd, projectPath, projectName)
}

// CommitRemove records a linked-project unlink in the campaign repo.
func CommitRemove(ctx context.Context, cfg *config.CampaignConfig, campaignRoot, projectPath, projectName string) commit.Result {
	return commitLinkedChange(ctx, cfg, campaignRoot, commit.ProjectRemove, projectPath, projectName)
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
