package ui

import (
	"fmt"
	"strconv"
	"strings"
)

// byteTiers are ordered largest first so the first match is the right unit.
// Binary divisors with decimal labels match what the rest of camp's output
// already does; the distinction never changes a user's decision at these
// magnitudes, and switching labels mid-tool would be worse than the imprecision.
var byteTiers = []struct {
	limit int64
	unit  string
}{
	{limit: 1 << 40, unit: "TB"},
	{limit: 1 << 30, unit: "GB"},
	{limit: 1 << 20, unit: "MB"},
	{limit: 1 << 10, unit: "KB"},
}

// FormatBytes renders a byte count for humans, e.g. "5.5 GB" or "900 B".
//
// Tiers run to TB because artifact roots are routinely gigabytes; a formatter
// that stops at MB reports a 5 GB video directory as "5632.0 MB", which is
// the kind of number a reader has to decode rather than read.
func FormatBytes(b int64) string {
	if b < 0 {
		return "0 B"
	}
	for _, tier := range byteTiers {
		if b >= tier.limit {
			return fmt.Sprintf("%.1f %s", float64(b)/float64(tier.limit), tier.unit)
		}
	}
	return fmt.Sprintf("%d B", b)
}

// FormatCount renders an integer with thousands separators, e.g. "1,204".
//
// File counts are the number a user scans to decide whether a directory is
// what they think it is, and "1204" versus "8432" is much harder to compare at
// a glance than "1,204" versus "8,432".
func FormatCount(n int) string {
	s := strconv.Itoa(n)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	var out strings.Builder
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	if negative {
		return "-" + out.String()
	}
	return out.String()
}
