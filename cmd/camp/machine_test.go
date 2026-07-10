package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestValidateMachineID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "valid lowercase", id: "devbox", wantErr: false},
		{name: "valid with digits and hyphens", id: "dev-box-2", wantErr: false},
		{name: "reserved local", id: "local", wantErr: true},
		{name: "empty", id: "", wantErr: true},
		{name: "uppercase", id: "DevBox", wantErr: true},
		{name: "leading digit", id: "2box", wantErr: true},
		{name: "spaces", id: "dev box", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMachineID(tt.id)
			if tt.wantErr && err == nil {
				t.Fatalf("validateMachineID(%q) error = nil, want error", tt.id)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateMachineID(%q) error = %v, want nil", tt.id, err)
			}
		})
	}
}

func TestNormalizeAuthMethod(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to ssh-agent", in: "", want: machines.AuthSSHAgent},
		{name: "tailscale-ssh", in: machines.AuthTailscaleSSH, want: machines.AuthTailscaleSSH},
		{name: "ssh-agent", in: machines.AuthSSHAgent, want: machines.AuthSSHAgent},
		{name: "ssh-password", in: machines.AuthSSHPassword, want: machines.AuthSSHPassword},
		{name: "unknown value rejected", in: "bogus", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAuthMethod(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeAuthMethod(%q) error = nil, want error", tt.in)
				}
				for _, valid := range []string{machines.AuthTailscaleSSH, machines.AuthSSHAgent, machines.AuthSSHPassword} {
					if !strings.Contains(err.Error(), valid) {
						t.Errorf("error %q does not list valid auth method %q", err.Error(), valid)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeAuthMethod(%q) error = %v, want nil", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeAuthMethod(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMachineJSONFieldNames locks in the decision that `camp machine list
// --json` uses the machines.yaml schema field names (id/label/host/auth_method/
// ssh_user/identity_file), not Go's default exported-field JSON encoding.
func TestMachineJSONFieldNames(t *testing.T) {
	m := machines.Machine{
		ID:           "devbox",
		Label:        "Dev Box",
		Host:         "devbox.ts.net",
		AuthMethod:   machines.AuthSSHAgent,
		SSHUser:      "lance",
		IdentityFile: "~/.ssh/id_ed25519",
	}
	data, err := json.Marshal(toMachineJSON(m))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, key := range []string{`"id"`, `"label"`, `"host"`, `"auth_method"`, `"ssh_user"`, `"identity_file"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshaled JSON %s missing key %s", data, key)
		}
	}
}

// TestMachineJSONLocalRowIsSyntheticAndMinimal locks in the Step-2 decision:
// the synthetic "local" row in `camp machine list --json` degrades to exactly
// {"id":"local"} (every other field omitted via omitempty), not six mostly-
// empty keys.
func TestMachineJSONLocalRowIsSyntheticAndMinimal(t *testing.T) {
	data, err := json.Marshal(machineJSON{ID: machines.LocalMachineID})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if got, want := string(data), `{"id":"local"}`; got != want {
		t.Fatalf("marshaled synthetic local row = %s, want %s", got, want)
	}
}

func TestToMachineJSONRoundTripsAllFields(t *testing.T) {
	m := machines.Machine{
		ID:           "devbox",
		Label:        "Dev Box",
		Host:         "devbox.ts.net",
		AuthMethod:   machines.AuthTailscaleSSH,
		SSHUser:      "lance",
		IdentityFile: "~/.ssh/id_ed25519",
	}
	got := toMachineJSON(m)
	want := machineJSON{
		ID:           "devbox",
		Label:        "Dev Box",
		Host:         "devbox.ts.net",
		AuthMethod:   machines.AuthTailscaleSSH,
		SSHUser:      "lance",
		IdentityFile: "~/.ssh/id_ed25519",
	}
	if got != want {
		t.Fatalf("toMachineJSON() = %+v, want %+v", got, want)
	}
}
