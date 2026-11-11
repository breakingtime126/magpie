# Dot-source only. Caller owns a just-launched Magpie process in a fresh copied
# package/home with lang=en. This helper never starts, stops or acts on Magpie.
function Invoke-OriginalConnectDiscovery([Diagnostics.Process]$OwnedProcess,[string]$ExpectedExecutable) {
    if ($PSVersionTable.PSEdition -cne 'Core' -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY -cne 'read-only-v1') { throw 'Explicit disposable discovery required.' }
    if ($null -eq $OwnedProcess -or -not [IO.Path]::IsPathFullyQualified($ExpectedExecutable)) { throw 'Owned target required.' }
    $OwnedProcess.Refresh()
    if ($OwnedProcess.HasExited -or $OwnedProcess.MainModule.FileName -ine $ExpectedExecutable -or
        $OwnedProcess.MainWindowHandle -eq [IntPtr]::Zero -or $OwnedProcess.MainWindowTitle -cne 'magpie') { throw 'Owned target unavailable.' }
    $worker=Join-Path $PSScriptRoot 'original-connect-discovery-worker.ps1'
    $start=[Diagnostics.ProcessStartInfo]::new()
    $start.FileName=Join-Path $PSHOME 'pwsh.exe';$start.UseShellExecute=$false;$start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
    $start.Environment.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP','GITHUB_ACTIONS','RUNNER_OS','RUNNER_ENVIRONMENT',
        'GITHUB_REPOSITORY','GITHUB_REF','MAGPIE_ORIGINAL_CONNECT_DISCOVERY')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($null -ne $value) { $start.Environment[$name]=$value }
    }
    foreach ($argument in @('-NoLogo','-NoProfile','-NonInteractive','-File',$worker,
        '-TargetProcessId',[string]$OwnedProcess.Id,'-TargetStartTicks',[string]$OwnedProcess.StartTime.ToUniversalTime().Ticks,
        '-TargetWindow',[string]$OwnedProcess.MainWindowHandle.ToInt64(),'-TargetExecutable',$ExpectedExecutable)) {
        $start.ArgumentList.Add($argument)
    }
    $process=[Diagnostics.Process]::new();$process.StartInfo=$start;$started=$false
    try {
        $started=$process.Start();if (-not $started) { throw 'Discovery worker did not start.' }
        # Bounded incremental reads, including stderr, avoid unbounded ReadToEnd.
        $readers=@($process.StandardOutput,$process.StandardError)
        $buffers=@([char[]]::new(1024),[char[]]::new(1024));$texts=@([Text.StringBuilder]::new(),[Text.StringBuilder]::new())
        $closed=@($false,$false);$reads=@($readers[0].ReadAsync($buffers[0],0,1024),$readers[1].ReadAsync($buffers[1],0,1024))
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while (-not ($closed[0] -and $closed[1] -and $process.HasExited)) {
            if ($watch.ElapsedMilliseconds -gt 15000) { throw 'Discovery worker timed out.' }
            for ($i=0;$i -lt 2;$i++) {
                if (-not $closed[$i] -and $reads[$i].IsCompleted) {
                    $count=$reads[$i].GetAwaiter().GetResult()
                    if ($count -eq 0) { $closed[$i]=$true;continue }
                    if ($texts[$i].Length+$count -gt 8192) { throw 'Discovery output limit.' }
                    [void]$texts[$i].Append($buffers[$i],0,$count)
                    $reads[$i]=$readers[$i].ReadAsync($buffers[$i],0,1024)
                }
            }
            Start-Sleep -Milliseconds 10
        }
        if ($process.ExitCode -ne 0 -or $texts[1].Length -ne 0) { throw 'Discovery worker failed.' }
        $result=$texts[0].ToString() | ConvertFrom-Json -ErrorAction Stop
        if ($result.schema -ne 1 -or $result.scope -cne 'original-magpie-controls-read-only' -or
            $result.actions -ne 0 -or $result.connectVerified -ne $false -or $result.desktopVerified -ne $false -or
            $result.status -cnotin @('blocked','observed') -or $result.reason -cnotin @('context','identity','uia','limit','none')) {
            throw 'Discovery report invalid.'
        }
        return $result
    } finally {
        if ($started -and -not $process.HasExited) {
            # Kill only this helper. Never signal the supplied Magpie PID.
            $process.Kill();if (-not $process.WaitForExit(3000)) { throw 'Discovery worker exit unconfirmed.' }
        }
        $process.Dispose()
    }
}
