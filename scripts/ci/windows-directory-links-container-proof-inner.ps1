#Requires -Version 5.1
<#
  Runs inside the Windows Server Core fixture container as ContainerUser.
  Exercises camp.exe project link/list/unlink with directory-link classification,
  external source inventory checks, collision/denied marker rollback, and git capture
  confinement where supported.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = 'C:\camp-dirlink-proof'
$Bin = Join-Path $Root 'bin'
$Camp = Join-Path $Bin 'camp.exe'
$GitCmd = Join-Path $Root 'Git\cmd'
$GitMingw = Join-Path $Root 'Git\mingw64\bin'
$DeniedCampRoot = Join-Path $Root 'camp-denied'

if (-not (Test-Path -LiteralPath $Camp)) { throw "missing camp binary: $Camp" }
if (-not (Test-Path -LiteralPath (Join-Path $GitCmd 'git.exe'))) { throw 'missing git in container fixture' }

$BasePath = $env:PATH
$env:PATH = "$Bin;$GitCmd;$GitMingw;$BasePath"
$env:CAMP_NO_DEFER = '1'
Remove-Item Env:CAMP_ROOT -ErrorAction SilentlyContinue
Remove-Item Env:CAMP_REGISTRY_PATH -ErrorAction SilentlyContinue

$ProcessExitWaitSec = 300

function Write-ProofMarker([string]$Line) {
    Write-Host $Line
}

function Invoke-CampCli {
    param(
        [string[]]$Arguments,
        [switch]$AllowFailure
    )
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & $Camp @Arguments 2>&1 | Out-String
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousPreference
    }
    if ($null -eq $code) { $code = 0 }
    if (-not $AllowFailure -and $code -ne 0) {
        throw "camp $($Arguments -join ' ') failed ($code): $out"
    }
    return [pscustomobject]@{ ExitCode = $code; Output = $out }
}

function New-CampGitFixture {
    param(
        [string]$CampRoot,
        [string]$CampaignId = 'camp-dirlink-proof'
    )
    Remove-Item -LiteralPath $CampRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path (Join-Path $CampRoot '.campaign') -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $CampRoot 'projects') -Force | Out-Null
    $yaml = @(
        "id: $CampaignId"
        'name: dirlink-proof'
        'type: product'
    ) -join "`n"
    [System.IO.File]::WriteAllText((Join-Path $CampRoot '.campaign\campaign.yaml'), $yaml, [System.Text.UTF8Encoding]::new($false))
    Push-Location $CampRoot
    try {
        & git init -b main 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "git init failed in $CampRoot" }
        & git config user.name 'camp-dirlink-proof' 2>&1 | Out-Null
        & git config user.email 'camp-dirlink-proof@users.noreply.github.com' 2>&1 | Out-Null
        & git add .campaign 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "git add .campaign failed" }
        & git commit -m 'init camp for dirlink proof' 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "git commit init failed" }
    } finally {
        Pop-Location
    }
}

function New-ExternalSource {
    param([string]$SourceRoot)
    Remove-Item -LiteralPath $SourceRoot -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $SourceRoot -Force | Out-Null
    'external-only-payload' | Set-Content -LiteralPath (Join-Path $SourceRoot 'external-only.txt') -Encoding utf8 -NoNewline
    New-Item -ItemType Directory -Path (Join-Path $SourceRoot 'nested') -Force | Out-Null
    'nested-data' | Set-Content -LiteralPath (Join-Path $SourceRoot 'nested\keep.txt') -Encoding utf8 -NoNewline
    Push-Location $SourceRoot
    try {
        & git init -b main 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "git init external source failed" }
        & git config user.name 'camp-dirlink-proof' 2>&1 | Out-Null
        & git config user.email 'camp-dirlink-proof@users.noreply.github.com' 2>&1 | Out-Null
        'readme' | Set-Content -LiteralPath (Join-Path $SourceRoot 'README.md') -Encoding utf8
        & git add README.md 2>&1 | Out-Null
        & git commit -m 'external seed' 2>&1 | Out-Null
    } finally {
        Pop-Location
    }
}

