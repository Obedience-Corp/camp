package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage the campaign registry",
	Long: `Manage the campaign registry at ~/.obey/campaign/registry.json.

The registry tracks all known campaigns for quick navigation and lookup.
Use these commands to maintain registry health and resolve issues.

Commands:
  prune   Remove stale entries (campaigns that no longer exist)
  sync    Update registry entry for current campaign
  check   Validate registry integrity

Examples:
  camp registry prune             Remove entries for non-existent campaigns
  camp registry prune --dry-run   Show what would be removed
  camp registry sync              Update path for current campaign
  camp registry check             Check for issues`,
	Aliases: []string{"reg"},
}

var registryPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove stale registry entries",
	Long: `Remove registry entries where the campaign no longer exists.

Checks each registered path and removes entries where:
- The path no longer exists
- The path has no .campaign/ directory

Options:
  --dry-run       Show what would be removed without making changes
  --include-temp  Also remove entries in /tmp/ directories

Examples:
  camp registry prune             Remove stale entries
  camp registry prune --dry-run   Preview what would be removed
  camp registry prune --include-temp  Also clean up test campaigns`,
	RunE: runRegistryPrune,
}

var registrySyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync current campaign with registry",
	Long: `Update the registry entry for the current campaign.

Run this after moving a campaign directory to update its path
in the registry. Reads the campaign ID from .campaign/campaign.yaml
and updates (or adds) the registry entry.

Examples:
  camp registry sync   # Run from inside a campaign`,
	RunE: runRegistrySync,
}

var registryCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check registry integrity",
	Long: `Validate the registry and report any issues found.

Checks for:
- Stale entries (paths that don't exist)
- Missing .campaign/ directories
- Campaigns in /tmp/ directories
- Duplicate entries (multiple IDs pointing to the same path)

Examples:
  camp registry check   Show any registry issues`,
	RunE: runRegistryCheck,
}

func init() {
	rootCmd.AddCommand(registryCmd)
	registryCmd.GroupID = "registry"

	// Add subcommands
	registryCmd.AddCommand(registryPruneCmd)
	registryCmd.AddCommand(registrySyncCmd)
	registryCmd.AddCommand(registryCheckCmd)

	// Prune flags
	registryPruneCmd.Flags().Bool("dry-run", false, "Show what would be removed without making changes")
	registryPruneCmd.Flags().Bool("include-temp", false, "Also remove entries in /tmp/ directories")
}

func runRegistryPrune(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	includeTemp, _ := cmd.Flags().GetBool("include-temp")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	if reg.Len() == 0 {
		fmt.Println(ui.Info("Registry is empty."))
		return nil
	}

	// Find duplicate path entries (multiple IDs pointing to same path)
	pathEntries := make(map[string][]config.RegisteredCampaign)
	for _, c := range reg.ListAll() {
		pathEntries[c.Path] = append(pathEntries[c.Path], c)
	}

	var duplicates []config.RegisteredCampaign
	for _, entries := range pathEntries {
		if len(entries) <= 1 {
			continue
		}
		// Sort by last_access descending (most recent first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].LastAccess.After(entries[j].LastAccess)
		})
		// Mark all but first (most recent) as duplicates
		for i := 1; i < len(entries); i++ {
			dup := entries[i]
			dup.Name = fmt.Sprintf("duplicate path (keeping %s)", entries[0].ID[:8])
			duplicates = append(duplicates, dup)
		}
	}

	// Find stale entries (skip entries already marked as duplicates)
	dupIDs := make(map[string]bool)
	for _, d := range duplicates {
		dupIDs[d.ID] = true
	}

	var stale []config.RegisteredCampaign
	for _, c := range reg.ListAll() {
		if dupIDs[c.ID] {
			continue // Already marked as duplicate
		}

		isStale := false
		reason := ""

		// Check if path exists
		if _, err := os.Stat(c.Path); os.IsNotExist(err) {
			isStale = true
			reason = "path does not exist"
		} else {
			// Check if .campaign/ exists
			campaignDir := filepath.Join(c.Path, config.CampaignDir)
			if _, err := os.Stat(campaignDir); os.IsNotExist(err) {
				isStale = true
				reason = "no .campaign/ directory"
			}
		}

		// Check for temp directories
		if includeTemp && strings.HasPrefix(c.Path, "/tmp/") {
			isStale = true
			reason = "temp directory"
		}

		if isStale {
			c.Name = reason // Reuse Name field to store reason for display
			stale = append(stale, c)
		}
	}

	// Combine duplicates and stale entries
	toRemove := append(duplicates, stale...)

	if len(toRemove) == 0 {
		fmt.Printf("%s Registry is clean (%d campaigns)\n", ui.SuccessIcon(), reg.Len())
		return nil
	}

	// Show what will be removed
	fmt.Printf("%s Found %d entries to remove:\n", ui.WarningIcon(), len(toRemove))
	for _, c := range toRemove {
		fmt.Printf("  %s %s (%s)\n", ui.Dim(c.ID[:8]), ui.Value(c.Path), ui.Dim(c.Name))
	}

	if dryRun {
		fmt.Println()
		fmt.Println(ui.Info("Dry run - no changes made."))
		return nil
	}

	// Confirm removal
	fmt.Print("\nRemove these entries? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println(ui.Dim("Aborted."))
		return nil
	}

	// Remove entries
	for _, c := range toRemove {
		reg.UnregisterByID(c.ID)
	}

	// Save registry
	if err := config.SaveRegistry(ctx, reg); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	fmt.Printf("%s Removed %d entries\n", ui.SuccessIcon(), len(toRemove))
	return nil
}

