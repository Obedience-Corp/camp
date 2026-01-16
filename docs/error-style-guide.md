# Error Message Style Guide

Guidelines for writing user-friendly error messages in the camp CLI.

## Principles

1. **Be helpful, not just accurate** - Tell users how to fix the problem
2. **Include context** - Show relevant values (paths, names, etc.)
3. **Suggest actions** - End with "Hint:" or "Try:" suggestions
4. **Avoid jargon** - Use plain language, not technical terms

## Format

```
<What went wrong>
Hint: <How to fix it>
```

### Examples

**Good:**
```
not inside a campaign directory
Hint: Run 'camp init' to create a campaign, or navigate to an existing one
```

**Bad:**
```
not inside a campaign directory
```

## Error Types

### Sentinel Errors

Use for programmatic checking. Include hints in the message:

```go
var ErrNotInCampaign = errors.New("not inside a campaign directory\n" +
    "Hint: Run 'camp init' to create a campaign, or navigate to an existing one")
```

### Structured Errors

Use for context-rich errors (like `DirectJumpError`):

```go
type DirectJumpError struct {
    Category Category
    Path     string
    Err      error
}

func (e *DirectJumpError) Error() string {
    if errors.Is(e.Err, ErrCategoryNotFound) {
        return fmt.Sprintf("category directory not found: %s (expected at %s)",
            e.Category, e.Path)
    }
    // ...
}
```

### Wrapped Errors

Include the context of what you were trying to do:

```go
// Good - says what failed AND why
return fmt.Errorf("failed to add submodule: %w\n%s", err, output)

// Bad - just passes through
return err
```

## Hint Guidelines

1. **Suggest commands** - `Run 'camp list' to see registered campaigns`
2. **Explain alternatives** - `or navigate to an existing one`
3. **Be specific** - Don't say "check the input", say what to check

## What NOT to Do

1. **Don't blame the user** - Say "not found" not "you entered wrong"
2. **Don't use technical jargon** - Say "directory" not "filesystem path"
3. **Don't be vague** - Include the actual value that was wrong
4. **Don't panic** - Always return errors, never panic in user-facing code

## Testing Errors

Test that errors are helpful:

```bash
# Campaign detection
cd /tmp && camp go p
# Should show hint about 'camp init'

# Unknown campaign
camp unregister nonexistent
# Should suggest 'camp list'
```
