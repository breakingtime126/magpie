# Test-only orchestration. Dot-source only in the explicitly opted-in disposable CI.
# Contract and limits: desktop-live-welcome.md.
. (Join-Path $PSScriptRoot 'desktop-unelevated-read-only.ps1')
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
function Get-LiveNativeSHA([bool]$Build4239=$false) {
    if ($Build4239) { return '4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382' }
    return '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
}
function Get-LiveFixtureScope([string]$FixtureProfile='') {
    if ($FixtureProfile -ceq '') { return 'desktop-loopback-fixtures-only' }
    if ($FixtureProfile -ceq 'synthetic-models-4239') { return 'desktop-self-authored-fixtures-only' }
    if ($FixtureProfile -ceq 'bridge-owned-catalog-4239') { return 'desktop-bridge-owned-catalog-fixtures-only' }
    throw 'Unreviewed live fixture profile.'
}
function Assert-LiveReady($Value,[bool]$Build4239=$false,[string]$FixtureProfile='',[string]$SetupProfile='') {
    if ($SetupProfile -and ($SetupProfile -cne 'unelevated-read-only-4239' -or $FixtureProfile -cne 'bridge-owned-catalog-4239')) { throw 'Invalid setup profile.' }
    $scope=Get-LiveFixtureScope $FixtureProfile
    if ($FixtureProfile -ceq 'bridge-owned-catalog-4239') {
        $keys=@('schema','scope','status','lifetime_seconds','native_sha256','catalog_source','owned_catalog_sha256')
        $lifetime=180
        if ($SetupProfile) { $keys+=@('sandbox_setup');$lifetime=360 }
        Assert-LiveShape $Value $keys
        Assert-LiveInteger $Value.schema 1 1
        Assert-LiveInteger $Value.lifetime_seconds $lifetime $lifetime
        if ($SetupProfile) { Assert-LiveSandboxSetup $Value.sandbox_setup -Complete }
        Assert-LiveStrings $Value @('scope','status','native_sha256','catalog_source','owned_catalog_sha256')
        if (-not $Build4239 -or $Value.scope -cne $scope -or $Value.status -cne 'ready' -or
            $Value.native_sha256 -cne (Get-LiveNativeSHA $true) -or
            $Value.catalog_source -cne 'bridge-owned-self-authored-test-models-v1' -or
            $Value.owned_catalog_sha256 -cnotmatch '^[0-9a-f]{64}$') { throw 'Invalid owned catalog readiness.' }
        return
    }
    if ($FixtureProfile -ceq 'synthetic-models-4239') {
        Assert-LiveShape $Value @('schema','scope','status','lifetime_seconds','native_sha256','catalog_source')
        Assert-LiveInteger $Value.schema 1 1
        Assert-LiveInteger $Value.lifetime_seconds 180 180
        Assert-LiveStrings $Value @('scope','status','native_sha256','catalog_source')
        if (-not $Build4239 -or $Value.scope -cne $scope -or $Value.status -cne 'ready' -or
            $Value.native_sha256 -cne (Get-LiveNativeSHA $true) -or
            $Value.catalog_source -cne 'self-authored-test-models-v1') { throw 'Invalid self-authored readiness.' }
        return
    }
    Assert-LiveShape $Value @('schema','scope','status','lifetime_seconds','native_sha256','catalog_blob')
    Assert-LiveInteger $Value.schema 1 1
    Assert-LiveInteger $Value.lifetime_seconds 180 180
    Assert-LiveStrings $Value @('scope','status','native_sha256','catalog_blob')
    if ($Value.scope -cne 'desktop-loopback-fixtures-only' -or $Value.status -cne 'ready' -or
        $Value.native_sha256 -cne (Get-LiveNativeSHA $Build4239) -or
        $Value.catalog_blob -cne '489ebe3f2483348641a16160ce8491dd077eabca') { throw 'Invalid live readiness.' }
}
function Assert-LiveAudit($Value, [int]$ExitCode,[string]$FixtureProfile='',[string]$SetupProfile='') {
    if ($SetupProfile -and ($SetupProfile -cne 'unelevated-read-only-4239' -or $FixtureProfile -cne 'bridge-owned-catalog-4239')) { throw 'Invalid setup profile.' }
    $scope=Get-LiveFixtureScope $FixtureProfile
    $keys=@('schema','scope','status','failure','alpha_requests','beta_requests','fixture_errors')
    if ($SetupProfile) { $keys+=@('sandbox_setup') }
    Assert-LiveShape $Value $keys
    if ($SetupProfile) { Assert-LiveSandboxSetup $Value.sandbox_setup -Complete }
    Assert-LiveInteger $Value.schema 1 1
    Assert-LiveInteger $Value.alpha_requests 0 16
    Assert-LiveInteger $Value.beta_requests 0 16
    Assert-LiveInteger $Value.fixture_errors 0 2147483647
    Assert-LiveStrings $Value @('scope','status','failure')
    $failures = @('input_invalid','platform_rejected','environment_rejected','scratch_invalid','scratch_not_empty',
        'native_hash_mismatch','catalog_hash_mismatch','catalog_invalid','native_version_mismatch',
        'native_version_failed','fixture_invalid','fixture_rejected_request','output_failed','stop_invalid','fixture_timeout','internal_failure')
    if ($Value.scope -cne $scope -or $Value.failure -isnot [string]) { throw 'Invalid live audit.' }
    if ($Value.status -ceq 'stopped') {
        if (-not $SetupProfile -and $FixtureProfile -cin @('synthetic-models-4239','bridge-owned-catalog-4239') -and ($Value.alpha_requests -ne 0 -or $Value.beta_requests -ne 0)) {
            throw 'Menu-only fixtures received an unauthorized model request.'
        }
        if ($ExitCode -ne 0 -or $Value.failure -cne '' -or $Value.fixture_errors -ne 0) { throw 'Invalid stopped audit.' }
    } elseif ($Value.status -ceq 'failed') {
        if ($ExitCode -ne 1 -or $failures -cnotcontains $Value.failure) { throw 'Invalid failed audit.' }
    } else { throw 'Invalid live audit status.' }
}
function ConvertTo-LiveConfiguration($Value, [string]$Scratch, [string]$Codex, [bool]$Build4239=$false,[string]$FixtureProfile='',$Readiness=$null,[string]$SetupProfile='') {
    $null=Get-LiveFixtureScope $FixtureProfile
    if ($FixtureProfile -ceq 'bridge-owned-catalog-4239') {
        Assert-LiveShape $Value @('backends','working_directory','control_plane','control_home','bridge_model_catalog_json')
    } else {
        Assert-LiveShape $Value @('backends','working_directory','control_plane','control_home')
    }
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
            $b.startup_probe_sha256 -cne (Get-LiveNativeSHA $Build4239)) { throw 'Invalid live backend authority.' }
        # Reconstruct, never copy command/path authority from the helper's JSON.
        $backends += [ordered]@{ id=$id; provider=($id+'_provider'); command=$Codex; home=$backendHome; codex_home=$ch }
    }
    $result=[pscustomobject]@{ Backends=$backends; Workspace=$workspace; ControlHome=$control }
    if ($FixtureProfile -ceq 'bridge-owned-catalog-4239') {
        Assert-LiveReady $Readiness -Build4239 $Build4239 -FixtureProfile $FixtureProfile -SetupProfile $SetupProfile
        $expected=Join-Path $control 'bridge-model-catalog.json'
        if ($Value.bridge_model_catalog_json -isnot [string] -or $Value.bridge_model_catalog_json -cne $expected -or
            -not [IO.Path]::IsPathFullyQualified($expected)) { throw 'Invalid owned catalog path.' }
        # Only this private fixture-owned file is inspected; no caller-supplied catalog path.
        foreach ($path in @($Scratch,$control,$expected)) {
            $item=Get-Item -LiteralPath $path -Force -ErrorAction Stop
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Invalid owned catalog file.' }
        }
        if ($item.PSIsContainer -or $item.Length -lt 1 -or $item.Length -gt 1048576) { throw 'Invalid owned catalog size.' }
        $digest=(Get-FileHash -Algorithm SHA256 -LiteralPath $expected).Hash.ToLowerInvariant()
        if ($digest -cne $Readiness.owned_catalog_sha256) { throw 'Owned catalog digest mismatch.' }
        $result | Add-Member -NotePropertyName OwnedCatalogPath -NotePropertyValue $expected
        $result | Add-Member -NotePropertyName OwnedCatalogSHA256 -NotePropertyValue $digest
    }
    return $result
}
# Read only the one freshly owned frontend file. This is a bounded, single
# observation, not a writer, retry loop, general TOML parser or restart proof.
function Assert-LiveOwnedSettingsFile([string]$ControlHome,[string]$Owner) {
    if ($Owner -cnotin @('alpha','beta') -or -not [IO.Path]::IsPathFullyQualified($ControlHome)) {
        throw 'Invalid owned settings observation.'
    }
    $path=Join-Path $ControlHome 'config.toml'
    foreach ($candidate in @($ControlHome,$path)) {
        $item=Get-Item -LiteralPath $candidate -Force -ErrorAction Stop
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Invalid owned settings file.' }
    }
    if ($item.PSIsContainer -or $item.Length -lt 1 -or $item.Length -gt 16384) { throw 'Invalid owned settings size.' }
    $stream=[IO.File]::Open($path,[IO.FileMode]::Open,[IO.FileAccess]::Read,
        ([IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete))
    try {
        $bytes=[byte[]]::new(16385);$count=0
        while ($count -lt $bytes.Length) {
            $n=$stream.Read($bytes,$count,$bytes.Length-$count)
            if ($n -eq 0) { break };$count+=$n
        }
        if ($count -lt 1 -or $count -gt 16384) { throw 'Invalid owned settings size.' }
        $text=[Text.UTF8Encoding]::new($false,$true).GetString($bytes,0,$count)
    } finally { $stream.Dispose() }
    $after=Get-Item -LiteralPath $path -Force -ErrorAction Stop
    if ($after.PSIsContainer -or ($after.Attributes -band [IO.FileAttributes]::ReparsePoint) -or
        $after.Length -ne $count -or $after.Length -ne $item.Length -or
        $after.LastWriteTimeUtc -ne $item.LastWriteTimeUtc) { throw 'Owned settings changed during observation.' }
    $values=[Collections.Generic.Dictionary[string,string]]::new([StringComparer]::Ordinal)
    # The test owns the two ASCII values. Accept either TOML quote style and
    # either field order, never arbitrary expressions, extra keys or duplicates.
    foreach ($line in ($text -csplit '\r?\n')) {
        if ($line -cmatch '^[ \t]*$') { continue }
        $match=[regex]::Match($line,'\A[ \t]*(model|model_reasoning_effort)[ \t]*=[ \t]*(?:"([^"\r\n]*)"|''([^''\r\n]*)'')[ \t]*\z')
        if (-not $match.Success -or $values.ContainsKey($match.Groups[1].Value)) { throw 'Invalid owned settings shape.' }
        $value=$(if($match.Groups[2].Success){$match.Groups[2].Value}else{$match.Groups[3].Value})
        $values.Add($match.Groups[1].Value,$value)
    }
    if ($values.Count -ne 2 -or $values['model'] -cne ('magpie/'+$Owner+'/test-'+$Owner) -or
        $values['model_reasoning_effort'] -cne 'none') { throw 'Owned settings value mismatch.' }
}
function Get-LiveOwnedSettingsOutcome($Prior,$Files,$RPC) {
    if ($Prior.exitCode -ne 0) { return $Prior }
    if ($null -eq $Files -or $null -eq $RPC -or -not $Files.initialVerified -or
        -not $Files.betaVerified -or -not $Files.alphaVerified -or $RPC.status -cne 'observed') {
        return [pscustomobject]@{status='blocked';stage='model-settings-verification-stopped';reason='The owned settings slice lacks two correlated matching successes or its one-time file observations. No version, restart or conversation result is established.';exitCode=1}
    }
    return [pscustomobject]@{status='observed';stage='synthetic-model-settings-observed';reason='Beta then Alpha UI selection, no observed settings-error toast, correlated matching request successes and fixed-file preference observations passed in this VM. No response-body, user-layer version, restart, thread migration or conversation validation.';exitCode=0}
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
  public BoundedProcess(ProcessStartInfo start, int maxBytes, int milliseconds)
   : this(start,maxBytes,milliseconds,null) {}
  // Offline fixture seam: observe the started PID, then fail before the timer exists.
  public static void TestFailureAfterStart(ProcessStartInfo start, Action<int> observeStarted) {
   if(observeStarted==null) throw new ArgumentNullException("observeStarted");
   using(var unexpected=new BoundedProcess(start,8192,30000,observeStarted)) {
    throw new InvalidOperationException("Constructor failure fixture did not fail.");
   }
  }
  BoundedProcess(ProcessStartInfo start, int maxBytes, int milliseconds, Action<int> failAfterStart) {
   if(start==null || maxBytes<=0 || milliseconds<=0) throw new ArgumentException("Invalid bounded process inputs.");
   limit=maxBytes; process=new Process(); process.StartInfo=start; bool started=false;
   try {
    if(!process.Start()) throw new InvalidOperationException("Process did not start.");
    started=true; var held=process.Handle;
    stdout=Task.Run(()=>Drain(process.StandardOutput,true)); stderr=Task.Run(()=>Drain(process.StandardError,false));
    if(failAfterStart!=null) { failAfterStart(process.Id); throw new InvalidOperationException("Injected constructor failure."); }
    timer=new Timer(_=>{ timedOut=true; Stop(); },null,milliseconds,Timeout.Infinite);
   } catch {
    bool reaped=!started;
    try { if(timer!=null) timer.Dispose(); } catch {}
    if(started) {
     try { if(!process.HasExited) process.Kill(); reaped=process.WaitForExit(5000); } catch { reaped=false; }
    }
    if(reaped) {
     try { if(stdout!=null) stdout.Wait(2000); } catch {}
     try { if(stderr!=null) stderr.Wait(2000); } catch {}
    }
    try { process.Dispose(); } catch {}
    throw new InvalidOperationException(reaped
     ? "Bounded process construction failed; owned process exit confirmed."
     : "Bounded process construction failed; owned process exit unconfirmed.");
   }
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
function Wait-LiveReady($Process,[bool]$Build4239=$false,[string]$FixtureProfile='',[string]$SetupProfile='') {
    $readySeconds=25
    if ($SetupProfile) { $readySeconds=235 }
    $watch=[Diagnostics.Stopwatch]::StartNew()
    while ($watch.Elapsed.TotalSeconds -lt $readySeconds -and -not $Process.TimedOut -and -not $Process.Overflow) {
        $text=$Process.Output
        if ($text.Contains("`n")) {
            $lines=@($text.TrimEnd("`r","`n") -split "`r?`n")
            if ($lines.Count -ne 1 -or $Process.Exited) { throw 'Live fixture did not remain ready.' }
            $ready=ConvertFrom-LiveJSON $lines[0]; Assert-LiveReady $ready -Build4239 $Build4239 -FixtureProfile $FixtureProfile -SetupProfile $SetupProfile
            $Process.Deadline([int]$ready.lifetime_seconds * 1000)
            return $ready
        }
        if ($Process.Exited) { break }; Start-Sleep -Milliseconds 50
    }
    throw 'Live fixture readiness unavailable.'
}
function Complete-LiveFixture($Process,[bool]$Build4239=$false,[string]$FixtureProfile='',[string]$SetupProfile='') {
    $Process.CloseInput()
    if (-not $Process.Wait(10000)) { $Process.Stop(); if (-not $Process.Wait(5000)) { throw 'Live helper exit unconfirmed.' } }
    $watch=[Diagnostics.Stopwatch]::StartNew()
    while (-not $Process.Drained -and $watch.Elapsed.TotalSeconds -lt 2) { Start-Sleep -Milliseconds 20 }
    if ($Process.TimedOut -or $Process.Overflow -or -not $Process.Drained) { throw 'Live fixture supervision failed.' }
    $lines=@($Process.Output.TrimEnd("`r","`n") -split "`r?`n")
    if ($lines.Count -ne 2) { throw 'Invalid live fixture records.' }
    Assert-LiveReady (ConvertFrom-LiveJSON $lines[0]) -Build4239 $Build4239 -FixtureProfile $FixtureProfile -SetupProfile $SetupProfile
    $audit=ConvertFrom-LiveJSON $lines[1]; Assert-LiveAudit $audit $Process.ExitCode -FixtureProfile $FixtureProfile -SetupProfile $SetupProfile
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

function Assert-ModelSelectionResult($Value) {
    Assert-LiveShape $Value @('schema','scope','status','reason','selectionAttempts','confirmedSelections','menuReopens',
        'initialAlphaSelected','betaSelected','alphaSelected','betaLabelMatched','alphaLabelMatched','betaCaptured','alphaCaptured','lastObservation','snapshotObservation','failureCapture')
    Assert-LiveInteger $Value.schema 1 1
    Assert-LiveStrings $Value @('scope','status','reason')
    if ($Value.scope -cne 'bridge-owned-current-draft-selection-only' -or $Value.status -cnotin @('observed','stopped') -or
        $Value.reason -cnotin @('selection_profile_unavailable','prior_menu_unavailable','initial_selection_unavailable','selection_read_unavailable',
            'selection_snapshot_base_unavailable','selection_snapshot_names_unavailable','selection_snapshot_target_guard_unavailable','selection_snapshot_scene_unavailable','selection_model_settings_error',
            'preselection_state_unavailable','selection_action_unavailable','selection_receipt_invalid','selected_label_unavailable',
            'selection_reopen_unavailable','selected_pair_unavailable','selection_capture_unavailable','both_current_draft_selections_verified')) {
        throw 'Invalid fixed selection outcome.'
    }
    foreach ($key in @('selectionAttempts','confirmedSelections','menuReopens')) { Assert-LiveInteger $Value.$key 0 2 }
    foreach ($key in @('initialAlphaSelected','betaSelected','alphaSelected','betaLabelMatched','alphaLabelMatched','betaCaptured','alphaCaptured')) {
        if ($Value.$key -isnot [bool]) { throw 'Invalid selection flag.' }
    }
    if ($Value.confirmedSelections -gt $Value.selectionAttempts -or $Value.menuReopens -gt $Value.selectionAttempts) { throw 'Invalid selection counts.' }
    $o=$Value.lastObservation
    Assert-LiveShape $o @('target','matches','controlType','enabled','selectorKind','selectionState','patternConfirmed')
    Assert-LiveStrings $o @('target','controlType','selectorKind','selectionState'); Assert-LiveInteger $o.matches 0 500
    if ($o.target -cnotin @('none','alpha','beta','current_alpha','current_beta') -or
        $o.controlType -cnotin @('absent','ambiguous','RadioButton','Button','MenuItem','other') -or
        $o.selectorKind -cnotin @('not_observed','runtime_slug','promoted_id','unsupported','missing') -or
        $o.selectionState -cnotin @('not_read','unavailable','selected','unselected') -or
        $o.enabled -isnot [bool] -or $o.patternConfirmed -isnot [bool]) { throw 'Invalid bounded selection observation.' }
    $snapshot=$Value.snapshotObservation
    if ($null -ne $snapshot) {
        Assert-LiveShape $snapshot @('stage','baseValidated','statsComplete','headingCount','protectedScreen','modelSettingsErrorCount','names')
        Assert-LiveStrings $snapshot @('stage'); Assert-LiveInteger $snapshot.headingCount 0 500; Assert-LiveInteger $snapshot.modelSettingsErrorCount 0 500
        if ($snapshot.stage -cnotin @('base_schema','name_scan','name_guard','scene','ready','model_settings_error') -or
            $snapshot.baseValidated -isnot [bool] -or $snapshot.statsComplete -isnot [bool] -or $snapshot.protectedScreen -isnot [bool]) {
            throw 'Invalid fixed snapshot stage.'
        }
        Assert-LiveShape $snapshot.names @('alpha','beta','currentAlpha','currentBeta')
        foreach ($name in @('alpha','beta','currentAlpha','currentBeta')) {
            $counts=$snapshot.names.$name
            $fields=@('total','hidden','disabled','directInvokable','radioButton','menuItem','button','text','other')
            Assert-LiveShape $counts $fields
            foreach ($key in $fields) { Assert-LiveInteger $counts.$key 0 500 }
            if ($counts.hidden -gt $counts.total -or $counts.disabled -gt $counts.total -or $counts.directInvokable -gt $counts.total -or
                ($counts.radioButton+$counts.menuItem+$counts.button+$counts.text+$counts.other) -ne $counts.total) { throw 'Inconsistent fixed snapshot counts.' }
        }
        if (($snapshot.stage -cne 'base_schema' -and -not $snapshot.baseValidated) -or
            ($snapshot.stage -cin @('name_guard','scene','ready','model_settings_error') -and -not $snapshot.statsComplete)) { throw 'Incomplete snapshot stage proof.' }
    }
    if ($Value.status -ceq 'observed' -and ($null -eq $snapshot -or $snapshot.stage -cne 'ready' -or
        $snapshot.protectedScreen -or $snapshot.modelSettingsErrorCount -ne 0 -or $snapshot.headingCount -ne 1 -or $snapshot.names.alpha.total -ne 1 -or
        $snapshot.names.beta.total -ne 1)) { throw 'Missing final snapshot validation.' }
    Assert-LiveStrings $Value @('failureCapture')
    if ($Value.failureCapture -cnotin @('not_attempted','unavailable','beta','alpha')) { throw 'Invalid fixed failure capture.' }
    if ($Value.reason -ceq 'selection_model_settings_error') {
        if ($Value.status -cne 'stopped' -or $null -eq $snapshot -or $snapshot.stage -cne 'model_settings_error' -or
            $snapshot.modelSettingsErrorCount -lt 1) { throw 'Application error lacks its fixed observation.' }
    } elseif ($Value.failureCapture -cne 'not_attempted') { throw 'Unexpected failure capture.' }
    if (($Value.failureCapture -ceq 'beta' -and $Value.selectionAttempts -ne 1) -or
        ($Value.failureCapture -ceq 'alpha' -and $Value.selectionAttempts -ne 2) -or
        ($Value.failureCapture -ceq 'unavailable' -and $Value.selectionAttempts -lt 1)) { throw 'Failure capture does not match the attempted selection.' }
    if ($Value.status -ceq 'observed' -and ($Value.reason -cne 'both_current_draft_selections_verified' -or
        $Value.selectionAttempts -ne 2 -or $Value.confirmedSelections -ne 2 -or -not $Value.initialAlphaSelected -or
        -not $Value.betaSelected -or -not $Value.alphaSelected -or -not $Value.betaLabelMatched -or -not $Value.alphaLabelMatched -or
        -not $Value.betaCaptured -or -not $Value.alphaCaptured)) { throw 'Incomplete selection success.' }
}
