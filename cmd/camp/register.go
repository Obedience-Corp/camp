package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/scaffold"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register [path]",
	Short: "Register campaign in global registry",
	Long: `Register an existing campaign in the global registry.

This adds the campaign to the registry at ~/.config/obey/campaign/registry.yaml,
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
	Aliases: []string{"reg"},
	Args:    cobra.MaximumNArgs(1),
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
		return fmt.Errorf("invalid path: %w", err)
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
			fmt.Printf("%s Initialized and registered campaign at %s\n", ui.SuccessIcon(), ui.Value(result.CampaignRoot))
			return nil
		}
		fmt.Println(ui.Dim("Aborted."))
		return nil
	}

	// Load campaign config for name/type
	cfg, err := config.LoadCampaignConfig(ctx, absPath)
	if err != nil {
		return fmt.Errorf("failed to load campaign config: %w", err)
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
		return fmt.Errorf("invalid campaign type: %s (must be product, research, tools, or personal)", typeFlag)
	}

	// Load registry
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return err
	}

	// Check for existing registration with same name but different path
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
		// Remove the old entry before adding new one
		reg.UnregisterByID(existing.ID)
	}

	// Check for path conflict (different campaign ID at same path)
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
		// Remove the conflicting entry before adding new one
		reg.UnregisterByID(existing.ID)
	}

	// Register using campaign ID
	if err := reg.Register(cfg.ID, name, absPath, ctype); err != nil {
		return fmt.Errorf("failed to register campaign: %w", err)
	}

	// Save registry
	if err := config.SaveRegistry(ctx, reg); err != nil {
		return err
	}

	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Registered: "+name))
	fmt.Println(ui.KeyValue("Path:", absPath))
	fmt.Println(ui.KeyValue("Campaign ID:", cfg.ID))
	return nil
}
