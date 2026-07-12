package remote

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
