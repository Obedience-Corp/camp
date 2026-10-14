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
$global:campTestNative = (Get-Command powershell.exe -CommandType Application | Select-Object -First 1).Source
function global:_camp_native_executable { return $global:campTestNative }
$originalEncoding = [Console]::OutputEncoding
$changedEncoding = $false
try {
    [Console]::OutputEncoding = New-Object System.Text.ASCIIEncoding
    $changedEncoding = $true
} catch {
    if (-not [Console]::IsOutputRedirected) { throw }
}
$previousCodePage = [Console]::OutputEncoding.CodePage
$childScript = '$text = "Project " + [char]0x65E5 + [char]0x672C + [char]0x8A9E; $bytes = (New-Object System.Text.UTF8Encoding($false)).GetBytes($text + [char]10); $stream = [Console]::OpenStandardOutput(); $stream.Write($bytes, 0, $bytes.Length)'
$childArgs = @('-NoProfile', '-NonInteractive', '-Command', $childScript)
try {
    # Exercise the direct UTF-8 pipe path even on hosts where the console
    # setter succeeds. The -Command argument includes spaces and quotes.
    if ((@(_camp_native_utf8 -Native $global:campTestNative -Arguments $childArgs) -join '') -ne 'Project 日本語') {
        throw 'UTF-8 fallback changed Unicode native output or quoted arguments'
    }
    if ($LASTEXITCODE -ne 0) { throw "UTF-8 fallback exit code was $LASTEXITCODE" }
    $null = _camp_native_utf8 -Native $global:campTestNative -Arguments @('-NoProfile', '-NonInteractive', '-Command', 'exit 7')
    if ($LASTEXITCODE -ne 7) { throw "UTF-8 fallback failure exit code was $LASTEXITCODE" }

    if ($changedEncoding) {
        function Test-CampNative {
            if ([Console]::OutputEncoding.CodePage -ne 65001) { throw 'native command did not receive UTF-8 output encoding' }
            if ($global:campTestFail) { throw 'mock native failure' }
            $global:LASTEXITCODE = 0
            return 'Project 日本語'
        }
        function global:_camp_native_executable { return 'Test-CampNative' }
        if ((_camp_native go p --print) -ne 'Project 日本語') { throw 'native output changed' }
        if ([Console]::OutputEncoding.CodePage -ne $previousCodePage) { throw 'successful native call changed session encoding' }
        $global:campTestFail = $true
        $failed = $false
        try { _camp_native go missing --print } catch { $failed = $_.Exception.Message -eq 'mock native failure' }
        if (-not $failed) { throw 'mock native failure did not propagate' }
        if ([Console]::OutputEncoding.CodePage -ne $previousCodePage) { throw 'failed native call changed session encoding' }
    } else {
        # On redirected Server Core the wrapper itself must take the fallback.
        if ((@(_camp_native @childArgs) -join '') -ne 'Project 日本語') { throw 'redirected native output changed' }
        if ($LASTEXITCODE -ne 0) { throw "redirected native exit code was $LASTEXITCODE" }
        if ([Console]::OutputEncoding.CodePage -ne $previousCodePage) { throw 'fallback changed session encoding' }
    }
} finally {
    if ($changedEncoding) { [Console]::OutputEncoding = $originalEncoding }
}
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
