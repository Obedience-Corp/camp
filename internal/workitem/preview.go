package workitem

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// findPrimaryDoc returns README.md if it exists, else the first top-level .md file.
func findPrimaryDoc(dir string) string {
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); err == nil {
		return readme
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// extractSummary returns the first maxLen characters of text, skipping blank lines.
func extractSummary(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Skip heading lines at the start
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Skip frontmatter delimiters
		if trimmed == "---" {
			continue
		}
		lines = append(lines, trimmed)
		// Collect enough lines
		joined := strings.Join(lines, " ")
		if len(joined) >= maxLen {
			break
		}
	}

	result := strings.Join(lines, " ")
	if len(result) > maxLen {
		// Truncate at word boundary
		result = result[:maxLen]
		if idx := strings.LastIndex(result, " "); idx > maxLen/2 {
			result = result[:idx]
		}
		result += "..."
	}
	return result
}

// extractSummaryFromFile reads a file and returns its summary.
func extractSummaryFromFile(path string, maxLen int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return extractSummary(string(content), maxLen)
}

// extractFirstHeading returns the first # heading from a markdown file.
func extractFirstHeading(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	inFrontmatter := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip YAML frontmatter
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter {
			continue
		}

		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}
	return ""
}

// humanizeBasename converts "camp-workitem-dashboard" to "Camp Workitem Dashboard".
func humanizeBasename(name string) string {
	// Replace hyphens and underscores with spaces
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	// Title case each word
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			runes := []rune(w)
			runes[0] = unicode.ToUpper(runes[0])
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}
