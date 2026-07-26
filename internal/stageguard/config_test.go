package stageguard

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestParseByteSizeErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "empty string", raw: ""},
		{name: "whitespace only", raw: "   "},
		{name: "unit with no number", raw: "MiB"},
		{name: "non-numeric", raw: "banana"},
		{name: "float is not accepted", raw: "1.5MiB"},
		{name: "negative", raw: "-10MiB"},
		{name: "unknown unit", raw: "10PiB"},
		{name: "trailing garbage", raw: "10MiB!"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseByteSize(tc.raw)
			if err == nil {
				t.Fatalf("ParseByteSize(%q) = %d, want error", tc.raw, got)
			}
		})
	}
}

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "bare number is bytes", raw: "1024", want: 1024},
		{name: "zero", raw: "0", want: 0},
		{name: "binary mebibyte", raw: "10MiB", want: 10 << 20},
		{name: "decimal megabyte", raw: "50MB", want: 50 * 1000 * 1000},
		{name: "case insensitive", raw: "10mib", want: 10 << 20},
		{name: "space before unit", raw: "10 MiB", want: 10 << 20},
		{name: "surrounding whitespace", raw: "  25MiB  ", want: 25 << 20},
		{name: "kibibyte", raw: "512KiB", want: 512 << 10},
		{name: "gibibyte", raw: "2GiB", want: 2 << 30},
		{name: "bare B suffix", raw: "900B", want: 900},
		{name: "short binary suffix", raw: "10M", want: 10 << 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseByteSize(tc.raw)
			if err != nil {
				t.Fatalf("ParseByteSize(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestApplyGuardsConfigErrors(t *testing.T) {
	cases := []struct {
		name    string
		guards  config.GuardsConfig
		project string
	}{
		{
			name:   "unknown large_files mode",
			guards: config.GuardsConfig{LargeFiles: "warn"},
		},
		{
			name:   "bulk rejects auto",
			guards: config.GuardsConfig{Bulk: "auto"},
		},
		{
			name:   "unparseable max_file_size",
			guards: config.GuardsConfig{MaxFileSize: "ten megs"},
		},
		{
			name:    "project override mode is validated too",
			guards:  config.GuardsConfig{Projects: map[string]config.GuardsProjectConfig{"camp": {LargeFiles: strPtr("loud")}}},
			project: "camp",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopeProject := tc.project != ""
			if _, err := applyGuardsConfig(tc.guards, scopeProject, tc.project); err == nil {
				t.Fatal("applyGuardsConfig() = nil error, want validation error")
			}
		})
	}
}

func TestApplyGuardsConfigDefaults(t *testing.T) {
	t.Run("empty config at campaign root", func(t *testing.T) {
		limits, err := applyGuardsConfig(config.GuardsConfig{}, false, "")
		if err != nil {
			t.Fatalf("applyGuardsConfig() error = %v", err)
		}
		if limits.MaxFileSize != DefaultMaxFileSize {
			t.Errorf("MaxFileSize = %d, want %d", limits.MaxFileSize, DefaultMaxFileSize)
		}
		if limits.LargeFiles != ModeAuto {
			t.Errorf("LargeFiles = %q, want %q", limits.LargeFiles, ModeAuto)
		}
		if limits.Bulk != ModeBlock {
			t.Errorf("Bulk = %q, want %q", limits.Bulk, ModeBlock)
		}
		if limits.ScopeProject {
			t.Error("ScopeProject = true, want false at the campaign root")
		}
	})

	t.Run("empty config inside a project raises the threshold", func(t *testing.T) {
		limits, err := applyGuardsConfig(config.GuardsConfig{}, true, "camp")
		if err != nil {
			t.Fatalf("applyGuardsConfig() error = %v", err)
		}
		if limits.MaxFileSize != DefaultProjectMaxFileSize {
			t.Errorf("MaxFileSize = %d, want %d", limits.MaxFileSize, DefaultProjectMaxFileSize)
		}
		if !limits.ScopeProject {
			t.Error("ScopeProject = false, want true inside a project")
		}
	})
}

func TestApplyGuardsConfigProjectOverrides(t *testing.T) {
	guards := config.GuardsConfig{
		LargeFiles:         "auto",
		Bulk:               "block",
		MaxFileSize:        "10MiB",
		MaxProjectFileSize: "25MiB",
		MaxAddedFiles:      1000,
		Allow:              []string{"docs/*.pdf"},
		Projects: map[string]config.GuardsProjectConfig{
			"camp": {
				MaxFileSize:   strPtr("100MiB"),
				LargeFiles:    strPtr("block"),
				MaxAddedFiles: intPtr(50),
				Allow:         []string{"testdata/**"},
			},
		},
	}

	cases := []struct {
		name              string
		scopeProject      bool
		project           string
		wantMaxFileSize   int64
		wantMode          Mode
		wantMaxAddedFiles int
		wantAllow         []string
	}{
		{
			name:              "campaign root ignores project legs",
			scopeProject:      false,
			project:           "",
			wantMaxFileSize:   10 << 20,
			wantMode:          ModeAuto,
			wantMaxAddedFiles: 1000,
			wantAllow:         []string{"docs/*.pdf"},
		},
		{
			name:              "overridden project wins",
			scopeProject:      true,
			project:           "camp",
			wantMaxFileSize:   100 << 20,
			wantMode:          ModeBlock,
			wantMaxAddedFiles: 50,
			wantAllow:         []string{"testdata/**"},
		},
		{
			name:              "project without an override takes the project default",
			scopeProject:      true,
			project:           "fest",
			wantMaxFileSize:   25 << 20,
			wantMode:          ModeAuto,
			wantMaxAddedFiles: 1000,
			wantAllow:         []string{"docs/*.pdf"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits, err := applyGuardsConfig(guards, tc.scopeProject, tc.project)
			if err != nil {
				t.Fatalf("applyGuardsConfig() error = %v", err)
			}
			if limits.MaxFileSize != tc.wantMaxFileSize {
				t.Errorf("MaxFileSize = %d, want %d", limits.MaxFileSize, tc.wantMaxFileSize)
			}
			if limits.LargeFiles != tc.wantMode {
				t.Errorf("LargeFiles = %q, want %q", limits.LargeFiles, tc.wantMode)
			}
			if limits.MaxAddedFiles != tc.wantMaxAddedFiles {
				t.Errorf("MaxAddedFiles = %d, want %d", limits.MaxAddedFiles, tc.wantMaxAddedFiles)
			}
			if len(limits.Allow) != len(tc.wantAllow) || (len(tc.wantAllow) > 0 && limits.Allow[0] != tc.wantAllow[0]) {
				t.Errorf("Allow = %v, want %v", limits.Allow, tc.wantAllow)
			}
		})
	}
}

// An explicit empty project allow list replaces the campaign-wide list rather
// than inheriting it, so a project can opt out of global exemptions.
func TestApplyGuardsConfigEmptyProjectAllowReplaces(t *testing.T) {
	guards := config.GuardsConfig{
		Allow: []string{"docs/*.pdf"},
		Projects: map[string]config.GuardsProjectConfig{
			"camp": {Allow: []string{}},
		},
	}
	limits, err := applyGuardsConfig(guards, true, "camp")
	if err != nil {
		t.Fatalf("applyGuardsConfig() error = %v", err)
	}
	if len(limits.Allow) != 0 {
		t.Errorf("Allow = %v, want empty", limits.Allow)
	}
}

func TestDefaultLimitsNoCampaignFallsBackToProjectScope(t *testing.T) {
	// Outside a campaign there is no campaign.yaml; a bare git repo is closer
	// to a project than to a campaign root, so it takes the higher threshold.
	limits := defaultLimits(true)
	if limits.MaxFileSize != DefaultProjectMaxFileSize {
		t.Errorf("MaxFileSize = %d, want %d", limits.MaxFileSize, DefaultProjectMaxFileSize)
	}
	if !limits.ScopeProject {
		t.Error("ScopeProject = false, want true")
	}
	if limits.LargeFiles != ModeAuto || limits.Bulk != ModeBlock {
		t.Errorf("modes = (%q, %q), want (auto, block)", limits.LargeFiles, limits.Bulk)
	}
}

func TestResolveScope(t *testing.T) {
	const root = "/home/u/campaign"

	cases := []struct {
		name             string
		repoPath         string
		wantScopeProject bool
		wantProject      string
	}{
		{
			name:             "campaign root itself",
			repoPath:         root,
			wantScopeProject: false,
			wantProject:      "",
		},
		{
			name:             "project submodule",
			repoPath:         root + "/projects/camp",
			wantScopeProject: true,
			wantProject:      "camp",
		},
		{
			name:             "nested path inside a project resolves to the project",
			repoPath:         root + "/projects/camp/internal/git",
			wantScopeProject: true,
			wantProject:      "camp",
		},
		{
			name:             "worktree resolves to the project it belongs to",
			repoPath:         root + "/projects/worktrees/camp/artifact-commit-guardrails",
			wantScopeProject: true,
			wantProject:      "camp",
		},
		{
			name:             "directory outside projects/ is still project scope",
			repoPath:         root + "/tools/clitest",
			wantScopeProject: true,
			wantProject:      "clitest",
		},
		{
			name:             "path outside the campaign entirely",
			repoPath:         "/home/u/elsewhere/repo",
			wantScopeProject: true,
			wantProject:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopeProject, project := resolveScope(root, tc.repoPath, config.DefaultCampaignPaths())
			if scopeProject != tc.wantScopeProject {
				t.Errorf("scopeProject = %v, want %v", scopeProject, tc.wantScopeProject)
			}
			if project != tc.wantProject {
				t.Errorf("project = %q, want %q", project, tc.wantProject)
			}
		})
	}
}

