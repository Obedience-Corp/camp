package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestEmitShellConnectLocal(t *testing.T) {
	var buf bytes.Buffer
	if err := emitShellConnect(&buf, false, "/home/lance/campaigns/obey", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "cd -- '/home/lance/campaigns/obey'\n"; got != want {
		t.Errorf("local shell-connect = %q, want %q", got, want)
	}
}

func TestEmitShellConnectRemoteWithUser(t *testing.T) {
	var buf bytes.Buffer
	m := &machines.Machine{ID: "devbox", Host: "devbox.ts.net", SSHUser: "lance", AuthMethod: machines.AuthSSHAgent}
	if err := emitShellConnect(&buf, true, "/srv/campaigns/obey", m); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "exec ssh -t ") {
		t.Errorf("remote line must start with exec ssh -t: %q", out)
	}
	if !strings.Contains(out, "'lance@devbox.ts.net'") {
		t.Errorf("remote target must be quoted user@host: %q", out)
	}
	if !strings.Contains(out, `'cd '\''/srv/campaigns/obey'\'' && exec $SHELL -l'`) {
		t.Errorf("remote command not correctly nested-quoted: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("remote line must end with newline: %q", out)
	}
}

func TestEmitShellConnectRemoteNoUser(t *testing.T) {
	var buf bytes.Buffer
	m := &machines.Machine{ID: "box", Host: "box.local", AuthMethod: machines.AuthSSHAgent}
	if err := emitShellConnect(&buf, true, "/x", m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), " 'box.local' ") {
		t.Errorf("target without ssh_user must be bare host: %q", buf.String())
	}
}

func TestEmitShellConnectRemoteInjectionSafe(t *testing.T) {
	var buf bytes.Buffer
	m := &machines.Machine{ID: "box", Host: "box.local", AuthMethod: machines.AuthSSHAgent}
	// A remote root containing shell metacharacters must be single-quoted so it
	// cannot break out of the command.
	if err := emitShellConnect(&buf, true, "/x; rm -rf $HOME", m); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// The dangerous payload must appear only inside the single-quoted remote
	// command segment, never as bare shell tokens.
	if strings.Contains(out, "; rm -rf $HOME &&") && !strings.Contains(out, `'/x; rm -rf $HOME'`) {
		t.Errorf("metacharacter payload not quoted: %q", out)
	}
	if !strings.Contains(out, `'/x; rm -rf $HOME'`) {
		t.Errorf("remote path with metachars must be single-quoted verbatim: %q", out)
	}
}
