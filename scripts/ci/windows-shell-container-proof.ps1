#Requires -Version 7.0
<#
  Compiles native Camp test binaries and stable/dev camp.exe on the host, then
  runs shell, flow, artifact, and peer proofs in PowerShell 5.1 Server Core.
  All fixture filesystem mutations occur inside the container.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$ContainerImage = 'mcr.microsoft.com/windows/servercore:ltsc2025'
$ContainerWork = 'C:\camp-shell-proof'
$ContainerUserName = 'ContainerUser'
$TestNames = @(
    'TestPowerShellNavigationFunctionsInFreshSession',
    'TestPowerShellNativeRestoresOutputEncoding',
    'TestPowerShellUnicodeDirectoryNavigation',
    'TestPowerShellBrowserPathOutput'
)
$FlowTestNames = @(
    'TestPowerShellFlowLiteralArgsAndWorkDir',
    'TestPowerShellFlowSyntaxFailureDoesNotRunPrefix',
    'TestPowerShellFlowCancellationStopsFollowup',
    'TestPowerShellFlowCancellationStopsDescendant'
)
$ShellCommandTestNames = @(
    'TestPowerShellCommandPreservesLiteralArguments',
    'TestPowerShellCommandSyntaxFailureDoesNotRunPrefix',
    'TestCommandRejectsUnknownInterpreter'
)
$ArtifactTestNames = @(
    'TestArtifactWindowsNativeShellLaunch',
    'TestArtifactWindowsNativeMissingAssociation'
)
$PeerTestSuites = @(
    @{ Package = 'remote'; Test = 'TestWindowsSSHPeerFailsBeforeDial' },
    @{ Package = 'peer'; Test = 'TestWindowsPeerResolutionFailsBeforeMachineLookup' },
    @{ Package = 'clone'; Test = 'TestWindowsPeerCloneFailsBeforeCreatingDestination' },
    @{ Package = 'sync'; Test = 'TestWindowsPeerSyncFailsBeforePreflight' },
    @{ Package = 'transfer'; Test = 'TestWindowsRemoteCopyFailsBeforeAnyTransfer' },
    @{ Package = 'artifacts'; Test = 'TestWindowsArtifactPullCannotReportIncompleteCopyAsSynced' }
)
$ProofMarker = 'Camp PowerShell 5.1 command and dev-flow proof passed'
$PeerProofMarker = 'Windows container peer boundary proof passed'

function Invoke-DockerCli([string[]]$DockerArgs) {
    & docker @DockerArgs
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($DockerArgs -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Invoke-DockerExec([string]$ContainerId, [string]$CmdLine, [string]$User = '') {
    $args = @('exec')
    if ($User) { $args += @('-u', $User) }
    $args += @($ContainerId, 'cmd', '/c', $CmdLine)
    $out = & docker @args 2>&1
    $code = $LASTEXITCODE
    $text = $out | Out-String
    if ($code -ne 0) { throw "docker exec failed ($code): $text" }
    return $text
}

function Invoke-DockerProof([string[]]$DockerArgs, [string]$PassMarker,
    [string]$Label, [int]$TimeoutSeconds = 300) {
    # PowerShell's native stderr pipeline can interpret the child PS 5.1
    # CLIXML stream as malformed XML, hiding the actual proof error.
    $start = [System.Diagnostics.ProcessStartInfo]::new()
    $start.FileName = (Get-Command docker.exe -ErrorAction Stop).Source
    $start.UseShellExecute = $false
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($arg in $DockerArgs) { [void]$start.ArgumentList.Add($arg) }

    $process = [System.Diagnostics.Process]::Start($start)
    if (-not $process) { throw "could not start docker for $Label" }
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
            $process.Kill($true)
            $process.WaitForExit()
            Write-Host ($stdout.GetAwaiter().GetResult() + $stderr.GetAwaiter().GetResult())
            throw "$Label timed out after $TimeoutSeconds seconds"
        }
        $process.WaitForExit()
        [string]$message = $stdout.GetAwaiter().GetResult() + $stderr.GetAwaiter().GetResult()
        $exitCode = $process.ExitCode
        $hasMarker = $message.Contains($PassMarker)
        Write-Host $message
        if ($exitCode -ne 0 -or -not $hasMarker) {
            throw "$Label failed (exit $exitCode); required marker: $PassMarker"
        }
    } finally {
        $process.Dispose()
    }
}

