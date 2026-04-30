// Package crawl provides reusable interactive primitives for
// "crawl-style" review loops: walk a list of items, present a small
// menu per item, optionally pick a destination, and accumulate a
// summary of decisions.
//
// The package is deliberately narrow. It owns prompt mechanics,
// option/decision shapes, and cancellation handling. It does NOT
// know about dungeon items, intents, audit logs, git commits, or
// any other domain. Domain packages adapt their own items into
// crawl primitives at the call boundary.
package crawl

import camperrors "github.com/Obedience-Corp/camp/internal/errors"

// ErrAborted is returned when the user aborts the crawl
// (typically via ctrl+c). Callers should match with errors.Is.
var ErrAborted = camperrors.Wrap(camperrors.ErrCancelled, "crawl aborted")

// Action is a high-level decision a user can make about an item.
// Domain packages may define their own actions in addition to the
// canonical ones below; the prompt layer treats Action as an opaque
// string identifier on Option entries.
type Action string

const (
	// ActionKeep leaves the item in its current location.
	ActionKeep Action = "keep"
	// ActionMove moves the item to a destination chosen in the
	// second-step prompt.
	ActionMove Action = "move"
	// ActionSkip records a skip decision and continues.
	ActionSkip Action = "skip"
	// ActionQuit ends the crawl session early. Already-applied
	// decisions are still summarized.
	ActionQuit Action = "quit"
)

// Item is the minimal display shape for an item being crawled.
// Domain packages convert their richer item types into Item before
// calling the prompt layer.
type Item struct {
	// ID identifies the item for logging and idempotency.
	ID string
	// Title is the short heading shown in prompts.
	Title string
	// Description is a free-form info line shown beneath the title.
	Description string
}

// Option is one selectable choice in a prompt.
//
// Used in two places:
//   - First-step prompts: Action is set (keep/move/skip/quit/...);
//     Target is unused.
//   - Second-step destination prompts: Action is typically
//     ActionMove; Target identifies the destination; Count carries
//     the number of items currently at that destination, when known.
type Option struct {
	// Label is the human-readable text shown in the prompt.
	Label string
	// Action is the high-level action this option represents.
	Action Action
	// Target identifies a destination for move-style options.
	// Empty for non-move options.
	Target string
	// RequiresReason is true when selecting this option must be
	// followed by a reason prompt.
	RequiresReason bool
	// Count is the number of items currently at this destination.
	// Used by destination pickers to render counts. Zero means
	// "no count to show".
	Count int
}
