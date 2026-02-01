package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/gather"
	"github.com/obediencecorp/camp/internal/paths"
)

var (
	gatherTitle     string
	gatherTag       string
	gatherHashtag   string
	gatherSimilar   string
	gatherMinScore  float64
	gatherType      string
	gatherConcept   string
	gatherPriority  string
	gatherHorizon   string
	gatherNoArchive bool
	gatherDryRun    bool
	gatherNoCommit  bool
)

var intentGatherCmd = &cobra.Command{
	Use:   "gather [ids...]",
	Short: "Gather related intents into a unified document",
	Long: `Gather multiple related intents into a single unified document.

DISCOVERY MODES:
  By IDs      Explicitly specify intent IDs to gather
  --tag       Find intents with a specific frontmatter tag
  --hashtag   Find intents containing a specific #hashtag
  --similar   Find intents similar to a given ID (TF-IDF)

The gather process:
  1. Find related intents using the specified discovery method
  2. Merge their content with full metadata preservation
  3. Create a new unified intent in inbox status
  4. Archive source intents (unless --no-archive)

Source intents are preserved with a 'gathered_into' reference.

Examples:
  # Gather by explicit IDs
  camp intent gather id1 id2 id3 --title "Auth System"

  # Find and gather by tag
  camp intent gather --tag auth --title "Auth System"

  # Find and gather by hashtag
  camp intent gather --hashtag login --title "Login System"

  # Find similar intents and gather
  camp intent gather --similar auth-feature --title "Auth Unified"

  # Gather without archiving sources
  camp intent gather id1 id2 --title "Combined" --no-archive

  # Dry run to preview what would be gathered
  camp intent gather --tag auth --title "Auth System" --dry-run`,
	RunE: runIntentGather,
}

func init() {
	intentCmd.AddCommand(intentGatherCmd)

	// Required title
	intentGatherCmd.Flags().StringVarP(&gatherTitle, "title", "t", "", "Title for the gathered intent (required)")

	// Discovery options
	intentGatherCmd.Flags().StringVar(&gatherTag, "tag", "", "Find intents by frontmatter tag")
	intentGatherCmd.Flags().StringVar(&gatherHashtag, "hashtag", "", "Find intents by content hashtag")
	intentGatherCmd.Flags().StringVar(&gatherSimilar, "similar", "", "Find intents similar to this ID")
	intentGatherCmd.Flags().Float64Var(&gatherMinScore, "min-score", 0.1, "Minimum similarity score (0.0-1.0)")

	// Merge overrides
	intentGatherCmd.Flags().StringVar(&gatherType, "type", "", "Override type (idea, feature, bug, research)")
	intentGatherCmd.Flags().StringVar(&gatherConcept, "concept", "", "Override concept path")
	intentGatherCmd.Flags().StringVar(&gatherPriority, "priority", "", "Override priority (low, medium, high)")
	intentGatherCmd.Flags().StringVar(&gatherHorizon, "horizon", "", "Override horizon (now, next, later)")

	// Behavior options
	intentGatherCmd.Flags().BoolVar(&gatherNoArchive, "no-archive", false, "Don't archive source intents")
	intentGatherCmd.Flags().BoolVar(&gatherDryRun, "dry-run", false, "Preview gather without making changes")
	intentGatherCmd.Flags().BoolVar(&gatherNoCommit, "no-commit", false, "Don't create a git commit")
}

