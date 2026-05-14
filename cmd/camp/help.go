package main

import "strings"

// cleanFlagUsages removes internal optional-value sentinels from runtime help.
// Bare optional flags use a NUL-prefixed NoOptDefVal so command parsing can
// distinguish `--campaign` from `--campaign pick`; pflag renders that value
// verbatim in help unless we clean the already-formatted usage line.
func cleanFlagUsages(usages string) string {
	lines := strings.Split(usages, "\n")
	for i, line := range lines {
		lines[i] = cleanFlagUsageLine(line)
	}
	return strings.Join(lines, "\n")
}

func cleanFlagUsageLine(line string) string {
	for {
		nul := strings.IndexRune(line, '\x00')
		if nul == -1 {
			return line
		}

		start := strings.LastIndex(line[:nul], `[="`)
		if start == -1 {
			line = line[:nul] + line[nul+1:]
			continue
		}

		line = line[:start] + "   " + line[nul+1:]
	}
}
