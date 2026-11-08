# Leverage reports for a Git repository

With Camp, Git and `scc` installed, point Camp at a local working tree. No camp initialization or Festival setup is needed:

```sh
camp leverage /path/to/repo
camp leverage /path/to/repo --author alice@example.com
camp leverage /path/to/repo --by-author
camp leverage /path/to/repo --by-author --author-threshold 0 --json
camp leverage /path/to/repo --people 2
```

`--dir /path/to/repo` is equivalent to the positional path. Inside a repository outside a camp, `camp leverage` defaults to the current directory. Inside a camp, use `.` to select the current directory instead of the camp's projects. An explicit target always defaults to the repository summary, even if a camp has a default personal author.

All explicit-directory reports and camp `--by-author` reports are read-only: they do not create configuration, snapshots, caches or commits. `--no-commit` remains accepted but is unnecessary for these reports. Ordinary no-path camp aggregate/personal commands retain their existing automatic persistence behavior.

## Scope and ownership

Camp uses the per-file output from the same `scc` scan that supplies the COCOMO estimate. Its `.sccignore`, `.gitignore`, `.ignore`, language filters and Camp directory exclusions therefore apply to contributor ownership too. For example, a `.sccignore` containing `.repos/` excludes an imported repository tree from both counting and blame. scc runs from the selected target directory so configuration is not accidentally selected from the invoking shell's directory.

A dirty checkout is supported. Retained committed lines belong to their Git author; new or modified uncommitted lines are accounted for separately. Untracked source is counted as uncommitted. Bots/excluded identities and files that cannot be attributed have separate accounting buckets. No unknown share is redistributed to humans.

The default contributor table shows identities owning at least 1% of counted physical lines. `--author-threshold 0` shows every attributable owner. It never includes historical contributors with no surviving lines. Changing the display threshold does not change allocations or effort. Personal selection supports existing author-email substring and configured alias matching; JSON lists all matched identities.

## Interpreting scores

The source estimate is `scc`'s estimated people multiplied by estimated schedule months. An author's allocation is that estimate multiplied by their share of surviving physical lines, including comments and blanks. Contributor leverage divides the allocation by the span between their first and last committer dates across local Git refs, with a 0.1-month minimum. Ignore rules limit source ownership, not that repository-wide historical span. Repository summary uses the existing blame-weighted effort convention; `--people` explicitly replaces it with team size multiplied by the repository's commit span.

These are model-based estimates, not actual hours, AI-use detection, work quality or causal productivity improvements. Uncommitted output remains part of the tree estimate but receives no individual score. Use the accounting buckets to interpret the summary. Contributor ratios must not be summed.

Git's mailmap normalization and existing configured aliases are respected; identical display names alone do not merge identities. Unknown history is not replaced with an invented one-commit span. Shallow history makes affected effort/ratios unavailable.

## Export and diagnostics

`--json` supports every view. Contributor reports use `leverage-contributors/v1`; standalone summary/personal reports use `leverage-repository/v1` and preserve the `campaign`/`projects` score keys with added scope metadata. Unknown effort or leverage is null. JSON records revisions, tool versions, file/content fingerprints, accounting and diagnostics. `--verbose` prints scope/config details on stderr.

A complete or empty report exits 0. A partial report is still written but exits 2, with unavailable attribution or history identified. Fatal scan/target failures do not produce a successful report. Progress and diagnostics use stderr. Export to a path outside the analyzed tree to avoid including the report in its own scan.

Input reads are best effort, not an atomic filesystem snapshot. Detected concurrent source, ref or configuration changes abort with a rerun message. Existing dirtiness is allowed. The report includes author identities and local paths; review them before sharing.

The legacy camp snapshot/history format is unchanged. New by-author reports do not update those historical records. Use the existing aggregate/snapshot commands when historical capture is intended.