func runIntentGather(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create services
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	intentSvc := intent.NewIntentService(campaignRoot, resolver.Intents())
	gatherSvc := gather.NewService(intentSvc, resolver.Intents())

	// Build index for tag/hashtag/similar discovery
	if gatherTag != "" || gatherHashtag != "" || gatherSimilar != "" {
		if err := gatherSvc.BuildIndex(ctx); err != nil {
			return fmt.Errorf("building index: %w", err)
		}
	}

	// Discover intents to gather
	ids, err := discoverIntentsToGather(ctx, gatherSvc, args)
	if err != nil {
		return err
	}

	if len(ids) < 2 {
		return fmt.Errorf("need at least 2 intents to gather, found %d", len(ids))
	}

	// Title is required
	if gatherTitle == "" {
		return fmt.Errorf("--title is required")
	}

	// Dry run: just show what would be gathered
	if gatherDryRun {
		return showDryRun(ctx, intentSvc, ids, gatherTitle)
	}

	// Build gather options
	opts := gather.GatherOptions{
		Title:          gatherTitle,
		Type:           intent.Type(gatherType),
		Concept:        gatherConcept,
		Priority:       intent.Priority(gatherPriority),
		Horizon:        intent.Horizon(gatherHorizon),
		ArchiveSources: !gatherNoArchive,
	}

	// Execute gather
	result, err := gatherSvc.Gather(ctx, ids, opts)
	if err != nil {
		return fmt.Errorf("gather failed: %w", err)
	}

	// Output results
	fmt.Printf("✓ Gathered %d intents into: %s\n", result.SourceCount, result.Gathered.Path)
	if len(result.ArchivedPaths) > 0 {
		fmt.Printf("  Archived %d source intents\n", len(result.ArchivedPaths))
	}

	// Git commit (unless --no-commit)
	if !gatherNoCommit {
		// Build commit message with campaign ID prefix
		shortID := cfg.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		commitMsg := fmt.Sprintf("[OBEY-CAMPAIGN-%s] Gather: %s\n\nUnified %d intents into %q\nSources: %s",
			shortID,
			gatherTitle,
			result.SourceCount,
			gatherTitle,
			strings.Join(ids, ", "),
		)
		if len(result.ArchivedPaths) > 0 {
			commitMsg += fmt.Sprintf("\nArchived: %d source intents", len(result.ArchivedPaths))
		}

		// CommitAll has built-in lock handling with retry
		if err := git.CommitAll(ctx, campaignRoot, commitMsg); err != nil {
			// Don't fail the gather - just warn about commit failure
			if errors.Is(err, git.ErrNoChanges) {
				// This shouldn't happen after gather, but handle gracefully
				fmt.Println("  (no changes to commit)")
			} else {
				fmt.Printf("  Warning: git commit failed: %v\n", err)
			}
		} else {
			fmt.Println("  Committed changes to git")
		}
	}

	return nil
}

func discoverIntentsToGather(ctx context.Context, svc *gather.Service, args []string) ([]string, error) {
	// Count discovery methods specified
	methods := 0
	if len(args) > 0 {
		methods++
	}
	if gatherTag != "" {
		methods++
	}
	if gatherHashtag != "" {
		methods++
	}
	if gatherSimilar != "" {
		methods++
	}

	if methods == 0 {
		return nil, fmt.Errorf("specify intent IDs or use --tag, --hashtag, or --similar")
	}

	if methods > 1 {
		return nil, fmt.Errorf("use only one discovery method: IDs, --tag, --hashtag, or --similar")
	}

	// By explicit IDs
	if len(args) > 0 {
		return args, nil
	}

	// By tag
	if gatherTag != "" {
		intents, err := svc.FindByTag(ctx, gatherTag)
		if err != nil {
			return nil, fmt.Errorf("finding by tag: %w", err)
		}
		ids := make([]string, len(intents))
		for i, intent := range intents {
			ids[i] = intent.ID
		}
		return ids, nil
	}

	// By hashtag
	if gatherHashtag != "" {
		intents, err := svc.FindByHashtag(ctx, gatherHashtag)
		if err != nil {
			return nil, fmt.Errorf("finding by hashtag: %w", err)
		}
		ids := make([]string, len(intents))
		for i, intent := range intents {
			ids[i] = intent.ID
		}
		return ids, nil
	}

	// By similarity
	if gatherSimilar != "" {
		similar, err := svc.FindSimilar(ctx, gatherSimilar, gatherMinScore)
		if err != nil {
			return nil, fmt.Errorf("finding similar: %w", err)
		}
		// Include the reference intent too
		ids := []string{gatherSimilar}
		for _, s := range similar {
			ids = append(ids, s.Intent.ID)
		}
		return ids, nil
	}

	return nil, fmt.Errorf("no discovery method specified")
}

func showDryRun(ctx context.Context, svc *intent.IntentService, ids []string, title string) error {
	fmt.Printf("Would gather %d intents into: %q\n\n", len(ids), title)
	fmt.Println("Source intents:")
	fmt.Println(strings.Repeat("-", 60))

	for _, id := range ids {
		intent, err := svc.Get(ctx, id)
		if err != nil {
			fmt.Printf("  %s (not found: %v)\n", id, err)
			continue
		}
		fmt.Printf("  %-30s %s\n", intent.ID, intent.Title)
	}

	fmt.Println(strings.Repeat("-", 60))

	if !gatherNoArchive {
		fmt.Println("\nSource intents would be archived after gathering.")
	} else {
		fmt.Println("\nSource intents would NOT be archived (--no-archive).")
	}

	return nil
}
