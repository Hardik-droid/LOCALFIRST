$ErrorActionPreference = 'Stop'
$packages = Join-Path $PSScriptRoot 'diwan\offline-package'
function Get-OfflinePackage {
    if (Test-Path -LiteralPath $packages -PathType Container) {
        Get-ChildItem -LiteralPath $packages -Directory -Filter 'windows-x64-*' |
            Where-Object {
                (Test-Path -LiteralPath (Join-Path $_.FullName 'localfirst-offline.exe') -PathType Leaf) -and
                (Test-Path -LiteralPath (Join-Path $_.FullName 'web\.localfirst-offline.json') -PathType Leaf) -and
                ((Test-Path -LiteralPath (Join-Path $_.FullName 'Start-LocalFirst-Offline.ps1') -PathType Leaf) -or
                 (Test-Path -LiteralPath (Join-Path $_.FullName 'Start-Diwan-Offline.ps1') -PathType Leaf))
            } | Sort-Object Name -Descending | Select-Object -First 1
    }
}
$package = Get-OfflinePackage
if (-not $package) {
    Write-Host 'No offline package found. Building it from the local source (Node.js and Go required for this first build).'
    if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw 'Install Node.js, then run this launcher again.' }
    Push-Location (Join-Path $PSScriptRoot 'diwan\web')
    try {
        if (-not (Test-Path -LiteralPath 'node_modules\.bin\vite.cmd' -PathType Leaf)) {
            & npm.cmd ci --no-audit --no-fund
            if ($LASTEXITCODE -ne 0) { throw 'Offline dependency installation failed.' }
        }
        & npm.cmd run build:offline
        if ($LASTEXITCODE -ne 0) { throw 'Offline frontend build failed.' }
        & npm.cmd run package:offline
        if ($LASTEXITCODE -ne 0) { throw 'Offline Windows package build failed. Check that Go is installed or available in .tools\go.' }
    } finally { Pop-Location }
    $package = Get-OfflinePackage
    if (-not $package) { throw 'The build did not produce a complete offline Windows package.' }
}
$launcher = Join-Path $package.FullName 'Start-LocalFirst-Offline.ps1'
if (-not (Test-Path -LiteralPath $launcher -PathType Leaf)) { $launcher = Join-Path $package.FullName 'Start-Diwan-Offline.ps1' }
if (-not (Test-Path -LiteralPath $launcher -PathType Leaf)) { throw 'The package has no offline launcher.' }
& $launcher
