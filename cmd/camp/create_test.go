package main

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestValidateCampaignName covers the name validation rules.
func TestValidateCampaignName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
			errMsg:  "campaign name is empty",
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
			errMsg:  "campaign name is empty",
		},
		{
			name:    "dot",
			input:   ".",
			wantErr: true,
			errMsg:  "invalid campaign name",
		},
		{
			name:    "double dot",
			input:   "..",
			wantErr: true,
			errMsg:  "invalid campaign name",
		},
		{
			name:    "foo/bar path separator",
			input:   "foo/bar",
			wantErr: true,
			errMsg:  "cannot contain path separators",
		},
		{
			name:    "foo\\bar backslash",
			input:   "foo\\bar",
			wantErr: true,
			errMsg:  "cannot contain path separators",
		},
		{
			name:    "dotdir starts with dot",
			input:   ".dotdir",
			wantErr: true,
			errMsg:  "cannot start with '.'",
		},
		{
			name:    "valid my-project",
			input:   "my-project",
			wantErr: false,
		},
		{
			name:    "valid my_project_v2",
			input:   "my_project_v2",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCampaignName(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateCampaignName(%q) = nil, want error containing %q", tc.input, tc.errMsg)
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("validateCampaignName(%q) error = %q, want it to contain %q", tc.input, err.Error(), tc.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateCampaignName(%q) = %v, want nil", tc.input, err)
				}
			}
		})
	}
}

// TestResolveCreateBase_ParentDirFlagOverride verifies that --parent-dir
// takes precedence over GlobalConfig.CampaignsDir.
func TestResolveCreateBase_ParentDirFlagOverride(t *testing.T) {
	ctx := context.Background()
	want := "/tmp/override"

	cmd := &cobra.Command{}
	cmd.Flags().String("parent-dir", "", "")
	if err := cmd.Flags().Set("parent-dir", want); err != nil {
		t.Fatalf("failed to set --parent-dir flag: %v", err)
	}

	got, err := resolveCreateBase(ctx, cmd)
	if err != nil {
		t.Fatalf("resolveCreateBase() error = %v", err)
	}
	if got != want {
		t.Errorf("resolveCreateBase() = %q, want %q", got, want)
	}
}
