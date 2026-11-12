## camp approvals commit

Commit approval events that are not committed yet

### Synopsis

Commit .campaign/approvals and nothing else.

Approval events are committed when they are recorded, and listing approvals
commits any that were missed. This does only the commit, for a script or before
a push, when an earlier commit could not be made.

```
camp approvals commit [flags]
```

### Options

```
  -h, --help   help for commit
```

### Options inherited from parent commands

```
      --json       Emit a structured JSON result
      --no-color   disable colored output
```

### SEE ALSO

* [camp approvals](camp_approvals.md)	 - See what in this camp is waiting on your approval
