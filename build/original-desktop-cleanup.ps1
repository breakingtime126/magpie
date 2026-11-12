# Cleanup evidence for retained process roots, not a descendant-drain proof.
# Dot-sourcing this file performs no process or UI action.
function New-AcceptanceRootCleanup {
    return [ordered]@{status='not-started';attempted=$false;identityVerified=$false;
        windowCloseAttempted=$false;windowCloseAccepted=$false;forceAttempted=$false;
        rootExitConfirmed=$false;exitKind='unknown';failureStage='none'}
}
function Invoke-AcceptanceRootCleanup {
    param([scriptblock]$HasExited,[scriptblock]$CheckIdentity,[scriptblock]$CloseWindow,
        [scriptblock]$WaitForExit,[scriptblock]$Terminate,[switch]$RequestWindowClose)
    $r=New-AcceptanceRootCleanup
    $r.attempted=$true;$r.status='unconfirmed';$stage='initial-exit-observation'
    try {
        if (& $HasExited) {
            $r.rootExitConfirmed=$true;$r.exitKind='already-exited';$r.status='exit-confirmed'
            return $r
        }
        $stage='identity'; & $CheckIdentity; $r.identityVerified=$true
        if ($RequestWindowClose) {
            $stage='window-close';$r.windowCloseAttempted=$true
            $r.windowCloseAccepted=[bool](& $CloseWindow)
        }
        $stage='wait-before-force'
        if (& $WaitForExit 5000) {
            $r.rootExitConfirmed=$true;$r.exitKind='without-force';$r.status='exit-confirmed'
            return $r
        }
        # Recheck the same retained owner, never find a replacement by PID/name.
        $stage='identity-before-force'; & $CheckIdentity
        $stage='force';$r.forceAttempted=$true; & $Terminate
        $stage='wait-after-force'
        if (-not (& $WaitForExit 5000)) { $r.failureStage=$stage;return $r }
        $r.rootExitConfirmed=$true;$r.exitKind='forced';$r.status='exit-confirmed'
    } catch {
        # Keep the last confirmed facts. A throw never turns a failed read into
        # absence, authorizes a retry, or exports arbitrary process/error text.
        $r.failureStage=$stage
    }
    return $r
}
function New-AcceptanceCleanup {
    return [ordered]@{status='not-attempted';recoveryAllowsCleanup=$false;
        originalQuit='not-attempted';originalRoot=(New-AcceptanceRootCleanup);
        desktopRoot=(New-AcceptanceRootCleanup);requiredRoots=0;confirmedRoots=0;
        originalProcessDisposal='not-required';packageResourceDisposal='not-required';
        rootExitsConfirmed=$false;resourcesDisposed=$false;
        gracefulShutdownVerified=$false;descendantExitVerified=$false}
}
function Complete-AcceptanceCleanup {
    param([Collections.IDictionary]$Cleanup,[bool]$RecoveryAllowsCleanup,
        [bool]$OriginalAttempted,[bool]$DesktopAttempted,
        [scriptblock]$DisposeOriginal,[scriptblock]$DisposePackage)
    $Cleanup.recoveryAllowsCleanup=$RecoveryAllowsCleanup
    foreach ($entry in @(@($OriginalAttempted,$Cleanup.originalRoot),@($DesktopAttempted,$Cleanup.desktopRoot))) {
        if ($entry[0]) {
            $Cleanup.requiredRoots++
            if ($entry[1].rootExitConfirmed) { $Cleanup.confirmedRoots++ }
        }
    }
    $Cleanup.rootExitsConfirmed=$Cleanup.requiredRoots -gt 0 -and
        $Cleanup.requiredRoots -eq $Cleanup.confirmedRoots
    if (-not $OriginalAttempted) { $Cleanup.originalQuit='not-started' }
    elseif (-not $RecoveryAllowsCleanup) { $Cleanup.originalQuit='blocked-recovery' }
    elseif ($Cleanup.originalRoot.exitKind -ceq 'already-exited') { $Cleanup.originalQuit='not-requested-already-exited' }
    elseif (-not $Cleanup.originalRoot.identityVerified -or $Cleanup.originalRoot.failureStage -ceq 'identity-before-force') {
        $Cleanup.originalQuit='blocked-owner-unverified'
    } else { $Cleanup.originalQuit='blocked-no-verified-original-control' }
    # Disposing query handles/locks proves neither process exit nor graceful
    # host shutdown. Each disposal is attempted even if the other one fails.
    foreach ($entry in @(@('originalProcessDisposal',$DisposeOriginal),@('packageResourceDisposal',$DisposePackage))) {
        if ($null -ne $entry[1]) {
            $Cleanup[$entry[0]]='failed'
            try { & $entry[1];$Cleanup[$entry[0]]='disposed' } catch { }
        }
    }
    $Cleanup.resourcesDisposed=$Cleanup.originalProcessDisposal -cne 'failed' -and
        $Cleanup.packageResourceDisposal -cne 'failed'
    # No root result or disposal result supplies either missing proof. In
    # particular, CloseMainWindow is not the original tray app's Quit action.
    $Cleanup.gracefulShutdownVerified=$false
    $Cleanup.descendantExitVerified=$false
    $Cleanup.status=if ($RecoveryAllowsCleanup) { 'blocked' } else { 'recovery-preserved' }
}
