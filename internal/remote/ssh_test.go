package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestTarget(t *testing.T) {
	if got := Target(&machines.Machine{Host: "devbox.ts.net"}); got != "devbox.ts.net" {
		t.Errorf("Target without user = %q, want host", got)
	}
	if got := Target(&machines.Machine{Host: "devbox.ts.net", SSHUser: "lance"}); got != "lance@devbox.ts.net" {
		t.Errorf("Target with user = %q, want user@host", got)
	}
}

func TestOptsAuthArgs(t *testing.T) {
	agent := Opts(&machines.Machine{AuthMethod: machines.AuthSSHAgent})
	if !slices.Contains(agent, "BatchMode=yes") {
		t.Errorf("agent opts missing BatchMode=yes: %v", agent)
	}
	if !slices.Contains(agent, "StrictHostKeyChecking=accept-new") {
		t.Errorf("opts missing StrictHostKeyChecking=accept-new: %v", agent)
	}
	if slices.Contains(agent, "-i") {
		t.Errorf("agent opts should not carry -i without an identity file: %v", agent)
	}

	withKey := Opts(&machines.Machine{AuthMethod: machines.AuthSSHAgent, IdentityFile: "/home/lance/.ssh/id_ed25519"})
	if !slices.Contains(withKey, "IdentitiesOnly=yes") || !slices.Contains(withKey, "/home/lance/.ssh/id_ed25519") {
		t.Errorf("identity-file opts missing IdentitiesOnly/-i: %v", withKey)
	}
}

func TestAuthArgsExpandsIdentityTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	withKey := Opts(&machines.Machine{AuthMethod: machines.AuthSSHAgent, IdentityFile: "~/.ssh/id_ed25519"})
	want := filepath.Join(home, ".ssh", "id_ed25519")
	if !slices.Contains(withKey, want) {
		t.Errorf("identity ~ not expanded to %q: %v", want, withKey)
	}
	if slices.ContainsFunc(withKey, func(s string) bool { return strings.HasPrefix(s, "~") }) {
		t.Errorf("opts still carry an unexpanded tilde: %v", withKey)
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := map[string]string{
		"~":                  home,
		"~/.ssh/id":          filepath.Join(home, ".ssh", "id"),
		"/abs/path/id":       "/abs/path/id",       // absolute untouched
		"relative/id":        "relative/id",        // relative untouched
		"~otheruser/.ssh/id": "~otheruser/.ssh/id", // other-user tilde left for ssh
		"/has/~/mid/path":    "/has/~/mid/path",    // mid-path tilde untouched
	}
	for in, want := range cases {
		if got := expandTilde(in); got != want {
			t.Errorf("expandTilde(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestControlSocketPathNoSideEffect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := ControlSocketPath(&machines.Machine{ID: "devbox"})
	want := filepath.Join(home, ".obey", "ssh-ctl", "devbox.sock")
	if got != want {
		t.Errorf("ControlSocketPath = %q, want %q", got, want)
	}
	// Purely computing the path must not create the ssh-ctl directory.
	if _, err := os.Stat(filepath.Join(home, ".obey", "ssh-ctl")); !os.IsNotExist(err) {
		t.Errorf("ControlSocketPath created the socket dir (stat err = %v); it must be side-effect free", err)
	}
}

func TestCheckControlMasterNoSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := CheckControlMaster(context.Background(), &machines.Machine{ID: "devbox", Host: "devbox.ts.net", AuthMethod: machines.AuthSSHAgent})
	if d.State != ControlNone {
		t.Errorf("no socket file should be ControlNone, got %q", d.State)
	}
	if d.MachineID != "devbox" {
		t.Errorf("diagnosis machine id = %q, want devbox", d.MachineID)
	}
}

func TestResetControlMasterNoSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No socket file present: reset is a no-op and must not error or shell out.
	if err := ResetControlMaster(context.Background(), &machines.Machine{ID: "devbox", Host: "devbox.ts.net", AuthMethod: machines.AuthSSHAgent}); err != nil {
		t.Errorf("reset with no socket should be a no-op, got %v", err)
	}
}

func TestOptsControlMaster(t *testing.T) {
	m := &machines.Machine{ID: "devbox", Host: "devbox.ts.net", AuthMethod: machines.AuthSSHAgent}
	opts := Opts(m)
	for _, want := range []string{"ControlMaster=auto", "ControlPersist=30s"} {
		if !slices.Contains(opts, want) {
			t.Errorf("Opts missing %q: %v", want, opts)
		}
	}
	// The ControlPath is per-machine and shared between ResolveRoot and the hop
	// (both build opts from Opts), so multiplexing reuses one connection.
	var ctlPath string
	for _, o := range opts {
		if after, ok := strings.CutPrefix(o, "ControlPath="); ok {
			ctlPath = after
		}
	}
	if !strings.HasSuffix(ctlPath, "/ssh-ctl/devbox.sock") {
		t.Errorf("ControlPath not the per-machine socket: %q", ctlPath)
	}
}

func TestEnsureKeyAuth(t *testing.T) {
	if err := EnsureKeyAuth(&machines.Machine{ID: "dev", AuthMethod: machines.AuthSSHAgent}); err != nil {
		t.Errorf("agent auth rejected: %v", err)
	}
	if err := EnsureKeyAuth(&machines.Machine{ID: "dev", AuthMethod: machines.AuthTailscaleSSH}); err != nil {
		t.Errorf("tailscale-ssh rejected: %v", err)
	}
	err := EnsureKeyAuth(&machines.Machine{ID: "dev", AuthMethod: machines.AuthSSHPassword})
	if err == nil {
		t.Fatal("password auth accepted, want a clear error")
	}
	if !strings.Contains(err.Error(), "password auth") {
		t.Errorf("password error not actionable: %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"campaign":       `'campaign'`,
		"org/campaign@f": `'org/campaign@f'`,
		"a'b":            `'a'\''b'`,
		"$(rm -rf /)":    `'$(rm -rf /)'`,
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCampRemoteCommandLineWrapsLoginShell(t *testing.T) {
	cases := []struct {
		name   string
		binary string
		args   string
		want   string
	}{
		{
			name:   "simple switch",
			binary: "camp",
			args:   "switch 'my-campaign' --print",
			want:   `sh -lc 'camp switch '\''my-campaign'\'' --print'`,
		},
		{
			name:   "list json",
			binary: "camp",
			args:   "list --json",
			want:   `sh -lc 'camp list --json'`,
		},
		{
			name:   "explicit binary path with a space",
			binary: `'/opt/my camp/camp'`,
			args:   "list --json",
			want:   `sh -lc ''\''/opt/my camp/camp'\'' list --json'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := campRemoteCommandLine(tc.binary, tc.args); got != tc.want {
				t.Errorf("campRemoteCommandLine(%q, %q) = %q, want %q", tc.binary, tc.args, got, tc.want)
			}
		})
	}
}

// TestInnerCommandRoundTripsThroughPosixShell proves the quoting
// campRemoteCommandLine relies on actually survives being parsed by a real
// POSIX shell, the way the remote's login shell (`sh -lc '<inner>'`) parses
// <inner> a second time on the far machine: it hands `binary+" "+args`
// (the exact string ShellQuote wraps as <inner>) to a plain, non-login
// `sh -c`, using an absolute-path probe script as the binary so the test
// depends on neither PATH nor any login-shell profile file — only on the
// shell's ordinary command-line parsing, which is what both the remote's
// non-login default shell and its login shell use for this. This is
// local-only shell parsing, not a live ssh connection.
func TestInnerCommandRoundTripsThroughPosixShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	dir := t.TempDir()
	probePath := filepath.Join(dir, "probe.sh")
	if err := os.WriteFile(probePath, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	cases := []struct {
		name      string
		remainder string
	}{
		{"plain", "my-campaign"},
		{"spaces", "my campaign name"},
		{"single quote", "o'brien's-campaign"},
		{"glob chars", "campaign-*[1]?"},
		{"org scoped", "obey/platform@f"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := resolveRootArgs(tc.remainder)
			inner := ShellQuote(probePath) + " " + args

			out, err := exec.Command("sh", "-c", inner).CombinedOutput()
			if err != nil {
				t.Fatalf("round-trip parse failed: %v\noutput: %s", err, out)
			}
			got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
			want := []string{"switch", tc.remainder, "--print"}
			if len(got) != len(want) {
				t.Fatalf("round-trip argv = %q, want %q", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("round-trip argv[%d] = %q, want %q", i, got[i], want[i])
				}
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

func TestRemoteCampBinaryDefault(t *testing.T) {
	t.Setenv(RemoteCampPathEnv, "")
	if got := remoteCampBinary(); got != "camp" {
		t.Errorf("remoteCampBinary() with no override = %q, want %q", got, "camp")
	}
}

func TestRemoteCampBinaryOverride(t *testing.T) {
	t.Setenv(RemoteCampPathEnv, "/opt/camp/bin/camp")
	want := `'/opt/camp/bin/camp'`
	if got := remoteCampBinary(); got != want {
		t.Errorf("remoteCampBinary() with override = %q, want %q", got, want)
	}
}

func TestRemoteCampBinaryOverrideWithSpace(t *testing.T) {
	t.Setenv(RemoteCampPathEnv, "/opt/my camp/camp")
	want := `'/opt/my camp/camp'`
	if got := remoteCampBinary(); got != want {
		t.Errorf("remoteCampBinary() with spaced override = %q, want %q", got, want)
	}
}

func TestCampNotFoundHintDetectsExit127(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	err := camperrors.NewCommand("ssh devbox", 127, "sh: line 1: camp: command not found", nil)
	got := campNotFoundHint(err, m, "camp")
	if got == nil {
		t.Fatal("expected a non-nil error")
	}
	if !strings.Contains(got.Error(), RemoteCampPathEnv) {
		t.Errorf("not-found hint missing %s mention: %v", RemoteCampPathEnv, got)
	}
	if !strings.Contains(got.Error(), "devbox") {
		t.Errorf("not-found hint missing machine id: %v", got)
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
	got := campNotFoundHint(original, m, "camp")
	if got != original {
		t.Errorf("campNotFoundHint relabeled a domain 'not found' error as a missing binary: got %v, want unchanged %v", got, original)
	}
}

func TestCampNotFoundHintIgnoresPermissionDenied(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	original := camperrors.NewCommand("ssh devbox", 126, "sh: camp: Permission denied", nil)
	got := campNotFoundHint(original, m, "camp")
	if got != original {
		t.Errorf("campNotFoundHint should leave a permission-denied (126, not 127) failure unchanged: got %v, want unchanged %v", got, original)
	}
}

func TestCampNotFoundHintLeavesUnrelatedErrorsUnchanged(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	original := camperrors.NewCommand("ssh devbox", 1, "campaign \"nope\" not registered", nil)
	got := campNotFoundHint(original, m, "camp")
	if got != original {
		t.Errorf("campNotFoundHint changed an unrelated command failure: got %v, want unchanged %v", got, original)
	}
}

func TestCampNotFoundHintPassesThroughNonCommandErrors(t *testing.T) {
	m := &machines.Machine{ID: "devbox"}
	original := camperrors.New("ssh to devbox timed out")
	got := campNotFoundHint(original, m, "camp")
	if got != original {
		t.Errorf("campNotFoundHint changed a non-CommandError: got %v, want unchanged %v", got, original)
	}
}

func TestParseTailscaleCheckURL(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/l623187f3a1372\n"
	url, ok := ParseTailscaleCheckURL(stderr)
	if !ok {
		t.Fatal("ParseTailscaleCheckURL returned false, want true")
	}
	if url != "https://login.tailscale.com/a/l623187f3a1372" {
		t.Errorf("url = %q", url)
	}

	if _, ok := ParseTailscaleCheckURL("ssh: connect to host timed out"); ok {
		t.Error("plain timeout should not parse as Tailscale check")
	}
	if _, ok := ParseTailscaleCheckURL(""); ok {
		t.Error("empty stderr should not parse")
	}
	// Marker without a URL is not actionable enough to claim success.
	if _, ok := ParseTailscaleCheckURL("Tailscale SSH requires an additional check."); ok {
		t.Error("marker without URL should return false")
	}
}

func TestSSHTimeoutErrorPreservesTailscaleCheckURL(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/abc123\n"
	err := sshTimeoutError("lance@archdtop.ts.net", stderr, context.DeadlineExceeded)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("should still match context.DeadlineExceeded: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "https://login.tailscale.com/a/abc123") {
		t.Errorf("timeout error dropped Tailscale check URL: %v", err)
	}
	if !strings.Contains(msg, "browser check") {
		t.Errorf("timeout error missing actionable check guidance: %v", err)
	}
	// Must not look like a bare "timed out" with no cause.
	if strings.HasPrefix(msg, "ssh to lance@archdtop.ts.net timed out") && !strings.Contains(msg, "login.tailscale.com") {
		t.Errorf("regressed to stderr-less timeout: %v", err)
	}
}

func TestSSHTimeoutErrorPreservesGenericStderr(t *testing.T) {
	err := sshTimeoutError("box", "kex_exchange_identification: Connection closed", context.DeadlineExceeded)
	if !strings.Contains(err.Error(), "kex_exchange_identification") {
		t.Errorf("generic stderr not preserved on timeout: %v", err)
	}
}

func TestSSHTimeoutErrorWithoutStderr(t *testing.T) {
	err := sshTimeoutError("box", "", context.DeadlineExceeded)
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("plain timeout message = %v", err)
	}
}

func TestSSHExitErrorAnnotatesTailscaleCheck(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/xyz\n"
	err := sshExitError("lance@box", 255, stderr, nil)
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("want CommandError, got %T: %v", err, err)
	}
	if !strings.Contains(cmdErr.Stderr, "https://login.tailscale.com/a/xyz") {
		t.Errorf("CommandError.Stderr = %q", cmdErr.Stderr)
	}
	if detail := TailscaleCheckDetail(err); detail == "" || !strings.Contains(detail, "login.tailscale.com/a/xyz") {
		t.Errorf("TailscaleCheckDetail = %q", detail)
	}
}

func TestTailscaleCheckDetailFromWrappedTimeout(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/wrap1\n"
	err := sshTimeoutError("host", stderr, context.DeadlineExceeded)
	detail := TailscaleCheckDetail(err)
	if !strings.Contains(detail, "https://login.tailscale.com/a/wrap1") {
		t.Errorf("TailscaleCheckDetail from timeout wrap = %q", detail)
	}
}

func TestCompactSSHStderr(t *testing.T) {
	if got := compactSSHStderr("# ignore\nreal problem here\n"); got != "real problem here" {
		t.Errorf("compactSSHStderr = %q", got)
	}
	if got := compactSSHStderr("single line"); got != "single line" {
		t.Errorf("single line = %q", got)
	}
}
