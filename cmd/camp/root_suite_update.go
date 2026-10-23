package main

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/obey-shared/updatecheck"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/version"
)

// suiteUpdateNoticeID keys the Festival suite update notice the way detector
// notices are keyed, so it reads as one more advisory rather than a new kind.
const suiteUpdateNoticeID = "festival-suite-update"

// Seams for the in-memory tests; production uses the shared checker and
// camp's home resolver.
var (
	suiteHome        = pathutil.Home
	suiteMaybeNotify = updatecheck.MaybeNotify
	suiteCheck       = updatecheck.Check
	// suiteFetcher is nil in production, which selects the shared checker's
	// GitHub releases client.
	suiteFetcher *updatecheck.Fetcher
)

// suiteHelpRendered records that the command printed help instead of doing
// its job, as group commands without a subcommand do. Help is not a command
// the user ran, so it gets no notice.
var suiteHelpRendered bool

// suiteAgentEnv adds the agent marker camp's ledger honors (internal/ledger)
// that the shared package's own agent list does not carry. An agent
// transcript is not a human who can upgrade.
var suiteAgentEnv = []string{"OBEY_SESSION"}

// suiteMachineFormats are --format values a program parses.
var suiteMachineFormats = map[string]bool{"json": true, "yaml": true, "simple": true}

// suiteMachineFlags select output another program reads: a path for the
// shell wrapper, a line to eval, a count, or a raw document.
var suiteMachineFlags = []string{"print", "shell-connect", "path-output", "shell", "yaml", "count", "plain"}

// suiteSilentCommands produce shell input, documentation, or help, or report
// versions themselves (version --check is the explicit path).
var suiteSilentCommands = map[string]bool{
	"help":       true,
	"gendocs":    true,
	"version":    true,
	"shell-init": true,
}

func init() {
	installSuiteUpdateNotice(rootCmd)
}

// installSuiteUpdateNotice hangs the notice off root's PersistentPostRun,
// which cobra runs only after a command's RunE returned nil, so a failed
// command never gains a notice. No subcommand defines its own persistent
// post-run, so every command inherits this one.
func installSuiteUpdateNotice(root *cobra.Command) {
	root.PersistentPostRun = func(cmd *cobra.Command, _ []string) {
		maybeNotifySuiteUpdate(cmd, os.Stderr)
	}
	showHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		suiteHelpRendered = true
		showHelp(cmd, args)
	})
}

// maybeNotifySuiteUpdate tells a human at a terminal that a newer Festival
// suite release exists. It runs after a command succeeded and must never
// change that outcome: every failure, including a panic, is silence.
func maybeNotifySuiteUpdate(cmd *cobra.Command, w io.Writer) {
	defer func() { _ = recover() }()

	bundle := version.Bundle
	if bundle == "" {
		return
	}
	if suiteNoticeSuppressed(cmd, stdoutIsTTY()) {
		return
	}
	opts, ok := suiteCheckOptions()
	if !ok {
		return
	}
	opts.Suppress = updatecheck.SuppressInput{
		StderrIsTTY: stderrIsTTY(),
		AgentMode:   suiteAgentRun(os.Getenv),
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if n := suiteMaybeNotify(ctx, bundle, opts); n != nil {
		renderSuiteNotice(w, *n)
	}
}

func renderSuiteNotice(w io.Writer, n updatecheck.Notice) {
	notice.Render(w, []notice.Notice{{
		ID:      suiteUpdateNoticeID,
		Message: n.Message,
		Command: n.Command,
		Action:  n.Kind,
	}})
}

// suiteCheckOptions resolves the shared cache through camp's home resolver.
// A missing, blank, or relative HOME skips the check instead of writing a
// cache under the working directory, where camp commit would pick it up.
func suiteCheckOptions() (updatecheck.Options, bool) {
	home, err := suiteHome()
	if err != nil || !filepath.IsAbs(home) {
		return updatecheck.Options{}, false
	}
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return updatecheck.Options{
		CachePath:      suiteCachePath(home),
		ExecutablePath: exe,
		Fetcher:        suiteFetcher,
	}, true
}

func suiteCachePath(home string) string {
	return filepath.Join(home, ".obey", "update-check", "suite.json")
}

// suiteNoticeSuppressed reports whether cmd's invocation must stay free of
// the suite notice. It answers from the command, its flags, and whether
// stdout is a terminal: captured stdout is a program reading camp, as in
// $(camp go --print) or eval "$(camp shell-init zsh)". The environment and
// stderr checks belong to the shared package.
func suiteNoticeSuppressed(cmd *cobra.Command, stdoutTTY bool) bool {
	if cmd == nil || !cmd.HasParent() || suiteHelpRendered || !stdoutTTY {
		return true
	}
	if suppressesNotices(cmd) {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Hidden || suiteSilentCommands[c.Name()] {
			return true
		}
	}
	if f := cmd.Flags().Lookup("format"); f != nil && suiteMachineFormats[f.Value.String()] {
		return true
	}
	for _, name := range suiteMachineFlags {
		if suiteFlagSelected(cmd, name) {
			return true
		}
	}
	return false
}

// suiteFlagSelected reports a bool flag set true, or any other flag given a
// non-empty value.
func suiteFlagSelected(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return false
	}
	if f.Value.Type() == "bool" {
		return f.Value.String() == "true"
	}
	return f.Changed && f.Value.String() != ""
}

func suiteAgentRun(getenv func(string) string) bool {
	for _, key := range suiteAgentEnv {
		if getenv(key) != "" {
			return true
		}
	}
	return false
}
