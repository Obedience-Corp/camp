package intent

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/gather"
	"github.com/Obedience-Corp/camp/internal/paths"
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
	Cmd.AddCommand(intentGatherCmd)

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
	intentGatherCmd.Flags().StringVar(&gatherHorizon, "horizon", "", "Override horizon (now, next, later, someday)")

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
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create services
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	intentSvc := intent.NewIntentService(campaignRoot, resolver.Intents())
	if err := intentSvc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "failed to ensure intent directories")
	}
	gatherSvc := gather.NewService(intentSvc, resolver.Intents())

	// Build index for tag/hashtag/similar discovery
	if gatherTag != "" || gatherHashtag != "" || gatherSimilar != "" {
		if err := gatherSvc.BuildIndex(ctx); err != nil {
			return camperrors.Wrap(err, "building index")
		}
	}

	// Discover intents to gather
	ids, err := discoverIntentsToGather(ctx, gatherSvc, intentSvc, args)
	if err != nil {
		return err
	}

	// Deduplicate IDs — prevents content duplication in nested gathers
	ids = deduplicateIDs(ids)

	if len(ids) < 2 {
		return fmt.Errorf("need at least 2 intents to gather, found %d", len(ids))
	}

	// Title is required
	if gatherTitle == "" {
		return fmt.Errorf("--title is required")
	}

	// Capture source file paths before gather potentially moves them.
	sourcePaths := make([]string, 0, len(ids))
	sourceStatusByID := make(map[string]intent.Status, len(ids))
	for _, id := range ids {
		src, getErr := intentSvc.Get(ctx, id)
		if getErr == nil {
			sourceStatusByID[id] = src.Status
		}
		if getErr == nil && src.Path != "" {
			sourcePaths = append(sourcePaths, src.Path)
		}
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
		return camperrors.Wrap(err, "gather failed")
	}

	if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
		Type:  audit.EventGather,
		ID:    result.Gathered.ID,
		Title: result.Gathered.Title,
		To:    string(result.Gathered.Status),
	}); err != nil {
		return err
	}

	if !gatherNoArchive && len(result.ArchivedSources) > 0 {
		reason := gather.ArchiveReason(result.Gathered.ID, result.Gathered.Title)
		for _, archived := range result.ArchivedSources {
			from := sourceStatusByID[archived.ID]
			if err := appendIntentAuditEvent(ctx, resolver.Intents(), audit.Event{
				Type:   audit.EventArchive,
				ID:     archived.ID,
				Title:  archived.Title,
				From:   string(from),
				To:     string(intent.StatusArchived),
				Reason: reason,
			}); err != nil {
				return err
			}
		}
	}

	// Output results
	fmt.Printf("✓ Gathered %d intents into: %s\n", result.SourceCount, result.Gathered.Path)
	if len(result.ArchivedPaths) > 0 {
		fmt.Printf("  Archived %d source intents\n", len(result.ArchivedPaths))
	}

	// Git commit (unless --no-commit)
	if !gatherNoCommit {
		// Build description for commit message
		description := fmt.Sprintf("Unified %d intents into %q\nSources: %s",
			result.SourceCount,
			gatherTitle,
			strings.Join(ids, ", "),
		)
		if len(result.ArchivedPaths) > 0 {
			description += fmt.Sprintf("\nArchived: %d source intents", len(result.ArchivedPaths))
		}

		files := make([]string, 0, len(sourcePaths)+len(result.ArchivedPaths)+1)
		files = append(files, sourcePaths...)
		if result.Gathered != nil && result.Gathered.Path != "" {
			files = append(files, result.Gathered.Path)
		}
		files = append(files, result.ArchivedPaths...)
		files = append(files, audit.FilePath(resolver.Intents()))

		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         commit.NormalizeFiles(campaignRoot, files...),
				SelectiveOnly: true,
			},
			Action:      commit.IntentGather,
			IntentTitle: gatherTitle,
			Description: description,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}

func discoverIntentsToGather(ctx context.Context, svc *gather.Service, intentSvc *intent.IntentService, args []string) ([]string, error) {
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

	// By explicit IDs — filter out dungeon intents
	if len(args) > 0 {
		filtered := make([]string, 0, len(args))
		for _, id := range args {
			i, err := intentSvc.Get(ctx, id)
			if err != nil {
				return nil, camperrors.Wrapf(err, "intent %q not found", id)
			}
			if i.Status.InDungeon() {
				fmt.Printf("  Skipping %s — status %s is not eligible for gathering\n", id, i.Status)
				continue
			}
			filtered = append(filtered, id)
		}
		return filtered, nil
	}

	// By tag
	if gatherTag != "" {
		intents, err := svc.FindByTag(ctx, gatherTag)
		if err != nil {
			return nil, camperrors.Wrap(err, "finding by tag")
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
			return nil, camperrors.Wrap(err, "finding by hashtag")
		}
		ids := make([]string, len(intents))
		for i, intent := range intents {
			ids[i] = intent.ID
		}
		return ids, nil
	}

	// By similarity
	if gatherSimilar != "" {
		// Validate reference intent is not in a final state
		refIntent, err := intentSvc.Get(ctx, gatherSimilar)
		if err != nil {
			return nil, camperrors.Wrapf(err, "reference intent %q not found", gatherSimilar)
		}
		if refIntent.Status.InDungeon() {
			return nil, fmt.Errorf("reference intent %q is in %s status — only inbox/active/ready intents can be gathered", gatherSimilar, refIntent.Status)
		}

		similar, err := svc.FindSimilar(ctx, gatherSimilar, gatherMinScore)
		if err != nil {
			return nil, camperrors.Wrap(err, "finding similar")
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

// deduplicateIDs removes duplicate intent IDs while preserving order.
func deduplicateIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}
