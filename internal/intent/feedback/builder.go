package feedback

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Builder generates and updates FEEDBACK intent markdown files.
type Builder struct {
	intentsDir string
}

// NewBuilder creates a builder that writes intents to the given intents directory.
func NewBuilder(intentsDir string) *Builder {
	return &Builder{intentsDir: intentsDir}
}

// BuildOrUpdate creates or updates a FEEDBACK intent file for a festival.
// Returns the path to the intent file and whether it was created (vs updated).
func (b *Builder) BuildOrUpdate(festival FestivalInfo, observations []Observation) (string, bool, error) {
	filename := fmt.Sprintf("FEEDBACK_%s.md", festival.ID)
	intentPath := filepath.Join(b.intentsDir, "inbox", filename)

	// Ensure inbox directory exists
	if err := os.MkdirAll(filepath.Dir(intentPath), 0755); err != nil {
		return "", false, fmt.Errorf("creating inbox directory: %w", err)
	}

	// Check if intent already exists
	existingContent, err := os.ReadFile(intentPath)
	if err == nil {
		// Update existing intent
		updated, err := b.updateIntent(string(existingContent), festival, observations)
		if err != nil {
			return "", false, fmt.Errorf("updating intent: %w", err)
		}
		if err := os.WriteFile(intentPath, []byte(updated), 0644); err != nil {
			return "", false, fmt.Errorf("writing intent: %w", err)
		}
		return intentPath, false, nil
	}

	if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("reading existing intent: %w", err)
	}

	// Create new intent
	content := b.buildIntent(festival, observations)
	if err := os.WriteFile(intentPath, []byte(content), 0644); err != nil {
		return "", false, fmt.Errorf("writing intent: %w", err)
	}

	return intentPath, true, nil
}

// buildIntent generates a new FEEDBACK intent markdown document.
func (b *Builder) buildIntent(festival FestivalInfo, observations []Observation) string {
	now := time.Now().Format("2006-01-02")

	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id: FEEDBACK_%s\n", festival.ID))
	sb.WriteString(fmt.Sprintf("title: \"Feedback: %s (%s)\"\n", festival.Name, festival.ID))
	sb.WriteString("type: feedback\n")
	sb.WriteString("concept: feedback/festivals\n")
	sb.WriteString("status: inbox\n")
	sb.WriteString(fmt.Sprintf("created_at: %s\n", now))
	sb.WriteString("author: camp-gather\n")
	sb.WriteString("priority: medium\n")
	sb.WriteString("horizon: now\n")
	sb.WriteString("tags:\n")
	sb.WriteString("  - feedback\n")
	sb.WriteString("  - auto-gathered\n")
	sb.WriteString("---\n\n")

	// Title
	sb.WriteString(fmt.Sprintf("# Feedback: %s (%s)\n\n", festival.Name, festival.ID))
	sb.WriteString(fmt.Sprintf("Feedback gathered from festival %s. Check items as addressed.\n", festival.ID))

	// Group observations by criteria
	b.writeObservations(&sb, observations)

	// Footer
	sb.WriteString(fmt.Sprintf("\n---\n*Gathered by `camp gather feedback` on %s*\n", now))

	return sb.String()
}

// updateIntent merges new observations into an existing intent, preserving checkbox state.
func (b *Builder) updateIntent(existing string, festival FestivalInfo, newObs []Observation) (string, error) {
	// Parse existing checkbox states
	checkedIDs := parseCheckedIDs(existing)

	// Parse all existing observations from the document
	existingObs := parseExistingObservations(existing)

	// Merge: add new observations to existing
	allObs := mergeObservations(existingObs, newObs)

	// Rebuild the document
	now := time.Now().Format("2006-01-02")

	// Preserve frontmatter from existing document
	frontmatter := extractFrontmatter(existing)

	var sb strings.Builder
	sb.WriteString(frontmatter)
	sb.WriteString("\n")

	// Title
	sb.WriteString(fmt.Sprintf("# Feedback: %s (%s)\n\n", festival.Name, festival.ID))
	sb.WriteString(fmt.Sprintf("Feedback gathered from festival %s. Check items as addressed.\n", festival.ID))

	// Write observations with preserved checkbox state
	b.writeObservationsWithChecked(&sb, allObs, checkedIDs)

	// Footer
	sb.WriteString(fmt.Sprintf("\n---\n*Gathered by `camp gather feedback` on %s*\n", now))

	return sb.String(), nil
}

// writeObservations writes observations grouped by criteria.
func (b *Builder) writeObservations(sb *strings.Builder, observations []Observation) {
	grouped := groupByCriteria(observations)
	criteria := sortedKeys(grouped)

	for _, c := range criteria {
		fmt.Fprintf(sb, "\n## %s\n\n", c)
		for _, obs := range grouped[c] {
			b.writeObservationLine(sb, obs, false)
		}
	}
}

