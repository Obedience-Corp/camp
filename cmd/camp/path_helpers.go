package main

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/transfer"
)

// resolveTransferArg resolves a move/copy argument to an absolute path.
// @-prefixed paths use campaign shortcuts. Other relative paths resolve to cwd.
func resolveTransferArg(campRoot, arg string, shortcuts map[string]string) (string, error) {
	if strings.HasPrefix(arg, "@") {
		return transfer.ResolveAtPrefix(campRoot, arg, shortcuts)
	}
	return transfer.ResolveCwdRelative(arg)
}

// buildShortcutsMap extracts key->path from campaign ShortcutConfig.
func buildShortcutsMap(cfg *config.CampaignConfig) map[string]string {
	shortcuts := make(map[string]string)
	for key, sc := range cfg.Shortcuts() {
		if sc.HasPath() {
			shortcuts[key] = sc.Path
		}
	}
	return shortcuts
}
