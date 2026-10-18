# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Installs the aicm agent for the current user on Windows: downloads the
# release, verifies its checksum, enrolls the machine and registers a
# scheduled task that starts at logon and restarts on failure.
[CmdletBinding()]
param(
    [string]$Hub,
    [string]$Org,
    [string]$Key,
    [string]$Token,
    [string]$Version,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

$Repo = 'atakanatamert/aicm'
$Binary = 'aicm-agent'
$DefaultBase = "https://github.com/$Repo/releases"
$DownloadBase = if ($env:AICM_AGENT_DOWNLOAD_BASE) { $env:AICM_AGENT_DOWNLOAD_BASE.TrimEnd('/') } else { $DefaultBase }
$InstallDir = Join-Path $env:LOCALAPPDATA $Binary
$Exe = Join-Path $InstallDir "$Binary.exe"
$TaskName = $Binary
$DataDir = Join-Path $env:APPDATA $Binary

function Stop-AgentTask {
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -ne $task) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    }
}

if ($Uninstall) {
    Stop-AgentTask
    if (Test-Path -LiteralPath $Exe) {
        Remove-Item -LiteralPath $Exe -Force
    }
    Write-Host "Uninstalled $Binary. Data kept at $DataDir"
    return
}

if (-not $Hub -or -not $Org -or -not $Key -or -not $Token) {
    throw "Usage: install.ps1 -Hub URL -Org ID -Key KEY -Token TOKEN [-Version vX.Y.Z] [-Uninstall]"
}

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$hasVersion = [bool]$Version
if (-not $hasVersion) {
    $headers = @{ 'User-Agent' = $Binary }
    try {
        $Version = (Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers).tag_name
    }
    catch {
        # No stable release yet: take the newest pre-release.
        $Version = (Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases?per_page=1" -Headers $headers)[0].tag_name
    }
}
if (-not $Version.StartsWith('v')) {
    $Version = "v$Version"
}
$assetVersion = $Version.Substring(1)
$asset = "${Binary}_${assetVersion}_windows_${arch}.zip"
$releaseDir = "$DownloadBase/download/$Version"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("aicm-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $zip = Join-Path $tmp $asset
    $sums = Join-Path $tmp 'checksums.txt'
    Write-Host "Downloading $asset"
    Invoke-WebRequest -Uri "$releaseDir/$asset" -OutFile $zip -UseBasicParsing
    Invoke-WebRequest -Uri "$releaseDir/checksums.txt" -OutFile $sums -UseBasicParsing

    $expected = $null
    foreach ($line in Get-Content -LiteralPath $sums) {
        $fields = $line -split '\s+'
        if ($fields.Count -ge 2) {
            $name = $fields[$fields.Count - 1].TrimStart('*')
            if ($name -eq $asset) {
                $expected = $fields[0]
                break
            }
        }
    }
    if (-not $expected) {
        throw "checksums.txt has no entry for $asset"
    }
    $actual = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash
    if ($actual -ne $expected) {
        throw "Checksum mismatch for $asset (expected $expected, got $actual)"
    }

    Expand-Archive -LiteralPath $zip -DestinationPath $tmp -Force
    $sourceExe = Join-Path $tmp "$Binary.exe"
    if (-not (Test-Path -LiteralPath $sourceExe)) {
        $found = Get-ChildItem -Path $tmp -Filter "$Binary.exe" -Recurse | Select-Object -First 1
        if ($null -eq $found) {
            throw "$asset does not contain $Binary.exe"
        }
        $sourceExe = $found.FullName
    }

    Stop-AgentTask
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -LiteralPath $sourceExe -Destination $Exe -Force

    Write-Host "Enrolling with $Hub"
    & $Exe enroll --hub $Hub --org $Org --key $Key --token $Token
    if ($LASTEXITCODE -ne 0) {
        throw "enroll failed with exit code $LASTEXITCODE"
    }

    $action = New-ScheduledTaskAction -Execute $Exe -Argument 'run'
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
    $settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) `
        -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero)
    $principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings `
        -Principal $principal -Force | Out-Null
    Start-ScheduledTask -TaskName $TaskName

    Write-Host "Installed $Binary and registered scheduled task '$TaskName'."
}
finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
