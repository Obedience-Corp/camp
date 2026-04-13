package main

import (
	"reflect"
	"testing"
)

func TestNormalizeOptionalValueFlagArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "space-separated long flag gets glued",
			in:   []string{"intent", "add", "--campaign", "dest", "Title"},
			want: []string{"intent", "add", "--campaign=dest", "Title"},
		},
		{
			name: "space-separated short flag gets glued",
			in:   []string{"intent", "add", "-c", "dest", "Title"},
			want: []string{"intent", "add", "-c=dest", "Title"},
		},
		{
			name: "already glued long flag left alone",
			in:   []string{"intent", "add", "--campaign=dest", "Title"},
			want: []string{"intent", "add", "--campaign=dest", "Title"},
		},
		{
			name: "already glued short flag left alone",
			in:   []string{"intent", "add", "-c=dest", "Title"},
			want: []string{"intent", "add", "-c=dest", "Title"},
		},
		{
			name: "bare long flag at end of args stays bare for NoOptDefVal",
			in:   []string{"intent", "add", "--campaign"},
			want: []string{"intent", "add", "--campaign"},
		},
		{
			name: "bare long flag followed by another flag stays bare",
			in:   []string{"intent", "add", "--campaign", "--no-commit"},
			want: []string{"intent", "add", "--campaign", "--no-commit"},
		},
		{
			name: "bare short flag followed by long flag stays bare",
			in:   []string{"intent", "add", "-c", "--no-commit"},
			want: []string{"intent", "add", "-c", "--no-commit"},
		},
		{
			name: "flag followed by `--` stays bare",
			in:   []string{"intent", "add", "--campaign", "--", "Title"},
			want: []string{"intent", "add", "--campaign", "--", "Title"},
		},
		{
			name: "token after `--` separator is never rewritten",
			in:   []string{"intent", "add", "--", "--campaign", "dest"},
			want: []string{"intent", "add", "--", "--campaign", "dest"},
		},
		{
			name: "unrelated flags untouched",
			in:   []string{"intent", "add", "--no-commit", "Title", "--type", "feature"},
			want: []string{"intent", "add", "--no-commit", "Title", "--type", "feature"},
		},
		{
			name: "end-to-end test invocation",
			in:   []string{"intent", "add", "--campaign", "intent-target-dest", "Targeted Intent", "--no-commit"},
			want: []string{"intent", "add", "--campaign=intent-target-dest", "Targeted Intent", "--no-commit"},
		},
		{
			name: "empty args",
			in:   []string{},
			want: []string{},
		},
		{
			name: "no optional-value flags present",
			in:   []string{"project", "list"},
			want: []string{"project", "list"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeOptionalValueFlagArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("normalizeOptionalValueFlagArgs(%v)\n  got:  %v\n  want: %v", tc.in, got, tc.want)
			}
		})
	}
}
