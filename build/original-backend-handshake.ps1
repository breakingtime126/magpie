# Fixed metadata only. Importing this file performs no IO and starts no process.
. (Join-Path $PSScriptRoot 'original-connect-preconditions.ps1')
function Get-OriginalHandshakeStages {
    return @('host-listening','host-probe-admitted','host-probe-acknowledged','host-jsonl-admitted',
        'adapter-entered','startup-probe-requested','jsonl-requested','profile-authenticated','endpoint-allocated',
        'probe-result-validated','probe-output-written','probe-failed','jsonl-failed','primary-started','secondary-started',
        'initialize-received','primary-initialize-written','secondary-initialize-written','primary-initialize-reply',
        'secondary-initialize-reply','primary-initialize-error','secondary-initialize-error','initialization-rejected',
        'initialization-ready','initialize-output-written','protocol-failed','stream-write-failed',
        'probe-environment-admitted','probe-environment-rejected')
}
function New-OriginalBackendHandshakeReport {
    return [ordered]@{schema=1;scope='read-only-original-backend-handshake';status='unknown';reason='missing';
        fileCount=0;files=@();hostListeningObserved=$false;hostProbeAdmittedObserved=$false;
        hostProbeAcknowledgedObserved=$false;hostJSONLAdmittedObserved=$false;adapterInvocationObserved=$false;
        initializeOutputWrittenObserved=$false;backendHandshakeVerified=$false;desktopProvenanceVerified=$false;
        changesRecoveryDecision=$false;authorizesAction=$false}
}
function ConvertFrom-OriginalHandshakeLog([byte[]]$Bytes,[string]$Role) {
    $result=[ordered]@{role=$Role;status='unknown';reason='missing';recordCount=0;stages=@();initializeOutputWrittenObserved=$false}
    if ($Role -cnotin @('host','bridge')) { $result.role='unknown';$result.reason='malformed';return $result }
    if ($null -eq $Bytes) { return $result }
    if ($Bytes.Length -eq 0) { $result.reason='empty';return $result }
    if ($Bytes.Length -gt 16384) { $result.reason='limit';return $result }
    try { $text=[Text.UTF8Encoding]::new($false,$true).GetString($Bytes) } catch { $result.reason='malformed';return $result }
    if (-not $text.EndsWith("`n")) { $result.reason='truncated';return $result }
    $lines=$text.Split([char]10)
    if ($lines.Length -gt 65) { $result.reason='limit';return $result }
    $allowed=Get-OriginalHandshakeStages;$stages=[Collections.Generic.List[string]]::new()
    for ($i=0;$i -lt $lines.Length-1;$i++) {
        $line=$lines[$i]
        $match=[regex]::Match($line,'\Anative-backend-handshake-v1 sequence=([1-9][0-9]?) stage=([a-z-]+)\z',[Text.RegularExpressions.RegexOptions]::CultureInvariant)
        if (-not $match.Success) { $result.reason='malformed';return $result }
        $stage=$match.Groups[2].Value
        if ([int]$match.Groups[1].Value -ne $i+1 -or $stage -cnotin $allowed -or $stages.Contains($stage) -or
            ($stage.StartsWith('host-') -ne ($Role -ceq 'host'))) { $result.reason='malformed';return $result }
        $stages.Add($stage)
    }
    $first=if ($Role -ceq 'host') {'host-listening'} else {'adapter-entered'}
    if ($stages.Count -eq 0 -or $stages[0] -cne $first) { $result.reason='incomplete';return $result }
    if ($stages.Contains('initialize-output-written')) {
        # All prerequisites must belong to this one invocation, with actual
        # router readiness before the exact successful client write.
        $ordered=@('adapter-entered','jsonl-requested','profile-authenticated','endpoint-allocated')
        $position=-1
        foreach ($stage in $ordered) {
            $next=$stages.IndexOf($stage)
            if ($next -le $position) { $result.reason='incomplete';return $result }
            $position=$next
        }
        $received=$stages.IndexOf('initialize-received');$ready=$stages.IndexOf('initialization-ready');$written=$stages.IndexOf('initialize-output-written')
        if ($received -le $position -or $ready -le $received -or $written -le $ready) { $result.reason='incomplete';return $result }
        foreach ($roleName in @('primary','secondary')) {
            $started=$stages.IndexOf($roleName+'-started');$sent=$stages.IndexOf($roleName+'-initialize-written');$reply=$stages.IndexOf($roleName+'-initialize-reply')
            if ($started -le $position -or $started -ge $received -or $sent -le $received -or $reply -le $sent -or $reply -ge $ready) { $result.reason='incomplete';return $result }
        }
        if ($stages.Contains('startup-probe-requested') -or $stages.Contains('probe-environment-admitted') -or
            $stages.Contains('probe-environment-rejected')) { $result.reason='malformed';return $result }
        $result.initializeOutputWrittenObserved=$true
    }
    $result.status='observed';$result.reason='none';$result.recordCount=$stages.Count;$result.stages=@($stages.ToArray())
    return $result
}
function Read-OriginalHandshakeFile([string]$Path,[string]$Role,[long]$ProcessStartTicks) {
    $result=ConvertFrom-OriginalHandshakeLog $null $Role;$stream=$null
    try {
        if (-not (Test-OriginalDiagnosticPath $Path $false)) { $result.reason='path';return $result }
        $before=[IO.FileInfo]::new($Path)
        if ($ProcessStartTicks -le 0 -or $before.CreationTimeUtc.Ticks -lt $ProcessStartTicks) { $result.reason='attempt-binding';return $result }
        $creation=$before.CreationTimeUtc.Ticks;$write=$before.LastWriteTimeUtc.Ticks;$length=$before.Length
        if ($length -gt 16384) { $result.reason='limit';return $result }
        $stream=[IO.File]::Open($Path,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::ReadWrite)
        if ($stream.Length -ne $length) { $result.reason='changed';return $result }
        $bytes=[byte[]]::new([int]$length);$offset=0
        while ($offset -lt $bytes.Length) {
            $count=$stream.Read($bytes,$offset,$bytes.Length-$offset)
            if ($count -le 0) { $result.reason='truncated';return $result };$offset+=$count
        }
        if ($stream.ReadByte() -ne -1 -or $stream.Length -ne $length -or -not (Test-OriginalDiagnosticPath $Path $false)) { $result.reason='changed';return $result }
        $after=[IO.FileInfo]::new($Path)
        if ($after.Length -ne $length -or $after.CreationTimeUtc.Ticks -ne $creation -or $after.LastWriteTimeUtc.Ticks -ne $write) { $result.reason='changed';return $result }
        return ConvertFrom-OriginalHandshakeLog $bytes $Role
    } catch [IO.FileNotFoundException] { return $result }
      catch [IO.DirectoryNotFoundException] { return $result }
      catch { $result.reason='read-error';return $result }
    finally { if ($null -ne $stream) { $stream.Dispose() } }
}
function Read-OriginalHandshakeDirectory([string]$OwnedRun,[long]$ProcessStartTicks) {
    $report=New-OriginalBackendHandshakeReport;$invalid=$false
    foreach ($role in @('host','bridge')) {
        for ($slot=1;$slot -le 16;$slot++) {
            $name='native-handshake-{0}.{1:00}.log' -f $role,$slot
            $file=Read-OriginalHandshakeFile ([IO.Path]::Combine($OwnedRun,$name)) $role $ProcessStartTicks
            if ($file.reason -ceq 'missing') { continue }
            $file.slot=$slot;$report.files+=@($file);$report.fileCount++
            if ($file.status -cne 'observed') { $invalid=$true;continue }
            if ($file.initializeOutputWrittenObserved) { $report.initializeOutputWrittenObserved=$true }
            foreach ($pair in @(@('host-listening','hostListeningObserved'),@('host-probe-admitted','hostProbeAdmittedObserved'),@('host-probe-acknowledged','hostProbeAcknowledgedObserved'),@('host-jsonl-admitted','hostJSONLAdmittedObserved'),@('adapter-entered','adapterInvocationObserved'))) {
                if ($file.stages -ccontains $pair[0]) { $report[$pair[1]]=$true }
            }
        }
    }
    if ($invalid) { $report.reason='incomplete' } elseif ($report.fileCount -gt 0) { $report.status='observed';$report.reason='none' }
    return $report
}
function Get-OriginalBackendHandshake {
    [CmdletBinding()]
    param([string]$OwnedRun,[Diagnostics.ProcessStartInfo]$OriginalStartInfo,[long]$ProcessStartTicks,
        $Sequence,[switch]$RecoveryChecksCompleted,[switch]$SequenceReportSaved)
    $report=New-OriginalBackendHandshakeReport
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
            $connect.mode -cne 'Connect' -or $connect.actionsAttempted -ne 1) { $report.reason='attempt-binding';return $report }
        if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { $report.reason='unsupported-platform';return $report }
        if ($OwnedRun -notmatch '\\original-desktop-acceptance-[0-9a-f]{32}\\app\z' -or
            [IO.Path]::GetDirectoryName([IO.Path]::GetDirectoryName($OwnedRun)) -ine $env:RUNNER_TEMP -or
            $null -eq $OriginalStartInfo -or $OriginalStartInfo.FileName -cne [IO.Path]::Combine($OwnedRun,'Magpie-Native-Codex-Experimental.exe') -or
            $OriginalStartInfo.WorkingDirectory -cne $OwnedRun -or $ProcessStartTicks -le 0) { $report.reason='attempt-binding';return $report }
        return Read-OriginalHandshakeDirectory $OwnedRun $ProcessStartTicks
    } catch { $report.reason='read-error';return $report }
}
