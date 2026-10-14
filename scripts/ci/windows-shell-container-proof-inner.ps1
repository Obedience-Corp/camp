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

[Console]::OutputEncoding = New-Object System.Text.ASCIIEncoding
$originalCodePage = [Console]::OutputEncoding.CodePage
if ($originalCodePage -ne 20127) { throw "expected ASCII console encoding, got $originalCodePage" }

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
$fixture = Join-Path $projects 'fixture'
[void][System.IO.Directory]::CreateDirectory((Join-Path $fixture '.git'))
$wantArgs = @('two words', 'a;b', '$(Write-Output injected)', '--help')
$env:CAMP_PROBE_OUT = Join-Path $Root 'run-argv.json'
Set-Location -LiteralPath $campRoot
& $Camp run $Probe @wantArgs
if ($LASTEXITCODE -ne 0) { throw "camp run argv proof failed ($LASTEXITCODE)" }
$runRecord = Get-Content -LiteralPath $env:CAMP_PROBE_OUT -Raw | ConvertFrom-Json
if ($runRecord.cwd -ne $campRoot -or $runRecord.camp_root -ne $campRoot) {
    throw "camp run cwd/root mismatch: $($runRecord | ConvertTo-Json -Compress)"
}
if ((@($runRecord.args) -join '|') -ne ($wantArgs -join '|')) {
    throw "camp run argv mismatch: $($runRecord | ConvertTo-Json -Compress)"
}

$help = & $Camp run --help 2>&1 | Out-String
if ($LASTEXITCODE -ne 0 -or $help -notmatch 'Usage:' -or $help -notmatch 'Execute command') {
    throw "camp run --help failed: $help"
}

$env:CAMP_PROBE_OUT = Join-Path $Root 'project-run-argv.json'
& $Camp project run -p fixture -- $Probe @wantArgs
if ($LASTEXITCODE -ne 0) { throw "camp project run argv proof failed ($LASTEXITCODE)" }
$projectRecord = Get-Content -LiteralPath $env:CAMP_PROBE_OUT -Raw | ConvertFrom-Json
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
      & { $result = @{ cwd = $PWD.Path; value = $env:CAMP_FLOW_VALUE; args = @($args) } | ConvertTo-Json -Compress; [IO.File]::WriteAllText($env:CAMP_FLOW_OUT, $result) }
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
& $CampDev flow run winargs -- @wantArgs
if ($LASTEXITCODE -ne 0) { throw "camp flow run winargs failed ($LASTEXITCODE)" }
$flowRecord = Get-Content -LiteralPath $env:CAMP_FLOW_OUT -Raw | ConvertFrom-Json
if ($flowRecord.cwd -ne $fixture -or $flowRecord.value -ne 'value with spaces') {
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
