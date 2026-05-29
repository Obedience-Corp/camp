# Documentation

Human-authored documentation and specifications.

## What Goes Here

- Technical specifications
- Architecture documents
- API documentation
- User guides
- Decision records

## Structure

```
docs/
├── architecture/       # System design documents
├── api/               # API specifications
├── guides/            # How-to guides
└── adr/               # Architecture Decision Records
```

## Guidelines

- Use Markdown for all documentation
- Keep documentation close to the code it describes
- Update docs when code changes

## Narrative References

Hand-authored references that explain concepts beyond the per-command
auto-generated CLI reference.

- [campaign-directory-reference.md](campaign-directory-reference.md)
- [campaign-settings-files.md](campaign-settings-files.md)
- [workflow-reference.md](workflow-reference.md) — custom workflow surface
  (`camp workflow create/list/show/doctor/sync` and `shortcut add`).
- [workitem-link-reference.md](workitem-link-reference.md) — link registry,
  `lnk_*` IDs, resolver tiers, broken-link recovery.
- [workitem-commit-reference.md](workitem-commit-reference.md) — scoped
  workitem commits, staging matrix, refusal mode, cross-repo query, tag
  grammar.
- [SHORTCUTS.md](SHORTCUTS.md)
- [shell-integration.md](shell-integration.md)
- [leverage-score.md](leverage-score.md)

## Migrations

Operator-facing rollout notes for behavior changes that affect existing
campaigns.

- [migrations/dungeon-behavior-upgrade-2026-03.md](migrations/dungeon-behavior-upgrade-2026-03.md)
- [migrations/intent-path-targeting-upgrade-2026-03.md](migrations/intent-path-targeting-upgrade-2026-03.md)
- [migrations/workitem-v1alpha6-2026-05.md](migrations/workitem-v1alpha6-2026-05.md) —
  `.workitem` schema bump (added `ref` and `quest_id`).
