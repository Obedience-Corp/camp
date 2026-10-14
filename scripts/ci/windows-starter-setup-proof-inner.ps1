#Requires -Version 5.1
<#
  Runs inside the Windows Server Core fixture container.
  Exercises real camp.exe setup --json with fest.exe and git on container-local paths only.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = 'C:\camp-starter-proof'
$Bin = Join-Path $Root 'bin'
$Camp = Join-Path $Bin 'camp.exe'
$Fest = Join-Path $Bin 'fest.exe'
$GitCmd = Join-Path $Root 'Git\cmd'
$GitMingw = Join-Path $Root 'Git\mingw64\bin'

if (-not (Test-Path -LiteralPath $Camp)) { throw "missing camp binary: $Camp" }
if (-not (Test-Path -LiteralPath $Fest)) { throw "missing fest binary: $Fest" }
if (-not (Test-Path -LiteralPath (Join-Path $GitCmd 'git.exe'))) { throw 'missing git in container fixture' }

$BasePath = $env:PATH
$env:PATH = "$Bin;$GitCmd;$GitMingw;$BasePath"
$env:CAMP_NO_DEFER = '1'
Remove-Item Env:CAMP_ROOT -ErrorAction SilentlyContinue
Remove-Item Env:CAMP_REGISTRY_PATH -ErrorAction SilentlyContinue

$JournalWaitSec = 120
$ProcessExitWaitSec = 300
$InterruptCleanupWaitSec = 30

function Initialize-StarterHome([string]$HomeRoot) {
    $configRoot = Join-Path $HomeRoot 'config\obey\campaign'
    $campsDir = Join-Path $HomeRoot 'camps'
    Remove-Item -LiteralPath $HomeRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $configRoot -Force | Out-Null
    $cfg = @{ campaigns_dir = ($campsDir -replace '\\', '/') } | ConvertTo-Json -Compress
    $configPath = Join-Path $configRoot 'config.json'
    [System.IO.File]::WriteAllText($configPath, $cfg, [System.Text.UTF8Encoding]::new($false))
    return @{
        Home       = $HomeRoot
        ConfigRoot = $configRoot
        CampsDir   = $campsDir
        Festival   = Join-Path $campsDir 'festival'
        Registry   = Join-Path $configRoot 'registry.json'
        Starter    = Join-Path $configRoot 'starter.json'
    }
}

function Use-StarterHome([hashtable]$Layout) {
    $env:HOME = $Layout.Home
    $env:USERPROFILE = $Layout.Home
    $env:XDG_CONFIG_HOME = Join-Path $Layout.Home 'config'
    $env:CAMP_REGISTRY_PATH = $Layout.Registry
    $env:TEMP = Join-Path $Layout.Home 'tmp'
    $env:TMP = $env:TEMP
    New-Item -ItemType Directory -Path $env:TEMP -Force | Out-Null
}

function Get-ToolPath {
    return "$Bin;$GitCmd;$GitMingw;$BasePath"
}

function Set-ToolPath([string]$PrefixDir) {
    if ($PrefixDir) {
        $env:PATH = "$PrefixDir;$(Get-ToolPath)"
    } else {
        $env:PATH = Get-ToolPath
    }
}

function Invoke-CampSetupJson {
    param(
        [switch]$AllowFailure
    )
    $stamp = [guid]::NewGuid().ToString('n')
    $stdoutPath = Join-Path $env:TEMP "setup-$stamp.json"
    $stderrPath = Join-Path $env:TEMP "setup-$stamp.err"
    $proc = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') -NoNewWindow -PassThru -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    try {
        Wait-ProcessExit -Proc $proc -TimeoutSec $ProcessExitWaitSec
    } finally {
        if (-not $proc.HasExited) { Stop-ProcessTree -RootPid $proc.Id }
    }
    $raw = if (Test-Path -LiteralPath $stdoutPath) { Get-Content -LiteralPath $stdoutPath -Raw } else { '' }
    $stderr = if (Test-Path -LiteralPath $stderrPath) { Get-Content -LiteralPath $stderrPath -Raw } else { '' }
    if (-not $AllowFailure -and $proc.ExitCode -ne 0) {
        throw "camp setup --json failed ($($proc.ExitCode)): stdout=$raw stderr=$stderr"
    }
    $parsed = $null
    if ($raw -and $raw.Trim()) {
        try {
            $parsed = $raw.Trim() | ConvertFrom-Json
        } catch {
            if (-not $AllowFailure) {
                throw "camp setup --json did not emit JSON: stdout=$raw stderr=$stderr"
            }
        }
    }
    return [pscustomobject]@{
        ExitCode = $proc.ExitCode
        Raw      = $raw
        Stderr   = $stderr
        Result   = $parsed
    }
}

