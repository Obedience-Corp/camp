// Package index provides content-based indexing for intent discovery.
package index

import (
	"regexp"
	"strings"
	"unicode"
)

// hashtagPattern matches hashtags in markdown content.
// Matches #word or #multi-word-tag after whitespace or at line start.
// Does not match markdown headings (# followed by space).
var hashtagPattern = regexp.MustCompile(`(?:^|\s)#([a-zA-Z][a-zA-Z0-9-]*)`)

// codeBlockPattern matches fenced code blocks.
var codeBlockPattern = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")

// inlineCodePattern matches inline code.
var inlineCodePattern = regexp.MustCompile("`[^`]+`")

// ExtractHashtags parses #hashtags from markdown content.
// It skips hashtags inside code blocks and inline code.
// Returns lowercase, deduplicated hashtags sorted alphabetically.
func ExtractHashtags(content string) []string {
	// Remove code blocks first
	cleaned := codeBlockPattern.ReplaceAllString(content, "")
	cleaned = inlineCodePattern.ReplaceAllString(cleaned, "")

	matches := hashtagPattern.FindAllStringSubmatch(cleaned, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []string

	for _, match := range matches {
		if len(match) > 1 {
			tag := strings.ToLower(match[1])
			if !seen[tag] {
				seen[tag] = true
				result = append(result, tag)
			}
		}
	}

	return result
}

// stopWords are common English words to exclude from indexing.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "must": true, "can": true, "this": true,
	"that": true, "these": true, "those": true, "it": true, "its": true,
	"we": true, "they": true, "you": true, "your": true, "our": true,
	"their": true, "and": true, "or": true, "but": true, "if": true,
	"then": true, "else": true, "when": true, "where": true, "what": true,
	"how": true, "why": true, "which": true, "who": true, "all": true,
	"any": true, "some": true, "no": true, "not": true, "from": true,
	"to": true, "for": true, "with": true, "by": true, "at": true,
	"on": true, "in": true, "of": true, "as": true, "so": true,
	"also": true, "just": true, "more": true, "most": true, "other": true,
	"into": true, "over": true, "such": true, "only": true, "own": true,
	"same": true, "than": true, "too": true, "very": true, "about": true,
}

// ExtractWords tokenizes content into normalized words for TF-IDF.
// It removes markdown syntax, lowercases, filters short words and stop words.
func ExtractWords(content string) []string {
	// Remove code blocks
	cleaned := codeBlockPattern.ReplaceAllString(content, "")
	cleaned = inlineCodePattern.ReplaceAllString(cleaned, "")

	// Remove markdown links [text](url) - keep text, remove url
	linkPattern := regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	cleaned = linkPattern.ReplaceAllString(cleaned, "$1")

	// Remove markdown headings markers
	headingPattern := regexp.MustCompile(`(?m)^#+\s*`)
	cleaned = headingPattern.ReplaceAllString(cleaned, "")

	// Remove other markdown syntax
	cleaned = strings.ReplaceAll(cleaned, "**", "")
	cleaned = strings.ReplaceAll(cleaned, "__", "")
	cleaned = strings.ReplaceAll(cleaned, "*", "")
	cleaned = strings.ReplaceAll(cleaned, "_", " ")
	cleaned = strings.ReplaceAll(cleaned, "#", " ")

	// Split on non-alphanumeric characters
	words := strings.FieldsFunc(cleaned, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})

	var result []string
	for _, word := range words {
		word = strings.ToLower(word)

		// Skip short words (less than 3 chars)
		if len(word) < 3 {
			continue
		}

		// Skip stop words
		if stopWords[word] {
			continue
		}

		result = append(result, word)
	}

	return result
}

// WordFrequency counts word occurrences in content.
func WordFrequency(content string) map[string]int {
	words := ExtractWords(content)
	freq := make(map[string]int)

	for _, word := range words {
		freq[word]++
	}

	return freq
}