function Get-SourceInventory {
    param([string]$SourceRoot)
    if (-not (Test-Path -LiteralPath $SourceRoot)) { return @() }
    # Camp owns the .camp marker and the linked repository's info/exclude.
    # Check the user's payload tree separately from that managed metadata.
    $items = Get-ChildItem -LiteralPath $SourceRoot -Recurse -Force -File -ErrorAction SilentlyContinue |
        ForEach-Object {
            $rel = $_.FullName.Substring($SourceRoot.Length).TrimStart('\')
            if ($rel -ne '.camp' -and $rel -notlike '.git\*') {
                $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
                "$rel|$hash|$($_.Length)"
            }
        }
    return @($items | Sort-Object)
}

function Assert-InventoryUnchanged {
    param(
        [string[]]$Before,
        [string[]]$After,
        [string]$Label
    )
    $b = ($Before -join "`n")
    $a = ($After -join "`n")
    if ($b -ne $a) {
        throw "external source inventory changed ($Label): before=`n$b`nafter=`n$a"
    }
    Write-ProofMarker "external source inventory unchanged ($Label)"
}

function Get-DirectoryLinkClassification {
    param([string]$LinkPath)
    if (-not (Test-Path -LiteralPath $LinkPath)) {
        throw "link path missing: $LinkPath"
    }
    $item = Get-Item -LiteralPath $LinkPath -Force
    $isReparse = ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
    if (-not $isReparse) {
        return [pscustomobject]@{
            Kind       = 'not-reparse'
            ReparseTag = $null
            Detail     = "attributes=$($item.Attributes)"
        }
    }
    $tag = $null
    $detail = ''
    $fsutil = Join-Path $env:SystemRoot 'System32\fsutil.exe'
    if (Test-Path -LiteralPath $fsutil) {
        $raw = & $fsutil reparsepoint query $LinkPath 2>&1 | Out-String
        $detail = $raw.Trim()
        if ($raw -match '(?i)Reparse Tag Value\s*:\s*(0x[0-9a-f]+|\d+)') {
            $tagText = $Matches[1]
            if ($tagText -match '^0x') {
                $tag = [Convert]::ToUInt32($tagText.Substring(2), 16)
            } else {
                $tag = [uint32][int64]$tagText
            }
        }
    }
    $kind = 'reparse-unknown'
    if ($null -ne $tag) {
        switch ($tag) {
            0xA0000003 { $kind = 'junction' }
            0xA000000C { $kind = 'directory-symlink' }
            default { $kind = "reparse-tag-0x$($tag.ToString('X'))" }
        }
    }
    return [pscustomobject]@{
        Kind       = $kind
        ReparseTag = $tag
        Detail     = $detail
    }
}

function Assert-GitCaptureDoesNotTraverseExternal {
    param(
        [string]$CampRoot,
        [string]$SourceRoot,
        [string]$LinkName
    )
    $linkRel = "projects/$LinkName"
    Push-Location $CampRoot
    try {
        & git ls-files --error-unmatch $linkRel 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "camp git index missing linked entry $linkRel"
        }
        $tracked = (& git ls-files 2>&1 | Out-String).Trim() -split "`n" | Where-Object { $_ }
        foreach ($line in $tracked) {
            if ($line -match '(?i)external-only\.txt|nested\\keep\.txt|nested/keep\.txt') {
                throw "git index captured external file content: $line"
            }
        }
        $secret = Join-Path $SourceRoot 'post-link-secret.txt'
        'must-not-enter-camp-index' | Set-Content -LiteralPath $secret -Encoding utf8
        & git add -A 2>&1 | Out-Null
        $status = (& git status --porcelain 2>&1 | Out-String).Trim()
        if ($status -match '(?i)external-only|post-link-secret|nested\\keep|nested/keep') {
            throw "git add -A in camp pulled external tree into index/status: $status"
        }
        Remove-Item -LiteralPath $secret -Force -ErrorAction SilentlyContinue
    } finally {
        Pop-Location
    }
    Push-Location $SourceRoot
    try {
        $extStatus = (& git status --porcelain 2>&1 | Out-String).Trim()
        if ($extStatus -match '(?i)\.camp') {
            throw ".camp should stay untracked in external repo via info/exclude; status=$extStatus"
        }
    } finally {
        Pop-Location
    }
    Write-ProofMarker 'git capture did not traverse external tree'
}