function Ensure-WindowsContainerEngine {
    foreach ($name in @('com.docker.service', 'docker')) {
        $service = Get-Service -Name $name -ErrorAction SilentlyContinue
        if (-not $service) { continue }
        if ($service.Status -ne 'Running') {
            Start-Service -Name $name
            ($service = Get-Service -Name $name).WaitForStatus('Running', (New-TimeSpan -Minutes 2))
        }
        break
    }
    $info = & docker info -f '{{.OSType}}' 2>&1
    if ($LASTEXITCODE -ne 0) { throw "docker info failed: $($info | Out-String)" }
    $osType = ($info | Out-String).Trim()
    if ($osType -ieq 'windows') { return }
    $dockerCli = Join-Path $env:ProgramFiles 'Docker\Docker\DockerCli.exe'
    if (-not (Test-Path -LiteralPath $dockerCli)) {
        throw "Docker daemon is in '$osType' mode and DockerCli.exe is unavailable; Windows containers are required"
    }
    & $dockerCli -SwitchDaemon
    if ($LASTEXITCODE -ne 0) { throw "DockerCli.exe -SwitchDaemon failed ($LASTEXITCODE)" }
    Start-Sleep -Seconds 5
    $info = & docker info -f '{{.OSType}}' 2>&1
    if ($LASTEXITCODE -ne 0 -or ($info | Out-String).Trim() -ine 'windows') {
        throw "Docker daemon did not switch to Windows containers: $($info | Out-String)"
    }
}

