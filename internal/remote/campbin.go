package remote

import (
	"context"
	"errors"
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// RemoteCampPathEnv, when set, is the exact camp invocation used on the far
// machine in place of resolving one there. It is the escape hatch for a
// machine where camp is installed somewhere neither the login-shell PATH nor
// campInstallDirs covers, and is documented on `camp switch --help` and
// `camp list --help`.
const RemoteCampPathEnv = "CAMP_REMOTE_CAMP_PATH"

// campInstallDir is one directory camp's own distribution channels install
// into. Shell is the token the far-side resolver tests (expanded by /bin/sh
// over there, so it may reference the remote's HOME/GOBIN/GOPATH); Display is
// how the same location reads in an error message.
type campInstallDir struct {
	Shell   string
	Display string
}

// campInstallDirs is the ordered fallback the far-side resolver walks when the
// login-shell PATH has no camp. Every entry is a place one of camp's own
// installers puts the binary, not a guess:
//
//   - ~/.local/bin: festival/install.sh's INSTALL_DIR default. That installer
//     appends its PATH line to ~/.zshrc / ~/.bashrc, which only interactive
//     shells read, so on Linux a stock installer setup is invisible to a
//     login shell.
//   - $GOBIN, $GOPATH/bin, ~/go/bin: `go install` and `just install`, in the
//     order go itself resolves the destination.
//   - Homebrew prefixes and /usr/local/bin.
//
// The login-shell PATH is always consulted first, so a machine whose profile
// exports one of these directories behaves exactly as before; this list only
// matters when that lookup fails.
var campInstallDirs = []campInstallDir{
	{Shell: `"$HOME/.local/bin"`, Display: "~/.local/bin"},
	{Shell: `${GOBIN:+"$GOBIN"}`, Display: "$GOBIN"},
	{Shell: `${GOPATH:+"$GOPATH/bin"}`, Display: "$GOPATH/bin"},
	{Shell: `"$HOME/go/bin"`, Display: "~/go/bin"},
	{Shell: `/opt/homebrew/bin`, Display: "/opt/homebrew/bin"},
	{Shell: `/home/linuxbrew/.linuxbrew/bin`, Display: "/home/linuxbrew/.linuxbrew/bin"},
	{Shell: `/usr/local/bin`, Display: "/usr/local/bin"},
}

// CampInstallDirsDisplay lists campInstallDirs the way an error message or
// diagnose line names them.
func CampInstallDirsDisplay() string {
	names := make([]string, 0, len(campInstallDirs))
	for _, d := range campInstallDirs {
		names = append(names, d.Display)
	}
	return strings.Join(names, ", ")
}

// remoteCampOverride returns CAMP_REMOTE_CAMP_PATH shell-quoted when set, else
// "" meaning "resolve camp on the far side".
func remoteCampOverride() string {
	if p := os.Getenv(RemoteCampPathEnv); p != "" {
		return ShellQuote(p)
	}
	return ""
}

// Resolver outcomes, printed by the far-side resolver's print mode and parsed
// back by RemoteCampLocation.
const (
	campFoundOnPath     = "path"
	campFoundInInstall  = "fallback"
	campResolverExec    = `exec "$c" "$@"`
	campResolverReport  = `echo "$how $c"`
	campResolverNoQuote = "'"
	// campNotFoundStderr is what the far side prints before exiting 127.
	campNotFoundStderr = "camp: not found on the login-shell PATH or in any of the usual install locations"
)

// remoteCampResolverScript is the /bin/sh script that finds camp on the far
// machine and then runs tail with $c (the binary) and $how (path|fallback)
// set. Discovery order: the login-shell PATH first (`command -v camp`, which
// is what the login shell would itself have executed), then campInstallDirs.
// When nothing is found it exits 127 — the POSIX "command not found" code the
// rest of camp already keys on — after naming every place it looked, so the
// operator's next step is never a PATH hunt.
//
// The script is deliberately free of single quotes and backslashes: it is
// single-quoted once for /bin/sh -c and then a second time by
// LoginShellCommand for the account's login shell, and that outer shell may
// be fish, whose single-quote escaping differs from POSIX exactly for those
// two characters. Keeping them out means both shells read the same bytes.
//
// dirs is campInstallDirs in production; tests pass a HOME-relative subset so
// a camp installed at an absolute prefix on the developer machine cannot leak
// into the "nothing found" case.
func remoteCampResolverScript(dirs []campInstallDir, tail string) string {
	shell := make([]string, 0, len(dirs))
	for _, d := range dirs {
		shell = append(shell, d.Shell)
	}
	// The stderr line stays generic on purpose: the dir names include $GOBIN
	// and $GOPATH, which /bin/sh would expand (to nothing, usually) inside the
	// message. campNotFoundHint enumerates them verbatim on the local side.
	script := `c=$(command -v camp 2>/dev/null); how=` + campFoundOnPath + `; ` +
		`if [ -z "$c" ]; then for d in ` + strings.Join(shell, " ") + `; do ` +
		`if [ -x "$d/camp" ]; then c="$d/camp"; how=` + campFoundInInstall + `; break; fi; done; fi; ` +
		`if [ -z "$c" ]; then echo "` + campNotFoundStderr + `" >&2; exit 127; fi; ` +
		tail
	if strings.ContainsAny(script, campResolverNoQuote+`\`) {
		panic("remote camp resolver must not contain single quotes or backslashes")
	}
	return script
}

// campRemoteCommandLine builds the single token handed to ssh as the remote
// command for a camp invocation. args is everything after the binary name,
// already shell-quoted where needed (e.g. `switch 'my campaign' --print`).
//
// With CAMP_REMOTE_CAMP_PATH set the line is exactly `<path> <args>` in the
// login shell — the operator asked for that path and gets it verbatim.
// Otherwise the login shell (which supplies the profile PATH) hands off to
// /bin/sh running remoteCampResolverScript, with args passed as positional
// parameters rather than spliced into the script. That keeps the script
// quote-free (see remoteCampResolverScript) and lets the login shell do the
// one job it is there for: word-splitting args exactly as it always has.
func campRemoteCommandLine(override, args string) string {
	return campRemoteCommandLineWith(campInstallDirs, override, args)
}

// campRemoteCommandLineWith is campRemoteCommandLine with an explicit
// fallback list, the seam the shell round-trip tests use.
func campRemoteCommandLineWith(dirs []campInstallDir, override, args string) string {
	if override != "" {
		return LoginShellCommand(override + " " + args)
	}
	inner := "exec /bin/sh -c " + ShellQuote(remoteCampResolverScript(dirs, campResolverExec)) + " sh " + args
	return LoginShellCommand(inner)
}

// RunCampCommand execs the remote machine's OWN camp binary over ssh, through
// the account's configured login shell (`$SHELL -lc`), and returns stdout.
// args is everything after the binary name, e.g. `switch 'foo' --print` or
// `list --json`.
//
// A bare non-interactive `ssh host 'camp ...'` runs under ssh's own
// non-login shell, which never sources a login profile (~/.profile,
// ~/.bash_profile, etc.) — so a camp installed via a PATH addition that only
// a login shell picks up was invisible to it. Re-entering `$SHELL` in login
// mode sources the profile for the shell the account actually uses. Because
// both of camp's own install paths (`go install` → ~/go/bin, and the festival
// installer → ~/.local/bin with its PATH line in ~/.zshrc) still leave a
// Linux login shell blind to the binary, the far side then falls back to
// campInstallDirs before giving up (remoteCampResolverScript).
func RunCampCommand(ctx context.Context, m *machines.Machine, args string) ([]byte, error) {
	return runCampCommand(ctx, m, args, false)
}

// RunCampCommandReuseOnly is RunCampCommand for callers that also report m's
// ControlMaster state (`camp machine diagnose`, the machine screen). It hops
// with OptsReuseOnly so the probe cannot create or replace the very socket
// being reported alongside it.
func RunCampCommandReuseOnly(ctx context.Context, m *machines.Machine, args string) ([]byte, error) {
	return runCampCommand(ctx, m, args, true)
}

// runCampCommand resolves the dial endpoint itself (rather than taking
// pre-built opts) so every remote camp invocation — version probe, ResolveRoot,
// the machine screen — survives a MagicDNS outage without its callers knowing
// the fallback exists. The per-process peer-table memo makes the repeated
// resolution effectively free.
func runCampCommand(ctx context.Context, m *machines.Machine, args string, reuseOnly bool) ([]byte, error) {
	if err := EnsureKeyAuth(m); err != nil {
		return nil, err
	}
	e := ResolveEndpoint(ctx, m)
	opts := e.Opts()
	if reuseOnly {
		opts = e.OptsReuseOnly()
	}
	override := remoteCampOverride()
	out, err := Run(ctx, e.Target(), opts, campRemoteCommandLine(override, args))
	if err != nil {
		return nil, campNotFoundHint(err, m, override)
	}
	return out, nil
}

// IsCampNotFound reports whether err is a remote invocation that exited 127 —
// the POSIX convention every common shell (sh, bash, dash, ash/busybox, zsh)
// uses exclusively for "command not found", and the code the far-side
// resolver exits with after exhausting campInstallDirs. Surfaces that would
// otherwise say "unreachable" use this to say what actually happened: ssh
// worked, camp is not there.
func IsCampNotFound(err error) bool {
	var cmdErr *camperrors.CommandError
	return errors.As(err, &cmdErr) && cmdErr.ExitCode == 127
}

// campNotFoundHint wraps a 127 with what was tried and the escape hatch. Only
// exit code 127 qualifies — that narrow, explicit signal (rather than matching
// stderr text) matters because camp's own domain errors can legitimately
// contain the words "not found" too (e.g. ErrCampaignNotFound, when the
// campaign name just does not resolve on a far machine whose camp binary ran
// fine), and mislabeling those as a missing binary would send the user
// chasing PATH for nothing. Any other exit code is returned unchanged.
func campNotFoundHint(err error, m *machines.Machine, override string) error {
	if !IsCampNotFound(err) {
		return err
	}
	if override != "" {
		return camperrors.Wrapf(err,
			"remote camp not found on %s at %s (from %s); check that path on that machine",
			m.ID, override, RemoteCampPathEnv)
	}
	return camperrors.Wrapf(err,
		"remote camp not found on %s: nothing named camp on the account's login-shell PATH, and none of camp's usual install locations (%s) has one; "+
			"if it lives elsewhere, set %s to its exact path on that machine",
		m.ID, CampInstallDirsDisplay(), RemoteCampPathEnv)
}

// CampLocation is where a machine's camp binary was found and how.
type CampLocation struct {
	// Path is the resolved binary on the far machine (meaningful only there).
	Path string `json:"path"`
	// OnPATH is true when the account's login shell finds it unaided. False
	// means the far-side resolver had to fall back to campInstallDirs — a
	// working hop, but one worth knowing about when the operator sees a
	// different camp interactively than camp does non-interactively.
	OnPATH bool `json:"on_path"`
	// Override is true when Path is CAMP_REMOTE_CAMP_PATH rather than a
	// far-side discovery.
	Override bool `json:"override,omitempty"`
}

// RemoteCampLocation asks the far machine where its camp binary is, using
// the same resolver a real invocation runs, so what diagnose reports is what
// a hop would execute. Reuse-only: it exists for diagnose, which must not
// disturb the socket it is reporting. Returns a 127-classified error (see
// IsCampNotFound) when nothing was found; any other error is the hop itself
// failing.
func RemoteCampLocation(ctx context.Context, m *machines.Machine) (CampLocation, error) {
	if override := remoteCampOverride(); override != "" {
		return CampLocation{Path: os.Getenv(RemoteCampPathEnv), Override: true}, nil
	}
	if err := EnsureKeyAuth(m); err != nil {
		return CampLocation{}, err
	}
	e := ResolveEndpoint(ctx, m)
	out, err := Run(ctx, e.Target(), e.OptsReuseOnly(), campLocationCommandLine(campInstallDirs))
	if err != nil {
		return CampLocation{}, campNotFoundHint(err, m, "")
	}
	return parseCampLocation(string(out))
}

// campLocationCommandLine is the ssh remote command for the resolver's report
// mode: same discovery as a real invocation, but it prints "<how> <path>"
// instead of exec-ing camp.
func campLocationCommandLine(dirs []campInstallDir) string {
	inner := "exec /bin/sh -c " + ShellQuote(remoteCampResolverScript(dirs, campResolverReport)) + " sh"
	return LoginShellCommand(inner)
}

// parseCampLocation reads the resolver's report line: "<how> <path>".
func parseCampLocation(out string) (CampLocation, error) {
	how, path, ok := strings.Cut(strings.TrimSpace(out), " ")
	if !ok || path == "" {
		return CampLocation{}, camperrors.New("remote camp resolver returned an unexpected report: " + strings.TrimSpace(out))
	}
	switch how {
	case campFoundOnPath:
		return CampLocation{Path: path, OnPATH: true}, nil
	case campFoundInInstall:
		return CampLocation{Path: path}, nil
	default:
		return CampLocation{}, camperrors.New("remote camp resolver returned an unexpected report: " + strings.TrimSpace(out))
	}
}

// resolveRootArgs builds the `switch` args RunCampCommand appends after the
// resolved camp binary. remainder is single-quoted for injection safety.
// Split out from ResolveRoot so it is unit-testable without ssh.
func resolveRootArgs(remainder string) string {
	return "switch " + ShellQuote(remainder) + " --print"
}

// ResolveRoot runs the remote's OWN `camp switch <remainder> --print` so the
// remote registry, org config, and fuzzy matching decide the campaign root —
// never the local filesystem. The remainder is single-quoted for injection
// safety. The returned path is meaningful only on the far machine.
func ResolveRoot(ctx context.Context, m *machines.Machine, remainder string) (string, error) {
	out, err := RunCampCommand(ctx, m, resolveRootArgs(remainder))
	if err != nil {
		return "", camperrors.Wrapf(err, "could not resolve %q on %s", remainder, m.ID)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", camperrors.New("could not resolve " + remainder + " on " + m.ID + ": remote returned no path")
	}
	return root, nil
}
