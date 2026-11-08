# Pure contract tests. No process, UI, network or security setting action.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'desktop-live-welcome.ps1')
$checks=0
function Confirm-Test([bool]$Condition,[string]$Label) {
    if (-not $Condition) { throw $Label };$script:checks++
}
function Reject-Test([scriptblock]$Action,[string]$Label) {
    $rejected=$false;try { & $Action } catch { $rejected=$true }
    Confirm-Test $rejected $Label
}
foreach ($file in @('desktop-unelevated-read-only.ps1','desktop-unelevated-read-only.tests.ps1',
    'desktop-live-welcome.ps1','test-desktop-cold-start.ps1')) {
    $tokens=$null;$errors=$null
    $null=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file),[ref]$tokens,[ref]$errors)
    Confirm-Test ($errors.Count -eq 0) ('Syntax: '+$file)
}
$profile='unelevated-read-only-4239'
$arguments=@($profile,'bridge-owned-catalog-4239','bridge-owned-model-selection-4239',
    'bridge-owned-model-settings-4239','bridge-unelevated-chat-v1','1')
Assert-DesktopUnelevatedProfile @arguments
Assert-DesktopUnelevatedProfile '' '' '' '' '' '2'
foreach ($index in 0..5) {
    foreach ($bad in @('','unknown','2')) {
        $changed=$arguments.Clone();$changed[$index]=$bad
        Reject-Test { Assert-DesktopUnelevatedProfile @changed } 'All explicit gates required'
    }
}
function New-SetupEvidence {
    return [pscustomobject][ordered]@{schema=1;profile='unelevated-read-only-4239';stage='complete';
        preflight_passed=2;setup_attempted=2;setup_started=2;setup_completed=2;postverified=2;
        peers_closed=4;partial_state_possible=$false;no_retry=$true}
}
$evidence=New-SetupEvidence
Assert-LiveSandboxSetup $evidence -Complete
foreach ($key in @('preflight_passed','setup_attempted','setup_started','setup_completed','postverified','peers_closed')) {
    foreach ($bad in @(-1,0,1,5,'2')) {
        $evidence=New-SetupEvidence;$evidence.$key=$bad
        Reject-Test { Assert-LiveSandboxSetup $evidence -Complete } 'Incomplete or mistyped setup evidence rejected'
    }
}
foreach ($key in @('partial_state_possible','no_retry')) {
    $evidence=New-SetupEvidence;$evidence.$key='false'
    Reject-Test { Assert-LiveSandboxSetup $evidence -Complete } 'Boolean evidence is typed'
}
$evidence=New-SetupEvidence;$evidence.partial_state_possible=$true
Reject-Test { Assert-LiveSandboxSetup $evidence -Complete } 'Uncertain completion rejected'
$evidence=New-SetupEvidence;$evidence.no_retry=$false
Reject-Test { Assert-LiveSandboxSetup $evidence } 'No automatic retry'
$evidence=New-SetupEvidence;$evidence|Add-Member NoteProperty path 'not-permitted'
Reject-Test { Assert-LiveSandboxSetup $evidence } 'No raw fields'
$partial=New-SetupEvidence;$partial.stage='setup';$partial.setup_started=1;$partial.setup_completed=1
$partial.postverified=0;$partial.peers_closed=0;$partial.partial_state_possible=$true
Assert-LiveSandboxSetup $partial
Reject-Test { Assert-LiveSandboxSetup $partial -Complete } 'Partial state is not ready'
$partial.partial_state_possible=$false
Reject-Test { Assert-LiveSandboxSetup $partial } 'Incomplete setup cannot erase uncertainty'

$ready=[pscustomobject][ordered]@{schema=1;scope='desktop-bridge-owned-catalog-fixtures-only';status='ready';
    lifetime_seconds=360;native_sha256='4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382';
    catalog_source='bridge-owned-self-authored-test-models-v1';owned_catalog_sha256=('a'*64);sandbox_setup=(New-SetupEvidence)}
Assert-LiveReady $ready $true 'bridge-owned-catalog-4239' $profile
Reject-Test { Assert-LiveReady $ready $true 'bridge-owned-catalog-4239' } 'Setup not implied by owned catalog'
$ready.lifetime_seconds=180
Reject-Test { Assert-LiveReady $ready $true 'bridge-owned-catalog-4239' $profile } 'New budget exact'
$ready.lifetime_seconds=360
$audit=[pscustomobject][ordered]@{schema=1;scope='desktop-bridge-owned-catalog-fixtures-only';status='stopped';
    failure='';alpha_requests=2;beta_requests=2;fixture_errors=0;sandbox_setup=(New-SetupEvidence)}
