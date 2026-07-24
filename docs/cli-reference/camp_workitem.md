## camp workitem

View active campaign work items

### Synopsis

View active campaign work items.

Launches an interactive dashboard on a TTY. Non-interactive callers must pass
--json, --list, or --print.

Examples:
  camp workitem                       # interactive dashboard
  camp workitem --json --type design  # JSON, filtered by type
  camp workitem --list                # compact grouped list
  camp workitem --print               # print a path for shell integration

```
camp workitem [flags]
```

### Options

```
      --attention-stage stringArray   Filter by attention stage
      --category stringArray          Filter by workflow category
      --group stringArray             Filter by workitem group
      --group-by string               Group sections (default "attention_stage")
  -h, --help                          help for workitem
      --json                          Output as JSON
      --limit int                     Maximum items to return
      --list                          Output a compact grouped list
      --print                         Print path only
      --query string                  Filter by search query
      --show-parked                   Include parked workitems
      --stage stringArray             Filter by lifecycle stage
      --status stringArray            Filter by displayed status
      --type stringArray              Filter by workflow type
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Campaign management CLI for multi-project AI workspaces
* [camp workitem adopt](camp_workitem_adopt.md)	 - Adopt an existing directory or file as a workitem
* [camp workitem commit](camp_workitem_commit.md)	 - Commit changes scoped to a workitem
* [camp workitem commits](camp_workitem_commits.md)	 - List commits referencing a workitem
* [camp workitem create](camp_workitem_create.md)	 - Create workitem tracking metadata
* [camp workitem current](camp_workitem_current.md)	 - Get, set, or clear the current workitem
* [camp workitem doctor](camp_workitem_doctor.md)	 - Report link-registry health issues
* [camp workitem group](camp_workitem_group.md)	 - Set or clear the group
* [camp workitem link](camp_workitem_link.md)	 - Create a workitem link
* [camp workitem links](camp_workitem_links.md)	 - List workitem links
* [camp workitem list](camp_workitem_list.md)	 - List or browse filtered workitems
* [camp workitem priority](camp_workitem_priority.md)	 - Set or clear the manual priority
* [camp workitem promote](camp_workitem_promote.md)	 - Promote a workitem to a festival, doc, or dungeon
* [camp workitem rename](camp_workitem_rename.md)	 - Rename a workitem and repair references
* [camp workitem repair](camp_workitem_repair.md)	 - Repair a workflow directory into a workitem
* [camp workitem resolve](camp_workitem_resolve.md)	 - Print the workitem for the current context
* [camp workitem stage](camp_workitem_stage.md)	 - Set or clear the attention stage
* [camp workitem unlink](camp_workitem_unlink.md)	 - Remove workitem links
* [camp workitem validate](camp_workitem_validate.md)	 - Validate workitem directories
* [camp workitem worktree](camp_workitem_worktree.md)	 - Create a project worktree from a workitem