// writeObservationsWithChecked writes observations preserving checkbox state.
func (b *Builder) writeObservationsWithChecked(sb *strings.Builder, observations []Observation, checkedIDs map[string]bool) {
	grouped := groupByCriteria(observations)
	criteria := sortedKeys(grouped)

	for _, c := range criteria {
		fmt.Fprintf(sb, "\n## %s\n\n", c)
		for _, obs := range grouped[c] {
			checked := checkedIDs[obs.ID]
			b.writeObservationLine(sb, obs, checked)
		}
	}
}

// writeObservationLine writes a single observation as a checkbox line.
func (b *Builder) writeObservationLine(sb *strings.Builder, obs Observation, checked bool) {
	checkbox := "- [ ]"
	if checked {
		checkbox = "- [x]"
	}

	// Format timestamp (extract date portion)
	date := formatObsDate(obs.Timestamp)

	// Build the line
	if obs.Severity != "" {
		fmt.Fprintf(sb, "%s **%s** [%s] (%s): %s\n", checkbox, obs.ID, obs.Severity, date, obs.Observation)
	} else {
		fmt.Fprintf(sb, "%s **%s** (%s): %s\n", checkbox, obs.ID, date, obs.Observation)
	}

	// Add suggestion as blockquote
	if obs.Suggestion != "" {
		fmt.Fprintf(sb, "  > **Suggestion**: %s\n", obs.Suggestion)
	}

	sb.WriteString("\n")
}

// parseCheckedIDs extracts observation IDs that have been checked off.
var checkboxPattern = regexp.MustCompile(`- \[([ x])\] \*\*(\d+)\*\*`)

func parseCheckedIDs(content string) map[string]bool {
	result := make(map[string]bool)
	matches := checkboxPattern.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			result[m[2]] = (m[1] == "x")
		}
	}
	return result
}

// obsLinePattern matches observation lines to extract their data.
var obsLinePattern = regexp.MustCompile(`- \[[ x]\] \*\*(\d+)\*\*`)

// parseExistingObservations extracts observation data from existing intent markdown.
// This is a best-effort parse - it captures IDs from checkbox lines.
func parseExistingObservations(content string) []Observation {
	var obs []Observation
	lines := strings.Split(content, "\n")

	currentCriteria := ""
	for i, line := range lines {
		// Track current criteria heading
		if after, found := strings.CutPrefix(line, "## "); found {
			currentCriteria = after
			continue
		}

		// Match observation lines
		match := obsLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		id := match[1]

		// Extract the observation text after the ID and date
		obsText := extractObsText(line)

		// Check for severity in brackets
		severity := extractSeverity(line)

		// Check for suggestion on next line
		suggestion := ""
		if i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			if after, found := strings.CutPrefix(nextLine, "> **Suggestion**: "); found {
				suggestion = after
			}
		}

		// Extract date
		date := extractDate(line)

		obs = append(obs, Observation{
			ID:          id,
			Criteria:    currentCriteria,
			Observation: obsText,
			Severity:    severity,
			Suggestion:  suggestion,
			Timestamp:   date,
		})
	}

	return obs
}

// extractObsText extracts the observation text from a checkbox line.
func extractObsText(line string) string {
	// Pattern: - [ ] **ID** [severity] (date): text  OR  - [ ] **ID** (date): text
	// Find ): and take everything after it
	idx := strings.Index(line, "): ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+3:])
}

// extractSeverity extracts severity from brackets in a checkbox line.
var severityPattern = regexp.MustCompile(`\[(low|medium|high)\]`)

func extractSeverity(line string) string {
	match := severityPattern.FindStringSubmatch(line)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// extractDate extracts the date from parentheses in a checkbox line.
var datePattern = regexp.MustCompile(`\((\d{4}-\d{2}-\d{2})\)`)

func extractDate(line string) string {
	match := datePattern.FindStringSubmatch(line)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// extractFrontmatter extracts the YAML frontmatter block including delimiters.
func extractFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ""
	}
	return content[:4+end+4] // Include closing ---
}

// mergeObservations merges new observations into existing ones.
// Existing observations are preserved; new ones with the same ID are skipped.
func mergeObservations(existing, newObs []Observation) []Observation {
	seen := make(map[string]struct{}, len(existing))
	for _, obs := range existing {
		seen[obs.ID] = struct{}{}
	}

	merged := make([]Observation, len(existing))
	copy(merged, existing)

	for _, obs := range newObs {
		if _, exists := seen[obs.ID]; !exists {
			merged = append(merged, obs)
			seen[obs.ID] = struct{}{}
		}
	}

	return merged
}

// groupByCriteria groups observations by their criteria field.
func groupByCriteria(observations []Observation) map[string][]Observation {
	grouped := make(map[string][]Observation)
	for _, obs := range observations {
		grouped[obs.Criteria] = append(grouped[obs.Criteria], obs)
	}
	return grouped
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string][]Observation) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// formatObsDate extracts a YYYY-MM-DD date from a timestamp string.
func formatObsDate(timestamp string) string {
	// Try parsing as RFC3339
	t, err := time.Parse(time.RFC3339, timestamp)
	if err == nil {
		return t.Format("2006-01-02")
	}

	// Try YYYY-MM-DD directly
	if len(timestamp) >= 10 {
		return timestamp[:10]
	}

	return timestamp
}