function Wait-Until {
    param(
        [scriptblock]$Condition,
        [int]$TimeoutSec,
        [string]$Label
    )
    $deadline = [datetime]::UtcNow.AddSeconds($TimeoutSec)
    while ([datetime]::UtcNow -lt $deadline) {
        if (& $Condition) { return }
        Start-Sleep -Milliseconds 250
    }
    throw "timeout after ${TimeoutSec}s waiting for $Label"
}

function Wait-ProcessExit {
    param(
        [System.Diagnostics.Process]$Proc,
        [int]$TimeoutSec
    )
    if ($Proc.HasExited) { return }
    $deadline = [datetime]::UtcNow.AddSeconds($TimeoutSec)
    while (-not $Proc.HasExited) {
        if ([datetime]::UtcNow -ge $deadline) {
            Stop-ProcessTree -RootPid $Proc.Id
            throw "process $($Proc.Id) did not exit within ${TimeoutSec}s"
        }
        Start-Sleep -Milliseconds 300
    }
}

function Stop-ProcessTree {
    param([int]$RootPid)
    if ($RootPid -le 0) { return }
    & (Join-Path $env:SystemRoot 'System32\taskkill.exe') /F /T /PID $RootPid 2>&1 | Out-Null
}

function Stop-AllCampProcessesInFixture {
    foreach ($proc in @(Get-Process -Name 'camp' -ErrorAction SilentlyContinue)) {
        Stop-ProcessTree -RootPid $proc.Id
    }
    Start-Sleep -Milliseconds 500
}

function Test-PrePublicationState([hashtable]$Layout) {
    if (Test-Path -LiteralPath $Layout.Festival) { return $false }
    if ((Read-RegistryCampaignCount $Layout.Registry) -ne 0) { return $false }
    return $true
}

