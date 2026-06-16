## camp workflow shortcut add

Attach a navigation shortcut to an existing workflow

### Synopsis

Attach a navigation shortcut to an existing workflow collection.

The command updates .campaign/settings/jumps.yaml so cgo and camp navigation
can jump to workflow/<type>/ by key. The workflow type must already exist. Use
--replace to overwrite a conflicting shortcut and --json for machine-readable
result details.

```
camp workflow shortcut add <type> <key> [flags]
```

### Options

```
  -h, --help      help for add
      --json      emit a structured JSON result
      --replace   replace an existing shortcut with the same name
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp workflow shortcut](camp_workflow_shortcut.md)	 - Manage navigation shortcuts for workflow collections
