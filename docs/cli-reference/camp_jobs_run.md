## camp jobs run

Serve every lane with pending work, then exit

### Synopsis

Serve every lane with pending work and exit when none is left.

This is the entrypoint camp spawns detached after enqueuing. Running it by
hand is safe: lanes are locked per repo, so a second worker simply finds the
lanes taken and exits.

```
camp jobs run [flags]
```

### Options

```
      --campaign string   Campaign root to serve (defaults to the detected campaign)
  -h, --help              help for run
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp jobs](camp_jobs.md)	 - Inspect and run camp's deferred commit queue
