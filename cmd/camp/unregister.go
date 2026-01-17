package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/spf13/cobra"
)

var unregisterCmd = &cobra.Command{
	Use:   "unregister <name-or-id>",
	Short: "Remove campaign from registry",
	Long: `Remove a campaign from the global registry.

This does NOT delete any files - it only removes the campaign from
tracking in the global registry. Use this when:
  - A campaign directory was deleted manually
  - A campaign was moved to a different location
  - You no longer want to track a campaign

The campaign files remain untouched on disk.

You can specify the campaign by name or ID (or ID prefix).

Examples:
  camp unregister old-project            # Remove by name
  camp unregister 550e84                 # Remove by ID prefix
  camp unregister old-project --force    # Remove without confirmation`,
	Aliases: []string{"unreg"},
	Args:    cobra.ExactArgs(1),
	RunE:    runUnregister,
}

func init() {
	rootCmd.AddCommand(unregisterCmd)

	unregisterCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
}

func runUnregister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	query := args[0]

	force, _ := cmd.Flags().GetBool("force")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return err
	}

	// Find campaign by ID, ID prefix, or name
	campaign, exists := reg.Get(query)
	if !exists {
		return fmt.Errorf("campaign %q not found in registry\n"+
			"Hint: Run 'camp list' to see registered campaigns", query)
	}

	// Confirm unless forced
	if !force {
		fmt.Printf("Unregister campaign '%s' (ID: %s) at %s? [y/N] ", campaign.Name, campaign.ID[:8], campaign.Path)
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Remove from registry by ID
	reg.UnregisterByID(campaign.ID)

	// Save registry
	if err := config.SaveRegistry(ctx, reg); err != nil {
		return err
	}

	fmt.Printf("Unregistered: %s (ID: %s)\n", campaign.Name, campaign.ID[:8])
	fmt.Println("Note: Files were not deleted.")
	return nil
}
