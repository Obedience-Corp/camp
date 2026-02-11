package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/obediencecorp/camp/internal/concept"
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
const maxCompletionCandidates = 8

// atCompletionCandidates returns completion candidates for an @ prefix query.
// query is the text after @ (e.g., "", "p", "p/", "p/fest").
func atCompletionCandidates(query, campaignRoot string) []string {
	if query == "" {
		// Show top-level @ shortcuts
		var out []string
		for prefix := range concept.DefaultAtPrefixes {
			out = append(out, prefix+"/")
		}
		sortStrings(out)
		return out
	}

	// Check if the query has a / (indicating a resolved prefix + path segment)
	slashIdx := strings.IndexByte(query, '/')
	if slashIdx < 0 {
		// No slash: fuzzy-match against top-level prefixes (e.g., "p", "w")
		var out []string
		for prefix := range concept.DefaultAtPrefixes {
			if fuzzyContains(prefix[1:], query) { // strip leading @
				out = append(out, prefix+"/")
			}
		}
		sortStrings(out)
		return out
	}

	// Has a slash: resolve the prefix part and list directory contents.
	// Split into prefix (e.g., "p") and rest (e.g., "" or "fe" or "fest/")
	prefixKey := query[:slashIdx]  // e.g., "p"
	rest := query[slashIdx+1:]     // e.g., "" or "fe"

	resolved, err := concept.ResolveAtPath("@" + prefixKey)
	if err != nil {
		return nil
	}

	// Determine the directory to list and the filter to apply.
	dirRel := resolved
	filter := ""
	if rest != "" && !strings.HasSuffix(rest, "/") {
		// rest contains a partial name — split at last /
		if lastSlash := strings.LastIndexByte(rest, '/'); lastSlash >= 0 {
			dirRel = resolved + "/" + rest[:lastSlash]
			filter = rest[lastSlash+1:]
		} else {
			filter = rest
		}
	} else if strings.HasSuffix(rest, "/") {
		dirRel = resolved + "/" + rest[:len(rest)-1]
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

	// Build candidate prefix (the portion before the filter)
	candidatePrefix := "@" + prefixKey + "/"
	if rest != "" {
		if lastSlash := strings.LastIndexByte(rest, '/'); lastSlash >= 0 {
			candidatePrefix += rest[:lastSlash+1]
		}
	}

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

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
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
