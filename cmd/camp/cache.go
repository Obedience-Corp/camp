package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the navigation index cache",
	Long:  `Manage the navigation index cache used for fast project lookups.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the navigation cache",
	Long:  `Delete the cached navigation index. It will be rebuilt on next navigation.`,
	RunE:  runCacheClear,
}

var cacheRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Force rebuild the navigation cache",
	Long:  `Force rebuild the navigation index cache, regardless of staleness.`,
	RunE:  runCacheRebuild,
}

var cacheInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show cache status and metadata",
	Long:  `Show information about the navigation index cache including path, size, age, and staleness.`,
	RunE:  runCacheInfo,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.GroupID = "navigation"

	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cacheRebuildCmd)
	cacheCmd.AddCommand(cacheInfoCmd)
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	if err := index.Delete(root); err != nil {
		return camperrors.Wrap(err, "failed to delete cache")
	}

	fmt.Printf("%s Cache cleared\n", ui.SuccessIcon())
	return nil
}

func runCacheRebuild(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	idx, err := index.GetOrBuild(ctx, root, true)
	if err != nil {
		return camperrors.Wrap(err, "failed to rebuild cache")
	}

	fmt.Printf("%s Cache rebuilt (%d targets)\n", ui.SuccessIcon(), len(idx.Targets))
	return nil
}

func runCacheInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	info, err := index.Info(root)
	if err != nil {
		return camperrors.Wrap(err, "failed to get cache info")
	}

	fmt.Println(ui.Subheader("Cache Info"))
	fmt.Println()
	fmt.Printf("  %s %s\n", ui.Label("Path:"), ui.Dim(info.Path))

	if !info.Exists {
		fmt.Printf("  %s %s\n", ui.Label("Status:"), ui.Warning("not found"))
		return nil
	}

	fmt.Printf("  %s %s\n", ui.Label("Size:"), ui.Value(formatBytes(info.Size)))
	fmt.Printf("  %s %s\n", ui.Label("Age:"), ui.Value(formatDuration(info.Age)))

	// Load index for target count and staleness
	idx, loadErr := index.Load(root)
	if loadErr == nil && idx != nil {
		fmt.Printf("  %s %s\n", ui.Label("Targets:"), ui.Value(fmt.Sprintf("%d", len(idx.Targets))))

		if index.IsStale(idx, root) {
			fmt.Printf("  %s %s\n", ui.Label("Status:"), ui.Warning("stale"))
		} else {
			fmt.Printf("  %s %s\n", ui.Label("Status:"), ui.Success("fresh"))
		}
	} else if info.Fresh {
		fmt.Printf("  %s %s\n", ui.Label("Status:"), ui.Success("fresh"))
	} else {
		fmt.Printf("  %s %s\n", ui.Label("Status:"), ui.Warning("stale"))
	}

	return nil
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatDuration formats a duration as a human-readable string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
