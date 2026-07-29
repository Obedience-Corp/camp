package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register [path]",
	Short: "Register campaign in global registry",
	Long: `Register an existing campaign in the global registry.

This adds the campaign to the registry at ~/.obey/campaign/registry.json,
enabling it to appear in 'camp list' and be accessible via navigation commands.

Note: 'camp init' automatically registers new campaigns. This command is for
registering existing campaigns that weren't created with camp or were unregistered.

If the specified path is not a campaign (has no .campaign/ directory),
you'll be offered the option to initialize it.

Examples:
  camp register                          # Register current directory
  camp register ~/Dev/my-project         # Register specified path
  camp register . --name custom-name     # Override the campaign name
  camp register . --type research        # Override the campaign type`,
	Args: cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Global registry modification",
	},
	RunE: runRegister,
}

func init() {
	rootCmd.AddCommand(registerCmd)
	registerCmd.GroupID = "setup"

	registerCmd.Flags().StringP("name", "n", "", "Override campaign name")
	registerCmd.Flags().StringP("type", "t", "", "Override campaign type (product, research, tools, personal)")
}

func runRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Determine path
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return camperrors.Wrap(err, "invalid path")
	}

	// Check .campaign/ exists
	campaignDir := filepath.Join(absPath, config.CampaignDir)
	if _, err := os.Stat(campaignDir); os.IsNotExist(err) {
		fmt.Printf("%s No campaign found at %s\n", ui.WarningIcon(), ui.Dim(absPath))
		fmt.Print("Would you like to initialize one? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "y" || response == "yes" {
			// Delegate to init
			nameFlag, _ := cmd.Flags().GetString("name")
			typeFlag, _ := cmd.Flags().GetString("type")
			opts := scaffold.InitOptions{
				Name: nameFlag,
			}
			if typeFlag != "" {
				opts.Type = config.CampaignType(typeFlag)
			}
			result, err := scaffold.Init(ctx, absPath, opts)
			if err != nil {
				return err
			}
			// Scaffolding alone leaves a campaign without festivals/, which is
			// the same campaign `camp init` would have set up completely. A
			// missing fest CLI is reported by InitializeFestivals and is not
			// fatal, matching how init treats it.
			if _, festErr := initcmd.InitializeFestivals(ctx, result.CampaignRoot, initcmd.Writers{HumanOut: os.Stdout}); festErr != nil {
				if !errors.Is(festErr, fest.ErrFestNotFound) {
					return festErr
				}
			}
			fmt.Printf("%s Initialized and registered campaign at %s\n", ui.SuccessIcon(), ui.Value(result.CampaignRoot))
			return nil
		}
		fmt.Println(ui.Dim("Aborted."))
		return nil
	}

	// Load campaign config for name/type
	cfg, err := config.LoadCampaignConfig(ctx, absPath)
	if err != nil {
		return camperrors.Wrap(err, "failed to load campaign config")
	}

	// Allow overrides from flags
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		name = cfg.Name
	}
	typeFlag, _ := cmd.Flags().GetString("type")
	var ctype config.CampaignType
	if typeFlag != "" {
		ctype = config.CampaignType(typeFlag)
	} else {
		ctype = cfg.Type
	}

	// Validate type if specified
	if typeFlag != "" && !ctype.Valid() {
		return camperrors.Wrapf(camperrors.ErrInvalidInput, "invalid campaign type: %s (must be product, research, tools, or personal)", typeFlag)
	}

	// Load registry
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	confirmed := registerConflictConfirmations{}

	// Check for existing registration with same name but different path.
	if existing, exists := reg.GetByName(name); exists && existing.Path != absPath {
		fmt.Printf("%s Campaign '%s' already registered at %s\n", ui.WarningIcon(), ui.Value(name), ui.Dim(existing.Path))
		fmt.Print("Replace with new path? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println(ui.Dim("Aborted."))
			return nil
		}
		confirmed.nameConflictID = existing.ID
	}

	// Check for path conflict (different campaign ID at same path).
	if existing, exists := reg.FindByPath(absPath); exists && existing.ID != cfg.ID {
		fmt.Printf("%s Path already registered to different campaign\n", ui.WarningIcon())
		fmt.Println(ui.KeyValue("Existing:", existing.Name+" ("+existing.ID[:8]+"...)"))
		fmt.Println(ui.KeyValue("New:", name+" ("+cfg.ID[:8]+"...)"))
		fmt.Print("Replace existing registration? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println(ui.Dim("Aborted."))
			return nil
		}
		confirmed.pathConflictID = existing.ID
	}

	if err := config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		return registerCampaignWithConfirmedConflicts(reg, cfg.ID, name, absPath, ctype, confirmed)
	}); err != nil {
		return err
	}

	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Registered: "+name))
	fmt.Println(ui.KeyValue("Path:", absPath))
	fmt.Println(ui.KeyValue("Campaign ID:", cfg.ID))
	return nil
}

type registerConflictConfirmations struct {
	nameConflictID string
	pathConflictID string
}

func registerCampaignWithConfirmedConflicts(reg *config.Registry, id, name, absPath string, ctype config.CampaignType, confirmed registerConflictConfirmations) error {
	if existing, exists := reg.GetByName(name); exists && existing.Path != absPath {
		if confirmed.nameConflictID == "" {
			return camperrors.Wrapf(camperrors.ErrConflict, "campaign %q is now registered at %s; re-run camp register to confirm replacement", name, existing.Path)
		}
		if existing.ID != confirmed.nameConflictID {
			return camperrors.Wrapf(camperrors.ErrConflict, "campaign %q registration changed from %s to %s; re-run camp register to confirm replacement", name, confirmed.nameConflictID, existing.ID)
		}
		reg.UnregisterByID(existing.ID)
	}
	if existing, exists := reg.FindByPath(absPath); exists && existing.ID != id {
		if confirmed.pathConflictID == "" {
			return camperrors.Wrapf(camperrors.ErrConflict, "path %s is now registered to campaign %s (%s); re-run camp register to confirm replacement", absPath, existing.Name, existing.ID)
		}
		if existing.ID != confirmed.pathConflictID {
			return camperrors.Wrapf(camperrors.ErrConflict, "path %s registration changed from %s to %s; re-run camp register to confirm replacement", absPath, confirmed.pathConflictID, existing.ID)
		}
		reg.UnregisterByID(existing.ID)
	}
	if err := reg.Register(id, name, absPath, ctype); err != nil {
		return camperrors.Wrap(err, "failed to register campaign")
	}
	return nil
}
