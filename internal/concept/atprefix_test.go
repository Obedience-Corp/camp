package concept

import (
	"strings"
	"testing"
)

func TestResolveAtPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		// Basic resolutions
		{name: "@p resolves to projects", input: "@p", want: "projects"},
		{name: "@w resolves to workflow", input: "@w", want: "workflow"},
		{name: "@f resolves to festivals", input: "@f", want: "festivals"},
		{name: "@d resolves to docs", input: "@d", want: "docs"},

		// With trailing paths
		{name: "@p with subpath", input: "@p/fest", want: "projects/fest"},
		{name: "@p with deep subpath", input: "@p/fest/internal", want: "projects/fest/internal"},
		{name: "@f with subpath", input: "@f/active/my-fest", want: "festivals/active/my-fest"},
		{name: "@d with subpath", input: "@d/README.md", want: "docs/README.md"},

		// Trailing slash
		{name: "@p with trailing slash", input: "@p/", want: "projects"},

		// Pass-through (no @ prefix)
		{name: "no prefix passes through", input: "workflow/design", want: "workflow/design"},
		{name: "empty passes through", input: "", want: ""},
		{name: "relative path passes through", input: "projects/camp", want: "projects/camp"},
		{name: "absolute path passes through", input: "/tmp/test", want: "/tmp/test"},

		// Errors
		{name: "unknown shortcut", input: "@x", wantErr: "unknown concept shortcut: @x"},
		{name: "unknown with path", input: "@z/foo", wantErr: "unknown concept shortcut: @z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAtPath(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveAtPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
