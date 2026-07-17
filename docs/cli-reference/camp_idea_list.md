## camp idea list

List ideas in the campaign

### Synopsis

List ideas with filtering, sorting, and output format options.

By default, lists ideas in inbox, active, and ready status.
Use --all to include dungeon ideas.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp idea list                         List active ideas
  camp idea ls --status inbox            List inbox only
  camp idea list -f json                 JSON output
  camp idea list -f simple | xargs ...   Pipe IDs to commands
  camp idea list --all                   Include archived

```
camp idea list [flags]
```

### Options

```
  -a, --all              Include dungeon ideas
  -f, --format string    Output format: table, simple, json (default "table")
  -h, --help             help for list
      --horizon string   Filter by horizon
      --json             emit a structured JSON result
  -n, --limit int        Limit results (0 = no limit)
  -p, --project string   Filter by project
  -S, --sort string      Sort by: updated, created, priority, title (default "updated")
  -s, --status strings   Filter by status (repeatable)
  -t, --type strings     Filter by type (repeatable)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp idea](camp_idea.md)	 - Manage campaign ideas
