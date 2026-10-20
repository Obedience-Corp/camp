# Quest workspace contract (quest-workspace/v1alpha1)

Camp owns the offline workspace layout schema and writer. The desktop app owns
interactive save, focus reload (W2), and in-memory merge. Persist-owner **C**:
Camp push vs app interactive persist share one on-disk document.

## Public commands (dev profile)

| Command | Role |
| --- | --- |
| `camp quest workspace show <quest> [--json]` | Print validated `ui-state.json` |
| `camp quest workspace validate <quest> [--json]` | Schema check; non-zero on invalid |
| `camp quest workspace push <quest> (--file path \| stdin) [--json]` | Append/replace one agent tab |

JSON success payloads use `schema_version: quest-workspace/v1alpha1`. Quest
commands remain behind the `dev` build tag; stable builds do not expose them.

Ordinary Camp install/use never requires the desktop app or `camp-app` plugin.
These commands do not probe app descriptors, sockets, or plugin PATH entries.

## On-disk shape

Path: `.campaign/quests/<quest-dir>/ui-state.json` (beside `quest.yaml`).

Document version **2** (festival-app compatible):

- `version`, `updatedAt`, `activeWorkspaceTabId`
- `workspaceTabs[]` with `id`, `title`, optional `origin`, `paneTree`, …
- Recursive `paneTree`: `pane` or `split` nodes
- Durable webview identity: `content.url` (not `browserTabId`)
- File/workitem paths: camp-relative after push canonicalization

Unknown document/tab keys are preserved across Camp push.

## Legacy root layouts

Documents with a root `paneTree` and no `workspaceTabs` are migrated on
push into user tab `workspace-tab-1` (Festival app default identity) before
the agent tab is appended. Unsupported `version` values above 2 are rejected.

## Push rules

1. Validate the entire tab before any write.
2. Stamp `origin=agent`. Strip runtime-only `browserTabId` / `terminalSessionId`.
3. Canonicalize `filePath` / `workitemPath` / `currentPath` inside the camp root.
4. Append a new ID, or replace only a previously agent-owned ID (idempotent).
5. Refuse user-owned or untagged ID replacement. Never resize/delete user tabs.

## Lock, generation, atomic write

| Concern | Contract |
| --- | --- |
| Lock file | `ui-state.lock` beside `ui-state.json` |
| Lock API | OS exclusive try-lock matching Rust `std::fs::File::try_lock` |
| Wait | 5s bound + `context` cancellation (10ms poll) |
| Generation | SHA-256 hex of exact on-disk UTF-8 bytes |
| Write | Same-directory temp + file fsync + rename + parent-dir fsync (`fsutil.WriteFileAtomically`; dir sync no-op on Windows) |
| CAS | Re-read generation after merge; `PERSISTENCE_CONFLICT` if changed |

Legacy writers that ignore `ui-state.lock` are outside the concurrency
guarantee; Camp still refuses to overwrite a detected mid-merge change.

## Fixtures

Committed under
`internal/quest/workspace/testdata/fixtures/`
(see that directory's README). App interop tasks should consume these paths
verbatim.

## Related

- Design: `workflow/design/agent-composed-quest-workspaces-pushed-20260723-161945/`
- Packaging: `workflow/design/festival-app-interface-cli/05-architecture-and-packaging.md`
- Issue: camp#581 / WI-8944b3
