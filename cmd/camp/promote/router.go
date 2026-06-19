package promote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
)

var RouterCmd = &cobra.Command{
	Use:     "promote [id]",
	Short:   "Promote any intent, workitem, or festival (universal front door)",
	GroupID: "planning",
	Args:    cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Dispatches to a type-specific promote; requires an explicit id and --target in non-interactive use",
	},
	RunE: runRouter,
}

func init() {
	addPromoteRouterFlags(RouterCmd)
	RouterCmd.ValidArgsFunction = completePromotable
}

func addPromoteRouterFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("target", "", "Promote target (kind-specific); required in non-interactive mode")
	flags.Bool("json", false, "Machine-readable output; implies non-interactive")
	flags.Bool("force", false, "Skip readiness checks")
	flags.Bool("dry-run", false, "Preview without making changes")
	flags.Bool("no-commit", false, "Skip auto-commit")
	flags.Bool("keep", false, "Keep the source (festival/doc targets)")
	flags.String("dest", "", "Destination override (doc/festival targets)")
	flags.String("goal", "", "Festival goal override")
}

func runRouter(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	target, _ := cmd.Flags().GetString("target")
	jsonOut, _ := cmd.Flags().GetBool("json")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	interactive := ui.IsTerminal() && !jsonOut

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

	item, resolved, err := resolveItem(ctx, campaignRoot, resolver, args)
	if err != nil {
		return err
	}
	if !resolved {
		if !interactive {
			return camperrors.New("no item in context: pass an id (and --target) or run camp promote in an interactive terminal")
		}
		item, err = selectItem(ctx, campaignRoot, resolver)
		if err != nil {
			return err
		}
	}

	pass := passthroughFlags(cmd)
	return dispatch(cmd, item, campaignRoot, target, interactive, dryRun, pass)
}

func resolveItem(ctx context.Context, campaignRoot string, resolver *paths.Resolver, args []string) (workitem.WorkItem, bool, error) {
	pool, err := promotablePool(ctx, campaignRoot, resolver)
	if err != nil {
		return workitem.WorkItem{}, false, err
	}

	if len(args) == 1 {
		for _, it := range pool {
			if it.SourceID == args[0] || it.Key == args[0] || it.StableID == args[0] {
				return it, true, nil
			}
		}
		return workitem.WorkItem{}, false, camperrors.Wrapf(camperrors.New("not a promotable item"), "id %q", args[0])
	}

	cwd, err := os.Getwd()
	if err != nil {
		return workitem.WorkItem{}, false, camperrors.Wrap(err, "getting current directory")
	}
	if loc, derr := locate.DetectFromCwd(campaignRoot, cwd); derr == nil {
		for _, it := range pool {
			if it.WorkflowType == workitem.WorkflowTypeIntent || it.WorkflowType == workitem.WorkflowTypeFestival {
				continue
			}
			if string(it.WorkflowType) == loc.Type && filepath.Base(it.RelativePath) == loc.Slug {
				return it, true, nil
			}
		}
	}
	return workitem.WorkItem{}, false, nil
}

func dispatch(cmd *cobra.Command, item workitem.WorkItem, campaignRoot, target string, interactive, dryRun bool, pass []string) error {
	ctx := cmd.Context()
	kind := kindForType(item.WorkflowType)

	if target == "" && interactive {
		picked, perr := pickTarget(ctx, kind)
		if perr != nil {
			return perr
		}
		target = picked
	}

	switch kind {
	case kindFestival:
		if _, ok := festivalTarget(target); !ok {
			return camperrors.New("festival target must be next, completed, archived, or someday")
		}
		display := target
		if display == "" {
			display = "next"
		}
		if dryRun {
			return printDryRun(cmd, item, display)
		}
		status, _ := festivalTarget(target)
		return dispatchFestival(ctx, item.AbsPath(campaignRoot), festPassthrough(status, pass))
	case kindIntent:
		if target == "" {
			return camperrors.New("--target is required in non-interactive mode")
		}
		if dryRun {
			return printDryRun(cmd, item, target)
		}
		return dispatchIntent(ctx, item.SourceID, target)
	case kindWorkitem:
		if target == "" {
			return camperrors.New("--target is required in non-interactive mode")
		}
		if dryRun {
			return printDryRun(cmd, item, target)
		}
		return dispatchWorkitem(ctx, item.AbsPath(campaignRoot), target, pass)
	}
	return camperrors.New("unknown promote kind")
}

func printDryRun(cmd *cobra.Command, item workitem.WorkItem, target string) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(),
		"dry-run: would promote %q (%s) to %s; no changes made\n", item.Title, item.WorkflowType, target)
	return err
}

func festivalTarget(target string) (string, bool) {
	switch target {
	case "", "next":
		return "", true
	case "completed", "archived", "someday":
		return target, true
	}
	return "", false
}

func passthroughFlags(cmd *cobra.Command) []string {
	var pass []string
	for _, name := range []string{"force", "no-commit", "keep", "json"} {
		if cmd.Flags().Changed(name) {
			pass = append(pass, "--"+name)
		}
	}
	for _, name := range []string{"dest", "goal"} {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetString(name)
			pass = append(pass, "--"+name, v)
		}
	}
	return pass
}
