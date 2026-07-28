## camp jobs

Inspect and run camp's deferred commit queue

### Synopsis

Inspect and run the deferred commit queue.

Camp defers its own bookkeeping commits so they do not hold your terminal. The
queue lives under .campaign/cache/jobs and is machine-local and disposable:
git is the record, this is only the work still on its way there.

Workers normally start themselves when a command enqueues work, so 'jobs run'
is mostly for debugging and for the detached child camp spawns.

### Options

```
  -h, --help   help for jobs
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Campaign management CLI for multi-project AI workspaces
* [camp jobs run](camp_jobs_run.md)	 - Serve every lane with pending work, then exit
