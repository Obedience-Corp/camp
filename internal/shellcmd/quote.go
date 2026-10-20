package shellcmd

import (
	"runtime"
	"strings"
)

// QuoteLiteral quotes s as one literal argument for a command camp prints
// for the user to paste: PowerShell on Windows, POSIX sh elsewhere.
func QuoteLiteral(s string) string {
	return quoteLiteralFor(runtime.GOOS, s)
}

func quoteLiteralFor(goos, s string) string {
	if goos == "windows" {
		return PowerShellLiteral(s)
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PowerShellLiteral single-quotes s for PowerShell. Single-quoted strings
// keep metacharacters and newlines literal; doubling an apostrophe is the
// only escape.
func PowerShellLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
