# Intent Path And Targeting Upgrade Guide (March 2026)

This note captures the verification evidence and rollout checks for the March
2026 intent storage move in `camp`.

## Scope

The rollout covers two behavior changes:

1. `.campaign/intents/` is now the canonical intent root for new and repaired
   campaigns.
2. Only `camp intent add` gains first-rollout cross-campaign targeting via
   `--campaign`.

Operator note:
- `camp intent` is the normal human interface.
- `camp go i` and `cgo i` remain available as operator shortcuts into the
  underlying state.

## Verification Summary

Validated on March 16, 2026 with:

- `go test ./cmd/camp/... ./internal/complete/... ./internal/config/... ./internal/contract/... ./internal/git/commit ./internal/intent/... ./internal/nav/... ./internal/paths ./internal/quest ./internal/scaffold/... ./internal/workitem/...`
- `go test -count=1 -tags=integration ./tests/integration`
- `go test ./...`
- `go build ./cmd/camp`
- `golangci-lint run --new-from-rev 124df44 ./internal/git/commit ./internal/intent/tui`

## Targeted Regression Coverage

### Config And Canonical Defaults

Representative evidence:
- `internal/config/defaults_concepts_test.go`
  - `TestDefaultConcepts_ExploreIncludedDungeonExcluded`
  - `TestCampaignConfigConcepts_FallbackExcludesDungeonIncludesExplore`
  - `TestDefaultNavigationShortcuts_DungeonIsNavigationOnly`
- `internal/config/settings_test.go`
  - `TestJumpsConfigNormalizeIntentNavigation`
  - `TestLoadJumpsConfig_NormalizesLegacyIntentNavigationWithoutPersisting`
  - `TestSaveJumpsConfig_NormalizesLegacyIntentNavigation`
- `internal/config/campaign_test.go`
  - `TestLoadCampaignConfig_PreservesLegacyShortcutsWithoutExploreAndAddsIntentShortcut`
  - `TestLoadCampaignConfig_LegacyIntentPathWithoutShortcutsPreservesDefaults`

These cover default path values, jumps normalization, stored legacy config
compatibility, and canonical shortcut behavior.

### Intent Service And Audit

Representative evidence:
- `internal/intent/service_test.go`
  - `TestIntentService_EnsureDirectories_CreatesCanonicalLayout`
  - `TestIntentService_EnsureDirectories_MigratesLegacyRootAndAudit`
  - `TestIntentService_EnsureDirectories_ConflictWhenBothRootsContainIntentData`
  - `TestIntentService_PlanLegacyIntentRootMigration`
  - `TestIntentService_PlanLegacyIntentRootCleanup`
- `internal/intent/audit/audit_test.go`
  - `TestFilePath`

These cover canonical directory creation, legacy-state migration, audit-log
relocation, cleanup-only repair behavior, and dual-root conflict detection.

### Navigation, Shortcuts, And Workitem Surfaces

Representative evidence:
- `internal/nav/shortcuts_test.go`
- `internal/nav/standard_paths_test.go`
  - `TestCategoryForStandardPath_LegacyIntentPathNotStandard`
- `internal/complete/category_test.go`
- `internal/complete/complete_test.go`
- `cmd/camp/navigation/go_test.go`
  - `TestFormatConfigShortcuts_ShowsCanonicalIntentPath`
- `internal/intent/tui/completion_test.go`
- `internal/workitem/discover_test.go`
- `internal/workitem/tui/model_test.go`
  - `TestModel_EmptyView`
- `tests/integration/navigation_test.go`
  - `TestGo_CategoryShortcuts`

These cover shortcut normalization, canonical path display, `camp go i`
resolution, completion behavior, discovery scanning, and dashboard empty-state
copy.

### Contracts, Quest Links, Scaffold, And Command Side Effects

Representative evidence:
- `internal/contract/entries_test.go`
  - `TestCampEntries_IntentStatusDirs`
- `internal/quest/link_test.go`
- `internal/scaffold/init_test.go`
  - `TestInit_CreatesCanonicalIntentDirectories`
  - `TestInit_RepairMigratesLegacyIntentState`
- `internal/scaffold/repair_test.go`
  - `TestComputeIntentMigrationChanges_DetectsLegacyIntentRoot`
  - `TestComputeIntentMigrationChanges_DetectsLegacyIntentScaffoldCleanup`
  - `TestComputeIntentMigrationChanges_Conflict`
- `cmd/camp/init_test.go`
  - `TestBuildRepairCommitFiles_IncludesIntentMigrations`
  - `TestBuildRepairCommitMessage_IncludesIntentMigrations`
- `internal/git/commit/commit_test.go`
  - `TestIntent_SelectiveStaging`
- `cmd/camp/intent/add_test.go`
  - `TestIntentAddCampaignResolver_ExplicitCampaignLookup`
  - `TestIntentAddCampaignResolver_BareCampaignUsesPicker`
  - `TestIntentAddCampaignResolver_BareCampaignNonInteractiveFails`
  - `TestIntentAdd_TargetCampaignSmoke`

These cover watcher contract paths, quest link classification, scaffold/init
output, repair planning, staged commit paths, and the first-rollout targeting
rules for `camp intent add`.

### Integration Coverage

Representative evidence:
- `tests/integration/intent_add_target_test.go`
  - `TestIntentAdd_TargetCampaignWritesToSelectedRoot`
- `tests/integration/promote_test.go`
  - promotion from legacy `workflow/intents/` seeds into canonical
    `.campaign/intents/`
- `tests/integration/gather_test.go`
  - feedback gather output lands in `.campaign/intents/inbox/`

These cover the end-to-end target-campaign add path plus migration-compatible
integration flows that still encounter legacy layout on input.

## Operator Smoke Checklist

Use this checklist when validating an upgraded branch or release candidate.

1. Brand-new campaign
   - Run `camp init <dir> --name <name>`.
   - Confirm `.campaign/intents/{inbox,active,ready}` exists.
   - Confirm `camp go i --print` resolves under `.campaign/intents/`.
   - Confirm `workflow/intents/` is not scaffolded as the canonical notebook.
2. Legacy campaign migration
   - Start from a copy that still has `workflow/intents/` state and legacy
     jumps config.
   - Run repair or an intent command that initializes the service.
   - Confirm intent files and `.intents.jsonl` move to `.campaign/intents/`.
   - Confirm legacy scaffold residue is only removed during repair/init
     normalization, not ordinary command execution.
3. Explicit dual-populated conflict
   - Create intent data in both `workflow/intents/` and `.campaign/intents/`.
   - Run repair or initialization.
   - Confirm the command fails with a conflict instead of silently merging both
     roots.
4. Cross-campaign add
   - From campaign A, run `camp intent add --campaign <campaign-b> "Title"`.
   - Confirm the new file is written under campaign B's
     `.campaign/intents/inbox/`.
   - Confirm campaign A does not receive a duplicate inbox item.
   - In non-interactive mode, confirm bare `--campaign` without a value errors
     clearly.

## Compatibility Expectations

Existing campaigns remain supported during rollout:

- legacy jumps data is normalized to canonical intent navigation at load/save
  time
- legacy `workflow/intents/` content migrates into `.campaign/intents/`
- explicit conflict states fail closed instead of auto-merging ambiguous data
- first-rollout cross-campaign behavior remains limited to `camp intent add`
