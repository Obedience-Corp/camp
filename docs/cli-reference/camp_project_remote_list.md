## camp project remote list

List remotes for the project

### Synopsis

List all git remotes configured for the current project.

For submodule projects, also shows whether the origin URL matches
the canonical URL declared in .gitmodules.

Examples:
  camp project remote list
  camp project remote list --project my-api

```
camp project remote list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --config string    config file (default: ~/.obey/campaign/config.json)
      --no-color         disable colored output
  -p, --project string   Project name (auto-detected from cwd if not specified)
      --verbose          enable verbose output
```

### SEE ALSO

* [camp project remote](camp_project_remote.md)	 - Manage remotes for a project
