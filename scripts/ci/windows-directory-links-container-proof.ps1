#Requires -Version 7.0
<#
.SYNOPSIS
  Compiles container_fs directory-link test binaries and runs them inside a Windows
  Server Core container as ContainerUser, then proves real camp.exe project
  link/list/second-process readback/unlink with ACL-isolated fixtures.

  Runs the task01 dirlink, project, and Git capture suites selected by
  CAMP_WINDOWS_DIRLINK_TEST_RUN.

  Bypasses tests/integration TestMain. Intended for windows-directory-links-lock on
  windows-2025 hosted runners (same Docker service pattern as windows-starter-lock).
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$ContainerImage = 'mcr.microsoft.com/windows/servercore:ltsc2025'
$ContainerWork = 'C:\camp-dirlink-proof'
$ContainerUserName = 'ContainerUser'

# Go -test.run regex (not .NET).
# Matches the focused dirlink, project, Git, and skill link suites.
$DefaultTestRunPattern = 'Test(.*DirectoryLink|AddLinked|UnlinkProject|Dirlink|CreateAndRead|RemoveDirectoryLink|GitCapture).*'
$TestRunPattern = if ($env:CAMP_WINDOWS_DIRLINK_TEST_RUN) { $env:CAMP_WINDOWS_DIRLINK_TEST_RUN.Trim() } else { $DefaultTestRunPattern }
$MinTargetedTests = if ($env:CAMP_WINDOWS_DIRLINK_MIN_TESTS) { [int]$env:CAMP_WINDOWS_DIRLINK_MIN_TESTS } else { 5 }

$RunLinePattern = '(?m)^=== RUN[ \t]+(Test[A-Za-z0-9_/]+)\r?$'
$FinalProofMarker = 'Windows container directory-link proof passed'
$RequiredTestNames = @(
    'TestCreateAndReadDirectoryLink',
    'TestRemoveDirectoryLinkLeavesTarget',
    'TestBrokenDirectoryLinkState',
    'TestAddLinked_MarkerRollbackOnLinkCollision',
    'TestUnlinkProject_RemovesLinkOnly',
    'TestUnlinkProject_RejectsOrdinaryDirectory',
    'TestCaptureBlobsDoesNotFollowDirectoryLink',
    'TestSkillProjectionDirectoryLinkReadback',
    'TestGrokSkillsAliasDirectoryLink'
)

$TestPackages = @(
    @{ ImportPath = './internal/dirlink'; Optional = $false }
    @{ ImportPath = './internal/project'; Optional = $false }
    @{ ImportPath = './internal/git'; Optional = $false }
    @{ ImportPath = './internal/skills'; Optional = $false }
)

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
            "Cannot run $ContainerImage on this runner without a Windows container engine."
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

function Invoke-DockerExec([string]$ContainerId, [string]$CmdLine, [string]$User = '') {
    $args = @('exec')
    if ($User) {
        $args += @('-u', $User)
    }
    $args += @($ContainerId, 'cmd', '/c', $CmdLine)
    $out = & docker @args 2>&1
    $code = $LASTEXITCODE
    $text = $out | Out-String
    if ($code -ne 0) {
        throw "docker exec failed ($code): $text"
    }
    return $text
}

function Invoke-DockerExecPowerShell {
    param(
        [string]$ContainerId,
        [string]$ScriptPath,
        [string]$User = ''
    )
    $args = @('exec')
    if ($User) {
        $args += @('-u', $User)
    }
    $args += @($ContainerId, 'powershell.exe', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $ScriptPath)
    $out = & docker @args 2>&1
    $code = $LASTEXITCODE
    $text = $out | Out-String
    if ($code -ne 0) {
        throw "docker exec powershell failed ($code): $text"
    }
    return $text
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

function Test-PackageHasContainerTests([string]$ImportPath) {
    $listed = & go list -tags container_fs -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' $ImportPath 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "go list $ImportPath failed: $listed"
    }
    return [bool](($listed | Out-String).Trim())
}

