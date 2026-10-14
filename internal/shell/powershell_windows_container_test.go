//go:build windows && container_fs

package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPowerShellUnicodeDirectoryNavigation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "Project With Spaces 日本語")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	runPowerShellNavigationProbe(t, destination)
}

func TestPowerShellBrowserPathOutput(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "Project With Spaces 日本語 [one]")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := Generate("powershell")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	probe := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$global:campBrowseRoot = %s
$global:campBrowseDestination = %s
$global:campBrowseMode = 'ok'
function global:_camp_is_interactive { return $true }
function global:_camp_native {
    $global:campBrowseArgs = @($args)
    $pathIndex = [Array]::IndexOf($args, '--path-output')
    if ($pathIndex -ge 0) { $global:campBrowseSelectionFile = [string]$args[$pathIndex + 1] }
    if ($global:campBrowseMode -eq 'failure') { $global:LASTEXITCODE = 7; return }
    $global:LASTEXITCODE = 0
    if ($pathIndex -lt 0) { return }
    if ($global:campBrowseMode -eq 'cancel') { return }
    $selection = $global:campBrowseDestination
    if ($global:campBrowseMode -eq 'stale') { $selection = 'Z:\camp-browser-missing' }
    if ($global:campBrowseMode -eq 'remote') { $selection = 'ssh-hop:machine:camp' }
    [IO.File]::WriteAllText($global:campBrowseSelectionFile, $selection, (New-Object System.Text.UTF8Encoding($false)))
}

Set-Location -LiteralPath $global:campBrowseRoot
camp list
if ((Get-Location).Path -ne $global:campBrowseDestination) { throw 'camp list did not navigate to the UTF-8 selection' }
if (($global:campBrowseArgs[0] -ne 'list') -or (-not ($global:campBrowseArgs -contains '--path-output'))) { throw 'camp list did not request path output' }
if (Test-Path -LiteralPath $global:campBrowseSelectionFile) { throw 'camp list left its selection file behind' }

Set-Location -LiteralPath $global:campBrowseRoot
camp project ls
if ((Get-Location).Path -ne $global:campBrowseDestination) { throw 'camp project ls did not navigate' }
if (($global:campBrowseArgs[0..1] -join '|') -ne 'project|list') { throw 'project list argv changed' }

Set-Location -LiteralPath $global:campBrowseRoot
camp quest list
if ((Get-Location).Path -ne $global:campBrowseDestination) { throw 'camp quest list did not navigate' }
if (($global:campBrowseArgs[0..1] -join '|') -ne 'quest|list') { throw 'quest list argv changed' }

Set-Location -LiteralPath $global:campBrowseRoot
foreach ($call in @({ camp list --json }, { camp project list --count }, { camp quest list --json })) {
    & $call
    if ($global:campBrowseArgs -contains '--path-output') { throw 'scriptable command unexpectedly requested path output' }
    if ((Get-Location).Path -ne $global:campBrowseRoot) { throw 'scriptable command changed location' }
}

$global:campBrowseMode = 'cancel'
camp list
if ((Get-Location).Path -ne $global:campBrowseRoot) { throw 'cancelled list changed location' }
if (Test-Path -LiteralPath $global:campBrowseSelectionFile) { throw 'cancelled list left its selection file behind' }

foreach ($mode in @('failure', 'stale', 'remote')) {
    $global:campBrowseMode = $mode
    $failed = $false
    try { camp list } catch {
        $failed = $true
        if ($mode -eq 'remote' -and $_.Exception.Message -notlike '*Remote camp hopping is unavailable*') {
            throw 'remote selection did not explain the unsupported hop'
        }
    }
    if (-not $failed -or (Get-Location).Path -ne $global:campBrowseRoot) { throw "$mode list failed to preserve location" }
    if (Test-Path -LiteralPath $global:campBrowseSelectionFile) { throw "$mode list left its selection file behind" }
}

function global:_camp_is_interactive { return $false }
$global:campBrowseMode = 'ok'
camp list
if ($global:campBrowseArgs -contains '--path-output') { throw 'noninteractive list requested path output' }
if ((Get-Location).Path -ne $global:campBrowseRoot) { throw 'noninteractive list changed location' }
'PowerShell browser path output passed'
`, quote(root), quote(destination))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script+probe)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PowerShell browser path output passed") {
		t.Fatalf("PowerShell browser path output proof failed: %v\n%s", err, out)
	}
}