// The projects and worktrees directories are campaign-configurable, so scope
// resolution must read them rather than assume the defaults.
func TestResolveScopeHonorsConfiguredPaths(t *testing.T) {
	const root = "/home/u/campaign"
	paths := config.CampaignPaths{
		Projects:  "repos/",
		Worktrees: "repos/wt/",
	}

	cases := []struct {
		name        string
		repoPath    string
		wantProject string
	}{
		{
			name:        "configured projects directory",
			repoPath:    root + "/repos/camp",
			wantProject: "camp",
		},
		{
			name:        "configured worktrees directory wins over its parent",
			repoPath:    root + "/repos/wt/camp/some-branch",
			wantProject: "camp",
		},
		{
			name:        "default layout no longer matches once reconfigured",
			repoPath:    root + "/projects/camp",
			wantProject: "camp",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopeProject, project := resolveScope(root, tc.repoPath, paths)
			if !scopeProject {
				t.Error("scopeProject = false, want true")
			}
			if project != tc.wantProject {
				t.Errorf("project = %q, want %q", project, tc.wantProject)
			}
		})
	}
}

// An unset container path must not match everything, which would key every
// repository to the wrong project name.
func TestResolveScopeEmptyConfiguredPaths(t *testing.T) {
	const root = "/home/u/campaign"

	scopeProject, project := resolveScope(root, root+"/projects/camp", config.CampaignPaths{})
	if !scopeProject {
		t.Error("scopeProject = false, want true")
	}
	if project != "camp" {
		t.Errorf("project = %q, want the trailing segment %q", project, "camp")
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// A malformed glob must fail config resolution rather than silently never
// matching, which would leave the user believing a path is exempt.
func TestApplyGuardsConfigRejectsMalformedAllowGlob(t *testing.T) {
	guards := config.GuardsConfig{Allow: []string{"docs/[a-.pdf"}}
	if _, err := applyGuardsConfig(guards, false, ""); err == nil {
		t.Fatal("applyGuardsConfig() = nil error, want validation error for a bad glob")
	}
}

// A .campaign/ directory with no campaign.yaml is a campaign mid-scaffold.
// Staging into it must still work: camp init and quest scaffolding stage files
// into a campaign they are still building, and the guard is a safety net
// rather than a precondition for camp functioning.
func TestResolveLimitsMissingCampaignConfigFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".campaign"), 0o755); err != nil {
		t.Fatalf("create .campaign: %v", err)
	}

	limits, err := ResolveLimits(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveLimits() error = %v; a campaign mid-scaffold must not fail staging", err)
	}
	if limits.MaxFileSize != DefaultMaxFileSize {
		t.Errorf("MaxFileSize = %d, want the campaign-root default %d", limits.MaxFileSize, DefaultMaxFileSize)
	}
	if limits.LargeFiles != ModeAuto || limits.Bulk != ModeBlock {
		t.Errorf("modes = (%q, %q), want the shipped defaults", limits.LargeFiles, limits.Bulk)
	}
}

// A malformed config is a different case: the user has configuration they
// believe is in effect, so silently substituting defaults could disable a
// guard they deliberately set.
func TestResolveLimitsMalformedCampaignConfigPropagates(t *testing.T) {
	dir := t.TempDir()
	campaignDir := filepath.Join(dir, ".campaign")
	if err := os.MkdirAll(campaignDir, 0o755); err != nil {
		t.Fatalf("create .campaign: %v", err)
	}
	if err := os.WriteFile(filepath.Join(campaignDir, "campaign.yaml"), []byte("\tnot: [valid"), 0o644); err != nil {
		t.Fatalf("write campaign.yaml: %v", err)
	}

	if _, err := ResolveLimits(context.Background(), dir); err == nil {
		t.Fatal("ResolveLimits() = nil error for a malformed campaign.yaml, want an error")
	}
}
