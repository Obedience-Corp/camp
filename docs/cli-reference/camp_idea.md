## camp idea

Manage campaign ideas

### Synopsis

Manage ideas for features and improvements not yet ready for full planning.

Ideas capture thoughts, bugs, features, and research topics that depend on work
not yet completed. They serve as structured storage for ideas that aren't ready
to become Festivals but need to be tracked.

CAPTURE MODES:
  Fast (default)    Quick capture with minimal fields
  Deep (--edit)     Open in editor for full context

IDEA LIFECYCLE:
  inbox  → Captured, not yet reviewed
  ready  → Reviewed/enriched, ready for promotion
  active → Promoted to festival/design doc, work in progress
  dungeon/* → Terminal statuses (done, killed, archived, someday)

"camp intent" (the original name) keeps working as an alias for every
command below; the storage path is .campaign/intents/ either way.

Examples:
  camp idea add "Add dark mode toggle"         Fast capture to inbox
  camp idea add -e "Refactor auth system"      Deep capture with editor
  camp idea list                               List all ideas
  camp idea list --status active               List active ideas
  camp idea edit add-dark                      Edit idea (fuzzy match)
  camp idea show 20260119-153412-add-dark      Show idea details
  camp idea move add-dark ready                Mark as ready
  camp idea promote add-dark                   Promote to active via festival
  camp idea archive add-dark                   Archive idea

```
camp idea [flags]
```

### Options

```
  -h, --help   help for idea
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Campaign management CLI for multi-project AI workspaces
* [camp idea add](camp_idea_add.md)	 - Create a new idea
* [camp idea archive](camp_idea_archive.md)	 - Archive an idea
* [camp idea convert](camp_idea_convert.md)	 - Convert a note into an idea
* [camp idea count](camp_idea_count.md)	 - Count ideas by status directory
* [camp idea crawl](camp_idea_crawl.md)	 - Interactive idea triage
* [camp idea edit](camp_idea_edit.md)	 - Edit an existing idea
* [camp idea explore](camp_idea_explore.md)	 - Interactive idea explorer
* [camp idea find](camp_idea_find.md)	 - Search for ideas by title or content
* [camp idea gather](camp_idea_gather.md)	 - Gather related ideas into a unified document
* [camp idea list](camp_idea_list.md)	 - List ideas in the campaign
* [camp idea move](camp_idea_move.md)	 - Move idea to a different status
* [camp idea note](camp_idea_note.md)	 - Capture a quick note
* [camp idea promote](camp_idea_promote.md)	 - Promote an idea through the pipeline
* [camp idea rename](camp_idea_rename.md)	 - Rename an idea
* [camp idea show](camp_idea_show.md)	 - Show detailed idea information
