package shell

// generateFish returns the fish initialization script.
// Shortcuts are injected dynamically from config.DefaultNavigationShortcuts().
func generateFish() string {
	out, err := renderTemplate("fish.sh.tmpl", templateData{
		ShortcutCompletions: fishShortcutCompletions(),
	})
	if err != nil {
		// Template is embedded and parsed at init; failure here is a programming error.
		panic("shell: fish template render failed: " + err.Error())
	}
	return out
}
