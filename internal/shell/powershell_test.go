package shell

import (
	"strings"
	"testing"
)

func TestGeneratePowerShellNavigation(t *testing.T) {
	script, err := Generate("powershell")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"function global:camp {",
		"function global:cgo {",
		"function global:csw {",
		"Get-Command -Name camp -CommandType Application",
		"_camp_native go @rest --print",
		"_camp_native switch @rest --print",
		"Test-Path -LiteralPath $destination -PathType Container",
		"Set-Location -LiteralPath $destination -ErrorAction Stop",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("PowerShell integration missing %q", want)
		}
	}
	if strings.Contains(script, "Invoke-Expression $destination") {
		t.Fatal("navigation must treat the CLI path as data, not shell code")
	}
}
