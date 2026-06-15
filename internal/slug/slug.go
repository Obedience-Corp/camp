// Package slug generates filesystem-safe slugs from human text.
package slug

import (
	"regexp"
	"strings"
)

var (
	whitespacePattern = regexp.MustCompile(`\s+`)
	nonAlphanumeric   = regexp.MustCompile(`[^a-z0-9-]+`)
	multipleHyphens   = regexp.MustCompile(`-+`)
)

// Generate creates a URL-safe slug from a title.
//
// The slug is lowercased, non-alphanumeric characters are removed, whitespace
// becomes hyphens, and the result is limited to 5 words and 50 characters.
func Generate(title string) string {
	slug := strings.ToLower(title)
	slug = whitespacePattern.ReplaceAllString(slug, "-")
	slug = nonAlphanumeric.ReplaceAllString(slug, "")
	slug = multipleHyphens.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return ""
	}

	words := strings.Split(slug, "-")
	if len(words) > 5 {
		words = words[:5]
	}
	slug = strings.Join(words, "-")

	if len(slug) > 50 {
		slug = strings.TrimRight(slug[:50], "-")
	}
	return slug
}
