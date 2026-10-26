# Workitem navigation and fuzzy completion for cgo

Status: proposed design; runtime behavior is unchanged.  
Tracking: `WI-bce810` (`design-cgo-workitem-navigation-2026-10-26`).

## Outcome

After an agent creates a workitem, the user can type `cgo partial-name`, use
Tab to discover matching workitems, and enter the selected workitem directory.
The user does not need to remember its workflow type or copy its full path.
`camp go --print` provides the same resolution for scripts.

## Current implementation findings

Inspected Camp main at `589560fa` on 2026-10-26.

- `cmd/camp/navigation/go.go` resolves configured shortcuts and pins before
  querying `internal/nav/index`. No-argument navigation toggles root/last location.
- `internal/nav/index/builder.go` scans standard navigation categories and
  festival lifecycle buckets. Design and explore directories already appear,
  but arbitrary marked workflow types and file-backed workitems do not share
  the workitem discovery contract.
- `internal/workitem/discover.go` is the shared workitem discovery entry point.
  It understands configured paths, custom marker-backed types, frontmatter
  files, intents, and festival residents. Its full model also loads timestamps,
  summaries, and workflow runtime state; measure that cost before putting the
  full scan on each Tab press.
- `internal/complete/complete.go` exposes plain and described completion with a
  200 ms context deadline. The plain and rich forms currently use different
  matching rules.
- `internal/shell/templates/zsh.sh.tmpl`, `bash.sh.tmpl`, and `fish.sh.tmpl`
  offer static shortcuts for an ordinary first argument. Dynamic first-argument
  completion is gated on `/` or `@`. Thus even already-indexed designs are not
  available through the expected `cgo partial<Tab>` interaction.
- Bash applies `compgen` prefix filtering after backend matching; zsh also
  applies its own matching. Returning fuzzy candidates alone does not prove
  that the real shell offers them.
- `internal/nav/index/query.go` maps names to a single target during rich
  completion. Duplicate slugs can lose their distinct paths. Resolution also
  chooses the first exact name. Completion must not promise a destination it
  cannot identify on execution.
- `internal/nav/index/cache.go` already checks workflow/intents/festival
  topology and explicit workitem mutations invalidate navigation caches.
  Its depth-bounded mtime checks need review for configured and nested paths.
- Fest's `CompleteGoTarget` uses the same fuzzy finder as navigation. Follow
  that consistency principle within Camp's existing architecture.

## Proposed behavior

1. Preserve toggle, configured shortcuts, pins, category navigation, project
   sub-shortcuts, and worktree syntax. Do not reserve a new short alias that
   could shadow existing camp configuration.
2. Include discoverable current workitems in the existing navigation target
   source. Respect the existing discovery policy for lifecycle stages and
   hidden/dungeon exclusions. Do not recursively treat arbitrary directories
   as workitems or create a second identity registry.
3. Match workitem slugs case-insensitively using the existing navigation fuzzy
   matcher. Exact names rank before partial matches. Preserve the existing
   best-match diagnostic for ordinary fuzzy navigation.
4. Preserve workitem identity separately from its destination. Directory items
   navigate to their own directory; file-backed items navigate to the file's
   parent directory. Selecting an item never redirects to its linked worktree.
5. Completion displays the relative source path with the name. Use a canonical
   camp-relative source path when names collide or conflict with reserved
   navigation names, and resolve that exact path back to the same item. Do not
   silently discard duplicate names. Include exact `WI-*` refs as a useful
   unambiguous route using the existing selector grammar.
6. Both plain and described completion use the same target selection and
   matching policy. First-argument completion combines configured shortcuts
   with dynamic candidates, including for an empty token, without duplicates.
7. Bash, zsh, and fish must preserve backend fuzzy candidates and quote names
   safely. POSIX sh has no interactive completion engine. Audit PowerShell's
   existing integration and wire its supported completion entry point without
   changing its navigation wrapper contract.
8. `--print` emits one existing directory on stdout. Diagnostics use stderr;
   missing/deleted targets, cancellation, and unresolved ambiguity must not
   emit a bogus destination. Completion errors remain quiet and bounded.

## Implementation sequence

### 1. Share navigation identities

Extend the existing discovery/index boundary with a compact representation
containing slug, source path, destination, type, and optional ref. Reuse workitem
discovery rules. Deduplicate targets by source identity/path while preserving
project shortcuts on existing targets. Keep source path distinct from a file's
parent destination. If the persisted index shape changes, bump its version.

First measure existing full discovery against the completion deadline. If it
cannot fit, extract a lightweight discovery projection in `internal/workitem`
that reuses the existing traversal and marker parsers, omitting summaries,
timestamp scans, and runtime enrichment. Do not add shell-side filesystem scans.

### 2. Resolve and complete consistently

Extend index matching with exact source paths and refs before fuzzy slug
matching. Retain configured-shortcut and pin precedence at the command layer.
Replace name-keyed candidate collapse with identity-preserving matching. Make
duplicate-name completions round-trip through their canonical source paths.
Ensure exact identifiers that fail do not accidentally fuzzy-match another item.

Audit `camp go -c`: the current category execution branch bypasses query
resolution. Define workitem-targeted command execution through the same resolved
directory before exposing it in examples; keep that correction focused on the
new target behavior rather than refactoring unrelated command handling.

### 3. Connect shell completion

Remove the ordinary first-token dynamic-completion restriction in the supported
shell templates. Preserve static shortcuts as a fallback when backend completion
times out. Pass the current token to the backend and avoid reapplying prefix-only
filters to fuzzy results. Verify `camp go` and `cgo` parity where their completion
entry points are shared. Regenerate CLI docs only when command metadata changes.

### 4. Prove the user journey

Use the existing containerized integration harness for all filesystem mutation
fixtures. Unit tests should cover pure matching/projection logic with in-memory
models. Use the real shell/PTY harness for completion; capturing backend output
alone is insufficient. Journey recordings belong in the registered
`social-fitness-app` camp, discovered with `camp list`, per camp conventions.

| Scenario | Required evidence |
| --- | --- |
| Create design, explore, and custom feature items | Partial-name jump and first-token Tab find each item immediately |
| Prefix, interior substring, and noncontiguous fuzzy query | Backend and actual shell agree on offered candidates |
| Same slug in two workflow types | Both paths visible; each selected token reaches its displayed destination |
| Slug collides with shortcut, pin, or project | Existing precedence preserved; explicit item path/ref remains usable |
| File-backed workitem and intent | Selected source resolves to its existing parent directory |
| Rename/move/delete with warm cache | New path appears; old path never escapes on stdout |
| Hidden/dungeon or unmarked custom directory | Not included by broad workitem discovery |
| Configured workflow location | Discovery and navigation agree outside default layout |
| Spaces/metacharacters in paths | Completion inserts a safe token and reaches the correct directory |
| Missing item, cancellation, malformed metadata | No false success, hanging shell, or polluted completion output |
| Existing category, project, worktree, toggle navigation | Regression fixtures retain behavior |

Run targeted container integration and real shell proofs, then the repository's
required `just gate-fast` before the implementation PR. For user-facing wording,
follow `docs/terminology.md` and its compatibility checks. Record exact commands,
results, and any platform not exercised; a passing build alone is insufficient.

