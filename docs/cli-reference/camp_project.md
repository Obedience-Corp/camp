## camp project

Manage camp projects

### Synopsis

Manage git submodules and project repositories in the camp.

A project can be:
  - a git repository tracked as a submodule under projects/
  - a machine-local linked workspace attached via symlink under projects/
  - an ordinary camp-owned directory tracked by the camp repository

Use 'camp project add' for submodules and 'camp project link' / 'camp project unlink'
for linked workspaces. Use 'camp project run' (or the 'cr -p' shell shorthand)
to run a command inside a project from anywhere in the camp.

Examples:
  camp project list                    List all projects
  camp project add git@github.com:org/repo.git  Add a new project
  camp project link ~/code/my-project  Link an existing local workspace
  camp project run -p fest -- just build  Run a command inside a project
  camp project commit -p fest -m "fix"  Commit changes in a project submodule
  camp project rename api-old api        Rename a managed project
  camp project prune                   Delete merged branches in the cwd's project
  camp project worktree add my-branch --project fest  Create a worktree for a project
  camp project remove api-service      Remove a project

```
camp project [flags]
```

### Options

```
  -h, --help   help for project
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Manage your camps and the projects and festivals inside them
* [camp project add](camp_project_add.md)	 - Add a project to camp
* [camp project commit](camp_project_commit.md)	 - Commit changes in a project submodule
* [camp project link](camp_project_link.md)	 - Link an existing local project into a camp
* [camp project list](camp_project_list.md)	 - List projects in camp
* [camp project new](camp_project_new.md)	 - Create a new project in camp
* [camp project prune](camp_project_prune.md)	 - Delete merged branches in a project
* [camp project remote](camp_project_remote.md)	 - Manage remotes for a project
* [camp project remove](camp_project_remove.md)	 - Remove a project from camp
* [camp project rename](camp_project_rename.md)	 - Rename a managed project
* [camp project run](camp_project_run.md)	 - Run a command inside a project directory, like cr but project-scoped
* [camp project stage](camp_project_stage.md)	 - Stage changes in a project submodule
* [camp project unlink](camp_project_unlink.md)	 - Unlink a linked project from a camp
* [camp project worktree](camp_project_worktree.md)	 - Manage worktrees for a project
