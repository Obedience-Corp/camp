package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestParseIntentStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    intent.Status
		wantErr bool
	}{
		{name: "inbox", input: "inbox", want: intent.StatusInbox},
		{name: "ready", input: "ready", want: intent.StatusReady},
		{name: "active", input: "active", want: intent.StatusActive},
		{name: "done short", input: "done", want: intent.StatusDone},
		{name: "done canonical", input: "dungeon/done", want: intent.StatusDone},
		{name: "killed short", input: "killed", want: intent.StatusKilled},
		{name: "killed canonical", input: "dungeon/killed", want: intent.StatusKilled},
		{name: "archived short", input: "archived", want: intent.StatusArchived},
		{name: "archived canonical", input: "dungeon/archived", want: intent.StatusArchived},
		{name: "someday short", input: "someday", want: intent.StatusSomeday},
		{name: "someday canonical", input: "dungeon/someday", want: intent.StatusSomeday},
		{name: "invalid", input: "pending", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIntentStatus(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseIntentStatus(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseIntentStatus(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseIntentStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
