# One disposable hosted-Windows original-UI transaction. No live-account work.
[CmdletBinding()]
param([string]$PackageDirectory,[string]$EvidenceDirectory,[string]$SourceCommit)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-connect-action.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-package.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-sequence.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-quit-discovery.ps1')
. (Join-Path $PSScriptRoot 'original-desktop-window-capture.ps1')
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
    foreach ($path in @($PackageDirectory,$EvidenceDirectory)) { Assert-OriginalRegularPath $path }
    if ($PackageDirectory -cne (Join-Path $env:RUNNER_TEMP 'native-magpie-package') -or
        $EvidenceDirectory -cne (Join-Path $env:RUNNER_TEMP 'native-magpie-evidence') -or
        -not [IO.Directory]::Exists($PackageDirectory) -or -not [IO.Directory]::Exists($EvidenceDirectory)) {
        throw 'Owned package and evidence directories required.'
    }
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
function Get-AcceptanceDiagnostics([bool]$CaptureFailure,[scriptblock]$Capture,[scriptblock]$Tray) {
    $diagnostics=[ordered]@{capture=$null;tray=[ordered]@{status='unavailable'}}
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
function Stop-AcceptanceOwner([Diagnostics.Process]$Process,[string]$Executable,[long]$Ticks) {
    $Process.Refresh()
    if ($Process.HasExited) { return }
    Assert-AcceptanceOwner $Process $Executable $Ticks
    [void]$Process.CloseMainWindow()
    if (-not $Process.WaitForExit(5000)) {
        Assert-AcceptanceOwner $Process $Executable $Ticks
        $Process.Kill()
        if (-not $Process.WaitForExit(5000)) { throw 'Owned process exit unconfirmed.' }
        # A forced root exit does not verify graceful host/descendant cleanup.
        throw 'Forced root termination does not establish clean shutdown.'
    }
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
    status='blocked';stage='context';preparation=$null;package=$null;sequence=$null;images=$null;diagnosticCapture=$null;trayDiscovery=$null;
    originalWindowCaptured=$false;desktopWindowCaptured=$false;adapterImageObserved=$false;
    selectionRestored=$false;journalRemoved=$false;ownedRootExitsConfirmed=$false;descendantExitVerified=$false;cleanupScope='retained-process-roots-only';preserveRecovery=$true;
    nativeHomeInitiallyEmpty=$false;realAccountsUsed=$false;providerTurnAttempted=$false;
    desktopInteractionAttempted=$false;backendHandshakeVerified=$false;chatVerified=$false;
    restoredDesktopStartVerified=$false;zeroEgressProven=$false}
$evidenceReady=$false
function Save-AcceptanceReport {
    if ($evidenceReady) {
        [IO.File]::WriteAllText((Join-Path $EvidenceDirectory 'original-desktop-acceptance.json'),($report | ConvertTo-Json -Depth 12),[Text.UTF8Encoding]::new($false))
    }
}
$context=$null;$process=$null;$started=$false;$sequenceEntered=$false;$safeCleanup=$false;$baseline=$null;$journal='';$ticks=[long]0;$exe=''
try {
    Assert-AcceptanceContext
    $evidenceReady=$true;Save-AcceptanceReport
    $report.stage='package-provenance'
    $build=[IO.File]::ReadAllText((Join-Path $PackageDirectory 'BUILD.json')) | ConvertFrom-Json
    if ($build.sourceCommit -cne $SourceCommit -or $build.originalGUI -ne $true -or
        $build.buildTags -cne 'production,nativecodex' -or $build.portable -ne $true) { throw 'Original package provenance mismatch.' }
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
    $parent=[Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
    foreach ($name in @('CODEX_WINDOWS_REGISTERED_CORE','CODEX_SQLITE_HOME','CODEX_CLI_PATH','MAGPIE_APP_SERVER_BRIDGE_CONFIG')) {
        if ($parent.Contains($name)) { throw 'Unexpected native override.' }
    }
    $start=[Diagnostics.ProcessStartInfo]::new();$start.FileName=$exe;$start.Arguments='gui agents';$start.WorkingDirectory=$run;$start.UseShellExecute=$false
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','ProgramFiles','ProgramFiles(x86)','CommonProgramFiles','ProgramData')) {
        if ($parent.Contains($name)) { $start.EnvironmentVariables[$name]=[string]$parent[$name] }
    }
    $start.EnvironmentVariables['PATH']=Join-Path $env:SystemRoot 'System32'
    foreach ($name in @('HOME','USERPROFILE','APPDATA','LOCALAPPDATA','TEMP','TMP','XDG_CONFIG_HOME','XDG_CACHE_HOME')) { $start.EnvironmentVariables[$name]=$homeRoot }
    $start.EnvironmentVariables['CODEX_HOME']=$nativeHome
    foreach ($name in @('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY','http_proxy','https_proxy','all_proxy','no_proxy','CODEX_CA_CERTIFICATE','SSL_CERT_FILE','SSL_CERT_DIR','CURL_CA_BUNDLE','REQUESTS_CA_BUNDLE','NODE_EXTRA_CA_CERTS')) {
        if ($parent.Contains($name)) { $start.EnvironmentVariables[$name]=[string]$parent[$name] }
    }
    $process=[Diagnostics.Process]::new();$process.StartInfo=$start;$started=$process.Start()
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
        $observation=[Diagnostics.Stopwatch]::StartNew()
        do {
            $report.images=Get-OriginalDesktopImageEvidence $context
            if ($report.images.status -ceq 'snapshot' -and ($report.images.adapterDirectChildren -gt 0 -or $report.images.nativeDirectChildren -gt 0)) { break }
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
    # Diagnostics follow the completed recovery checks and saved report. Neither
    # result changes uncertainty, cleanup authority, or the original failure.
    $diagnostics=Get-AcceptanceDiagnostics -CaptureFailure ($report.sequence.status -cne 'verified') -Capture {
        Invoke-OriginalDesktopWindowCapture -OwnedProcess $process -ExpectedExecutable $exe -PrivateDirectory $root -EvidenceDirectory $EvidenceDirectory -SequenceReportSaved
    } -Tray {
        Invoke-OriginalQuitDiscovery -OwnedProcess $process -ExpectedExecutable $exe
    }
    $report.diagnosticCapture=$diagnostics.capture;$report.trayDiscovery=$diagnostics.tray
    Save-AcceptanceReport
    if ($report.sequence.status -cne 'verified') { throw 'Original transaction not verified.' }
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
} catch {
    # Report fixed stages only, never arbitrary UI text, paths, child stderr,
    # profile data, credentials or full process command lines.
} finally {
    if (-not $sequenceEntered -and $null -eq $report.sequence -and $null -ne $baseline -and $journal) {
        try {
            $report.journalRemoved=-not (Test-AcceptanceJournal $journal)
            $report.selectionRestored=Test-OriginalSelectionEqual (Read-OriginalSelection) $baseline
            $safeCleanup=$report.journalRemoved -and $report.selectionRestored
            $report.preserveRecovery=-not $safeCleanup
        } catch { $safeCleanup=$false;$report.preserveRecovery=$true }
    }
    $cleanupError=$false
    # If recovery is unknown, leave original GUI and its journal untouched for
    # the disposable runner's remaining lifetime. No substitute rollback.
    if ($safeCleanup) {
        if ($null -ne $context -and $context.Evidence.activated) {
            try { Stop-AcceptanceOwner $context.ActivationProcess $context.DesktopExecutable $context.ActivationCreatedTicks } catch { $cleanupError=$true }
        }
        if ($started) { try { Stop-AcceptanceOwner $process $exe $ticks } catch { $cleanupError=$true } }
        $report.ownedRootExitsConfirmed=-not $cleanupError
    }
    if ($null -ne $process) { $process.Dispose() }
    if ($null -ne $context) { try { Close-OriginalDesktopPackage $context } catch { $cleanupError=$true } }
    if ($cleanupError) { $report.status='blocked';$report.stage='owned-cleanup' }
    Save-AcceptanceReport
}
if ($report.status -cne 'verified') { throw 'Original Desktop acceptance blocked; see fixed stage report.' }
