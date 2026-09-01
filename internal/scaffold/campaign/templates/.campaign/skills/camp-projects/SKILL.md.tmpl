---
name: camp-projects
description: Manage a camp's projects with `camp project`. Use when the user wants to add, link, or remove a project; when deciding between `camp project add` and `camp project link`; or when committing inside `projects/*`.
---

# Camp Projects

A project in a camp may be a git submodule under `projects/`, a linked
project (a symlink at `projects/<name>`), or an ordinary directory owned by
the camp repository. Most external repositories prefer linking. Default
toward link unless the user has stated a reason for a submodule.

## Decide: `add` or `link` — ASK the User

- **URL given, not cloned locally** → `camp project add <url>`. Clones
  the repo as a git submodule under `projects/<name>`.
- **Already cloned on the user's machine** → `camp project link <path>`.
  Creates a symlink at `projects/<name>` pointing back to the user's
  existing directory; the directory stays where it is on disk.
- **User wants a multi-device camp** → submodules are required. Linked
  projects only resolve on a device where the linked path exists at the
  same filesystem location. If the user has an already-local repo and
  wants it to travel with the camp, tell them so and let them decide.

See `docs/cli-reference/` for niche flags like `add --local`.

## Remove

`camp project remove <name>` works for both kinds without `--delete`:

- Linked project → safely unlinks (identical to `camp project unlink`).
- Submodule → deinits from git without deleting files on disk.

`--delete` is the destructive path. It errors on linked projects (camp
won't delete an external target) and deletes the submodule's directory
on disk for submodules.

Prefer `camp project unlink <name>` when the intent is specifically to
remove a link.

## Rename

Use the first-class transaction instead of remove/add or a raw filesystem
move:

```bash
camp project rename <current> <new>
camp project rename <current> <new> --remote-url <new-origin-url>
camp project rename <current> <new> --dry-run --json
```

It supports declared submodules, linked workspace symlinks, and ordinary
camp-owned directories. It preserves dirty project/worktree content and
migrates typed Camp references. Camp never infers a provider-side repository
rename; pass `--remote-url` when origin changed too.

## Scope Limits for Links (Current Behavior)

`camp status --sub`, `camp pull --sub`, `camp push --sub`, and the
`status all` / `pull all` / `push all` paths enumerate **submodules
only** today. They error or skip when the current project is linked.

To operate on a linked project, cd into the linked directory and use
`git` directly, or use `camp project run -p <name> <cmd>`.

## Commit in Submodules

```bash
camp p commit -m "fix: message"
```

Pointer sync is a separate intentional action:

```bash
camp refs-sync
camp refs-sync projects/camp
```

## Other Project Operations

```bash
camp project list [--format table|simple|json]
camp project new <name>                        # scaffold new submodule (always auto-commits)
camp project rename <current> <new> [--remote-url <url>]
camp project run -p <name> -- <command>        # run command inside a project
camp project prune [<name>] [--dry-run] [--remote] [--remote-delete]
camp project remote {list|add|set-url|remove|rename} [args] [-p <name>]
camp project worktree {add|list|remove} <name> [-p <name>]
```

`camp fresh` is project-scoped (not a camp-root command); it
resets a project's working state after a merge:

```bash
camp fresh [<name>]                         # checkout default, pull, prune
camp fresh [<name>] --branch <branch>       # plus create a working branch
camp fresh all                              # run across every submodule
```

## Common Mistakes

- Using `camp project add` paths on a repo that already lives on the
  user's machine. Default to `camp project link` instead.
- Assuming submodule commits auto-update camp-root pointers — they
  don't. Use `camp refs-sync` intentionally.
- Expecting `--sub` or `all` to cover linked projects. They don't today.
- Using `remove --delete` when `remove` alone would do the right thing.
- Using `camp project remove` on a link when the intent is specifically
  "unlink" — `camp project unlink` reads clearer at the call site.
