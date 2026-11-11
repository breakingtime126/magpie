# One disposable download only. The parent bounds this owned process externally.
param([string]$Kind,[string]$Destination)
$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
. (Join-Path $PSScriptRoot 'original-desktop-package.ps1')
$result=[ordered]@{schema=1;status='blocked';bytes=0;failure=$null}
try {
    Assert-OriginalDesktopPackageRunner
    Assert-OriginalDesktopDownloadDestination $Kind $Destination
    # Reserve a newly created file in the caller's private directory. Never reuse
    # a prior output or delete a partial output as an implicit download retry.
    $reservation=[IO.File]::Open($Destination,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
    $reservation.Dispose()
    if ($Kind -ceq 'package') {
        Invoke-WebRequest -UseBasicParsing -Uri 'https://persistent.oaistatic.com/codex-app-prod/releases/26.1103.4239.0/ChatGPT-x64.msix' -OutFile $Destination
    } else {
        Invoke-WebRequest -UseBasicParsing -Uri 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-License.xml' -OutFile $Destination
    }
    Assert-PublicRegularPath $Destination
    $length=(Get-Item -LiteralPath $Destination -ErrorAction Stop).Length
    if ($length -lt 1 -or $length -gt 4294967296) { throw 'Official download length is outside its bound.' }
    $result.status='completed';$result.bytes=[long]$length
} catch { $result.failure=Get-OriginalDesktopFailureEvidence $_ }
# No command output, exception text, response body, or destination is published.
$result | ConvertTo-Json -Depth 4 -Compress
