# Pure admission and report validation for one explicitly selected disposable
# Windows experiment. This file never performs a sandbox setup or a UI action.
function Assert-DesktopUnelevatedProfile([string]$Profile,[string]$FixtureProfile,
    [string]$SelectionProfile,[string]$SettingsProfile,[string]$Marker,[string]$Attempt) {
    if ($Profile -ceq '' -and $Marker -ceq '') { return }
    if ($Profile -cne 'unelevated-read-only-4239' -or
        $FixtureProfile -cne 'bridge-owned-catalog-4239' -or
        $SelectionProfile -cne 'bridge-owned-model-selection-4239' -or
        $SettingsProfile -cne 'bridge-owned-model-settings-4239' -or
        $Marker -cne 'bridge-unelevated-chat-v1' -or $Attempt -cne '1') {
        throw 'Explicit first-attempt disposable unelevated read-only experiment required.'
    }
}

function Assert-LiveSandboxSetup($Value,[switch]$Complete) {
    Assert-LiveShape $Value @('schema','profile','stage','preflight_passed','setup_attempted',
        'setup_started','setup_completed','postverified','peers_closed','partial_state_possible','no_retry')
    Assert-LiveInteger $Value.schema 1 1
    Assert-LiveStrings $Value @('profile','stage')
    if ($Value.profile -cne 'unelevated-read-only-4239' -or
        $Value.stage -cnotin @('preflight','setup','restart-verify','complete') -or
        $Value.partial_state_possible -isnot [bool] -or $Value.no_retry -isnot [bool] -or -not $Value.no_retry) {
        throw 'Invalid sandbox setup evidence.'
    }
    foreach ($key in @('preflight_passed','setup_attempted','setup_started','setup_completed','postverified')) {
        Assert-LiveInteger $Value.$key 0 2
    }
    Assert-LiveInteger $Value.peers_closed 0 4
    if ($Value.setup_started -gt $Value.setup_attempted -or
        $Value.setup_completed -gt $Value.setup_started -or
        $Value.postverified -gt $Value.setup_completed -or
        ($Value.setup_attempted -gt 0 -and $Value.preflight_passed -ne 2)) {
        throw 'Inconsistent sandbox setup evidence.'
    }
    if ($Complete -or $Value.stage -ceq 'complete') {
        foreach ($key in @('preflight_passed','setup_attempted','setup_started','setup_completed','postverified')) {
            if ($Value.$key -ne 2) { throw 'Sandbox setup did not complete for both backends.' }
        }
        if ($Value.stage -cne 'complete' -or $Value.peers_closed -ne 4 -or $Value.partial_state_possible) {
            throw 'Sandbox setup lifecycle remains uncertain.'
        }
    } elseif ($Value.setup_attempted -gt 0 -and -not $Value.partial_state_possible) {
        throw 'Incomplete sandbox setup must preserve partial-state uncertainty.'
    }
}

function Get-LiveUnelevatedFailure($Process) {
    # A timeout or lost reply is not permission to repeat a setup. Try only the
    # bounded, final failure record; never infer no side effects from no output.
    $result=[ordered]@{status='unavailable';partial_state_possible=$true;no_retry=$true}
    try {
        if ($null -eq $Process -or -not $Process.Exited -or $Process.Overflow) { return $result }
        $lines=@($Process.Output.TrimEnd("`r","`n") -split "`r?`n")
        if ($lines.Count -ne 1) { return $result }
        $value=ConvertFrom-LiveJSON $lines[0]
        if ($value.status -cne 'failed' -or -not $value.PSObject.Properties['sandbox_setup']) { return $result }
        Assert-LiveSandboxSetup $value.sandbox_setup
        # Preserve only reviewed constant classifications, never backend error text,
        # paths or configuration payloads. Unknown/missing values remain unknown.
        $failure='unknown'
        $allowedFailures=@('internal_failure','input_invalid','environment_rejected',
            'native_hash_mismatch','version_invalid','process_start_failed','process_ended',
            'protocol_invalid','protocol_limit','protocol_write_failed','native_error',
            'unexpected_server_request','deadline','cleanup_failed','shutdown_failed',
            'scratch_invalid','scratch_not_empty','fixture_invalid','fixture_rejected_request',
            'sandbox_setup_profile_rejected','sandbox_setup_already_used','sandbox_setup_config_rejected',
            'sandbox_setup_file_changed','sandbox_setup_home_mismatch','sandbox_setup_layers_rejected',
            'sandbox_setup_permissions_rejected','sandbox_setup_policy_changed','sandbox_setup_requirements_rejected',
            'sandbox_setup_start_rejected','sandbox_setup_completion_rejected','sandbox_setup_readiness_rejected',
            'sandbox_setup_not_complete',
            'sandbox_setup_approval_rejected','sandbox_setup_config_rpc_failed',
            'sandbox_setup_default_permissions_rejected','sandbox_setup_initial_file_rejected',
            'sandbox_setup_initialize_rpc_failed','sandbox_setup_initialized_send_failed',
            'sandbox_setup_layer_count_rejected','sandbox_setup_layer_disabled',
            'sandbox_setup_layer_metadata_rejected','sandbox_setup_layer_source_rejected',
            'sandbox_setup_peer_start_failed','sandbox_setup_permission_profile_rejected',
            'sandbox_setup_readonly_rejected','sandbox_setup_requirements_rpc_failed',
            'sandbox_setup_session_layer_rejected','sandbox_setup_system_layer_rejected',
            'sandbox_setup_thread_identity_rejected','sandbox_setup_thread_rpc_failed',
            'sandbox_setup_user_content_rejected','sandbox_setup_user_identity_rejected',
            'sandbox_setup_user_layer_missing')
        if ($value.PSObject.Properties['failure'] -and $value.failure -is [string] -and
            $value.failure -cin $allowedFailures) { $failure=$value.failure }
        return [ordered]@{status='failed';failure=$failure;sandbox_setup=$value.sandbox_setup;
            partial_state_possible=$value.sandbox_setup.partial_state_possible;no_retry=$true}
    } catch { return $result }
}

