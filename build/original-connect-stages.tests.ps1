[CmdletBinding()]
param([string]$Implementation='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if ($Implementation -ceq '') { $Implementation=Join-Path $PSScriptRoot 'original-connect-stages.ps1' }
. $Implementation
function Assert([bool]$Value,[string]$Message) { if (-not $Value) { throw ('FAIL: '+$Message) } }
function Bytes([string]$Text) { return ,([Text.UTF8Encoding]::new($false).GetBytes($Text)) }
function Line([int]$Number,[string]$Stage,[string]$Outcome,[string]$Attempt='1') {
    return ('2026/11/11 12:00:00 native-connect-preparation-v1 attempt='+$Attempt+' sequence='+$Number+' stage='+$Stage+' outcome='+$Outcome+"`n")
}
function Unknown([string]$Text,[string]$Reason) {
    $r=ConvertFrom-OriginalConnectStageLog (Bytes $Text)
    Assert ($r.status -ceq 'unknown' -and $r.reason -ceq $Reason -and $r.attemptOutcome -ceq 'unknown' -and $r.firstFailedStage -ceq 'unknown' -and $null -eq $r.stages) ('unknown '+$Reason+' at fixture line '+$MyInvocation.ScriptLineNumber+' observed '+$r.reason)
    Assert (-not $r.changesRecoveryDecision -and -not $r.authorizesAction) 'unknown has no authority'
}
$valid=(Line 1 connect entered)+(Line 2 runtime-discovery entered)+(Line 3 runtime-discovery entered)+
    (Line 4 runtime-discovery completed)+(Line 5 runtime-discovery failed)+(Line 6 connect failed)
$r=ConvertFrom-OriginalConnectStageLog (Bytes $valid)
Assert ($r.status -ceq 'complete' -and $r.attemptOutcome -ceq 'failed' -and $r.recordCount -eq 6) 'complete failed attempt'
Assert ($r.stages['runtime-discovery'].entered -eq 2 -and $r.stages['runtime-discovery'].completed -eq 1 -and $r.stages['runtime-discovery'].failed -eq 1) 'same-stage nesting counts'
Assert (-not $r.changesRecoveryDecision -and -not $r.authorizesAction) 'complete report has no authority'
# Actual offline Go observer execution, with only the date/time prefix normalized.
$replay=[IO.File]::ReadAllBytes((Join-Path $PSScriptRoot 'testdata/original-connect-stages/version-cleanup.log'))
$observed=ConvertFrom-OriginalConnectStageLog $replay
Assert ($observed.status -ceq 'complete' -and $observed.recordCount -eq 16 -and $observed.attemptOutcome -ceq 'failed' -and $observed.firstFailedStage -ceq 'runtime-version') 'actual Go observer replay binding'
Assert ($observed.stages['runtime-discovery'].entered -eq 2 -and $observed.stages['runtime-discovery'].completed -eq 2) 'actual nested discovery replay'
Assert ($observed.stages['runtime-version'].failed -eq 1 -and $observed.stages['probe-cleanup'].failed -eq 1 -and
    $observed.stages['probe-directory'].completed -eq 1 -and $observed.stages.connect.failed -eq 1) 'actual primary failure and separate cleanup replay'
# Actual new inert Go query runner trace; no PowerShell/native query was run.
$queryReplay=[IO.File]::ReadAllBytes((Join-Path $PSScriptRoot 'testdata/original-connect-stages/query-wait-failure.log'))
$queryObserved=ConvertFrom-OriginalConnectStageLog $queryReplay
Assert ($queryObserved.status -ceq 'complete' -and $queryObserved.recordCount -eq 8 -and
    $queryObserved.firstFailedStage -ceq 'query-wait-failure' -and $queryObserved.attemptOutcome -ceq 'failed') 'actual original-query observer replay'
Assert ($queryObserved.stages['query-start'].completed -eq 1 -and $queryObserved.stages['query-wait'].failed -eq 1 -and
    $queryObserved.stages['query-wait-failure'].failed -eq 1) 'query nested branch ordering'
foreach ($queryStage in @('query-environment','query-start','query-wait','query-deadline','query-cancelled',
    'query-exit-two','query-exit-three','query-exit-nonzero','query-wait-failure','query-output-bound','query-json','query-identity')) {
    $queryTrace=(Line 1 connect entered)+(Line 2 $queryStage entered)+(Line 3 $queryStage failed)+(Line 4 connect failed)
    $queryReport=ConvertFrom-OriginalConnectStageLog (Bytes $queryTrace)
    Assert ($queryReport.status -ceq 'complete' -and $queryReport.firstFailedStage -ceq $queryStage -and
        -not $queryReport.changesRecoveryDecision -and -not $queryReport.authorizesAction) 'fixed query stage allowlist'
}
# Observation is a completed fixed snapshot, never the original failure.
foreach ($rootStage in @('query-root-running','query-root-exited','query-root-unknown')) {
    $trace=(Line 1 connect entered)+(Line 2 query-wait entered)+(Line 3 $rootStage entered)+
        (Line 4 $rootStage completed)+(Line 5 query-deadline entered)+(Line 6 query-deadline failed)+
        (Line 7 query-wait failed)+(Line 8 connect failed)
    $rootReport=ConvertFrom-OriginalConnectStageLog (Bytes $trace)
    Assert ($rootReport.status -ceq 'complete' -and $rootReport.firstFailedStage -ceq 'query-deadline' -and
        $rootReport.stages[$rootStage].completed -eq 1 -and $rootReport.stages[$rootStage].failed -eq 0 -and
        -not $rootReport.changesRecoveryDecision -and -not $rootReport.authorizesAction) 'root snapshot preserves deadline failure and no authority'
}
# Progress snapshots remain completed observations, including unknown, and
# neither overtake the original failure nor authorize recovery or success.
foreach ($progressStage in @('query-progress-unknown','query-progress-entry','query-progress-setup','query-progress-appx-import','query-progress-appx-return','query-progress-appx',
    'query-progress-management','query-progress-utility','query-progress-packages','query-progress-path','query-progress-json')) {
    foreach ($failure in @('query-deadline','query-cancelled')) {
        $trace=(Line 1 connect entered)+(Line 2 query-wait entered)+(Line 3 $progressStage entered)+
            (Line 4 $progressStage completed)+(Line 5 query-root-running entered)+(Line 6 query-root-running completed)+
            (Line 7 $failure entered)+(Line 8 $failure failed)+(Line 9 query-wait failed)+(Line 10 connect failed)
        $progressReport=ConvertFrom-OriginalConnectStageLog (Bytes $trace)
        Assert ($progressReport.status -ceq 'complete' -and $progressReport.firstFailedStage -ceq $failure -and
            $progressReport.attemptOutcome -ceq 'failed' -and $progressReport.stages[$progressStage].completed -eq 1 -and
            $progressReport.stages[$progressStage].failed -eq 0 -and -not $progressReport.changesRecoveryDecision -and
            -not $progressReport.authorizesAction) 'progress snapshot preserves original failure and no authority'
    }
}
Unknown ((Line 1 connect entered)+(Line 2 query-progress-private entered)+(Line 3 query-progress-private completed)+(Line 4 connect failed)) malformed
Unknown ((Line 1 connect entered)+(Line 2 query-progress-json entered)) incomplete
$success=(Line 1 connect entered)+(Line 2 connect completed)
Assert ((ConvertFrom-OriginalConnectStageLog (Bytes $success)).firstFailedStage -ceq 'unknown') 'no invented failure on success'
Assert ((ConvertFrom-OriginalConnectStageLog (Bytes $success)).attemptOutcome -ceq 'completed') 'complete success is observer outcome only'
Unknown '' missing
Unknown ($success+"applog: app.log couldn't be kept as app.old.log (PRIVATE_CANARY): emptied instead`n") rotation
Unknown "unrelated PRIVATE_KEY_CANARY C:\private\path`n" missing
Unknown ($success.TrimEnd([char]10)) truncated
Unknown ((Line 1 connect entered)+(Line 2 runtime-version entered)) incomplete
$partial=ConvertFrom-OriginalConnectStageLog (Bytes ((Line 1 connect entered)+(Line 2 runtime-version entered)))
Assert ($partial.lastEnteredStage -ceq 'runtime-version' -and $null -eq $partial.stages -and $partial.attemptOutcome -ceq 'unknown') 'entered is unknown not failed'
Unknown ((Line 1 connect entered)+(Line 3 connect failed)) malformed
Unknown ((Line 1 connect entered)+(Line 2 runtime-version entered)+(Line 3 connect failed)) malformed
Unknown ((Line 1 connect entered)+(Line 2 connect entered)) multiple-attempts
Unknown ($success+(Line 1 connect entered '2')) multiple-attempts
Unknown ((Line 1 connect entered '2')+(Line 2 connect failed '2')) attempt-binding
Unknown ($success+$success) multiple-attempts
Unknown ($success.Replace('stage=connect','stage=private-model')) malformed
Unknown ($success.Replace('outcome=completed','outcome=unknown')) malformed
Unknown ($success.Replace('outcome=completed','outcome=completed PRIVATE_CANARY')) malformed
Unknown ($success.Replace('attempt=1','attempt=01')) malformed
Unknown ($success.Replace('attempt=1','attempt=18446744073709551616')) malformed
Unknown ($success.Replace('2026/11/11','2026/99/99')) malformed
Unknown ($success.Replace('preparation-v1','preparation-v2')) malformed
Unknown ($success.Replace("`n","`r`r`n")) malformed
Unknown ($success+'prefix native-connect-preparation-v1 PRIVATE_CANARY'+"`n") malformed
Unknown (($success)+'X'*4097+"`n") limit
Unknown ("`n"*4097) limit
Unknown ('X'*1048577) limit
$over=Line 1 connect entered
for ($i=2;$i -le 129;$i++) { $over+=Line $i runtime-discovery entered }
Unknown $over limit
$r=ConvertFrom-OriginalConnectStageLog ([byte[]]@(255,10))
Assert ($r.status -ceq 'unknown' -and $r.reason -ceq 'malformed') 'invalid UTF8 is unknown'
$r=ConvertFrom-OriginalConnectStageLog (Bytes ("PRIVATE_KEY_CANARY C:\private\path`n"+$valid))
$json=$r|ConvertTo-Json -Depth 6 -Compress
Assert ($json -notmatch 'PRIVATE_KEY_CANARY|private.path|2026/11|attempt=|sequence=') 'no raw log metadata'
$seq=@{cleanupAllowed=$false;preserveRecovery=$true;connect=@{schema=1;scope='original-magpie-ui-action';mode='Connect';actionsAttempted=1}}
$before=$seq|ConvertTo-Json -Depth 5 -Compress
foreach ($flags in @(@{},@{RecoveryChecksCompleted=$true},@{SequenceReportSaved=$true})) {
    $r=Get-OriginalConnectStages -Sequence $seq @flags
    Assert ($r.reason -ceq 'sequence-evidence-required') 'requires completed recovery and saved sequence'
}
$seq.connect.actionsAttempted=-1
$r=Get-OriginalConnectStages -Sequence $seq -RecoveryChecksCompleted -SequenceReportSaved
Assert ($r.reason -ceq 'attempt-binding') 'unknown UI dispatch cannot bind log'
$seq.connect.actionsAttempted=1
Assert (($seq|ConvertTo-Json -Depth 5 -Compress) -ceq $before) 'collector cannot modify recovery'
$tokens=$null;$errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($Implementation,[ref]$tokens,[ref]$errors)
Assert ($errors.Count -eq 0) 'collector syntax'
$source=$ast.Extent.Text
foreach ($required in @('1048576','4097','4096','$records -gt 128','Test-OriginalDiagnosticPath $logPath $false',"'data\logs\app.log'",'[IO.FileAccess]::Read','[IO.FileShare]::ReadWrite')) {
    Assert ($source.Contains($required)) ('bounded exact read '+$required)
}
Assert ($source -notmatch '::(WriteAllText|WriteAllBytes|CreateDirectory|Delete|Start)\s*\(') 'collector does not mutate or launch'
$caller=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1'))
$callAt=$caller.IndexOf('$report.connectStages=Get-OriginalConnectStages')
$sequenceAt=$caller.IndexOf('$report.sequence=Invoke-OriginalDesktopSequence')
$savedAt=$caller.IndexOf('Save-AcceptanceReport', $caller.IndexOf('$report.preserveRecovery=-not $safeCleanup',$sequenceAt))
Assert ($callAt -gt $savedAt -and $savedAt -gt $sequenceAt) 'collector after recovery result and saved report'
Assert ($caller.Contains('-Sequence $report.sequence -RecoveryChecksCompleted -SequenceReportSaved')) 'actual sequence passed to collector'
Assert ([regex]::Matches($caller,'Get-OriginalConnectStages -OwnedRun').Count -eq 1) 'one diagnostic collection only'
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-codex-connectivity-preview.yml'))
Assert ($workflow.Contains('& $hostName -NoProfile -NonInteractive -File build/original-connect-stages.tests.ps1') -and
    $workflow.Contains('& $hostName -NoProfile -NonInteractive -File build/original-connect-stages.mutations.ps1')) 'both-host fixture gates'
Assert ($workflow.IndexOf('build/original-connect-stages.tests.ps1') -lt $workflow.IndexOf('Build original portable GUI')) 'fixtures precede live work'
$appLog=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../internal/applog/applog.go'))
$guiEntry=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../internal/cli/gui_on.go'))
Assert ($appLog.Contains('log.SetOutput(io.MultiWriter(quiet{os.Stderr}, w))') -and $appLog -notmatch 'log\.Set(Flags|Prefix)\(') 'production app logger preserves standard prefix'
Assert ($guiEntry.Contains('startLog("app")') -and $guiEntry.Contains('applog.Start(version, how)')) 'original GUI starts file logger'
$packager=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'package-native-magpie-windows.ps1'))
foreach ($gate in @('./internal/nativeprepdiag', '^(TestRuntimePreparationDiagnosticsObserveOriginalCalls|TestInstalledQueryBranches|TestInstalledQuerySourceContract)$', '^TestOriginalConnectDiagnosticsBoundToOriginalInvocation$', '^TestInstalledQueryRoot')) {
    Assert ($packager.IndexOf($gate) -ge 0 -and $packager.IndexOf($gate) -lt $packager.IndexOf('& go run github.com/tc-hib/go-winres')) 'offline Windows observer gates precede packaging'
}
Write-Output 'PASS strict protocol, nesting, bounds, partial unknown, redaction, immutable recovery and caller ordering'
if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) {
    $fixture=Join-Path ([IO.Path]::GetTempPath()) ('connect-stage-fixture-'+[guid]::NewGuid().ToString('N'))
    $oldRunnerTemp=$env:RUNNER_TEMP;$lock=$null;$junction=$false
    try {
        $env:RUNNER_TEMP=$fixture
        $run=Join-Path $fixture ('original-desktop-acceptance-'+[guid]::NewGuid().ToString('N')+'\app')
        $logs=Join-Path $run 'data\logs';$logPath=Join-Path $logs 'app.log'
        $startTicks=[datetime]::UtcNow.AddMinutes(-1).Ticks
        [void][IO.Directory]::CreateDirectory($logs)
        $start=[Diagnostics.ProcessStartInfo]::new();$start.FileName=Join-Path $run 'Magpie-Native-Codex-Experimental.exe';$start.WorkingDirectory=$run
        $argsMap=@{OwnedRun=$run;OriginalStartInfo=$start;ProcessStartTicks=$startTicks;Sequence=$seq;RecoveryChecksCompleted=$true;SequenceReportSaved=$true}
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.status -ceq 'unknown' -and $r.reason -ceq 'missing') 'missing log is unknown'
        [IO.File]::WriteAllText($logPath,"SECRET_CANARY_NO_EXPORT`n"+$valid,[Text.UTF8Encoding]::new($false))
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.status -ceq 'complete' -and $r.attemptOutcome -ceq 'failed') 'exact owned regular file collection'
        Assert (($r|ConvertTo-Json -Depth 6 -Compress) -notmatch 'SECRET_CANARY|connect-stage-fixture|app.log|2026/11') 'file collector redacts raw data'
        $oldLog=Join-Path $logs 'app.old.log';[IO.File]::WriteAllText($oldLog,'PRIVATE_OLD_LOG_NOT_READ')
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.reason -ceq 'rotation' -and $r.status -ceq 'unknown') 'rotated history stays unknown'
        [IO.File]::Delete($oldLog)
        $lock=[IO.File]::Open($logPath,[IO.FileMode]::Open,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None)
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.status -ceq 'unknown' -and $r.reason -ceq 'read-error') 'read denial remains unknown'
        $lock.Dispose();$lock=$null
        [IO.File]::SetCreationTimeUtc($logPath,[datetime]::new($startTicks).AddMinutes(-1))
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.reason -ceq 'attempt-binding') 'copied older log refused'
        [IO.File]::SetCreationTimeUtc($logPath,[datetime]::UtcNow)
        [IO.File]::WriteAllText($logPath,('X'*1048577))
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.reason -ceq 'limit') 'oversized file refused before parse'
        [IO.File]::WriteAllText($logPath,$valid,[Text.UTF8Encoding]::new($false))
        $originalLogs=Join-Path $run 'data\logs-original';[IO.Directory]::Move($logs,$originalLogs)
        New-Item -ItemType Junction -Path $logs -Target $originalLogs -ErrorAction Stop | Out-Null;$junction=$true
        $r=Get-OriginalConnectStages @argsMap
        Assert ($r.status -ceq 'unknown' -and $r.reason -ceq 'path') 'reparse ancestor never followed'
        [IO.Directory]::Delete($logs);$junction=$false
        Assert ([IO.File]::Exists((Join-Path $originalLogs 'app.log'))) 'junction cleanup preserves target'
        Assert (($seq|ConvertTo-Json -Depth 5 -Compress) -ceq $before) 'filesystem diagnostics preserve recovery'
        Write-Output 'PASS Windows exact owned file, missing/read-denied/stale/oversized/reparse fixtures'
    } finally {
        if ($null -ne $lock) { $lock.Dispose() }
        if ($junction) { [IO.Directory]::Delete($logs) }
        if ([IO.Directory]::Exists($fixture)) { [IO.Directory]::Delete($fixture,$true) }
        $env:RUNNER_TEMP=$oldRunnerTemp
    }
} else { Write-Output 'NOT RUN Windows filesystem fixtures (non-Windows host)' }
