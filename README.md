# camp

Campaign management CLI for organizing multi-project AI workspaces.

## Overview

Camp provides structure and navigation for AI-powered development workflows. It creates standardized campaign directories, manages git submodules as projects, and enables lightning-fast navigation through category shortcuts and TUI fuzzy finding.

## Installation

```bash
go install github.com/obediencecorp/camp@latest
```

Or build from source:

```bash
just install
```

## Usage

```bash
# Initialize a new campaign
camp init

# Add a project
camp project add <repo-url>

# List projects
camp project list

# Navigate to campaign root
cgo

# Navigate to projects directory
cgo p

# Navigate to festivals
cgo f
```

## Development

```bash
# Build
just build

# Run tests
just test

# Run with arguments
just run <args>

# Install locally
just install
```

## License

MIT
