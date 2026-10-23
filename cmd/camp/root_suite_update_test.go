package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/obey-shared/updatecheck"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/version"
)

// suiteNotifyCall records what the wiring handed the shared checker, so the
// tests prove which version and cache path reach it without touching disk.
type suiteNotifyCall struct {
	local string
	opts  updatecheck.Options
}

// stubSuiteNotice replaces every side-effecting seam: no home lookup, no
// cache, no network, both streams a terminal. It returns the recorded calls.
func stubSuiteNotice(t *testing.T, bundle string, reply *updatecheck.Notice) *[]suiteNotifyCall {
	t.Helper()
	prevBundle, prevVersion := version.Bundle, version.Version
	prevHome, prevNotify := suiteHome, suiteMaybeNotify
	prevOut, prevErr := stdoutIsTTY, stderrIsTTY
	prevHelp := suiteHelpRendered
	t.Cleanup(func() {
		version.Bundle, version.Version = prevBundle, prevVersion
		suiteHome, suiteMaybeNotify = prevHome, prevNotify
		stdoutIsTTY, stderrIsTTY = prevOut, prevErr
		suiteHelpRendered = prevHelp
	})

	version.Bundle = bundle
	version.Version = "0.16.3"
	suiteHelpRendered = false
	stdoutIsTTY = func() bool { return true }
	stderrIsTTY = func() bool { return true }

	calls := &[]suiteNotifyCall{}
	suiteHome = func() (string, error) {
		*calls = append(*calls, suiteNotifyCall{local: "home-lookup"})
		return "/fixture/home", nil
	}
	suiteMaybeNotify = func(_ context.Context, local string, opts updatecheck.Options) *updatecheck.Notice {
		*calls = append(*calls, suiteNotifyCall{local: local, opts: opts})
		return reply
	}
	return calls
}

func newSuiteTestTree(t *testing.T) (*cobra.Command, *cobra.Command) {
	t.Helper()
	root := &cobra.Command{Use: "camp", RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	leaf := &cobra.Command{Use: "status", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(leaf)
	return root, leaf
}

func TestSuiteNotice_EmptyBundleDoesNoWork(t *testing.T) {
	calls := stubSuiteNotice(t, "", &updatecheck.Notice{Message: "m", Command: "c", Kind: "run"})
	_, leaf := newSuiteTestTree(t)

	var out bytes.Buffer
	maybeNotifySuiteUpdate(leaf, &out)

	if len(*calls) != 0 {
		t.Fatalf("a build outside a suite release resolved home or ran the checker: %+v", *calls)
	}
	if out.Len() != 0 {
		t.Fatalf("a build outside a suite release printed %q", out.String())
	}
}

func TestSuiteNotice_PassesTheSuiteVersionAndCampCachePath(t *testing.T) {
	withSuiteColorProfile(t, termenv.Ascii)
	calls := stubSuiteNotice(t, "v0.3.20", &updatecheck.Notice{
		Message: "Festival suite update available (0.3.20 → 0.3.24)",
		Command: "festival update",
		Kind:    "run",
	})
	_, leaf := newSuiteTestTree(t)

	var out bytes.Buffer
	maybeNotifySuiteUpdate(leaf, &out)

	var checker []suiteNotifyCall
	for _, c := range *calls {
		if c.local != "home-lookup" {
			checker = append(checker, c)
		}
	}
	if len(checker) != 1 {
		t.Fatalf("checker calls = %d, want 1: %+v", len(checker), *calls)
	}
	got := checker[0]
	if got.local != "v0.3.20" {
		t.Fatalf("checker got local version %q, want the suite bundle v0.3.20 (never camp's own %q)", got.local, version.Version)
	}
	if want := "/fixture/home/.obey/update-check/suite.json"; got.opts.CachePath != want {
		t.Fatalf("CachePath = %q, want %q", got.opts.CachePath, want)
	}
	if !got.opts.Suppress.StderrIsTTY {
		t.Fatal("stderr terminal state did not reach the checker")
	}
	want := "⚠ notice: Festival suite update available (0.3.20 → 0.3.24)\n  run: festival update\n"
	if out.String() != want {
		t.Fatalf("notice = %q, want %q", out.String(), want)
	}
}

func TestSuiteNotice_SeeHintRendersSee(t *testing.T) {
	withSuiteColorProfile(t, termenv.Ascii)
	stubSuiteNotice(t, "v0.3.20", &updatecheck.Notice{Message: "m", Command: "https://example.test/latest", Kind: "see"})
	_, leaf := newSuiteTestTree(t)

	var out bytes.Buffer
	maybeNotifySuiteUpdate(leaf, &out)

	if !strings.Contains(out.String(), "  see: https://example.test/latest\n") {
		t.Fatalf("see hint rendered as %q", out.String())
	}
}

func TestSuiteNotice_CheckerPanicIsSilence(t *testing.T) {
	stubSuiteNotice(t, "v0.3.20", nil)
	suiteMaybeNotify = func(context.Context, string, updatecheck.Options) *updatecheck.Notice {
		panic("checker bug")
	}
	_, leaf := newSuiteTestTree(t)

	var out bytes.Buffer
	maybeNotifySuiteUpdate(leaf, &out)
	if out.Len() != 0 {
		t.Fatalf("panic path printed %q", out.String())
	}
}

func TestSuiteNotice_HomeFailureSkipsTheChecker(t *testing.T) {
	calls := stubSuiteNotice(t, "v0.3.20", &updatecheck.Notice{Message: "m", Command: "c"})
	suiteHome = func() (string, error) { return "", errors.New("no home") }
	_, leaf := newSuiteTestTree(t)

	maybeNotifySuiteUpdate(leaf, &bytes.Buffer{})
	if len(*calls) != 0 {
		t.Fatalf("checker ran without a home directory: %+v", *calls)
	}
}

// pathutil.Home rejects only an empty HOME; a relative one would put the
// cache under whatever directory camp ran in.
func TestSuiteNotice_RelativeHomeSkipsTheChecker(t *testing.T) {
	for _, home := range []string{".", "rel"} {
		calls := stubSuiteNotice(t, "v0.3.20", &updatecheck.Notice{Message: "m", Command: "c"})
		suiteHome = func() (string, error) { return home, nil }
		_, leaf := newSuiteTestTree(t)

		maybeNotifySuiteUpdate(leaf, &bytes.Buffer{})
		if len(*calls) != 0 {
			t.Fatalf("HOME=%q reached the checker: %+v", home, *calls)
		}
		if _, ok := suiteCheckOptions(); ok {
			t.Fatalf("HOME=%q produced cache options", home)
		}
	}
}

// The post-run hook is the only entry point, so the lifecycle cases go
// through cobra: a failed command, --help, and a group command printing help
// must never reach the checker.
func TestSuiteNotice_PostRunLifecycle(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		calls int
	}{
		{"successful command", []string{"status"}, 1},
		{"failed command", []string{"broken"}, 0},
		{"help flag", []string{"status", "--help"}, 0},
		{"help command", []string{"help", "status"}, 0},
		{"group command printing help", []string{"group"}, 0},
		{"bare root", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubSuiteNotice(t, "v0.3.20", nil)
			root, _ := newSuiteTestTree(t)
			root.AddCommand(&cobra.Command{Use: "broken", RunE: func(*cobra.Command, []string) error { return errors.New("boom") }})
			group := &cobra.Command{Use: "group", RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
			group.AddCommand(&cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }})
			root.AddCommand(group)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SilenceErrors = true
			root.SilenceUsage = true
			installSuiteUpdateNotice(root)
			root.SetArgs(tc.args)

			_ = root.Execute()

			got := 0
			for _, c := range *calls {
				if c.local != "home-lookup" {
					got++
				}
			}
			if got != tc.calls {
				t.Fatalf("checker calls = %d, want %d", got, tc.calls)
			}
		})
	}
}

