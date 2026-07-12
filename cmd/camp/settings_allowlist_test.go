package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func newTestAllowlist() *config.Allowlist {
	return &config.Allowlist{
		Version: 1,
		Commands: map[string]config.AllowlistCommand{
			"git":  {Allowed: true, Description: "Version control"},
			"curl": {Allowed: false, Description: "HTTP"},
		},
		InheritDefaults: true,
	}
}

func TestAllowlistOptions_SortedWithControls(t *testing.T) {
	al := newTestAllowlist()
	got := optionValues(allowlistOptions(al))
	want := []string{"cmd:curl", "cmd:git", allowlistAddValue, valSeparator, valBack}
	if len(got) != len(want) {
		t.Fatalf("allowlistOptions values = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("allowlistOptions[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSetCommandAllowed(t *testing.T) {
	al := newTestAllowlist()
	setCommandAllowed(al, "curl", true)
	if !al.Commands["curl"].Allowed {
		t.Error("curl should be allowed after toggle")
	}
	// Description preserved; other fields untouched.
	if al.Commands["curl"].Description != "HTTP" {
		t.Errorf("description changed: %q", al.Commands["curl"].Description)
	}
	if !al.InheritDefaults || al.Version != 1 {
		t.Error("toggle must not touch InheritDefaults or Version")
	}
	// Unknown command is a no-op (no panic, no insert).
	setCommandAllowed(al, "nope", true)
	if _, ok := al.Commands["nope"]; ok {
		t.Error("toggling an unknown command must not create it")
	}
}

func TestRemoveAllowlistCommand(t *testing.T) {
	al := newTestAllowlist()
	removeAllowlistCommand(al, "git")
	if _, ok := al.Commands["git"]; ok {
		t.Error("git should be removed")
	}
	if !al.InheritDefaults {
		t.Error("remove must not touch InheritDefaults")
	}
}

func TestAddAllowlistCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmdName string
		wantErr bool
	}{
		{"empty name rejected", "  ", true},
		{"duplicate rejected", "git", true},
		{"new command added", "docker", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al := newTestAllowlist()
			err := addAllowlistCommand(al, tt.cmdName, true, "desc")
			if (err != nil) != tt.wantErr {
				t.Fatalf("addAllowlistCommand(%q) err = %v, wantErr %v", tt.cmdName, err, tt.wantErr)
			}
			if !tt.wantErr {
				c, ok := al.Commands["docker"]
				if !ok || !c.Allowed || c.Description != "desc" {
					t.Errorf("added command = %+v, ok=%v", c, ok)
				}
				if !al.InheritDefaults {
					t.Error("add must not touch InheritDefaults")
				}
			}
		})
	}
}
