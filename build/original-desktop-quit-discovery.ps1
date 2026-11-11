# Dot-source only. Caller owns a just-launched Magpie process in a fresh copied
# package/home with lang=en. This helper never starts, stops or acts on Magpie.
function Invoke-OriginalQuitDiscovery([Diagnostics.Process]$OwnedProcess,[string]$ExpectedExecutable) {
    . (Join-Path $PSScriptRoot 'original-desktop-quit-discovery-core.ps1')
    if ($env:GITHUB_ACTIONS -cne 'true' -or
        $env:GITHUB_EVENT_NAME -cne 'push' -or $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_QUIT_DISCOVERY -cne 'read-only-v1') { throw 'Explicit disposable discovery required.' }
    if ($null -eq $OwnedProcess -or -not [IO.Path]::IsPathRooted($ExpectedExecutable)) { throw 'Owned target required.' }
    $OwnedProcess.Refresh()
    if ($OwnedProcess.HasExited -or $OwnedProcess.MainModule.FileName -cne $ExpectedExecutable) { throw 'Owned target unavailable.' }
    $ticks=$OwnedProcess.StartTime.ToUniversalTime().Ticks
    $null=$OwnedProcess.Handle
    $worker=Join-Path $PSScriptRoot 'original-desktop-quit-discovery-worker.ps1'
    $start=[Diagnostics.ProcessStartInfo]::new()
    $start.FileName=Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'PowerShell\7\pwsh.exe';$start.UseShellExecute=$false;$start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP','GITHUB_ACTIONS','RUNNER_OS','RUNNER_ENVIRONMENT',
        'GITHUB_REPOSITORY','GITHUB_REF','GITHUB_EVENT_NAME','MAGPIE_ORIGINAL_QUIT_DISCOVERY')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($null -ne $value) { $start.EnvironmentVariables[$name]=$value }
    }
    $quoted=foreach ($argument in @('-NoLogo','-NoProfile','-NonInteractive','-File',$worker,
        '-TargetProcessId',[string]$OwnedProcess.Id,'-TargetStartTicks',[string]$ticks,
        '-TargetExecutable',$ExpectedExecutable)) {
        if ($argument.Contains('"') -or $argument.Contains("`r") -or $argument.Contains("`n") -or $argument.EndsWith('\')) { throw 'Unsafe worker argument.' }
        '"'+$argument+'"' 
    }
    $start.Arguments=$quoted -join ' '
    $process=[Diagnostics.Process]::new();$process.StartInfo=$start;$started=$false;$attempted=$false
    try {
        $attempted=$true;$started=$process.Start();if (-not $started) { throw 'Discovery worker did not start.' }
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
        $parsed=$texts[0].ToString() | ConvertFrom-Json -ErrorAction Stop
        $result=[ordered]@{}
        foreach ($property in $parsed.PSObject.Properties) { $result[$property.Name]=$property.Value }
        Assert-OriginalTrayReport $result
        $OwnedProcess.Refresh()
        if ($OwnedProcess.HasExited -or $OwnedProcess.StartTime.ToUniversalTime().Ticks -ne $ticks -or
            $OwnedProcess.MainModule.FileName -cne $ExpectedExecutable) { throw 'Owner changed during probe.' }
        return $result
    } finally {
        if ($started -or $attempted) {
            # Kill only this helper. Never signal the supplied Magpie PID.
            try {
                if (-not $process.HasExited) {
                    $process.Kill();if (-not $process.WaitForExit(3000)) { throw 'Discovery worker exit unconfirmed.' }
                }
            } catch { throw 'Discovery worker ownership or exit unconfirmed.' }
        }
        $process.Dispose()
    }
}