// The notice shares a terminal with output a shell may capture, so it must
// land on stderr and leave stdout to the command.
func TestSuiteNotice_AmbientPathNeverWritesStdout(t *testing.T) {
	withSuiteColorProfile(t, termenv.Ascii)
	stubSuiteNotice(t, "v0.3.20", &updatecheck.Notice{Message: "Festival suite update available (0.3.20 → 0.3.24)", Command: "festival update", Kind: "run"})
	root, _ := newSuiteTestTree(t)
	installSuiteUpdateNotice(root)
	root.SetArgs([]string{"status"})

	stdout, stderr := captureStdStreams(t, func() {
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if stdout != "" {
		t.Fatalf("ambient path wrote stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "⚠ notice: Festival suite update available") {
		t.Fatalf("notice missing from stderr: %q", stderr)
	}
}

// captureStdStreams swaps os.Stdout and os.Stderr for pipes while fn runs.
func captureStdStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = prevOut, prevErr }()

	outC, errC := make(chan string), make(chan string)
	go func() { b, _ := io.ReadAll(outR); outC <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errC <- string(b) }()
	fn()
	_ = outW.Close()
	_ = errW.Close()
	return <-outC, <-errC
}

func TestSuiteNoticeSuppressed(t *testing.T) {
	type build func(t *testing.T) *cobra.Command
	leafWith := func(name string, setup func(c *cobra.Command), args ...string) build {
		return func(t *testing.T) *cobra.Command {
			t.Helper()
			root := &cobra.Command{Use: "camp"}
			parent := root
			if strings.Contains(name, " ") {
				parts := strings.SplitN(name, " ", 2)
				parent = &cobra.Command{Use: parts[0]}
				root.AddCommand(parent)
				name = parts[1]
			}
			leaf := &cobra.Command{Use: name}
			setup(leaf)
			parent.AddCommand(leaf)
			if err := leaf.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			return leaf
		}
	}
	none := func(*cobra.Command) {}
	boolFlag := func(name string) func(*cobra.Command) {
		return func(c *cobra.Command) { c.Flags().Bool(name, false, "") }
	}
	strFlag := func(name, def string) func(*cobra.Command) {
		return func(c *cobra.Command) { c.Flags().String(name, def, "") }
	}

	cases := []struct {
		name       string
		cmd        build
		stdoutTTY  bool
		suppressed bool
	}{
		{"plain human command", leafWith("status", none), true, false},
		{"camp go without --print", leafWith("go", boolFlag("print")), true, false},
		{"stdout captured", leafWith("status", none), false, true},
		{"--json", leafWith("list", boolFlag("json"), "--json"), true, true},
		{"--json=false still counts as typed", leafWith("list", boolFlag("json"), "--json=false"), true, true},
		{"--format json", leafWith("list", strFlag("format", "table"), "--format", "json"), true, true},
		{"--format yaml", leafWith("show", strFlag("format", "text"), "--format", "yaml"), true, true},
		{"--format simple", leafWith("list", strFlag("format", "table"), "--format", "simple"), true, true},
		{"--format table", leafWith("list", strFlag("format", "table"), "--format", "table"), true, false},
		{"go --print", leafWith("go", boolFlag("print"), "--print"), true, true},
		{"switch --shell-connect", leafWith("switch", boolFlag("shell-connect"), "--shell-connect"), true, true},
		{"--path-output", leafWith("list", strFlag("path-output", ""), "--path-output", "/tmp/x"), true, true},
		{"quest use --shell", leafWith("quest use", strFlag("shell", ""), "--shell", "posix"), true, true},
		{"--yaml", leafWith("quest show", boolFlag("yaml"), "--yaml"), true, true},
		{"--count", leafWith("list", boolFlag("count"), "--count"), true, true},
		{"--plain", leafWith("notify", boolFlag("plain"), "--plain"), true, true},
		{"version", leafWith("version", boolFlag("check")), true, true},
		{"version --check", leafWith("version", boolFlag("check"), "--check"), true, true},
		{"help", leafWith("help", none), true, true},
		{"gendocs", leafWith("gendocs", none), true, true},
		{"shell-init", leafWith("shell-init", none), true, true},
		{"complete", leafWith("complete", none), true, true},
		{"completion zsh", leafWith("completion zsh", none), true, true},
		{"__complete", leafWith("__complete", none), true, true},
		{"hidden command", leafWith("internal", func(c *cobra.Command) { c.Hidden = true }), true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := suiteHelpRendered
			suiteHelpRendered = false
			t.Cleanup(func() { suiteHelpRendered = prev })

			if got := suiteNoticeSuppressed(tc.cmd(t), tc.stdoutTTY); got != tc.suppressed {
				t.Fatalf("suppressed = %v, want %v", got, tc.suppressed)
			}
		})
	}

	t.Run("child of a hidden command", func(t *testing.T) {
		root := &cobra.Command{Use: "camp"}
		hidden := &cobra.Command{Use: "worker", Hidden: true}
		leaf := &cobra.Command{Use: "run"}
		root.AddCommand(hidden)
		hidden.AddCommand(leaf)
		if !suiteNoticeSuppressed(leaf, true) {
			t.Fatal("a subcommand of a hidden command got the notice")
		}
	})
	t.Run("root itself", func(t *testing.T) {
		if !suiteNoticeSuppressed(&cobra.Command{Use: "camp"}, true) {
			t.Fatal("bare camp (help) got the notice")
		}
	})
}

