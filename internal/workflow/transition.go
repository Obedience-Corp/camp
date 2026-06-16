package workflow

import (
	"fmt"
)

// Transition represents a status transition for an item.
type Transition struct {
	Item string // Item name
	From string // Source status
	To   string // Destination status
}

// CommitMessage returns a formatted commit message for this transition.
func (t Transition) CommitMessage() string {
	return fmt.Sprintf("flow: move %s from %s to %s", t.Item, t.From, t.To)
}
