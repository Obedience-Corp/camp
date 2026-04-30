package intent

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	sharedcrawl "github.com/Obedience-Corp/camp/internal/crawl"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	intentcrawl "github.com/Obedience-Corp/camp/internal/intent/crawl"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var intentCrawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Interactive intent triage",
	Long: `Walk live intents one at a time and decide their fate.

Default scope is the working set: inbox, ready, and active. Each candidate is
shown with a compact preview. For each one you can keep, move to another
status, skip, or quit. Moves to dungeon statuses require a reason.

Existing dungeon intents are not crawl candidates. Use 'camp intent move' to
restore them explicitly.

Examples:
  camp intent crawl
  camp intent crawl --status inbox --limit 25
  camp intent crawl --status ready --status active --sort priority
  camp intent crawl --no-commit`,
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive intent triage session",
		"interactive":   "true",
	},
	RunE: runIntentCrawl,
}

func init() {
	Cmd.AddCommand(intentCrawlCmd)

	intentCrawlCmd.Flags().StringSlice("status", nil, "Restrict to live statuses (repeatable: inbox, ready, active)")
	intentCrawlCmd.Flags().Int("limit", 0, "Stop after N candidates (0 = no limit)")
	intentCrawlCmd.Flags().String("sort", string(intentcrawl.SortStale), "Sort mode: stale, updated, created, priority, title")
	intentCrawlCmd.Flags().Bool("no-commit", false, "Apply moves and logs but do not auto-commit")
}

func runIntentCrawl(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	statusFlags, _ := cmd.Flags().GetStringSlice("status")
	limit, _ := cmd.Flags().GetInt("limit")
	sortMode, _ := cmd.Flags().GetString("sort")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	statuses, err := parseStatusFlags(statusFlags)
	if err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	intentsDir := resolver.Intents()
	svc := intent.NewIntentService(campaignRoot, intentsDir)
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	opts := intentcrawl.Options{
		Statuses: statuses,
		Limit:    limit,
		Sort:     intentcrawl.SortMode(sortMode),
	}
	runCfg := intentcrawl.Config{
		Store:      svc,
		Prompt:     sharedcrawl.NewDefaultPrompt(),
		IntentsDir: intentsDir,
		Actor:      resolveIntentActor(ctx),
	}

	result, err := intentcrawl.Run(ctx, runCfg, opts)
	aborted := false
	if err != nil {
		if errors.Is(err, sharedcrawl.ErrAborted) {
			aborted = true
		} else {
			return err
		}
	}

	if !aborted && result.CandidateCount == 0 {
		fmt.Println(intentcrawl.FormatNoCandidates(result.Statuses))
		return nil
	}

	printIntentCrawlSummary(result, aborted)

	if aborted || noCommit || !result.Summary.HasMoves() {
		return nil
	}

	return commitIntentCrawl(ctx, campaignRoot, cfg.ID, result)
}

func parseStatusFlags(raw []string) ([]intent.Status, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]intent.Status, 0, len(raw))
	seen := make(map[intent.Status]struct{}, len(raw))
	for _, r := range raw {
		s, err := intentcrawl.ParseStatusFlag(r)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func printIntentCrawlSummary(result *intentcrawl.Result, aborted bool) {
	fmt.Println()
	if aborted {
		fmt.Printf("%s Intent crawl cancelled.\n", ui.InfoIcon())
	} else {
		fmt.Printf("%s Intent crawl complete!\n", ui.SuccessIcon())
	}

	targets := make([]string, 0, len(result.Summary.Moved))
	for t := range result.Summary.Moved {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		fmt.Printf("  %s Moved to %s: %d\n", ui.BulletIcon(), t, result.Summary.Moved[t])
	}
	if result.Summary.Kept > 0 {
		fmt.Printf("  %s Kept:    %d\n", ui.BulletIcon(), result.Summary.Kept)
	}
	if result.Summary.Skipped > 0 {
		fmt.Printf("  %s Skipped: %d\n", ui.BulletIcon(), result.Summary.Skipped)
	}
}

func commitIntentCrawl(ctx context.Context, campaignRoot, campaignID string, result *intentcrawl.Result) error {
	res := commit.Intent(ctx, commit.IntentOptions{
		Options: commit.Options{
			CampaignRoot:  campaignRoot,
			CampaignID:    campaignID,
			Files:         result.CommitPaths.Files,
			PreStaged:     result.CommitPaths.PreStaged,
			SelectiveOnly: true,
		},
		Action:      commit.IntentCrawl,
		IntentTitle: "intent crawl completed",
		Description: buildIntentCrawlCommitDescription(result),
	})

	if res.Committed {
		fmt.Printf("\n%s %s\n", ui.SuccessIcon(), res.Message)
		return nil
	}
	if res.NoChanges {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), res.Message)
		return nil
	}
	if res.Err != nil {
		fmt.Printf("\n%s Intent crawl moves were applied on disk, but auto-commit failed.\n", ui.WarningIcon())
		fmt.Printf("%s %v\n", ui.WarningIcon(), res.Err)
		return camperrors.Wrap(res.Err, "auto-committing intent crawl changes")
	}
	if res.Message != "" {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), res.Message)
	}
	return nil
}

func buildIntentCrawlCommitDescription(result *intentcrawl.Result) string {
	if result == nil || result.Summary == nil {
		return ""
	}
	keys := make([]string, 0, len(result.Summary.Paths))
	for k := range result.Summary.Paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out string
	for _, k := range keys {
		paths := result.Summary.Paths[k]
		if len(paths) == 0 {
			continue
		}
		out += fmt.Sprintf("Moved to %s:\n", k)
		for _, p := range paths {
			out += fmt.Sprintf("  - %s\n", p)
		}
		out += "\n"
	}
	if l := len(out); l > 0 && out[l-1] == '\n' {
		out = out[:l-1]
	}
	return out
}
