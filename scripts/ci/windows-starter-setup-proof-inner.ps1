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

$env:PATH = "$Bin;$GitCmd;$GitMingw;$env:PATH"
$env:CAMP_NO_DEFER = '1'
Remove-Item Env:CAMP_ROOT -ErrorAction SilentlyContinue
Remove-Item Env:CAMP_REGISTRY_PATH -ErrorAction SilentlyContinue

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

function Invoke-CampSetupJson {
    $stamp = [guid]::NewGuid().ToString('n')
    $stdoutPath = Join-Path $env:TEMP "setup-$stamp.json"
    $stderrPath = Join-Path $env:TEMP "setup-$stamp.err"
    $proc = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') -NoNewWindow -PassThru -Wait -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    $raw = Get-Content -LiteralPath $stdoutPath -Raw
    $stderr = Get-Content -LiteralPath $stderrPath -Raw
    if ($proc.ExitCode -ne 0) {
        throw "camp setup --json failed ($($proc.ExitCode)): stdout=$raw stderr=$stderr"
    }
    try {
        return ($raw.Trim() | ConvertFrom-Json)
    } catch {
        throw "camp setup --json did not emit JSON: stdout=$raw stderr=$stderr"
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
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null

$first = Invoke-CampSetupJson
if ($first.action -ne 'created') {
    throw "first setup action=$($first.action); want created"
}
$normPath = ($layoutA.Festival -replace '\\', '/')
if (($first.path -replace '\\', '/') -ne $normPath) {
    throw "first setup path=$($first.path); want $normPath"
}
Assert-SingleStarterPublication $layoutA

$second = Invoke-CampSetupJson
if ($second.action -ne 'complete') {
    throw "repeat setup action=$($second.action); want complete"
}
Assert-SingleStarterPublication $layoutA
$beforeYaml = Get-Content -LiteralPath (Join-Path $layoutA.Festival '.campaign\campaign.yaml') -Raw
$third = Invoke-CampSetupJson
if ($third.action -ne 'complete') {
    throw "third setup action=$($third.action); want complete"
}
$afterYaml = Get-Content -LiteralPath (Join-Path $layoutA.Festival '.campaign\campaign.yaml') -Raw
if ($beforeYaml -ne $afterYaml) {
    throw 'repeat setup modified campaign.yaml'
}

Write-Host '=== camp setup --json: concurrent CLI processes ==='
$layoutB = Initialize-StarterHome (Join-Path $Root 'home-concurrent')
Use-StarterHome $layoutB
& git config --global user.name 'camp-windows-proof' | Out-Null
& git config --global user.email 'camp-windows-proof@users.noreply.github.com' | Out-Null
& git config --global init.defaultBranch main | Out-Null
$outA = Join-Path $layoutB.Home 'first.json'
$outB = Join-Path $layoutB.Home 'second.json'
$p1 = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') -RedirectStandardOutput $outA -RedirectStandardError (Join-Path $layoutB.Home 'first.err') -NoNewWindow -PassThru
$p2 = Start-Process -FilePath $Camp -ArgumentList @('setup', '--json') -RedirectStandardOutput $outB -RedirectStandardError (Join-Path $layoutB.Home 'second.err') -NoNewWindow -PassThru
Wait-Process -Id @($p1.Id, $p2.Id)
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

Write-Host 'Windows container camp setup proof: first creation, repeat setup, and concurrent setup passed'
