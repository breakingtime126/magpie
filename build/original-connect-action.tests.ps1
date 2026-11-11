# Offline authored fixtures only. Never execute the UIA worker body or registry.
[CmdletBinding()]
param([string]$CorePath='')
if ($CorePath -eq '') { $CorePath=Join-Path $PSScriptRoot 'original-connect-action-core.ps1' }
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
foreach ($name in @($CorePath,(Join-Path $PSScriptRoot 'original-connect-action.ps1'),(Join-Path $PSScriptRoot 'original-connect-action-worker.ps1'))) {
    $tokens=$null;$errors=$null
    $null=[Management.Automation.Language.Parser]::ParseFile($name,[ref]$tokens,[ref]$errors)
    if ($errors.Count) { throw ('Syntax error: '+$errors[0].Message) }
}
. $CorePath
function Reject([scriptblock]$Call,[string]$Why) {
    $rejected=$false;try { & $Call | Out-Null } catch { $rejected=$true }
    if (-not $rejected) { throw ('FAIL: '+$Why) }
}
function Copy-Object($Object) { return ($Object | ConvertTo-Json -Depth 20 -Compress | ConvertFrom-Json -AsHashtable) }
$before=@{cli=@{present=$false;kind=0;text=''};profile=@{present=$true;kind=2;text='%TEMP%\prior'}}
Assert-OriginalSelection $before
if (-not (Test-OriginalSelectionEqual $before (Copy-Object $before))) { throw 'FAIL: value equality' }
foreach ($case in @('presence','type','text','empty')) {
    $changed=Copy-Object $before
    switch ($case) {
        presence { $changed.cli=@{present=$true;kind=1;text=''} }
        type { $changed.profile.kind=1 }
        text { $changed.profile.text='%TEMP%\PRIOR' }
        empty { $changed.profile.text='' }
    }
    if (Test-OriginalSelectionEqual $before $changed) { throw ('FAIL: exact '+$case) }
    Reject { Assert-OriginalRestored $false $changed $before } ('restoration '+$case)
}
Assert-OriginalRestored $false $before $before
Reject { Assert-OriginalRestored $true $before $before } 'journal removal required'
foreach ($value in @(@{present=$false;kind=1;text=''},@{present=$true;kind=4;text='x'},@{present='true';kind=1;text='x'},@{present=$true;kind=1;text="x`0"})) {
    Reject { Assert-OriginalSelection @{cli=$value;profile=$before.profile} } 'malformed registry value'
}
$label='Configure Codex launch selection'
$target=@{name=$label;enabled=$true;offscreen=$false;role='ControlType.Button';pattern='Toggle';state='Off'}
$null=Assert-OriginalTarget @($target) $label 'Toggle' 'Off'
if (-not (Test-OriginalTargetReady @($target) $label 'Toggle' 'Off')) { throw 'FAIL: completion target' }
$busy=Copy-Object $target;$busy.enabled=$false;$busy.pattern='Invoke';$busy.state=''
if (Test-OriginalTargetReady @($busy) $label 'Toggle' 'Off') { throw 'FAIL: busy completion accepted' }
Reject { Assert-OriginalTarget @($busy) $label 'Toggle' 'Off' } 'busy action rejected'
Reject { Test-OriginalTargetReady @($target,$target) $label 'Toggle' 'Off' } 'duplicate completion rejected'
Reject { Assert-OriginalTarget @() $label 'Toggle' 'Off' } 'missing target'
Reject { Assert-OriginalTarget @($target,$target) $label 'Toggle' 'Off' } 'duplicate target'
foreach ($pair in @(@('enabled',$false),@('offscreen',$true),@('name',($label+' ')),@('role','ControlType.Pane'),@('pattern','Invoke'),@('state','On'))) {
    $changed=Copy-Object $target;$changed[$pair[0]]=$pair[1]
    Reject { Assert-OriginalTarget @($changed) $label 'Toggle' 'Off' } ('target '+$pair[0])
}
$owner=@{pid=42;ticks=123456;window=77;executable='/owned/magpie.exe';title='magpie';exited=$false}
Assert-OriginalOwnerIdentity $owner $owner
foreach ($field in @('pid','ticks','window','executable','title','exited')) {
    $changed=Copy-Object $owner;$changed[$field]=if ($field -eq 'exited') { $true } else { 'changed' }
    Reject { Assert-OriginalOwnerIdentity $owner $changed } ('owner '+$field)
}
Assert-OriginalDeadline 44999
Reject { Assert-OriginalDeadline 45000 } 'deadline boundary'
Reject { Assert-OriginalDeadline -1 } 'invalid elapsed'
$events=[Collections.Generic.List[string]]::new();$ledger=@{}
Reject { Invoke-OriginalOnce $ledger 'configure' { $events.Add('check') } {param($a) $events.Add('mark')} { $events.Add('act');throw 'lost return' } } 'uncertain operation'
Reject { Invoke-OriginalOnce $ledger 'configure' { $events.Add('check-again') } {param($a) $events.Add('mark-again')} { $events.Add('retry') } } 'no action retry'
if (($events -join ',') -cne 'check,mark,act' -or $ledger.configure -cne 'uncertain') { throw 'FAIL: uncertainty recorded before action or retry occurred' }
$events.Clear();$ledger=@{}
Invoke-OriginalOnce $ledger 'confirm' { $events.Add('check') } {param($a) $events.Add('mark')} { $events.Add('act') }
if (($events -join ',') -cne 'check,mark,act,check' -or $ledger.confirm -cne 'returned') { throw 'FAIL: checks must surround action' }
$events.Clear();$ledger=@{}
Reject { Invoke-OriginalOnce $ledger 'configure' { throw 'identity drift' } {param($a) $events.Add('mark')} { $events.Add('act') } } 'pre-action identity drift'
if ($events.Count -ne 0) { throw 'FAIL: acted after identity drift' }
$ledger=@{};Reject { Invoke-OriginalOnce $ledger 'configure' {} {param($a) throw 'marker unavailable'} { throw 'must not run' } } 'marker failure'
if ($ledger.configure -cne 'uncertain') { throw 'FAIL: marker uncertainty lost' }
$events.Clear();$ledger=@{};$checks=@{count=0}
Reject { Invoke-OriginalOnce $ledger 'confirm' { $checks.count++;if ($checks.count -eq 2) { throw 'post-action identity drift' } } {param($a) $events.Add('mark')} { $events.Add('act') } } 'post-action drift'
if ($ledger.confirm -cne 'uncertain' -or ($events -join ',') -cne 'mark,act') { throw 'FAIL: post-action drift promoted to success' }
Reject { Invoke-OriginalOnce $ledger 'confirm' {} {} { $events.Add('retry') } } 'post-action drift cannot retry'
$temp=Join-Path ([IO.Path]::GetTempPath()) ('original-action-tests-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
try {
    $journalPath=Join-Path $temp 'selection.json';$launcher=Join-Path $temp 'adapter.exe'
    $profile=Join-Path (Join-Path $temp 'profile-123') 'bridge.json'
    $journal=@{version=1;before=$before;installed=@{cli=@{present=$true;kind=1;text=$launcher};profile=@{present=$true;kind=1;text=$profile}};profileSHA256=('a'*64)}
    Assert-OriginalJournal $journal $before $launcher $journalPath
    Assert-OriginalRecoverySelection $before $journal
    Assert-OriginalRecoverySelection $journal.installed $journal
    Assert-OriginalRecoverySelection @{cli=$before.cli;profile=$journal.installed.profile} $journal
    $changed=Copy-Object $journal.installed;$changed.cli.text+='external'
    Reject { Assert-OriginalRecoverySelection $changed $journal } 'external change must block recovery'
    $changed=Copy-Object $journal;$changed.before.profile.kind=1
    Reject { Assert-OriginalJournal $changed $before $launcher $journalPath } 'journal baseline type'
    $changed=Copy-Object $journal;$changed.installed.profile.text=Join-Path $temp 'bridge.json'
    Reject { Assert-OriginalJournal $changed $before $launcher $journalPath } 'profile generation containment'
    $changed=Copy-Object $journal;$changed.installed.cli.text+='other'
    Reject { Assert-OriginalJournal $changed $before $launcher $journalPath } 'launcher identity'
    $path=Join-Path $temp 'snapshot.json'
    Write-OriginalExclusive $path ($before | ConvertTo-Json -Depth 5 -Compress)
    $parsed=Read-OriginalJSON $path
    if (-not (Test-OriginalSelectionEqual $parsed $before)) { throw 'FAIL: strict JSON roundtrip' }
    Reject { Write-OriginalExclusive $path 'replacement' } 'snapshot cannot overwrite'
    foreach ($bad in @('{"x":1,"x":2}','{"x":1,"X":2}','{','',('{"nested":{"x":1,"x":2}}'))) {
        [IO.File]::WriteAllText($path,$bad)
        Reject { Read-OriginalJSON $path } 'strict JSON duplicate/parse/empty'
    }
    [IO.File]::WriteAllText($path,(' '*131073))
    Reject { Read-OriginalJSON $path } 'bounded JSON'
    Reject { Read-OriginalJSON (Join-Path $temp 'absent') } 'unreadable not empty'
} finally { [IO.Directory]::Delete($temp,$true) }
$worker=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-connect-action-worker.ps1'))
$supervisor=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-connect-action.ps1'))
foreach ($required in @('::RawViewWalker','GetRuntimeId()','Invoke-OriginalOnce','Assert-OriginalRestored',"(`$Mode+'.claim')",'$nodes -ge 2048','$item.depth -ge 20','Assert-OriginalRecoverySelection','profileSHA256')) {
    if (-not $worker.Contains($required)) { throw ('FAIL: worker contract '+$required) }
}
foreach ($required in @('$watch.ElapsedMilliseconds -ge 60000','$texts[$i].Length+$count -gt 8192','$process.Kill()','PowerShell\7\pwsh.exe','$start.EnvironmentVariables.Clear()')) {
    if (-not $supervisor.Contains($required)) { throw ('FAIL: supervisor contract '+$required) }
}
if ($supervisor.IndexOf("`$result.status='uncertain';`$result.actionsAttempted=-1") -gt $supervisor.IndexOf('$started=$process.Start()')) { throw 'FAIL: start must be marked uncertain before dispatch' }
# Initial UIA readiness is a read-only sequence, not a repeated Configure.
$readyTarget=Copy-Object $target;$readyTarget.runtime='synthetic-retained-id'
$busyTarget=Copy-Object $readyTarget;$busyTarget.enabled=$false
$hiddenTarget=Copy-Object $readyTarget;$hiddenTarget.offscreen=$true
$fixture=@{clock=0;reads=0;ownerChecks=0;delays=0}
$readiness=New-OriginalConfigureReadiness
$retained=Wait-OriginalConfigureTarget $readiness {
    $fixture.reads++
    switch ($fixture.reads) { 1 { return @() };2 { return $busyTarget };3 { return $hiddenTarget };default { return $readyTarget } }
} { $fixture.ownerChecks++ } { $fixture.clock } { param($ms) $fixture.clock+=$ms;$fixture.delays++ }
if (-not [object]::ReferenceEquals($retained,$readyTarget) -or $fixture.reads -ne 4 -or $fixture.ownerChecks -ne 8 -or
    $fixture.delays -ne 3 -or $readiness.status -cne 'ready' -or $readiness.completedReads -ne 4 -or
    $readiness.lastCompleteObservation.reason -cne 'ready') { throw 'FAIL: readiness must wait using reads and retain actual target' }
Assert-OriginalConfigureReadiness $readiness
foreach ($case in @('duplicate','role','pattern','state')) {
    $bad=Copy-Object $readyTarget
    switch ($case) { role { $bad.role='ControlType.Pane' };pattern { $bad.pattern='Invoke' };state { $bad.state='On' } }
    $fixture=@{reads=0;delays=0};$readiness=New-OriginalConfigureReadiness
    Reject { Wait-OriginalConfigureTarget $readiness {
        $fixture.reads++;if ($fixture.reads -gt 1) { throw 'test stops an erroneous repeated read' }
        if ($case -ceq 'duplicate') { return @($bad,$bad) };return $bad
    } {} { 0 } { param($ms) $fixture.delays++ } } ('readiness '+$case)
    if ($fixture.reads -ne 1 -or $fixture.delays -ne 0 -or $readiness.status -cne 'rejected' -or
        $readiness.lastCompleteObservation.reason -cne $case) { throw ('FAIL: readiness must immediately reject '+$case) }
    Assert-OriginalConfigureReadiness $readiness
}
$fixture=@{reads=0;checks=0};$readiness=New-OriginalConfigureReadiness
Reject { Wait-OriginalConfigureTarget $readiness { $fixture.reads++;return $readyTarget } {
    $fixture.checks++;if ($fixture.checks -eq 2) { throw 'owner changed during read' }
} { 0 } { throw 'must not delay after drift' } } 'readiness owner drift'
if ($readiness.status -cne 'owner-check' -or $fixture.reads -ne 1 -or $readiness.completedReads -ne 1) { throw 'FAIL: readiness drift must stop' }
$fixture=@{reads=0};$readiness=New-OriginalConfigureReadiness
Reject { Wait-OriginalConfigureTarget $readiness { $fixture.reads++;throw 'UIA failure' } {} { 0 } { throw 'must not retry failed read' } } 'UIA readiness failure'
if ($fixture.reads -ne 1 -or $readiness.status -cne 'reading' -or $readiness.completedReads -ne 0 -or
    $null -ne $readiness.lastCompleteObservation) { throw 'FAIL: failed read is unknown, not missing' }
$fixture=@{clock=14900;reads=0;delays=0};$readiness=New-OriginalConfigureReadiness
Reject { Wait-OriginalConfigureTarget $readiness { $fixture.reads++;return @() } {} { $fixture.clock } {
    param($ms) $fixture.delays++;$fixture.clock+=$ms
    if ($fixture.delays -gt 1) { throw 'test bounds mutated deadline' }
} } 'bounded initial wait'
if ($fixture.reads -ne 1 -or $fixture.delays -ne 1 -or $readiness.status -cne 'deadline' -or
    $readiness.lastCompleteObservation.reason -cne 'missing') { throw 'FAIL: readiness deadline must stop before another read' }
$fixture=@{clock=0};$readiness=New-OriginalConfigureReadiness
Reject { Wait-OriginalConfigureTarget $readiness { $fixture.clock=15000;return $readyTarget } {} { $fixture.clock } {} } 'late ready control'
if ($readiness.status -cne 'deadline') { throw 'FAIL: late ready control must not escape deadline' }
Assert-OriginalConfigureReadiness $readiness
$observation=Get-OriginalConfigureObservation @($readyTarget,$busyTarget,$hiddenTarget)
if ($observation.total -ne 3 -or $observation.enabledVisible -ne 1 -or $observation.button -ne 3 -or
    $observation.toggle -ne 3 -or $observation.off -ne 3 -or $observation.reason -cne 'duplicate') { throw 'FAIL: fixed readiness counts' }
$badReport=New-OriginalConfigureReadiness;$badReport.status='arbitrary private UI text'
Reject { Assert-OriginalConfigureReadiness $badReport } 'fixed readiness vocabulary'
$badReport=New-OriginalConfigureReadiness;$badReport.completedReads=1
Reject { Assert-OriginalConfigureReadiness $badReport } 'missing complete observation'
$badReport=New-OriginalConfigureReadiness;$badReport.readAttempts=1;$badReport.completedReads=1
$badReport.lastCompleteObservation=Copy-Object $observation;$badReport.lastCompleteObservation.secret='private'
Reject { Assert-OriginalConfigureReadiness $badReport } 'no unknown diagnostic fields'

foreach ($required in @('Wait-OriginalConfigureTarget $result.configureReadiness',
    "Invoke-Target 'configure' 'Configure Codex launch selection' 'Toggle' 'Off' `$ready")) {
    if (-not $worker.Contains($required)) { throw 'FAIL: readiness must feed retained target into original one-shot action' }
}
# Post-action diagnostics are observations only, never dispatch or recovery evidence.
$labels=Get-OriginalPostActionLabels
$configure=Copy-Object $target
$restore=Copy-Object $target;$restore.name=$labels[1];$restore.enabled=$false;$restore.pattern='Invoke';$restore.state=''
$errorNode=Copy-Object $target;$errorNode.name=$labels[2];$errorNode.role='ControlType.Text';$errorNode.pattern='';$errorNode.state=''
$unknownError=Copy-Object $errorNode;$unknownError.name='arbitrary private error and path'
$nearError=Copy-Object $errorNode;$nearError.name=$labels[2]+' extra'
$post=New-OriginalPostActionReadback
$result=@{actionsAttempted=1;selectionVerified=$false;restorationVerified=$false;preserveRecovery=$true}
Update-OriginalPostActionReadback $post { return @($configure,$unknownError,$nearError) }
if ($post.firstComplete.configureOff -ne 1 -or $post.sawKnownError -or $post.sawDisabledRestore) { throw 'FAIL: unknown error or off control is not dispatch evidence' }
$firstObservation=$post.firstComplete
Update-OriginalPostActionReadback $post { return @($restore) }
if (-not $post.sawDisabledRestore -or $post.lastComplete.restoreDisabledVisible -ne 1 -or $post.lastComplete.restoreInvoke -ne 1) { throw 'FAIL: busy recovery observation lost' }
Update-OriginalPostActionReadback $post { return @($configure,$errorNode) }
if (-not [object]::ReferenceEquals($firstObservation,$post.firstComplete) -or -not $post.sawDisabledRestore -or
    $post.firstComplete.unavailableErrors -ne 0 -or $post.firstComplete.configureOff -ne 1 -or $post.lastComplete.unavailableErrors -ne 1 -or -not $post.sawKnownError -or
    $post.completedReads -ne 3 -or $post.readAttempts -ne 3) { throw 'FAIL: post-action first/last/history metadata' }
if (($result | ConvertTo-Json -Compress) -notmatch '"actionsAttempted":1' -or $result.selectionVerified -or $result.restorationVerified -or -not $result.preserveRecovery) { throw 'FAIL: diagnostics must not promote action or restoration' }
Assert-OriginalPostActionReadback $post
$prior=$post.lastComplete
Reject { Update-OriginalPostActionReadback $post { $configure;throw 'incomplete UIA traversal' } } 'incomplete post-action read'
if ($post.status -cne 'read-failed' -or $post.completedReads -ne 3 -or $post.readAttempts -ne 4 -or
    -not [object]::ReferenceEquals($prior,$post.lastComplete)) { throw 'FAIL: incomplete post-action data must not replace last complete snapshot' }
Assert-OriginalPostActionReadback $post
$changedOwner=Copy-Object $owner;$changedOwner.ticks++
$post=New-OriginalPostActionReadback
Reject { Update-OriginalPostActionReadback $post { Assert-OriginalOwnerIdentity $owner $changedOwner;return $configure } } 'post-action owner drift'
if ($post.status -cne 'read-failed' -or $post.completedReads -ne 0 -or $null -ne $post.firstComplete) { throw 'FAIL: owner drift cannot publish observation' }
foreach ($name in @($labels[2].ToUpperInvariant(),(' '+$labels[2]),($labels[2]+"`nprivate"),'private')) {
    $n=Copy-Object $errorNode;$n.name=$name
    $o=Get-OriginalPostActionObservation @($n)
    if (($o.Values | Measure-Object -Sum).Sum -ne 0) { throw 'FAIL: error labels must match exactly' }
}
$errorNode.offscreen=$true;$o=Get-OriginalPostActionObservation @($errorNode)
if ($o.unavailableErrors -ne 0) { throw 'FAIL: hidden error is not visible failure evidence' }
$post=New-OriginalPostActionReadback;Update-OriginalPostActionReadback $post { return @($configure,$configure) }
if ($post.lastComplete.configureTotal -ne 2) { throw 'FAIL: diagnostic duplicates must remain visible as counts' }
$post.lastComplete.raw='private'
Reject { Assert-OriginalPostActionReadback $post } 'no arbitrary post-action output'
foreach ($required in @('[string[]]$Label','if ($c.Name -cin $Label)',
    'Update-OriginalPostActionReadback $result.postActionReadback { Find-Targets (Get-OriginalPostActionLabels) }',
    '$nextObservation=$watch.ElapsedMilliseconds+1000')) {
    if (-not $worker.Contains($required)) { throw 'FAIL: bounded read-only post-action wiring' }
}
$observationAt=$worker.IndexOf('Update-OriginalPostActionReadback $result.postActionReadback')
$journalAt=$worker.IndexOf('if (Test-JournalPresent)', $worker.IndexOf("`$result.stage='selection-readback'"))
if ($observationAt -lt 0 -or $observationAt -gt $journalAt) { throw 'FAIL: missing journal must not suppress UI observations' }

if ([regex]::Matches($worker,'\$result\.postActionReadback').Count -ne 1) { throw 'FAIL: diagnostic fields must not drive worker action or recovery decisions' }
# A transient known error must survive later clean reads and later categories.
$errorNode.offscreen=$false
$post=New-OriginalPostActionReadback
Update-OriginalPostActionReadback $post { return @($restore) }
if ($null -ne $post.firstKnownError -or $post.sawKnownError) { throw 'FAIL: no invented first error' }
Update-OriginalPostActionReadback $post { return @($configure,$errorNode) }
$firstError=$post.firstKnownError
if ($null -eq $firstError -or $firstError.unavailableErrors -ne 1) { throw 'FAIL: first error snapshot missing' }
$laterError=Copy-Object $errorNode;$laterError.name=$labels[3]
Update-OriginalPostActionReadback $post { return @($configure,$laterError) }
Update-OriginalPostActionReadback $post { return @($configure) }
if (-not [object]::ReferenceEquals($firstError,$post.firstKnownError) -or $post.firstKnownError.unavailableErrors -ne 1 -or
    $post.firstKnownError.conflictErrors -ne 0 -or $post.lastComplete.unavailableErrors -ne 0 -or
    $post.lastComplete.conflictErrors -ne 0 -or -not $post.sawKnownError) { throw 'FAIL: transient first error overwritten or lost' }
Assert-OriginalPostActionReadback $post
Reject { Update-OriginalPostActionReadback $post { $laterError;throw 'partial read after first error' } } 'partial error snapshot'
if (-not [object]::ReferenceEquals($firstError,$post.firstKnownError)) { throw 'FAIL: partial read replaced first error' }
Assert-OriginalPostActionReadback $post
foreach ($case in @('missing','false-flag','zero-error','unknown-field','negative-count')) {
    $bad=Copy-Object $post
    switch ($case) {
        missing { $bad.firstKnownError=$null }
        false-flag { $bad.sawKnownError=$false }
        zero-error { $bad.firstKnownError.unavailableErrors=0 }
        unknown-field { $bad.firstKnownError.raw='private' }
        negative-count { $bad.firstKnownError.unavailableErrors=-1 }
    }
    Reject { Assert-OriginalPostActionReadback $bad } ('first error invariant '+$case)
}
# No complete read means neither a known-error flag nor a retained error.
$bad=New-OriginalPostActionReadback;$bad.sawKnownError=$true
Reject { Assert-OriginalPostActionReadback $bad } 'flag without observed error'
$bad=New-OriginalPostActionReadback;$bad.firstKnownError=Copy-Object $firstError;$bad.sawKnownError=$true
Reject { Assert-OriginalPostActionReadback $bad } 'error without complete read'
# All four existing fixed categories are retained, with no new labels/selectors.
foreach ($i in @(2,3,4,5)) {
    $node=Copy-Object $errorNode;$node.name=$labels[$i]
    $p=New-OriginalPostActionReadback;Update-OriginalPostActionReadback $p { return @($node) }
    $field=@{2='unavailableErrors';3='conflictErrors';4='uncertainErrors';5='catalogErrors'}[$i]
    if ($p.firstKnownError[$field] -ne 1) { throw 'FAIL: known category not retained' }
    Assert-OriginalPostActionReadback $p
}

Write-Output 'ORIGINAL_CONNECT_ACTION_OFFLINE_TESTS_PASSED'
