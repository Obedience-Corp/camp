# Archived

This directory contains work that has been truly archived - moved out of the way
and committed to git history.

## What Lives Here

Items moved here during `camp dungeon crawl` are considered "done" - you've made
the decision to archive them and move on.

## Recovery

Everything in this directory is tracked by git. If you ever need something back:

### Find the commit
```bash
git log --all -- dungeon/archived/item-name
```

### Restore from history
```bash
# Restore to its original location
git checkout <commit>^ -- dungeon/archived/item-name

# Or restore to a new location
git show <commit>^:dungeon/archived/item-name > recovered-item
```

### View without restoring
```bash
git show <commit>^:dungeon/archived/item-name
```

## Guidelines

- Don't manually move items here - use `camp dungeon crawl`
- Don't manually delete from here - let git history be your backup
- If you find yourself frequently recovering items, maybe they shouldn't be archived
