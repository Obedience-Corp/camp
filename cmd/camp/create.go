package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new campaign at the default campaigns directory",
	Long:  `Create a new campaign at <campaigns_dir>/<name>/, using the same scaffolding as 'camp init'. The default campaigns directory is ~/campaigns/ and can be configured via 'camp settings' or by editing the campaigns_dir field in ~/.obey/campaign/config.json.`,
	Example: `  camp create my-project
  camp create my-project -d "Description" -m "Mission"
  camp create my-project --path ~/Dev/sandbox
  camp create my-project --dry-run`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive with -d and -m; interactive fallback otherwise",
		"interactive":   "true",
	},
	GroupID: "setup",
	RunE:    runCreate,
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.Flags().StringP("name", "n", "", "Campaign display name (defaults to <name> positional)")
	createCmd.Flags().StringP("type", "t", "product", "Campaign type (product, research, tools, personal)")
	createCmd.Flags().StringP("description", "d", "", "Campaign description")
	createCmd.Flags().StringP("mission", "m", "", "Campaign mission statement")
	createCmd.Flags().Bool("no-git", false, "Skip git repository initialization")
	createCmd.Flags().Bool("dry-run", false, "Show what would be done without creating anything")
	createCmd.Flags().String("path", "", "Override the base campaigns directory (campaign created at <path>/<name>/)")
}

func runCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateCampaignName(name); err != nil {
		return err
	}
	ctx := cmd.Context()
	dryRun := cmdutil.GetFlagBool(cmd, "dry-run")

	base, err := resolveCreateBase(ctx, cmd)
	if err != nil {
		return err
	}

	w := initcmd.ChooseWriters()

	if _, statErr := os.Stat(base); os.IsNotExist(statErr) {
		if dryRun {
			_, _ = fmt.Fprintf(w.HumanOut, "would create base directory: %s\n", base)
		} else {
			if err := os.MkdirAll(base, 0o755); err != nil {
				return camperrors.Wrapf(err, "failed to ensure campaigns directory %s", base)
			}
		}
	}

	target := filepath.Join(base, name)
	if err := checkCreateTarget(target); err != nil {
		return err
	}

	displayName := cmdutil.GetFlagString(cmd, "name")
	if displayName == "" {
		displayName = name
	}

	p := initcmd.Params{
		Dir:         target,
		Name:        displayName,
		TypeStr:     cmdutil.GetFlagString(cmd, "type"),
		Description: cmdutil.GetFlagString(cmd, "description"),
		Mission:     cmdutil.GetFlagString(cmd, "mission"),
		NoGit:       cmdutil.GetFlagBool(cmd, "no-git"),
		DryRun:      dryRun,
		// force, noRegister, repair, yes stay zero — create deliberately does not support them.
	}
	return initcmd.RunFlow(ctx, p, w, tui.IsTerminal())
}

// validateCampaignName returns an error if name is not a valid single-segment
// directory component suitable for use as a campaign name.
func validateCampaignName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return camperrors.New("campaign name is empty")
	}
	if trimmed == "." || trimmed == ".." {
		return camperrors.New(fmt.Sprintf("invalid campaign name: %q", trimmed))
	}
	if strings.HasPrefix(trimmed, ".") {
		return camperrors.New(fmt.Sprintf("campaign name cannot start with '.': %q", trimmed))
	}
	if strings.ContainsAny(trimmed, "/\\") {
		return camperrors.New(fmt.Sprintf("campaign name cannot contain path separators: %q", trimmed))
	}
	return nil
}

// resolveCreateBase returns the absolute base directory in which the new campaign
// directory will be created. When --path is set, that value takes precedence.
// Otherwise the directory comes from GlobalConfig.CampaignsDir (via
// ResolvedCampaignsDir), which defaults to ~/campaigns/ when unset.
func resolveCreateBase(ctx context.Context, cmd *cobra.Command) (string, error) {
	if path := cmdutil.GetFlagString(cmd, "path"); path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", camperrors.Wrapf(err, "resolving --path %q", path)
		}
		return abs, nil
	}
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return "", camperrors.Wrap(err, "loading global config")
	}
	return cfg.ResolvedCampaignsDir(ctx)
}

// checkCreateTarget verifies the target directory is safe to use for a new campaign.
// A missing target or an empty target directory are both accepted. A non-empty target
// that already contains a campaign is rejected with a hint to use 'camp init --repair'.
// A non-empty target without a campaign marker is rejected with a hint to remove it.
func checkCreateTarget(target string) error {
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return camperrors.Wrapf(err, "stating target %q", target)
	}
	if !info.IsDir() {
		return camperrors.New(fmt.Sprintf("target exists and is not a directory: %s", target))
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return camperrors.Wrapf(err, "reading target %q", target)
	}
	if len(entries) == 0 {
		return nil
	}
	campaignMarker := filepath.Join(target, ".campaign")
	if _, err := os.Stat(campaignMarker); err == nil {
		return camperrors.New(fmt.Sprintf("target %s already contains a campaign; use 'camp init --repair %s' to repair it", target, target))
	}
	return camperrors.New(fmt.Sprintf("target %s exists and is not empty; choose a different name or remove the directory", target))
}