function Assert-PublicFixtureGuardEvent($Record,[string]$SetupProfile='') {
    $kinds=@('fixture_readonly_guard_started','fixture_guard_rejected','fixture_readonly_turn_completed')
    $hasMetadata=[bool]$Record.PSObject.Properties['fixtureReadOnly']
    if ($Record.kind -cnotin $kinds) {
        if ($hasMetadata) { throw 'Misplaced fixture guard evidence.' };return
    }
    if ($SetupProfile -cne 'unelevated-read-only-4239') { throw 'Unexpected fixture guard evidence.' }
    $keys=@('seq','pid','kind')
    if ($Record.kind -ceq 'fixture_guard_rejected') {
        $keys+=@('direction')
        if ($Record.direction -cnotin @('to_bridge','from_bridge')) { throw 'Invalid guard direction.' }
    } elseif ($Record.kind -ceq 'fixture_readonly_turn_completed') {
        $keys+=@('fixtureReadOnly')
        Assert-LiveShape $Record.fixtureReadOnly @('schema','owner','completed','threadTurn')
        Assert-LiveInteger $Record.fixtureReadOnly.schema 1 1
        Assert-LiveInteger $Record.fixtureReadOnly.completed 1 16
        Assert-LiveInteger $Record.fixtureReadOnly.threadTurn 1 16
        if ($Record.fixtureReadOnly.owner -isnot [string] -or
            $Record.fixtureReadOnly.owner -cnotin @('alpha','beta')) { throw 'Invalid guard owner.' }
    }
    Assert-LiveShape $Record $keys
}

function Get-LiveReadonlyConversationEvidence([object[]]$Records) {
    if ($Records.Count -gt 65536) { throw 'Conversation evidence bound exceeded.' }
    $starts=@();$completed=@();$rejected=0
    foreach ($record in $Records) {
        Assert-PublicFixtureGuardEvent $record 'unelevated-read-only-4239'
        switch -CaseSensitive ($record.kind) {
            fixture_readonly_guard_started {$starts+=,$record}
            fixture_guard_rejected {$rejected++}
            fixture_readonly_turn_completed {$completed+=,$record}
        }
    }
    $valid=$starts.Count -eq 1 -and $completed.Count -eq 4 -and $rejected -eq 0
    if ($valid) {
        $previous=$starts[0].seq
        for ($index=0;$index -lt 4;$index++) {
            $event=$completed[$index];$owner=$(if($index -lt 2){'alpha'}else{'beta'});$round=($index % 2)+1
            $valid=$valid -and $event.pid -eq $starts[0].pid -and $event.seq -gt $previous -and
                $event.fixtureReadOnly.owner -ceq $owner -and $event.fixtureReadOnly.completed -eq $round -and
                $event.fixtureReadOnly.threadTurn -eq $round
            $previous=$event.seq
        }
    }
    return [pscustomobject]@{schema=1;status=$(if($valid){'observed'}else{'incomplete'});
        guardStarts=$starts.Count;guardRejections=$rejected;completedTurns=$completed.Count;
        alphaThenBetaTwoTurnsSameOwnedThreads=$valid;
        completionEvents=@($completed | Select-Object -First 8);
        limit='Real correlated completion under the test guard and logical alias ownership; must also match per-backend loopback counts and UI observations. No raw IDs or payloads.'}
}

