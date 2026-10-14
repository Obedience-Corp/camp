//go:build windows

package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The child process changes only its own working directory. It creates no files.
func TestPowerShellNavigationFunctionsInFreshSession(t *testing.T) {
	destination := os.Getenv("ProgramFiles")
	if destination == "" {
		t.Fatal("ProgramFiles is required for a path-with-spaces navigation proof")
	}
	if !strings.Contains(destination, " ") {
		t.Fatalf("ProgramFiles directory %q does not exercise spaces", destination)
	}
	runPowerShellNavigationProbe(t, destination)
}

// The child changes only its process encoding; it creates no fixture files.
func TestPowerShellNativeRestoresOutputEncoding(t *testing.T) {
	script, err := Generate("powershell")
	if err != nil {
		t.Fatal(err)
	}
	const probe = `
$ErrorActionPreference = 'Stop'
function global:_camp_native_executable { return 'Test-CampNative' }
function Test-CampNative {
    if ([Console]::OutputEncoding.CodePage -ne 65001) { throw 'native command did not receive UTF-8 output encoding' }
    if ($global:campTestFail) { throw 'mock native failure' }
    $global:LASTEXITCODE = 0
    return 'Project 日本語'
}
[Console]::OutputEncoding = New-Object System.Text.ASCIIEncoding
$previousCodePage = [Console]::OutputEncoding.CodePage
if ((_camp_native go p --print) -ne 'Project 日本語') { throw 'native output changed' }
if ([Console]::OutputEncoding.CodePage -ne $previousCodePage) { throw 'successful native call changed session encoding' }
$global:campTestFail = $true
$failed = $false
try { _camp_native go missing --print } catch { $failed = $_.Exception.Message -eq 'mock native failure' }
if (-not $failed) { throw 'mock native failure did not propagate' }
if ([Console]::OutputEncoding.CodePage -ne $previousCodePage) { throw 'failed native call changed session encoding' }
'PowerShell native encoding passed'
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script+probe)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PowerShell native encoding passed") {
		t.Fatalf("fresh PowerShell native encoding proof failed: %v\n%s", err, out)
	}
}

func runPowerShellNavigationProbe(t *testing.T, destination string) {
	t.Helper()
	script, err := Generate("powershell")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(destination); err != nil || !info.IsDir() {
		t.Fatalf("destination directory %q unavailable: %v", destination, err)
	}
	root := filepath.VolumeName(destination) + `\`
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	probe := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$global:campTestPath = %s
$global:campTestRoot = %s
$global:campTestMode = 'ok'
function global:_camp_native {
    $global:campTestArgs = @($args)
    if ($global:campTestMode -eq 'fail') { $global:LASTEXITCODE = 7; return }
    $global:LASTEXITCODE = 0
    if ($global:campTestMode -eq 'stale') { return 'Z:\camp-missing-target' }
    return $global:campTestPath
}

cgo p
if ((Get-Location).Path -ne $global:campTestPath) { throw 'cgo did not change the current directory' }
if (($global:campTestArgs -join '|') -ne 'go|p|--print') { throw 'cgo did not preserve argv' }

$global:campTestPath = $global:campTestRoot
csw local-camp
if ((Get-Location).Path -ne $global:campTestRoot) { throw 'csw did not change the current directory' }
if (($global:campTestArgs -join '|') -ne 'switch|local-camp|--print') { throw 'csw did not preserve argv' }

$before = (Get-Location).Path
$global:campTestMode = 'fail'
$failed = $false
try { csw missing-camp } catch { $failed = $true }
if (-not $failed -or (Get-Location).Path -ne $before) { throw 'failed switch changed location or reported success' }

$global:campTestMode = 'stale'
$failed = $false
try { cgo stale } catch { $failed = $true }
if (-not $failed -or (Get-Location).Path -ne $before) { throw 'stale go path changed location or reported success' }

$global:campTestMode = 'ok'
$printed = cgo --print
if ($printed -ne $global:campTestRoot -or (Get-Location).Path -ne $before) { throw '--print did not pass through without navigation' }
if (($global:campTestArgs -join '|') -ne 'go|--print') { throw '--print was changed by the wrapper' }

'PowerShell navigation functions passed'
`, quote(destination), quote(root))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script+probe)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PowerShell navigation functions passed") {
		t.Fatalf("fresh PowerShell navigation proof failed: %v\n%s", err, out)
	}
}
