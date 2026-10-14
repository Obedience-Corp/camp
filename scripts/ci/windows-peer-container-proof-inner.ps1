#Requires -Version 5.1
# Runs as ContainerUser in the Windows Server Core shell fixture. This is a
# real camp.exe journey, isolated from host
# configuration and without a reachable peer.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$HarnessRoot = 'C:\camp-shell-proof'
$Root = Join-Path $HarnessRoot 'peer-boundary'
$Camp = Join-Path $HarnessRoot 'bin\camp.exe'
if (-not (Test-Path -LiteralPath $Camp)) { throw "missing camp binary: $Camp" }

[void][System.IO.Directory]::CreateDirectory((Join-Path $Root '.campaign'))
[void][System.IO.Directory]::CreateDirectory((Join-Path $Root 'projects'))
$env:CAMP_ROOT = $Root
$env:CAMP_NO_DEFER = '1'
$env:CAMP_MACHINES_PATH = Join-Path $Root 'machines.yaml'
$yaml = "id: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee`nname: peer-boundary`ntype: product`n"
[System.IO.File]::WriteAllText((Join-Path $Root '.campaign\campaign.yaml'), $yaml,
    (New-Object System.Text.UTF8Encoding($false)))
[System.IO.File]::WriteAllText($env:CAMP_MACHINES_PATH,
    "version: 1`nmachines:`n  - id: other`n    host: other.invalid`n    auth_method: ssh-agent`n",
    (New-Object System.Text.UTF8Encoding($false)))
$source = Join-Path $Root 'source.txt'
$dest = Join-Path $Root 'copied.txt'
[System.IO.File]::WriteAllText($source, 'keep-original', (New-Object System.Text.UTF8Encoding($false)))
Set-Location -LiteralPath $Root

function Assert-PeerDeclined([string[]]$ArgsToCamp, [string]$Label) {
    $oldPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $Camp @ArgsToCamp 2>&1 | Out-String
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $oldPreference
    }
    if ($code -eq 0 -or $output -notmatch 'unavailable on Windows' -or
        $output -match '(?im)^Transferred\s|"success"\s*:\s*true') {
        throw "$Label reported an unsafe result (exit $code): $output"
    }
    Write-Host "$Label failed explicitly before copy"
}

Assert-PeerDeclined @('sync', '--from', 'other') 'camp sync --from'
if (Test-Path -LiteralPath (Join-Path $Root '.campaign\cache')) {
    throw 'sync --from created a peer cache after declining the transfer'
}
Assert-PeerDeclined @('clone', 'https://example.invalid/project.git', 'clone-output', '--from', 'other') 'camp clone --from'
if (Test-Path -LiteralPath (Join-Path $Root 'clone-output')) {
    throw 'clone --from created a destination after declining the transfer'
}
Assert-PeerDeclined @('transfer', 'source.txt', 'other:peer-boundary:copied.txt') 'camp transfer machine endpoint'
Assert-PeerDeclined @('artifacts', 'resolve', 'source.txt', '--from', 'other', '--take-peer') 'artifact take-peer'
if (([System.IO.File]::ReadAllText($source)) -ne 'keep-original' -or
    (Test-Path -LiteralPath $dest)) {
    throw 'peer boundary changed source or created destination after declined transfer'
}

Write-Host 'Windows container peer boundary proof passed'