func TestSuiteAgentRun(t *testing.T) {
	for _, key := range suiteAgentEnv {
		env := map[string]string{key: "1"}
		if !suiteAgentRun(func(k string) string { return env[k] }) {
			t.Errorf("%s did not mark an agent run", key)
		}
	}
	if suiteAgentRun(func(string) string { return "" }) {
		t.Error("an empty environment was classified as an agent run")
	}
}

func withSuiteColorProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// cobra runs only the nearest persistent post-run, so a subcommand that
// defined its own would silently drop the notice for its whole subtree.
func TestSuiteNotice_NoSubcommandShadowsThePostRun(t *testing.T) {
	if rootCmd.PersistentPostRun == nil {
		t.Fatal("root has no persistent post-run")
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.PersistentPostRun != nil || sub.PersistentPostRunE != nil {
				t.Errorf("%s defines its own persistent post-run and hides the suite notice", sub.CommandPath())
			}
			walk(sub)
		}
	}
	walk(rootCmd)
}

// The design names status and go as the priority surfaces; the registered
// commands must stay eligible, and go's shell-integration form must not.
func TestSuiteNotice_PrioritySurfacesAreEligible(t *testing.T) {
	prev := suiteHelpRendered
	suiteHelpRendered = false
	t.Cleanup(func() { suiteHelpRendered = prev })

	for _, path := range [][]string{{"status"}, {"go"}} {
		cmd, _, err := rootCmd.Find(path)
		if err != nil {
			t.Fatal(err)
		}
		if suiteNoticeSuppressed(cmd, true) {
			t.Errorf("camp %s is suppressed", strings.Join(path, " "))
		}
	}

	goCmd, _, err := rootCmd.Find([]string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := goCmd.Flags().Set("print", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = goCmd.Flags().Set("print", "false")
		goCmd.Flags().Lookup("print").Changed = false
	})
	if !suiteNoticeSuppressed(goCmd, true) {
		t.Error("camp go --print is eligible")
	}
}
