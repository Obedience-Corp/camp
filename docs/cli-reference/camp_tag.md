## camp tag

Label camps with tags

### Synopsis

Label camps with tags from a single global pool.

Tags are orthogonal to orgs: any camp can carry any tag regardless of its
org, and the same tag can appear across orgs. Tags are a set per camp
(re-adding is a no-op).

Commands:
  add   Add tags to a camp
  rm    Remove tags from a camp
  list  List all tags in use with counts

```
camp tag [flags]
```

### Examples

```
  camp tag add obey-campaign paid-work q3-2026
  camp tag rm obey-campaign q3-2026
  camp tag list
```

### Options

```
  -h, --help   help for tag
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Manage your camps and the projects and festivals inside them
* [camp tag add](camp_tag_add.md)	 - Add tags to a camp
* [camp tag list](camp_tag_list.md)	 - List all tags in use with camp counts
* [camp tag rm](camp_tag_rm.md)	 - Remove tags from a camp
