package config

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// mustConfigDir fails the test if the config directory cannot be resolved.
func mustConfigDir(t *testing.T) string {
	t.Helper()
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	return dir
}

// mustGlobalConfigPath fails the test if the global config path cannot be resolved.
func mustGlobalConfigPath(t *testing.T) string {
	t.Helper()
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath() error = %v", err)
	}
	return path
}

// mustRegistryPath fails the test if the registry path cannot be resolved.
func mustRegistryPath(t *testing.T) string {
	t.Helper()
	path, err := RegistryPath()
	if err != nil {
		t.Fatalf("RegistryPath() error = %v", err)
	}
	return path
}

// clearHome unsets every variable the path resolvers consult, so the resolvers
// see the same environment camp sees under a login shell that never exported
// HOME. os.UserHomeDir reads $HOME on unix and %USERPROFILE% on Windows.
func clearHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CAMP_REGISTRY_PATH", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
}

// A campaign registry resolved to a relative path is written under whatever
// directory camp was invoked from and is then invisible to the next invocation
// from anywhere else, which presents as "camp create did not register my
// campaign". Failing to resolve must be an error, never a relative path.
func TestRegistryPath_NoHomeIsAnError(t *testing.T) {
	clearHome(t)

	got, err := RegistryPath()
	if err == nil {
		t.Fatalf("RegistryPath() = %q, want an error when no home directory resolves", got)
	}
	if got != "" {
		t.Errorf("RegistryPath() = %q, want empty path alongside the error", got)
	}
	if !strings.Contains(err.Error(), "CAMP_REGISTRY_PATH") {
		t.Errorf("RegistryPath() error = %q, want it to name the CAMP_REGISTRY_PATH override", err)
	}
}

func TestConfigDir_NoHomeIsAnError(t *testing.T) {
	clearHome(t)

	got, err := ConfigDir()
	if err == nil {
		t.Fatalf("ConfigDir() = %q, want an error when no home directory resolves", got)
	}
	if got != "" {
		t.Errorf("ConfigDir() = %q, want empty path alongside the error", got)
	}
}

func TestGlobalConfigPath_NoHomeIsAnError(t *testing.T) {
	clearHome(t)

	if got, err := GlobalConfigPath(); err == nil {
		t.Fatalf("GlobalConfigPath() = %q, want an error when no home directory resolves", got)
	}
}

func TestEnsureConfigDir_NoHomeIsAnError(t *testing.T) {
	clearHome(t)

	if err := EnsureConfigDir(); err == nil {
		t.Fatal("EnsureConfigDir() = nil, want an error when no home directory resolves")
	}
	if _, err := os.Stat(filepath.Join(".obey", AppName)); err == nil {
		t.Error("EnsureConfigDir() created a working-directory-relative config dir")
	}
}

// The overrides still win with no home directory: they are the documented way
// to run camp where HOME is not set.
func TestRegistryPath_OverrideWinsWithoutHome(t *testing.T) {
	clearHome(t)
	want := filepath.Join(t.TempDir(), "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", want)

	got, err := RegistryPath()
	if err != nil {
		t.Fatalf("RegistryPath() error = %v", err)
	}
	if got != want {
		t.Errorf("RegistryPath() = %q, want %q", got, want)
	}
}

func TestConfigDir_XDGWinsWithoutHome(t *testing.T) {
	clearHome(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	if want := filepath.Join(dir, OrgName, AppName); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

// campaigns_dir is where `camp create` puts the campaign itself. A blank HOME
// expands "~/campaigns" to a relative path, and the "still relative" repair step
// then joins it onto the same blank home again, so the campaign lands in
// ./<blank>/<blank>/campaigns under the invoking directory. Refuse instead.
func TestResolvedCampaignsDir_NoUsableHomeIsAnError(t *testing.T) {
	for _, home := range []string{"", "   "} {
		t.Run("home="+strconv.Quote(home), func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)

			cfg := DefaultGlobalConfig()
			got, err := cfg.ResolvedCampaignsDir(context.Background())
			if err == nil {
				t.Fatalf("ResolvedCampaignsDir() = %q, want an error with no usable home", got)
			}
			if got != "" {
				t.Errorf("ResolvedCampaignsDir() = %q, want empty path alongside the error", got)
			}
		})
	}
}

// An absolute campaigns_dir never consults the home directory, so it keeps
// working where HOME does not exist.
func TestResolvedCampaignsDir_AbsoluteNeedsNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	want := t.TempDir()
	cfg := DefaultGlobalConfig()
	cfg.CampaignsDir = want

	got, err := cfg.ResolvedCampaignsDir(context.Background())
	if err != nil {
		t.Fatalf("ResolvedCampaignsDir() error = %v", err)
	}
	if got != want {
		t.Errorf("ResolvedCampaignsDir() = %q, want %q", got, want)
	}
}
