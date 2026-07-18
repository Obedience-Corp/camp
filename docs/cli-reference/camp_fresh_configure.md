## camp fresh configure

Configure camp fresh follow-up commands

### Synopsis

Manage the follow-up command workflows camp fresh runs after a
successful sync/prune/branch cycle. Configuration lives in
.campaign/settings/fresh.yaml: a global default list, plus optional
per-project override lists that replace the global list entirely.

Run without a subcommand to open the interactive setup for humans. Use
show, add, move, and remove for scripts and agents.

The interactive setup opens on the project you are standing in, resolved the
same way camp fresh picks its target, so the overrides you edit are the ones
that run. Pass --project to open on a different project, and edit the global
defaults by selecting them in the left pane.

Examples:
  camp fresh configure
  camp fresh configure --project camp
  camp fresh show-workflow camp
  camp fresh configure show
  camp fresh configure add install --run "npm install"
  camp fresh configure add build --run "go build ./..." --project camp --dir cmd/camp
  camp fresh configure move build --up --project camp
  camp fresh configure remove install
  camp fresh configure remove build --project camp

```
camp fresh configure [flags]
```

### Options

```
  -h, --help             help for configure
      --project string   Open the setup on a project scope (default: detected from the current directory)
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```

### SEE ALSO

* [camp fresh](camp_fresh.md)	 - Post-merge branch cycling: sync to default branch and optionally create a new working branch
* [camp fresh configure add](camp_fresh_configure_add.md)	 - Add a follow-up command workflow step
* [camp fresh configure move](camp_fresh_configure_move.md)	 - Move a follow-up command workflow step
* [camp fresh configure remove](camp_fresh_configure_remove.md)	 - Remove a follow-up command workflow step
* [camp fresh configure show](camp_fresh_configure_show.md)	 - Show configured follow-up workflows
