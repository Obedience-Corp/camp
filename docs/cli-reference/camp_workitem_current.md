## camp workitem current

Removed: was the local current-workitem pointer

### Synopsis

camp workitem current has been removed.

It previously stored a single campaign-local pointer in
.campaign/workitems/current.yaml. That model collided with multi-item
attention stages and was easy for humans and agents to misuse when stale.

Scope workitems with --workitem, by working inside a workitem directory, or
with a primary project/festival link (camp workitem link).

```
camp workitem current [selector] [flags]
```

### Options

```
      --clear   removed: previously cleared current.yaml
  -h, --help    help for current
      --json    emit a structured JSON error envelope
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp workitem](camp_workitem.md)	 - View active campaign work items
