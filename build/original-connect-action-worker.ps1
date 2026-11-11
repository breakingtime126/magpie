# Fixed Core subprocess. The supervisor provides the hard deadline for blocking UIA.
[CmdletBinding()]
param([ValidateSet('Connect','Restore')][string]$Mode,[int]$TargetProcessId,[long]$TargetStartTicks,
    [long]$TargetWindow,[string]$TargetExecutable,[string]$ExpectedLauncher,[string]$JournalPath,
    [string]$SnapshotPath,[string]$AttemptDirectory)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-connect-action-core.ps1')
$result=[ordered]@{schema=1;scope='original-magpie-ui-action';mode=$Mode;status='blocked';stage='context';
    actionsAttempted=0;selectionVerified=$false;restorationVerified=$false;desktopVerified=$false;preserveRecovery=$true;configureReadiness=(New-OriginalConfigureReadiness)}
$process=$null;$ledger=@{}
try {
    if ($Mode -cnotin @('Connect','Restore')) { throw 'Exact action mode required.' }
    Assert-OriginalActionContext
    $result.stage='claim'
    Assert-OriginalRegularPath $AttemptDirectory
    Write-OriginalExclusive (Join-Path $AttemptDirectory ($Mode+'.claim')) 'one-shot'
    $result.stage='identity'
    if ($TargetProcessId -le 0 -or $TargetWindow -eq 0 -or $TargetStartTicks -le 0) { throw 'Invalid owner.' }
    Assert-OriginalRegularPath $TargetExecutable;Assert-OriginalRegularPath $ExpectedLauncher
    Assert-OriginalRegularPath $JournalPath
    $process=Get-Process -Id $TargetProcessId -ErrorAction Stop;$null=$process.Handle
    $expected=@{pid=$TargetProcessId;ticks=$TargetStartTicks;window=$TargetWindow;executable=$TargetExecutable;title='magpie'}
    $watch=[Diagnostics.Stopwatch]::StartNew()
    function Assert-Owner {
        Assert-OriginalDeadline $watch.ElapsedMilliseconds
        $process.Refresh()
        Assert-OriginalOwnerIdentity $expected @{pid=$process.Id;ticks=$process.StartTime.ToUniversalTime().Ticks;
            window=$process.MainWindowHandle.ToInt64();executable=$process.MainModule.FileName;title=$process.MainWindowTitle;exited=$process.HasExited}
    }
    Assert-Owner
    $before=Read-OriginalJSON $SnapshotPath;Assert-OriginalSelection $before
    function Read-Journal {
        $j=Read-OriginalJSON $JournalPath
        Assert-OriginalJournal $j $before $ExpectedLauncher $JournalPath
        return $j
    }
    function Test-JournalPresent {
        Assert-OriginalRegularPath $JournalPath
        try { $null=[IO.File]::GetAttributes($JournalPath);return $true }
        catch [IO.FileNotFoundException] { return $false }
        catch [IO.DirectoryNotFoundException] { return $false }
    }
    $result.stage='precondition'
    if ($Mode -ceq 'Connect') {
        if ((Test-JournalPresent) -or -not (Test-OriginalSelectionEqual (Read-OriginalSelection) $before)) { throw 'Not fresh.' }
    } else {
        $journal=Read-Journal
        $journalDigest=(Get-FileHash -LiteralPath $JournalPath -Algorithm SHA256).Hash
        Assert-OriginalRecoverySelection (Read-OriginalSelection) $journal
    }
    Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes
    $root=[Windows.Automation.AutomationElement]::FromHandle([IntPtr]$TargetWindow)
    if ($null -eq $root -or $root.Current.ProcessId -ne $TargetProcessId) { throw 'Root identity changed.' }
    $walker=[Windows.Automation.TreeWalker]::RawViewWalker
    function Find-Targets([string]$Label) {
        Assert-Owner
        $queue=[Collections.Generic.Queue[object]]::new();$queue.Enqueue(@{element=$root;depth=0})
        $found=[Collections.Generic.List[object]]::new();$nodes=0
        while ($queue.Count) {
            Assert-Owner
            if ($nodes -ge 2048) { throw 'UIA node limit.' }
            $item=$queue.Dequeue();$nodes++;$e=$item.element;$c=$e.Current
            if ($c.Name -ceq $Label) {
                $toggle=$null;$invoke=$null;$pattern='';$state='';$handle=$null
                if ($e.TryGetCurrentPattern([Windows.Automation.TogglePattern]::Pattern,[ref]$toggle)) {
                    $pattern='Toggle';$state=[string]$toggle.Current.ToggleState;$handle=$toggle
                } elseif ($e.TryGetCurrentPattern([Windows.Automation.InvokePattern]::Pattern,[ref]$invoke)) {
                    $pattern='Invoke';$handle=$invoke
                }
                $found.Add(@{name=$c.Name;enabled=$c.IsEnabled;offscreen=$c.IsOffscreen;role=$c.ControlType.ProgrammaticName;
                    pattern=$pattern;state=$state;element=$e;handle=$handle;runtime=($e.GetRuntimeId() -join ',')})
            }
            $child=$walker.GetFirstChild($e)
            if ($null -ne $child -and $item.depth -ge 20) { throw 'UIA depth limit.' }
            while ($null -ne $child) {
                Assert-Owner
                if ($nodes+$queue.Count -ge 2048) { throw 'UIA node limit.' }
                $queue.Enqueue(@{element=$child;depth=$item.depth+1});$child=$walker.GetNextSibling($child)
            }
        }
        Assert-Owner
        return $found.ToArray()
    }
    function Invoke-Target([string]$Action,[string]$Label,[string]$Pattern,[string]$State,$ReadyTarget=$null) {
        $target=if ($null -eq $ReadyTarget) { Assert-OriginalTarget @(Find-Targets $Label) $Label $Pattern $State }
            else { Assert-OriginalTarget @($ReadyTarget) $Label $Pattern $State }
        $check={ Assert-Owner }
        $mark={ param($name)
            Write-OriginalExclusive (Join-Path $AttemptDirectory ($name+'.attempt')) 'outcome-uncertain-until-readback'
            $result.actionsAttempted++
        }
        $act={
            # Re-read the retained element AND uniqueness just before action.
            $now=Assert-OriginalTarget @(Find-Targets $Label) $Label $Pattern $State
            if ($target.runtime -cne $now.runtime -or ($target.element.GetRuntimeId() -join ',') -cne $target.runtime) { throw 'Target identity drift.' }
            $c=$target.element.Current
            if ($c.Name -cne $Label -or -not $c.IsEnabled -or $c.IsOffscreen) { throw 'Target changed.' }
            Assert-Owner
            if ($Pattern -ceq 'Toggle') {
                if ([string]$target.handle.Current.ToggleState -cne $State) { throw 'Toggle state changed.' }
                $target.handle.Toggle()
            } else { $target.handle.Invoke() }
        }
        Invoke-OriginalOnce $ledger $Action $check $mark $act
    }
    if ($Mode -ceq 'Connect') {
        $result.stage='configure'
        $readinessWatch=[Diagnostics.Stopwatch]::StartNew()
        $ready=Wait-OriginalConfigureTarget $result.configureReadiness `
            { Find-Targets 'Configure Codex launch selection' } { Assert-Owner } `
            { $readinessWatch.ElapsedMilliseconds } { param($milliseconds) Start-Sleep -Milliseconds $milliseconds }
        Invoke-Target 'configure' 'Configure Codex launch selection' 'Toggle' 'Off' $ready
        $result.stage='selection-readback'
        while ($true) {
            Assert-Owner
            if (Test-JournalPresent) {
                $journal=Read-Journal
                if (Test-OriginalSelectionEqual (Read-OriginalSelection) $journal.installed) {
                    $profile=$journal.installed.profile.text;Assert-OriginalRegularPath $profile
                    if ((Get-FileHash -LiteralPath $profile -Algorithm SHA256).Hash.ToLowerInvariant() -cne $journal.profileSHA256) { throw 'Profile hash changed.' }
                    $targets=@(Find-Targets 'Disconnect and restore Codex launch selection')
                    if (Test-OriginalTargetReady $targets 'Disconnect and restore Codex launch selection' 'Toggle' 'On') {
                        $result.selectionVerified=$true;break
                    }
                }
            }
            Start-Sleep -Milliseconds 100
        }
    } else {
        $result.stage='restore-dialog'
        # The recovery UI is an Invoke button if the original transaction was
        # uncertain; an installed selection is an On Toggle. Discover it now.
        $targets=@(Find-Targets 'Disconnect and restore Codex launch selection')
        if ($targets.Count -ne 1) { throw 'Restore is not unique.' }
        $pattern=$targets[0].pattern;$state=$targets[0].state
        if (-not (($pattern -ceq 'Toggle' -and $state -ceq 'On') -or ($pattern -ceq 'Invoke' -and $state -ceq ''))) { throw 'Restore pattern unavailable.' }
        Invoke-Target 'restore-dialog' 'Disconnect and restore Codex launch selection' $pattern $state
        $result.stage='restore-confirm'
        while (@(Find-Targets 'Disconnect and restore').Count -eq 0) { Assert-Owner;Start-Sleep -Milliseconds 100 }
        # The dialog's exact explanatory text distinguishes this confirmation
        # from unrelated legacy Disconnect controls. Text is never exported.
        $description='Restore the previous CODEX_CLI_PATH and MAGPIE_APP_SERVER_BRIDGE_CONFIG values recorded in Magpie''s journal. External changes are refused. Native authentication, history and already running windows are not changed.'
        $descriptions=@(Find-Targets $description)
        if ($descriptions.Count -ne 1 -or $descriptions[0].offscreen) { throw 'Recovery confirmation context missing.' }
        if ((Get-FileHash -LiteralPath $JournalPath -Algorithm SHA256).Hash -cne $journalDigest) { throw 'Journal changed.' }
        Assert-OriginalRecoverySelection (Read-OriginalSelection) (Read-Journal)
        Invoke-Target 'restore-confirm' 'Disconnect and restore' 'Invoke' ''
        $result.stage='restoration-readback'
        while ($true) {
            Assert-Owner
            if (-not (Test-JournalPresent)) {
                Assert-OriginalRestored (Test-JournalPresent) (Read-OriginalSelection) $before
                $targets=@(Find-Targets 'Configure Codex launch selection')
                if (Test-OriginalTargetReady $targets 'Configure Codex launch selection' 'Toggle' 'Off') {
                    $result.restorationVerified=$true;$result.preserveRecovery=$false;break
                }
            } elseif ((Get-FileHash -LiteralPath $JournalPath -Algorithm SHA256).Hash -cne $journalDigest) { throw 'Journal changed.' }
            Start-Sleep -Milliseconds 100
        }
    }
    Assert-Owner;$result.status='verified';$result.stage='complete'
} catch {
    $result.selectionVerified=$false;$result.restorationVerified=$false;$result.preserveRecovery=$true
    if ($ledger.Count -gt 0) { $result.status='uncertain' }
} finally { if ($null -ne $process) { $process.Dispose() } }
$result | ConvertTo-Json -Depth 4 -Compress
