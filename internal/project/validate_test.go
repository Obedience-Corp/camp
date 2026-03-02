package project

import (
	"context"
	"errors"
	"testing"
)

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple alphanumeric", input: "camp", wantErr: false},
		{name: "with hyphen", input: "my-project", wantErr: false},
		{name: "with underscore", input: "my_project", wantErr: false},
		{name: "with dot", input: "my.project", wantErr: false},
		{name: "mixed", input: "camp-v2.0_final", wantErr: false},
		{name: "single char", input: "a", wantErr: false},

		{name: "empty string", input: "", wantErr: true},
		{name: "dotdot alone", input: "..", wantErr: true},
		{name: "dotdot with path", input: "../etc", wantErr: true},
		{name: "embedded dotdot", input: "proj/../evil", wantErr: true},
		{name: "forward slash", input: "proj/evil", wantErr: true},
		{name: "backslash", input: `proj\evil`, wantErr: true},
		{name: "absolute path", input: "/etc/passwd", wantErr: true},
		{name: "starts with dot", input: ".hidden", wantErr: true},
		{name: "starts with hyphen", input: "-bad", wantErr: true},
		{name: "space in name", input: "my project", wantErr: true},
		{name: "null byte", input: "proj\x00evil", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProjectName(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateProjectName(%q): expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateProjectName(%q): expected no error, got: %v", tc.input, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidProjectName) {
				t.Errorf("ValidateProjectName(%q): expected ErrInvalidProjectName, got: %v", tc.input, err)
			}
		})
	}
}

func TestRemove_RejectsInvalidName(t *testing.T) {
	ctx := context.Background()
	_, err := Remove(ctx, "/nonexistent-root", "../escape", RemoveOptions{})
	if err == nil {
		t.Fatal("expected error for traversal name, got nil")
	}
	if !errors.Is(err, ErrInvalidProjectName) {
		t.Errorf("expected ErrInvalidProjectName, got: %v", err)
	}
}
