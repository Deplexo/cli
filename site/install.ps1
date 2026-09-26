#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Deplexo\bin'),
    [string]$Version = $env:DEPLEXO_VERSION
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($env:OS -ne 'Windows_NT') { throw 'Use install.sh on Linux or macOS.' }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$cpu = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) { $cpu = $env:PROCESSOR_ARCHITEW6432 }
$arch = switch ($cpu) { 'AMD64' { 'amd64' } 'ARM64' { 'arm64' } default { throw 'This CPU architecture is not supported.' } }
$repo = 'https://github.com/Deplexo/cli'
$number = '(0|[1-9][0-9]*)'
$identifier = "($number|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
$versionPattern = "\Av$number\.$number\.$number(-$identifier(\.$identifier)*)?\z"
if ($Version) {
    if ($Version -cnotmatch $versionPattern) { throw 'Version must be a release tag such as v0.1.0 or v0.1.0-beta.1.' }
    $tag = $Version
}
else {
    try { $tag = (Invoke-RestMethod -Uri 'https://cli.deplexo.com/latest-version' -TimeoutSec 30).TrimEnd("`r", "`n") }
    catch { throw 'Could not find the current release. Check https://github.com/Deplexo/cli/releases.' }
    if ($tag -cnotmatch "\Av$number\.$number\.$number(-beta\.$number)?\z") { throw 'The release version is invalid.' }
}
$work = Join-Path ([IO.Path]::GetTempPath()) ('deplexo-install-' + [guid]::NewGuid())
$stage = $null
try {
    $null = New-Item -ItemType Directory -Path $work
    $archive = "deplexo_${tag}_windows_${arch}.zip"
    Write-Host "Downloading Deplexo $tag for windows/$arch..."
    Invoke-WebRequest -UseBasicParsing -Uri "$repo/releases/download/$tag/$archive" -OutFile (Join-Path $work $archive) -TimeoutSec 180
    Invoke-WebRequest -UseBasicParsing -Uri "$repo/releases/download/$tag/SHA256SUMS" -OutFile (Join-Path $work 'SHA256SUMS') -TimeoutSec 30
    $pattern = '^([0-9a-f]{64})  ' + [regex]::Escape($archive) + '$'
    $matchesFound = @(Get-Content -LiteralPath (Join-Path $work 'SHA256SUMS') | Where-Object { $_ -cmatch $pattern })
    if ($matchesFound.Count -ne 1) { throw 'The release checksum is missing or ambiguous.' }
    $expected = [regex]::Match($matchesFound[0], $pattern).Groups[1].Value
    $actual = (Get-FileHash -LiteralPath (Join-Path $work $archive) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -cne $expected) { throw 'The download checksum does not match. Your installed binary was not changed.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $work $archive))
    try {
        $entries = @($zip.Entries | Where-Object { $_.FullName -ceq 'deplexo.exe' })
        if ($entries.Count -ne 1 -or $entries[0].Length -eq 0) { throw 'The archive must contain one executable.' }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], (Join-Path $work 'deplexo.exe'))
    }
    finally { $zip.Dispose() }
    & (Join-Path $work 'deplexo.exe') version
    if ($LASTEXITCODE -ne 0) { throw 'The downloaded binary cannot run on this system.' }
    if ($InstallDir -notmatch '^(?:[A-Za-z]:[\\/]|\\\\[^\\/]+[\\/][^\\/]+[\\/]?)') { throw 'InstallDir must be an absolute path.' }
    $null = New-Item -ItemType Directory -Path $InstallDir -Force
    $target = Join-Path $InstallDir 'deplexo.exe'
    $stage = Join-Path $InstallDir ('.deplexo-' + [guid]::NewGuid() + '.exe')
    Copy-Item -LiteralPath (Join-Path $work 'deplexo.exe') -Destination $stage
    if (Test-Path -LiteralPath $target) {
        if ((Get-Item -LiteralPath $target).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'The installed executable is a link. Choose another install directory.' }
        [IO.File]::Replace($stage, $target, $null)
    }
    else { [IO.File]::Move($stage, $target) }
    $stage = $null
    Write-Host "Installed Deplexo $tag at $target"
    if (($env:PATH -split ';') -notcontains $InstallDir) { Write-Host "Add $InstallDir to your user PATH, then open a new terminal." }
    Write-Host 'Run deplexo auth login to sign in.'
    if ($Version) { Write-Host 'To update, choose a newer -Version.' }
    else { Write-Host 'Run this installer again to update.' }
}
finally {
    if ($stage -and (Test-Path -LiteralPath $stage)) { Remove-Item -LiteralPath $stage -Force }
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
}
