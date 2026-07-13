package peer

import (
	"strings"
	"testing"
)

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

func TestSourceRefspec(t *testing.T) {
	s := FromPath("studio-mac", "/x")
	want := "+refs/heads/*:refs/peer/studio-mac/*"
	if got := s.Refspec(); got != want {
		t.Errorf("Refspec() = %q, want %q", got, want)
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
