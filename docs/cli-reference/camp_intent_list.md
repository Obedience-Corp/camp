## camp intent list

List intents in the campaign

### Synopsis

List intents with filtering, sorting, and output format options.

By default, lists intents in inbox, active, and ready status.
Use --all to include dungeon intents.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp intent list                         List active intents
  camp intent ls --status inbox            List inbox only
  camp intent list -f json                 JSON output
  camp intent list -f simple | xargs ...   Pipe IDs to commands
  camp intent list --all                   Include archived
  camp intent list --stale                 Claimed intents with no update in 7 days
  camp intent list --stale --days 3        Same, with a 3 day threshold

```
camp intent list [flags]
```

### Options

```
  -a, --all              Include dungeon intents
      --days int         Staleness threshold in days, used with --stale (default 7)
  -f, --format string    Output format: table, simple, json (default "table")
  -h, --help             help for list
      --horizon string   Filter by horizon
      --json             emit a structured JSON result
  -n, --limit int        Limit results (0 = no limit)
  -p, --project string   Filter by project
  -S, --sort string      Sort by: updated, created, priority, title (default "updated")
      --stale            Only show claimed intents with no update in --days (default 7)
  -s, --status strings   Filter by status (repeatable)
  -t, --type strings     Filter by type (repeatable)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp intent](camp_intent.md)	 - Manage campaign intents
