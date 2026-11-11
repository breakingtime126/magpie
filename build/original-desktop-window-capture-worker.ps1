param([int]$TargetId,[long]$StartTicks,[long]$Window,[string]$Executable,[string]$PrivateDirectory,[string]$EvidenceDirectory)
# Progress is nonessential UI; errors remain terminating and privately classified.
$ProgressPreference='SilentlyContinue'
$ErrorActionPreference='Stop'
try {
    [Console]::Out.Write("worker-entered`n");[Console]::Out.Flush()
    . ([IO.Path]::Combine($PSScriptRoot,'original-desktop-window-capture.ps1'))
    [Console]::Out.Write("helper-loaded`n");[Console]::Out.Flush()
    Assert-OriginalCaptureContext
    [Console]::Out.Write("context-checked`n");[Console]::Out.Flush()
    Assert-OriginalCapturePaths $Executable $PrivateDirectory $EvidenceDirectory
    [Console]::Out.Write("fixture-checked`n");[Console]::Out.Flush()
    $output=[IO.Path]::Combine($PrivateDirectory,'window-capture','original-magpie-uncertain.png')
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
