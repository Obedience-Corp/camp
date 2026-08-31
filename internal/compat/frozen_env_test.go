package compat

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/defercommit"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// TestFrozenEnvVarNames pins the variables scripts, agent harnesses, and CI
// already export. Renaming one does not fail loudly: the new name is simply
// never set, so camp silently reverts to its default behavior.
func TestFrozenEnvVarNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"campaign root override", campaign.EnvCampaignRoot, "CAMP_ROOT"},
		{"cache disable", campaign.EnvCacheDisable, "CAMP_CACHE_DISABLE"},
		{"machine name", artifacts.EnvMachineName, "CAMP_MACHINE_NAME"},
		{"quest context", quest.QuestEnvVar, "CAMP_QUEST"},
		{"deferral disable", defercommit.EnvNoDefer, "CAMP_NO_DEFER"},
		{"peer fallback disable", remote.NoPeerFallbackEnv, "CAMP_NO_PEER_FALLBACK"},
		{"remote camp binary path", remote.RemoteCampPathEnv, "CAMP_REMOTE_CAMP_PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("frozen env var renamed: got %q, want %q (docs/terminology.md, Environment variables)", tt.got, tt.want)
			}
		})
	}
}

// TestFrozenEnvVarsStillReferenced covers the variables whose names are not
// exported as constants: they are read from string literals, or consumed by a
// sibling tool out of camp's own environment. A source scan is the honest check
// here, and it is the one that catches a bulk rename pass.
func TestFrozenEnvVarsStillReferenced(t *testing.T) {
	sources := goSources(t, moduleRoot(t))

	for _, name := range []string{
		"CAMP_REGISTRY_PATH",
		"CAMP_MACHINES_PATH",
		"CAMP_HOP_ORIGIN",
		"CAMP_WORKITEM_REF",
		"OBEY_SESSION",
		"OBEY_AGENT",
	} {
		t.Run(name, func(t *testing.T) {
			if !containsNeedle(sources, name) {
				t.Fatalf("%s no longer appears in camp's Go sources; a frozen environment variable was renamed away", name)
			}
		})
	}
}

// moduleRoot returns the repository root, since this package sits two levels
// below it.
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving working directory: %v", err)
	}
	return filepath.Join(wd, "..", "..")
}

// goSources reads camp's production Go files once, so a table of names costs
// one tree walk rather than one per name.
//
// Test files are excluded, and that exclusion is the point rather than an
// optimization: this file spells out every name it goes looking for, so a scan
// that included tests would find each one in its own assertion and pass no
// matter what the rest of camp says.
func goSources(t *testing.T, root string) [][]byte {
	t.Helper()

	var files [][]byte
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "node_modules", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files = append(files, data)
		return nil
	})
	if err != nil {
		t.Fatalf("scanning Go sources: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("scanned no Go sources; this check is looking in the wrong place")
	}
	return files
}

func containsNeedle(sources [][]byte, needle string) bool {
	probe := []byte(needle)
	for _, src := range sources {
		if bytes.Contains(src, probe) {
			return true
		}
	}
	return false
}