function Read-LiveGuardActivation([string]$Prefix,[int[]]$ObservedProcessIds) {
    $started=@();$rejected=$false
    foreach ($slot in 1..16) {
        $path=$Prefix+('.{0:D2}.jsonl' -f $slot)
        if (-not (Test-Path -LiteralPath $path)) { continue }
        $info=Get-Item -LiteralPath $path -Force
        if ($info.PSIsContainer -or ($info.Attributes -band [IO.FileAttributes]::ReparsePoint) -or
            $info.Length -lt 1 -or $info.Length -gt 131072) { throw 'Guard activation trace unavailable.' }
        $stream=[IO.File]::Open($path,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::ReadWrite)
        try {
            $bytes=[byte[]]::new(131073);$count=0
            while ($count -lt $bytes.Length) {
                $read=$stream.Read($bytes,$count,$bytes.Length-$count)
                if ($read -eq 0) { break };$count+=$read
            }
        } finally { $stream.Dispose() }
        if ($count -lt 1 -or $count -gt 131072 -or $bytes[$count-1] -ne 10) { throw 'Guard activation snapshot incomplete.' }
        $text=[Text.UTF8Encoding]::new($false,$true).GetString($bytes,0,$count)
        foreach ($line in @($text.TrimEnd("`r","`n") -split "`r?`n")) {
            $record=ConvertFrom-LiveJSON $line 4096
            if ($record.kind -cnotin @('fixture_readonly_guard_started','fixture_guard_rejected')) { continue }
            Assert-PublicFixtureGuardEvent $record 'unelevated-read-only-4239'
            Assert-LiveInteger $record.pid 1 2147483647
            Assert-LiveInteger $record.seq 1 2147483647
            if ($ObservedProcessIds -notcontains [int]$record.pid) { throw 'Guard activation process unobserved.' }
            if ($record.kind -ceq 'fixture_guard_rejected') { $rejected=$true }
            else { $started+=,[int]$record.pid }
        }
    }
    if ($started.Count -ne 1 -or $rejected) { throw 'Live stream guard not uniquely active.' }
    return $started[0]
}

function Assert-LiveGuardProcess($Process,[string]$CreatedUtc,[string]$Executable,[int]$Session) {
    if ($null -eq $Process -or $Process.HasExited -or $Process.SessionId -ne $Session -or
        $Process.StartTime.ToUniversalTime().ToString('o') -cne $CreatedUtc -or
        $Process.MainModule.FileName -ine $Executable) { throw 'Live guard process changed or exited.' }
}

function Get-LiveUnelevatedChatOutcome($Prior,$Chat,$Guard,$Audit,[int]$FileObservations) {
    if ($null -ne $Chat -and $Chat.status -cne 'observed') {
        return [pscustomobject]@{status='blocked';stage='synthetic-chat-'+$Chat.stage;
            reason=$Chat.reason;exitCode=1}
    }
    if ($Prior.exitCode -ne 0) { return $Prior }
    $complete=$null -ne $Chat -and $null -ne $Guard -and $null -ne $Audit
    if ($complete) {
        $complete=$Chat.status -ceq 'observed' -and $Chat.sendAttempts -eq 4 -and $Chat.uiAnswers -eq 4 -and
            $Chat.alphaTurns -eq 2 -and $Chat.betaTurns -eq 2 -and $Chat.newChats -eq 1 -and
            -not $Chat.partialActionUncertain -and -not $Chat.restartAttempted -and
            $Guard.status -ceq 'observed' -and $Audit.status -ceq 'stopped' -and
            $Audit.alpha_requests -eq 2 -and $Audit.beta_requests -eq 2 -and $Audit.fixture_errors -eq 0 -and
            $FileObservations -eq 4
    }
    if (-not $complete) {
        return [pscustomobject]@{status='blocked';stage='synthetic-chat-evidence-incomplete';
            reason='UI, guarded native completion, fixed-file defaults and two isolated loopback request counts did not all agree.';exitCode=1}
    }
    return [pscustomobject]@{status='observed';stage='unelevated-readonly-synthetic-chat-observed';
        reason='Two new provider-owned chats each completed a second turn through official Desktop and the fixed official backend, with local synthetic replies only. Not real accounts, hosted inference, elevated sandbox, Desktop restart or normal-startup acceptance.';exitCode=0}
}
