package explore

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// CleanText strips escape sequences and control characters, C1 included,
// from text read out of the camp. Line breaks and tabs become spaces.
func CleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, ansi.Strip(s))
}

// CleanDocument is CleanText for multi-line documents: it keeps line breaks
// and tabs.
func CleanDocument(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}
