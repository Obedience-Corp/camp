package promote

import (
	"context"
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
	flags := RouterCmd.Flags()
	flags.String("target", "", "Promote target (kind-specific); required in non-interactive mode")
	flags.Bool("json", false, "Machine-readable output; implies non-interactive")
	flags.Bool("force", false, "Skip readiness checks")
	flags.Bool("dry-run", false, "Preview without making changes")
	flags.Bool("no-commit", false, "Skip auto-commit")
	flags.Bool("keep", false, "Keep the source (festival/doc targets)")
	flags.String("dest", "", "Destination override (doc/festival targets)")
	flags.String("goal", "", "Festival goal override")
	RouterCmd.ValidArgsFunction = completePromotable
}

func runRouter(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	target, _ := cmd.Flags().GetString("target")
	jsonOut, _ := cmd.Flags().GetBool("json")
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
	return dispatch(ctx, item, campaignRoot, target, interactive, pass)
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
			if filepath.Base(it.RelativePath) == loc.Slug {
				return it, true, nil
			}
		}
	}
	return workitem.WorkItem{}, false, nil
}

func dispatch(ctx context.Context, item workitem.WorkItem, campaignRoot, target string, interactive bool, pass []string) error {
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
		status, ok := festivalTarget(target)
		if !ok {
			return camperrors.New("festival target must be next, completed, archived, or someday")
		}
		return dispatchFestival(ctx, item.AbsPath(campaignRoot), festPassthrough(status, pass))
	case kindIntent:
		if target == "" {
			return camperrors.New("--target is required in non-interactive mode")
		}
		return dispatchIntent(ctx, item.SourceID, target, pass)
	case kindWorkitem:
		if target == "" {
			return camperrors.New("--target is required in non-interactive mode")
		}
		return dispatchWorkitem(ctx, item.AbsPath(campaignRoot), target, pass)
	}
	return camperrors.New("unknown promote kind")
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
	for _, name := range []string{"force", "dry-run", "no-commit", "keep", "json"} {
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
