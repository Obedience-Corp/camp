package main

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// TestOptionalValueFlagsCoverNoOptDefValFlags guards against silent drift
// between the hand-maintained optionalValueFlags map and the set of
// non-bool flags with NoOptDefVal set across the cobra command tree.
//
// pflag auto-assigns NoOptDefVal="true" on every bool flag (so bare
// --verbose means --verbose=true); those are a built-in pflag mechanism,
// not the "optional string value" pattern the preprocessor targets and
// they are excluded from this check.
//
// For non-bool flags, NoOptDefVal is the deliberate "bare flag means
// sentinel default" pattern — see cmd/camp/intent/add.go for the only
// current example. Without this guard, someone adding a second such flag
// would reintroduce the original bug: `--flag value` never consumes the
// space-separated token, leaving `value` as a stray positional argument.
//
// If this test fails, add every reported flag token to optionalValueFlags
// in cmd/camp/args_normalize.go.
func TestOptionalValueFlagsCoverNoOptDefValFlags(t *testing.T) {
	var missing []string
	seen := map[string]struct{}{}

	check := func(f *pflag.Flag) {
		if f.NoOptDefVal == "" {
			return
		}
		// Bool flags get NoOptDefVal="true" automatically from pflag and
		// must not be rewritten to `--flag=value` (which would fail bool
		// parsing on anything that isn't a bool literal). Skip them.
		if f.Value.Type() == "bool" {
			return
		}
		long := "--" + f.Name
		if _, ok := seen[long]; !ok {
			seen[long] = struct{}{}
			if _, ok := optionalValueFlags[long]; !ok {
				missing = append(missing, long)
			}
		}
		if f.Shorthand != "" {
			short := "-" + f.Shorthand
			if _, ok := seen[short]; !ok {
				seen[short] = struct{}{}
				if _, ok := optionalValueFlags[short]; !ok {
					missing = append(missing, short)
				}
			}
		}
	}

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.PersistentFlags().VisitAll(check)
		cmd.Flags().VisitAll(check)
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)

	if len(missing) > 0 {
		t.Errorf(
			"non-bool flags with NoOptDefVal set but missing from optionalValueFlags in cmd/camp/args_normalize.go:\n  %v\n\n"+
				"Add each token to the optionalValueFlags map so space-separated `--flag value` parses correctly.",
			missing,
		)
	}
}
