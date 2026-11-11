param([int]$TargetId,[long]$StartTicks,[long]$Window,[string]$Executable,[string]$PrivateDirectory,[string]$EvidenceDirectory)
$ErrorActionPreference='Stop'
try {
    . (Join-Path $PSScriptRoot 'original-desktop-window-capture.ps1')
    Assert-OriginalCaptureContext
    Assert-OriginalCapturePaths $Executable $PrivateDirectory $EvidenceDirectory
    $output=Join-Path $PrivateDirectory 'window-capture\original-magpie-uncertain.png'
    Assert-OriginalCaptureRegularPath $output
    Initialize-OriginalCaptureNative
    [OriginalCapture.Native]::Capture($TargetId,$StartTicks,$Executable,$Window,$output)
    [Console]::Out.Write('{"schema":1,"status":"captured"}')
    exit 0
} catch {
    # No raw exception, path, arbitrary window text, or private output escapes.
    exit 1
}
