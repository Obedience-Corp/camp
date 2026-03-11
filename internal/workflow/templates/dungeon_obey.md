# Dungeon

Holding area for work that's finished, archived, or deferred.

## Purpose

The dungeon contains three categories of "done" work:

- **completed/** - Successfully finished work
- **archived/** - Preserved for history, truly done
- **someday/** - Deprioritized but might revisit

Items moved into these status directories are organized into `YYYY-MM-DD/`
subdirectories so humans and agents can see when work was moved there.

## Structure

```
dungeon/
├── OBEY.md           # This file
├── completed/        # Completed work, bucketed by move date
├── archived/         # Historical archive, bucketed by move date
└── someday/          # Deferred work, bucketed by move date
```

## Workflow

### Finishing Work
```bash
camp flow move some-item dungeon/completed   # Done
camp flow move some-item dungeon/archived    # Done, won't need again
```

### Deferring Work
```bash
camp flow move some-item dungeon/someday     # Not now, maybe later
```

### Reviving Work
```bash
camp flow move old-item active    # Back to work
camp flow move old-item ready     # Queue it up
```

## Reviewing Items

Run the interactive crawl to review dungeon contents:
```bash
camp dungeon crawl
```

## Best Practices

1. **completed/** - For work you completed
2. **archived/** - For true history (old versions, superseded designs)
3. **someday/** - For work you intentionally deferred
4. Review periodically with `camp dungeon crawl`
