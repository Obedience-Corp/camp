# Custom Shortcuts

Camp supports custom shortcuts defined in `.campaign/campaign.yaml` for both navigation and command execution.

## Overview

Shortcuts allow you to define quick access to frequently-used directories and commands within your campaign. There are two types of shortcuts:

1. **Navigation shortcuts** - Jump to specific directories
2. **Command shortcuts** - Execute commands from specific directories

## Configuration

Add shortcuts to your `.campaign/campaign.yaml` file:

```yaml
shortcuts:
  # Navigation shortcut
  api:
    path: "projects/api-service"
    description: "Jump to API service"

  # Command shortcut
  build:
    command: "just build"
    description: "Build all projects"

  # Command with custom working directory
  dev:
    command: "docker compose up -d"
    workdir: "dev/infrastructure"
    description: "Start dev environment"
```

## Shortcut Types

### Navigation Shortcuts

Navigation shortcuts define paths relative to the campaign root:

```yaml
shortcuts:
  api:
    path: "projects/api-service"
    description: "Jump to API service project"

  infra:
    path: "dev/infrastructure"
    description: "Infrastructure code"
```

Usage:
```bash
camp go api        # Navigate to projects/api-service
camp go infra      # Navigate to dev/infrastructure
```

### Command Shortcuts

Command shortcuts execute shell commands:

```yaml
shortcuts:
  build:
    command: "just build"
    description: "Build all projects"

  test:
    command: "just test"
    description: "Run tests"
```

Usage:
```bash
camp run build     # Execute "just build" from campaign root
camp run test      # Execute "just test" from campaign root
```

### Command Shortcuts with Working Directory

Specify a `workdir` to run commands from a specific directory:

```yaml
shortcuts:
  dev:
    command: "docker compose up -d"
    workdir: "dev/infrastructure"
    description: "Start dev environment"

  deploy:
    command: "just deploy staging"
    workdir: "projects/api-service"
    description: "Deploy API to staging"
```

Usage:
```bash
camp run dev       # Runs docker compose from dev/infrastructure
camp run deploy    # Runs just deploy from projects/api-service
```

## Listing Shortcuts

View all available shortcuts:

```bash
camp shortcuts     # List all shortcuts
camp sc            # Alias for shortcuts
```

Output example:
```
Built-in Navigation Shortcuts:

  a    -> ai_docs/
  c    -> corpus/
  d    -> docs/
  f    -> festivals/
  p    -> projects/
  pi   -> pipelines/
  cr   -> code_reviews/
  wt   -> worktrees/

Custom Shortcuts:

  Navigation (use with: camp go <shortcut>):
    api        -> projects/api-service        # Jump to API service
    infra      -> dev/infrastructure          # Infrastructure code

  Commands (use with: camp run <shortcut>):
    build      -> just build                  # Build all projects
    dev        -> [dev/infrastructure] docker compose up -d  # Start dev environment
    deploy     -> [projects/api-service] just deploy staging  # Deploy API to staging
```

## Examples

See `examples/shortcuts-campaign.yaml` for a complete example configuration.

## Built-in vs Custom Shortcuts

- **Built-in shortcuts** (p, f, c, etc.) are always available for standard campaign directories
- **Custom navigation shortcuts** can override built-in shortcuts or add new ones
- **Custom shortcuts** work alongside built-in shortcuts - they don't replace them

## Shell Integration

Navigation shortcuts work with the `cgo` shell function:

```bash
cgo api            # Navigate to API service
cgo infra          # Navigate to infrastructure
```

## Passing Arguments to Command Shortcuts

You can pass additional arguments to command shortcuts:

```bash
camp run test -- --verbose          # Pass --verbose to test command
camp run deploy -- production       # Pass production to deploy command
```

Arguments are appended to the configured command.

## Field Reference

### ShortcutConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | For navigation | Relative path from campaign root |
| `command` | string | For commands | Shell command to execute |
| `workdir` | string | Optional | Working directory for command (relative to campaign root) |
| `description` | string | Optional | Help text for this shortcut |

## Advanced: Shortcuts with Both Path and Command

A shortcut can have both `path` and `command` defined. In this case:
- It's treated as a navigation shortcut when used with `camp go`
- It's treated as a command shortcut when used with `camp run`

```yaml
shortcuts:
  api-dev:
    path: "projects/api-service"
    command: "just dev"
    description: "API service development"
```

Usage:
```bash
camp go api-dev    # Navigate to projects/api-service
camp run api-dev   # Execute "just dev" from campaign root
```

## Best Practices

1. **Use descriptive names** - Choose shortcut names that clearly indicate their purpose
2. **Add descriptions** - Help text makes shortcuts self-documenting
3. **Group related shortcuts** - Keep navigation and command shortcuts organized
4. **Use workdir when needed** - Specify working directory for commands that need to run from specific locations
5. **Keep it simple** - Shortcuts should be quick to type and remember
6. **Document complex commands** - Use descriptions to explain what complex commands do
