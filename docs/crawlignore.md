# .crawlignore

A `.crawlignore` file controls which files and directories are excluded from the dungeon triage crawl. It uses gitignore-style pattern syntax.

## Placement

Place `.crawlignore` in the **parent directory** of the dungeon (the directory being scanned during triage). The file itself is automatically excluded from triage candidates.

```
my-workspace/
├── .crawlignore      ← controls triage exclusions for this directory
├── dungeon/
│   └── .crawl.yaml   ← separate: dungeon-internal config
├── debug.log         ← excluded by *.log pattern
└── my-project/       ← included (no matching pattern)
```

## Syntax

`.crawlignore` follows the same syntax as `.gitignore`:

| Pattern | Meaning |
|---------|---------|
| `*.log` | Exclude all files ending in `.log` |
| `test-*` | Exclude anything starting with `test-` |
| `temp` | Exclude a file or directory named `temp` |
| `!important.log` | Re-include `important.log` (negation) |
| `# comment` | Lines starting with `#` are comments |
| (blank line) | Empty lines are ignored |

## Example

```gitignore
# Exclude build artifacts
*.log
*.tmp
build-*

# Exclude scratch directories
scratch

# But keep the important log
!audit.log
```

## Relationship to .crawl.yaml

`.crawlignore` and `.crawl.yaml` coexist as separate exclusion layers:

- **`.crawl.yaml`** lives inside the dungeon directory. Its `excludes:` field lists exact directory names to skip during triage. It is dungeon-internal configuration.
- **`.crawlignore`** lives in the parent directory. It supports glob patterns, comments, and negation. It is user-facing exclusion configuration.

Both are applied during triage. Items excluded by either mechanism are skipped.

## Exclusion Layer Order

During triage, items are filtered through 5 layers in order:

1. **Hardcoded system files** — `.git`, `CLAUDE.md`, `README.md`, etc.
2. **Workflow schema directories** — directories defined in `.workflow.yaml`
3. **OBEY.md-managed directories** — any directory containing `OBEY.md`
4. **`.crawl.yaml` excludes** — exact name matches from dungeon config
5. **`.crawlignore` patterns** — gitignore-style glob matching
