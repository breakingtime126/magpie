param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[0-9a-f]{40}$')]
    [string]$ExpectedSha
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($PSVersionTable.PSEdition -ne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) {
    throw 'This job requires Windows PowerShell 5.1.'
}
$actual = git rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $actual.Trim() -cne $ExpectedSha) {
    throw 'Checkout does not match the triggering commit.'
}
& "$PSScriptRoot/preview.test.ps1"
if (-not $?) { throw 'Draft tests failed.' }

# Build only the standalone checker; no native backend or application is launched.
$package = Join-Path $env:RUNNER_TEMP ('bridge-helper-package-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $package -ErrorAction Stop)
try {
    Copy-Item -LiteralPath "$PSScriptRoot/preview.ps1" -Destination $package
    $adapter = Join-Path $package 'magpie-codex-bridge-windows-amd64.exe'
    & go build -tags nogui -o $adapter ./cmd/magpie-codex-bridge
    if ($LASTEXITCODE -ne 0) { throw 'Independent checker build failed.' }
    @{ nativeAdapterSHA256=(Get-FileHash -LiteralPath $adapter -Algorithm SHA256).Hash.ToLowerInvariant() } |
        ConvertTo-Json | Set-Content -LiteralPath (Join-Path $package 'BUILD.json') -Encoding UTF8
    & "$PSScriptRoot/preview.gui.test.ps1" -PackageDirectory $package -EvidenceDirectory (Join-Path $env:RUNNER_TEMP 'bridge-helper-evidence')
    if (-not $?) { throw 'Owned-window acceptance failed.' }
} finally { Remove-Item -LiteralPath $package -Recurse -Force -ErrorAction Stop }
