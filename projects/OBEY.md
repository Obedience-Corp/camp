# Projects

Git submodules and project repositories for this campaign.

## What Goes Here

- Git submodules added via `camp project add`
- Each subdirectory is a complete git repository
- Shared libraries and dependencies

## Structure

```
projects/
├── worktrees/          # Git worktrees for parallel development
├── api-service/        # Backend API
├── frontend/           # Web application
└── shared-libs/        # Common utilities
```

## Usage

```bash
# Add a project
camp project add git@github.com:org/repo.git

# List all projects
camp project list

# Navigate to a project
cgo p project-name
```