Push-Location $RepoRoot
try {
    Ensure-WindowsContainerEngine
    $workHost = Join-Path $env:TEMP ("camp-windows-shell-proof-{0}" -f [guid]::NewGuid().ToString('n'))
    New-Item -ItemType Directory -Path $workHost | Out-Null
    try {
        $campHost = Join-Path $workHost 'camp.exe'
        $campDevHost = Join-Path $workHost 'camp-dev.exe'
        $probeHost = Join-Path $workHost 'argv-probe.exe'
        $testsHost = Join-Path $workHost 'shell.test.exe'
        $flowTestsHost = Join-Path $workHost 'flow.test.exe'
        $shellCommandTestsHost = Join-Path $workHost 'shellcmd.test.exe'
        $artifactTestsHost = Join-Path $workHost 'camp-artifacts.test.exe'
        $env:CGO_ENABLED = '0'
        & go build -o $campHost ./cmd/camp
        if ($LASTEXITCODE -ne 0) { throw "go build ./cmd/camp failed ($LASTEXITCODE)" }
        & go build -tags dev -o $campDevHost ./cmd/camp
        if ($LASTEXITCODE -ne 0) { throw "go build -tags dev ./cmd/camp failed ($LASTEXITCODE)" }
        & go build -o $probeHost ./scripts/ci/windows-argv-probe.go
        if ($LASTEXITCODE -ne 0) { throw "go build Windows argv probe failed ($LASTEXITCODE)" }
        & go test -c -tags container_fs -o $testsHost ./internal/shell
        if ($LASTEXITCODE -ne 0) { throw "go test -c ./internal/shell failed ($LASTEXITCODE)" }
        & go test -c -tags container_fs -o $flowTestsHost ./internal/flow
        if ($LASTEXITCODE -ne 0) { throw "go test -c ./internal/flow failed ($LASTEXITCODE)" }
        & go test -c -o $shellCommandTestsHost ./internal/shellcmd
        if ($LASTEXITCODE -ne 0) { throw "go test -c ./internal/shellcmd failed ($LASTEXITCODE)" }
        & go test -c -tags container_fs -o $artifactTestsHost ./cmd/camp
        if ($LASTEXITCODE -ne 0) { throw "go test -c ./cmd/camp failed ($LASTEXITCODE)" }
        foreach ($peerSuite in $PeerTestSuites) {
            $peerTestHost = Join-Path $workHost "$($peerSuite.Package).test.exe"
            & go test -c -o $peerTestHost "./internal/$($peerSuite.Package)"
            if ($LASTEXITCODE -ne 0) {
                throw "go test -c ./internal/$($peerSuite.Package) failed ($LASTEXITCODE)"
            }
        }

        $innerHost = Join-Path $PSScriptRoot 'windows-shell-container-proof-inner.ps1'
        if (-not (Test-Path -LiteralPath $innerHost)) { throw "missing shell proof script: $innerHost" }
        $peerInnerHost = Join-Path $PSScriptRoot 'windows-peer-container-proof-inner.ps1'
        if (-not (Test-Path -LiteralPath $peerInnerHost)) { throw "missing peer proof script: $peerInnerHost" }

        Invoke-DockerCli -DockerArgs @('pull', $ContainerImage)
        $createOut = & docker create --pull=never $ContainerImage cmd /c "ping -n 3600 127.0.0.1 > nul" 2>&1
        if ($LASTEXITCODE -ne 0) { throw "docker create failed: $($createOut | Out-String)" }
        $containerId = ($createOut | Out-String).Trim()
        if ($containerId -notmatch '^[0-9a-f]{64}$') { throw "invalid container id: $containerId" }
        try {
            Invoke-DockerCli -DockerArgs @('start', $containerId) | Out-Null
            Invoke-DockerExec -ContainerId $containerId -CmdLine "mkdir $ContainerWork" | Out-Null
            Invoke-DockerExec -ContainerId $containerId -CmdLine "mkdir $ContainerWork\bin" | Out-Null
            Invoke-DockerExec -ContainerId $containerId -CmdLine "mkdir $ContainerWork\tmp" | Out-Null
            $identity = Invoke-DockerExec -ContainerId $containerId -CmdLine 'whoami /user /fo csv /nh' -User $ContainerUserName
            if ($identity -notmatch '(S-\d+(?:-\d+)+)') { throw "cannot resolve ContainerUser SID: $identity" }
            $userSid = $Matches[1]
            Invoke-DockerCli -DockerArgs @('exec', $containerId, 'icacls.exe', $ContainerWork,
                '/grant', "*${userSid}:(OI)(CI)M", '/T', '/C', '/Q') | Out-Null

            Invoke-DockerCli -DockerArgs @('cp', $campHost, "${containerId}:$ContainerWork\bin\camp.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $campDevHost, "${containerId}:$ContainerWork\bin\camp-dev.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $probeHost, "${containerId}:$ContainerWork\bin\argv-probe.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $testsHost, "${containerId}:$ContainerWork\shell.test.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $flowTestsHost, "${containerId}:$ContainerWork\flow.test.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $shellCommandTestsHost, "${containerId}:$ContainerWork\shellcmd.test.exe") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $artifactTestsHost, "${containerId}:$ContainerWork\camp-artifacts.test.exe") | Out-Null
            foreach ($peerSuite in $PeerTestSuites) {
                $peerTestHost = Join-Path $workHost "$($peerSuite.Package).test.exe"
                Invoke-DockerCli -DockerArgs @('cp', $peerTestHost,
                    "${containerId}:$ContainerWork\$($peerSuite.Package).test.exe") | Out-Null
            }
            Invoke-DockerCli -DockerArgs @('cp', $innerHost, "${containerId}:$ContainerWork\shell-proof.ps1") | Out-Null
            Invoke-DockerCli -DockerArgs @('cp', $peerInnerHost, "${containerId}:$ContainerWork\peer-proof.ps1") | Out-Null

            $testRun = '^(' + ($TestNames -join '|') + ')$'
            $testArgs = @(
                'exec', '-u', $ContainerUserName, '-w', $ContainerWork,
                '-e', "HOME=$ContainerWork", '-e', "USERPROFILE=$ContainerWork",
                '-e', "TEMP=$ContainerWork\tmp", '-e', "TMP=$ContainerWork\tmp",
                $containerId, "$ContainerWork\shell.test.exe",
                '-test.v', '-test.count=1', '-test.timeout=5m', "-test.run=$testRun"
            )
            $testOut = & docker @testArgs 2>&1 | Out-String
            if ($LASTEXITCODE -ne 0) { throw "native shell tests failed: $testOut" }
            foreach ($name in $TestNames) {
                if ($testOut -notmatch ('(?m)^--- PASS: ' + [regex]::Escape($name) + ' \(')) {
                    throw "native shell test did not pass: $name`n$testOut"
                }
            }
            Write-Host $testOut

            foreach ($suite in @(
                @{ Exe = 'flow.test.exe'; Names = $FlowTestNames; Timeout = '5m' },
                @{ Exe = 'shellcmd.test.exe'; Names = $ShellCommandTestNames; Timeout = '5m' },
                @{ Exe = 'camp-artifacts.test.exe'; Names = $ArtifactTestNames; Timeout = '2m' }
            )) {
                $suiteRun = '^(' + ($suite.Names -join '|') + ')$'
                $suiteArgs = @(
                    'exec', '-u', $ContainerUserName, '-w', $ContainerWork,
                    '-e', "HOME=$ContainerWork", '-e', "USERPROFILE=$ContainerWork",
                    '-e', "TEMP=$ContainerWork\tmp", '-e', "TMP=$ContainerWork\tmp",
                    $containerId, "$ContainerWork\$($suite.Exe)",
                    '-test.v', '-test.count=1', "-test.timeout=$($suite.Timeout)", "-test.run=$suiteRun"
                )
                $suiteOut = & docker @suiteArgs 2>&1 | Out-String
                if ($LASTEXITCODE -ne 0) { throw "native $($suite.Exe) tests failed: $suiteOut" }
                foreach ($name in $suite.Names) {
                    if ($suiteOut -notmatch ('(?m)^--- PASS: ' + [regex]::Escape($name) + ' \(')) {
                        throw "native test did not pass: $name`n$suiteOut"
                    }
                }
                Write-Host $suiteOut
            }

            foreach ($peerSuite in $PeerTestSuites) {
                $peerTestArgs = @(
                    'exec', '-u', $ContainerUserName, '-w', $ContainerWork,
                    '-e', "HOME=$ContainerWork", '-e', "USERPROFILE=$ContainerWork",
                    '-e', "TEMP=$ContainerWork\tmp", '-e', "TMP=$ContainerWork\tmp",
                    $containerId, "$ContainerWork\$($peerSuite.Package).test.exe",
                    '-test.v', '-test.count=1', '-test.timeout=2m', "-test.run=^$($peerSuite.Test)$"
                )
                $peerTestOut = & docker @peerTestArgs 2>&1 | Out-String
                if ($LASTEXITCODE -ne 0 -or
                    $peerTestOut -notmatch ('(?m)^--- PASS: ' + [regex]::Escape($peerSuite.Test) + ' \(')) {
                    throw "native $($peerSuite.Package) test did not pass: $($peerSuite.Test)`n$peerTestOut"
                }
                Write-Host $peerTestOut
            }

            $proofArgs = @(
                'exec', '-u', $ContainerUserName, '-w', $ContainerWork,
                '-e', "HOME=$ContainerWork", '-e', "USERPROFILE=$ContainerWork",
                '-e', "TEMP=$ContainerWork\tmp", '-e', "TMP=$ContainerWork\tmp",
                $containerId, 'powershell.exe', '-NoProfile', '-NonInteractive',
                '-ExecutionPolicy', 'Bypass', '-File', "$ContainerWork\shell-proof.ps1"
            )
            Invoke-DockerProof -DockerArgs $proofArgs -PassMarker $ProofMarker `
                -Label 'real camp.exe PowerShell 5.1 proof'

            $peerProofArgs = @(
                'exec', '-u', $ContainerUserName, '-w', $ContainerWork,
                '-e', "HOME=$ContainerWork", '-e', "USERPROFILE=$ContainerWork",
                '-e', "TEMP=$ContainerWork\tmp", '-e', "TMP=$ContainerWork\tmp",
                $containerId, 'powershell.exe', '-NoProfile', '-NonInteractive',
                '-ExecutionPolicy', 'Bypass', '-File', "$ContainerWork\peer-proof.ps1"
            )
            Invoke-DockerProof -DockerArgs $peerProofArgs -PassMarker $PeerProofMarker `
                -Label 'real camp.exe peer boundary proof'
        } finally {
            & docker rm -f $containerId 2>&1 | Out-Null
        }
    } finally {
        Remove-Item -LiteralPath $workHost -Recurse -Force -ErrorAction SilentlyContinue
    }
} finally {
    Pop-Location
}
