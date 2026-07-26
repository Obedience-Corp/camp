package ui_test

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/ui"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want string
	}{
		{name: "negative clamps to zero", in: -1, want: "0 B"},
		{name: "zero", in: 0, want: "0 B"},
		{name: "bytes below a kibibyte", in: 900, want: "900 B"},
		{name: "one byte under KB tier", in: 1023, want: "1023 B"},
		{name: "exactly one kibibyte", in: 1 << 10, want: "1.0 KB"},
		{name: "exactly one mebibyte", in: 1 << 20, want: "1.0 MB"},
		{name: "exactly one gibibyte", in: 1 << 30, want: "1.0 GB"},
		{name: "gigabytes do not render as thousands of MB", in: 5905580032, want: "5.5 GB"},
		{name: "terabyte tier", in: 1 << 40, want: "1.0 TB"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ui.FormatBytes(tc.in); got != tc.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatCount(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want string
	}{
		{name: "zero", in: 0, want: "0"},
		{name: "single digit", in: 9, want: "9"},
		{name: "three digits get no separator", in: 999, want: "999"},
		{name: "four digits", in: 1000, want: "1,000"},
		{name: "the node_modules case", in: 8432, want: "8,432"},
		{name: "seven digits", in: 1234567, want: "1,234,567"},
		{name: "negative", in: -8432, want: "-8,432"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ui.FormatCount(tc.in); got != tc.want {
				t.Errorf("FormatCount(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
