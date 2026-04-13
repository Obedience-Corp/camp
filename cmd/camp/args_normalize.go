package main

import (
	"strings"
)

// optionalValueFlags are long/short flag tokens whose flag definitions set
// NoOptDefVal so that a bare occurrence (no value) falls back to a sentinel
// default — see cmd/camp/intent/add.go. Cobra's pflag library refuses to
// consume the next space-separated argument as the value for these flags,
// which means users who run `--campaign foo "title"` end up with "foo"
// treated as a positional argument instead of the flag's value.
//
// normalizeOptionalValueFlagArgs rewrites the space-separated form to the
// `--flag=value` form before cobra ever sees os.Args, so the standard
// Unix convention (`--flag value`) works as expected while the bare-flag
// picker UX is preserved.
//
// Keep this list in sync with every flag that sets NoOptDefVal.
var optionalValueFlags = map[string]struct{}{
	"--campaign": {},
	"-c":         {},
}

// normalizeOptionalValueFlagArgs rewrites `--flag value` to `--flag=value`
// for flags listed in optionalValueFlags, returning a new slice. Bare
// occurrences (last arg, or followed by another flag starting with `-`)
// are left untouched so the NoOptDefVal sentinel still fires.
//
// Tokens after a standalone `--` separator are treated as positionals and
// not rewritten.
func normalizeOptionalValueFlagArgs(args []string) []string {
	out := make([]string, 0, len(args))
	sawDoubleDash := false
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if sawDoubleDash {
			out = append(out, arg)
			continue
		}
		if arg == "--" {
			sawDoubleDash = true
			out = append(out, arg)
			continue
		}

		// Skip `--flag=value` forms — already glued.
		if strings.Contains(arg, "=") {
			out = append(out, arg)
			continue
		}

		if _, ok := optionalValueFlags[arg]; !ok {
			out = append(out, arg)
			continue
		}

		// Flag is in our set. Look at the next token to decide whether to glue.
		next := ""
		hasNext := i+1 < len(args)
		if hasNext {
			next = args[i+1]
		}

		// Bare flag cases — keep the flag as-is and let NoOptDefVal apply.
		//   - last argument
		//   - next token is another flag (starts with `-`, or is the `--` separator)
		if !hasNext || strings.HasPrefix(next, "-") {
			out = append(out, arg)
			continue
		}

		// Glue: `--flag value` → `--flag=value`.
		out = append(out, arg+"="+next)
		i++
	}
	return out
}
