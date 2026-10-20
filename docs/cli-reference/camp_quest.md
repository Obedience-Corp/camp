## camp quest

Manage working contexts within a camp

### Synopsis

Manage working contexts within a camp.

A quest is a long-lived working context: a sub-initiative that may span
multiple projects, agent sessions, documents, and festivals over days or
weeks. Think of it as the current "what am I working on" scope, not a
single task or feature ticket.

Quests live under .campaign/quests/ and are orthogonal to planning (which
lives in festivals). A quest groups related activity; a festival plans and
executes specific deliverables within that activity.

Examples:
  camp quest create "q2-reliability" --purpose "harden platform for Q2 launch"
  camp quest create "data-pipeline-rethink" --no-editor
  camp quest list
  camp quest pause q2-reliability
  camp quest complete data-pipeline-rethink

```
camp quest [flags]
```

### Options

```
  -h, --help   help for quest
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Manage your camps and the projects and festivals inside them
* [camp quest archive](camp_quest_archive.md)	 - Archive a quest
* [camp quest checklist](camp_quest_checklist.md)	 - List the checklist for a quest
* [camp quest clear](camp_quest_clear.md)	 - Clear the current terminal's quest context
* [camp quest complete](camp_quest_complete.md)	 - Mark a quest completed
* [camp quest create](camp_quest_create.md)	 - Create a new quest
* [camp quest edit](camp_quest_edit.md)	 - Edit an existing quest
* [camp quest item](camp_quest_item.md)	 - Manage checklist items on a quest
* [camp quest link](camp_quest_link.md)	 - Link a camp artifact to a quest
* [camp quest links](camp_quest_links.md)	 - List linked artifacts for a quest
* [camp quest list](camp_quest_list.md)	 - List quests
* [camp quest pause](camp_quest_pause.md)	 - Pause an active quest
* [camp quest rename](camp_quest_rename.md)	 - Rename a quest
* [camp quest restore](camp_quest_restore.md)	 - Restore a quest from the dungeon
* [camp quest resume](camp_quest_resume.md)	 - Resume a paused quest
* [camp quest show](camp_quest_show.md)	 - Show quest metadata
* [camp quest status](camp_quest_status.md)	 - Show the active terminal quest context
* [camp quest unlink](camp_quest_unlink.md)	 - Remove a linked artifact from a quest
* [camp quest update](camp_quest_update.md)	 - Update quest metadata
* [camp quest use](camp_quest_use.md)	 - Activate a quest for the current terminal
