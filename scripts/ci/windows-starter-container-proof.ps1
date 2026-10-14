#Requires -Version 7.0
<#
.SYNOPSIS
  Compiles internal/starter container_fs tests and runs them inside a Windows Server Core container,
  then proves real camp setup --json with camp.exe, fest.exe, and host Git copied into the fixture.

  Bypasses tests/integration TestMain (Unix Colima/docker.sock probe). Intended for the
  windows-starter-lock gate on windows-2025 hosted runners.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$ContainerImage = 'mcr.microsoft.com/windows/servercore:ltsc2025'
$MinStarterTests = 7
$RunLinePattern = '(?m)^=== RUN[ \t]+(Test[A-Za-z0-9_]+)\r?$'

function Write-Section([string]$Title) {
    Write-Host ""
    Write-Host "=== $Title ==="
}

function Report-DockerServices {
    Write-Section 'Windows Docker services'
    $services = @(Get-Service -Name '*docker*' -ErrorAction SilentlyContinue)
    if ($services.Count -eq 0) {
        Write-Host 'No Windows services matching *docker* were found.'
        return
    }
    $services | Format-Table Name, Status, StartType -AutoSize | Out-String | Write-Host
}

function Start-DockerServiceIfPresent {
    foreach ($name in @('com.docker.service', 'docker')) {
        $svc = Get-Service -Name $name -ErrorAction SilentlyContinue
        if (-not $svc) { continue }
        if ($svc.Status -ne 'Running') {
            Write-Host "Starting service $name (was $($svc.Status))..."
            Start-Service -Name $name
            ($svc = Get-Service -Name $name).WaitForStatus('Running', (New-TimeSpan -Minutes 2))
        }
        Write-Host "Service $name is $($svc.Status)."
        return
    }
}

function Invoke-DockerCli([string[]]$DockerArgs) {
    & docker @DockerArgs
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($DockerArgs -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Ensure-WindowsContainerEngine {
    $dockerCli = Join-Path $env:ProgramFiles 'Docker\Docker\DockerCli.exe'
    $info = & docker info -f '{{.OSType}}' 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "docker info failed: $info"
    }
    $osType = ($info | Out-String).Trim()
    Write-Host "Docker daemon OSType: $osType"
    if ($osType -ieq 'windows') {
        return
    }
    if (-not (Test-Path -LiteralPath $dockerCli)) {
        throw @(
            "Docker daemon is in '$osType' mode; Windows container images require OSType=windows.",
            "DockerCli.exe was not found at $dockerCli.",
            "Hosted windows-2025 may not expose a switchable Windows engine — use a self-hosted",
            "Windows Server 2025 runner with Windows containers enabled (see D005 / 005 fixture lane)."
        ) -join ' '
    }
    Write-Host "Switching Docker Desktop to Windows containers via DockerCli.exe..."
    & $dockerCli -SwitchDaemon
    if ($LASTEXITCODE -ne 0) {
        throw "DockerCli.exe -SwitchDaemon failed with exit code $LASTEXITCODE"
    }
    Start-Sleep -Seconds 5
    $info = & docker info -f '{{.OSType}}' 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "docker info after engine switch failed: $info"
    }
    $osType = ($info | Out-String).Trim()
    Write-Host "Docker daemon OSType after switch: $osType"
    if ($osType -ine 'windows') {
        throw @(
            "Docker daemon is still '$osType' after -SwitchDaemon.",
            "Cannot run $ContainerImage on this runner without a Windows container engine.",
            "Document this capability gap and use a self-hosted Windows Server 2025 lane for proof."
        ) -join ' '
    }
}

function Report-DockerCapability {
    Write-Section 'Docker CLI capability'
    try {
        Invoke-DockerCli -DockerArgs @('version')
    } catch {
        Write-Host $_
        throw @(
            'Docker CLI cannot reach a daemon on this runner.',
            'If com.docker.service exists, start it; otherwise hosted windows-2025 may lack a',
            'runnable Windows container engine — use self-hosted Windows Server 2025 with Docker.'
        ) -join ' '
    }
    Invoke-DockerCli -DockerArgs @('info')
    Ensure-WindowsContainerEngine
}