function Assert-ReadbackViaSeparateProcess {
    param(
        [string]$CampRoot,
        [string]$LinkName
    )
    $readbackPath = Join-Path $Root 'readback.ps1'
    $readbackOut = Join-Path $Root 'readback.out'
    Remove-Item -LiteralPath $readbackOut -Force -ErrorAction SilentlyContinue
    $lines = @(
        '#Requires -Version 5.1'
        'Set-StrictMode -Version Latest'
        '$ErrorActionPreference = ''Stop'''
        "`$env:PATH = '$($env:PATH)'"
        "`$env:CAMP_NO_DEFER = '1'"
        "Remove-Item Env:CAMP_ROOT -ErrorAction SilentlyContinue"
        "`$camp = '$Camp'"
        "`$campRoot = '$CampRoot'"
        "`$linkName = '$LinkName'"
        "`$outPath = '$readbackOut'"
        '$list = & $camp project list 2>&1 | Out-String'
        'if ($LASTEXITCODE -ne 0) { throw "camp project list failed: $list" }'
        'if ($list -notmatch [regex]::Escape($linkName)) { throw "camp project list missing link: $list" }'
        "`$linkPath = Join-Path `$campRoot ('projects\' + `$linkName)"
        'if (-not (Test-Path -LiteralPath $linkPath)) { throw "link missing in readback process" }'
        '$payload = Get-Content -LiteralPath (Join-Path $linkPath ''external-only.txt'') -Raw'
        'if ($payload -notmatch ''external-only-payload'') { throw "linked payload unreadable in readback process" }'
        "'separate-process project link readback ok' | Set-Content -LiteralPath `$outPath -Encoding utf8"
    )
    [System.IO.File]::WriteAllLines($readbackPath, $lines)
    $proc = Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $readbackPath
    ) -WorkingDirectory $CampRoot -PassThru -NoNewWindow
    try {
        if (-not $proc.WaitForExit($ProcessExitWaitSec * 1000)) {
            & (Join-Path $env:SystemRoot 'System32\taskkill.exe') /F /T /PID $proc.Id 2>&1 | Out-Null
            throw 'readback process timed out'
        }
    } finally {
        if (-not $proc.HasExited) {
            & (Join-Path $env:SystemRoot 'System32\taskkill.exe') /F /T /PID $proc.Id 2>&1 | Out-Null
        }
    }
    if (-not (Test-Path -LiteralPath $readbackOut)) {
        throw 'readback process did not write success marker'
    }
    $marker = Get-Content -LiteralPath $readbackOut -Raw
    if ($marker -notmatch 'separate-process project link readback ok') {
        throw "unexpected readback marker: $marker"
    }
    Write-ProofMarker 'separate-process project link readback ok'
}

