## camp notify

Manage camp state notices

### Synopsis

Manage the advisory notices camp surfaces on commands you already run.

Notices describe camp state you may not know is true, such as a declared
artifact root that has never synced. Each one carries its own dismiss command.

Dismissals are stored in .campaign/notices.yaml, which is committed: a
dismissal you make on one machine travels to your others, the same way the
artifact declarations it concerns do.

### Options

```
  -h, --help   help for notify
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Manage your camps and the projects and festivals inside them
* [camp notify dismiss](camp_notify_dismiss.md)	 - Stop showing a notice
* [camp notify list](camp_notify_list.md)	 - List dismissed notices
* [camp notify restore](camp_notify_restore.md)	 - Show a dismissed notice again
