// Package shortcuts provides shortcut expansion and handling for the camp CLI.
package shortcuts

import (
	"fmt"
	"io"
	"sort"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/ui"
)

// FeedbackWriter provides user feedback for shortcut expansion.
type FeedbackWriter struct {
	out       io.Writer
	shortcuts map[string]config.ShortcutConfig
}

// NewFeedbackWriter creates a new FeedbackWriter.
func NewFeedbackWriter(out io.Writer, shortcuts map[string]config.ShortcutConfig) *FeedbackWriter {
	return &FeedbackWriter{
		out:       out,
		shortcuts: shortcuts,
	}
}

// ShowExpansion displays what shortcut expanded to.
func (f *FeedbackWriter) ShowExpansion(result ExpansionResult) {
	if !result.WasExpanded {
		return
	}
	fmt.Fprintf(f.out, "%s %s %s %s\n",
		ui.Dim("expanded:"),
		ui.Accent(result.Shortcut),
		ui.ArrowIcon(),
		ui.Value(result.ExpandedTo))
}

// ShowNavigationOnly explains that a shortcut is for navigation only.
func (f *FeedbackWriter) ShowNavigationOnly(shortcut string) {
	sc, ok := f.shortcuts[shortcut]
	if !ok {
		return
	}

	fmt.Fprintf(f.out, "%s Shortcut '%s' is for navigation only\n",
		ui.WarningIcon(), ui.Accent(shortcut))
	fmt.Fprintf(f.out, "  %s %s\n", ui.Label("Path:"), ui.Value(sc.Path))
	fmt.Fprintf(f.out, "\n")
	fmt.Fprintf(f.out, "  Use %s to navigate to this directory.\n",
		ui.Accent(fmt.Sprintf("cgo %s", shortcut)))
}

// ShowUnknownShortcut suggests similar shortcuts for typos.
func (f *FeedbackWriter) ShowUnknownShortcut(unknown string) {
	suggestions := f.findSimilar(unknown, 3)

	fmt.Fprintf(f.out, "%s Unknown shortcut: '%s'\n",
		ui.WarningIcon(), ui.Accent(unknown))

	if len(suggestions) > 0 {
		fmt.Fprintf(f.out, "\n  Did you mean:\n")
		for _, s := range suggestions {
			sc := f.shortcuts[s]
			desc := ""
			if sc.Description != "" {
				desc = ui.Dim(" - " + sc.Description)
			}
			fmt.Fprintf(f.out, "    %s%s\n", ui.Accent(s), desc)
		}
	}

	fmt.Fprintf(f.out, "\n  Run %s to see all shortcuts.\n",
		ui.Accent("camp shortcuts"))
}

// ShowAvailableShortcuts lists all available shortcuts.
func (f *FeedbackWriter) ShowAvailableShortcuts() {
	if len(f.shortcuts) == 0 {
		fmt.Fprintf(f.out, "%s No shortcuts configured.\n", ui.Dim("(none)"))
		return
	}

	// Separate by type
	var navOnly, cmdEnabled []string
	for key, sc := range f.shortcuts {
		if sc.Concept != "" {
			cmdEnabled = append(cmdEnabled, key)
		} else if sc.Path != "" {
			navOnly = append(navOnly, key)
		}
	}

	sort.Strings(cmdEnabled)
	sort.Strings(navOnly)

	// Show command-enabled shortcuts first
	if len(cmdEnabled) > 0 {
		fmt.Fprintf(f.out, "%s\n", ui.Info("Command shortcuts (usable with camp <shortcut>):"))
		for _, key := range cmdEnabled {
			sc := f.shortcuts[key]
			fmt.Fprintf(f.out, "  %s %s %s",
				ui.Accent(fmt.Sprintf("%-4s", key)),
				ui.ArrowIcon(),
				ui.Value(sc.Concept))
			if sc.Description != "" {
				fmt.Fprintf(f.out, " %s", ui.Dim("# "+sc.Description))
			}
			fmt.Fprintf(f.out, "\n")
		}
		fmt.Fprintf(f.out, "\n")
	}

	// Show navigation-only shortcuts
	if len(navOnly) > 0 {
		fmt.Fprintf(f.out, "%s\n", ui.Info("Navigation shortcuts (use with cgo <shortcut>):"))
		for _, key := range navOnly {
			sc := f.shortcuts[key]
			fmt.Fprintf(f.out, "  %s %s %s",
				ui.Accent(fmt.Sprintf("%-4s", key)),
				ui.ArrowIcon(),
				ui.Dim(sc.Path))
			if sc.Description != "" {
				fmt.Fprintf(f.out, " %s", ui.Dim("# "+sc.Description))
			}
			fmt.Fprintf(f.out, "\n")
		}
	}
}

// findSimilar finds shortcuts similar to the given string using Levenshtein distance.
func (f *FeedbackWriter) findSimilar(target string, maxResults int) []string {
	type scored struct {
		key   string
		score int
	}

	var scores []scored
	for key := range f.shortcuts {
		dist := levenshtein(target, key)
		// Only include if reasonably similar (distance <= half the length + 1)
		threshold := len(target)/2 + 1
		if dist <= threshold {
			scores = append(scores, scored{key, dist})
		}
	}

	// Sort by score (lower is better)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score < scores[j].score
	})

	// Return top results
	var results []string
	for i := 0; i < len(scores) && i < maxResults; i++ {
		results = append(results, scores[i].key)
	}

	return results
}

// levenshtein computes the Levenshtein distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create matrix
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
	}

	// Initialize first column
	for i := 0; i <= len(a); i++ {
		matrix[i][0] = i
	}

	// Initialize first row
	for j := 0; j <= len(b); j++ {
		matrix[0][j] = j
	}

	// Fill in the rest
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}

// min returns the minimum of three integers.
func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
