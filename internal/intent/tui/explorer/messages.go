// Package explorer provides the Intent Explorer TUI component.
package explorer

import "github.com/obediencecorp/camp/internal/intent"

// intentsLoadedMsg is sent when intents are loaded from the service.
type intentsLoadedMsg struct {
	intents []*intent.Intent
	err     error
}

// editorFinishedMsg is sent when an external editor closes.
type editorFinishedMsg struct {
	err  error
	path string
}

// openFinishedMsg is sent when system open completes.
type openFinishedMsg struct {
	err error
}

// moveFinishedMsg is sent when an intent move completes.
type moveFinishedMsg struct {
	err       error
	intentID  string
	newStatus intent.Status
}

// archiveFinishedMsg is sent when archive completes.
type archiveFinishedMsg struct {
	err      error
	intentID string
}

// deleteFinishedMsg is sent when delete completes.
type deleteFinishedMsg struct {
	err   error
	title string
}

// gatherFinishedMsg is sent when gather completes.
type gatherFinishedMsg struct {
	err           error
	gatheredID    string
	gatheredTitle string
	sourceCount   int
}