function Invoke-DockerExec([string]$ContainerId, [string]$CmdLine) {
    $out = & docker exec $ContainerId cmd /c $CmdLine 2>&1
    $code = $LASTEXITCODE
    $text = $out | Out-String
    if ($code -ne 0) {
        throw "docker exec failed ($code): $text"
    }
    return $text
}

function Invoke-DockerExecPowerShell([string]$ContainerId, [string]$ScriptPath) {
    $out = & docker exec $ContainerId powershell.exe -NoProfile -ExecutionPolicy Bypass -File $ScriptPath 2>&1
    $code = $LASTEXITCODE
    $text = $out | Out-String
    if ($code -ne 0) {
        throw "docker exec powershell failed ($code): $text"
    }
    return $text
}

function Resolve-FestSource {
    if ($env:CAMP_TEST_FEST_SRC) {
        $abs = (Resolve-Path -LiteralPath $env:CAMP_TEST_FEST_SRC).Path
        if (-not (Test-Path -LiteralPath (Join-Path $abs 'cmd\fest'))) {
            throw "CAMP_TEST_FEST_SRC=$abs has no cmd\fest"
        }
        return $abs
    }
    $searched = [System.Collections.Generic.List[string]]::new()
    for ($dir = $RepoRoot; ; $dir = Split-Path -Parent $dir) {
        foreach ($name in @('fest-src', 'fest')) {
            $candidate = Join-Path $dir $name
            $searched.Add($candidate)
            if (Test-Path -LiteralPath (Join-Path $candidate 'cmd\fest')) {
                return (Resolve-Path -LiteralPath $candidate).Path
            }
        }
        $parent = Split-Path -Parent $dir
        if ($parent -eq $dir) { break }
    }
    throw @(
        'fest source not found for windows setup proof.',
        "Searched: $($searched -join '; ').",
        'Check out github.com/Obedience-Corp/fest (cf0006-windows) or set CAMP_TEST_FEST_SRC.'
    ) -join ' '
}

function Get-HostGitRoot {
    $gitCmd = (Get-Command git.exe -ErrorAction Stop).Source
    for ($dir = Split-Path -Parent $gitCmd; $dir; $dir = Split-Path -Parent $dir) {
        if (Test-Path -LiteralPath (Join-Path $dir 'cmd\git.exe')) {
            return $dir
        }
        $parent = Split-Path -Parent $dir
        if ($parent -eq $dir) { break }
    }
    throw "host git layout unexpected at $gitCmd (no parent with cmd\git.exe)"
}

