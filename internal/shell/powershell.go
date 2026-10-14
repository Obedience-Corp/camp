package shell

// generatePowerShell returns integration for Windows PowerShell and PowerShell 7.
func generatePowerShell() string {
	out, err := renderTemplate("powershell.ps1.tmpl", templateData{})
	if err != nil {
		// The template is embedded and parsed at init.
		panic("shell: powershell template render failed: " + err.Error())
	}
	return out
}
