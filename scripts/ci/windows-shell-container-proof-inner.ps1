#Requires -Version 5.1
<#
  Runs as ContainerUser in Windows Server Core with the inbox powershell.exe.
  Keep this source ASCII so Windows PowerShell 5.1 reads it consistently; make
  the Unicode fixture name from character codes at runtime.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = 'C:\camp-shell-proof'
$Bin = Join-Path $Root 'bin'
$Camp = Join-Path $Bin 'camp.exe'
if (-not (Test-Path -LiteralPath $Camp)) { throw "missing camp binary: $Camp" }
$CampDev = Join-Path $Bin 'camp-dev.exe'
$Probe = Join-Path $Bin 'argv-probe.exe'
foreach ($required in @($CampDev, $Probe)) {
    if (-not (Test-Path -LiteralPath $required)) { throw "missing proof binary: $required" }
}

$env:PATH = "$Bin;$env:PATH"
$env:HOME = Join-Path $Root 'home'
$env:USERPROFILE = $env:HOME
$env:XDG_CONFIG_HOME = Join-Path $env:HOME 'config'
$env:CAMP_REGISTRY_PATH = Join-Path $env:HOME 'registry.json'
$env:CAMP_NO_DEFER = '1'
Remove-Item Env:CAMP_ROOT -ErrorAction SilentlyContinue

$unicode = -join @([char]0x65E5, [char]0x672C, [char]0x8A9E)
$campRoot = Join-Path $Root "Camp With Spaces $unicode [one]"
$projects = Join-Path $campRoot 'projects'
[void][System.IO.Directory]::CreateDirectory((Join-Path $campRoot '.campaign'))
[void][System.IO.Directory]::CreateDirectory($projects)
[void][System.IO.Directory]::CreateDirectory($env:HOME)
$yaml = "id: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee`nname: shell-proof`ntype: product`n"
[System.IO.File]::WriteAllText((Join-Path $campRoot '.campaign\campaign.yaml'), $yaml,
    (New-Object System.Text.UTF8Encoding($false)))

& $Camp register $campRoot | Out-Null
if ($LASTEXITCODE -ne 0) { throw "camp register failed ($LASTEXITCODE)" }
$integration = & $Camp shell-init powershell | Out-String
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($integration)) {
    throw "camp shell-init powershell failed ($LASTEXITCODE)"
}
Invoke-Expression $integration

$originalCodePage = [Console]::OutputEncoding.CodePage
# A redirected Server Core process may refuse the console encoding setter.
# The shell wrapper must still decode Camp's UTF-8 output and leave the
# original encoding intact in that environment.
try {
    [Console]::OutputEncoding = New-Object System.Text.ASCIIEncoding
    $originalCodePage = [Console]::OutputEncoding.CodePage
} catch {
    if (-not [Console]::IsOutputRedirected) { throw }
}

Set-Location -LiteralPath $campRoot
cgo p
if ((Get-Location).Path -ne $projects) { throw "cgo reached wrong destination: $(Get-Location)" }
if ([Console]::OutputEncoding.CodePage -ne $originalCodePage) { throw 'cgo did not restore output encoding' }

Set-Location -LiteralPath $Root
csw shell-proof
if ((Get-Location).Path -ne $campRoot) { throw "csw reached wrong destination: $(Get-Location)" }
if ([Console]::OutputEncoding.CodePage -ne $originalCodePage) { throw 'csw did not restore output encoding' }

$before = (Get-Location).Path
$failed = $false
try { cgo no-such-camp-shortcut } catch { $failed = $true }
if (-not $failed -or (Get-Location).Path -ne $before) {
    throw 'failed cgo did not preserve the current directory'
}
if ([Console]::OutputEncoding.CodePage -ne $originalCodePage) {
    throw 'failed cgo did not restore output encoding'
}

$failed = $false
try { csw no-such-registered-camp } catch { $failed = $true }
if (-not $failed -or (Get-Location).Path -ne $before) {
    throw 'failed csw did not preserve the current directory'
}
if ([Console]::OutputEncoding.CodePage -ne $originalCodePage) {
    throw 'failed csw did not restore output encoding'
}

Write-Output 'Camp PowerShell 5.1 navigation proof passed'

