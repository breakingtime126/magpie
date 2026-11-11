# Offline authored fixtures only. Never execute the UIA worker body or registry.
[CmdletBinding()]
param([string]$CorePath=(Join-Path $PSScriptRoot 'original-connect-action-core.ps1'))
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
Write-Output 'ORIGINAL_CONNECT_ACTION_OFFLINE_TESTS_PASSED'
