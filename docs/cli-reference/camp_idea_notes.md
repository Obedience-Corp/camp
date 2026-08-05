## camp idea notes

Manage the note store (folders, moves, meetings)

### Synopsis

Manage the campaign note store under .campaign/intents/notes/.

Use "camp idea note" to capture a note. This command group manages folders
and placement of notes already in the store.

Examples:
  camp idea notes folders                 List note folders
  camp idea notes folders --json          Machine-readable folder list
  camp idea notes folders add reading     Create notes/reading/
  camp idea notes folders rm reading      Remove empty folder
  camp idea notes folders mv a b          Rename folder a → b
  camp idea notes mv <note-id> reading    Move a note into a folder

```
camp idea notes [flags]
```

### Options

```
  -h, --help   help for notes
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp idea](camp_idea.md)	 - Manage campaign ideas
* [camp idea notes folders](camp_idea_notes_folders.md)	 - List note folders
* [camp idea notes import-meeting](camp_idea_notes_import-meeting.md)	 - Import a meeting bundle into notes/meetings/
* [camp idea notes mv](camp_idea_notes_mv.md)	 - Move a note into a folder
