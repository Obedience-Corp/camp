// Package settings declares the catalog of settings files that the
// `camp settings` TUI knows about. The catalog is the single authority for
// which files are shown, how their paths are displayed, and whether they may be
// edited. It is kept separate from internal/config (which owns the load/save
// code the catalog points at) so the catalog can import config for paths
// without an import cycle.
package settings

// Scope is where a settings file lives and how its path is displayed.
type Scope int

const (
	// ScopeLocal files live under the campaign root and render with a
	// campaign-root-relative path, e.g. .campaign/campaign.yaml.
	ScopeLocal Scope = iota
	// ScopeGlobal files live on the machine and render with a tilde-based
	// path, e.g. ~/.obey/campaign/registry.json.
	ScopeGlobal
)

// Edit is how (or whether) the TUI may edit a file.
type Edit int

const (
	// EditStructured means a hand-authored form exists for this file.
	EditStructured Edit = iota
	// EditReadOnly means the file is shown but not editable here.
	EditReadOnly
	// EditHidden means the file is never listed (machine-managed).
	EditHidden
	// EditSecret means the file is never listed and never read
	// (e.g. ~/.obey/.env).
	EditSecret
)

// Format is the on-disk encoding of a settings file, used for display and to
// pick the right parser/serializer.
type Format int

const (
	// FormatYAML is a YAML-encoded file.
	FormatYAML Format = iota
	// FormatJSON is a JSON-encoded file.
	FormatJSON
)

// SettingEntry declares one settings file the TUI knows about.
type SettingEntry struct {
	// ID is a stable identifier, e.g. "campaign_manifest".
	ID string
	// Title is the menu label, e.g. "Campaign manifest".
	Title string
	// Desc is a one-line description shown under the row.
	Desc string
	// Scope is where the file lives and how its path is displayed.
	Scope Scope
	// Path is the file location. For ScopeLocal it is relative to the
	// campaign root; for ScopeGlobal it is the real resolved path.
	Path string
	// Format is the on-disk encoding.
	Format Format
	// Edit is how (or whether) the TUI may edit the file.
	Edit Edit
	// Owner is the component that manages the file, e.g. "camp", "daemon",
	// "fest".
	Owner string
}

// Editable reports whether this entry may be edited from the settings TUI.
func (e SettingEntry) Editable() bool { return e.Edit == EditStructured }

// Listable reports whether this entry appears in a menu at all.
func (e SettingEntry) Listable() bool {
	return e.Edit == EditStructured || e.Edit == EditReadOnly
}