Assert-LiveAudit $audit 0 'bridge-owned-catalog-4239' $profile
Reject-Test { Assert-LiveAudit $audit 0 'bridge-owned-catalog-4239' } 'Old menu profile cannot authorize requests'
$audit.fixture_errors=1
Reject-Test { Assert-LiveAudit $audit 0 'bridge-owned-catalog-4239' $profile } 'Fixture rejection remains fatal'

$unknown=Get-LiveUnelevatedFailure $null
Confirm-Test ($unknown.partial_state_possible -and $unknown.no_retry) 'Missing output is not proof of no changes'
$raw=[pscustomobject]@{Exited=$true;Overflow=$false;Output=('{"status":"failed","sandbox_setup":'+((New-SetupEvidence)|ConvertTo-Json -Compress)+'}')}
$known=Get-LiveUnelevatedFailure $raw
Confirm-Test ($known.status -ceq 'failed' -and $known.no_retry) 'Fixed complete setup evidence survives later failure'
$raw.Output='{"status":"failed","failure":"sandbox_setup_layers_rejected","sandbox_setup":'+((New-SetupEvidence)|ConvertTo-Json -Compress)+'}'
Confirm-Test ((Get-LiveUnelevatedFailure $raw).failure -ceq 'sandbox_setup_layers_rejected') 'Exact fixed native failure classification retained'
foreach ($fixedFailure in @('sandbox_setup_approval_rejected','sandbox_setup_config_rpc_failed','sandbox_setup_default_permissions_rejected','sandbox_setup_initial_file_rejected','sandbox_setup_initialize_rpc_failed','sandbox_setup_initialized_send_failed','sandbox_setup_layer_count_rejected','sandbox_setup_layer_disabled','sandbox_setup_layer_metadata_rejected','sandbox_setup_layer_source_rejected','sandbox_setup_peer_start_failed','sandbox_setup_permission_profile_rejected','sandbox_setup_readonly_rejected','sandbox_setup_requirements_rpc_failed','sandbox_setup_session_layer_rejected','sandbox_setup_system_layer_rejected','sandbox_setup_thread_identity_rejected','sandbox_setup_thread_rpc_failed','sandbox_setup_user_content_rejected','sandbox_setup_user_identity_rejected','sandbox_setup_user_layer_missing')) {
    $raw.Output=([ordered]@{status='failed';failure=$fixedFailure;sandbox_setup=(New-SetupEvidence)}|ConvertTo-Json -Compress)
    Confirm-Test ((Get-LiveUnelevatedFailure $raw).failure -ceq $fixedFailure) 'Specific fixed preflight classification retained'
}
foreach ($invalidFailure in @('C:\private\config.toml','native error contains secret','SANDBOX_SETUP_LAYERS_REJECTED','',12,$null)) {
    $raw.Output=([ordered]@{status='failed';failure=$invalidFailure;sandbox_setup=(New-SetupEvidence)}|ConvertTo-Json -Compress)
    $redacted=Get-LiveUnelevatedFailure $raw
    Confirm-Test ($redacted.status -ceq 'failed' -and $redacted.failure -ceq 'unknown' -and $redacted.no_retry) 'Unknown native failure text never published'
}
$raw.Output='{}'
$unknown=Get-LiveUnelevatedFailure $raw
Confirm-Test ($unknown.partial_state_possible -and $unknown.no_retry) 'Unrecognized failure remains uncertain'
Write-Output ('UNELEVATED_READONLY_CONTRACT_CHECKS='+$checks)

