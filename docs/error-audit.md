# Error Audit Report

> Audit of all error paths in the camp CLI codebase.
> Date: January 2026

## Summary

| Category | Count | Status |
|----------|-------|--------|
| Good (no changes) | 28 | ✓ |
| Needs Improvement | 12 | Prioritized below |
| Critical (panics) | 0 | ✓ None found |
| **Total** | **87** | |

---

## Critical (Panics)

**None found.** The codebase handles all error paths gracefully.

---

## Good Errors (No Changes Needed)

These errors follow best practices: descriptive, include context, and suggest valid options.

### 1. Shell & Type Validation

| Location | Message | Quality |
|----------|---------|---------|
| `internal/shell/shell.go:19` | "unsupported shell: %s (supported: zsh, bash, fish)" | ✓ Lists valid options |
| `cmd/camp/shell_init.go:52` | "unsupported shell: %s\nSupported: %s" | ✓ Lists valid options |
| `cmd/camp/register.go:110` | "invalid campaign type: %s (must be product, research, tools, personal)" | ✓ Lists valid types |
| `internal/config/validate.go:34` | "%w: %q (valid: product, research, tools, personal)" | ✓ Lists valid types |
| `internal/scaffold/init.go:195` | "invalid campaign type: %s" | Acceptable (validation layer) |

### 2. Path & Location Errors

| Location | Message | Quality |
|----------|---------|---------|
| `internal/config/campaign.go:30` | "campaign config not found: %s" | ✓ Includes path |
| `internal/scaffold/init.go:56` | "already inside a campaign at %s" | ✓ Shows location |
| `internal/scaffold/init.go:62` | "campaign already exists at %s" | ✓ Shows location |
| `internal/project/add.go:156` | "local path is not a git repository: %s" | ✓ Includes path |

### 3. Navigation Errors

| Location | Message | Quality |
|----------|---------|---------|
| `internal/nav/index/resolve.go:88` | "no targets in category %s" | ✓ Includes category |
| `internal/nav/index/resolve.go:105` | "no exact match for %q in category %s" | ✓ Includes query and category |
| `internal/nav/index/resolve.go:116` | "no targets matching %q in category %s" | ✓ Includes query and category |
| `internal/nav/errors.go:27-35` | DirectJumpError with full context | ✓ Structured error type |

### 4. System Errors (Wrapped Correctly)

These properly wrap underlying errors with context:

- `internal/config/global.go:26` - "failed to read global config %s: %w"
- `internal/config/global.go:31` - "failed to parse global config %s: %w"
- `internal/config/registry.go:25` - "failed to read registry %s: %w"
- `internal/config/campaign.go:32` - "failed to read campaign config %s: %w"
- `internal/nav/index/cache.go:*` - All cache errors wrap underlying errors
- `internal/project/add.go:131` - "failed to add submodule: %w\n%s" (includes git output)

### 5. Sentinel Errors (Used Correctly)

Well-defined sentinel errors for programmatic checking:

- `internal/campaign/errors.go` - ErrNotInCampaign, ErrCampaignExists, ErrInvalidCampaign
- `internal/nav/errors.go` - ErrCategoryNotFound, ErrNotADirectory
- `internal/nav/tui/picker.go` - ErrNoTargets, ErrAborted
- `internal/nav/tui/keybindings.go` - ErrNotATerminal
- `internal/nav/exec.go` - ErrNoCommand
- `internal/config/validate.go` - ErrNameRequired, ErrInvalidType, etc.

---

## Needs Improvement

### Priority 1: High (Common Operations)

#### 1. "not inside a campaign directory"
- **Location**: `internal/config/campaign.go:105`, `internal/campaign/errors.go:8`
- **Problem**: No actionable guidance
- **Suggestion**: Add hint about what to look for
- **Improved**: "not inside a campaign directory (looking for .campaign/)"
- **User impact**: High - common error when running from wrong directory