function Assert-CollisionAndDeniedMarkerRollback {
    param(
        [string]$CampRoot,
        [string]$LinkName
    )
    $sourceA = Join-Path $Root 'external-collision-a'
    $sourceB = Join-Path $Root 'external-collision-b'
    New-ExternalSource $sourceA
    New-ExternalSource $sourceB

    Push-Location $CampRoot
    try {
        $first = Invoke-CampCli -Arguments @('project', 'link', $sourceA, '--name', $LinkName, '--yes', '--no-commit')
        if ($first.Output -notmatch '(?i)linked project') {
            throw "first link output unexpected: $($first.Output)"
        }
        $second = Invoke-CampCli -Arguments @('project', 'link', $sourceB, '--name', $LinkName, '--yes', '--no-commit') -AllowFailure
        if ($second.ExitCode -eq 0) {
            throw 'expected collision link to fail when name already exists'
        }
        if (Test-Path -LiteralPath (Join-Path $sourceB '.camp')) {
            throw 'collision failure left .camp marker on second source (rollback expected)'
        }
        if (-not (Test-Path -LiteralPath (Join-Path $sourceA '.camp'))) {
            throw 'collision attempt removed first source marker'
        }
        Write-ProofMarker 'collision denied marker rollback verified'
    } finally {
        Pop-Location
    }

    $deniedSource = Join-Path $Root 'external-denied'
    New-ExternalSource $deniedSource
    $deniedYaml = Join-Path $DeniedCampRoot '.campaign\campaign.yaml'
    if (-not (Test-Path -LiteralPath $deniedYaml)) {
        throw "missing prebuilt denied camp fixture at $DeniedCampRoot (host admin bootstrap should create it)"
    }
    Push-Location $DeniedCampRoot
    try {
        $denied = Invoke-CampCli -Arguments @('project', 'link', $deniedSource, '--name', 'denied', '--yes', '--no-commit') -AllowFailure
        if ($denied.ExitCode -eq 0) {
            throw 'expected denied projects write to fail link'
        }
        if (Test-Path -LiteralPath (Join-Path $deniedSource '.camp')) {
            throw 'denied link failure left .camp marker on source (rollback expected)'
        }
        Write-ProofMarker 'denied creation marker rollback verified'
    } finally {
        Pop-Location
    }
}

function Get-VolumeSerialNumber {
    param([string]$Drive)
    $output = & fsutil fsinfo volumeinfo $Drive 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0 -or $output -notmatch '(?im)Volume Serial Number\s*:\s*(0x[0-9a-f]+)') {
        throw "cannot read volume serial for $Drive`: $output"
    }
    return $Matches[1].ToLowerInvariant()
}

