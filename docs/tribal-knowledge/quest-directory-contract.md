# Quest Directory Contract

A directory under `.campaign/quests/` (and under `.campaign/quests/dungeon/<status>/`)
is a quest **if and only if** it contains a `quest.yaml` file.

`quest.List` classifies each directory into one of three cases:

| Case | Condition | Behavior |
| --- | --- | --- |
| valid | `quest.yaml` present and parseable | included in the listing |
| stray | no `quest.yaml` | skipped silently |
| malformed | `quest.yaml` present but unparseable | skipped with a `warning:` line on stderr |

## Why stray directories exist

festival-app writes per-quest UI state (for example `ui-state.json`) into the
quest directory, so a directory such as `.campaign/quests/20260608-test/` can
exist with UI state but no `quest.yaml`. camp must tolerate these directories
without emitting error-level noise; a missing `quest.yaml` is a normal,
expected state, not a fault.

Warnings are reserved for the malformed case, where a `quest.yaml` exists but
cannot be parsed, because that signals real corruption a user should see.

## Follow-up (not camp's responsibility)

festival-app should avoid creating quest directories that never receive a
`quest.yaml`. Regardless of whether it does, camp tolerates them per the
contract above.