function Compile-ContainerTestBinary {
    param(
        [string]$ImportPath,
        [string]$OutPath
    )
    $env:CGO_ENABLED = '0'
    & go test -c -tags container_fs -o $OutPath $ImportPath
    if ($LASTEXITCODE -ne 0) {
        throw "go test -c $ImportPath failed with exit code $LASTEXITCODE"
    }
}

function Count-TargetedTestRuns([string]$Output, [string]$Pattern) {
    $runs = [regex]::Matches($Output, $RunLinePattern)
    if ($runs.Count -eq 0) {
        return 0
    }
    $goPattern = [regex]::new("^$Pattern")
    $count = 0
    foreach ($m in $runs) {
        $name = $m.Groups[1].Value
        if ($goPattern.IsMatch($name)) {
            $count++
        }
    }
    return $count
}

function Get-ContainerUserSid([string]$ContainerId) {
    $identity = Invoke-DockerExec -ContainerId $ContainerId -CmdLine 'whoami /user /fo csv /nh' -User $ContainerUserName
    if ($identity -notmatch '(S-\d+(?:-\d+)+)') {
        throw "cannot resolve $ContainerUserName SID: $identity"
    }
    return $Matches[1]
}

function Initialize-ContainerFixtureACL([string]$ContainerId, [string]$WorkRoot, [string]$UserSid) {
    $cmd = @(
        "mkdir $WorkRoot\tmp"
        "icacls $WorkRoot /grant `"*${UserSid}:(OI)(CI)M`" /T /C /Q"
    ) -join ' && '
    Invoke-DockerExec -ContainerId $ContainerId -CmdLine $cmd | Out-Null
    Invoke-DockerExec -ContainerId $ContainerId -CmdLine "icacls D:\ /grant `"*${UserSid}:(OI)(CI)M`" /T /C /Q" | Out-Null
}

function Get-DistinctHostVolume {
    $disks = @(Get-CimInstance Win32_LogicalDisk | Where-Object { $_.DriveType -eq 3 })
    $systemDisk = $disks | Where-Object { $_.DeviceID -eq 'C:' } | Select-Object -First 1
    if (-not $systemDisk -or -not $systemDisk.VolumeSerialNumber) {
        throw 'cannot identify the host C: volume for cross-volume proof'
    }
    $other = $disks | Where-Object {
        $_.DeviceID -ne 'C:' -and $_.VolumeSerialNumber -and
        $_.VolumeSerialNumber -ne $systemDisk.VolumeSerialNumber
    } | Select-Object -First 1
    if (-not $other) {
        throw 'no distinct writable host volume for cross-volume Windows proof; use a runner with two volumes'
    }
    return $other.DeviceID
}

function Initialize-DeniedCampFixture {
    param(
        [string]$ContainerId,
        [string]$WorkRoot,
        [string]$DeniedYamlHost,
        [string]$UserSid
    )
    $deniedCamp = "$WorkRoot\camp-denied"
    $containerYaml = "$deniedCamp\.campaign\campaign.yaml"
    Invoke-DockerExec -ContainerId $ContainerId -CmdLine "mkdir $deniedCamp\.campaign $deniedCamp\projects" | Out-Null
    Invoke-DockerCli -DockerArgs @('cp', $DeniedYamlHost, "${ContainerId}:$containerYaml") | Out-Null
    $gitPath = "$WorkRoot\Git\cmd;$WorkRoot\Git\mingw64\bin"
    $initCmd = @(
        "set `"PATH=$gitPath;%PATH%`""
        "cd /d $deniedCamp"
        "git init -b main"
        "git config user.name camp-dirlink-proof"
        "git config user.email camp-dirlink-proof@users.noreply.github.com"
        "git add .campaign"
        "git commit -m init-denied-camp"
        "icacls $deniedCamp\projects /deny `"*${UserSid}:(OI)(CI)M`" /C /Q"
    ) -join ' && '
    Invoke-DockerExec -ContainerId $ContainerId -CmdLine $initCmd | Out-Null
}

try {
    Push-Location $RepoRoot

    Write-Host "directory-link test -test.run pattern: $TestRunPattern (override with CAMP_WINDOWS_DIRLINK_TEST_RUN)"
    Write-Host "minimum targeted test executions: $MinTargetedTests (override with CAMP_WINDOWS_DIRLINK_MIN_TESTS)"

    Report-DockerServices
    Start-DockerServiceIfPresent
    Report-DockerCapability

    $workHost = Join-Path $env:TEMP ("camp-windows-dirlink-proof-{0}" -f [guid]::NewGuid().ToString('n'))
    $otherDrive = Get-DistinctHostVolume
    $crossVolumeHost = Join-Path "$otherDrive\" ("camp-dirlink-cross-volume-{0}" -f [guid]::NewGuid().ToString('n'))
    New-Item -ItemType Directory -Path $workHost | Out-Null
    New-Item -ItemType Directory -Path $crossVolumeHost | Out-Null
    Write-Host "Cross-volume host fixture: $crossVolumeHost mounted as D: inside container"
    try {
        $compiled = [System.Collections.Generic.List[object]]::new()
        Write-Section 'Compile container_fs directory-link test binaries (host compile only; no FS mutations on host)'
        foreach ($pkg in $TestPackages) {
            $importPath = $pkg.ImportPath
            if (-not (Test-PackageHasContainerTests $importPath)) {
                if ($pkg.Optional) {
                    Write-Host "skip optional package (no container_fs tests yet): $importPath"
                    continue
                }
                throw "required package has no container_fs tests: $importPath"
            }
            $safeName = ($importPath -replace '[\\./]', '_')
            $outHost = Join-Path $workHost "$safeName.test.exe"
            Compile-ContainerTestBinary -ImportPath $importPath -OutPath $outHost
            $compiled.Add([pscustomobject]@{
                    ImportPath  = $importPath
                    HostPath    = $outHost
                    ContainerPath = "$ContainerWork\$safeName.test.exe"
                })
        }
        if ($compiled.Count -eq 0) {
            throw 'no directory-link test binaries compiled; add internal/dirlink or internal/project container_fs tests (task01)'
        }

        Write-Section 'Windows Server Core fixture'
        Invoke-DockerCli -DockerArgs @('pull', $ContainerImage)
        $createOut = & docker create --pull=never -v "${crossVolumeHost}:D:" $ContainerImage cmd /c "ping -n 3600 127.0.0.1 > nul" 2>&1
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
            Invoke-DockerExec -ContainerId $containerId -CmdLine "mkdir $ContainerWork" | Out-Null
            $userSid = Get-ContainerUserSid -ContainerId $containerId
            Write-Host "ContainerUser SID: $userSid"
            Initialize-ContainerFixtureACL -ContainerId $containerId -WorkRoot $ContainerWork -UserSid $userSid

            $gitRoot = Get-HostGitRoot
            Write-Host "Copying host Git from $gitRoot into container (needed before denied-camp admin fixture)"
            Invoke-DockerCli -DockerArgs @('cp', $gitRoot, "${containerId}:$ContainerWork\Git") | Out-Null
            $deniedYamlHost = Join-Path $workHost 'camp-denied-campaign.yaml'
            @(
                'id: camp-denied'
                'name: denied'
                'type: product'
            ) | Set-Content -LiteralPath $deniedYamlHost -Encoding utf8
            Initialize-DeniedCampFixture -ContainerId $containerId -WorkRoot $ContainerWork -DeniedYamlHost $deniedYamlHost -UserSid $userSid

            foreach ($bin in $compiled) {
                Invoke-DockerCli -DockerArgs @('cp', $bin.HostPath, "${containerId}:$($bin.ContainerPath)") | Out-Null
            }

            $testCmdParts = @(
                "set `"HOME=$ContainerWork`""
                "set `"USERPROFILE=$ContainerWork`""
                "set `"TEMP=$ContainerWork\tmp`""
                "set `"TMP=$ContainerWork\tmp`""
                "set `"PATH=$ContainerWork\Git\cmd;$ContainerWork\Git\mingw64\bin;%PATH%`""
                "cd /d $ContainerWork"
            )
            $totalTargeted = 0
            $allTestOut = New-Object System.Text.StringBuilder
            foreach ($bin in $compiled) {
                $runCmd = ($testCmdParts + @(
                        "$($bin.ContainerPath) -test.v -test.count=1 -test.timeout=10m -test.run=`"$TestRunPattern`""
                    )) -join ' && '
                Write-Section "Run $($bin.ImportPath) inside container as $ContainerUserName"
                $execOut = Invoke-DockerExec -ContainerId $containerId -CmdLine $runCmd -User $ContainerUserName
                Write-Host $execOut
                [void]$allTestOut.AppendLine($execOut)
                if ($execOut -notmatch 'PASS') {
                    throw "$($bin.ImportPath) suite output did not contain PASS"
                }
                if ($execOut -match '(?m)^--- SKIP:[ \t]+Test') {
                    throw "$($bin.ImportPath) skipped a targeted Windows proof test: $execOut"
                }
                $targeted = Count-TargetedTestRuns -Output $execOut -Pattern $TestRunPattern
                Write-Host "targeted tests executed in $($bin.ImportPath): $targeted"
                $totalTargeted += $targeted
            }
            if ($totalTargeted -lt $MinTargetedTests) {
                throw @(
                    "expected at least $MinTargetedTests targeted test(s) matching -test.run=$TestRunPattern;",
                    "saw $totalTargeted across compiled packages.",
                    "When task01 adds internal/dirlink tests, update CAMP_WINDOWS_DIRLINK_TEST_RUN if names differ.",
                    "Output:`n$($allTestOut.ToString())"
                ) -join ' '
            }
            foreach ($name in $RequiredTestNames) {
                $passed = '(?m)^--- PASS: ' + [regex]::Escape($name) + '(?: \(|\r?$)'
                if ($allTestOut.ToString() -notmatch $passed) {
                    throw "required native directory-link test did not pass: $name"
                }
            }

            Write-Section 'Compile camp.exe for project link CLI proof'
            $campOut = Join-Path $workHost 'camp.exe'
            $env:CGO_ENABLED = '0'
            & go build -o $campOut ./cmd/camp
            if ($LASTEXITCODE -ne 0) {
                throw "go build ./cmd/camp failed with exit code $LASTEXITCODE"
            }

            $innerScriptHost = Join-Path $PSScriptRoot 'windows-directory-links-container-proof-inner.ps1'
            if (-not (Test-Path -LiteralPath $innerScriptHost)) {
                throw "missing inner directory-link proof script: $innerScriptHost"
            }

            $containerBinDir = "$ContainerWork\bin"
            $containerInner = "$ContainerWork\dirlink-proof.ps1"
            Invoke-DockerExec -ContainerId $containerId -CmdLine "mkdir $containerBinDir" | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $campOut, "${containerId}:$containerBinDir\camp.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $innerScriptHost, "${containerId}:$containerInner") | Out-Null

            Write-Section "Run camp project link/list/readback/unlink proof as $ContainerUserName"
            $proofOut = Invoke-DockerExecPowerShell -ContainerId $containerId -ScriptPath $containerInner -User $ContainerUserName
            Write-Host $proofOut
            if ($proofOut -notmatch [regex]::Escape($FinalProofMarker)) {
                throw "directory-link CLI proof output missing success marker:`n$proofOut"
            }

            Write-Host ""
            Write-Host "Windows container directory-link proof: $totalTargeted targeted test(s) in $ContainerImage; CLI markers verified"
        } finally {
            & docker rm -f $containerId 2>&1 | Out-Null
        }
    } finally {
        Remove-Item -LiteralPath $workHost -Recurse -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $crossVolumeHost -Recurse -Force -ErrorAction SilentlyContinue
    }
} finally {
    Pop-Location
}
