## camp project link

Link an existing local project into a campaign

### Synopsis

Link an existing local directory into a campaign without cloning it.

Linked projects are added as symlinks under projects/ and receive a local
.camp marker so camp can recover campaign context from inside the external
workspace later.

If path is omitted, the current working directory is linked.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or a bare --campaign to
pick a registered target campaign interactively.

Examples:
  camp project link
  camp project link --campaign platform
  camp project link ~/code/my-project
  camp project link ~/code/my-project --name backend
  camp project link ~/code/my-project --campaign platform
  camp project link ~/code/my-project --campaign

```
camp project link [path] [flags]
```

### Options

```
  -c, --campaign string   Target campaign by name or ID; omit value to pick interactively
  -h, --help              help for link
  -n, --name string       Override project name (defaults to directory name)
      --no-commit         Skip automatic git commit
```

### Options inherited from parent commands

```
      --config string   config file (default: ~/.obey/campaign/config.json)
      --no-color        disable colored output
      --verbose         enable verbose output
```

### SEE ALSO

* [camp project](camp_project.md)	 - Manage campaign projects

