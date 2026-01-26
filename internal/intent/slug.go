package intent

import (
	"regexp"
	"strings"
	"time"
)

var (
	// whitespacePattern matches any whitespace character.
	whitespacePattern = regexp.MustCompile(`\s+`)
	// nonAlphanumeric matches any character that isn't a-z, 0-9, or hyphen.
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]+`)
	// multipleHyphens matches two or more consecutive hyphens.
	multipleHyphens = regexp.MustCompile(`-+`)
)

// GenerateSlug creates a URL-safe slug from a title.
//
// Algorithm:
//  1. Convert to lowercase
//  2. Replace spaces with hyphens
//  3. Remove all characters except a-z, 0-9, and hyphens
//  4. Collapse multiple consecutive hyphens to single hyphen
//  5. Trim leading/trailing hyphens
//  6. Limit to 5 words maximum
//  7. Limit to 50 characters maximum
func GenerateSlug(title string) string {
	// 1. Lowercase
	slug := strings.ToLower(title)

	// 2. Replace all whitespace (spaces, tabs, newlines) with hyphens
	slug = whitespacePattern.ReplaceAllString(slug, "-")

	// 3. Remove non-alphanumeric (keep hyphens)
	slug = nonAlphanumeric.ReplaceAllString(slug, "")

	// 4. Collapse multiple hyphens
	slug = multipleHyphens.ReplaceAllString(slug, "-")

	// 5. Trim leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Handle empty slug after sanitization
	if slug == "" {
		return ""
	}

	// 6. Limit to 5 words
	words := strings.Split(slug, "-")
	if len(words) > 5 {
		words = words[:5]
	}
	slug = strings.Join(words, "-")

	// 7. Limit to 50 characters (don't end on hyphen)
	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.TrimRight(slug, "-")
	}

	return slug
}

// GenerateID creates a unique intent ID from title and timestamp.
//
// Format: slug-YYYYMMDD-HHMMSS
// Example: add-dark-mode-toggle-20260119-153412
func GenerateID(title string, timestamp time.Time) string {
	suffix := timestamp.Format("20060102-150405")
	slug := GenerateSlug(title)
	if slug == "" {
		return suffix
	}
	return slug + "-" + suffix
}
