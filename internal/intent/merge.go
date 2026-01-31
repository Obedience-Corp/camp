package intent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MergeOptions configures the gather/merge operation.
type MergeOptions struct {
	Title    string   // Required: title for gathered intent
	Type     Type     // Optional: override type resolution
	Concept  string   // Optional: override concept resolution
	Priority Priority // Optional: override priority resolution
	Horizon  Horizon  // Optional: override horizon resolution
}

// GatherResult contains the result of a gather operation.
type GatherResult struct {
	NewIntent     *Intent   // The newly created merged intent
	SourceIntents []*Intent // Original intents that were gathered
}

// MergeIntents combines multiple source intents into a single gathered intent.
// The sources are not modified; the caller is responsible for archiving them.
func MergeIntents(sources []*Intent, opts MergeOptions) (*Intent, error) {
	if len(sources) < 2 {
		return nil, fmt.Errorf("need at least 2 intents to gather, got %d", len(sources))
	}

	if opts.Title == "" {
		return nil, fmt.Errorf("title is required for gathered intent")
	}

	now := time.Now()

	merged := &Intent{
		ID:           generateMergedID(opts.Title, now),
		Title:        opts.Title,
		Status:       StatusInbox,
		CreatedAt:    now,
		GatheredAt:   now,
		GatheredFrom: buildGatheredSources(sources),
	}

	// Resolve metadata from sources or use explicit overrides
	merged.Type = resolveType(sources, opts.Type)
	merged.Concept = resolveConcept(sources, opts.Concept)
	merged.Priority = resolvePriority(sources, opts.Priority)
	merged.Horizon = resolveHorizon(sources, opts.Horizon)
	merged.Tags = mergeTags(sources)
	merged.BlockedBy = mergeDependencies(sources, "blocked_by")
	merged.DependsOn = mergeDependencies(sources, "depends_on")

	// Build merged content
	merged.Content = buildMergedContent(sources, now)

	return merged, nil
}

// generateMergedID creates an ID for the gathered intent.
func generateMergedID(title string, t time.Time) string {
	slug := generateSlugFromTitle(title)
	timestamp := t.Format("20060102-150405")
	return fmt.Sprintf("%s-%s", timestamp, slug)
}

// generateSlugFromTitle creates a URL-friendly slug from a title.
func generateSlugFromTitle(title string) string {
	// Lowercase
	slug := strings.ToLower(title)

	// Replace spaces with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")

	// Remove non-alphanumeric chars (except hyphens)
	var b strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	slug = b.String()

	// Collapse multiple hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	// Trim leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Limit length
	if len(slug) > 50 {
		slug = slug[:50]
		// Don't end with hyphen
		slug = strings.TrimRight(slug, "-")
	}

	return slug
}

// buildGatheredSources creates GatheredSource entries from source intents.
func buildGatheredSources(sources []*Intent) []GatheredSource {
	result := make([]GatheredSource, len(sources))

	for i, src := range sources {
		result[i] = GatheredSource{
			ID:        src.ID,
			Title:     src.Title,
			Filename:  filepath.Base(src.Path),
			CreatedAt: src.CreatedAt,
			UpdatedAt: src.UpdatedAt,
			Type:      src.Type,
			Concept:   src.Concept,
			Priority:  src.Priority,
			Horizon:   src.Horizon,
			Tags:      src.Tags,
			Author:    src.Author,
			BlockedBy: src.BlockedBy,
			DependsOn: src.DependsOn,
		}
	}

	return result
}

// resolveType determines the type for the merged intent.
func resolveType(sources []*Intent, explicit Type) Type {
	if explicit != "" {
		return explicit
	}

	counts := make(map[Type]int)
	for _, s := range sources {
		if s.Type != "" {
			counts[s.Type]++
		}
	}

	return mostCommonType(counts)
}

// resolveConcept determines the concept for the merged intent.
func resolveConcept(sources []*Intent, explicit string) string {
	if explicit != "" {
		return explicit
	}

	counts := make(map[string]int)
	for _, s := range sources {
		if s.Concept != "" {
			counts[s.Concept]++
		}
	}

	return mostCommonString(counts)
}

// resolvePriority determines the priority for the merged intent.
// Uses highest priority from sources.
func resolvePriority(sources []*Intent, explicit Priority) Priority {
	if explicit != "" {
		return explicit
	}

	highest := PriorityLow
	for _, s := range sources {
		if priorityRank(s.Priority) > priorityRank(highest) {
			highest = s.Priority
		}
	}

	// Return empty if all sources had empty priority
	if highest == PriorityLow {
		for _, s := range sources {
			if s.Priority != "" {
				return highest
			}
		}
		return ""
	}

	return highest
}

