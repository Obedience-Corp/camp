// Package shellcmd chooses an interpreter for configured shell expressions.
package shellcmd

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Command creates a cancellable command for a stored shell expression. An
// empty interpreter uses PowerShell on Windows and sh on other platforms.
// Extra arguments remain distinct data values in both interpreters.
func Command(ctx context.Context, interpreter, expression string, extraArgs []string) (*exec.Cmd, error) {
	switch strings.ToLower(strings.TrimSpace(interpreter)) {
	case "", "auto":
		if runtime.GOOS == "windows" {
			return powerShellCommand(ctx, expression, extraArgs)
		}
		return shCommand(ctx, expression, extraArgs)
	case "powershell":
		return powerShellCommand(ctx, expression, extraArgs)
	case "sh":
		return shCommand(ctx, expression, extraArgs)
	default:
		return nil, camperrors.Newf("unsupported command shell %q (use powershell or sh)", interpreter)
	}
}

func shCommand(ctx context.Context, expression string, extraArgs []string) (*exec.Cmd, error) {
	sh := "sh"
	if runtime.GOOS == "windows" {
		var err error
		sh, err = findGitBashSh()
		if err != nil {
			return nil, err
		}
	}
	args := []string{"-c", expression}
	if len(extraArgs) > 0 {
		args = append([]string{"-c", expression + ` "$@"`, "sh"}, extraArgs...)
	}
	return exec.CommandContext(ctx, sh, args...), nil
}

func powerShellCommand(ctx context.Context, expression string, extraArgs []string) (*exec.Cmd, error) {
	name := "pwsh"
	if runtime.GOOS == "windows" {
		name = "powershell.exe"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, camperrors.Wrapf(err, "PowerShell interpreter %s was not found on PATH", name)
	}
	script, err := powerShellScript(expression, extraArgs)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script)), nil
}

func powerShellScript(expression string, extraArgs []string) (string, error) {
	// YAML block scalars commonly end with a newline. Splatting must remain
	// attached to the final command rather than becoming a new expression.
	expression = strings.TrimRight(expression, " \t\r\n")
	script := "$ErrorActionPreference = 'Stop'\n"
	if len(extraArgs) > 0 {
		literals := make([]string, len(extraArgs))
		for i, arg := range extraArgs {
			// Single-quoted PowerShell strings preserve metacharacters and
			// embedded newlines. Doubling an apostrophe is the only escape.
			literals[i] = "'" + strings.ReplaceAll(arg, "'", "''") + "'"
		}
		script += "$campArgs = [string[]]@(" + strings.Join(literals, ",") + ")\n"
		script += "& { " + expression + " @args } @campArgs\n"
	} else {
		script += "& { " + expression + " }\n"
	}
	script += "if ($LASTEXITCODE -is [int] -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\n"
	return script, nil
}

func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func findGitBashSh() (string, error) {
	if git, err := exec.LookPath("git.exe"); err == nil {
		gitRoot := filepath.Dir(filepath.Dir(git))
		for _, rel := range []string{"bin/sh.exe", "usr/bin/sh.exe"} {
			candidate := filepath.Join(gitRoot, filepath.FromSlash(rel))
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
		if root == "" {
			continue
		}
		candidate := filepath.Join(root, "Git", "bin", "sh.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidate := filepath.Join(local, "Programs", "Git", "bin", "sh.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", camperrors.New("stored POSIX shell expression requires Git Bash (sh.exe); install Git for Windows or set shell: powershell in the flow registry")
}