function New-BlockingFestStub {
    param(
        [string]$Dir,
        [string]$ReleaseFlag,
        [string]$RealFestExe
    )
    Remove-Item -LiteralPath $Dir -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    $cmd = Join-Path $Dir 'fest.cmd'
    $body = @(
        '@echo off'
        'setlocal'
        ':wait_release'
        "if exist `"$ReleaseFlag`" goto run_real"
        '%SystemRoot%\System32\PING.EXE -n 2 127.0.0.1 >nul'
        "if exist `"$ReleaseFlag`" goto run_real"
        'goto wait_release'
        ':run_real'
        "`"$RealFestExe`" %*"
        'exit /b %ERRORLEVEL%'
    ) -join "`r`n"
    Set-Content -LiteralPath $cmd -Value $body -Encoding Ascii
}

function New-FailingFestStub([string]$Dir) {
    Remove-Item -LiteralPath $Dir -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    $cmd = Join-Path $Dir 'fest.cmd'
    '@echo off' + "`r`n" + 'exit /b 1' | Set-Content -LiteralPath $cmd -Encoding Ascii
}

function Test-StarterJournalWithStaging([hashtable]$Layout) {
    if (-not (Test-Path -LiteralPath $Layout.Starter)) { return $false }
    try {
        $state = Get-Content -LiteralPath $Layout.Starter -Raw | ConvertFrom-Json
        if (-not $state.staging) { return $false }
        return Test-Path -LiteralPath $state.staging
    } catch {
        return $false
    }
}

function Invoke-CompetingSetupInterruptBeforePublication([hashtable]$Layout) {
    $releaseFlag = Join-Path $Layout.Home 'release-fest.flag'
    Remove-Item -LiteralPath $releaseFlag -Force -ErrorAction SilentlyContinue
    $blockDir = Join-Path $Layout.Home 'blocking-fest'
    New-BlockingFestStub -Dir $blockDir -ReleaseFlag $releaseFlag -RealFestExe $Fest
    Set-ToolPath $blockDir

    $out1 = Join-Path $Layout.Home 'interrupt-first.json'
    $err1 = Join-Path $Layout.Home 'interrupt-first.err'
    $out2 = Join-Path $Layout.Home 'interrupt-second.json'
    $err2 = Join-Path $Layout.Home 'interrupt-second.err'

    $p1 = $null
    $p2 = $null
    try {
        $p1 = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') `
            -RedirectStandardOutput $out1 -RedirectStandardError $err1 -PassThru -NoNewWindow
        Start-Sleep -Milliseconds 300
        $p2 = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') `
            -RedirectStandardOutput $out2 -RedirectStandardError $err2 -PassThru -NoNewWindow

        Wait-Until -Condition { Test-StarterJournalWithStaging $Layout } `
            -TimeoutSec $JournalWaitSec -Label 'starter journal with staging before publication'

        if (-not (Test-PrePublicationState $Layout)) {
            throw 'setup reached publication before process interrupt could run'
        }

        $victim = $null
        foreach ($candidate in @($p1, $p2)) {
            if ($candidate -and -not $candidate.HasExited) {
                $victim = $candidate
                break
            }
        }
        if (-not $victim) {
            throw 'both camp setup processes exited before interrupt (fest may have timed out)'
        }

        Write-Host "terminating camp setup process tree pid=$($victim.Id) while fest init is blocked and before publication"
        Stop-ProcessTree -RootPid $victim.Id
        Wait-ProcessExit -Proc $victim -TimeoutSec $InterruptCleanupWaitSec

        if (-not (Test-PrePublicationState $Layout)) {
            throw 'publication happened during interrupt cleanup'
        }
        Assert-InterruptedPartialState $Layout

        New-Item -ItemType File -Path $releaseFlag -Force | Out-Null
        foreach ($proc in @($p1, $p2)) {
            if ($proc -and -not $proc.HasExited) {
                Wait-ProcessExit -Proc $proc -TimeoutSec $ProcessExitWaitSec
            }
        }

        $published = (Test-Path -LiteralPath $Layout.Festival) -and ((Read-RegistryCampaignCount $Layout.Registry) -eq 1)
        if (-not $published) {
            Write-Host 'surviving setup did not publish; retrying camp setup with real fest.exe'
            Set-ToolPath $null
            $retry = Invoke-CampSetupJson
            if ($retry.Result.action -notin @('created', 'complete')) {
                throw "retry after process interrupt action=$($retry.Result.action); want created or complete"
            }
        }
    } finally {
        Remove-Item -LiteralPath $releaseFlag -Force -ErrorAction SilentlyContinue
        Set-ToolPath $null
        Stop-AllCampProcessesInFixture
    }
}

function Assert-StagingClean([string]$CampsDir) {
    if (-not (Test-Path -LiteralPath $CampsDir)) { return }
    $leftovers = @(Get-ChildItem -LiteralPath $CampsDir -Directory -Filter '.festival-setup-*' -ErrorAction SilentlyContinue)
    if ($leftovers.Count -gt 0) {
        throw "unexpected staging directories: $($leftovers.FullName -join ', ')"
    }
}

function Read-RegistryCampaignCount([string]$RegistryPath) {
    if (-not (Test-Path -LiteralPath $RegistryPath)) { return 0 }
    $reg = Get-Content -LiteralPath $RegistryPath -Raw | ConvertFrom-Json
    if (-not $reg.campaigns) { return 0 }
    return @($reg.campaigns.PSObject.Properties).Count
}

function Get-StagingDirectories([string]$CampsDir) {
    if (-not (Test-Path -LiteralPath $CampsDir)) { return @() }
    return @(Get-ChildItem -LiteralPath $CampsDir -Directory -Filter '.festival-setup-*' -ErrorAction SilentlyContinue)
}

function Assert-InterruptedPartialState([hashtable]$Layout) {
    if (Test-Path -LiteralPath $Layout.Festival) {
        throw "published starter exists after failed setup: $($Layout.Festival)"
    }
    if (Test-Path -LiteralPath $Layout.Registry) {
        throw "registry.json written after failed setup: $($Layout.Registry)"
    }
    if (-not (Test-Path -LiteralPath $Layout.Starter)) {
        throw 'starter.json missing after interrupted setup (journal should persist)'
    }
    $state = Get-Content -LiteralPath $Layout.Starter -Raw | ConvertFrom-Json
    if ($state.completed) {
        throw 'starter.json completed=true after failed setup'
    }
    if (-not $state.staging) {
        throw 'starter.json missing staging path after interrupted setup'
    }
    if (-not (Test-Path -LiteralPath $state.staging)) {
        throw "staging workspace missing on disk: $($state.staging)"
    }
    $stageParents = Get-StagingDirectories $Layout.CampsDir
    if ($stageParents.Count -lt 1) {
        throw 'expected .festival-setup-* staging parent after interrupted setup'
    }
    $normStaging = ($state.staging -replace '\\', '/').ToLowerInvariant()
    $found = $false
    foreach ($parent in $stageParents) {
        $candidate = (Join-Path $parent.FullName 'festival' -replace '\\', '/').ToLowerInvariant()
        if ($candidate -eq $normStaging) {
            $found = $true
            break
        }
    }
    if (-not $found) {
        throw "journal staging $($state.staging) not under preserved .festival-setup-* directories"
    }
    Write-Host "interrupted setup preserved staging at $($state.staging)"
}

function Assert-ReadbackViaSeparateProcess([hashtable]$Layout) {
    $readbackPath = Join-Path $Layout.Home 'readback.ps1'
    $readbackOut = Join-Path $Layout.Home 'readback.out'
    $festNorm = ($Layout.Festival -replace '\\', '/')
    $lines = @(
        '#Requires -Version 5.1'
        'Set-StrictMode -Version Latest'
        '$ErrorActionPreference = ''Stop'''
        "`$env:HOME = '$($Layout.Home)'"
        "`$env:USERPROFILE = '$($Layout.Home)'"
        "`$env:XDG_CONFIG_HOME = '$(Join-Path $Layout.Home 'config')'"
        "`$env:CAMP_REGISTRY_PATH = '$($Layout.Registry)'"
        "`$env:TEMP = '$(Join-Path $Layout.Home 'tmp')'"
        '$env:TMP = $env:TEMP'
        "`$env:PATH = '$(Get-ToolPath)'"
        '$env:CAMP_NO_DEFER = ''1'''
        "Remove-Item Env:CAMP_ROOT -ErrorAction SilentlyContinue"
        "`$outPath = '$readbackOut'"
        "`$list = & '$Camp' list --format json 2>&1 | Out-String"
        "if (`$LASTEXITCODE -ne 0) { throw ""camp list failed: `$list"" }"
        "if (`$list -notmatch 'festival') { throw ""camp list missing festival: `$list"" }"
        "if (-not (Test-Path -LiteralPath '$($Layout.Registry)')) { throw 'registry missing in readback process' }"
        '$reg = Get-Content -LiteralPath $env:CAMP_REGISTRY_PATH -Raw | ConvertFrom-Json'
        'if (@($reg.campaigns.PSObject.Properties).Count -ne 1) { throw "registry count not 1" }'
        "if (-not (Test-Path -LiteralPath '$($Layout.Starter)')) { throw 'starter.json missing in readback process' }"
        "`$starter = Get-Content -LiteralPath '$($Layout.Starter)' -Raw | ConvertFrom-Json"
        'if (-not $starter.completed) { throw "starter.json not completed in readback" }'
        "if (-not (Test-Path -LiteralPath '$($Layout.Festival)\.git')) { throw 'festival .git missing in readback' }"
        "if (-not (Test-Path -LiteralPath '$($Layout.Festival)\festivals\.festival')) { throw 'fest marker missing in readback' }"
        "if (-not (Test-Path -LiteralPath '$($Layout.Festival)\.campaign\campaign.yaml')) { throw 'campaign.yaml missing in readback' }"
        "'separate-process readback ok' | Set-Content -LiteralPath `$outPath -Encoding utf8"
    )
    [System.IO.File]::WriteAllLines($readbackPath, $lines)
    $proc = Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $readbackPath
    ) -WorkingDirectory $Layout.Home -PassThru -NoNewWindow
    try {
        Wait-ProcessExit -Proc $proc -TimeoutSec $ProcessExitWaitSec
    } finally {
        if (-not $proc.HasExited) { Stop-ProcessTree -RootPid $proc.Id }
    }
    if ($proc.ExitCode -ne 0) {
        throw "separate-process readback failed with exit $($proc.ExitCode)"
    }
    if (-not (Test-Path -LiteralPath $readbackOut)) {
        throw 'readback process did not write success marker'
    }
    $marker = Get-Content -LiteralPath $readbackOut -Raw
    if ($marker -notmatch 'separate-process readback ok') {
        throw "unexpected readback marker: $marker"
    }
    Write-Host "separate process verified registry, starter.json, and camp tree at $festNorm"
}