func runRegistrySync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not inside a campaign: %w", err)
	}

	// Load registry
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	// Check if campaign is already registered by ID
	existing, existsByID := reg.GetByID(cfg.ID)
	if existsByID && existing.Path == campaignRoot {
		fmt.Printf("%s Registry already up to date\n", ui.SuccessIcon())
		fmt.Println(ui.KeyValue("Campaign:", cfg.Name))
		fmt.Println(ui.KeyValue("Path:", campaignRoot))
		return nil
	}

	// Check if path is registered to a DIFFERENT ID
	conflicting, conflictExists := reg.FindByPath(campaignRoot)
	if conflictExists && conflicting.ID != cfg.ID {
		fmt.Printf("%s Path is registered to a different campaign\n", ui.WarningIcon())
		fmt.Println(ui.KeyValue("Current registration:", conflicting.Name+" ("+conflicting.ID[:8]+"...)"))
		fmt.Println(ui.KeyValue("Campaign.yaml ID:", cfg.ID[:8]+"..."))
		fmt.Println()
		fmt.Println("This can happen when:")
		fmt.Println("  • Campaign was re-initialized with a new ID")
		fmt.Println("  • Registry has stale duplicate entries")
		fmt.Println()
		fmt.Print("Update registry to use the campaign.yaml ID? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println(ui.Dim("Aborted."))
			return nil
		}

		// Remove the conflicting entry first
		reg.UnregisterByID(conflicting.ID)
	}

	// Register/update
	if err := reg.Register(cfg.ID, cfg.Name, campaignRoot, cfg.Type); err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}

	// Save registry
	if err := config.SaveRegistry(ctx, reg); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	if conflictExists {
		fmt.Printf("%s Updated registry (replaced conflicting entry)\n", ui.SuccessIcon())
		fmt.Println(ui.KeyValue("Campaign:", cfg.Name))
		fmt.Println(ui.KeyValue("Path:", campaignRoot))
		fmt.Println(ui.KeyValue("New ID:", cfg.ID[:8]+"..."))
	} else if existsByID {
		fmt.Printf("%s Updated registry path\n", ui.SuccessIcon())
		fmt.Println(ui.KeyValue("Campaign:", cfg.Name))
		fmt.Println(ui.KeyValue("Old path:", existing.Path))
		fmt.Println(ui.KeyValue("New path:", campaignRoot))
	} else {
		fmt.Printf("%s Added campaign to registry\n", ui.SuccessIcon())
		fmt.Println(ui.KeyValue("Campaign:", cfg.Name))
		fmt.Println(ui.KeyValue("Path:", campaignRoot))
	}

	return nil
}

func runRegistryCheck(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	if reg.Len() == 0 {
		fmt.Println(ui.Info("Registry is empty."))
		return nil
	}

	var issues []string

	// Check each entry
	staleCount := 0
	tempCount := 0
	for _, c := range reg.ListAll() {
		// Check if path exists
		if _, err := os.Stat(c.Path); os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("Stale: %s (%s) - path does not exist", c.Name, c.Path))
			staleCount++
			continue
		}

		// Check if .campaign/ exists
		campaignDir := filepath.Join(c.Path, config.CampaignDir)
		if _, err := os.Stat(campaignDir); os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("Invalid: %s (%s) - no .campaign/ directory", c.Name, c.Path))
			staleCount++
			continue
		}

		// Check for temp directories
		if strings.HasPrefix(c.Path, "/tmp/") {
			issues = append(issues, fmt.Sprintf("Temp: %s (%s) - test campaign in /tmp/", c.Name, c.Path))
			tempCount++
		}
	}

	// Check for duplicate paths
	pathEntries := make(map[string][]config.RegisteredCampaign)
	for _, c := range reg.ListAll() {
		pathEntries[c.Path] = append(pathEntries[c.Path], c)
	}
	dupCount := 0
	for path, entries := range pathEntries {
		if len(entries) > 1 {
			issues = append(issues, fmt.Sprintf("Duplicate: %d entries for path %s", len(entries), path))
			dupCount += len(entries) - 1
		}
	}

	if len(issues) == 0 {
		fmt.Printf("%s Registry is healthy (%d campaigns)\n", ui.SuccessIcon(), reg.Len())
		return nil
	}

	fmt.Printf("%s Found %d issues:\n\n", ui.WarningIcon(), len(issues))
	for _, issue := range issues {
		fmt.Printf("  • %s\n", issue)
	}

	fmt.Println()
	if staleCount > 0 || dupCount > 0 {
		fmt.Printf("Run %s to remove stale/duplicate entries\n", ui.Accent("camp registry prune"))
	}
	if tempCount > 0 {
		fmt.Printf("Run %s to also clean temp entries\n", ui.Accent("camp registry prune --include-temp"))
	}

	return nil
}
