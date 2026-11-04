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
$encodedChild = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($childScript))
$childArgs = @('-NoProfile', '-NonInteractive', '-EncodedCommand', $encodedChild)
try {
    # Exercise direct UTF-8 pipe decoding even when the console setter works.
    # EncodedCommand keeps the child script intact through PS 5.1 argv parsing.
    if ((@(_camp_native_utf8 -Native $global:campTestNative -Arguments $childArgs) -join '') -ne 'Project 日本語') {
        throw 'UTF-8 fallback changed Unicode native output'
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

// All probes replace command discovery and invocation in memory. No plugin or
// fixture files are installed, and only the child process environment changes.
func TestPowerShellAppFollowDiagnostics(t *testing.T) {
	script, err := Generate("powershell")
	if err != nil {
		t.Fatal(err)
	}
	const probe = `
$ErrorActionPreference = 'Stop'
function global:Get-Command {
    param($Name, $CommandType, $ErrorAction)
    if ($Name -ne 'camp-app' -or $CommandType -ne 'Application') { throw 'unexpected command lookup' }
    $global:pluginChecks++
    if (-not $global:pluginMissing) { return [PSCustomObject]@{ Source = 'mock-camp-app' } }
}
function global:_camp_native {
    if (($args -join '|') -ne 'app|terminal|switch-completed|--shell-context|--timeout|5s') { throw 'unexpected follow arguments or deadline' }
    $global:pluginCalls++
    if ($global:pluginMissing) { throw 'missing plugin was invoked' }
    if ($global:pluginFails) { $global:LASTEXITCODE = 8; return }
    $global:LASTEXITCODE = 0
    return '{"campaignId":"destination"}'
}
foreach ($case in @('missing', 'failure', 'success', 'external')) {
    $global:pluginChecks = 0
    $global:pluginCalls = 0
    $global:pluginMissing = $case -eq 'missing' -or $case -eq 'external'
    $global:pluginFails = $case -eq 'failure'
    $env:FESTIVAL_APP_TERMINAL_BINDING = if ($case -eq 'external') { '' } else { 'binding' }
    $env:OBEY_APP_TERMINAL_CONTEXT = 'original'
    $global:LASTEXITCODE = 17
    $errors = New-Object System.IO.StringWriter
    $previousError = [Console]::Error
    try {
        [Console]::SetError($errors)
        _camp_app_follow
    } finally {
        [Console]::SetError($previousError)
    }
    $message = $errors.ToString()
    if ($LASTEXITCODE -ne 17) { throw "$case changed the navigation exit code" }
    switch ($case) {
        'missing' {
            if ($pluginCalls -ne 0 -or $pluginChecks -ne 1) { throw 'missing plugin was dispatched' }
            if ($env:OBEY_APP_TERMINAL_CONTEXT) { throw 'missing plugin retained stale context' }
            if ($message -notmatch 'Install camp-app' -or $message -match 'Run csw again to retry\.') { throw 'missing plugin diagnostic is not actionable' }
        }
        'failure' {
            if ($pluginCalls -ne 1 -or $env:OBEY_APP_TERMINAL_CONTEXT) { throw 'failed sync did not clear context' }
            if ($message -notmatch 'Run csw again to retry\.' -or $message -match 'Install camp-app' -or $message -notmatch 'app synchronization was not confirmed') { throw 'transient failure lost retry advice' }
        }
        'success' {
            if ($pluginCalls -ne 1 -or $env:OBEY_APP_TERMINAL_CONTEXT -ne '{"campaignId":"destination"}' -or $message) { throw 'successful sync failed' }
        }
        'external' {
            if ($pluginChecks -ne 0 -or $pluginCalls -ne 0 -or $env:OBEY_APP_TERMINAL_CONTEXT -ne 'original' -or $message) { throw 'external shell attempted app sync' }
        }
    }
}
'PowerShell app follow diagnostics passed'
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script+probe)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PowerShell app follow diagnostics passed") {
		t.Fatalf("PowerShell app follow diagnostics failed: %v\n%s", err, out)
	}
}
