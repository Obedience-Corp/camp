# camp promote is the promotion front door

Promotion is the same verb across the three things that move through lifecycles
in a campaign, and `camp promote` is the single discoverable entrypoint over all
of them:

| Kind | Type-specific command | Targets |
|---|---|---|
| intent | `camp intent promote <id> --target <t>` | ready, festival, design |
| workitem | `camp workitem promote <id> --target <t>` | festival, doc, completed, archived, someday |
| festival | `fest promote [--dungeon <s>] [--force]` | next lifecycle stage, or dungeon status |

`camp promote` resolves the item in context (or opens a selector over promotable
intents, workitems, and festivals) and dispatches to the right type-specific
command. Scriptable callers use the type-specific commands directly.

`camp flow` is NOT a promotion command. It is the low-level `.workflow.yaml`
status engine and is hidden from `camp --help`. Its dungeon move
(`internal/dungeon.Service.MoveToDungeonStatus`) is the primitive that the
`camp workitem promote` dungeon targets and the deprecated `camp shelve` alias
build on. Reach for `camp promote`, not `camp flow`.
