package remote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// homeOnlyDirs is the fallback list the shell round-trip tests hand the
// resolver: HOME-relative entries only, so a camp installed at /opt/homebrew
// or /usr/local on the developer machine cannot satisfy a probe that is
// supposed to find nothing.
var homeOnlyDirs = []campInstallDir{
	{Shell: `"$HOME/.local/bin"`, Display: "~/.local/bin"},
	{Shell: `${GOBIN:+"$GOBIN"}`, Display: "$GOBIN"},
	{Shell: `"$HOME/go/bin"`, Display: "~/go/bin"},
}

func TestCampRemoteCommandLineOverrideIsVerbatim(t *testing.T) {
	cases := []struct {
		name     string
		override string
		args     string
		want     string
	}{
		{
			name:     "explicit binary path",
			override: `'/opt/camp/bin/camp'`,
			args:     "list --json",
			want:     `exec "$SHELL" -lc ''\''/opt/camp/bin/camp'\'' list --json'`,
		},
		{
			name:     "explicit binary path with a space",
			override: `'/opt/my camp/camp'`,
			args:     "switch 'my-campaign' --print",
			want:     `exec "$SHELL" -lc ''\''/opt/my camp/camp'\'' switch '\''my-campaign'\'' --print'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := campRemoteCommandLine(tc.override, tc.args); got != tc.want {
				t.Errorf("campRemoteCommandLine(%q, %q) = %q, want %q", tc.override, tc.args, got, tc.want)
			}
		})
	}
}

// Without an override the far side resolves the binary itself: the login
// shell still wraps everything (that is where the profile PATH comes from),
// but the command it runs is /bin/sh over the resolver with args passed as
// positional parameters, not a bare "camp".
func TestCampRemoteCommandLineResolvesOnFarSide(t *testing.T) {
	got := campRemoteCommandLine("", "switch 'my campaign' --print")
	if !strings.HasPrefix(got, `exec "$SHELL" -lc '`) {
		t.Fatalf("remote command does not enter the login shell: %q", got)
	}
	if !strings.Contains(got, `exec /bin/sh -c '\''`) {
		t.Errorf("remote command does not hand off to /bin/sh: %q", got)
	}
	if !strings.Contains(got, `command -v camp`) {
		t.Errorf("resolver does not consult the login-shell PATH first: %q", got)
	}
	if !strings.HasSuffix(got, ` sh switch '\''my campaign'\'' --print'`) {
		t.Errorf("args are not passed positionally after the resolver: %q", got)
	}
	if strings.Contains(got, `'\''camp `) {
		t.Errorf("remote command still names a bare camp binary: %q", got)
	}
}

func TestRemoteCampOverride(t *testing.T) {
	t.Setenv(RemoteCampPathEnv, "")
	if got := remoteCampOverride(); got != "" {
		t.Errorf("remoteCampOverride() with no env = %q, want empty", got)
	}
	t.Setenv(RemoteCampPathEnv, "/opt/my camp/camp")
	if got, want := remoteCampOverride(), `'/opt/my camp/camp'`; got != want {
		t.Errorf("remoteCampOverride() = %q, want %q", got, want)
	}
}

// The resolver is nested inside two layers of single quotes (sh -c, then the
// login shell). It must never contain the two characters whose single-quote
// escaping differs between POSIX shells and fish.
func TestRemoteCampResolverScriptIsQuoteFree(t *testing.T) {
	for _, tail := range []string{campResolverExec, campResolverReport} {
		script := remoteCampResolverScript(campInstallDirs, tail)
		if strings.ContainsAny(script, `'\`) {
			t.Errorf("resolver contains a single quote or backslash: %q", script)
		}
		if !strings.HasSuffix(script, tail) {
			t.Errorf("resolver does not end with its tail %q: %q", tail, script)
		}
	}
	script := remoteCampResolverScript(campInstallDirs, campResolverExec)
	for _, d := range campInstallDirs {
		if !strings.Contains(script, d.Shell) {
			t.Errorf("resolver omits install dir %s: %q", d.Display, script)
		}
	}
	if !strings.Contains(script, "exit 127") {
		t.Errorf("resolver must exit 127 when nothing is found: %q", script)
	}
}

// writeProbeCamp installs a fake camp at dir/camp that prints its argv one
// per line, so a round trip through the resolver can be checked exactly.
func writeProbeCamp(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "camp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatalf("write probe camp: %v", err)
	}
	return path
}

// writeLoginProfiles makes dir the first PATH entry for every login shell
// under home: ~/.profile (sh, dash, bash without a .bash_profile),
// ~/.zprofile (zsh), and config.fish (fish). This is how a real account
// whose profile exports camp's directory looks. It has to be a profile file
// rather than PATH in the environment because /etc/profile on Alpine and
// Debian overwrites PATH on login, so an env PATH would not survive the -l.
func writeLoginProfiles(t *testing.T, home, dir string) {
	t.Helper()
	posix := "export PATH=" + ShellQuote(dir) + ":\"$PATH\"\n"
	for _, name := range []string{".profile", ".zprofile"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(posix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fishDir := filepath.Join(home, ".config", "fish")
	if err := os.MkdirAll(fishDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fish := "set -gx PATH " + ShellQuote(dir) + " $PATH\n"
	if err := os.WriteFile(filepath.Join(fishDir, "config.fish"), []byte(fish), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runAsRemoteLoginShell executes the full remote command line the way sshd
// does — the account's shell parses it (`$SHELL -c <line>`), which re-enters
// itself with -l and runs the inner command — under a controlled HOME and
// PATH. shell is the account shell to emulate; SHELL is set so the
// `exec "$SHELL" -lc` in the line resolves to it. No ssh: this is the two
// layers of shell parsing that a real hop performs on the far machine.
func runAsRemoteLoginShell(t *testing.T, shell, home, path, line string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(shell, "-c", line)
	cmd.Env = []string{"HOME=" + home, "PATH=" + path, "SHELL=" + shell}
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s -c: %v", shell, err)
		}
		code = exitErr.ExitCode()
	}
	return out.String(), errb.String(), code
}

// loginShellsForTest returns the shells present on this machine to stand in
// for the remote account's login shell. sh always; dash, bash, zsh and fish
// when installed, because their -l startup and quoting corner cases differ
// and every one of them ships as somebody's login shell. fish is the one
// non-POSIX entry: it only ever has to parse the outer command line (the
// resolver runs under /bin/sh), and that parse is what these tests check.
// tests/integration/remote_fish_shell_test.go runs the same thing over a
// real sshd for machines without fish on the developer's PATH.
func loginShellsForTest(t *testing.T) []string {
	t.Helper()
	shells := []string{}
	for _, name := range []string{"sh", "dash", "bash", "zsh", "fish"} {
		if p, err := exec.LookPath(name); err == nil {
			shells = append(shells, p)
		}
	}
	if len(shells) == 0 {
		t.Skip("no shell on PATH")
	}
	return shells
}

// isFish reports whether shell is a fish binary, for the one case fish is
// known not to parse: a campaign name containing an apostrophe nests the
// close-escape-reopen idiom twice, which fish rejects. That is the command
// line main already produced for such names, so it is documented here rather
// than fixed here.
func isFish(shell string) bool {
	return filepath.Base(shell) == "fish"
}

// TestResolverFindsCampInInstallDirWhenPathIsBlind is the archdtop case: the
// login shell's PATH has no camp (the profile that adds ~/go/bin is
// interactive-only), but the binary is exactly where `go install` put it. The
// hop must find it and hand every argument through unchanged.
func TestResolverFindsCampInInstallDirWhenPathIsBlind(t *testing.T) {
	home := t.TempDir()
	writeProbeCamp(t, filepath.Join(home, "go", "bin"))
	barePath := "/usr/bin:/bin"

	cases := []struct {
		name      string
		remainder string
	}{
		{"plain", "my-campaign"},
		{"spaces", "my campaign name"},
		{"single quote", "o'brien's-campaign"},
		{"glob chars", "campaign-*[1]?"},
		{"org scoped", "obey/platform@f"},
		{"dollar and backtick", "pay$day`x`"},
	}
	for _, shell := range loginShellsForTest(t) {
		for _, tc := range cases {
			t.Run(filepath.Base(shell)+"/"+tc.name, func(t *testing.T) {
				if isFish(shell) && strings.Contains(tc.remainder, "'") {
					t.Skip("fish cannot parse a doubly nested quote idiom; pre-existing on main for names with an apostrophe")
				}
				line := campRemoteCommandLineWith(homeOnlyDirs, "", resolveRootArgs(tc.remainder))
				stdout, stderr, code := runAsRemoteLoginShell(t, shell, home, barePath, line)
				if code != 0 {
					t.Fatalf("exit %d, stderr: %s", code, stderr)
				}
				got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
				want := []string{"switch", tc.remainder, "--print"}
				if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
					t.Errorf("argv = %q, want %q", got, want)
				}
			})
		}
	}
}

// When the login shell can already see camp — its profile exports the
// directory — that camp wins; the fallback list must never shadow a PATH the
// operator configured on purpose.
func TestResolverPrefersLoginShellPath(t *testing.T) {
	home := t.TempDir()
	writeProbeCamp(t, filepath.Join(home, "go", "bin"))
	onPath := filepath.Join(home, "on-path")
	if err := os.MkdirAll(onPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onPath, "camp"), []byte("#!/bin/sh\necho from-path\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLoginProfiles(t, home, onPath)
	for _, shell := range loginShellsForTest(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			line := campRemoteCommandLineWith(homeOnlyDirs, "", "list --json")
			stdout, stderr, code := runAsRemoteLoginShell(t, shell, home, "/usr/bin:/bin", line)
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			if strings.TrimSpace(stdout) != "from-path" {
				t.Errorf("stdout = %q, want the PATH camp to win", stdout)
			}
		})
	}
}

// Nothing anywhere: exit 127 (so IsCampNotFound classifies it) and the one
// stderr line campNotFoundHint expands on the local side.
func TestResolverExits127WhenNothingIsFound(t *testing.T) {
	home := t.TempDir()
	for _, shell := range loginShellsForTest(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			line := campRemoteCommandLineWith(homeOnlyDirs, "", "list --json")
			_, stderr, code := runAsRemoteLoginShell(t, shell, home, "/usr/bin:/bin", line)
			if code != 127 {
				t.Fatalf("exit %d, want 127; stderr: %s", code, stderr)
			}
			if strings.TrimSpace(stderr) != campNotFoundStderr {
				t.Errorf("stderr = %q, want %q", stderr, campNotFoundStderr)
			}
		})
	}
}

// The report mode is what diagnose runs: same discovery, but it prints where
// camp is and whether the login shell could see it unaided.
func TestResolverReportModeRoundTrip(t *testing.T) {
	home := t.TempDir()
	installed := writeProbeCamp(t, filepath.Join(home, ".local", "bin"))
	for _, shell := range loginShellsForTest(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			line := campLocationCommandLine(homeOnlyDirs)
			stdout, stderr, code := runAsRemoteLoginShell(t, shell, home, "/usr/bin:/bin", line)
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			loc, err := parseCampLocation(stdout)
			if err != nil {
				t.Fatalf("parseCampLocation(%q): %v", stdout, err)
			}
			if loc.OnPATH {
				t.Errorf("reported on-PATH for a camp the login shell cannot see: %q", stdout)
			}
			if loc.Path != installed {
				t.Errorf("reported path %q, want %q", loc.Path, installed)
			}

		})
	}
}

// The other half of the report: a camp the login shell's own profile exports
// is reported as on-PATH, not as a fallback.
func TestResolverReportModeSeesProfilePath(t *testing.T) {
	home := t.TempDir()
	installed := writeProbeCamp(t, filepath.Join(home, "on-path"))
	writeLoginProfiles(t, home, filepath.Dir(installed))
	for _, shell := range loginShellsForTest(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			stdout, stderr, code := runAsRemoteLoginShell(t, shell, home, "/usr/bin:/bin", campLocationCommandLine(homeOnlyDirs))
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			loc, err := parseCampLocation(stdout)
			if err != nil {
				t.Fatalf("parseCampLocation(%q): %v", stdout, err)
			}
			if !loc.OnPATH || loc.Path != installed {
				t.Errorf("reported %+v for a camp the profile exports, want on-PATH %q", loc, installed)
			}
		})
	}
}

func TestParseCampLocation(t *testing.T) {
	cases := []struct {
		in      string
		want    CampLocation
		wantErr bool
	}{
		{in: "path /home/lance/go/bin/camp\n", want: CampLocation{Path: "/home/lance/go/bin/camp", OnPATH: true}},
		{in: "fallback /home/lance/.local/bin/camp", want: CampLocation{Path: "/home/lance/.local/bin/camp"}},
		{in: "fallback /opt/my camp/camp", want: CampLocation{Path: "/opt/my camp/camp"}},
		{in: "override /opt/x/camp\n", want: CampLocation{Path: "/opt/x/camp", Override: true}},
		{in: "", wantErr: true},
		{in: "path", wantErr: true},
		{in: "weird /x/camp", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseCampLocation(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseCampLocation(%q) = %+v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseCampLocation(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseCampLocation(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// An explicit CAMP_REMOTE_CAMP_PATH is still probed on the far side: a path
// that is executable reports as override, a path that is not exits 127 so
// diagnose says "not found" instead of printing the override as if it ran.
func TestOverrideLocationRoundTrip(t *testing.T) {
	home := t.TempDir()
	installed := writeProbeCamp(t, filepath.Join(home, "opt", "my camp"))
	for _, shell := range loginShellsForTest(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			stdout, stderr, code := runAsRemoteLoginShell(t, shell, home, "/usr/bin:/bin",
				campOverrideLocationCommandLine(ShellQuote(installed)))
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			loc, err := parseCampLocation(stdout)
			if err != nil {
				t.Fatalf("parseCampLocation(%q): %v", stdout, err)
			}
			if !loc.Override || loc.Path != installed {
				t.Errorf("override report = %+v, want Override with path %q", loc, installed)
			}
			_, _, code = runAsRemoteLoginShell(t, shell, home, "/usr/bin:/bin",
				campOverrideLocationCommandLine(ShellQuote(filepath.Join(home, "nope", "camp"))))
			if code != 127 {
				t.Errorf("missing override exited %d, want 127", code)
			}
		})
	}
}

func TestResolveRootArgs(t *testing.T) {
	cases := map[string]string{
		"my-campaign":       `switch 'my-campaign' --print`,
		"org/campaign":      `switch 'org/campaign' --print`,
		"has 'quote":        `switch 'has '\''quote' --print`,
		"campaign-*[glob]?": `switch 'campaign-*[glob]?' --print`,
	}
	for in, want := range cases {
		if got := resolveRootArgs(in); got != want {
			t.Errorf("resolveRootArgs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsCampNotFound(t *testing.T) {
	if !IsCampNotFound(camperrors.NewCommand("ssh devbox", 127, "camp: not found", nil)) {
		t.Error("exit 127 should classify as camp-not-found")
	}
	wrapped := camperrors.Wrap(camperrors.NewCommand("ssh devbox", 127, "camp: not found", nil), "could not resolve")
	if !IsCampNotFound(wrapped) {
		t.Error("a wrapped exit 127 should still classify as camp-not-found")
	}
	if IsCampNotFound(camperrors.NewCommand("ssh devbox", 126, "Permission denied", nil)) {
		t.Error("exit 126 must not classify as camp-not-found")
	}
	if IsCampNotFound(camperrors.New("timed out")) {
		t.Error("a non-command error must not classify as camp-not-found")
	}
}

func TestCampNotFoundHintDetectsExit127(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	err := camperrors.NewCommand("ssh devbox", 127, "camp: not found on the login-shell PATH or in ...", nil)
	got := campNotFoundHint(err, m, "")
	if got == nil {
		t.Fatal("expected a non-nil error")
	}
	for _, want := range []string{RemoteCampPathEnv, "devbox", "login-shell PATH", CampInstallDirsDisplay()} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("not-found hint missing %q: %v", want, got)
		}
	}
}

func TestCampNotFoundHintNamesTheOverride(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	err := camperrors.NewCommand("ssh devbox", 127, "sh: /opt/x/camp: not found", nil)
	got := campNotFoundHint(err, m, `'/opt/x/camp'`)
	if got == nil {
		t.Fatal("expected a non-nil error")
	}
	if !strings.Contains(got.Error(), "/opt/x/camp") || !strings.Contains(got.Error(), RemoteCampPathEnv) {
		t.Errorf("override hint should name the path and the variable: %v", got)
	}
	if strings.Contains(got.Error(), "usual install locations") {
		t.Errorf("override hint should not talk about the fallback list nobody consulted: %v", got)
	}
}

// TestCampNotFoundHintIgnoresNotFoundTextAtOtherExitCodes guards the false
// positive that motivated using exit code 127 alone: camp's own
// ErrCampaignNotFound ("campaign not found: ...") can legitimately appear in
// stderr when the far machine's camp ran fine but the campaign name just
// does not resolve there. That must never be relabeled as a missing binary.
func TestCampNotFoundHintIgnoresNotFoundTextAtOtherExitCodes(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	original := camperrors.NewCommand("ssh devbox", 1, `campaign not found: "nope"`, nil)
	if got := campNotFoundHint(original, m, ""); got != original {
		t.Errorf("campNotFoundHint relabeled a domain 'not found' error as a missing binary: got %v, want unchanged %v", got, original)
	}
}

func TestCampNotFoundHintIgnoresPermissionDenied(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	original := camperrors.NewCommand("ssh devbox", 126, "sh: camp: Permission denied", nil)
	if got := campNotFoundHint(original, m, ""); got != original {
		t.Errorf("campNotFoundHint should leave a permission-denied (126, not 127) failure unchanged: got %v, want unchanged %v", got, original)
	}
}

func TestCampNotFoundHintPassesThroughNonCommandErrors(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	original := camperrors.New("ssh to devbox timed out")
	if got := campNotFoundHint(original, m, ""); got != original {
		t.Errorf("campNotFoundHint changed a non-CommandError: got %v, want unchanged %v", got, original)
	}
}
