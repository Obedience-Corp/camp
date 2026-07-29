# Shared brand theme

Camp's TUI adapter resolves `github.com/Obedience-Corp/obey-shared/brand`
semantic roles through `internal/ui/theme`. Huh forms use the configured
adaptive, light, dark, or high-contrast mode. Custom TUIs use the same resolved
palette through `theme.TUI()`, while command output uses the semantic role
constants in `internal/ui/styles.go`.

The adapter preserves Camp's existing configuration keys and plain-output
behavior. `NO_COLOR`, `TERM=dumb`, non-TTY output, CI, and `camp --no-color`
resolve the shared plain palette. `COLORFGBG` is used only to resolve an
adaptive background when it is explicitly available; an unknown background
keeps the shared dark default so startup never performs a blocking terminal
query. Reduced-motion policy remains owned by the shared capabilities API.

Campaign, intent, and workflow categories are intentional local composition of
shared roles (`Accent`, `AccentHighlight`, `StatusWarning`, `StatusSuccess`,
`AccentSubtle`, and `TextMuted`); they do not define another color table. The
few black foregrounds used on highlighted accent backgrounds are deliberate
contrast choices because v0.4.5 does not expose an `OnAccent` role.

Shared logo helpers are reserved for welcome, loading, and completion moments.
Steady-state list, status, and editor surfaces do not animate, which keeps
navigation calm and respects reduced-motion policy; logo placement is reviewed
in the canonical evidence and cross-repository adoption gates.
