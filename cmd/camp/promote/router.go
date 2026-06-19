package promote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	item, resolved, err := resolveItem(ctx, campaignRoot, resolver, args, interactive)
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
	return dispatch(cmd, item, campaignRoot, target, interactive, dryRun, jsonOut, pass)
}

func resolveItem(ctx context.Context, campaignRoot string, resolver *paths.Resolver, args []string, interactive bool) (workitem.WorkItem, bool, error) {
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

	if !interactive {
		return workitem.WorkItem{}, false, nil
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

func dispatch(cmd *cobra.Command, item workitem.WorkItem, campaignRoot, target string, interactive, dryRun, jsonOut bool, pass []string) error {
	ctx := cmd.Context()
	kind := kindForType(item.WorkflowType)

	if target == "" && interactive {
		picked, perr := pickTarget(ctx, kind)
		if perr != nil {
			return perr
		}
		target = picked
	}

	display := target
	switch kind {
	case kindFestival:
		if _, ok := festivalTarget(target); !ok {
			return camperrors.New("festival target must be next, completed, archived, or someday")
		}
		if display == "" {
			display = "next"
		}
	case kindIntent, kindWorkitem:
		if target == "" {
			return camperrors.New("--target is required in non-interactive mode")
		}
	}

	if dryRun {
		return reportPlan(cmd, jsonOut, kind, item, display)
	}

	out := cmd.OutOrStdout()
	switch kind {
	case kindFestival:
		status, _ := festivalTarget(target)
		return dispatchFestival(ctx, item.AbsPath(campaignRoot), festPassthrough(status, pass), out)
	case kindIntent:
		ipass := intentPassthrough(pass)
		if jsonOut {
			if err := dispatchIntent(ctx, item.SourceID, target, ipass, io.Discard); err != nil {
				return err
			}
			return reportResult(cmd, kind, item, display)
		}
		return dispatchIntent(ctx, item.SourceID, target, ipass, out)
	case kindWorkitem:
		return dispatchWorkitem(ctx, item.AbsPath(campaignRoot), target, pass, out)
	}
	return camperrors.New("unknown promote kind")
}

type routerResult struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title"`
	Target string `json:"target"`
	DryRun bool   `json:"dry_run"`
	OK     bool   `json:"ok"`
}

func reportPlan(cmd *cobra.Command, jsonOut bool, kind promoteKind, item workitem.WorkItem, target string) error {
	if !jsonOut {
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"dry-run: would promote %q (%s) to %s; no changes made\n", item.Title, item.WorkflowType, target)
		return err
	}
	return encodeResult(cmd, routerResult{
		Kind: kindName(kind), ID: item.Key, Title: item.Title, Target: target, DryRun: true, OK: true,
	})
}

func reportResult(cmd *cobra.Command, kind promoteKind, item workitem.WorkItem, target string) error {
	return encodeResult(cmd, routerResult{
		Kind: kindName(kind), ID: item.Key, Title: item.Title, Target: target, OK: true,
	})
}

func encodeResult(cmd *cobra.Command, res routerResult) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func kindName(kind promoteKind) string {
	switch kind {
	case kindIntent:
		return "intent"
	case kindFestival:
		return "festival"
	default:
		return "workitem"
	}
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
