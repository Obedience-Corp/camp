## camp

Manage your camps and the projects and festivals inside them

### Synopsis

Camp manages your camps.

A camp is one context in your life: your job, a side project, your taxes. Each
camp holds the projects you work on and hosts the festivals you run in them.
Camp creates camps, manages git submodules as projects, and gives you
lightning-fast navigation through category shortcuts and TUI fuzzy finding.

GETTING STARTED:
  camp init               Initialize a new camp in the current directory
  camp project list       List all projects in the camp
  camp list               Show all registered camps

NAVIGATION (using cgo shell function):
  cgo                     Navigate to camp root
  cgo p                   Navigate to projects directory
  cgo f                   Navigate to festivals directory
  cgo <name>              Fuzzy find and navigate to any target

COMMON WORKFLOWS:
  camp project add <url>  Add a git repo as a project submodule
  camp run <command>      Run command from camp root directory
  camp shortcuts          View all available navigation shortcuts

Run 'camp shell-init' to enable the cgo navigation function.

```
camp [flags]
```

### Options

```
  -h, --help       help for camp
      --no-color   disable colored output
```

### SEE ALSO

* [camp artifacts](camp_artifacts.md)	 - Manage declared artifact roots (.campaign/artifacts.yaml)
* [camp attach](camp_attach.md)	 - Attach an external directory to a camp
* [camp cache](camp_cache.md)	 - Manage the navigation index cache
* [camp clone](camp_clone.md)	 - Clone a camp with full submodule setup
* [camp commit](camp_commit.md)	 - Commit changes in the camp root
* [camp completion](camp_completion.md)	 - Generate the autocompletion script for the specified shell
* [camp concepts](camp_concepts.md)	 - List configured concepts
* [camp copy](camp_copy.md)	 - Copy a file or directory within the camp
* [camp create](camp_create.md)	 - Create a new camp at the default camps directory
* [camp date](camp_date.md)	 - Append date suffix to file or directory name
* [camp detach](camp_detach.md)	 - Remove the current camp's attachment binding
* [camp doctor](camp_doctor.md)	 - Diagnose and fix camp health issues
* [camp dungeon](camp_dungeon.md)	 - Manage the camp dungeon
* [camp festivals](camp_festivals.md)	 - List festivals across camps, filtered by org/tag
* [camp fresh](camp_fresh.md)	 - Post-merge branch cycling: sync to default branch and optionally create a new working branch
* [camp gather](camp_gather.md)	 - Gather related work into unified items
* [camp go](camp_go.md)	 - Navigate to camp directories
* [camp id](camp_id.md)	 - Print the current camp ID
* [camp idea](camp_idea.md)	 - Manage camp ideas
* [camp init](camp_init.md)	 - Initialize a new camp
* [camp jobs](camp_jobs.md)	 - Inspect and run camp's deferred commit queue
* [camp leverage](camp_leverage.md)	 - Compute leverage scores for the camp's projects
* [camp lifecycle](camp_lifecycle.md)	 - Manage camp lifecycle status
* [camp list](camp_list.md)	 - List all registered camps
* [camp log](camp_log.md)	 - Show git log of the camp
* [camp machine](camp_machine.md)	 - Manage remote machines (~/.obey/machines.yaml)
* [camp move](camp_move.md)	 - Move a file or directory within the camp
* [camp notify](camp_notify.md)	 - Manage camp state notices
* [camp org](camp_org.md)	 - Group camps into orgs
* [camp pack](camp_pack.md)	 - Pack a directory into a portable .festival bundle
* [camp pin](camp_pin.md)	 - Pin a directory
* [camp pins](camp_pins.md)	 - List all pinned directories
* [camp plugins](camp_plugins.md)	 - List discovered camp plugins on PATH
* [camp project](camp_project.md)	 - Manage camp projects
* [camp promote](camp_promote.md)	 - Promote any intent, workitem, or festival (universal front door)
* [camp pull](camp_pull.md)	 - Pull latest changes from remote
* [camp push](camp_push.md)	 - Push camp changes to remote
* [camp refs-sync](camp_refs-sync.md)	 - Sync submodule ref pointers in camp root
* [camp register](camp_register.md)	 - Register a camp in the global registry
* [camp registry](camp_registry.md)	 - Manage the camp registry
* [camp root](camp_root.md)	 - Print the current camp root
* [camp run](camp_run.md)	 - Execute command from camp root, or just recipe in a project
* [camp settings](camp_settings.md)	 - Manage camp configuration
* [camp shell-init](camp_shell-init.md)	 - Output shell initialization code
* [camp shortcuts](camp_shortcuts.md)	 - List all available shortcuts
* [camp skills](camp_skills.md)	 - Manage camp skill directory links
* [camp stage](camp_stage.md)	 - Stage changes in the camp root
* [camp status](camp_status.md)	 - Show git status of the camp
* [camp switch](camp_switch.md)	 - Switch to a different camp
* [camp sync](camp_sync.md)	 - Safely synchronize submodules
* [camp tag](camp_tag.md)	 - Label camps with tags
* [camp transfer](camp_transfer.md)	 - Copy files between camps (and machines)
* [camp triage](camp_triage.md)	 - Review the camp's workitems in a recorded session
* [camp unbundle](camp_unbundle.md)	 - Unbundle a .festival archive into a directory
* [camp unpin](camp_unpin.md)	 - Remove a saved pin
* [camp unregister](camp_unregister.md)	 - Remove a camp from the registry
* [camp version](camp_version.md)	 - Show version information
* [camp workflow](camp_workflow.md)	 - Manage workflow collections
* [camp workitem](camp_workitem.md)	 - View active camp work items
