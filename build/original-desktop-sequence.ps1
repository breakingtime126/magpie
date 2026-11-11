# Pure sequencing policy. Runtime adapters supply UIA, read-only assertions and
# one normal activation. Never retries an uncertain action or writes registry.
function Invoke-OriginalDesktopSequence {
    param([scriptblock]$Connect,[scriptblock]$Activate,[scriptblock]$HasJournal,
        [scriptblock]$Restore,[scriptblock]$AtBaseline)
    $result=[ordered]@{schema=1;scope='original-magpie-desktop-sequence';status='blocked';
        stage='connect';failureStage='none';connect=$null;restore=$null;
        connectVerified=$false;activationAttempted=$false;desktopActivationVerified=$false;
        restoreAttempted=$false;originalRestoreVerified=$false;selectionRestored=$false;
        journalRemoved=$false;cleanupAllowed=$false;preserveRecovery=$true;
        backendHandshakeVerified=$false;restoredDesktopStartVerified=$false;chatVerified=$false}
    try {
        $result.connect=& $Connect
        if ($result.connect.status -cne 'verified' -or $result.connect.selectionVerified -ne $true) {
            throw 'Original Connect was not verified.'
        }
        $result.connectVerified=$true;$result.stage='activation';$result.activationAttempted=$true
        $activation=& $Activate
        if ($activation.activated -ne $true) { throw 'Normal activation was not verified.' }
        $result.desktopActivationVerified=$true
    } catch { $result.failureStage=$result.stage }
    finally {
        $result.stage='restore'
        try {
            # Absence is never itself proof of restoration. Both independent
            # registry readback and journal absence must succeed for cleanup.
            if (& $HasJournal) {
                $result.restoreAttempted=$true
                $result.restore=& $Restore
                if ($result.restore.status -cne 'verified' -or $result.restore.restorationVerified -ne $true) {
                    throw 'Original restoration was not verified.'
                }
                $result.originalRestoreVerified=$true
            }
            $result.journalRemoved=-not (& $HasJournal)
            $result.selectionRestored=[bool](& $AtBaseline)
            # A timed-out UIA call may have dispatched asynchronous Connect even
            # while both readbacks still look clean. Only actual original Restore
            # or positively zero dispatched actions establishes quiescence.
            $zeroActions=$null -ne $result.connect -and $result.connect.status -ceq 'blocked' -and
                $result.connect.actionsAttempted -eq 0
            $quiescent=$result.originalRestoreVerified -or $zeroActions
            $result.cleanupAllowed=$result.journalRemoved -and $result.selectionRestored -and $quiescent
            $result.preserveRecovery=-not $result.cleanupAllowed
            if (-not $result.cleanupAllowed) { throw 'Recovery readback unavailable.' }
        } catch { if ($result.failureStage -ceq 'none') { $result.failureStage='restore' } }
    }
    if ($result.connectVerified -and $result.desktopActivationVerified -and
        $result.originalRestoreVerified -and $result.cleanupAllowed -and $result.failureStage -ceq 'none') {
        $result.status='verified';$result.stage='complete'
    }
    return $result
}