function New-GuardEvents {
    $records=@([pscustomobject]@{seq=10;pid=9;kind='fixture_readonly_guard_started'})
    foreach ($index in 0..3) {
        $owner=$(if($index -lt 2){'alpha'}else{'beta'});$round=($index % 2)+1
        $records+=,[pscustomobject]@{seq=(20+$index);pid=9;kind='fixture_readonly_turn_completed';
            fixtureReadOnly=[pscustomobject]@{schema=1;owner=$owner;completed=$round;threadTurn=$round}}
    }
    return $records
}
$events=@(New-GuardEvents)
Confirm-Test ((Get-LiveReadonlyConversationEvidence $events).status -ceq 'observed') 'Correlated guarded four turns'
foreach ($fault in @('missing','duplicate','reordered','cross-process','different-thread','rejected','no-start')) {
    $events=@(New-GuardEvents)
    switch ($fault) {
        missing {$events=@($events[0..3])}
        duplicate {$events+=,$events[4]}
        reordered {$events[2].seq=19}
        'cross-process' {$events[4].pid=8}
        'different-thread' {$events[2].fixtureReadOnly.threadTurn=1}
        rejected {$events+=,[pscustomobject]@{seq=30;pid=9;kind='fixture_guard_rejected';direction='to_bridge'}}
        'no-start' {$events=@($events[1..4])}
    }
    Confirm-Test ((Get-LiveReadonlyConversationEvidence $events).status -cne 'observed') ('Guard evidence refuses '+$fault)
}
$events=@(New-GuardEvents)
Reject-Test { Assert-PublicFixtureGuardEvent $events[0] } 'Guard metadata requires explicit profile'
$events[1].fixtureReadOnly|Add-Member NoteProperty threadId 'private'
Reject-Test { Get-LiveReadonlyConversationEvidence $events } 'No raw IDs'
$prior=[pscustomobject]@{status='observed';stage='settings';reason='';exitCode=0}
$chat=[pscustomobject]@{status='observed';stage='complete';reason='';sendAttempts=4;uiAnswers=4;
    alphaTurns=2;betaTurns=2;newChats=1;partialActionUncertain=$false;restartAttempted=$false}
$guard=Get-LiveReadonlyConversationEvidence @(New-GuardEvents)
$audit.fixture_errors=0
Confirm-Test ((Get-LiveUnelevatedChatOutcome $prior $chat $guard $audit 4).exitCode -eq 0) 'Independent evidence intersects'
$audit.beta_requests=0
Confirm-Test ((Get-LiveUnelevatedChatOutcome $prior $chat $guard $audit 4).exitCode -ne 0) 'UI is not inference proof'
$audit.beta_requests=2;$chat.status='blocked';$chat.stage='alpha-entry';$chat.reason='target_absent_or_ambiguous'
Confirm-Test ((Get-LiveUnelevatedChatOutcome $prior $chat $guard $audit 4).reason -ceq 'target_absent_or_ambiguous') 'Actual first UI blocker preserved'
Write-Output ('UNELEVATED_READONLY_FULL_CHECKS='+$checks)

$scratch=Join-Path ([IO.Path]::GetTempPath()) ('guard-activation-test-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($scratch)
try {
    $prefix=Join-Path $scratch 'trace';$path=$prefix+'.01.jsonl'
    $start='{"seq":1,"pid":9,"kind":"fixture_readonly_guard_started"}'+"`n"
    [IO.File]::WriteAllText($path,$start,[Text.UTF8Encoding]::new($false))
    Confirm-Test ((Read-LiveGuardActivation $prefix @(9)) -eq 9) 'Actual guard record required before UI'
    Reject-Test { Read-LiveGuardActivation $prefix @(8) } 'Guard must belong to observed process'
    [IO.File]::WriteAllText($path,($start+$start),[Text.UTF8Encoding]::new($false))
    Reject-Test { Read-LiveGuardActivation $prefix @(9) } 'Duplicate active streams rejected'
    [IO.File]::WriteAllText($path,($start+'{"seq":2,"pid":9,"kind":"fixture_guard_rejected","direction":"to_bridge"}'+"`n"),[Text.UTF8Encoding]::new($false))
    Reject-Test { Read-LiveGuardActivation $prefix @(9) } 'Rejected guard not active for UI'
    [IO.File]::WriteAllText($path,$start.TrimEnd("`n"),[Text.UTF8Encoding]::new($false))
    Reject-Test { Read-LiveGuardActivation $prefix @(9) } 'Partial trace is not activation proof'
    [IO.File]::WriteAllText($path,('{"seq":1,"pid":9,"kind":"observer_start"}'+"`n"),[Text.UTF8Encoding]::new($false))
    Reject-Test { Read-LiveGuardActivation $prefix @(9) } 'Ordinary observer is not guarded'
} finally { [IO.Directory]::Delete($scratch,$true) }
Write-Output ('UNELEVATED_READONLY_ACTIVATION_CHECKS='+$checks)
