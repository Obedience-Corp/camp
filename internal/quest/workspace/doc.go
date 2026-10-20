// Package workspace owns the Camp quest-workspace/v1alpha1 contract: validation,
// agent-tab push merge, ui-state.lock OS locking, generation checks, and durable
// atomic writes for .campaign/quests/<quest>/ui-state.json.
//
// On-disk documents remain festival-app version 2 (workspaceTabs + paneTree).
// Camp is the offline producer; the desktop app owns interactive save/merge.
// Ordinary Camp use never requires the app or camp-app plugin.
package workspace
