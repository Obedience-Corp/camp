package explore

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// CleanText strips escape sequences, control characters (C1 included), and
// invisible format characters from text read out of the camp. Line breaks and
// tabs become spaces.
func CleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unprintable(r):
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
		if r == '\n' || r == '\t' || !unprintable(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

// unprintable reports a rune a terminal acts on or hides: a control, or a
// format character such as a bidi override or a zero-width space.
// The join controls (U+200C, U+200D) stay: emoji sequences and Persian and
// Indic words need them, and they cannot reorder or cover text.
func unprintable(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Join_Control, r)
}
