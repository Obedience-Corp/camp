## camp dungeon explore

Browse finished work across every dungeon

### Synopsis

Browse finished work across every dungeon in this camp.

The feed is newest day first. Each row shows the work, the day it was put in
the dungeon, and which dungeon it came from. Festivals with a replay show that
replay on the focused row. Enter reads the item. g jumps the shell to it when
shell integration is installed.

[ and ] move between dungeons. s changes the status lens. The opening lens is
finished work: completed and done.

  camp dungeon explore
  camp dungeon explore --status archived
  camp dungeon explore --dungeon Festivals --query FA0024
  camp dungeon explore --json --since 2026-09-01

A pipe, or --json, prints the same index and does not open the feed.

```
camp dungeon explore [flags]
```

### Options

```
      --dungeon string   Dungeon lens: all, a label, or a camp-relative dungeon path (default "all")
  -h, --help             help for explore
      --images string    Replay rendering: auto, kitty, iterm, or off (default "auto")
      --json             Print the feed as JSON
      --no-media         Do not draw replays
      --query string     Case-insensitive substring matched against title, id, summary, dungeon, and path
      --since string     Include items put in the dungeon on or after this YYYY-MM-DD date
      --status string    Status lens: finished, completed, done, archived, someday, killed, holding, all, or a status name (default "finished")
      --until string     Include items put in the dungeon on or before this YYYY-MM-DD date
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp dungeon](camp_dungeon.md)	 - Manage the camp dungeon
