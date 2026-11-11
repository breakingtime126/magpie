[CmdletBinding()]
param([switch]$BreakRestoration)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-desktop-sequence.ps1')
function Assert([bool]$Value,[string]$Message) { if (-not $Value) { throw ('FAIL: '+$Message) } }
function Case([string]$Name,[scriptblock]$Body) { & $Body; Write-Output ('PASS '+$Name) }
function New-State { return @{calls=[Collections.Generic.List[string]]::new();journal=$false;baseline=$true} }
function Run($State,[string]$Failure='') {
    Invoke-OriginalDesktopSequence -Connect {
        $State.calls.Add('connect');$State.journal=$true;$State.baseline=$false
        if ($Failure -ceq 'connect-throw') { throw 'lost output' }
        if ($Failure -ceq 'connect-uncertain') { return @{status='uncertain';selectionVerified=$false;actionsAttempted=-1} }
        return @{status='verified';selectionVerified=$true;actionsAttempted=1}
    } -Activate {
        $State.calls.Add('activate')
        if ($Failure -ceq 'activate') { throw 'activation uncertain' }
        return @{activated=$true}
    } -HasJournal { if ($Failure -ceq 'journal-read') { throw 'unknown' };return $State.journal } -Restore {
        $State.calls.Add('restore')
        if ($Failure -ceq 'restore') { return @{status='uncertain';restorationVerified=$false} }
        if (-not $BreakRestoration) { $State.journal=$false;$State.baseline=$true }
        return @{status='verified';restorationVerified=$true}
    } -AtBaseline { if ($Failure -ceq 'registry-read') { throw 'unknown' };return $State.baseline }
}
Case 'normal flow restores once and verifies independent readback' {
    $s=New-State;$r=Run $s
    Assert ($r.status -ceq 'verified' -and $r.cleanupAllowed -and -not $r.preserveRecovery) 'normal original restoration removes journal and restores both values'
    Assert (($s.calls -join ',') -ceq 'connect,activate,restore') 'normal flow order'
    Assert (-not $r.chatVerified -and -not $r.backendHandshakeVerified -and -not $r.restoredDesktopStartVerified) 'limited acceptance claims'
}
foreach ($failure in @('connect-throw','connect-uncertain','activate','restore','registry-read','journal-read')) {
    Case $failure {
        $s=New-State;$r=Run $s $failure
        Assert ($r.status -ceq 'blocked') 'failure cannot become success'
        Assert (@($s.calls | Where-Object { $_ -ceq 'connect' }).Count -eq 1) 'Connect never retries'
        Assert (@($s.calls | Where-Object { $_ -ceq 'activate' }).Count -le 1) 'activation never retries'
        Assert (@($s.calls | Where-Object { $_ -ceq 'restore' }).Count -le 1) 'Restore never retries'
        if ($failure -like 'connect-*') {
            Assert (($s.calls -join ',') -ceq 'connect,restore') 'uncertain Connect uses distinct journal recovery without activation'
        }
        if ($failure -cin @('restore','registry-read','journal-read')) {
            Assert (-not $r.cleanupAllowed -and $r.preserveRecovery) 'unknown recovery retains live state'
        } else { Assert ($r.cleanupAllowed -and $r.selectionRestored) 'verified recovery after earlier failure allows cleanup' }
    }
}
Case 'no journal does not imply unchanged selection' {
    $r=Invoke-OriginalDesktopSequence -Connect {throw 'failed'} -Activate {throw 'must not run'} -HasJournal {$false} -Restore {throw 'must not run'} -AtBaseline {$false}
    Assert (-not $r.cleanupAllowed -and $r.preserveRecovery -and -not $r.selectionRestored) 'missing journal with drift stays blocked'
}
Case 'pre-action failure clean baseline is safe but not acceptance' {
    $r=Invoke-OriginalDesktopSequence -Connect {@{status='blocked';selectionVerified=$false;actionsAttempted=0}} -Activate {throw 'must not run'} -HasJournal {$false} -Restore {throw 'must not run'} -AtBaseline {$true}
    Assert ($r.cleanupAllowed -and $r.status -ceq 'blocked' -and -not $r.originalRestoreVerified) 'clean no-action failure stays unaccepted'
}

foreach ($outcome in @('throw','uncertain')) {
    Case ('pending Connect clean snapshot '+$outcome) {
        $r=Invoke-OriginalDesktopSequence -Connect {
            if ($outcome -ceq 'throw') { throw 'worker output lost' }
            return @{status='uncertain';selectionVerified=$false;actionsAttempted=-1}
        } -Activate {throw 'must not run'} -HasJournal {$false} -Restore {throw 'must not run'} -AtBaseline {$true}
        Assert (-not $r.cleanupAllowed -and $r.preserveRecovery -and $r.status -ceq 'blocked') 'uncertain asynchronous Connect cannot be cleaned up from clean snapshots'
    }
}
