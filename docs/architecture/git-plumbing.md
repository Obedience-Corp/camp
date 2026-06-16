# Git Plumbing

Camp git helpers should prefer `internal/git.RunGitCmd`, `internal/git.Output`, or the package-local `gitCmd` helper when adding new git execution paths. These helpers centralize repository `-C` handling, deterministic `LC_ALL=C`/`LANG=C` output, and shared error classification so callers do not quietly diverge on lock, locale, and porcelain parsing behavior.

The current codebase still has a direct git-exec long tail: 176 non-test `exec.CommandContext(..., "git", ...)` call sites across 46 files at the time this note was added. This task only migrated the call sites touched by the sequence-11 hardening fixes. Future plumbing cleanup should convert that long tail incrementally by subsystem, with focused tests around each command family's stderr handling and output parsing.
