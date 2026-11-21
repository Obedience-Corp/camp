## camp idea add

Create a new idea

### Synopsis

Create a new idea with fast or deep capture mode.

CAPTURE MODES:
  Ultra-fast          Title provided as argument → immediate creation
  Fast TUI (default)  Step-through form (title, type, concept)
  Full TUI (--full)   Step-through form including body textarea
  Deep (--edit)       Full template in $EDITOR

Fast capture is optimized for speed - ideas are saved immediately.
Use --full when you want to add a body description in the form.
Use --edit when you need the complete template in your editor.

PROGRAMMATIC (agent) FLAGS:
  --body                Set idea body from a literal string
  --body-file           Read idea body from a file (- for stdin)
  --concept             Set the concept field (e.g., "projects/camp")
  --tag                 Add a tag (repeatable)
  --priority            Set priority (low, medium, high)
  --horizon             Set horizon (now, next, later, someday)
  --blocked-by          Idea or festival ID this is blocked by (repeatable)
  --depends-on          Idea or festival ID this depends on (repeatable)
  --promotion-criteria  What must be true before this can become a festival
  --note                Create a note instead of a lifecycle idea
  --author              Override the default author attribution
  --json                Print the created idea's id and path as JSON

  --body and --body-file are mutually exclusive.
  --full + body flags is a usage error.
  --edit + body flags pre-fills the editor template.

ONE-CALL CAPTURE:
  A title plus flags creates the complete idea in one command. Put a
  multi-line markdown body on stdin with --body-file -:

    camp idea add "Login returns 500 after password reset" \
      --type bug --concept projects/camp --priority high --horizon next \
      --tag auth --body-file - <<'EOF'
    What is wrong or wanted, in a few sentences.

    Where it came from.

    Done when: the observable result that closes it.
    EOF

Examples:
  camp idea add "Add dark mode"        Ultra-fast capture
  camp idea add -c obey-campaign "Add dark mode"
  camp idea add                        Fast TUI (3-step form)
  camp idea add --campaign             Pick a target camp interactively
  camp idea add --full                 Full TUI (includes body)
  camp idea add --note                 Note TUI (title + body, no type/concept)
  camp idea add --note "Meeting note" --body "Follow up next week"
  camp idea add -e "Complex feature"   Deep capture with editor
  camp idea add -t feature "New API"   Set type explicitly
  camp idea add "Fix login" --body "The login page returns 500"
  camp idea add "Migrate DB" --body-file spec.md --concept projects/camp
  camp idea add "Fix login" --priority high --horizon now --blocked-by <id>
  echo "body" | camp idea add "Idea" --body-file -

```
camp idea add [title] [flags]
```

### Options

```
      --author string               Override the default author attribution
      --blocked-by stringArray      Idea or festival ID this is blocked by (repeatable)
      --body string                 Set idea body as a literal string
      --body-file string            Read idea body from file (- for stdin, 10 MiB cap)
  -c, --campaign string             Target camp by name or ID; omit value to pick interactively
      --concept string              Set the concept field (e.g., projects/camp)
      --depends-on stringArray      Idea or festival ID this depends on (repeatable)
  -e, --edit                        Open in $EDITOR for deep capture
      --full                        Full TUI mode with body textarea
  -h, --help                        help for add
      --horizon string              Set horizon (now, next, later, someday; default later)
      --json                        emit a structured JSON result
      --no-commit                   Don't create a git commit
      --note                        Create a note instead of a lifecycle idea
      --priority string             Set priority (low, medium, high; default medium)
      --promotion-criteria string   What must be true before this can become a festival
      --tag stringArray             Add a tag (repeatable)
  -t, --type string                 Type (idea, feature, bug, research, chore) (default "idea")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp idea](camp_idea.md)	 - Manage camp ideas
