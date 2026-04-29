# Dungeon Behavior Upgrade Guide (March 2026)

This note covers rollout guidance for the dungeon behavior updates delivered in `camp` during March 2026.

## Scope

The rollout includes three behavior groups:

1. Nearest-dungeon context resolution for dungeon commands.
2. Campaign-root docs routing for dungeon triage (CLI and crawl TUI).
3. Concept/scaffold default alignment around `workflow/explore`.

## What Changed

### 1. Nearest Dungeon Resolution

`camp dungeon list`, `camp dungeon move`, and `camp dungeon crawl` now resolve context by walking from the current working directory up to campaign root and selecting the nearest `dungeon/`.

Operational impact:
- Running from nested paths now targets the nearest local dungeon context instead of assuming root dungeon.
- Commands fail with actionable guidance when no dungeon context exists.

### 2. Docs Routing in Triage

Triage now supports routing items into campaign-root docs destinations.

CLI contract:
- `camp dungeon move <item> --triage --to-docs <subdir>`

Rules:
- Destination is validated and resolved under campaign-root `docs/`.
- Destination must be an existing docs subdirectory (the flow does not create new docs paths).
- Traversal/escape destinations are rejected.
- `--to-docs` requires `--triage` and cannot be combined with status argument.

TUI contract:
- Triage crawl includes a `Route to docs` action.
- Destination picker is constrained to docs subdirectories (with suggestions from existing docs dirs).

### 3. Concept + Scaffold Defaults

Default concept and scaffold behavior now treats dungeon as dynamic workflow behavior rather than a default static concept entry.

Changes:
- Default concepts add `workflow/explore` and drop default `dungeon` concept entry.
- New scaffolds include `workflow/explore/OBEY.md` guidance.
- Default navigation shortcuts add `ex -> workflow/explore/`.

## Compatibility Expectations

Existing campaigns are preserved:
- Explicit legacy concept lists (including `dungeon`) continue to load and are not overwritten.
- Legacy shortcut sets in `jumps.yaml` are preserved; new defaults are not force-written into existing custom shortcut maps.
- Repair flow preserves existing explicit concept lists.

## Operator Rollout Checklist

Use this checklist when upgrading active campaign workspaces.

1. Validate nearest-context command behavior.
   - From nested directory with local dungeon, run:
     - `camp dungeon list`
     - `camp dungeon move <item> --triage`
   - Confirm operations target nearest dungeon context.
   - Confirm successful moves auto-commit; dungeon triage history depends on
     those commits.
2. Validate docs routing behavior.
   - Run `camp dungeon move <item> --triage --to-docs <subdir>`.
   - Confirm destination path is under campaign-root `docs/`.
   - Confirm traversal attempts fail with clear errors.
   - Confirm successful docs routing auto-commits.
3. Validate scaffold defaults on a scratch campaign.
   - Initialize new campaign.
   - Confirm `workflow/explore/` and `workflow/explore/OBEY.md` exist.
   - Confirm `camp go ex --print` resolves to `workflow/explore`.
4. Validate compatibility on an existing campaign copy.
   - Load campaign with legacy explicit `dungeon` concept entry.
   - Confirm concept list persists unchanged.
   - Confirm existing `jumps.yaml` shortcuts remain intact.

## Rollback / Fallback Expectations

If rollout issues are detected:

1. Keep explicit concept configuration in campaign config for legacy workflows.
   - Existing explicit concept lists (including `dungeon`) remain supported.
2. Keep or restore prior shortcut mappings in `.campaign/settings/jumps.yaml`.
   - New `ex` shortcut can be removed in campaign-local config if not desired.
3. Use standard status-based triage moves if docs routing is temporarily deferred.
   - `camp dungeon move <item> <status> --triage`

## Reference Links

CLI reference:
- `docs/cli-reference/camp_dungeon.md`
- `docs/cli-reference/camp_dungeon_list.md`
- `docs/cli-reference/camp_dungeon_move.md`
- `docs/cli-reference/camp_dungeon_crawl.md`

Representative test evidence:
- `tests/integration/dungeon_test.go`
  - `TestDungeonList_UsesNearestContextFromNestedDir`
  - `TestDungeonMove_TriageToDocsDestination`
  - `TestDungeonMove_TriageToDocsFromNestedDirAnchorsToCampaignRootDocs`
- `tests/integration/init_test.go`
  - `TestInit_WorkflowExploreScaffoldAndShortcut`
- `internal/config/campaign_test.go`
  - `TestLoadCampaignConfig_PreservesLegacyDungeonConceptList`
  - `TestLoadCampaignConfig_PreservesLegacyShortcutsWithoutExplore`
