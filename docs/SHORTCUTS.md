# Custom Shortcuts

Camp loads navigation paths and shortcuts from `.campaign/settings/jumps.yaml`.

`camp init` creates that file with the current default paths and built-in shortcuts. If you want to add or override shortcuts for a specific campaign, edit `jumps.yaml`.

## Configuration

Shortcut entries live under the `shortcuts:` key in `.campaign/settings/jumps.yaml`:

```yaml
shortcuts:
  api:
    path: "projects/api-service"
    description: "Jump to the API service"

  build:
    command: "just build"
    description: "Build all projects"

  deploy:
    command: "just deploy staging"
    workdir: "projects/api-service"
    description: "Deploy the API service"
```

For the full scaffolded file shape, initialize a fresh campaign with `camp init` and inspect `.campaign/settings/jumps.yaml` directly.

## Shortcut Types

### Navigation shortcuts

Navigation shortcuts define a relative path from the campaign root:

```yaml
shortcuts:
  api:
    path: "projects/api-service"
    description: "Jump to the API service"
```

Use them with:

```bash
camp go api
cgo api
```

### Command shortcuts

Command shortcuts execute a shell command:

```yaml
shortcuts:
  test:
    command: "just test"
    description: "Run the test suite"
```

Use them with:

```bash
camp run test
```

### Command shortcuts with a working directory

Add `workdir` when the command should run from a specific directory:

```yaml
shortcuts:
  deploy:
    command: "just deploy staging"
    workdir: "projects/api-service"
    description: "Deploy the API service"
```

Use them with:

```bash
camp run deploy
```

## Built-in shortcuts

These defaults are written by `camp init` and can be overridden per campaign:

| Shortcut | Path | Notes |
|----------|------|-------|
| `p` | `projects/` | navigation plus command expansion |
| `f` | `festivals/` | navigation plus command expansion |
| `i` | `.campaign/intents/` | navigation plus command expansion |
| `wt` | `projects/worktrees/` | navigation plus command expansion |
| `w` | `workflow/` | navigation only |
| `ai` | `ai_docs/` | navigation only |
| `d` | `docs/` | navigation only |
| `du` | `dungeon/` | navigation only |
| `cr` | `workflow/code_reviews/` | navigation only |
| `pi` | `workflow/pipelines/` | navigation only |
| `de` | `workflow/design/` | navigation only |
| `ex` | `workflow/explore/` | navigation only |
| `cfg` | no path | command expansion only |

`camp go i` and `cgo i` remain available as operator shortcuts, but the primary
human interface for this state is `camp intent`.

## Listing shortcuts

View the current shortcut set with:

```bash
camp shortcuts
camp sc
```

## Passing arguments to command shortcuts

Additional arguments are appended to the configured command:

```bash
camp run test -- --verbose
camp run deploy -- production
```

## Field Reference

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Relative path used by `camp go` / `cgo` |
| `command` | string | Shell command used by `camp run` |
| `workdir` | string | Optional working directory relative to campaign root |
| `description` | string | Optional help text shown in shortcut listings |
| `concept` | string | Optional command-group expansion target |

## Shortcuts with both path and command

A shortcut can define both `path` and `command`:

```yaml
shortcuts:
  api-dev:
    path: "projects/api-service"
    command: "just dev"
    description: "API development shortcut"
```

- `camp go api-dev` navigates to `projects/api-service`
- `camp run api-dev` runs `just dev`
