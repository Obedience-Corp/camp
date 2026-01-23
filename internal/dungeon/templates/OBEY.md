# Dungeon

The dungeon is a holding area for work you're unsure about or want out of the way.

## Purpose

When you have code, documents, or projects that:
- You're not ready to delete but don't want cluttering your workspace
- Need more thought before deciding their fate
- Are experiments that didn't pan out but might have value later
- Were started but priorities shifted

Move them here. The dungeon keeps them visible without them competing for your attention.

## Structure

```
dungeon/
├── OBEY.md          # This file
├── archived/        # Truly archived work (committed to history)
│   └── README.md    # Instructions for archived items
├── crawl.jsonl      # History of crawl decisions
└── [your items]     # Items awaiting review
```

## Workflow

### Adding Items

Move items directly into the dungeon root:
```bash
mv some-experiment/ dungeon/
```

### Reviewing Items

Run the interactive crawl to review each item:
```bash
camp dungeon crawl
```

For each item, you can:
- **Keep**: Leave it in the dungeon for later
- **Archive**: Move to `archived/` - truly out of the way
- **Skip**: Come back to it another time

### Recovery

Items in `archived/` are committed to git history. If you need them back:
```bash
git log --all -- dungeon/archived/item-name
git checkout <commit>^ -- dungeon/archived/item-name
```

## Best Practices

1. **Regular crawls**: Review the dungeon periodically (weekly/monthly)
2. **Don't let it grow**: The dungeon should be temporary, not permanent storage
3. **Archive decisively**: If you haven't touched something in months, archive it
4. **Trust git**: Archived items are in history - you can always recover them
