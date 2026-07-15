package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/sync"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// formatSyncResult formats and displays the sync result.
func formatSyncResult(result *sync.SyncResult, opts syncOptions, preflight *sync.PreflightResult) {
	if opts.json {
		formatSyncJSON(result, preflight)
		return
	}
	formatSyncHuman(result, opts, preflight)
}

// formatSyncHuman displays human-readable sync output.
func formatSyncHuman(result *sync.SyncResult, opts syncOptions, preflight *sync.PreflightResult) {
	// Dry run header
	if opts.dryRun {
		fmt.Println(ui.Info("DRY RUN - No changes will be made"))
		fmt.Println()
	}

	// Pre-flight checks section
	formatPreflightSection(preflight, opts.verbose)

	// Handle preflight failure - don't show more sections
	if !result.PreflightPassed && !opts.force {
		fmt.Println()
		fmt.Fprintln(os.Stderr, ui.Error("Aborting: Submodules have uncommitted changes or unpushed commits."))
		fmt.Println()
		formatFixSuggestions(preflight)
		return
	}

	// URL synchronization section
	formatURLSection(result, opts.dryRun)

	// For dry-run, stop here
	if opts.dryRun {
		fmt.Println()
		if result.Success {
			fmt.Println(ui.Success("Dry run complete. No issues detected."))
		}
		return
	}

	// Update results section
	formatUpdateSection(result, opts.verbose)
	formatArtifactsSection(result, opts.verbose)
	formatWarningsSection(result.Warnings)

	// Final status
	fmt.Println()
	switch {
	case result.Success && opts.verifyArtifacts:
		fmt.Println(ui.Success("Artifact verification complete."))
	case result.Success && opts.artifactsOnly:
		fmt.Println(ui.Success("Artifacts synchronized successfully."))
	case result.Success:
		fmt.Println(ui.Success("Campaign synchronized successfully."))
	case opts.verifyArtifacts:
		fmt.Fprintln(os.Stderr, ui.Error("Artifact verification found discrepancies. See details above."))
	default:
		fmt.Fprintln(os.Stderr, ui.Error("Sync failed. See errors or warnings above."))
	}
}

// formatPreflightSection displays the preflight check results.
func formatPreflightSection(preflight *sync.PreflightResult, verbose bool) {
	fmt.Println("Pre-flight checks:")

	if preflight == nil {
		fmt.Println("  " + ui.WarningIcon() + " No preflight data available")
		return
	}

	// Count totals
	dirtyCount := len(preflight.UncommittedChanges) + len(preflight.UnpushedCommits)

	// If we have details about dirty submodules, show them
	if len(preflight.UncommittedChanges) > 0 {
		for _, status := range preflight.UncommittedChanges {
			fmt.Printf("  %s %s - uncommitted changes", ui.WarningIcon(), status.Path)
			if status.Details != "" && verbose {
				fmt.Printf(" (%s)", status.Details)
			}
			fmt.Println()
		}
	}

	if len(preflight.UnpushedCommits) > 0 {
		for _, status := range preflight.UnpushedCommits {
			fmt.Printf("  %s %s - %s\n", ui.WarningIcon(), status.Path, status.Details)
		}
	}

	// Show URL mismatches (these are informational)
	if len(preflight.URLMismatches) > 0 && verbose {
		for _, mismatch := range preflight.URLMismatches {
			fmt.Printf("  %s %s - URL mismatch (will be fixed)\n", ui.InfoIcon(), mismatch.Submodule)
		}
	}

	// Show detached HEADs as warnings
	if len(preflight.DetachedHEADs) > 0 {
		for _, detached := range preflight.DetachedHEADs {
			if detached.HasLocalWork {
				fmt.Printf("  %s %s - detached HEAD with %d local commits\n",
					ui.WarningIcon(), detached.Path, detached.LocalCommits)
			} else if verbose {
				fmt.Printf("  %s %s - detached HEAD at %s\n",
					ui.InfoIcon(), detached.Path, detached.Commit)
			}
		}
	}

	// If nothing specific to report, show summary
	if dirtyCount == 0 && len(preflight.DetachedHEADs) == 0 {
		// Everything clean
		fmt.Printf("  %s All checks passed\n", ui.SuccessIcon())
	} else if preflight.Passed {
		// Passed despite warnings (force mode or informational only)
		fmt.Printf("  %s Checks passed with warnings\n", ui.SuccessIcon())
	}
}

