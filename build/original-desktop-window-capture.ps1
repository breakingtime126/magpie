# Dot-source only. Diagnostic capture AFTER sequence finally and its fixed report
# is saved. Caller must retain its original failure, uncertainty and cleanup decision.
# PrivateDirectory is the fresh fixture root, not an arbitrary output directory.
# This file never recovers, retries Connect/Restore, launches Desktop or cleans up.
. (Join-Path $PSScriptRoot 'original-desktop-window-capture-native.ps1')
function Assert-OriginalCaptureContext {
    if ($env:GITHUB_ACTIONS -cne 'true' -or $env:GITHUB_EVENT_NAME -cne 'push' -or
        $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_CONNECT_ACTION -cne 'original-ui-v1' -or
        $env:MAGPIE_ORIGINAL_WINDOW_CAPTURE -cne 'diagnostic-v1') { throw 'Capture context unavailable.' }
}
function Assert-OriginalCaptureRegularPath([string]$Path) {
    if (-not [IO.Path]::IsPathRooted($Path) -or [IO.Path]::GetFullPath($Path) -cne $Path -or
        $Path -match '[\x00-\x1f"*?]' -or $Path.StartsWith('\\')) { throw 'Capture path invalid.' }
    $cursor=$Path
    while ($cursor) {
        try { $attributes=[IO.File]::GetAttributes($cursor) }
        catch [IO.FileNotFoundException] { $cursor=[IO.Path]::GetDirectoryName($cursor);continue }
        catch [IO.DirectoryNotFoundException] { $cursor=[IO.Path]::GetDirectoryName($cursor);continue }
        if (($attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Capture link refused.' }
        $cursor=[IO.Path]::GetDirectoryName($cursor)
    }
}
function Assert-OriginalCapturePaths([string]$Executable,[string]$PrivateDirectory,[string]$EvidenceDirectory) {
    foreach ($path in @($env:RUNNER_TEMP,$Executable,$PrivateDirectory,$EvidenceDirectory)) { Assert-OriginalCaptureRegularPath $path }
    if ([IO.Path]::GetDirectoryName($PrivateDirectory) -cne $env:RUNNER_TEMP -or
        [IO.Path]::GetFileName($PrivateDirectory) -cnotmatch '^original-desktop-acceptance-[0-9a-f]{32}$' -or
        $Executable -cne (Join-Path $PrivateDirectory 'app\Magpie-Native-Codex-Experimental.exe') -or
        $EvidenceDirectory -cne (Join-Path $env:RUNNER_TEMP 'native-magpie-evidence') -or
        -not [IO.Directory]::Exists($PrivateDirectory) -or -not [IO.Directory]::Exists($EvidenceDirectory) -or
        -not [IO.File]::Exists($Executable)) { throw 'Capture fixture unavailable.' }
}
function Test-OriginalCaptureIdentity($OwnedProcess,$Identity) {
    return [OriginalCapture.Native]::Matches($OwnedProcess,$Identity.Id,$Identity.Ticks,$Identity.Executable,$Identity.Window)
}
function New-OriginalCaptureStart($Identity,[string]$PrivateDirectory,[string]$EvidenceDirectory) {
    $hostPath=Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    Assert-OriginalCaptureRegularPath $hostPath
    if (-not [IO.File]::Exists($hostPath)) { throw 'Capture host unavailable.' }
    $worker=Join-Path $PSScriptRoot 'original-desktop-window-capture-worker.ps1'
    Assert-OriginalCaptureRegularPath $worker
    # Encoded command avoids command-line path quoting ambiguities on Desktop.
    $values=@($worker,$Identity.Executable,$PrivateDirectory,$EvidenceDirectory) | ForEach-Object { "'"+$_.Replace("'","''")+"'" }
    $command="& $($values[0]) -TargetId $($Identity.Id) -StartTicks $($Identity.Ticks) -Window $($Identity.Window) -Executable $($values[1]) -PrivateDirectory $($values[2]) -EvidenceDirectory $($values[3])"
    $start=[Diagnostics.ProcessStartInfo]::new()
    $start.FileName=$hostPath;$start.UseShellExecute=$false;$start.CreateNoWindow=$true
    $start.RedirectStandardInput=$true;$start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
    $start.Arguments='-NoLogo -NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP','RUNNER_TEMP','GITHUB_ACTIONS','GITHUB_EVENT_NAME','RUNNER_OS',
        'RUNNER_ENVIRONMENT','GITHUB_REPOSITORY','GITHUB_REF','MAGPIE_ORIGINAL_CONNECT_ACTION','MAGPIE_ORIGINAL_WINDOW_CAPTURE')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($null -ne $value) { $start.EnvironmentVariables[$name]=$value }
    }
    return $start
}
# Only a complete ordered fixed prefix is evidence of a phase. Never return raw
# child output; partial/unknown/out-of-order records invalidate phase evidence.
function Get-OriginalCaptureProtocol {
    return "worker-entered`nnative-compile`ntarget-identity`nprint-window`nrender-returned`npng-encode`nprivate-write`ncapture-complete`n"
}
function Get-OriginalCapturePhase([string]$Output) {
    if ($Output.Length -eq 0) { return 'not-observed' }
    $prefix=Get-OriginalCaptureProtocol
    if ($Output -ceq ($prefix+'{"schema":1,"status":"captured"}')) { return 'capture-complete' }
    $length=0
    foreach ($phase in $prefix.Split([char]10)) {
        if ($phase.Length -eq 0) { continue }
        $length+=$phase.Length+1
        if ($Output -ceq $prefix.Substring(0,$length)) { return $phase }
    }
    return 'unverified'
}
# The 8s deadline bounds the started capture child, NOT this entire call or target
# rendering. Normal explicit waits can add 2s exit + 1s drain + 2s finally, and
# Dispose adds up to 5s exit + 2s drain. Constructor-error cleanup adds up to
# 5s exit + two 2s stream waits. OS start/kill/metadata/dispose calls themselves
# have no promised wall-clock bound. An unassociated throwing start stays unknown.
function Invoke-OriginalCaptureChild($Start,[int]$Deadline=8000) {
    . (Join-Path $PSScriptRoot 'original-desktop-window-capture-supervisor.ps1')
    Initialize-OriginalCaptureSupervisor
    $child=$null;$confirmed=$false;$success=$false;$reason='child-failed';$phase='not-observed'
    try {
        $savedEncoding=[Console]::InputEncoding
        $changeEncoding=$savedEncoding.GetPreamble().Length -ne 0
        if ($changeEncoding -and ($savedEncoding.CodePage -ne 65001 -or
            [BitConverter]::ToString($savedEncoding.GetPreamble()) -cne 'EF-BB-BF')) { throw 'Unsupported input preamble.' }
        try {
            # Preserve the console code page; only suppress a UTF-8 pipe preamble.
            if ($changeEncoding) { [Console]::InputEncoding=[Text.UTF8Encoding]::new($false) }
            $child=[OriginalCaptureSupervisor.BoundedProcess]::new($Start,256,$Deadline)
        } finally { if ($changeEncoding) { [Console]::InputEncoding=$savedEncoding } }
        $child.CloseInput()
        if (-not $child.Wait($Deadline)) { $reason='deadline';$child.Stop() }
        if (-not $child.Wait(2000) -or -not $child.Exited) { throw 'Child exit unconfirmed.' }
        $confirmed=$true
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while (-not $child.Drained -and $watch.ElapsedMilliseconds -lt 1000) { Start-Sleep -Milliseconds 10 }
        if ($child.TimedOut) { $reason='deadline' }
        elseif ($child.Overflow -or $child.ErrorOutput -or -not $child.Drained) { $reason='output-invalid' }
        elseif ($child.ExitCode -eq 0 -and $child.Output -ceq ((Get-OriginalCaptureProtocol)+'{"schema":1,"status":"captured"}')) {
            $success=$true;$reason='none'
        }
    } catch { $success=$false }
    finally {
        if ($null -ne $child) {
            try { $child.Stop();$confirmed=$child.Wait(2000) -and $child.Exited } catch { $confirmed=$false }
            # A killed child may leave complete prefix records. Read only after
            # exit/drain confirmation; overflow or incomplete streams are unknown.
            if ($confirmed -and $child.Drained -and -not $child.Overflow -and -not $child.ErrorOutput) {
                $phase=Get-OriginalCapturePhase $child.Output
            } else { $phase='unverified' }
            $child.Dispose()
        }
    }
    return @{Success=($success -and $confirmed);ExitConfirmed=$confirmed;Reason=$reason;Phase=$phase}
}
function Read-OriginalCaptureImage([string]$Path) {
    Assert-OriginalCaptureRegularPath $Path
    $stream=[IO.File]::Open($Path,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
    try {
        if ($stream.Length -lt 67 -or $stream.Length -gt 10485760) { throw 'Capture image size invalid.' }
        $bytes=[byte[]]::new([int]$stream.Length);$offset=0
        while ($offset -lt $bytes.Length) {
            $n=$stream.Read($bytes,$offset,$bytes.Length-$offset)
            if ($n -eq 0) { throw 'Capture image incomplete.' };$offset+=$n
        }
    } finally { $stream.Dispose() }
    if (-not [OriginalCapture.Native]::ValidPng($bytes)) { throw 'Capture image invalid.' }
    return ,$bytes
}
function Invoke-OriginalDesktopWindowCapture {
    param([Diagnostics.Process]$OwnedProcess,[string]$ExpectedExecutable,[string]$PrivateDirectory,
        [string]$EvidenceDirectory,[switch]$SequenceReportSaved)
    $result=[ordered]@{schema=1;scope='original-magpie-diagnostic';status='blocked';reason='context';phase='not-observed';childExitConfirmed=$false;published=$false}
    try {
        Assert-OriginalCaptureContext
        if (-not $SequenceReportSaved) { return [pscustomobject]$result }
        Assert-OriginalCapturePaths $ExpectedExecutable $PrivateDirectory $EvidenceDirectory
        $result.reason='identity'
        Initialize-OriginalCaptureNative
        if ($null -eq $OwnedProcess) { throw 'Owned process missing.' }
        $identity=@{Id=$OwnedProcess.Id;Ticks=$OwnedProcess.StartTime.ToUniversalTime().Ticks;
            Window=$OwnedProcess.MainWindowHandle.ToInt64();Executable=$ExpectedExecutable}
        if (-not (Test-OriginalCaptureIdentity $OwnedProcess $identity)) { throw 'Capture identity unavailable.' }
        $result.reason='already-attempted'
        $attempt=Join-Path $PrivateDirectory 'window-capture.attempt'
        Assert-OriginalCaptureRegularPath $attempt
        # Persistent exclusive marker: a failed or interrupted attempt is never retried.
        $marker=[IO.File]::Open($attempt,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None);$marker.Dispose()
        $private=Join-Path $PrivateDirectory 'window-capture'
        Assert-OriginalCaptureRegularPath $private
        if ([IO.Directory]::Exists($private)) { throw 'Capture directory already exists.' }
        [void][IO.Directory]::CreateDirectory($private)
        $target=Join-Path $EvidenceDirectory 'original-magpie-uncertain.png'
        Assert-OriginalCaptureRegularPath $target
        if ([IO.File]::Exists($target)) { throw 'Capture evidence already exists.' }
        $result.reason='child-failed';$result.status='failed'
        $child=Invoke-OriginalCaptureChild (New-OriginalCaptureStart $identity $PrivateDirectory $EvidenceDirectory)
        $result.childExitConfirmed=[bool]$child.ExitConfirmed;$result.reason=$child.Reason;$result.phase=$child.Phase
        if (-not $child.Success -or -not $child.ExitConfirmed) { return [pscustomobject]$result }
        $result.reason='identity'
        if (-not (Test-OriginalCaptureIdentity $OwnedProcess $identity)) { throw 'Capture identity changed.' }
        $result.reason='image-invalid'
        $source=Join-Path $private 'original-magpie-uncertain.png'
        $bytes=Read-OriginalCaptureImage $source
        $result.reason='identity'
        if (-not (Test-OriginalCaptureIdentity $OwnedProcess $identity)) { throw 'Capture identity changed.' }
        Assert-OriginalCapturePaths $ExpectedExecutable $PrivateDirectory $EvidenceDirectory
        Assert-OriginalCaptureRegularPath $source;Assert-OriginalCaptureRegularPath $target
        # Stage complete validated bytes privately, then same-volume no-overwrite rename.
        # Failed writes never leave a partial PNG in public evidence.
        $result.reason='publication-failed'
        $stage=Join-Path $private 'validated.png'
        Assert-OriginalCaptureRegularPath $stage
        $file=[IO.File]::Open($stage,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
        try { $file.Write($bytes,0,$bytes.Length);$file.Flush() } finally { $file.Dispose() }
        if (-not (Test-OriginalCaptureIdentity $OwnedProcess $identity)) { $result.reason='identity';throw 'Capture identity changed.' }
        Assert-OriginalCaptureRegularPath $target;Assert-OriginalCaptureRegularPath $stage
        [IO.File]::Move($stage,$target)
        $result.status='captured';$result.reason='none';$result.published=$true
    } catch { } # Fixed labels only. Preserve all GUI, journal and profile state.
    return [pscustomobject]$result
}
