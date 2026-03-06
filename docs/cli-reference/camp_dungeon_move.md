## camp dungeon move

Move dungeon items between statuses

### Synopsis

Move items within the dungeon or from the parent directory into the dungeon.

Without --triage, moves an item already in the dungeon root to a status directory.
With --triage, moves an item from the parent directory into the dungeon.
With --triage and --to-docs, routes an item to campaign-root docs/\<subdirectory\>.
Dungeon context is resolved by walking from current directory to campaign root
and selecting the nearest dungeon.

Statuses: completed, archived, someday

Examples:
  camp dungeon move old-feature archived         Move dungeon item to archived
  camp dungeon move stale-doc completed          Move dungeon item to completed
  camp dungeon move old-project --triage         Move parent item into dungeon root
  camp dungeon move old-project archived --triage Move parent item directly to archived
  camp dungeon move stale-note.md --triage --to-docs architecture/api Route to docs subdirectory

```
camp dungeon move <item> [status] [flags]
```

### Options

```
  -h, --help        help for move
      --no-commit   Don't create a git commit
      --to-docs string   Route triage item into campaign-root docs/<subdir> (requires --triage)
      --triage      Move from parent directory (not from dungeon root)
```

### Options inherited from parent commands

```
      --config string   config file (default: ~/.obey/campaign/config.yaml)
      --no-color        disable colored output
      --verbose         enable verbose output
```

### SEE ALSO

* [camp dungeon](camp_dungeon.md)	 - Manage the campaign dungeon
