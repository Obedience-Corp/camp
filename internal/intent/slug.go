package intent

import (
	"time"

	"github.com/Obedience-Corp/camp/internal/slug"
)

// SlugFromTitle creates a URL-safe slug from a title. It is the single slug
// generator shared by create, merge, and rename so they all produce identical
// slugs.
//
// Algorithm:
//  1. Convert to lowercase
//  2. Replace spaces with hyphens
//  3. Remove all characters except a-z, 0-9, and hyphens
//  4. Collapse multiple consecutive hyphens to single hyphen
//  5. Trim leading/trailing hyphens
//  6. Limit to 5 words maximum
//  7. Limit to 50 characters maximum
func SlugFromTitle(title string) string {
	return slug.Generate(title)
}

// GenerateID creates a unique intent ID from title and timestamp.
//
// Format: slug-YYYYMMDD-HHMMSS
// Example: add-dark-mode-toggle-20260119-153412
func GenerateID(title string, timestamp time.Time) string {
	suffix := timestamp.Format("20060102-150405")
	slug := SlugFromTitle(title)
	if slug == "" {
		return suffix
	}
	return slug + "-" + suffix
}
