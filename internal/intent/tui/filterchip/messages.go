// Package filterchip provides interactive filter chip components for TUI.
package filterchip

// FilterChangedMsg is sent when a filter selection changes.
type FilterChangedMsg struct {
	Label string // Which filter changed (e.g., "Type", "Status")
	Value string // The new selected value
	Index int    // Index of the selected option
}

// FilterBarFocusMsg is sent when focus enters/exits the filter bar.
type FilterBarFocusMsg struct {
	Focused bool // Whether the filter bar gained or lost focus
}
