package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ObeyContent contains OBEY.md templates for each directory.
var ObeyContent = map[string]string{
	"projects": `# Projects

Git submodules and project repositories for this campaign.

## What Goes Here

- Git submodules added via ` + "`camp project add`" + `
- Each subdirectory is a complete git repository
- Shared libraries and dependencies

## Usage

` + "```bash" + `
# Add a project
camp project add git@github.com:org/repo.git

# List all projects
camp project list

# Navigate to a project
camp go project-name
` + "```" + `

## Structure

` + "```" + `
projects/
├── api-service/        # Backend API
├── frontend/           # Web application
└── shared-libs/        # Common utilities
` + "```" + `
`,
	"worktrees": `# Worktrees

Git worktrees for parallel development across projects.

## What Goes Here

- Git worktrees for feature branches
- Organized by project name, then branch name
- Created automatically via camp worktree commands

## Structure

` + "```" + `
worktrees/
├── api-service/
│   ├── feature-auth/
│   └── bugfix-logging/
└── frontend/
    └── redesign-2024/
` + "```" + `

## Usage

` + "```bash" + `
# Create a worktree for a project branch
camp worktree create api-service feature-auth

# List all worktrees
camp worktree list

# Remove a worktree
camp worktree remove api-service feature-auth
` + "```" + `
`,
	"ai_docs": `# AI Documentation

AI-generated documentation and research materials.

## What Goes Here

- AI-generated analysis and summaries
- Research notes from AI assistants
- Automated documentation outputs
- AI conversation exports

## Structure

` + "```" + `
ai_docs/
├── analysis/           # Code analysis reports
├── research/           # Research summaries
├── conversations/      # Notable AI conversations
└── generated/          # Auto-generated docs
` + "```" + `

## Guidelines

- Keep AI outputs organized by topic
- Date-stamp major documents
- Review and curate periodically
`,
	"docs": `# Documentation

Human-authored documentation and specifications.

## What Goes Here

- Technical specifications
- Architecture documents
- API documentation
- User guides
- Decision records

## Structure

` + "```" + `
docs/
├── architecture/       # System design documents
├── api/               # API specifications
├── guides/            # How-to guides
└── adr/               # Architecture Decision Records
` + "```" + `

## Guidelines

- Use Markdown for all documentation
- Keep documentation close to the code it describes
- Update docs when code changes
`,
	"corpus": `# Corpus

Reference materials and knowledge base documents.

## What Goes Here

- Reference documentation from external sources
- Code examples and snippets
- Research papers and articles
- Training data for AI context

## Structure

` + "```" + `
corpus/
├── references/         # External documentation
├── examples/          # Code examples
├── papers/            # Research papers
└── context/           # AI context documents
` + "```" + `

## Guidelines

- Organize by topic or source
- Include citations and sources
- Keep up-to-date with latest versions
`,
	"pipelines": `# Pipelines

CI/CD pipeline definitions and automation scripts.

## What Goes Here

- GitHub Actions workflows
- GitLab CI configurations
- Docker build files
- Deployment scripts
- Automation utilities

## Structure

` + "```" + `
pipelines/
├── github/            # GitHub Actions workflows
├── docker/            # Dockerfiles and compose
├── scripts/           # Automation scripts
└── terraform/         # Infrastructure as code
` + "```" + `

## Guidelines

- Keep pipelines versioned with the code
- Document required secrets and environment variables
- Test pipelines in isolated environments
`,
	"code_reviews": `# Code Reviews

Code review notes and feedback documents.

## What Goes Here

- Code review feedback
- Review checklists
- Quality gate reports
- Security audit findings

## Structure

` + "```" + `
code_reviews/
├── pending/           # Reviews in progress
├── completed/         # Finished reviews
└── templates/         # Review templates
` + "```" + `

## Guidelines

- Link reviews to PRs/issues
- Track action items from reviews
- Archive completed reviews periodically
`,
}

// CreateObeyFiles generates OBEY.md files for each directory.
func CreateObeyFiles(ctx context.Context, dir string, minimal bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	dirs := StandardDirs
	if minimal {
		dirs = MinimalDirs
	}

	for _, d := range dirs {
		// Skip .campaign directory - it doesn't get an OBEY.md
		if d == ".campaign" {
			continue
		}

		content, ok := ObeyContent[d]
		if !ok {
			continue
		}

		obeyPath := filepath.Join(dir, d, "OBEY.md")

		// Skip if OBEY.md already exists
		if _, err := os.Stat(obeyPath); err == nil {
			continue
		}

		if err := os.WriteFile(obeyPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create OBEY.md in %s: %w", d, err)
		}
	}

	return nil
}

// CreateClaudeMD creates the CLAUDE.md template at the campaign root.
func CreateClaudeMD(ctx context.Context, dir string, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	content := fmt.Sprintf(`# CLAUDE.md

## Campaign: %s

This is the AI agent instruction file for the %s campaign.

## Overview

<!-- Describe what this campaign is about -->

## Projects

<!-- List and describe your projects -->

## Development Guidelines

<!-- Add coding standards, patterns, etc. -->

## Directory Structure

| Directory | Purpose |
|-----------|---------|
| projects/ | Git submodules and project repositories |
| worktrees/ | Git worktrees for parallel development |
| ai_docs/ | AI-generated documentation |
| docs/ | Human-authored documentation |
| corpus/ | Reference materials and knowledge base |
| pipelines/ | CI/CD and automation |
| code_reviews/ | Review notes and feedback |

## AI Instructions

<!-- Add specific instructions for AI agents -->

---

> See individual directory OBEY.md files for detailed usage information.
`, name, name)

	claudePath := filepath.Join(dir, "CLAUDE.md")

	// Skip if CLAUDE.md already exists
	if _, err := os.Stat(claudePath); err == nil {
		return nil
	}

	return os.WriteFile(claudePath, []byte(content), 0644)
}

// CreateAgentsMDSymlink creates AGENTS.md as a symlink to CLAUDE.md.
func CreateAgentsMDSymlink(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	agentsPath := filepath.Join(dir, "AGENTS.md")

	// Skip if AGENTS.md already exists
	if _, err := os.Lstat(agentsPath); err == nil {
		return nil
	}

	// Create symlink to CLAUDE.md (relative path)
	return os.Symlink("CLAUDE.md", agentsPath)
}
