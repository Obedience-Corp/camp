package main

// normalizeAutoWriteAlias rewrites the exact -aw token before cobra parses
// flags. Cobra/pflag only supports single-character shorthand flags.
func normalizeAutoWriteAlias(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, arg := range out {
		if arg == "-aw" {
			out[i] = "--auto-write"
		}
	}
	return out
}
