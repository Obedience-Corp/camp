# Workflow Sub-Concepts Upgrade Guide (June 2026)

This note covers the concept-menu and `campaign.yaml` changes delivered in
`camp` during June 2026, where workflows become sub-concepts of a single
`workflow` parent.

## What Changed

### 1. The concept picker is now a small tree

The intent concept picker previously listed every workflow collection as its own
top-level entry (`festivals`, `design`, `explore`, `code_reviews`, `pipelines`,
plus `worktrees`, `intents`, and any custom workflow). It now shows three
top-level concepts:

```
projects
workflow
  festivals
  design
  explore
  code_reviews
  pipelines
docs
```

Selecting `workflow` opens its sub-concepts. Each sub-concept stores the same
path-based `Intent.Concept` value as before (`festivals/`, `workflow/design/`,
and so on), so existing intents need no migration. `worktrees` and `dungeon` are
no longer picker concepts (worktrees is a projects detail, and dungeon is
resolved dynamically).

### 2. `camp workflow create` nests new workflows

`camp workflow create <type>` now registers the new collection as a child of the
`workflow` concept instead of a flat top-level entry. The new workflow appears in
the picker submenu automatically, and `camp workflow list`, `show`, `doctor`, and
`sync` continue to work against the nested tree.

### 3. The config gained a recursive `children` field

`ConceptEntry` now has an optional `children` list. A concept without `children`
behaves exactly as before, so configs that never nest are unaffected.

## Upgrading an Existing Campaign

Run the repair command from the campaign root:

```
camp init --repair
```

The migration is non-destructive and idempotent:

- Built-in workflow-family concepts (`festivals`, `design`, `explore`,
  `code_reviews`, `pipelines`) and any `workflow/`-prefixed concept move under a
  single `workflow` parent.
- The default `worktrees` entry is dropped from the picker (navigation shortcuts
  are unaffected).
- `projects`, `docs`, and any custom or unknown top-level concept are preserved.
- Custom workflows already nested under `workflow` are left in place.
- Running `--repair` again makes no further changes.

## Customizing Concepts

Concepts live in `.campaign/campaign.yaml` under the `concepts:` block. To add or
reorder workflow collections, edit the `workflow` concept's `children:` list:

```yaml
concepts:
  - name: workflow
    path: workflow/
    description: Workflows
    children:
      - name: festivals
        path: festivals/
      - name: design
        path: workflow/design/
      # add custom workflows here, or use `camp workflow create`
```

`camp concepts` prints the current tree and a customization hint. See the
campaign config reference for the full schema.
