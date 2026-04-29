package crawl

import (
	"github.com/Obedience-Corp/camp/internal/crawl"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// destinationDisplayOrder controls how destinations appear in the
// picker. Live statuses come first, then dungeon statuses in
// canonical lifecycle order.
var destinationDisplayOrder = []intent.Status{
	intent.StatusInbox,
	intent.StatusReady,
	intent.StatusActive,
	intent.StatusDone,
	intent.StatusKilled,
	intent.StatusArchived,
	intent.StatusSomeday,
}

// firstStepOptions returns the first-step menu options for an intent.
// The "Keep" label embeds the current status so the user does not
// have to remember it.
func firstStepOptions(in *intent.Intent) []crawl.Option {
	return []crawl.Option{
		{Label: "Keep in " + string(in.Status), Action: crawl.ActionKeep},
		{Label: "Move to another status", Action: crawl.ActionMove},
		{Label: "Skip", Action: crawl.ActionSkip},
		{Label: "Quit", Action: crawl.ActionQuit},
	}
}

// destinationOptions returns the destination picker options for the
// given intent and current per-status counts.
//
// Rules (per design 02-command-and-ux.md):
//   - omit the intent's current status
//   - include all live statuses + dungeon statuses
//   - omit inbox and ready when promoted_to is set
//   - dungeon destinations carry RequiresReason
//   - include item counts
func destinationOptions(in *intent.Intent, counts map[intent.Status]int) []crawl.Option {
	out := make([]crawl.Option, 0, len(destinationDisplayOrder)-1)
	for _, status := range destinationDisplayOrder {
		if status == in.Status {
			continue
		}
		if !destinationAllowed(in, status) {
			continue
		}
		out = append(out, crawl.Option{
			Label:          string(status),
			Action:         crawl.ActionMove,
			Target:         string(status),
			RequiresReason: status.InDungeon(),
			Count:          counts[status],
		})
	}
	return out
}

// destinationAllowed returns true if status is a valid move target
// for in. The promoted_to rule blocks live re-entry to inbox/ready.
func destinationAllowed(in *intent.Intent, status intent.Status) bool {
	if in.PromotedTo != "" {
		switch status {
		case intent.StatusInbox, intent.StatusReady:
			return false
		}
	}
	return true
}

// countsByStatus is a small helper that turns the
// IntentService.Count return value into a status-keyed map.
func countsByStatus(counts []intent.StatusCount) map[intent.Status]int {
	out := make(map[intent.Status]int, len(counts))
	for _, c := range counts {
		out[c.Status] = c.Count
	}
	return out
}