# Direct CLI commands keep each argument as data, including a help flag after
# the executable. The helper records what a real child process receives.
# Windows PowerShell 5.1's native-command binder discards empty arguments and
# embedded quotes. Build the Windows command line explicitly so this fixture
# can prove what Camp does with values that actually reach its executable.
function ConvertTo-WindowsArgument {
    param([AllowEmptyString()][string]$Value)

    $quoted = New-Object System.Text.StringBuilder
    [void]$quoted.Append('"')
    $backslashes = 0
    foreach ($character in $Value.ToCharArray()) {
        if ($character -eq '\') {
            $backslashes++
            continue
        }
        if ($character -eq '"') {
            [void]$quoted.Append(('\' * (2 * $backslashes + 1)))
            [void]$quoted.Append('"')
            $backslashes = 0
            continue
        }
        if ($backslashes -gt 0) {
            [void]$quoted.Append(('\' * $backslashes))
            $backslashes = 0
        }
        [void]$quoted.Append($character)
    }
    if ($backslashes -gt 0) { [void]$quoted.Append(('\' * (2 * $backslashes))) }
    [void]$quoted.Append('"')
    return $quoted.ToString()
}

function Invoke-LiteralCampCli {
    param([string]$Executable, [string[]]$ArgumentValues)

    $start = New-Object System.Diagnostics.ProcessStartInfo
    $start.FileName = $Executable
    $encodedArguments = New-Object 'System.Collections.Generic.List[string]'
    foreach ($argument in $ArgumentValues) {
        [void]$encodedArguments.Add((ConvertTo-WindowsArgument -Value $argument))
    }
    $start.Arguments = [string]::Join(' ', $encodedArguments)
    $start.WorkingDirectory = (Get-Location).Path
    $start.UseShellExecute = $false
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.CreateNoWindow = $true
    $process = [System.Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEnd()
        $stderr = $process.StandardError.ReadToEnd()
        $process.WaitForExit()
        if ($process.ExitCode -ne 0) {
            throw "Camp CLI exit $($process.ExitCode): $stdout`n$stderr"
        }
    } finally {
        $process.Dispose()
    }
}

$fixture = Join-Path $projects 'fixture'
[void][System.IO.Directory]::CreateDirectory((Join-Path $fixture '.git'))
$wantArgs = @('two words', '', 'O''Brien "quoted"', "$unicode argument", 'a;b', '$(Write-Output injected)', '--help')
$env:CAMP_PROBE_OUT = Join-Path $Root 'run-argv.json'
Set-Location -LiteralPath $campRoot
Invoke-LiteralCampCli -Executable $Camp -ArgumentValues (@('run', $Probe) + $wantArgs)
# Windows PowerShell 5.1 otherwise decodes BOM-less JSON using the local ANSI code page.
$runRecord = Get-Content -LiteralPath $env:CAMP_PROBE_OUT -Raw -Encoding UTF8 | ConvertFrom-Json
if ($runRecord.cwd -ne $campRoot -or $runRecord.camp_root -ne $campRoot) {
    throw "camp run cwd/root mismatch: $($runRecord | ConvertTo-Json -Compress)"
}
if ((@($runRecord.args) -join '|') -ne ($wantArgs -join '|')) {
    throw "camp run argv mismatch: $($runRecord | ConvertTo-Json -Compress)"
}

$help = & $Camp run --help 2>&1 | Out-String
if ($LASTEXITCODE -ne 0 -or $help -notmatch 'Usage:' -or $help -notmatch 'Execute any command') {
    throw "camp run --help failed: $help"
}

$env:CAMP_PROBE_OUT = Join-Path $Root 'project-run-argv.json'
Invoke-LiteralCampCli -Executable $Camp -ArgumentValues (@('project', 'run', '-p', 'fixture', '--', $Probe) + $wantArgs)
$projectRecord = Get-Content -LiteralPath $env:CAMP_PROBE_OUT -Raw -Encoding UTF8 | ConvertFrom-Json
if ($projectRecord.cwd -ne $fixture -or $projectRecord.camp_root -ne $campRoot) {
    throw "camp project run cwd/root mismatch: $($projectRecord | ConvertTo-Json -Compress)"
}
if ((@($projectRecord.args) -join '|') -ne ($wantArgs -join '|')) {
    throw "camp project run argv mismatch: $($projectRecord | ConvertTo-Json -Compress)"
}

# camp flow is a dev-profile command. Exercise its actual registry/CLI path
# with the inbox PowerShell 5.1 interpreter and persisted output.
$flowsDir = Join-Path $campRoot '.campaign\flows'
[void][System.IO.Directory]::CreateDirectory($flowsDir)
$registry = @'
version: 1
flows:
  winargs:
    command: |
      & { [string[]]$received = $args; $result = @{ cwd = $PWD.Path; process_cwd = [IO.Directory]::GetCurrentDirectory(); value = $env:CAMP_FLOW_VALUE; args = $received } | ConvertTo-Json -Compress; [IO.File]::WriteAllText($env:CAMP_FLOW_OUT, $result) }
    workdir: projects/fixture
    env:
      CAMP_FLOW_VALUE: value with spaces
  shmissing:
    command: echo should-not-run
    shell: sh
    workdir: .
'@
[System.IO.File]::WriteAllText((Join-Path $flowsDir 'registry.yaml'), $registry,
    (New-Object System.Text.UTF8Encoding($false)))
$env:CAMP_FLOW_OUT = Join-Path $Root 'flow-result.json'
Invoke-LiteralCampCli -Executable $CampDev -ArgumentValues (@('flow', 'run', 'winargs', '--') + $wantArgs)
$flowRecord = Get-Content -LiteralPath $env:CAMP_FLOW_OUT -Raw -Encoding UTF8 | ConvertFrom-Json
if ($flowRecord.cwd -ne $fixture -or $flowRecord.process_cwd -ne $fixture -or
    $flowRecord.value -ne 'value with spaces') {
    throw "camp flow cwd/env mismatch: $($flowRecord | ConvertTo-Json -Compress)"
}
if ((@($flowRecord.args) -join '|') -ne ($wantArgs -join '|')) {
    throw "camp flow argv mismatch: $($flowRecord | ConvertTo-Json -Compress)"
}

$priorErrorAction = $ErrorActionPreference
try {
    $ErrorActionPreference = 'Continue'
    $missingShell = & $CampDev flow run shmissing 2>&1 | Out-String
    $missingShellCode = $LASTEXITCODE
} finally {
    $ErrorActionPreference = $priorErrorAction
}
if ($missingShellCode -eq 0 -or $missingShell -notmatch 'requires Git Bash') {
    throw "missing Git Bash did not report an actionable error: $missingShell"
}
Write-Output 'Camp PowerShell 5.1 command and dev-flow proof passed'
