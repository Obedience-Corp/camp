## camp approvals withdraw

Clear a request whose source is no longer waiting

### Synopsis

Record that a request is no longer waiting, without approving or rejecting
anything.

Use it for a request that outlived its source: the workflow was deleted, the
worktree is gone, or the request was recorded by mistake. Withdrawing only
removes the request from this list. It does not unblock anything, because the
ledger never grants an approval: if the source is in fact still waiting, it
reports itself again the next time it is advanced.

```
camp approvals withdraw <id> [flags]
```

### Options

```
  -h, --help            help for withdraw
      --reason string   Why the request is being withdrawn (required)
```

### Options inherited from parent commands

```
      --json       Emit a structured JSON result
      --no-color   disable colored output
```

### SEE ALSO

* [camp approvals](camp_approvals.md)	 - See what in this camp is waiting on your approval
