param([int]$TargetId,[long]$StartTicks,[long]$Window,[string]$Executable,[string]$PrivateDirectory,[string]$EvidenceDirectory)
$ErrorActionPreference='Stop'
try {
    [Console]::Out.Write("worker-entered`n");[Console]::Out.Flush()
    . (Join-Path $PSScriptRoot 'original-desktop-window-capture.ps1')
    Assert-OriginalCaptureContext
    Assert-OriginalCapturePaths $Executable $PrivateDirectory $EvidenceDirectory
    $output=Join-Path $PrivateDirectory 'window-capture\original-magpie-uncertain.png'
    Assert-OriginalCaptureRegularPath $output
    [Console]::Out.Write("native-compile`n");[Console]::Out.Flush()
    Initialize-OriginalCaptureNative
    [OriginalCapture.Native]::Capture($TargetId,$StartTicks,$Executable,$Window,$output)
    [Console]::Out.Write("capture-complete`n");[Console]::Out.Flush()
    [Console]::Out.Write('{"schema":1,"status":"captured"}')
    exit 0
} catch {
    # No raw exception, path, arbitrary window text, or private output escapes.
    exit 1
}