function Build-SetupProofBinaries([string]$WorkDir) {
    $campOut = Join-Path $WorkDir 'camp.exe'
    $festOut = Join-Path $WorkDir 'fest.exe'
    $festRoot = Resolve-FestSource
    Write-Host "Building camp.exe from $RepoRoot"
    $env:CGO_ENABLED = '0'
    & go build -o $campOut ./cmd/camp
    if ($LASTEXITCODE -ne 0) {
        throw "go build ./cmd/camp failed with exit code $LASTEXITCODE"
    }
    Write-Host "Building fest.exe from $festRoot"
    Push-Location $festRoot
    try {
        & go build -o $festOut ./cmd/fest
        if ($LASTEXITCODE -ne 0) {
            throw "go build ./cmd/fest failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
    return @{
        Camp = $campOut
        Fest = $festOut
    }
}

try {
    Push-Location $RepoRoot

    Report-DockerServices
    Start-DockerServiceIfPresent
    Report-DockerCapability

    $workHost = Join-Path $env:TEMP ("camp-windows-starter-proof-{0}" -f [guid]::NewGuid().ToString('n'))
    New-Item -ItemType Directory -Path $workHost | Out-Null
    try {
        $binPath = Join-Path $workHost 'starter.test.exe'
        Write-Section 'Compile starter container_fs test binary'
        $env:CGO_ENABLED = '0'
        & go test -c -tags container_fs -o $binPath ./internal/starter
        if ($LASTEXITCODE -ne 0) {
            throw "go test -c ./internal/starter failed with exit code $LASTEXITCODE"
        }

        $containerWork = 'C:\camp-starter-proof'
        $containerBin = "$containerWork\starter.test.exe"

        Write-Section 'Windows Server Core fixture'
        Invoke-DockerCli -DockerArgs @('pull', $ContainerImage)
        $createOut = & docker create --pull=never $ContainerImage cmd /c "ping -n 3600 127.0.0.1 > nul" 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "docker create failed: $($createOut | Out-String)"
        }
        $containerId = ($createOut | Out-String).Trim()
        if ($containerId -notmatch '^[0-9a-f]{64}$') {
            throw "docker create returned an invalid container id: $containerId"
        }
        Write-Host "Container id: $containerId"

        try {
            Invoke-DockerCli -DockerArgs @('start', $containerId) | Out-Null
            Invoke-DockerExec $containerId "mkdir $containerWork" | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $binPath, "${containerId}:${containerBin}") | Out-Null

            $testCmd = @(
                "set `"HOME=$containerWork`""
                "set `"USERPROFILE=$containerWork`""
                "set `"TEMP=$containerWork\tmp`""
                "set `"TMP=$containerWork\tmp`""
                "mkdir $containerWork\tmp"
                "cd /d $containerWork"
                "$containerBin -test.v -test.count=1 -test.timeout=5m"
            ) -join ' && '

            Write-Section 'Run starter suite inside container (filesystem mutations on container FS)'
            $execOut = Invoke-DockerExec $containerId $testCmd
            Write-Host $execOut

            if ($execOut -notmatch 'PASS') {
                throw "starter suite output did not contain PASS"
            }
            $runs = [regex]::Matches($execOut, $RunLinePattern)
            if ($runs.Count -lt $MinStarterTests) {
                throw "expected at least $MinStarterTests starter tests; saw $($runs.Count). Output:`n$execOut"
            }
            Write-Host ""
            Write-Host "Windows container starter proof: $($runs.Count) tests executed in $ContainerImage"

            Write-Section 'Compile camp.exe and fest.exe for setup --json proof'
            $cliBin = Build-SetupProofBinaries -WorkDir $workHost
            $gitRoot = Get-HostGitRoot
            $innerScriptHost = Join-Path $PSScriptRoot 'windows-starter-setup-proof-inner.ps1'
            if (-not (Test-Path -LiteralPath $innerScriptHost)) {
                throw "missing inner setup proof script: $innerScriptHost"
            }

            $containerBinDir = "$containerWork\bin"
            $containerGitDir = "$containerWork\Git"
            $containerInner = "$containerWork\setup-proof.ps1"
            Invoke-DockerExec $containerId "mkdir $containerBinDir" | Out-Null
            Invoke-DockerCli -DockerArgs @(
                'cp', $cliBin.Camp, "${containerId}:$containerBinDir\camp.exe"
            ) | Out-Null
            Invoke-DockerCli -DockerArgs @(
                'cp', $cliBin.Fest, "${containerId}:$containerBinDir\fest.exe"
            ) | Out-Null
            Write-Host "Copying host Git from $gitRoot into container (real git for camp init)"
            Invoke-DockerCli -DockerArgs @('cp', $gitRoot, "${containerId}:$containerGitDir") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $innerScriptHost, "${containerId}:$containerInner") | Out-Null

            Write-Section 'Run camp setup --json proof inside container'
            $setupOut = Invoke-DockerExecPowerShell $containerId $containerInner
            Write-Host $setupOut
            if ($setupOut -notmatch 'separate-process readback passed') {
                throw "camp setup proof output missing success marker:`n$setupOut"
            }
            Write-Host ""
            Write-Host 'Windows container camp setup proof: process interrupt, fest-fail retry, occupied target, concurrent setup, and readback verified'
        } finally {
            & docker rm -f $containerId 2>&1 | Out-Null
        }
    } finally {
        Remove-Item -LiteralPath $workHost -Recurse -Force -ErrorAction SilentlyContinue
    }
} finally {
    Pop-Location
}