// resolveHorizon determines the horizon for the merged intent.
// Uses most urgent horizon from sources.
func resolveHorizon(sources []*Intent, explicit Horizon) Horizon {
	if explicit != "" {
		return explicit
	}

	mostUrgent := HorizonLater
	for _, s := range sources {
		if horizonUrgency(s.Horizon) > horizonUrgency(mostUrgent) {
			mostUrgent = s.Horizon
		}
	}

	// Return empty if all sources had empty horizon
	if mostUrgent == HorizonLater {
		for _, s := range sources {
			if s.Horizon != "" {
				return mostUrgent
			}
		}
		return ""
	}

	return mostUrgent
}

// horizonUrgency returns a numeric rank for horizon comparison.
func horizonUrgency(h Horizon) int {
	switch h {
	case HorizonNow:
		return 3
	case HorizonNext:
		return 2
	case HorizonLater:
		return 1
	default:
		return 0
	}
}

// mergeTags unions all tags from sources, removing duplicates.
func mergeTags(sources []*Intent) []string {
	seen := make(map[string]bool)
	var result []string

	for _, s := range sources {
		for _, tag := range s.Tags {
			normalized := strings.ToLower(strings.TrimSpace(tag))
			if normalized != "" && !seen[normalized] {
				seen[normalized] = true
				result = append(result, normalized)
			}
		}
	}

	sort.Strings(result)
	return result
}

// mergeDependencies unions all dependencies from sources,
// excluding IDs of the source intents themselves.
func mergeDependencies(sources []*Intent, field string) []string {
	sourceIDs := make(map[string]bool)
	for _, s := range sources {
		sourceIDs[s.ID] = true
	}

	seen := make(map[string]bool)
	var result []string

	for _, s := range sources {
		var deps []string
		if field == "blocked_by" {
			deps = s.BlockedBy
		} else {
			deps = s.DependsOn
		}

		for _, dep := range deps {
			if !sourceIDs[dep] && !seen[dep] {
				seen[dep] = true
				result = append(result, dep)
			}
		}
	}

	return result
}

// mostCommonType returns the most common type from counts.
func mostCommonType(counts map[Type]int) Type {
	var maxType Type
	maxCount := 0

	for t, count := range counts {
		if count > maxCount {
			maxCount = count
			maxType = t
		}
	}

	return maxType
}

// mostCommonString returns the most common string from counts.
func mostCommonString(counts map[string]int) string {
	var maxStr string
	maxCount := 0

	for s, count := range counts {
		if count > maxCount {
			maxCount = count
			maxStr = s
		}
	}

	return maxStr
}

// buildMergedContent generates the markdown content for the gathered intent.
func buildMergedContent(sources []*Intent, gatheredAt time.Time) string {
	var b strings.Builder

	// Sources summary section
	b.WriteString("## Sources\n\n")
	b.WriteString(fmt.Sprintf("This intent was gathered from %d source intents on %s.\n\n",
		len(sources), gatheredAt.Format("2006-01-02")))

	b.WriteString("| Source | Created | Type | Priority |\n")
	b.WriteString("|--------|---------|------|----------|\n")
	for _, s := range sources {
		typeStr := string(s.Type)
		if typeStr == "" {
			typeStr = "-"
		}
		priorityStr := string(s.Priority)
		if priorityStr == "" {
			priorityStr = "-"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			s.Title,
			s.CreatedAt.Format("2006-01-02"),
			typeStr,
			priorityStr,
		))
	}

	b.WriteString("\n---\n\n")

	// Concatenate source content
	for i, s := range sources {
		b.WriteString(fmt.Sprintf("## From: %s\n\n", s.Title))

		body := extractBodyContent(s.Content)
		if strings.TrimSpace(body) != "" {
			b.WriteString(body)
		} else {
			b.WriteString("*No content in original intent.*\n")
		}

		if i < len(sources)-1 {
			b.WriteString("\n---\n\n")
		}
	}

	// Notes section for additional context
	b.WriteString("\n\n---\n\n## Gathered Notes\n\n")
	b.WriteString("<!-- Add any additional context or notes about this gathered intent -->\n")

	return b.String()
}

// extractBodyContent extracts the markdown body, skipping frontmatter and title.
func extractBodyContent(content string) string {
	lines := strings.Split(content, "\n")

	// Skip blank lines at start and first heading
	started := false
	skippedTitle := false
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !started {
			if trimmed == "" {
				continue
			}
			started = true
		}

		// Skip the first markdown heading (duplicate of title)
		if !skippedTitle && strings.HasPrefix(trimmed, "# ") {
			skippedTitle = true
			continue
		}

		result = append(result, line)
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}
