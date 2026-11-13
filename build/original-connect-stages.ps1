# Read-only fixed-stage log collector. Importing this file performs no IO.
. (Join-Path $PSScriptRoot 'original-connect-preconditions.ps1')
function Get-OriginalConnectStageNames {
    return @('connect','manager-admission','selection-inspection','supplier-metadata','supplier-freshness',
        'gateway-ownership','runtime-preparation','runtime-homes','runtime-discovery','runtime-executable',
        'runtime-path-resolution','runtime-path-identity','runtime-file-info','runtime-digest','runtime-cancelled',
        'probe-directory','runtime-version','runtime-rediscovery','runtime-revalidation','probe-cleanup',
        'supplier-revalidation','generation-directory','supplier-catalog','bridge-configuration','host-preparation',
        'host-inputs','launcher-resolution','launcher-verification','profile-preparation','host-admission',
        'channel-start','channel-publication','bootstrap-listeners','prepared-plan','controller-validation',
        'selection-lock','selection-read','selection-profile','selection-publication','preparation-cleanup',
        'query-environment','query-start','query-wait','query-deadline','query-cancelled','query-exit-two','query-exit-three','query-exit-nonzero','query-wait-failure','query-output-bound','query-json','query-identity','query-root-running','query-root-exited','query-root-unknown',
        'query-progress-unknown','query-progress-entry','query-progress-setup','query-progress-appx-import','query-progress-appx-return','query-progress-appx','query-progress-management',
        'query-progress-utility','query-progress-packages','query-progress-path','query-progress-json')
}
function New-OriginalConnectStageReport {
    return [ordered]@{schema=1;scope='read-only-original-connect-stages';status='unknown';reason='missing';
        attemptOutcome='unknown';lastEnteredStage='unknown';firstFailedStage='unknown';recordCount=0;stages=$null;
        changesRecoveryDecision=$false;authorizesAction=$false}
}
function ConvertFrom-OriginalConnectStageLog([byte[]]$Bytes) {
    $report=New-OriginalConnectStageReport
    if ($null -eq $Bytes -or $Bytes.Length -eq 0) { return $report }
    if ($Bytes.Length -gt 1048576) { $report.reason='limit';return $report }
    try { $text=[Text.UTF8Encoding]::new($false,$true).GetString($Bytes) }
    catch { $report.reason='malformed';return $report }
    if (-not $text.EndsWith("`n")) { $report.reason='truncated';return $report }
    $lines=$text.Split([char]10)
    if ($lines.Length -gt 4097) { $report.reason='limit';return $report }
    $names=Get-OriginalConnectStageNames
    $counts=[ordered]@{}
    foreach ($name in $names) { $counts[$name]=[ordered]@{entered=0;completed=0;failed=0} }
    $stack=[Collections.Generic.List[string]]::new();$records=0;$finished=$false;$lastEntered='unknown';$terminal='unknown';$firstFailed='unknown'
    $pattern='\A[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} native-connect-preparation-v1 attempt=([1-9][0-9]{0,19}) sequence=([1-9][0-9]{0,2}) stage=([a-z-]+) outcome=(entered|completed|failed)\z'
    foreach ($rawLine in $lines) {
        $line=if ($rawLine.EndsWith("`r")) { $rawLine.Substring(0,$rawLine.Length-1) } else { $rawLine }
        if ($line.Length -gt 4096) { $report.reason='limit';return $report }
        if ($line.StartsWith('applog: app.log ')) { $report.reason='rotation';return $report }
        # Non-protocol app text is neither interpreted nor returned. A malformed
        # protocol-looking line invalidates the whole snapshot, not just that line.
        if (-not $line.Contains('native-connect-preparation')) { continue }
        $match=[regex]::Match($line,$pattern,[Text.RegularExpressions.RegexOptions]::CultureInvariant)
        if (-not $match.Success) { $report.reason='malformed';return $report }
        $stamp=[datetime]::MinValue
        if (-not [datetime]::TryParseExact($line.Substring(0,19),'yyyy/MM/dd HH:mm:ss',[Globalization.CultureInfo]::InvariantCulture,[Globalization.DateTimeStyles]::None,[ref]$stamp)) { $report.reason='malformed';return $report }
        $attempt=[uint64]0
        if (-not [uint64]::TryParse($match.Groups[1].Value,[ref]$attempt)) { $report.reason='malformed';return $report }
        if ($attempt -ne 1) { $report.reason=if ($records -gt 0) {'multiple-attempts'} else {'attempt-binding'};return $report }
        if ($finished) { $report.reason='multiple-attempts';return $report }
        $sequence=[int]$match.Groups[2].Value;$stage=$match.Groups[3].Value;$outcome=$match.Groups[4].Value
        if ($stage -cnotin $names -or $sequence -ne $records+1) { $report.reason='malformed';return $report }
        $records++
        if ($records -gt 128) { $report.reason='limit';return $report }
        if ($records -eq 1 -and ($stage -cne 'connect' -or $outcome -cne 'entered')) { $report.reason='malformed';return $report }
        if ($stage -ceq 'connect' -and $records -gt 1 -and $outcome -ceq 'entered') { $report.reason='multiple-attempts';return $report }
        if ($outcome -ceq 'entered') {
            $stack.Add($stage);$lastEntered=$stage
        } else {
            if ($stack.Count -eq 0 -or $stack[$stack.Count-1] -cne $stage) { $report.reason='malformed';return $report }
            $stack.RemoveAt($stack.Count-1)
            if ($stage -ceq 'connect') { $finished=$true;$terminal=$outcome }
        }
        if ($outcome -ceq 'failed' -and $firstFailed -ceq 'unknown') { $firstFailed=$stage }
        $counts[$stage][$outcome]++
    }
    if ($records -eq 0) { return $report }
    if (-not $finished -or $stack.Count -ne 0) {
        # An entered stage without its terminal event is unknown, never failed.
        $report.reason='incomplete';$report.lastEnteredStage=$lastEntered;return $report
    }
    $report.status='complete';$report.reason='none';$report.attemptOutcome=$terminal
    $report.recordCount=$records;$report.lastEnteredStage=$lastEntered;$report.firstFailedStage=$firstFailed;$report.stages=$counts
    return $report
}
function Test-OriginalConnectStageRotation([string]$OwnedRun) {
    # Metadata only; never open or export the previous log. A fresh fixture
    # should not have one, and a rotation cannot establish a complete history.
    $oldPath=[IO.Path]::Combine($OwnedRun,'data\logs\app.old.log')
    try {
        $null=[IO.File]::GetAttributes($oldPath)
        return $true
    } catch [IO.FileNotFoundException] { return $false }
      catch [IO.DirectoryNotFoundException] { return $false }
}
function Read-OriginalConnectStageLog([string]$OwnedRun,[long]$ProcessStartTicks) {
    $report=New-OriginalConnectStageReport;$stream=$null
    try {
        $logPath=[IO.Path]::Combine($OwnedRun,'data\logs\app.log')
        if (-not (Test-OriginalDiagnosticPath $logPath $false)) { $report.reason='path';return $report }
        if (Test-OriginalConnectStageRotation $OwnedRun) { $report.reason='rotation';return $report }
        $before=[IO.FileInfo]::new($logPath)
        if ($ProcessStartTicks -le 0 -or $before.CreationTimeUtc.Ticks -lt $ProcessStartTicks) { $report.reason='attempt-binding';return $report }
        $creation=$before.CreationTimeUtc.Ticks;$write=$before.LastWriteTimeUtc.Ticks;$length=$before.Length
        if ($length -gt 1048576) { $report.reason='limit';return $report }
        # No delete sharing: an ordinary log rotation cannot replace this open
        # file on Windows. This is not an adversarial same-user atomic snapshot.
        $stream=[IO.File]::Open($logPath,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::ReadWrite)
        if ($stream.Length -ne $length) { $report.reason='changed';return $report }
        $bytes=[byte[]]::new([int]$length);$offset=0
        while ($offset -lt $bytes.Length) {
            $count=$stream.Read($bytes,$offset,$bytes.Length-$offset)
            if ($count -le 0) { $report.reason='truncated';return $report }
            $offset+=$count
        }
        if ($stream.ReadByte() -ne -1 -or $stream.Length -ne $length) { $report.reason='changed';return $report }
        if (-not (Test-OriginalDiagnosticPath $logPath $false)) { $report.reason='path';return $report }
        if (Test-OriginalConnectStageRotation $OwnedRun) { $report.reason='rotation';return $report }
        $after=[IO.FileInfo]::new($logPath)
        if ($after.Length -ne $length -or $after.CreationTimeUtc.Ticks -ne $creation -or $after.LastWriteTimeUtc.Ticks -ne $write) {
            $report.reason='changed';return $report
        }
        return ConvertFrom-OriginalConnectStageLog $bytes
    } catch [IO.FileNotFoundException] { return $report }
      catch [IO.DirectoryNotFoundException] { return $report }
      catch { $report.reason='read-error';return $report }
    finally { if ($null -ne $stream) { $stream.Dispose() } }
}
function Get-OriginalConnectStages {
    [CmdletBinding()]
    param([string]$OwnedRun,[Diagnostics.ProcessStartInfo]$OriginalStartInfo,[long]$ProcessStartTicks,
        $Sequence,[switch]$RecoveryChecksCompleted,[switch]$SequenceReportSaved)
    $report=New-OriginalConnectStageReport
    try {
        if (-not $RecoveryChecksCompleted -or -not $SequenceReportSaved -or
            $Sequence -isnot [Collections.IDictionary] -or $Sequence['cleanupAllowed'] -isnot [bool] -or
            $Sequence['preserveRecovery'] -isnot [bool] -or $null -eq $Sequence['connect']) {
            $report.reason='sequence-evidence-required';return $report
        }
        $connect=$Sequence['connect']
        if (($connect.schema -isnot [int] -and $connect.schema -isnot [long]) -or
            ($connect.actionsAttempted -isnot [int] -and $connect.actionsAttempted -isnot [long]) -or
            $connect.schema -ne 1 -or $connect.scope -cne 'original-magpie-ui-action' -or
            $connect.mode -cne 'Connect' -or $connect.actionsAttempted -ne 1) {
            $report.reason='attempt-binding';return $report
        }
        if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { $report.reason='unsupported-platform';return $report }
        if ($OwnedRun -notmatch '\\original-desktop-acceptance-[0-9a-f]{32}\\app\z' -or
            [IO.Path]::GetDirectoryName([IO.Path]::GetDirectoryName($OwnedRun)) -ine $env:RUNNER_TEMP -or
            $null -eq $OriginalStartInfo -or $OriginalStartInfo.FileName -cne [IO.Path]::Combine($OwnedRun,'Magpie-Native-Codex-Experimental.exe') -or
            $OriginalStartInfo.WorkingDirectory -cne $OwnedRun -or $ProcessStartTicks -le 0) {
            $report.reason='attempt-binding';return $report
        }
        return Read-OriginalConnectStageLog $OwnedRun $ProcessStartTicks
    } catch { $report.reason='read-error';return $report }
}
