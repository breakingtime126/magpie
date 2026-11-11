# Desktop/Core caller; the owned Magpie handle stays with the caller. All UIA
# work runs in the fixed Core worker, so blocking calls have a hard deadline.
. (Join-Path $PSScriptRoot 'original-connect-action-core.ps1')
function New-OriginalConnectSnapshot([string]$Path) {
    # No registry mutation and no launch. The strict Core worker parses it again.
    $selection=Read-OriginalSelection
    Write-OriginalExclusive $Path ($selection | ConvertTo-Json -Depth 4 -Compress)
}
function Invoke-OriginalConnectAction {
    [CmdletBinding()]
    param([ValidateSet('Connect','Restore')][string]$Mode,[Diagnostics.Process]$OwnedProcess,
        [string]$ExpectedExecutable,[string]$ExpectedLauncher,[string]$JournalPath,
        [string]$SnapshotPath,[string]$AttemptDirectory)
    if ($Mode -cnotin @('Connect','Restore')) { throw 'Exact action mode required.' }
    if ($env:GITHUB_ACTIONS -cne 'true' -or $env:GITHUB_EVENT_NAME -cne 'push' -or $env:RUNNER_OS -cne 'Windows' -or
        $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_CONNECT_ACTION -cne 'original-ui-v1') { throw 'Explicit disposable action required.' }
    foreach ($path in @($ExpectedExecutable,$ExpectedLauncher,$JournalPath,$SnapshotPath,$AttemptDirectory)) { Assert-OriginalRegularPath $path }
    $OwnedProcess.Refresh()
    if ($OwnedProcess.HasExited -or $OwnedProcess.MainModule.FileName -cne $ExpectedExecutable -or
        $OwnedProcess.MainWindowHandle -eq [IntPtr]::Zero -or $OwnedProcess.MainWindowTitle -cne 'magpie') { throw 'Owned target unavailable.' }
    $worker=Join-Path $PSScriptRoot 'original-connect-action-worker.ps1'
    $core=Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'PowerShell\7\pwsh.exe'
    Assert-OriginalRegularPath $core
    if (-not [IO.File]::Exists($core)) { throw 'Installed PowerShell Core unavailable.' }
    $start=[Diagnostics.ProcessStartInfo]::new();$start.FileName=$core
    $start.UseShellExecute=$false;$start.CreateNoWindow=$true;$start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP','GITHUB_ACTIONS','RUNNER_OS','RUNNER_ENVIRONMENT',
        'GITHUB_REPOSITORY','GITHUB_REF','GITHUB_EVENT_NAME','MAGPIE_ORIGINAL_CONNECT_ACTION')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($null -ne $value) { $start.EnvironmentVariables[$name]=$value }
    }
    $arguments=@('-NoLogo','-NoProfile','-NonInteractive','-File',$worker,'-Mode',$Mode,
        '-TargetProcessId',[string]$OwnedProcess.Id,'-TargetStartTicks',[string]$OwnedProcess.StartTime.ToUniversalTime().Ticks,
        '-TargetWindow',[string]$OwnedProcess.MainWindowHandle.ToInt64(),'-TargetExecutable',$ExpectedExecutable,
        '-ExpectedLauncher',$ExpectedLauncher,'-JournalPath',$JournalPath,'-SnapshotPath',$SnapshotPath,'-AttemptDirectory',$AttemptDirectory)
    # Inputs are canonical paths or bounded fixed words/numbers. Refuse quotes,
    # newline and trailing slash rather than inventing a shell quoting fallback.
    $quoted=foreach ($argument in $arguments) {
        if ($argument.Contains('"') -or $argument.Contains("`r") -or $argument.Contains("`n") -or $argument.EndsWith('\')) { throw 'Unsafe worker argument.' }
        '"'+$argument+'"'
    }
    $start.Arguments=$quoted -join ' '
    $process=[Diagnostics.Process]::new();$process.StartInfo=$start;$started=$false;$startAttempted=$false
    $result=[ordered]@{schema=1;scope='original-magpie-ui-action';mode=$Mode;status='blocked';stage='supervisor';
        actionsAttempted=0;selectionVerified=$false;restorationVerified=$false;desktopVerified=$false;preserveRecovery=$true}
    try {
        # After process start, loss of output is uncertain regardless of whether
        # a marker has become visible. Never infer zero actions from a timeout.
        $result.status='uncertain';$result.actionsAttempted=-1
        $startAttempted=$true
        $started=$process.Start()
        if (-not $started) {
            $result.status='blocked';$result.actionsAttempted=0
            throw 'Worker did not start.'
        }
        $readers=@($process.StandardOutput,$process.StandardError)
        $buffers=@([char[]]::new(1024),[char[]]::new(1024));$texts=@([Text.StringBuilder]::new(),[Text.StringBuilder]::new())
        $closed=@($false,$false);$reads=@($readers[0].ReadAsync($buffers[0],0,1024),$readers[1].ReadAsync($buffers[1],0,1024))
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while (-not ($closed[0] -and $closed[1] -and $process.HasExited)) {
            if ($watch.ElapsedMilliseconds -ge 60000) { throw 'Worker timed out.' }
            for ($i=0;$i -lt 2;$i++) {
                if (-not $closed[$i] -and $reads[$i].IsCompleted) {
                    $count=$reads[$i].GetAwaiter().GetResult()
                    if ($count -eq 0) { $closed[$i]=$true;continue }
                    if ($texts[$i].Length+$count -gt 8192) { throw 'Worker output limit.' }
                    [void]$texts[$i].Append($buffers[$i],0,$count)
                    $reads[$i]=$readers[$i].ReadAsync($buffers[$i],0,1024)
                }
            }
            Start-Sleep -Milliseconds 10
        }
        if ($process.ExitCode -ne 0 -or $texts[1].Length -ne 0) { throw 'Worker failed.' }
        $r=$texts[0].ToString() | ConvertFrom-Json -ErrorAction Stop
        if ($r.schema -ne 1 -or $r.scope -cne 'original-magpie-ui-action' -or $r.mode -cne $Mode -or
            $r.status -cnotin @('blocked','uncertain','verified') -or $r.desktopVerified -ne $false -or
            $r.actionsAttempted -lt 0 -or $r.actionsAttempted -gt 2 -or
            $r.stage -cnotin @('context','claim','identity','precondition','configure','selection-readback','restore-dialog','restore-confirm','restoration-readback','complete')) { throw 'Worker report invalid.' }
        if ($r.status -ceq 'verified' -and ($r.stage -cne 'complete' -or
            ($Mode -ceq 'Connect' -and ($r.selectionVerified -ne $true -or $r.restorationVerified -ne $false -or $r.actionsAttempted -ne 1)) -or
            ($Mode -ceq 'Restore' -and ($r.restorationVerified -ne $true -or $r.selectionVerified -ne $false -or $r.actionsAttempted -ne 2)))) { throw 'Worker success invalid.' }
        $result=$r
    } catch { } finally {
        if ($started -or ($startAttempted -and $result.status -ceq 'uncertain')) {
            try {
                # A Start exception can still leave an associated owned handle.
                # Inspect only that object, never find a replacement by PID/name.
                if (-not $process.HasExited) {
                    $process.Kill()
                    if (-not $process.WaitForExit(3000)) { throw 'Worker exit unconfirmed.' }
                }
            } catch {
                $result.status='uncertain';$result.stage='supervisor';$result.preserveRecovery=$true
                $result.selectionVerified=$false;$result.restorationVerified=$false
            }
        }
        $process.Dispose()
    }
    return $result
}
