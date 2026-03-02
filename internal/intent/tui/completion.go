package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// completionState tracks @ prefix autocompletion in the body editor.
type completionState struct {
	active     bool
	query      string   // text after @ (e.g., "p/fe")
	candidates []string // filtered candidates
	selected   int      // currently selected index
	atOffset   int      // column position of @ in the line
}

// maxCompletionCandidates limits the displayed candidate list.
const maxCompletionCandidates = 4

// atCompletionCandidates returns completion candidates for an @ prefix query.
// query is the text after @ (e.g., "", "de", "workflow/", "workflow/design/arch").
// shortcuts maps shortcut keys to campaign-relative paths (e.g., "de" → "workflow/design/").
func atCompletionCandidates(query, campaignRoot string, shortcuts map[string]string) []string {
	if query == "" {
		// Show all shortcut paths as candidates
		seen := make(map[string]bool)
		var out []string
		for _, path := range shortcuts {
			display := "@" + path
			if !seen[display] {
				seen[display] = true
				out = append(out, display)
			}
		}
		sort.Strings(out)
		if len(out) > maxCompletionCandidates {
			out = out[:maxCompletionCandidates]
		}
		return out
	}

	slashIdx := strings.IndexByte(query, '/')
	if slashIdx < 0 {
		// No slash: fuzzy-match against shortcut keys AND shortcut paths
		seen := make(map[string]bool)
		var out []string
		for key, path := range shortcuts {
			display := "@" + path
			if seen[display] {
				continue
			}
			// Match against the shortcut key or the path
			if fuzzyContains(key, query) || fuzzyContains(path, query) {
				seen[display] = true
				out = append(out, display)
			}
		}
		sort.Strings(out)
		if len(out) > maxCompletionCandidates {
			out = out[:maxCompletionCandidates]
		}
		return out
	}

	// Has a slash: treat the entire query as a campaign-relative path.
	// Split into directory portion + filter.
	dirRel := query[:slashIdx]
	rest := query[slashIdx+1:]

	filter := ""
	if rest != "" && !strings.HasSuffix(rest, "/") {
		if lastSlash := strings.LastIndexByte(rest, '/'); lastSlash >= 0 {
			dirRel = query[:slashIdx+1+lastSlash]
			filter = rest[lastSlash+1:]
		} else {
			filter = rest
		}
	} else if strings.HasSuffix(rest, "/") {
		dirRel = query[:len(query)-1]
	}

	fullPath := filepath.Join(campaignRoot, dirRel)
	info, err := os.Stat(fullPath)
	if err != nil || !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil
	}

	// Build the candidate prefix (everything up to the filter)
	candidatePrefix := "@" + dirRel + "/"

	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filter != "" && !fuzzyContains(name, filter) {
			continue
		}
		candidate := candidatePrefix + name
		if e.IsDir() {
			candidate += "/"
		}
		out = append(out, candidate)
	}

	if len(out) > maxCompletionCandidates {
		out = out[:maxCompletionCandidates]
	}
	return out
}

// extractAtQuery finds the @ word at the cursor position in a line.
// Returns the query (text after @) and the column offset of @.
// Returns "", -1 if no @ word is found at cursor.
func extractAtQuery(line string, cursorCol int) (query string, atCol int) {
	if cursorCol > len(line) {
		cursorCol = len(line)
	}

	// Walk backwards from cursor to find @
	for i := cursorCol - 1; i >= 0; i-- {
		ch := rune(line[i])
		if ch == '@' {
			return line[i+1 : cursorCol], i
		}
		// Stop at whitespace or certain punctuation (completion boundary)
		if unicode.IsSpace(ch) {
			return "", -1
		}
	}

	return "", -1
}

// fuzzyContains checks if needle chars appear in haystack in order.
func fuzzyContains(haystack, needle string) bool {
	h := strings.ToLower(haystack)
	n := strings.ToLower(needle)
	hi := 0
	for ni := 0; ni < len(n); ni++ {
		found := false
		for hi < len(h) {
			if h[hi] == n[ni] {
				hi++
				found = true
				break
			}
			hi++
		}
		if !found {
			return false
		}
	}
	return true
}

// completionView renders the completion popup.
func completionView(cs *completionState) string {
	if !cs.active || len(cs.candidates) == 0 {
		return ""
	}

	var b strings.Builder
	for i, c := range cs.candidates {
		style := lipgloss.NewStyle().Foreground(pal.TextSecondary)
		if i == cs.selected {
			style = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Accent).
				Background(pal.BgSelected)
		}
		b.WriteString(style.Render("  " + c))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
