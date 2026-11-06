# Test-only orchestration. Dot-source only in the explicitly opted-in disposable CI.
# Contract and limits: desktop-live-welcome.md.
function Assert-LiveWelcomeOptions([bool]$Enabled, [string]$Mode, [bool]$Startup, [bool]$Compatibility) {
    if ($Enabled -and ($Mode -cne 'observe' -or -not $Startup -or -not $Compatibility)) {
        throw 'Live welcome requires observe and both reviewed compatibility opt-ins.'
    }
}
function Assert-LiveShape($Value, [string[]]$Keys) {
    if ($null -eq $Value -or $Value -isnot [pscustomobject]) { throw 'Invalid live metadata.' }
    $actual = @($Value.PSObject.Properties.Name)
    if ($actual.Count -ne $Keys.Count) { throw 'Invalid live metadata shape.' }
    foreach ($key in $actual) { if ($Keys -cnotcontains $key) { throw 'Invalid live metadata field.' } }
}
function Assert-LiveStrings($Value, [string[]]$Keys) {
    foreach ($key in $Keys) { if ($Value.$key -isnot [string]) { throw 'Invalid live label type.' } }
}
function Read-LiveConfig([string]$Path) {
    $stream=[IO.File]::OpenRead($Path)
    try {
        $bytes=[byte[]]::new(16385); $count=0
        while ($count -lt $bytes.Length) {
            $n=$stream.Read($bytes,$count,$bytes.Length-$count)
            if ($n -eq 0) { break }; $count+=$n
        }
        if ($count -lt 1 -or $count -gt 16384) { throw 'Invalid internal live config size.' }
        $utf8=[Text.UTF8Encoding]::new($false,$true)
        return (ConvertFrom-LiveJSON ($utf8.GetString($bytes,0,$count)) 16384)
    } finally { $stream.Dispose() }
}
function Assert-LiveInteger($Value, [long]$Min, [long]$Max) {
    if (($Value -isnot [int] -and $Value -isnot [long]) -or $Value -lt $Min -or $Value -gt $Max) { throw 'Invalid live count.' }
}
function ConvertFrom-LiveJSON([string]$Text, [int]$Limit = 8192) {
    if ([Text.Encoding]::UTF8.GetByteCount($Text) -gt $Limit -or [string]::IsNullOrWhiteSpace($Text)) { throw 'Invalid live output size.' }
    # JsonDocument detects duplicate properties before PowerShell normalizes them.
    $doc = [System.Text.Json.JsonDocument]::Parse($Text)
    try { Assert-LiveUniqueJSON $doc.RootElement } finally { $doc.Dispose() }
    return ($Text | ConvertFrom-Json -ErrorAction Stop)
}
function Assert-LiveUniqueJSON($Element) {
    if ($Element.ValueKind.ToString() -ceq 'Object') {
        $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
        foreach ($property in $Element.EnumerateObject()) {
            if (-not $seen.Add($property.Name)) { throw 'Duplicate live field.' }
            Assert-LiveUniqueJSON $property.Value
        }
    } elseif ($Element.ValueKind.ToString() -ceq 'Array') {
        foreach ($item in $Element.EnumerateArray()) { Assert-LiveUniqueJSON $item }
    }
}
function Assert-LiveReady($Value) {
    Assert-LiveShape $Value @('schema','scope','status','lifetime_seconds','native_sha256','catalog_blob')
    Assert-LiveInteger $Value.schema 1 1
    Assert-LiveInteger $Value.lifetime_seconds 180 180
    Assert-LiveStrings $Value @('scope','status','native_sha256','catalog_blob')
    if ($Value.scope -cne 'desktop-loopback-fixtures-only' -or $Value.status -cne 'ready' -or
        $Value.native_sha256 -cne '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39' -or
        $Value.catalog_blob -cne '489ebe3f2483348641a16160ce8491dd077eabca') { throw 'Invalid live readiness.' }
}
function Assert-LiveAudit($Value, [int]$ExitCode) {
    Assert-LiveShape $Value @('schema','scope','status','failure','alpha_requests','beta_requests','fixture_errors')
    Assert-LiveInteger $Value.schema 1 1
    Assert-LiveInteger $Value.alpha_requests 0 16
    Assert-LiveInteger $Value.beta_requests 0 16
    Assert-LiveInteger $Value.fixture_errors 0 2147483647
    Assert-LiveStrings $Value @('scope','status','failure')
    $failures = @('input_invalid','platform_rejected','environment_rejected','scratch_invalid','scratch_not_empty',
        'native_hash_mismatch','catalog_hash_mismatch','catalog_invalid','native_version_mismatch',
        'native_version_failed','fixture_invalid','fixture_rejected_request','output_failed','stop_invalid','fixture_timeout','internal_failure')
    if ($Value.scope -cne 'desktop-loopback-fixtures-only' -or $Value.failure -isnot [string]) { throw 'Invalid live audit.' }
    if ($Value.status -ceq 'stopped') {
        if ($ExitCode -ne 0 -or $Value.failure -cne '' -or $Value.fixture_errors -ne 0) { throw 'Invalid stopped audit.' }
    } elseif ($Value.status -ceq 'failed') {
        if ($ExitCode -ne 1 -or $failures -cnotcontains $Value.failure) { throw 'Invalid failed audit.' }
    } else { throw 'Invalid live audit status.' }
}
function ConvertTo-LiveConfiguration($Value, [string]$Scratch, [string]$Codex) {
    Assert-LiveShape $Value @('backends','working_directory','control_plane','control_home')
    Assert-LiveStrings $Value @('working_directory','control_plane','control_home')
    $workspace = Join-Path $Scratch 'workspace'; $control = Join-Path $Scratch 'control-home'
    if ($Value.working_directory -cne $workspace -or $Value.control_home -cne $control -or
        $Value.control_plane -cne 'routing-v0.167.0-alpha.3.2' -or $Value.backends -isnot [array] -or $Value.backends.Count -ne 2) {
        throw 'Invalid internal live configuration.'
    }
    $backends = @(); $i = 0
    foreach ($id in @('alpha','beta')) {
        $b = $Value.backends[$i++]; $backendHome = Join-Path $Scratch ($id + '-home'); $ch = Join-Path $Scratch ($id + '-codex')
        Assert-LiveShape $b @('id','provider','command','home','codex_home','startup_probe_sha256')
        Assert-LiveStrings $b @('id','provider','command','home','codex_home','startup_probe_sha256')
        if ($b.id -cne $id -or $b.provider -cne ($id + '_provider') -or $b.command -cne $Codex -or
            $b.home -cne $backendHome -or $b.codex_home -cne $ch -or
            $b.startup_probe_sha256 -cne '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39') { throw 'Invalid live backend authority.' }
        # Reconstruct, never copy command/path authority from the helper's JSON.
        $backends += [ordered]@{ id=$id; provider=($id+'_provider'); command=$Codex; home=$backendHome; codex_home=$ch }
    }
    return [pscustomobject]@{ Backends=$backends; Workspace=$workspace; ControlHome=$control }
}
function Assert-WelcomeResult($Value) {
    Assert-LiveShape $Value @('schema','scope','status','reason','stage','actions','suggestionsDisabled','engineeringSelected','continueInvoked','nextScreen','targetObservations','initialReadObservation')
    if ($null -ne $Value.targetObservations) {
        Assert-LiveShape $Value.targetObservations @('engineering','suggestions','continue')
        foreach ($key in @('engineering','suggestions','continue')) {
            $observation=$Value.targetObservations.$key
            Assert-LiveShape $observation @('interactiveCount','controlType')
            Assert-LiveInteger $observation.interactiveCount 0 500
            Assert-LiveStrings $observation @('controlType')
            if ($observation.interactiveCount -eq 0) {
                if ($observation.controlType -cne 'absent') { throw 'Invalid absent welcome target.' }
            } elseif ($observation.interactiveCount -gt 1) {
                if ($observation.controlType -cne 'ambiguous') { throw 'Invalid duplicate welcome target.' }
            } elseif (@('other','Button','RadioButton','CheckBox','Edit','ComboBox','Hyperlink','MenuItem','ListItem','TabItem','Slider','Spinner','SplitButton') -cnotcontains $observation.controlType) { throw 'Invalid welcome target type.' }
        }
    }
    Assert-LiveInteger $Value.schema 1 1; Assert-LiveInteger $Value.actions 0 3
    Assert-LiveStrings $Value @('scope','status','reason','stage','nextScreen')
    foreach ($key in @('suggestionsDisabled','engineeringSelected','continueInvoked')) {
        if ($Value.$key -isnot [bool]) { throw 'Invalid welcome state.' }
    }
    if ($Value.scope -cne 'synthetic-welcome-only' -or @('stopped','observed') -cnotcontains $Value.status -or
        @('uia_provider_error','unsupported_runtime','invalid_context','deadline_exceeded','identity_or_foreground_changed','uia_tree_limit','uia_owner_mismatch','target_unavailable','suggestions_not_off','selection_state_changed','protected_screen','unreviewed_screen','unreviewed_control','unreviewed_text','ambiguous_control','accessibility_not_ready','single_transition_attempted','owner_release_failed') -cnotcontains $Value.reason -or
        @('not_observed','protected_screen','welcome_still_visible','unreviewed_screen') -cnotcontains $Value.nextScreen) { throw 'Invalid welcome labels.' }
    if (@('initialization','ownership','welcome_read','suggestions_off','engineering_select','continue_invoke','next_screen_read','complete') -cnotcontains $Value.stage) { throw 'Invalid welcome stage.' }
    if ($Value.status -ceq 'observed' -and ($Value.stage -cne 'complete')) { throw 'Incomplete welcome observation.' }
    if ($Value.status -ceq 'observed' -and ($Value.reason -cne 'single_transition_attempted' -or
        -not $Value.suggestionsDisabled -or -not $Value.engineeringSelected -or -not $Value.continueInvoked -or
        $Value.actions -lt 1 -or $Value.nextScreen -ceq 'not_observed')) { throw 'Invalid welcome observation.' }
    if ($Value.status -ceq 'stopped' -and ($Value.reason -ceq 'single_transition_attempted' -or ($Value.nextScreen -cne 'not_observed' -and $Value.reason -cne 'owner_release_failed'))) { throw 'Invalid stopped welcome.' }
    $initialReasons = @('protected_screen','unreviewed_screen','unreviewed_control','unreviewed_text','ambiguous_control','accessibility_not_ready')
    if ($Value.stage -ceq 'welcome_read' -and $initialReasons -ccontains $Value.reason -and
        $null -eq $Value.initialReadObservation) { throw 'Missing initial welcome observation.' }
    if ($null -ne $Value.initialReadObservation) {
        if ($Value.status -cne 'stopped' -or $Value.stage -cne 'welcome_read' -or $Value.actions -ne 0 -or
            $Value.suggestionsDisabled -or $Value.engineeringSelected -or $Value.continueInvoked -or
            $Value.nextScreen -cne 'not_observed' -or $null -eq $Value.targetObservations -or
            ($initialReasons -cnotcontains $Value.reason -and $Value.reason -cne 'owner_release_failed')) {
            throw 'Initial observation must describe a zero-action stop.'
        }
        $initial = $Value.initialReadObservation
        Assert-LiveShape $initial @('snapshotCount','headingExactCount','headingWhitespaceNormalizedCount','targets','chromeRejection')
        Assert-LiveInteger $initial.snapshotCount 1 4
        Assert-LiveInteger $initial.headingExactCount 0 500
        Assert-LiveInteger $initial.headingWhitespaceNormalizedCount $initial.headingExactCount 500
        Assert-LiveStrings $initial @('chromeRejection')
        if (@('none','node_count','window_count','unexpected_interactive','named_content','container_type') -cnotcontains $initial.chromeRejection) {
            throw 'Invalid initial chrome classification.'
        }
        Assert-LiveShape $initial.targets @('engineering','suggestions','continue')
        $labelCount = $initial.headingWhitespaceNormalizedCount
        foreach ($key in @('engineering','suggestions','continue')) {
            $target = $initial.targets.$key
            Assert-LiveShape $target @('interactiveCount','noninteractiveCount')
            Assert-LiveInteger $target.interactiveCount 0 500
            Assert-LiveInteger $target.noninteractiveCount 0 500
            if ($target.interactiveCount -ne $Value.targetObservations.$key.interactiveCount) {
                throw 'Initial target counts disagree.'
            }
            $labelCount += $target.interactiveCount + $target.noninteractiveCount
        }
        if ($labelCount -gt 500) { throw 'Initial labels exceed snapshot bound.' }
        if ($Value.reason -ceq 'unreviewed_screen' -and
            ($initial.headingExactCount -ne 0 -or $initial.chromeRejection -ceq 'none')) {
            throw 'Invalid unreviewed initial screen observation.'
        }
        if ($Value.reason -ceq 'accessibility_not_ready' -and
            ($labelCount -ne 0 -or $initial.chromeRejection -cne 'none')) {
            throw 'Invalid shell-only initial observation.'
        }
    }
    if ($Value.reason -ceq 'accessibility_not_ready') {
        if ($Value.status -cne 'stopped' -or $Value.stage -cne 'welcome_read' -or $Value.actions -ne 0 -or
            $Value.suggestionsDisabled -or $Value.engineeringSelected -or $Value.continueInvoked -or
            $Value.nextScreen -cne 'not_observed' -or $null -eq $Value.targetObservations) { throw 'Invalid accessibility readiness stop.' }
        foreach ($key in @('engineering','suggestions','continue')) {
            if ($Value.targetObservations.$key.interactiveCount -ne 0 -or
                $Value.targetObservations.$key.controlType -cne 'absent') { throw 'Readiness stop cannot include welcome targets.' }
        }
    }
}
function Get-LiveWelcomeOutcome($Prior, [bool]$FixtureStopped, $Welcome, [bool]$AfterCaptured) {
    # Earlier Desktop or independent native failures always retain precedence.
    if ($Prior.exitCode -ne 0) { return $Prior }
    if (-not $FixtureStopped -or -not $AfterCaptured -or $null -eq $Welcome -or $Welcome.status -cne 'observed' -or
        $Welcome.nextScreen -ceq 'welcome_still_visible') {
        return [pscustomobject]@{status='blocked';stage='live-welcome-stopped';reason='The reviewed welcome slice stopped or remained unchanged; no onboarding, composer or model success is established.';exitCode=1}
    }
    return [pscustomobject]@{status='observed';stage='single-welcome-transition-observed';reason='One reviewed welcome transition was attempted and next-screen pixels captured. The next screen is unreviewed; no full onboarding, composer or model conversation success is claimed.';exitCode=0}
}
function Initialize-LiveSupervisor {
    if ('DesktopLive.BoundedProcess' -as [type]) { return }
    Add-Type -TypeDefinition @'
using System;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
namespace DesktopLive {
 public sealed class BoundedProcess : IDisposable {
  readonly object gate=new object(); readonly StringBuilder output=new StringBuilder();
  readonly Process process; readonly int limit; readonly Timer timer;
  readonly Task stdout, stderr; volatile bool timedOut, overflow; bool disposed;
  public bool TimedOut { get { return timedOut; } } public bool Overflow { get { return overflow; } }
  public bool Exited { get { try { return process.HasExited; } catch { return false; } } }
  public int ExitCode { get { return process.ExitCode; } }
  public string Output { get { lock(gate) return output.ToString(); } }
  public bool Drained { get { return stdout.IsCompleted && stderr.IsCompleted; } }
  public BoundedProcess(ProcessStartInfo start, int maxBytes, int milliseconds) {
   limit=maxBytes; process=new Process(); process.StartInfo=start;
   if(!process.Start()) throw new Exception("start failed"); var held=process.Handle;
   stdout=Task.Run(()=>Drain(process.StandardOutput,true)); stderr=Task.Run(()=>Drain(process.StandardError,false));
   timer=new Timer(_=>{ timedOut=true; Stop(); },null,milliseconds,Timeout.Infinite);
  }
  void Drain(StreamReader reader,bool retain) {
   try { var buffer=new char[1]; int count,total=0;
    while((count=reader.Read(buffer,0,buffer.Length))>0) {
     total+=Encoding.UTF8.GetByteCount(buffer,0,count);
     if(total>limit) { overflow=true; Stop(); return; }
     if(retain) lock(gate) output.Append(buffer,0,count);
    }
   } catch { overflow=true; }
  }
  public void Deadline(int milliseconds) { timer.Change(milliseconds,Timeout.Infinite); }
  public void CloseInput() { try { process.StandardInput.Close(); } catch {} }
  public void Stop() { try { if(!process.HasExited) process.Kill(); } catch {} }
  public bool Wait(int milliseconds) { return process.WaitForExit(milliseconds); }
  public void Dispose() { if(disposed)return; disposed=true; timer.Dispose(); Stop();
   if(process.WaitForExit(5000)) { try { Task.WaitAll(new[]{stdout,stderr},2000); } catch {} process.Dispose(); }
  }
 }
}
'@
}
function New-LiveStart([string]$Executable, [string[]]$Arguments, [string]$Directory, [switch]$Welcome) {
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName=$Executable; $start.UseShellExecute=$false; $start.CreateNoWindow=$true
    $start.WorkingDirectory=$Directory; $start.RedirectStandardInput=$true
    $start.RedirectStandardOutput=$true; $start.RedirectStandardError=$true
    foreach ($arg in $Arguments) { $start.ArgumentList.Add($arg) }
    $start.Environment.Clear()
    $keys = @('SystemRoot','WINDIR','TEMP','TMP')
    if ($Welcome) { $keys += @('USERPROFILE','APPDATA','LOCALAPPDATA','COMSPEC','HOMEDRIVE','HOMEPATH',
        'USERNAME','USERDOMAIN','ALLUSERSPROFILE','PROGRAMDATA','PROGRAMFILES','PROGRAMFILES(X86)',
        'COMMONPROGRAMFILES','COMMONPROGRAMFILES(X86)','PROCESSOR_ARCHITECTURE','NUMBER_OF_PROCESSORS','OS','SESSIONNAME') }
    foreach ($key in $keys) { $v=[Environment]::GetEnvironmentVariable($key); if ($v) { $start.Environment[$key]=$v } }
    $start.Environment['PATH']=(Join-Path $env:SystemRoot 'System32')+';'+$env:SystemRoot
    if ($Welcome) { $start.Environment['GITHUB_ACTIONS']='true'; $start.Environment['RUNNER_OS']='Windows' }
    else { foreach ($key in @('HOME','USERPROFILE','APPDATA','LOCALAPPDATA','CODEX_HOME')) { $start.Environment[$key]=$Directory } }
    return $start
}
function Wait-LiveReady($Process) {
    $watch=[Diagnostics.Stopwatch]::StartNew()
    while ($watch.Elapsed.TotalSeconds -lt 25 -and -not $Process.TimedOut -and -not $Process.Overflow) {
        $text=$Process.Output
        if ($text.Contains("`n")) {
            $lines=@($text.TrimEnd("`r","`n") -split "`r?`n")
            if ($lines.Count -ne 1 -or $Process.Exited) { throw 'Live fixture did not remain ready.' }
            $ready=ConvertFrom-LiveJSON $lines[0]; Assert-LiveReady $ready
            $Process.Deadline(180000)
            return $ready
        }
        if ($Process.Exited) { break }; Start-Sleep -Milliseconds 50
    }
    throw 'Live fixture readiness unavailable.'
}
function Complete-LiveFixture($Process) {
    $Process.CloseInput()
    if (-not $Process.Wait(10000)) { $Process.Stop(); if (-not $Process.Wait(5000)) { throw 'Live helper exit unconfirmed.' } }
    $watch=[Diagnostics.Stopwatch]::StartNew()
    while (-not $Process.Drained -and $watch.Elapsed.TotalSeconds -lt 2) { Start-Sleep -Milliseconds 20 }
    if ($Process.TimedOut -or $Process.Overflow -or -not $Process.Drained) { throw 'Live fixture supervision failed.' }
    $lines=@($Process.Output.TrimEnd("`r","`n") -split "`r?`n")
    if ($lines.Count -ne 2) { throw 'Invalid live fixture records.' }
    Assert-LiveReady (ConvertFrom-LiveJSON $lines[0])
    $audit=ConvertFrom-LiveJSON $lines[1]; Assert-LiveAudit $audit $Process.ExitCode
    return $audit
}
function Get-LiveDesktopExecutable([string]$InstallLocation, [string]$ManifestExecutable) {
    # Only separator normalization of the already reviewed exact executable.
    if (-not [IO.Path]::IsPathRooted($InstallLocation) -or
        $ManifestExecutable.Replace('\','/') -cne 'app/ChatGPT.exe') { throw 'Unreviewed Desktop executable.' }
    return [IO.Path]::GetFullPath((Join-Path (Join-Path $InstallLocation 'app') 'ChatGPT.exe'))
}
function Invoke-LiveWelcome($Start, [System.Collections.IDictionary]$Diagnostic = @{}) {
    $child=$null
    $Diagnostic.stage='helper_start'; $Diagnostic.reason='helper_start_failed'
    $Diagnostic.exitCode=$null; $Diagnostic.stdoutBytes=0; $Diagnostic.outputLimitExceeded=$false
    # Constructor failure may follow process creation. Never retry on uncertainty.
    $Diagnostic.partialActionUncertain=$true
    try {
        $child=[DesktopLive.BoundedProcess]::new($Start,8192,30000)
        $Diagnostic.stage='helper_wait'; $Diagnostic.reason='helper_wait_failed'
        if (-not $child.Wait(30000)) {
            $Diagnostic.reason='helper_timeout'
            $child.Stop(); $null=$child.Wait(5000); throw 'Welcome supervisor stopped.'
        }
        $Diagnostic.stage='helper_output'; $Diagnostic.reason='helper_output_unavailable'
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while (-not $child.Drained -and $watch.Elapsed.TotalSeconds -lt 2) { Start-Sleep -Milliseconds 20 }
        if ($child.TimedOut) { $Diagnostic.reason='helper_timeout'; throw 'Welcome supervisor stopped.' }
        if ($child.Overflow) { $Diagnostic.reason='helper_output_limit'; throw 'Welcome supervisor stopped.' }
        if (-not $child.Drained) { $Diagnostic.reason='helper_output_unclosed'; throw 'Welcome supervisor stopped.' }
        if ($child.ExitCode -ne 0) { $Diagnostic.reason='helper_exit_nonzero'; throw 'Welcome supervisor stopped.' }
        $Diagnostic.stage='helper_schema'; $Diagnostic.reason='helper_schema_invalid'
        $value=ConvertFrom-LiveJSON $child.Output; Assert-WelcomeResult $value
        $Diagnostic.stage='complete'; $Diagnostic.reason='helper_result_validated'
        $Diagnostic.partialActionUncertain=($value.status -ceq 'stopped' -and $value.actions -gt 0)
        return $value
    } finally {
        if ($null -ne $child) {
            try {
                if ($child.Exited) { $Diagnostic.exitCode=$child.ExitCode }
                $Diagnostic.stdoutBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
                $Diagnostic.outputLimitExceeded=$child.Overflow
            } catch { }
            try { $child.Dispose() } catch {
                if ($Diagnostic.stage -ceq 'complete') {
                    $Diagnostic.stage='helper_cleanup'; $Diagnostic.reason='helper_cleanup_unconfirmed'
                    $Diagnostic.partialActionUncertain=$true
                }
                throw 'Welcome supervisor cleanup failed.'
            }
        }
    }
}
