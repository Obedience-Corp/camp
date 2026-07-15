package peer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A config/usage failure (unknown machine id) must be classified ErrPeerConfig
// so callers fail fast; it must not be mistaken for a reachability failure.
func TestFromMachine_UnknownMachineIsConfigError(t *testing.T) {
	machinesFile := filepath.Join(t.TempDir(), "machines.yaml")
	if err := os.WriteFile(machinesFile, []byte("version: 1\nmachines: []\n"), 0o644); err != nil {
		t.Fatalf("writing machines file: %v", err)
	}
	t.Setenv("CAMP_MACHINES_PATH", machinesFile)

	_, err := FromMachine(context.Background(), "no-such-machine", "campaign")
	if err == nil {
		t.Fatal("FromMachine() error = nil, want ErrPeerConfig")
	}
	if !errors.Is(err, ErrPeerConfig) {
		t.Errorf("FromMachine() error = %v, want wrapped ErrPeerConfig", err)
	}
}

// The reserved "local" id is a usage error, also ErrPeerConfig.
func TestFromMachine_LocalIsConfigError(t *testing.T) {
	machinesFile := filepath.Join(t.TempDir(), "machines.yaml")
	if err := os.WriteFile(machinesFile, []byte("version: 1\nmachines: []\n"), 0o644); err != nil {
		t.Fatalf("writing machines file: %v", err)
	}
	t.Setenv("CAMP_MACHINES_PATH", machinesFile)

	_, err := FromMachine(context.Background(), "local", "campaign")
	if err == nil || !errors.Is(err, ErrPeerConfig) {
		t.Errorf("FromMachine(local) error = %v, want ErrPeerConfig", err)
	}
}

func TestSourceURL(t *testing.T) {
	tests := []struct {
		name    string
		source  *Source
		relPath string
		want    string
	}{
		{
			name:    "filesystem root",
			source:  FromPath("vol", "/Volumes/backup/campaign"),
			relPath: "",
			want:    "/Volumes/backup/campaign",
		},
		{
			name:    "filesystem submodule",
			source:  FromPath("vol", "/Volumes/backup/campaign"),
			relPath: "projects/camp",
			want:    "/Volumes/backup/campaign/projects/camp",
		},
		{
			name:    "ssh root",
			source:  &Source{id: "studio", root: "/Users/me/campaign", target: "me@studio.local"},
			relPath: "",
			want:    "ssh://me@studio.local/Users/me/campaign",
		},
		{
			name:    "ssh submodule",
			source:  &Source{id: "studio", root: "/Users/me/campaign", target: "studio.local"},
			relPath: "projects/camp",
			want:    "ssh://studio.local/Users/me/campaign/projects/camp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.URL(tt.relPath); got != tt.want {
				t.Errorf("URL(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestSourceRsyncSpec(t *testing.T) {
	tests := []struct {
		name    string
		source  *Source
		relPath string
		want    string
	}{
		{
			name:    "filesystem source is an unquoted local path",
			source:  FromPath("vol", "/Volumes/backup/campaign"),
			relPath: "media/renders",
			want:    "/Volumes/backup/campaign/media/renders/",
		},
		{
			// The remote path is unquoted: rsync 3.2.4+ protects args itself
			// (no remote shell), so a caller quote would be taken literally.
			// The pull's -s/--secluded-args flag is what keeps spaces and
			// metacharacters safe over the wire.
			name:    "ssh source leaves the remote path unquoted",
			source:  &Source{root: "/home/me/campaign", target: "me@studio"},
			relPath: "media/renders",
			want:    "me@studio:/home/me/campaign/media/renders/",
		},
		{
			name:    "space in an ssh remote path stays unquoted (protected by -s)",
			source:  &Source{root: "/home/me/campaign", target: "me@studio"},
			relPath: "Final Renders",
			want:    "me@studio:/home/me/campaign/Final Renders/",
		},
		{
			name:    "shell metacharacters in an ssh remote path stay unquoted (protected by -s)",
			source:  &Source{root: "/root", target: "me@studio"},
			relPath: "a; rm -rf ~",
			want:    "me@studio:/root/a; rm -rf ~/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.RsyncSpec(tt.relPath); got != tt.want {
				t.Errorf("RsyncSpec(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestSourceRefspec(t *testing.T) {
	s := FromPath("studio-mac", "/x")
	wantHeads := "+refs/heads/*:refs/peer/studio-mac/*"
	if got := s.Refspec(); got != wantHeads {
		t.Errorf("Refspec() = %q, want %q", got, wantHeads)
	}
	want := []string{
		"+HEAD:refs/peer/studio-mac/HEAD",
		wantHeads,
	}
	got := s.Refspecs()
	if len(got) != len(want) {
		t.Fatalf("Refspecs() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Refspecs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSourceGitEnv(t *testing.T) {
	fs := FromPath("vol", "/x")
	if env := fs.GitEnv(); env != nil {
		t.Errorf("filesystem source GitEnv() = %v, want nil", env)
	}

	ssh := &Source{
		id:      "studio",
		root:    "/x",
		target:  "me@studio",
		sshOpts: []string{"-o", "BatchMode=yes", "-o", "ControlPath=/tmp/has space/x.sock"},
	}
	env := ssh.GitEnv()
	if env == nil {
		t.Fatal("ssh source GitEnv() = nil, want environment")
	}
	var sshCmd string
	for _, e := range env {
		if rest, ok := strings.CutPrefix(e, "GIT_SSH_COMMAND="); ok {
			sshCmd = rest
		}
	}
	if sshCmd == "" {
		t.Fatal("GIT_SSH_COMMAND not present in GitEnv()")
	}
	if !strings.HasPrefix(sshCmd, "ssh ") {
		t.Errorf("GIT_SSH_COMMAND = %q, want ssh prefix", sshCmd)
	}
	if !strings.Contains(sshCmd, "'BatchMode=yes'") {
		t.Errorf("GIT_SSH_COMMAND = %q, want quoted BatchMode option", sshCmd)
	}
	if !strings.Contains(sshCmd, "'ControlPath=/tmp/has space/x.sock'") {
		t.Errorf("GIT_SSH_COMMAND = %q, want space-containing option single-quoted", sshCmd)
	}
}
