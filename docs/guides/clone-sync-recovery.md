# Recover a partial camp clone

`camp clone` can leave a usable, registered camp even when a required submodule fails. Exit 3 means initialization or validation is incomplete; the output names the path, operation, and original Git cause. Keep that directory and fix the reported cause before retrying. Exit 1 means the root clone itself failed, and exit 2 means the command invocation was invalid. An unresolved machine-specific project link is optional setup and does not cause clone or sync to fail.

From the cloned camp:

```sh
camp status --show-refs
camp sync --dry-run
camp sync projects/failing-module
camp sync
```

The targeted sync retries the named module after its origin, credentials, or pinned object is available. The full sync checks all modules, including children discovered after a parent's selected branch is checked out. Run it again to confirm a stable result. If preflight reports dirty files or unpushed commits in another module, preserve that work and resolve it yourself before full sync; targeted sync can repair an independent path without `--force`. `camp status --show-refs` separates root submodule pointer changes from ordinary file status.

Clone selects each module's declared branch or remote default before discovering nested children. Sync preserves an existing local branch and fast-forwards only when safe. A selected branch can legitimately point beyond the recorded gitlink; the status and validation output report that policy difference. Neither command silently resets a diverged branch or rewrites `.gitmodules` to make validation pass.

`--json` exposes the same result for scripts. Clone reports `submodules.initialized` for verified top-level checkouts, `submodules.succeeded` for complete subtrees, `failed` and `skipped` separately, nested child results, `validation.checkStates`, and optional links. Sync reports ordered `updateResults.results`, counts, validation status, and optional links. Sync exits 1 for preflight or update failures, 3 when updates complete but validation alone fails, and 0 when checks pass. `camp clone --no-submodules` intentionally skips module setup; its validation checks say `skipped`. `--no-validate` skips checks but still returns 3 for a failed required initialization.

## Optional local project links

A broken `projects/<name>` symlink can be normal when a camp moves between machines. Preview possible local checkouts and their origin and `.camp` owner:

```sh
camp project repair obey-voice
camp project repair obey-voice --path /absolute/path/to/obey-voice
```

Select a checkout by path after reviewing its origin. To replace the still-broken link explicitly, run `camp project repair obey-voice --path /absolute/path/to/obey-voice --apply`. Camp leaves the checkout and its existing `.camp` ownership alone. If the symlink is tracked, this changes the camp working tree and is not committed automatically. `camp project repair obey-voice --undo` restores the saved old target. Ordinary clone and sync never prompt for link repair.

## Metadata mismatches

A `.gitmodules` declaration without a Git gitlink is different from an uninitialized submodule. A gitlink without a declaration is also reported. Camp does not infer whether to remove a declaration or restore a gitlink. Inspect repository history and confirm intent before editing either; keep the section name and path distinct when they differ. A missing pinned commit needs its original object made available or an owner-approved new pin, not a blanket force sync.