function Assert-CrossVolumeProjectLink {
    $cSerial = Get-VolumeSerialNumber -Drive 'C:'
    $dSerial = Get-VolumeSerialNumber -Drive 'D:'
    if ($cSerial -eq $dSerial) {
        throw "C: and D: have the same volume serial $cSerial; cross-volume proof requires distinct volumes"
    }
    Write-ProofMarker "cross-volume identities verified: C:=$cSerial D:=$dSerial"

    $crossCamp = Join-Path $Root 'camp-cross-volume'
    $crossSource = 'D:\external-cross-volume'
    $crossName = 'cross-volume-app'
    New-CampGitFixture -CampRoot $crossCamp -CampaignId 'camp-cross-volume-proof'
    New-ExternalSource -SourceRoot $crossSource
    $before = Get-SourceInventory -SourceRoot $crossSource

    Push-Location $crossCamp
    try {
        $link = Invoke-CampCli -Arguments @('project', 'link', $crossSource, '--name', $crossName, '--yes', '--no-commit')
        if ($link.Output -notmatch '(?i)linked project') {
            throw "cross-volume link output unexpected: $($link.Output)"
        }
        $linkPath = Join-Path $crossCamp ('projects\' + $crossName)
        $class = Get-DirectoryLinkClassification -LinkPath $linkPath
        if ($class.Kind -ne 'junction' -or $class.ReparseTag -ne 0xA0000003) {
            throw "cross-volume project link is not a junction: $($class.Detail)"
        }
        if (-not (Test-Path -LiteralPath (Join-Path $linkPath 'external-only.txt'))) {
            throw 'cross-volume project link cannot read the source payload'
        }
        $list = Invoke-CampCli -Arguments @('project', 'list')
        if ($list.Output -notmatch [regex]::Escape($crossName)) {
            throw "cross-volume project missing from list: $($list.Output)"
        }
    } finally {
        Pop-Location
    }

    Assert-ReadbackViaSeparateProcess -CampRoot $crossCamp -LinkName $crossName
    Push-Location $crossCamp
    try {
        $null = Invoke-CampCli -Arguments @('project', 'unlink', $crossName, '--no-commit')
        if (Test-Path -LiteralPath (Join-Path $crossCamp ('projects\' + $crossName))) {
            throw 'cross-volume managed link remains after unlink'
        }
    } finally {
        Pop-Location
    }
    Assert-InventoryUnchanged -Before $before -After (Get-SourceInventory -SourceRoot $crossSource) -Label 'cross-volume unlink'
    Write-ProofMarker 'cross-volume junction project lifecycle verified'
}

Write-ProofMarker '=== directory-link identity ==='
$id = (& whoami 2>&1 | Out-String).Trim()
Write-ProofMarker "directory-link identity: $id"
if ($id -notmatch '(?i)\\ContainerUser$') {
    throw "expected non-elevated ContainerUser; whoami=$id"
}

$CampRoot = Join-Path $Root 'camp-main'
$SourceRoot = Join-Path $Root 'external-main'
$LinkName = 'linked-app'

New-CampGitFixture -CampRoot $CampRoot
New-ExternalSource -SourceRoot $SourceRoot
$inventoryBefore = Get-SourceInventory -SourceRoot $SourceRoot

Push-Location $CampRoot
try {
    $link = Invoke-CampCli -Arguments @('project', 'link', $SourceRoot, '--name', $LinkName, '--yes')
    if ($link.Output -notmatch '(?i)linked project') {
        throw "project link output unexpected: $($link.Output)"
    }
} finally {
    Pop-Location
}

$linkPath = Join-Path $CampRoot ('projects\' + $LinkName)
$mainMarker = Join-Path $SourceRoot '.camp'
if (-not (Test-Path -LiteralPath $mainMarker)) {
    throw 'linked source is missing its .camp marker'
}
$class = Get-DirectoryLinkClassification -LinkPath $linkPath
Write-ProofMarker "directory-link classification: kind=$($class.Kind) reparseTag=$($class.ReparseTag)"
if ($class.Kind -ne 'junction' -or $class.ReparseTag -ne 0xA0000003) {
    throw "local project link is not a junction: $($class.Detail)"
}

Push-Location $CampRoot
try {
    $list = Invoke-CampCli -Arguments @('project', 'list')
} finally {
    Pop-Location
}
if ($list.Output -notmatch [regex]::Escape($LinkName)) {
    throw "camp project list missing $LinkName`: $($list.Output)"
}

Assert-GitCaptureDoesNotTraverseExternal -CampRoot $CampRoot -SourceRoot $SourceRoot -LinkName $LinkName
Assert-ReadbackViaSeparateProcess -CampRoot $CampRoot -LinkName $LinkName

$inventoryAfterLink = Get-SourceInventory -SourceRoot $SourceRoot
Assert-InventoryUnchanged -Before $inventoryBefore -After $inventoryAfterLink -Label 'after link'

Assert-CollisionAndDeniedMarkerRollback -CampRoot $CampRoot -LinkName 'collision-name'

Push-Location $CampRoot
try {
    $unlink = Invoke-CampCli -Arguments @('project', 'unlink', $LinkName, '--no-commit')
    if ($unlink.Output -notmatch '(?i)unlinked project') {
        throw "project unlink output unexpected: $($unlink.Output)"
    }
    if (Test-Path -LiteralPath $linkPath) {
        throw 'link entry still present after unlink'
    }
} finally {
    Pop-Location
}

$inventoryAfterUnlink = Get-SourceInventory -SourceRoot $SourceRoot
Assert-InventoryUnchanged -Before $inventoryBefore -After $inventoryAfterUnlink -Label 'after unlink'
if (Test-Path -LiteralPath $mainMarker) {
    throw 'unlink left the managed .camp marker on the external source'
}

Assert-CrossVolumeProjectLink

Write-ProofMarker 'Windows container directory-link proof passed'
