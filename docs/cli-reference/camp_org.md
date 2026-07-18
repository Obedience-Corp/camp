## camp org

Group campaigns into orgs

### Synopsis

Group related campaigns into first-class orgs.

Every campaign belongs to exactly one org (default "default"). Orgs are first-class:
they persist in the machine-wide registry, can hold zero members, and are deleted
explicitly with 'camp org delete'.

In a terminal, 'camp org' (no arguments) opens an interactive browser of orgs
and their members where you can move, create, rename, and return campaigns. When
piped or with --json it prints the current campaign's org instead; use
'camp org which' to print the org unconditionally.

Commands:
  which   Print the current campaign's org
  create  Create an org (optionally --empty) and optionally join campaigns
  add     Assign campaigns to an org (also reassigns; single-membership)
  remove  Return campaigns to the default org
  delete  Delete an org (empty only unless --force)

```
camp org [flags]
```

### Examples

```
  camp org                                       Browse and manage orgs interactively (TTY)
  camp org which                                 Print the current campaign's org
  camp org create obey                           Add the current campaign to "obey"
  camp org create empty-org --empty              Create an org with no members
  camp org add obey obey-campaign obey-content   Move campaigns into "obey"
  camp org remove obey-content                   Return a campaign to "default"
  camp org delete empty-org                      Delete an empty org
```

### Options

```
  -h, --help          help for org
  -i, --interactive   Open the interactive org browser (prints the org list when stdout is not a terminal)
      --json          Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Campaign management CLI for multi-project AI workspaces
* [camp org add](camp_org_add.md)	 - Assign campaigns to an org (reassigns; single-membership)
* [camp org create](camp_org_create.md)	 - Create an org (optionally empty) and join campaigns
* [camp org delete](camp_org_delete.md)	 - Delete an org (empty only unless --force)
* [camp org list](camp_org_list.md)	 - List orgs with member and active counts
* [camp org next](camp_org_next.md)	 - Switch to the next campaign in the current campaign's org
* [camp org remove](camp_org_remove.md)	 - Return campaigns to the default org
* [camp org rename](camp_org_rename.md)	 - Rename an org, reassigning all members atomically
* [camp org show](camp_org_show.md)	 - Show an org's member campaigns
* [camp org toggle](camp_org_toggle.md)	 - Toggle back to the last-visited campaign in the current org
* [camp org which](camp_org_which.md)	 - Print the current campaign's org