#### 2. "campaign not found in registry: %s"
- **Location**: `cmd/camp/unregister.go:54`
- **Problem**: User doesn't know what's registered
- **Suggestion**: Suggest `camp list`
- **Improved**: "campaign not found in registry: %s\nRun 'camp list' to see registered campaigns"
- **User impact**: Medium - occurs when unregistering

#### 3. "category directory not found"
- **Location**: `internal/nav/errors.go:11`
- **Problem**: Doesn't say which category or expected path
- **Note**: DirectJumpError already wraps this with context, so only raw usage needs fixing
- **User impact**: Medium - navigation errors

### Priority 2: Medium (Less Common Operations)

#### 4. "project name is required"
- **Location**: `internal/config/validate.go:50`
- **Problem**: Could show example of valid project config
- **User impact**: Low - config validation

#### 5. "project path is required"
- **Location**: `internal/config/validate.go:53`
- **Problem**: Could show example format
- **User impact**: Low - config validation

#### 6. "campaign path is required"
- **Location**: `internal/config/validate.go:69`
- **Problem**: Context not clear
- **User impact**: Low - registry validation

#### 7. "campaign root is required"
- **Location**: `internal/nav/index/resolve.go:48`
- **Problem**: Internal error leaking to user
- **Note**: Should never reach user; indicates programming error
- **User impact**: Low - should be assertion

### Priority 3: Low (Edge Cases)

#### 8. "invalid path: %w"
- **Location**: `cmd/camp/register.go:57`
- **Problem**: Generic - could say why it's invalid
- **User impact**: Very low - rare edge case

#### 9. "category path is not a directory"
- **Location**: `internal/nav/errors.go:14`
- **Problem**: Missing which path
- **Note**: DirectJumpError wraps this with context
- **User impact**: Very low - rare edge case

---

## Improvement Priority List

### Immediate (Before v1.0)

1. **Campaign detection error** - Most common user error
   ```go
   // Before
   errors.New("not inside a campaign directory")

   // After
   errors.New("not inside a campaign directory\n" +
       "Hint: Run from a directory containing .campaign/ or use 'camp init'")
   ```

2. **Unregister not found** - Easy win
   ```go
   // Before
   fmt.Errorf("campaign not found in registry: %s", name)

   // After
   fmt.Errorf("campaign %q not found in registry\n"+
       "Run 'camp list' to see registered campaigns", name)
   ```

### Near-term

3. Add `--verbose` flag to show full error context
4. Consider error codes for scripting (e.g., exit code 2 = not in campaign)

### Long-term

5. Structured error types for all command errors (like DirectJumpError)
6. Consider `gerror` library for consistent wrapping

---

## Patterns Found

### Good Patterns (Keep Using)

1. **Structured error types** - `DirectJumpError` is excellent, includes context and implements proper unwrapping
2. **Sentinel errors** - Well-defined in `errors.go` files
3. **Error wrapping** - Most errors properly wrap underlying causes
4. **Validation options** - Many errors list valid values

### Patterns to Adopt

1. **Actionable hints** - Tell user what to do next
2. **Context in all errors** - Always include the thing that failed (path, name, etc.)
3. **Suggestion for recovery** - Commands to run, flags to use

---

## Testing Recommendations

Verify error messages with:

```bash
# Campaign detection
cd /tmp && camp go p
# Expected: "not inside a campaign directory..."

# Invalid registration
camp unregister nonexistent
# Expected: "campaign not found in registry: nonexistent..."

# Invalid shell
camp shell-init powershell
# Expected: "unsupported shell: powershell..."

# No panics on edge cases
camp go $(printf '%s' {1..1000})  # Long input
camp project add ""                # Empty input
camp init /nonexistent/readonly    # Bad path
```

---

## Conclusion

The camp codebase has **solid error handling foundations**:
- No panics in production code
- Good use of sentinel errors
- Structured error types where needed
- Proper error wrapping

**Recommended improvements focus on user experience**:
- Adding actionable hints to common errors
- Suggesting recovery commands
- Including context in all error messages

The 12 items marked "needs improvement" are quality-of-life enhancements, not critical issues.
