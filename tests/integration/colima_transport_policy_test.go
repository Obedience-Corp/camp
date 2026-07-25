//go:build integration && (darwin || linux)

package integration

import (
	"path/filepath"
	"testing"
)

func TestColimaSSHConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerHost string
		config     string
		host       string
		ok         bool
	}{
		{
			name:       "default profile",
			dockerHost: "unix:///Users/test/.colima/default/docker.sock",
			config:     "/Users/test/.colima/_lima/colima/ssh.config",
			host:       "lima-colima",
			ok:         true,
		},
		{
			name:       "legacy default socket",
			dockerHost: "unix:///Users/test/.colima/docker.sock",
			config:     "/Users/test/.colima/_lima/colima/ssh.config",
			host:       "lima-colima",
			ok:         true,
		},
		{
			name:       "named profile",
			dockerHost: "unix:///Users/test/.colima/ci/docker.sock",
			config:     "/Users/test/.colima/_lima/colima-ci/ssh.config",
			host:       "lima-colima-ci",
			ok:         true,
		},
		{name: "native Docker", dockerHost: "unix:///var/run/docker.sock"},
		{name: "remote Docker", dockerHost: "tcp://docker.example:2376"},
		{name: "not Docker socket", dockerHost: "unix:///Users/test/.colima/default/containerd.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config, host, ok := colimaSSHConfig(tt.dockerHost)
			if ok != tt.ok {
				t.Fatalf("colimaSSHConfig(%q) ok = %v, want %v", tt.dockerHost, ok, tt.ok)
			}
			if filepath.Clean(config) != filepath.Clean(tt.config) {
				t.Errorf("colimaSSHConfig(%q) config = %q, want %q", tt.dockerHost, config, tt.config)
			}
			if host != tt.host {
				t.Errorf("colimaSSHConfig(%q) host = %q, want %q", tt.dockerHost, host, tt.host)
			}
		})
	}
}