function Assert-SingleStarterPublication([hashtable]$Layout) {
    if (-not (Test-Path -LiteralPath $Layout.Festival)) {
        throw "starter camp missing at $($Layout.Festival)"
    }
    if (-not (Test-Path -LiteralPath (Join-Path $Layout.Festival '.git'))) {
        throw 'starter camp is not a git repository'
    }
    $festMarker = Join-Path $Layout.Festival 'festivals\.festival'
    if (-not (Test-Path -LiteralPath $festMarker)) {
        throw "fest init did not create $festMarker"
    }
    $campCount = Read-RegistryCampaignCount $Layout.Registry
    if ($campCount -ne 1) {
        throw "registry campaigns = $campCount; want 1"
    }
    if (-not (Test-Path -LiteralPath $Layout.Starter)) {
        throw 'starter.json missing after setup'
    }
    $state = Get-Content -LiteralPath $Layout.Starter -Raw | ConvertFrom-Json
    if (-not $state.completed) {
        throw "starter.json completed=false: $($state | ConvertTo-Json -Compress)"
    }
    Assert-StagingClean $Layout.CampsDir
    $dupes = @(Get-ChildItem -LiteralPath $Layout.CampsDir -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -eq 'festival' })
    if ($dupes.Count -ne 1) {
        throw "expected exactly one festival directory; saw $($dupes.Count)"
    }
}

