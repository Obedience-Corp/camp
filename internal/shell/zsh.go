package shell

// generateZsh returns the zsh initialization script.
// Shortcuts are injected dynamically from config.DefaultNavigationShortcuts().
func generateZsh() string {
	out, err := renderTemplate("zsh.sh.tmpl", templateData{
		ShortcutTargets: zshShortcutTargets(),
	})
	if err != nil {
		// Template is embedded and parsed at init; failure here is a programming error.
		panic("shell: zsh template render failed: " + err.Error())
	}
	return out
}
