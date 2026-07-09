package remote

import (
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