// formatURLSection displays URL synchronization results.
func formatURLSection(result *sync.SyncResult, dryRun bool) {
	if len(result.URLChanges) == 0 {
		return
	}

	fmt.Println()
	if dryRun {
		fmt.Println("Would synchronize URLs:")
	} else {
		fmt.Println("URL synchronization:")
	}

	fmt.Printf("  %s URLs synchronized (%d changed)\n", ui.SuccessIcon(), len(result.URLChanges))
	for _, change := range result.URLChanges {
		fmt.Printf("    %s: %s %s %s\n",
			change.Submodule,
			ui.Dim(change.OldURL),
			ui.ArrowIcon(),
			change.NewURL)
	}
}

// formatUpdateSection displays submodule update results.
func formatUpdateSection(result *sync.SyncResult, verbose bool) {
	if len(result.UpdateResults) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("Updating submodules:")

	succeeded := 0
	failed := 0
	for _, sub := range result.UpdateResults {
		if sub.Success {
			succeeded++
			if verbose {
				fmt.Printf("  %s %s\n", ui.SuccessIcon(), sub.Path)
			}
		} else {
			failed++
			fmt.Printf("  %s %s", ui.ErrorIcon(), sub.Path)
			if sub.Error != nil {
				fmt.Printf(" - %s", sub.Error.Error())
			}
			fmt.Println()
		}
	}

	// Summary
	total := len(result.UpdateResults)
	if failed == 0 {
		if !verbose {
			fmt.Printf("  %s %d/%d submodules updated\n", ui.SuccessIcon(), succeeded, total)
		}
	} else {
		fmt.Printf("  %s %d/%d submodules failed\n", ui.ErrorIcon(), failed, total)
	}
}

// formatWarningsSection displays non-fatal sync issues that still require attention.
func formatWarningsSection(warnings []string) {
	if len(warnings) == 0 {
		return
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Warnings:")
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.WarningIcon(), warning)
	}
}

// formatFixSuggestions displays actionable fix suggestions.
func formatFixSuggestions(preflight *sync.PreflightResult) {
	if preflight == nil {
		return
	}

	fmt.Println("To fix:")

	// Suggestions for uncommitted changes
	if len(preflight.UncommittedChanges) > 0 {
		for _, status := range preflight.UncommittedChanges {
			fmt.Printf("  cd %s && git stash  # or git commit\n", status.Path)
		}
	}

	// Suggestions for unpushed commits
	if len(preflight.UnpushedCommits) > 0 {
		for _, status := range preflight.UnpushedCommits {
			fmt.Printf("  cd %s && git push\n", status.Path)
		}
	}

	fmt.Println()
	fmt.Println("Or sync anyway (may lose changes):")
	fmt.Println("  camp sync --force")
}

// formatArtifactsSection displays artifact pull/verify outcomes; silent when
// the sync involved no artifacts.
func formatArtifactsSection(result *sync.SyncResult, verbose bool) {
	if len(result.Artifacts) == 0 && len(result.ArtifactVerifies) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("Artifacts:")
	for _, a := range result.Artifacts {
		switch {
		case a.Warning != "":
			fmt.Fprintf(os.Stderr, "  %s %s: %s\n", ui.ErrorIcon(), a.Root, a.Warning)
		case a.FirstSync:
			fmt.Printf("  %s %s (first sync; %d pre-existing local files left untouched)\n",
				ui.SuccessIcon(), a.Root, a.Protected)
		case len(a.SkippedConflicts) > 0:
			fmt.Printf("  %s %s (synced; %d conflicts kept local)\n",
				ui.WarningIcon(), a.Root, len(a.SkippedConflicts))
			for i, p := range a.SkippedConflicts {
				if !verbose && i == 5 {
					fmt.Printf("      ... and %d more (use --verbose)\n", len(a.SkippedConflicts)-i)
					break
				}
				fmt.Printf("      %s (local edit preserved; remove the file to take the peer's copy)\n", p)
			}
		default:
			fmt.Printf("  %s %s (synced)\n", ui.SuccessIcon(), a.Root)
		}
	}
	for _, v := range result.ArtifactVerifies {
		if v.Result.Clean() {
			fmt.Printf("  %s %s vs %s: clean (%d files)\n", ui.SuccessIcon(), v.Result.Root, v.Peer, v.Result.Checked)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s %s vs %s: %d missing, %d differ, %d extra\n",
			ui.ErrorIcon(), v.Result.Root, v.Peer, len(v.Result.Missing), len(v.Result.Differ), len(v.Result.Extra))
		if verbose {
			for _, p := range v.Result.Missing {
				fmt.Fprintf(os.Stderr, "      missing: %s\n", p)
			}
			for _, p := range v.Result.Differ {
				fmt.Fprintf(os.Stderr, "      differs: %s\n", p)
			}
			for _, p := range v.Result.Extra {
				fmt.Fprintf(os.Stderr, "      extra:   %s\n", p)
			}
		}
	}
}

