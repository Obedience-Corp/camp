package config

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

// ErrNotInCampaign reports that no campaign root exists at or above a path.
// Re-exported so callers resolving campaign-scoped config can distinguish
// "there is no campaign here" (a supported state with shipped defaults) from a
// filesystem or cancellation failure, without importing internal/campaign.
var ErrNotInCampaign = campaign.ErrNotInCampaign

// DetectCampaignRoot walks up from startDir and returns the directory holding
// .campaign/. It returns ErrNotInCampaign when there is none.
func DetectCampaignRoot(ctx context.Context, startDir string) (string, error) {
	return campaign.Detect(ctx, startDir)
}

// CommitConfig holds campaign-scoped commit behavior from campaign.yaml. It is
// distinct from CommitPrefs, which lives in machine-global config.json and
// campaign-local settings/local.json and governs ref syncing and subject tags.
type CommitConfig struct {
	// Guards configures the staging guards applied at camp's stage chokepoint.
	Guards GuardsConfig `yaml:"guards,omitempty"`
}

// GuardsConfig configures the size and bulk staging guards. Empty fields take
// the shipped defaults; see the stageguard package for their values.
type GuardsConfig struct {
	// LargeFiles selects the per-file guard mode: "auto" (declare an artifact
	// root, exclude, report), "block" (refuse the commit), or "off".
	LargeFiles string `yaml:"large_files,omitempty"`
	// Bulk selects the bulk guard mode: "block" or "off". There is deliberately
	// no "auto" -- camp cannot infer whether a large untracked tree wants
	// gitignore or an artifact root.
	Bulk string `yaml:"bulk,omitempty"`
	// Allow lists globs exempt from every guard, for the legitimate large file
	// committed once.
	Allow []string `yaml:"allow,omitempty"`
	// MaxFileSize is the campaign-root per-file threshold ("10MiB").
	MaxFileSize string `yaml:"max_file_size,omitempty"`
	// MaxProjectFileSize is the per-file threshold applied inside a project
	// submodule ("25MiB"). It is higher than MaxFileSize because the project
	// outcome is a stop rather than a silent fix, and stops must stay rare.
	MaxProjectFileSize string `yaml:"max_project_file_size,omitempty"`
	// MaxAddedFiles is the bulk trigger: untracked files under one dominant
	// prefix. There is no total-bytes leg.
	MaxAddedFiles int `yaml:"max_added_files,omitempty"`
	// Projects holds per-project overrides keyed by project name.
	Projects map[string]GuardsProjectConfig `yaml:"projects,omitempty"`
}

// GuardsProjectConfig holds per-project guard overrides. Pointer types
// distinguish "not set" from "set to the zero value", matching the
// FreshProjectConfig precedent.
type GuardsProjectConfig struct {
	LargeFiles    *string `yaml:"large_files,omitempty"`
	Bulk          *string `yaml:"bulk,omitempty"`
	MaxFileSize   *string `yaml:"max_file_size,omitempty"`
	MaxAddedFiles *int    `yaml:"max_added_files,omitempty"`
	// Allow, when non-nil (including an explicit empty list), entirely replaces
	// the campaign-wide Allow list for this project rather than extending it,
	// following the FollowUp override precedent.
	Allow []string `yaml:"allow,omitempty"`
}

// ResolveGuardMode returns the large_files mode for projectName, preferring a
// project override over the campaign-wide value. An empty projectName resolves
// campaign-root scope. The zero value "" means "unset"; the caller applies the
// shipped default.
func (g GuardsConfig) ResolveGuardMode(projectName string) string {
	if pc, ok := g.Projects[projectName]; ok && pc.LargeFiles != nil {
		return *pc.LargeFiles
	}
	return g.LargeFiles
}

// ResolveBulkMode returns the bulk mode for projectName, preferring a project
// override over the campaign-wide value.
func (g GuardsConfig) ResolveBulkMode(projectName string) string {
	if pc, ok := g.Projects[projectName]; ok && pc.Bulk != nil {
		return *pc.Bulk
	}
	return g.Bulk
}

// ResolveMaxFileSize returns the per-file threshold string for projectName.
// Inside a project the priority is project override, then the campaign-wide
// project default, then "". At the campaign root the project legs do not apply.
func (g GuardsConfig) ResolveMaxFileSize(projectName string) string {
	if projectName == "" {
		return g.MaxFileSize
	}
	if pc, ok := g.Projects[projectName]; ok && pc.MaxFileSize != nil {
		return *pc.MaxFileSize
	}
	return g.MaxProjectFileSize
}

// ResolveMaxAddedFiles returns the bulk trigger count for projectName,
// preferring a project override. Zero means "unset".
func (g GuardsConfig) ResolveMaxAddedFiles(projectName string) int {
	if pc, ok := g.Projects[projectName]; ok && pc.MaxAddedFiles != nil {
		return *pc.MaxAddedFiles
	}
	return g.MaxAddedFiles
}

// ResolveAllow returns the allowlist globs for projectName. A non-nil project
// list replaces the campaign-wide list entirely.
func (g GuardsConfig) ResolveAllow(projectName string) []string {
	if pc, ok := g.Projects[projectName]; ok && pc.Allow != nil {
		return pc.Allow
	}
	return g.Allow
}
