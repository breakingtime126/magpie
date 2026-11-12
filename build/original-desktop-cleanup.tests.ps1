# Offline behavior only. No real process, Windows API, UI, registry or network.
[CmdletBinding()]
param([string]$Implementation='', [string]$CallerPath='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if (-not $Implementation) { $Implementation=Join-Path $PSScriptRoot 'original-desktop-cleanup.ps1' }
if (-not $CallerPath) { $CallerPath=Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1' }
. $Implementation
function Need([bool]$Condition,[string]$Message) { if (-not $Condition) { throw ('FAIL: '+$Message) } }
function Parse-Cleanup([string]$Path) {
    $tokens=$null;$errors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile($Path,[ref]$tokens,[ref]$errors)
    Need ($errors.Count -eq 0) 'cleanup parses'
    return $ast
}
$core=Parse-Cleanup $Implementation
$caller=Parse-Cleanup $CallerPath
function Run-Root([string]$Case,[bool]$RequestClose=$true) {
    $state=@{calls=[Collections.Generic.List[string]]::new();checks=0;waits=0}
    $result=Invoke-AcceptanceRootCleanup -HasExited {
        $state.calls.Add('read')
        if ($Case -ceq 'read-throws') { throw 'private-read-error' }
        return $Case -ceq 'already-exited'
    } -CheckIdentity {
        $state.calls.Add('identity');$state.checks++
        if ($Case -ceq 'identity-mismatch' -or ($Case -ceq 'identity-drift' -and $state.checks -eq 2)) { throw 'private-identity-error' }
    } -CloseWindow {
        $state.calls.Add('close')
        if ($Case -ceq 'close-throws') { throw 'private-close-error' }
        return $Case -cne 'close-refused'
    } -WaitForExit {
        param($Milliseconds)
        Need ($Milliseconds -eq 5000) 'root wait remains bounded'
        $state.waits++;$state.calls.Add('wait')
        if ($Case -ceq 'first-wait-throws' -or ($Case -ceq 'second-wait-throws' -and $state.waits -eq 2)) { throw 'private-wait-error' }
        if ($Case -ceq 'exit-without-force') { return $true }
        return $state.waits -eq 2 -and $Case -cne 'timeout'
    } -Terminate {
        $state.calls.Add('kill')
        if ($Case -ceq 'kill-throws') { throw 'private-kill-error' }
    } -RequestWindowClose:$RequestClose
    Need (($result | ConvertTo-Json -Compress) -cnotmatch 'private|error') 'root output has no arbitrary error text'
    Need (@($state.calls | Where-Object { $_ -ceq 'kill' }).Count -le 1) 'root termination is not retried'
    return @{result=$result;calls=($state.calls -join ',')}
}
$case=Run-Root 'already-exited'
Need ($case.result.rootExitConfirmed -and $case.result.exitKind -ceq 'already-exited' -and $case.calls -ceq 'read') 'already exited retained root needs no action'
$case=Run-Root 'exit-without-force'
Need ($case.result.rootExitConfirmed -and $case.result.exitKind -ceq 'without-force' -and -not $case.result.forceAttempted) 'window-close exit is a root fact only'
Need ($case.calls -ceq 'read,identity,close,wait') 'close then wait without termination'
$case=Run-Root 'forced-exit'
Need ($case.result.rootExitConfirmed -and $case.result.exitKind -ceq 'forced') 'forced exit keeps confirmed root fact'
Need ($case.calls -ceq 'read,identity,close,wait,identity,kill,wait') 'force rechecks owner then confirms exit'
$case=Run-Root 'close-refused'
Need (-not $case.result.windowCloseAccepted -and $case.result.rootExitConfirmed -and $case.result.exitKind -ceq 'forced') 'refused window close does not become graceful exit'
$case=Run-Root 'forced-exit' $false
Need (-not $case.result.windowCloseAttempted -and $case.calls -ceq 'read,identity,wait,identity,kill,wait') 'original tray root is not closed as if that were Quit'
foreach ($failure in @(
    @('read-throws','initial-exit-observation','read',$false),
    @('identity-mismatch','identity','read,identity',$false),
    @('identity-drift','identity-before-force','read,identity,close,wait,identity',$false),
    @('close-throws','window-close','read,identity,close',$false),
    @('first-wait-throws','wait-before-force','read,identity,close,wait',$false),
    @('kill-throws','force','read,identity,close,wait,identity,kill',$true),
    @('second-wait-throws','wait-after-force','read,identity,close,wait,identity,kill,wait',$true),
    @('timeout','wait-after-force','read,identity,close,wait,identity,kill,wait',$true)
)) {
    $case=Run-Root $failure[0]
    Need (-not $case.result.rootExitConfirmed -and $case.result.status -ceq 'unconfirmed' -and
        $case.result.failureStage -ceq $failure[1] -and $case.calls -ceq $failure[2] -and
        $case.result.forceAttempted -eq $failure[3]) ('unknown root remains unknown: '+$failure[0])
}
function Run-Aggregate($Original,$Desktop,[bool]$Recovery=$true,[bool]$OriginalAttempted=$true,[bool]$DesktopAttempted=$true,[string]$DisposalFailure='') {
    $cleanup=New-AcceptanceCleanup;$cleanup.originalRoot=$Original;$cleanup.desktopRoot=$Desktop
    $state=@{calls=[Collections.Generic.List[string]]::new()}
    Complete-AcceptanceCleanup $cleanup $Recovery $OriginalAttempted $DesktopAttempted {
        $state.calls.Add('original');if ($DisposalFailure -cin @('original','both')) { throw 'private-original-disposal' }
    } {
        $state.calls.Add('package');if ($DisposalFailure -cin @('package','both')) { throw 'private-package-disposal' }
    }
    Need (($state.calls -join ',') -ceq 'original,package') 'each disposal runs independently'
    Need (-not $cleanup.gracefulShutdownVerified -and -not $cleanup.descendantExitVerified -and $cleanup.status -cne 'verified') 'root and disposal never prove graceful or descendants'
    Need (($cleanup | ConvertTo-Json -Depth 5 -Compress) -cnotmatch 'private') 'cleanup output has fixed vocabulary'
    return $cleanup
}
$forced=(Run-Root 'forced-exit').result
$natural=(Run-Root 'exit-without-force').result
$already=(Run-Root 'already-exited').result
$unknown=(Run-Root 'timeout').result
foreach ($first in @($forced,$natural,$already)) {
    $r=Run-Aggregate $first $forced
    Need ($r.rootExitsConfirmed -and $r.confirmedRoots -eq 2 -and $r.resourcesDisposed) 'two root exits recorded independently of shutdown acceptance'
}
foreach ($failure in @('original','package','both')) {
    $r=Run-Aggregate $forced $forced $true $true $true $failure
    Need ($r.rootExitsConfirmed -and -not $r.resourcesDisposed) ('disposal cannot erase known root exits: '+$failure)
}
$r=Run-Aggregate $forced $unknown
Need (-not $r.rootExitsConfirmed -and $r.confirmedRoots -eq 1 -and $r.resourcesDisposed) 'partial root success is retained'
$r=Run-Aggregate $unknown $forced
Need (-not $r.rootExitsConfirmed -and $r.confirmedRoots -eq 1) 'other partial root success is retained'
$r=Run-Aggregate (New-AcceptanceRootCleanup) (New-AcceptanceRootCleanup) $true $false $false
Need (-not $r.rootExitsConfirmed -and $r.requiredRoots -eq 0 -and $r.originalQuit -ceq 'not-started') 'no roots is not observed exit success'
$r=Run-Aggregate $forced (New-AcceptanceRootCleanup) $true $true $true
Need (-not $r.rootExitsConfirmed -and $r.requiredRoots -eq 2) 'uncertain activation cannot disappear from required roots'
foreach ($mapping in @(
    @($already,$true,'not-requested-already-exited'),
    @($forced,$true,'blocked-no-verified-original-control'),
    @((Run-Root 'identity-mismatch').result,$true,'blocked-owner-unverified'),
    @((Run-Root 'identity-drift').result,$true,'blocked-owner-unverified'),
    @((New-AcceptanceRootCleanup),$false,'blocked-recovery')
)) {
    $r=Run-Aggregate $mapping[0] $forced $mapping[1]
    Need ($r.originalQuit -ceq $mapping[2]) ('original Quit classification '+$mapping[2])
}
# Exercise the actual coordinator finally block using inert owned-object doubles.
# Exceptions and preexisting failures must not skip the other owner's cleanup.
$tryStatements=@($caller.EndBlock.Statements | Where-Object { $_ -is [Management.Automation.Language.TryStatementAst] })
Need ($tryStatements.Count -eq 1) 'one acceptance transaction'
$finallyText=$tryStatements[0].Finally.Extent.Text
$finallyBody=[scriptblock]::Create($finallyText.Substring(1,$finallyText.Length-2))
function Stop-AcceptanceOwner($Process,$Executable,$Ticks,[switch]$RequestWindowClose) {
    $script:cleanupCalls.Add($Process.kind)
    if ($Process.kind -ceq 'original') {
        Need (-not $RequestWindowClose) 'coordinator never sends original window close as Quit'
        return (Run-Root $script:originalCase $false).result
    }
    return (Run-Root $script:desktopCase).result
}
function Close-OriginalDesktopPackage($Context) {
    $script:cleanupCalls.Add('package-disposal')
    if ($script:disposalCase -cin @('package','both')) { throw 'private-package-disposal' }
}
function Save-AcceptanceReport { $script:cleanupCalls.Add('save') }
function New-OriginalBackendHandshakeReport { return @{reason='missing';backendHandshakeVerified=$false} }
function Get-OriginalBackendHandshake {
    param($OwnedRun,$OriginalStartInfo,$ProcessStartTicks,$Sequence,[switch]$RecoveryChecksCompleted,[switch]$SequenceReportSaved)
    Need ($RecoveryChecksCompleted -and $SequenceReportSaved) 'final handshake read requires saved recovery'
    $script:cleanupCalls.Add('handshake-final')
    if ($script:finalReadFails) { throw 'private-final-log-error' }
    return @{reason='observed';backendHandshakeVerified=$false}
}
function Run-Coordinator([bool]$Recovery,[string]$Original='forced-exit',[string]$Desktop='forced-exit',[string]$Disposal='', [string]$FailureStage='none',[bool]$Activated=$true,[bool]$Diagnostic=$false,[bool]$Handshake=$false,[bool]$RecoverySaved=$true,[bool]$FinalReadFailure=$false) {
    $script:originalCase=$Original;$script:desktopCase=$Desktop;$script:disposalCase=$Disposal;$script:finalReadFails=$FinalReadFailure
    $script:cleanupCalls=[Collections.Generic.List[string]]::new()
    $process=[pscustomobject]@{kind='original'}
    $process | Add-Member -MemberType ScriptMethod -Name Dispose -Value {
        $script:cleanupCalls.Add('original-disposal')
        if ($script:disposalCase -cin @('original','both')) { throw 'private-original-disposal' }
    }
    $context=@{ActivationAttempted=$true;Evidence=@{activated=$Activated};ActivationProcess=[pscustomobject]@{kind='desktop'};DesktopExecutable='owned-desktop';ActivationCreatedTicks=1}
    $report=@{status='verified';stage='complete';failureStage=$FailureStage;cleanup=(New-AcceptanceCleanup);ownedRootExitsConfirmed=$false;descendantExitVerified=$false;sequence=@{status='verified'};backendHandshakeObservation=@{reason='preliminary'};backendHandshakeObservationFinal=$null}
    if ($FailureStage -cne 'none') { $report.stage=$FailureStage;$report.status='blocked' }
    $buildMode=@{handshakeDiagnostic=$Handshake};$recoveryReportSaved=$RecoverySaved;$run='owned-run';$start=[Diagnostics.ProcessStartInfo]::new()
    $sequenceEntered=$true;$safeCleanup=$Recovery;$startAttempted=$true;$started=$true;$ticks=1;$exe='owned-original';$stageDiagnosticsOnly=$Diagnostic
    try { & $finallyBody } catch { throw ('FAIL: coordinator cleanup escaped instead of preserving evidence: '+$_.Exception.GetType().Name) }
    return @{report=$report;calls=($script:cleanupCalls -join ',')}
}
$r=Run-Coordinator $true
Need ($r.report.ownedRootExitsConfirmed -and $r.report.status -ceq 'verified' -and $r.report.cleanup.status -ceq 'verified-retained-roots') 'coordinator verifies retained-root cleanup without graceful or tree-drain claims'
Need ($r.calls -ceq 'desktop,original,original-disposal,package-disposal,save') 'coordinator cleans both owned roots then disposes and saves'
foreach ($disposal in @('original','package','both')) {
    $r=Run-Coordinator $true 'forced-exit' 'forced-exit' $disposal
    Need ($r.report.ownedRootExitsConfirmed -and -not $r.report.cleanupResourcesDisposed -and $r.calls.EndsWith('package-disposal,save')) 'coordinator preserves root proof across disposal failure'
}
$r=Run-Coordinator $true 'timeout' 'forced-exit'
Need (-not $r.report.ownedRootExitsConfirmed -and $r.report.cleanup.confirmedRoots -eq 1) 'coordinator preserves one confirmed root'
$r=Run-Coordinator $false
Need ($r.calls -ceq 'original-disposal,package-disposal,save' -and $r.report.cleanup.originalQuit -ceq 'blocked-recovery') 'unknown recovery performs no root close or kill'
$r=Run-Coordinator $true 'forced-exit' 'forced-exit' '' 'owned-window-evidence'
Need ($r.report.failureStage -ceq 'owned-window-evidence' -and $r.report.stage -ceq 'owned-window-evidence' -and $r.report.cleanup.status -ceq 'verified-retained-roots' -and $r.report.status -ceq 'blocked') 'cleanup preserves the prior operation failure'
$r=Run-Coordinator $true 'forced-exit' 'forced-exit' '' 'none' $false
Need ($r.calls -notmatch '^desktop,' -and -not $r.report.ownedRootExitsConfirmed -and $r.report.cleanup.requiredRoots -eq 2) 'uncertain activation is not acted on or claimed absent'
$r=Run-Coordinator $true 'forced-exit' 'forced-exit' '' 'none' $true $true
Need ($r.report.status -ceq 'diagnostic-only' -and $r.report.cleanup.status -ceq 'verified-retained-roots') 'diagnostic status remains separate from retained-root cleanup'
foreach ($original in @('forced-exit','timeout')) {
    $r=Run-Coordinator -Recovery $true -Original $original -Handshake $true
    Need ($r.calls -ceq 'desktop,original,original-disposal,package-disposal,handshake-final,save') 'final handshake read follows both cleanup and disposal attempts'
    $final=$r.report.backendHandshakeObservationFinal
    Need ($final.rootExitsConfirmed -eq ($original -ceq 'forced-exit') -and -not $final.traceCompletenessVerified -and
        -not $final.descendantExitVerified -and $r.report.backendHandshakeObservation.reason -ceq 'preliminary') 'final handshake snapshot preserves preliminary and labels root uncertainty'
}
$r=Run-Coordinator -Recovery $false -Handshake $true
Need ($null -eq $r.report.backendHandshakeObservationFinal -and -not $r.calls.Contains('handshake-final')) 'unknown recovery skips final handshake read'
$r=Run-Coordinator -Recovery $true -Handshake $true -RecoverySaved $false
Need ($null -eq $r.report.backendHandshakeObservationFinal -and -not $r.calls.Contains('handshake-final')) 'unsaved recovery skips final handshake read'
$r=Run-Coordinator -Recovery $true -Handshake $true -FinalReadFailure $true -FailureStage 'owned-window-evidence'
Need ($r.report.backendHandshakeObservationFinal.observation.reason -ceq 'read-error' -and
    $r.report.failureStage -ceq 'owned-window-evidence' -and $r.report.ownedRootExitsConfirmed -and
    ($r.report | ConvertTo-Json -Depth 8 -Compress) -cnotmatch 'private') 'final observer read failure preserves prior failure and confirmed roots'
# Production adapters never reopen cleanup targets, enumerate or kill trees.
$owner=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Stop-AcceptanceOwner'},$false))
Need ($owner.Count -eq 1) 'unique retained owner cleanup adapter'
$adapter=$owner[0].Extent.Text
foreach ($forbidden in @('GetProcessById','Get-Process','Stop-Process','taskkill','Kill($true)','TerminateJob','Start-Process','Invoke-RestMethod','SendMessage','PostMessage')) {
    Need (-not $adapter.Contains($forbidden) -and -not $core.Extent.Text.Contains($forbidden)) ('no cleanup authority expansion '+$forbidden)
}
Need ($adapter.Contains('$Process.Kill()') -and $adapter.Contains('Assert-AcceptanceOwner $Process $Executable $Ticks')) 'termination uses same retained checked process'

$r=Run-Aggregate $forced $forced
Need ($r.status -ceq 'verified-retained-roots') 'complete retained roots and resources admit only scoped cleanup'
foreach ($c in @((Run-Aggregate $forced $unknown),(Run-Aggregate $forced $forced $true $true $true 'original'),(Run-Aggregate $forced $forced $false))) {
    Need ($c.status -cne 'verified-retained-roots') 'partial, disposal failure and unrecovered cleanup stay blocked'
}

Write-Output 'ORIGINAL_DESKTOP_CLEANUP_TESTS_PASSED'
