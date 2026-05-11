## camp attach

Attach an external directory to a campaign

### Synopsis

Attach a non-project directory to a campaign by writing a .camp marker.

The user manages the symlink (if any). camp attach only writes the marker at
the resolved target so commands run from inside that directory know which
campaign owns it.

If the target is reached through a symlink, camp follows it once and writes
the marker at the final directory.

Examples:
  camp attach ai_docs/examples/external-repo
  camp attach ~/scratch/notes-link
  camp attach /abs/path/to/dir --campaign platform

```
camp attach <path> [flags]
```

### Options

```
  -c, --campaign string   Target campaign by name or ID; defaults to current campaign or interactive picker
      --force             Overwrite an existing attachment marker
  -h, --help              help for attach
```

### Options inherited from parent commands

```
      --config string   config file (default: ~/.obey/campaign/config.json)
      --no-color        disable colored output
      --verbose         enable verbose output
```

### SEE ALSO

* [camp](camp.md)	 - Campaign management CLI for multi-project AI workspaces
