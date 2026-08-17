package shell

// generateBash returns the bash initialization script.
// Shortcuts are injected dynamically from config.DefaultNavigationShortcuts().
func generateBash() string {
	out, err := renderTemplate("bash.sh.tmpl", templateData{
		ShortcutWords: bashShortcutWords(),
		ShellWords:    shellWords(),
	})
	if err != nil {
		// Template is embedded and parsed at init; failure here is a programming error.
		panic("shell: bash template render failed: " + err.Error())
	}
	return out
}
