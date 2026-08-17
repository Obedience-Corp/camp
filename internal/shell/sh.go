package shell

// generateSh returns the POSIX sh initialization script, for dash, busybox
// ash, and other Bourne-family shells that are neither bash nor zsh. It shares
// the posix_core blocks with the bash script and adds nothing on top: POSIX sh
// has no programmable completion, so the completion functions bash installs
// have no equivalent to generate here.
func generateSh() string {
	out, err := renderTemplate("sh.sh.tmpl", templateData{})
	if err != nil {
		// Template is embedded and parsed at init; failure here is a programming error.
		panic("shell: sh template render failed: " + err.Error())
	}
	return out
}