Write-Host '=== camp setup --json: first creation and repeat ==='
$layoutA = Initialize-StarterHome (Join-Path $Root 'home-first')
Use-StarterHome $layoutA
Set-ToolPath $null
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null

$first = Invoke-CampSetupJson
if ($first.Result.action -ne 'created') {
    throw "first setup action=$($first.Result.action); want created"
}
$normPath = ($layoutA.Festival -replace '\\', '/')
if (($first.Result.path -replace '\\', '/') -ne $normPath) {
    throw "first setup path=$($first.Result.path); want $normPath"
}
Assert-SingleStarterPublication $layoutA

$second = Invoke-CampSetupJson
if ($second.Result.action -ne 'complete') {
    throw "repeat setup action=$($second.Result.action); want complete"
}
Assert-SingleStarterPublication $layoutA
$beforeYaml = Get-Content -LiteralPath (Join-Path $layoutA.Festival '.campaign\campaign.yaml') -Raw
$third = Invoke-CampSetupJson
if ($third.Result.action -ne 'complete') {
    throw "third setup action=$($third.Result.action); want complete"
}
$afterYaml = Get-Content -LiteralPath (Join-Path $layoutA.Festival '.campaign\campaign.yaml') -Raw
if ($beforeYaml -ne $afterYaml) {
    throw 'repeat setup modified campaign.yaml'
}

Write-Host '=== camp setup --json: competing processes, kill one before publication ==='
$layoutInterruptKill = Initialize-StarterHome (Join-Path $Root 'home-interrupt-kill')
Use-StarterHome $layoutInterruptKill
Set-ToolPath $null
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null
Invoke-CompetingSetupInterruptBeforePublication $layoutInterruptKill
Assert-SingleStarterPublication $layoutInterruptKill
Assert-ReadbackViaSeparateProcess $layoutInterruptKill