// formatSyncJSON outputs sync results as JSON.
func formatSyncJSON(result *sync.SyncResult, preflight *sync.PreflightResult) {
	// Build preflight submodules info
	var preflightSubmodules []map[string]interface{}
	if preflight != nil {
		// Track which submodules have issues
		submoduleStatus := make(map[string]map[string]interface{})

		for _, status := range preflight.UncommittedChanges {
			if submoduleStatus[status.Path] == nil {
				submoduleStatus[status.Path] = make(map[string]interface{})
			}
			submoduleStatus[status.Path]["name"] = status.Path
			submoduleStatus[status.Path]["clean"] = false
			submoduleStatus[status.Path]["uncommittedChanges"] = true
		}

		for _, status := range preflight.UnpushedCommits {
			if submoduleStatus[status.Path] == nil {
				submoduleStatus[status.Path] = make(map[string]interface{})
			}
			submoduleStatus[status.Path]["name"] = status.Path
			submoduleStatus[status.Path]["unpushedCommits"] = true
		}

		for _, detached := range preflight.DetachedHEADs {
			if submoduleStatus[detached.Path] == nil {
				submoduleStatus[detached.Path] = make(map[string]interface{})
			}
			submoduleStatus[detached.Path]["name"] = detached.Path
			submoduleStatus[detached.Path]["headDetached"] = true
			submoduleStatus[detached.Path]["localCommits"] = detached.LocalCommits
		}

		for _, sub := range submoduleStatus {
			if sub["clean"] == nil {
				sub["clean"] = true
			}
			if sub["headDetached"] == nil {
				sub["headDetached"] = false
			}
			preflightSubmodules = append(preflightSubmodules, sub)
		}
	}

	// Build URL changes
	urlChanges := make([]map[string]string, len(result.URLChanges))
	for i, change := range result.URLChanges {
		urlChanges[i] = map[string]string{
			"submodule": change.Submodule,
			"old":       change.OldURL,
			"new":       change.NewURL,
		}
	}

	// Build update results
	updateResults := map[string]interface{}{
		"total":     len(result.UpdateResults),
		"succeeded": countSucceeded(result.UpdateResults),
		"failed":    countFailed(result.UpdateResults),
	}

	// Build warnings
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}

	output := map[string]interface{}{
		"success": result.Success,
		"preflightChecks": map[string]interface{}{
			"passed":     result.PreflightPassed,
			"submodules": preflightSubmodules,
		},
		"urlChanges":    urlChanges,
		"updateResults": updateResults,
		"warnings":      warnings,
	}

	// Peer-transport keys appear only when the feature ran, keeping default
	// `camp sync --json` byte-identical for existing consumers.
	if len(result.Artifacts) > 0 {
		output["artifacts"] = result.Artifacts
	}
	if len(result.ArtifactVerifies) > 0 {
		verifies := make([]map[string]interface{}, len(result.ArtifactVerifies))
		for i, v := range result.ArtifactVerifies {
			verifies[i] = map[string]interface{}{
				"peer":    v.Peer,
				"root":    v.Result.Root,
				"clean":   v.Result.Clean(),
				"checked": v.Result.Checked,
				"missing": emptyIfNil(v.Result.Missing),
				"differ":  emptyIfNil(v.Result.Differ),
				"extra":   emptyIfNil(v.Result.Extra),
			}
		}
		output["artifactVerify"] = verifies
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(output)
}

// emptyIfNil keeps JSON list fields as [] rather than null.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// countSucceeded counts successful submodule results.
func countSucceeded(results []sync.SubmoduleResult) int {
	count := 0
	for _, r := range results {
		if r.Success {
			count++
		}
	}
	return count
}

// countFailed counts failed submodule results.
func countFailed(results []sync.SubmoduleResult) int {
	count := 0
	for _, r := range results {
		if !r.Success {
			count++
		}
	}
	return count
}

// syncOptions is a copy of the flag struct for passing to formatters.
type syncOptions struct {
	dryRun          bool
	force           bool
	verbose         bool
	parallel        int
	noFetch         bool
	json            bool
	verifyArtifacts bool
	artifactsOnly   bool
}
