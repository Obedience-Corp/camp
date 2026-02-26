// Package paths provides centralized path resolution for campaign directories.
// This ensures consistent path handling across the codebase and makes
// directory structure changes easier to manage.
package paths

import (
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
)

// Resolver provides methods to resolve full paths for campaign directories.
// It combines the campaign root with configured relative paths.
type Resolver struct {
	root  string
	paths config.CampaignPaths
}

// NewResolver creates a new path resolver for the given campaign root and paths config.
func NewResolver(campaignRoot string, paths config.CampaignPaths) *Resolver {
	return &Resolver{
		root:  campaignRoot,
		paths: paths,
	}
}

// NewResolverFromConfig creates a resolver from a CampaignConfig.
func NewResolverFromConfig(campaignRoot string, cfg *config.CampaignConfig) *Resolver {
	return NewResolver(campaignRoot, cfg.Paths())
}

// Root returns the campaign root directory.
func (r *Resolver) Root() string {
	return r.root
}

// Config returns the underlying CampaignPaths configuration.
func (r *Resolver) Config() config.CampaignPaths {
	return r.paths
}

// Projects returns the full path to the projects directory.
func (r *Resolver) Projects() string {
	return filepath.Join(r.root, r.paths.Projects)
}

// Worktrees returns the full path to the worktrees directory.
func (r *Resolver) Worktrees() string {
	return filepath.Join(r.root, r.paths.Worktrees)
}

// Festivals returns the full path to the festivals directory.
func (r *Resolver) Festivals() string {
	return filepath.Join(r.root, r.paths.Festivals)
}

// AIDocs returns the full path to the AI docs directory.
func (r *Resolver) AIDocs() string {
	return filepath.Join(r.root, r.paths.AIDocs)
}

// Docs returns the full path to the docs directory.
func (r *Resolver) Docs() string {
	return filepath.Join(r.root, r.paths.Docs)
}

// Workflow returns the full path to the workflow directory.
func (r *Resolver) Workflow() string {
	return filepath.Join(r.root, r.paths.Workflow)
}

// Intents returns the full path to the intents directory.
func (r *Resolver) Intents() string {
	return filepath.Join(r.root, r.paths.Intents)
}

// CodeReviews returns the full path to the code reviews directory.
func (r *Resolver) CodeReviews() string {
	return filepath.Join(r.root, r.paths.CodeReviews)
}

// Pipelines returns the full path to the pipelines directory.
func (r *Resolver) Pipelines() string {
	return filepath.Join(r.root, r.paths.Pipelines)
}

// Design returns the full path to the design directory.
func (r *Resolver) Design() string {
	return filepath.Join(r.root, r.paths.Design)
}

// Dungeon returns the full path to the dungeon directory.
func (r *Resolver) Dungeon() string {
	return filepath.Join(r.root, r.paths.Dungeon)
}

// Project returns the full path to a specific project by name.
func (r *Resolver) Project(name string) string {
	return filepath.Join(r.Projects(), name)
}

// RelativeProjects returns the relative path to projects (for config/display).
func (r *Resolver) RelativeProjects() string {
	return r.paths.Projects
}

// RelativeIntents returns the relative path to intents (for config/display).
func (r *Resolver) RelativeIntents() string {
	return r.paths.Intents
}

// RelativeFestivals returns the relative path to festivals (for config/display).
func (r *Resolver) RelativeFestivals() string {
	return r.paths.Festivals
}

// RelativeWorkflow returns the relative path to workflow (for config/display).
func (r *Resolver) RelativeWorkflow() string {
	return r.paths.Workflow
}

// RelativeDungeon returns the relative path to dungeon (for config/display).
func (r *Resolver) RelativeDungeon() string {
	return r.paths.Dungeon
}
