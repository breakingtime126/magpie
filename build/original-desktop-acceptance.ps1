# One disposable hosted-Windows original-UI transaction. No live-account work.
[CmdletBinding()]
param([string]$PackageDirectory,[string]$EvidenceDirectory,[string]$SourceCommit)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-connect-action.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-package.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-environment.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-sequence.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-cleanup.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-quit-discovery.ps1')
$stageDiagnosticsOnly=$env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS -ceq 'stage-diagnostics-v1'
if (-not $stageDiagnosticsOnly) { . (Join-Path $PSScriptRoot 'original-desktop-window-capture.ps1') }
. (Join-Path $PSScriptRoot 'original-connect-preconditions.ps1')
. (Join-Path $PSScriptRoot 'original-connect-stages.ps1')
. (Join-Path $PSScriptRoot 'original-backend-handshake.ps1')
function Get-AcceptanceBuildTags([bool]$StageOnly,[string]$CommitMessage) {
    $handshake=$CommitMessage.Contains('[original-backend-handshake-v1]')
    if ($handshake -and (-not $StageOnly -or -not $CommitMessage.Contains('[original-connect-stages-only-v1]') -or
        -not $CommitMessage.Contains('[original-connect-desktop-v1]'))) { throw 'Handshake diagnostics require original stage-only acceptance.' }
    $gui='production,nativecodex';$adapter='nogui,nativecodex'
    if ($handshake) { $gui+=',nativehandshakediag';$adapter+=',nativehandshakediag' }
    return [ordered]@{gui=$gui;adapter=$adapter;handshakeDiagnostic=$handshake}
}
function Assert-AcceptanceBuildTags($Build,$Expected) {
    if ($Build.buildTags -cne $Expected.gui -or $Build.adapterBuildTags -cne $Expected.adapter -or
        $Build.handshakeDiagnostic -isnot [bool] -or $Build.handshakeDiagnostic -ne $Expected.handshakeDiagnostic) {
        throw 'Original package build mode mismatch.'
    }
}
function Assert-AcceptanceContext {
    Assert-OriginalDesktopPackageRunner
    if ($env:GITHUB_EVENT_NAME -cne 'push' -or $SourceCommit -cnotmatch '^[0-9a-f]{40}$' -or
        $SourceCommit -cne $env:GITHUB_SHA -or $env:MAGPIE_ORIGINAL_CONNECT_ACTION -cne 'original-ui-v1') {
        throw 'Exact explicitly opted-in source required.'
    }
    $head=(& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $head -cne $SourceCommit) { throw 'Source mismatch.' }
    $message=(& git log -1 --format=%B) -join "`n"
    if ($LASTEXITCODE -ne 0 -or -not $message.Contains('[original-connect-desktop-v1]')) { throw 'Acceptance marker required.' }
    if ($env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS -and (-not $stageDiagnosticsOnly -or -not $message.Contains('[original-connect-stages-only-v1]'))) { throw 'Stage diagnostics marker required.' }
    if ($message.Contains('[original-connect-stages-only-v1]') -ne $stageDiagnosticsOnly) { throw 'Stage diagnostics mode mismatch.' }
    foreach ($path in @($PackageDirectory,$EvidenceDirectory)) { Assert-OriginalRegularPath $path }
    if ($PackageDirectory -cne (Join-Path $env:RUNNER_TEMP 'native-magpie-package') -or
        $EvidenceDirectory -cne (Join-Path $env:RUNNER_TEMP 'native-magpie-evidence') -or
        -not [IO.Directory]::Exists($PackageDirectory) -or -not [IO.Directory]::Exists($EvidenceDirectory)) {
        throw 'Owned package and evidence directories required.'
    }
    return Get-AcceptanceBuildTags $stageDiagnosticsOnly $message
}
function Test-AcceptanceJournal([string]$Path) {
    Assert-OriginalRegularPath $Path
    try { $attributes=[IO.File]::GetAttributes($Path) }
    catch [IO.FileNotFoundException] { return $false }
    catch [IO.DirectoryNotFoundException] { return $false }
    if (($attributes -band [IO.FileAttributes]::Directory) -ne 0) { throw 'Journal is not a regular file.' }
    return $true
}
function Get-AcceptanceSupplierFixture {
    # Ordinary explicit provider picks only. This public inert key is never a
    # credential. No account, plugin, model fetch, refresh or decision endpoint.
    return [ordered]@{noAutoGroups=$true;providers=@([ordered]@{
        id='original-ui-fixture';name='AIHub isolated acceptance fixture';
        key='PUBLIC-INERT-NOT-A-CREDENTIAL';responses='https://aihub.friendtimes.net/v1';
        models=@('original-ui-fixture-model')})}
}
function Get-AcceptanceDiagnostics([bool]$CaptureFailure,[scriptblock]$Capture,[scriptblock]$Tray,[scriptblock]$Preconditions) {
    $diagnostics=[ordered]@{capture=$null;tray=[ordered]@{status='unavailable'};preconditions=[ordered]@{status='unavailable'}}
    try { $diagnostics.preconditions=& $Preconditions } catch { }
    if ($CaptureFailure) {
        $diagnostics.capture=[ordered]@{status='unavailable'}
        try { $diagnostics.capture=& $Capture } catch { }
    }
    try { $diagnostics.tray=& $Tray } catch { }
    return $diagnostics
}
function Assert-AcceptanceOwner([Diagnostics.Process]$Process,[string]$Executable,[long]$Ticks) {
    $Process.Refresh()
    if ($Process.HasExited -or $Process.StartTime.ToUniversalTime().Ticks -ne $Ticks -or
        $Process.MainModule.FileName -ine $Executable) { throw 'Owned process identity changed.' }
}
function Stop-AcceptanceOwner([Diagnostics.Process]$Process,[string]$Executable,[long]$Ticks,[switch]$RequestWindowClose) {
    Invoke-AcceptanceRootCleanup -HasExited { $Process.Refresh();$Process.HasExited } `
        -CheckIdentity { Assert-AcceptanceOwner $Process $Executable $Ticks } `
        -CloseWindow { $Process.CloseMainWindow() } -WaitForExit { param($Milliseconds) $Process.WaitForExit($Milliseconds) } `
        -Terminate { $Process.Kill() } -RequestWindowClose:$RequestWindowClose
}
function Save-AcceptanceWindow([Diagnostics.Process]$Process,[string]$Executable,[long]$Ticks,[string]$Path) {
    Assert-AcceptanceOwner $Process $Executable $Ticks
    if ($Process.MainWindowHandle -eq [IntPtr]::Zero) { throw 'Owned window unavailable.' }
    if (-not ('OriginalAcceptanceCapture.Native' -as [type])) {
        Add-Type -AssemblyName System.Drawing
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace OriginalAcceptanceCapture {
 public static class Native {
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left,Top,Right,Bottom; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out RECT r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h,IntPtr dc,uint flags);
 }
}
'@
    }
    $window=$Process.MainWindowHandle;$rect=New-Object OriginalAcceptanceCapture.Native+RECT
    if (-not [OriginalAcceptanceCapture.Native]::GetWindowRect($window,[ref]$rect)) { throw 'Window bounds unavailable.' }
    $width=$rect.Right-$rect.Left;$height=$rect.Bottom-$rect.Top
    if ($width -le 0 -or $height -le 0 -or $width -gt 2400 -or $height -gt 2000) { throw 'Owned window bounds invalid.' }
    $bitmap=[Drawing.Bitmap]::new($width,$height);$graphics=[Drawing.Graphics]::FromImage($bitmap);$dc=$graphics.GetHdc()
    try { if (-not [OriginalAcceptanceCapture.Native]::PrintWindow($window,$dc,2)) { throw 'Owned capture failed.' } }
    finally { $graphics.ReleaseHdc($dc);$graphics.Dispose() }
    try {
        Assert-AcceptanceOwner $Process $Executable $Ticks
        if ($Process.MainWindowHandle -ne $window) { throw 'Owned window changed.' }
        $bitmap.Save($Path,[Drawing.Imaging.ImageFormat]::Png)
    } finally { $bitmap.Dispose() }
}
$report=[ordered]@{schema=1;scope='original-magpie-connect-desktop-restore';sourceCommit=$SourceCommit;
    status='blocked';diagnosticOnly=[bool]$stageDiagnosticsOnly;stage='context';failureStage='none';preparation=$null;package=$null;sequence=$null;images=$null;diagnosticCapture=$null;trayDiscovery=$null;connectPreconditions=$null;connectStages=$null;backendHandshakeObservation=(New-OriginalBackendHandshakeReport);backendHandshakeObservationFinal=$null;
    originalWindowCaptured=$false;desktopWindowCaptured=$false;adapterImageObserved=$false;
    selectionRestored=$false;journalRemoved=$false;ownedRootExitsConfirmed=$false;descendantExitVerified=$false;gracefulShutdownVerified=$false;cleanupResourcesDisposed=$false;cleanup=(New-AcceptanceCleanup);cleanupScope='retained-process-roots-only';preserveRecovery=$true;
    nativeHomeInitiallyEmpty=$false;launchIdentityMatchesSession=$false;isolatedNativeHomeVerified=$false;realAccountsUsed=$false;providerTurnAttempted=$false;
    desktopInteractionAttempted=$false;backendHandshakeVerified=$false;chatVerified=$false;
    restoredDesktopStartVerified=$false;zeroEgressProven=$false}
$evidenceReady=$false
function Save-AcceptanceReport {
    if ($evidenceReady) {
        [IO.File]::WriteAllText((Join-Path $EvidenceDirectory 'original-desktop-acceptance.json'),($report | ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false))
    }
}
$context=$null;$process=$null;$started=$false;$startAttempted=$false;$sequenceEntered=$false;$recoveryReportSaved=$false;$safeCleanup=$false;$baseline=$null;$journal='';$ticks=[long]0;$exe=''
try {
    $buildMode=Assert-AcceptanceContext
    $evidenceReady=$true;Save-AcceptanceReport
    $report.stage='package-provenance'
    $build=[IO.File]::ReadAllText((Join-Path $PackageDirectory 'BUILD.json')) | ConvertFrom-Json
    Assert-AcceptanceBuildTags $build $buildMode
    if ($build.sourceCommit -cne $SourceCommit -or $build.originalGUI -ne $true -or
        $build.portable -ne $true) { throw 'Original package provenance mismatch.' }
    foreach ($pair in @(@('Magpie-Native-Codex-Experimental.exe','guiSHA256'),@('magpie-codex-bridge.exe','adapterSHA256'))) {
        $path=Join-Path $PackageDirectory $pair[0];Assert-OriginalRegularPath $path
        if ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne $build.($pair[1])) {
            throw 'Original package bytes mismatch.'
        }
    }
    $report.stage='fixture-directories'
    $root=Join-Path $env:RUNNER_TEMP ('original-desktop-acceptance-'+[guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $root -ErrorAction Stop | Out-Null
    $run=Join-Path $root 'app';Copy-Item -LiteralPath $PackageDirectory -Destination $run -Recurse
    $homeRoot=Join-Path $root 'home';New-Item -ItemType Directory -Path $homeRoot | Out-Null
    $nativeHome=Join-Path $homeRoot '.codex';New-Item -ItemType Directory -Path $nativeHome | Out-Null
    $attempts=Join-Path $root 'attempts';New-Item -ItemType Directory -Path $attempts | Out-Null
    $snapshot=Join-Path $root 'before-selection.json'
    $report.stage='selection-snapshot'
    New-OriginalConnectSnapshot $snapshot
    $baseline=Read-OriginalSelection
    if ($baseline.cli.present -or $baseline.profile.present) { throw 'Existing user launch selection blocks acceptance.' }
    $journal=Join-Path $run 'data\native-codex-connection\selection.json'
    $launcher=Join-Path $run 'magpie-codex-bridge.exe';$exe=Join-Path $run 'Magpie-Native-Codex-Experimental.exe'
    $report.stage='fixture-configuration'
    [IO.File]::WriteAllText((Join-Path $run 'data\settings.json'),'{"noAccountDiscovery":true,"lang":"en","theme":"light","guideHidden":true}',[Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $run 'data\providers.json'),((Get-AcceptanceSupplierFixture) | ConvertTo-Json -Depth 5 -Compress),[Text.UTF8Encoding]::new($false))
    if (@(Get-ChildItem -LiteralPath $nativeHome -Force).Count -ne 0 -or (Test-AcceptanceJournal $journal)) { throw 'Fresh isolated native state required.' }
    $report.nativeHomeInitiallyEmpty=$true
    $report.stage='official-package-preparation'
    $report.preparation=[ordered]@{}
    $context=New-OriginalDesktopPackage -Work (Join-Path $root 'official') -BridgeExecutable $launcher -PreparationEvidence $report.preparation
    $report.package=$context.Evidence
    $report.stage='original-window'
    $start=New-OriginalDesktopStartInfo -Executable $exe -OwnedRun $run -OwnedHome $homeRoot
    $launchIdentityHome=[string]$start.EnvironmentVariables['USERPROFILE']
    $report.launchIdentityMatchesSession=$true
    $report.isolatedNativeHomeVerified=$true
    $process=[Diagnostics.Process]::new();$process.StartInfo=$start;$startAttempted=$true;$started=$process.Start()
    if (-not $started) { throw 'Original GUI did not start.' }
    $null=$process.Handle;$ticks=$process.StartTime.ToUniversalTime().Ticks
    $watch=[Diagnostics.Stopwatch]::StartNew()
    do {
        Assert-AcceptanceOwner $process $exe $ticks
        if ($process.MainWindowHandle -ne [IntPtr]::Zero -and $process.MainWindowTitle -ceq 'magpie') { break }
        if ($watch.ElapsedMilliseconds -ge 45000) { throw 'Original GUI window deadline.' }
        Start-Sleep -Milliseconds 100
    } while ($true)
    $report.stage='original-ui-sequence';Save-AcceptanceReport
    $sequenceEntered=$true
    $report.sequence=Invoke-OriginalDesktopSequence -Connect {
        $action=Invoke-OriginalConnectAction -Mode Connect -OwnedProcess $process -ExpectedExecutable $exe -ExpectedLauncher $launcher -JournalPath $journal -SnapshotPath $snapshot -AttemptDirectory $attempts
        [IO.File]::WriteAllText((Join-Path $EvidenceDirectory 'original-connect-action.json'),($action | ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
        return $action
    } -Activate {
        $null=Invoke-OriginalDesktopPackageActivation $context
        # Read-only bounded image observation before restoration. No Desktop UI
        # action, setup or login. Missing events never prove no child existed.
        # Diagnostic builds use the full existing observation budget so an
        # early image event does not abbreviate handshake evidence collection.
        $observation=[Diagnostics.Stopwatch]::StartNew()
        do {
            $report.images=Get-OriginalDesktopImageEvidence $context
            if (-not $buildMode.handshakeDiagnostic -and $report.images.status -ceq 'snapshot' -and ($report.images.adapterDirectChildren -gt 0 -or $report.images.nativeDirectChildren -gt 0)) { break }
            if ($observation.ElapsedMilliseconds -ge 15000) { break }
            Start-Sleep -Milliseconds 100
        } while ($true)
        return $context.Evidence
    } -HasJournal { Test-AcceptanceJournal $journal } -Restore {
        $action=Invoke-OriginalConnectAction -Mode Restore -OwnedProcess $process -ExpectedExecutable $exe -ExpectedLauncher $launcher -JournalPath $journal -SnapshotPath $snapshot -AttemptDirectory $attempts
        [IO.File]::WriteAllText((Join-Path $EvidenceDirectory 'original-restore-action.json'),($action | ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
        return $action
    } -AtBaseline { Test-OriginalSelectionEqual (Read-OriginalSelection) $baseline }
    $safeCleanup=$report.sequence.cleanupAllowed
    $report.selectionRestored=$report.sequence.selectionRestored;$report.journalRemoved=$report.sequence.journalRemoved
    $report.preserveRecovery=-not $safeCleanup
    if ($null -ne $report.images -and $report.images.status -ceq 'snapshot') { $report.adapterImageObserved=$report.images.adapterDirectChildren -gt 0 }
    Save-AcceptanceReport
    $recoveryReportSaved=$true
    # Fixed-stage log content is observed only after recovery decisions and their
    # report are saved. Never use this result to authorize an action or cleanup.
    $report.connectStages=New-OriginalConnectStageReport
    try {
        $report.connectStages=Get-OriginalConnectStages -OwnedRun $run -OriginalStartInfo $start -ProcessStartTicks $ticks -Sequence $report.sequence -RecoveryChecksCompleted -SequenceReportSaved
    } catch { $report.connectStages.reason='read-error' }
    Save-AcceptanceReport
    # Compile-only handshake traces are read after saved recovery. Their fixed
    # milestones cannot authorize another action or promote acceptance claims.
    if ($buildMode.handshakeDiagnostic) {
        try {
            $report.backendHandshakeObservation=Get-OriginalBackendHandshake -OwnedRun $run -OriginalStartInfo $start -ProcessStartTicks $ticks -Sequence $report.sequence -RecoveryChecksCompleted -SequenceReportSaved
        } catch {
            $report.backendHandshakeObservation=New-OriginalBackendHandshakeReport
            $report.backendHandshakeObservation.reason='read-error'
        }
    }
    Save-AcceptanceReport
    # Diagnostics follow the completed recovery checks and saved report. Neither
    # result changes uncertainty, cleanup authority, or the original failure.
    $diagnostics=Get-AcceptanceDiagnostics -CaptureFailure (-not $stageDiagnosticsOnly -and $report.sequence.status -cne 'verified') -Capture {
        Invoke-OriginalDesktopWindowCapture -OwnedProcess $process -ExpectedExecutable $exe -PrivateDirectory $root -EvidenceDirectory $EvidenceDirectory -SequenceReportSaved
    } -Tray {
        Invoke-OriginalQuitDiscovery -OwnedProcess $process -ExpectedExecutable $exe
    } -Preconditions {
        Get-OriginalConnectPreconditions -PackageContext $context -OriginalStartInfo $start -OwnedRun $run -OwnedHome $homeRoot -LaunchIdentityHome $launchIdentityHome -Sequence $report.sequence -RecoveryChecksCompleted -SequenceReportSaved
    }
    $report.diagnosticCapture=$diagnostics.capture;$report.trayDiscovery=$diagnostics.tray;$report.connectPreconditions=$diagnostics.preconditions
    Save-AcceptanceReport
    if ($report.sequence.status -cne 'verified') { throw 'Original transaction not verified.' }
    if (-not $stageDiagnosticsOnly) {
        # Capture only after recovery. A slow/failing screenshot must never postpone
        # the original Restore action or stand in for its registry/journal readback.
        $report.stage='owned-window-evidence'
        Save-AcceptanceWindow $process $exe $ticks (Join-Path $EvidenceDirectory 'original-magpie-restored.png')
        $report.originalWindowCaptured=$true
        if ($context.Evidence.activated) {
            Save-AcceptanceWindow $context.ActivationProcess $context.DesktopExecutable $context.ActivationCreatedTicks (Join-Path $EvidenceDirectory 'official-desktop-after-restore.png')
            $report.desktopWindowCaptured=$true
        }
        if (-not $report.adapterImageObserved) { throw 'Desktop activation did not establish adapter image observation.' }
        $report.status='verified';$report.stage='complete'
    }
} catch {
    # Report fixed stages only, never arbitrary UI text, paths, child stderr,
    # profile data, credentials or full process command lines.
    $report.failureStage=$report.stage
} finally {
    if (-not $sequenceEntered -and $null -eq $report.sequence -and $null -ne $baseline -and $journal) {
        try {
            $report.journalRemoved=-not (Test-AcceptanceJournal $journal)
            $report.selectionRestored=Test-OriginalSelectionEqual (Read-OriginalSelection) $baseline
            $safeCleanup=$report.journalRemoved -and $report.selectionRestored
            $report.preserveRecovery=-not $safeCleanup
        } catch { $safeCleanup=$false;$report.preserveRecovery=$true }
    }
    if ($startAttempted) { $report.cleanup.originalRoot.status='start-unconfirmed' }
    if ($started) { $report.cleanup.originalRoot.status='not-attempted' }
    $desktopAttempted=$null -ne $context -and $context.ActivationAttempted
    if ($desktopAttempted) { $report.cleanup.desktopRoot.status='ownership-unverified' }
    if ($null -ne $context -and $context.Evidence.activated) { $report.cleanup.desktopRoot.status='not-attempted' }
    # If recovery is unknown, leave original GUI and its journal untouched for
    # the disposable runner's remaining lifetime. No substitute rollback.
    if ($safeCleanup) {
        if ($null -ne $context -and $context.Evidence.activated) {
            $report.cleanup.desktopRoot=Stop-AcceptanceOwner $context.ActivationProcess $context.DesktopExecutable $context.ActivationCreatedTicks -RequestWindowClose
        }
        if ($started) {
            # Closing Magpie's main window only hides it. No original Quit
            # control has been verified; do not disguise that as graceful exit.
            $report.cleanup.originalRoot=Stop-AcceptanceOwner $process $exe $ticks
        }
    }
    $disposeOriginal=$null;$disposePackage=$null
    if ($null -ne $process) { $disposeOriginal={ $process.Dispose() } }
    if ($null -ne $context) { $disposePackage={ Close-OriginalDesktopPackage $context } }
    Complete-AcceptanceCleanup $report.cleanup $safeCleanup $startAttempted $desktopAttempted $disposeOriginal $disposePackage
    $report.ownedRootExitsConfirmed=$report.cleanup.rootExitsConfirmed
    $report.cleanupResourcesDisposed=$report.cleanup.resourcesDisposed
    $report.gracefulShutdownVerified=$report.cleanup.gracefulShutdownVerified
    $report.descendantExitVerified=$report.cleanup.descendantExitVerified
    if ($safeCleanup -and $sequenceEntered -and $recoveryReportSaved -and $buildMode.handshakeDiagnostic) {
        # This later snapshot preserves the preliminary one. Even confirmed
        # roots do not prove all writers/descendants stopped or logs complete.
        $report.backendHandshakeObservationFinal=[ordered]@{phase='after-cleanup-attempts';
            rootExitsConfirmed=$report.ownedRootExitsConfirmed;descendantExitVerified=$false;
            traceCompletenessVerified=$false;observation=(New-OriginalBackendHandshakeReport)}
        try {
            $report.backendHandshakeObservationFinal.observation=Get-OriginalBackendHandshake -OwnedRun $run -OriginalStartInfo $start -ProcessStartTicks $ticks -Sequence $report.sequence -RecoveryChecksCompleted -SequenceReportSaved
        } catch { $report.backendHandshakeObservationFinal.observation.reason='read-error' }
    }
    if ($report.cleanup.status -cne 'verified-retained-roots') {
        # Preserve the first operation's failure alongside cleanup evidence.
        if ($report.failureStage -ceq 'none') { $report.failureStage='owned-cleanup';$report.stage='owned-cleanup' }
        $report.status='blocked'
    }
    if ($stageDiagnosticsOnly) { $report.status='diagnostic-only' }
    Save-AcceptanceReport
}
if ($report.status -cne 'verified') { throw 'Original Desktop acceptance blocked; see fixed stage report.' }
