package main

// normalizeAutoWriteAlias rewrites the exact -aw token for commit commands
// after shortcut expansion and before cobra parses flags. Cobra/pflag only
// supports single-character shorthand flags.
func normalizeAutoWriteAlias(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	if !isAutoWriteCommitInvocation(out) {
		return out
	}
	for i, arg := range out {
		if arg == "-aw" {
			out[i] = "--auto-write"
		}
	}
	return out
}

func isAutoWriteCommitInvocation(args []string) bool {
	command, commandIndex := findFirstPositionalArg(args)
	switch command {
	case "commit":
		return true
	case "project", "worktrees":
		subcommand, _ := findNextPositionalArg(args, commandIndex+1)
		return subcommand == "commit"
	default:
		return false
	}
}

func findNextPositionalArg(args []string, start int) (string, int) {
	for i := start; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1], i + 1
			}
			return "", 0
		}
		if len(arg) == 0 || arg[0] != '-' {
			return arg, i
		}
	}
	return "", 0
}