Write-Host '=== camp setup --json: failed fest command (journal retry, no process kill) ==='
$layoutFestFail = Initialize-StarterHome (Join-Path $Root 'home-fest-fail')
Use-StarterHome $layoutFestFail
Set-ToolPath $null
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null
$failingFest = Join-Path $layoutFestFail.Home 'failing-fest'
New-FailingFestStub $failingFest
Set-ToolPath $failingFest
try {
    $failed = Invoke-CampSetupJson -AllowFailure
    if ($failed.ExitCode -eq 0) {
        throw 'expected failing fest setup to exit non-zero'
    }
    Assert-InterruptedPartialState $layoutFestFail
} finally {
    Set-ToolPath $null
    Stop-AllCampProcessesInFixture
}
$retryFestFail = Invoke-CampSetupJson
if ($retryFestFail.Result.action -ne 'created') {
    throw "fest-fail retry action=$($retryFestFail.Result.action); want created"
}
Assert-SingleStarterPublication $layoutFestFail

Write-Host '=== camp setup --json: occupied target (no false completion) ==='
$layoutOccupied = Initialize-StarterHome (Join-Path $Root 'home-occupied')
Use-StarterHome $layoutOccupied
Set-ToolPath $null
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null
New-Item -ItemType Directory -Path $layoutOccupied.Festival -Force | Out-Null
$minePath = Join-Path $layoutOccupied.Festival 'mine.txt'
'precious' | Set-Content -LiteralPath $minePath -Encoding utf8 -NoNewline
$skipped = Invoke-CampSetupJson
if ($skipped.Result.action -ne 'skipped') {
    throw "occupied setup action=$($skipped.Result.action); want skipped"
}
if ($skipped.Result.message -notmatch [regex]::Escape($layoutOccupied.Festival)) {
    throw "occupied setup message missing target path: $($skipped.Result.message)"
}
if (-not (Test-Path -LiteralPath $minePath)) {
    throw 'occupied target removed during skipped setup'
}
if ((Get-Content -LiteralPath $minePath -Raw) -ne 'precious') {
    throw 'occupied target file modified during skipped setup'
}
if (Test-Path -LiteralPath $layoutOccupied.Starter) {
    throw 'starter.json written for occupied-target skip'
}
if (Test-Path -LiteralPath $layoutOccupied.Registry) {
    throw 'registry.json written for occupied-target skip'
}
Write-Host 'occupied target preserved; setup skipped without starter.json'

Write-Host '=== camp setup --json: concurrent CLI processes ==='
$layoutB = Initialize-StarterHome (Join-Path $Root 'home-concurrent')
Use-StarterHome $layoutB
Set-ToolPath $null
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null
$outA = Join-Path $layoutB.Home 'first.json'
$outB = Join-Path $layoutB.Home 'second.json'
$p1 = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') -RedirectStandardOutput $outA -RedirectStandardError (Join-Path $layoutB.Home 'first.err') -NoNewWindow -PassThru
$p2 = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') -RedirectStandardOutput $outB -RedirectStandardError (Join-Path $layoutB.Home 'second.err') -NoNewWindow -PassThru
try {
    Wait-ProcessExit -Proc $p1 -TimeoutSec $ProcessExitWaitSec
    Wait-ProcessExit -Proc $p2 -TimeoutSec $ProcessExitWaitSec
} finally {
    foreach ($proc in @($p1, $p2)) {
        if ($proc -and -not $proc.HasExited) { Stop-ProcessTree -RootPid $proc.Id }
    }
}
foreach ($proc in @($p1, $p2)) {
    if ($proc.ExitCode -ne 0) {
        throw "concurrent camp setup exit $($proc.ExitCode)"
    }
}
$jsonA = Get-Content -LiteralPath $outA -Raw | ConvertFrom-Json
$jsonB = Get-Content -LiteralPath $outB -Raw | ConvertFrom-Json
$actions = @($jsonA.action, $jsonB.action) | Sort-Object
$expected = @('complete', 'created')
if (($actions -join ',') -ne ($expected -join ',')) {
    throw "concurrent actions=$($actions -join ','); want created,complete"
}
Assert-SingleStarterPublication $layoutB
Assert-ReadbackViaSeparateProcess $layoutB

Write-Host 'Windows container camp setup proof: process interrupt before publication, fest-fail retry, occupied target, concurrent setup, and separate-process readback passed'
