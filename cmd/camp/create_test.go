package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
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
				if tc.errMsg != "" && !containsStr(err.Error(), tc.errMsg) {
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

// containsStr is a helper to check substring membership without importing strings.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// TestResolveCreateBase_FromConfig verifies that when --parent-dir is absent,
// resolveCreateBase reads CampaignsDir from the global config.
func TestResolveCreateBase_FromConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Write a minimal global config with CampaignsDir set.
	ctx := context.Background()
	want := "/tmp/test-campaigns-dir"
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = want
	if err := config.SaveGlobalConfig(ctx, &cfg); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	// Build a synthetic cobra command with the parent-dir flag (not set).
	cmd := &cobra.Command{}
	cmd.Flags().String("parent-dir", "", "")

	got, err := resolveCreateBase(ctx, cmd)
	if err != nil {
		t.Fatalf("resolveCreateBase() error = %v", err)
	}
	if got != want {
		t.Errorf("resolveCreateBase() = %q, want %q", got, want)
	}
}

// TestResolveCreateBase_ParentDirFlagOverride verifies that --parent-dir
// takes precedence over GlobalConfig.CampaignsDir.
func TestResolveCreateBase_ParentDirFlagOverride(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

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

// TestCheckCreateTarget covers the collision-detection rules.
func TestCheckCreateTarget(t *testing.T) {
	t.Run("missing target is ok", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "nonexistent")
		if err := checkCreateTarget(target); err != nil {
			t.Errorf("checkCreateTarget(missing) = %v, want nil", err)
		}
	})

	t.Run("empty dir is ok", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "empty")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := checkCreateTarget(target); err != nil {
			t.Errorf("checkCreateTarget(empty dir) = %v, want nil", err)
		}
	})

	t.Run("non-empty without .campaign errors with exists and is not empty", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "notempty")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, "somefile.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		err := checkCreateTarget(target)
		if err == nil {
			t.Fatal("checkCreateTarget(non-empty) = nil, want error")
		}
		if !containsStr(err.Error(), "exists and is not empty") {
			t.Errorf("error = %q, want to contain 'exists and is not empty'", err.Error())
		}
	})

	t.Run("non-empty with .campaign errors mentioning camp init --repair", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "hascampaign")
		campaignMarker := filepath.Join(target, ".campaign")
		if err := os.MkdirAll(campaignMarker, 0o755); err != nil {
			t.Fatalf("MkdirAll .campaign: %v", err)
		}
		err := checkCreateTarget(target)
		if err == nil {
			t.Fatal("checkCreateTarget(has .campaign) = nil, want error")
		}
		if !containsStr(err.Error(), "camp init --repair") {
			t.Errorf("error = %q, want to contain 'camp init --repair'", err.Error())
		}
	})
}

// TestChooseCreateWriters asserts the correct writer routing in both modes.
func TestChooseCreateWriters(t *testing.T) {
	t.Run("default mode both writers are stdout", func(t *testing.T) {
		w := chooseCreateWriters(false)
		if w.humanOut != os.Stdout {
			t.Errorf("default mode humanOut = %v, want os.Stdout", w.humanOut)
		}
		if w.machineOut != os.Stdout {
			t.Errorf("default mode machineOut = %v, want os.Stdout", w.machineOut)
		}
	})

	t.Run("print-path mode humanOut is stderr, machineOut is stdout", func(t *testing.T) {
		w := chooseCreateWriters(true)
		if w.humanOut != os.Stderr {
			t.Errorf("print-path humanOut = %v, want os.Stderr", w.humanOut)
		}
		if w.machineOut != os.Stdout {
			t.Errorf("print-path machineOut = %v, want os.Stdout", w.machineOut)
		}
	})
}
