package pathsafe

import (
	"strings"
	"testing"
)

func TestValidateSegment(t *testing.T) {
	cases := []struct {
		value string
		ok    bool
	}{
		{"foo", true},
		{"foo-bar", true},
		{"foo_bar", true},
		{"Foo", true},
		{"v1.2", true},
		{"", false},
		{"foo bar", false},
		{"foo/bar", false},
		{`foo\bar`, false},
		{".hidden", false},
		{"-flaglike", false},
		{"foo\x00bar", false},
		{strings.Repeat("a", 81), false},
	}

	for _, tc := range cases {
		err := ValidateSegment("slug", tc.value)
		if (err == nil) != tc.ok {
			t.Fatalf("ValidateSegment(%q) error = %v, want ok %v", tc.value, err, tc.ok)
		}
	}
}
