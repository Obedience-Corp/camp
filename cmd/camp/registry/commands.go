package registry

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

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
	Cmd.AddCommand(registryPruneCmd)
	Cmd.AddCommand(registrySyncCmd)
	Cmd.AddCommand(registryCheckCmd)

	registryPruneCmd.Flags().Bool("dry-run", false, "Show what would be removed without making changes")
	registryPruneCmd.Flags().Bool("include-temp", false, "Also remove entries in /tmp/ directories")
}

func runRegistryPrune(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	includeTemp, _ := cmd.Flags().GetBool("include-temp")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}

	if reg.Len() == 0 {
		fmt.Println(ui.Info("Registry is empty."))
		return nil
	}

	// Find duplicate path entries (multiple IDs pointing to same path).
	pathEntries := make(map[string][]config.RegisteredCampaign)
	for _, c := range reg.ListAll() {
		pathEntries[c.Path] = append(pathEntries[c.Path], c)
	}

	var duplicates []config.RegisteredCampaign
	for _, entries := range pathEntries {
		if len(entries) <= 1 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].LastAccess.After(entries[j].LastAccess)
		})
		for i := 1; i < len(entries); i++ {
			dup := entries[i]
			dup.Name = fmt.Sprintf("duplicate path (keeping %s)", entries[0].ID[:8])
			duplicates = append(duplicates, dup)
		}
	}

	// Find stale entries (skip entries already marked as duplicates).
	dupIDs := make(map[string]bool)
	for _, d := range duplicates {
		dupIDs[d.ID] = true
	}

	var stale []config.RegisteredCampaign
	for _, c := range reg.ListAll() {
		if dupIDs[c.ID] {
			continue
		}

		isStale := false
		reason := ""

		if _, err := os.Stat(c.Path); os.IsNotExist(err) {
			isStale = true
			reason = "path does not exist"
		} else {
			campaignDir := filepath.Join(c.Path, config.CampaignDir)
			if _, err := os.Stat(campaignDir); os.IsNotExist(err) {
				isStale = true
				reason = "no .campaign/ directory"
			}
		}

		if includeTemp && strings.HasPrefix(c.Path, "/tmp/") {
			isStale = true
			reason = "temp directory"
		}

		if isStale {
			c.Name = reason
			stale = append(stale, c)
		}
	}

	toRemove := append(duplicates, stale...)

	if len(toRemove) == 0 {
		fmt.Printf("%s Registry is clean (%d campaigns)\n", ui.SuccessIcon(), reg.Len())
		return nil
	}

	fmt.Printf("%s Found %d entries to remove:\n", ui.WarningIcon(), len(toRemove))
	for _, c := range toRemove {
		fmt.Printf("  %s %s (%s)\n", ui.Dim(c.ID[:8]), ui.Value(c.Path), ui.Dim(c.Name))
	}

	if dryRun {
		fmt.Println()
		fmt.Println(ui.Info("Dry run - no changes made."))
		return nil
	}

	fmt.Print("\nRemove these entries? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println(ui.Dim("Aborted."))
		return nil
	}

	for _, c := range toRemove {
		reg.UnregisterByID(c.ID)
	}

	if err := config.SaveRegistry(ctx, reg); err != nil {
		return camperrors.Wrap(err, "failed to save registry")
	}

	fmt.Printf("%s Removed %d entries\n", ui.SuccessIcon(), len(toRemove))
	return nil
}

func runRegistrySync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not inside a campaign")
	}

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}

	existing, existsByID := reg.GetByID(cfg.ID)
	if existsByID && existing.Path == campaignRoot {
		fmt.Printf("%s Registry already up to date\n", ui.SuccessIcon())
		fmt.Println(ui.KeyValue("Campaign:", cfg.Name))
		fmt.Println(ui.KeyValue("Path:", campaignRoot))
		return nil
	}

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

		reg.UnregisterByID(conflicting.ID)
	}

	if err := reg.Register(cfg.ID, cfg.Name, campaignRoot, cfg.Type); err != nil {
		return camperrors.Wrap(err, "failed to register")
	}

	if err := config.SaveRegistry(ctx, reg); err != nil {
		return camperrors.Wrap(err, "failed to save registry")
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
		return camperrors.Wrap(err, "failed to load registry")
	}

	if reg.Len() == 0 {
		fmt.Println(ui.Info("Registry is empty."))
		return nil
	}

	var issues []string

	staleCount := 0
	tempCount := 0
	for _, c := range reg.ListAll() {
		if _, err := os.Stat(c.Path); os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("Stale: %s (%s) - path does not exist", c.Name, c.Path))
			staleCount++
			continue
		}

		campaignDir := filepath.Join(c.Path, config.CampaignDir)
		if _, err := os.Stat(campaignDir); os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("Invalid: %s (%s) - no .campaign/ directory", c.Name, c.Path))
			staleCount++
			continue
		}

		if strings.HasPrefix(c.Path, "/tmp/") {
			issues = append(issues, fmt.Sprintf("Temp: %s (%s) - test campaign in /tmp/", c.Name, c.Path))
			tempCount++
		}
	}

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
