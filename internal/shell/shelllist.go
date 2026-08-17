package shell

import (
	"fmt"
	"strings"
)

// shellDescriptions labels each supported shell in completion menus. A shell
// missing from here still completes; it just shows no description.
var shellDescriptions = map[string]string{
	"zsh":  "Zsh shell",
	"bash": "Bash shell",
	"fish": "Fish shell",
	"sh":   "POSIX sh (dash, busybox ash)",
}

// The three functions below render SupportedShells for each completion syntax.
// They exist so the list lives in exactly one place: the templates used to
// hardcode "zsh bash fish", so adding sh as a target left every shell's tab
// completion still offering the old three.

// shellWords renders the supported shells for bash's compgen -W.
func shellWords() string {
	return strings.Join(SupportedShells, " ")
}

// shellQuoted renders the supported shells as a zsh array literal body.
func shellQuoted() string {
	quoted := make([]string, 0, len(SupportedShells))
	for _, s := range SupportedShells {
		quoted = append(quoted, "'"+s+"'")
	}
	return strings.Join(quoted, " ")
}

// shellCompletions renders one fish complete command per supported shell.
func shellCompletions() string {
	var b strings.Builder
	for _, s := range SupportedShells {
		desc := shellDescriptions[s]
		if desc == "" {
			desc = s
		}
		fmt.Fprintf(&b, "complete -c camp -n \"__fish_seen_subcommand_from shell-init\" -a \"%s\" -d \"%s\"\n", s, desc)
	}
	return strings.TrimRight(b.String(), "\n")
}
