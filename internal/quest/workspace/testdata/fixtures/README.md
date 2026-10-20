# Quest workspace fixtures (quest-workspace/v1alpha1)

Shared on-disk / push-spec fixtures for Camp offline composition and app
interop. Document version remains festival-app **2** (`workspaceTabs` +
`paneTree`). Public CLI schema version is `quest-workspace/v1alpha1`.

| File | Purpose |
| --- | --- |
| `valid-layout.json` | Valid version-2 document with a user file pane |
| `invalid-tree.json` | Malformed split (bad direction / child count) |
| `agent-metadata.json` | Agent tab with durable webview `url` and unknown metadata |
| `push-spec-valid.json` | Valid push stdin/--file spec (strips `browserTabId`) |
| `push-spec-out-of-bounds.json` | Path escapes camp root |
| `conflicting-ownership.json` | User / untagged tabs that push must not replace |
| `concurrent-additions-base.json` | Baseline for two independent agent appends |

## Generation and lock (coordinator note)

- Lock file: `ui-state.lock` beside `ui-state.json`
- Lock semantics: OS exclusive try-lock matching Rust `std::fs::File::try_lock`
  (Unix `flock` LOCK_EX|LOCK_NB; Windows `LockFileEx` fail-immediately), 5s
  bounded wait, context cancellation
- Generation: SHA-256 hex of the **exact UTF-8 bytes** on disk (Camp writes
  pretty-printed JSON with two-space indent via `encoding/json.MarshalIndent`)
- App worker durable fields / W2: consume these fixtures; leave any remaining
  pretty-print key-order differences between Go and Rust documented on the app
  side if generation strings must match across producers for the same logical
  document

## Legacy writers

Writers that do not hold `ui-state.lock` are outside the concurrency guarantee.
Camp detects a mid-merge byte change and returns `PERSISTENCE_CONFLICT` rather
than overwriting.
