# CI-only native package entry helpers. Dot-sourcing has no side effects.
function Get-EntryVariableNames { return @('CODEX_CLI_PATH', 'MAGPIE_APP_SERVER_BRIDGE_CONFIG') }
function Assert-EntryOptions([string]$Case, [string]$Mode, [bool]$OtherOptIn, [string]$WorkflowRef) {
    $modes = @{ baseline='baseline'; treatment='bridge'; 'restore-before-activation'='bridge'; 'alias-control'='bridge' }
    if (-not $modes.ContainsKey($Case) -or $modes[$Case] -cne $Mode -or $OtherOptIn -or
        $WorkflowRef -cne 'breakingtime126/magpie/.github/workflows/experimental-desktop-native-entry.yml@refs/heads/experiment/app-server-bridge-v0.1.3035') {
        throw 'Native entry requires its dedicated disposable workflow and exact case, without other opt-ins.'
    }
}
function Get-EntryRegistrySnapshot([Microsoft.Win32.RegistryKey]$Key, [switch]$RequireAbsent) {
    if ($null -eq $Key) { throw 'Environment registry key is unavailable.' }
    $names = @($Key.GetValueNames())
    if ($RequireAbsent) {
        foreach ($name in Get-EntryVariableNames) {
            if ($names -icontains $name) { throw 'Unexpected user environment override.' }
        }
    }
    $snapshot = @{}
    foreach ($name in Get-EntryVariableNames) {
        $present = $names -icontains $name
        $snapshot[$name] = [pscustomobject]@{ Present=$present; Kind=$null; Value=$null }
        if ($present) {
            $snapshot[$name].Kind = $Key.GetValueKind($name)
            if ($snapshot[$name].Kind -notin @([Microsoft.Win32.RegistryValueKind]::String,
                [Microsoft.Win32.RegistryValueKind]::ExpandString)) { throw 'Unexpected user environment value type.' }
            $snapshot[$name].Value = $Key.GetValue($name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        }
    }
    return $snapshot
}
function Read-EntryUserEnvironment([switch]$RequireAbsent) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $false)
    try { return Get-EntryRegistrySnapshot $key -RequireAbsent:$RequireAbsent } finally { if ($null -ne $key) { $key.Dispose() } }
}
function Write-EntryUserEnvironment([string]$Name, $Item) {
    if (@(Get-EntryVariableNames) -cnotcontains $Name) { throw 'Unreviewed environment name.' }
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try {
        if ($null -eq $key) { throw 'Environment registry key is unavailable.' }
        if ($Item.Present) { $key.SetValue($Name, $Item.Value, $Item.Kind) }
        else { $key.DeleteValue($Name, $false) }
    } finally { if ($null -ne $key) { $key.Dispose() } }
}
function Test-EntrySnapshotsEqual($Left, $Right) {
    foreach ($name in Get-EntryVariableNames) {
        if ($null -eq $Left[$name] -or $null -eq $Right[$name] -or
            $Left[$name].Present -ne $Right[$name].Present) { return $false }
        if ($Left[$name].Present -and ($Left[$name].Kind -ne $Right[$name].Kind -or
            $Left[$name].Value -cne $Right[$name].Value)) { return $false }
    }
    return $true
}
function Assert-EntryFresh($UserSnapshot, [bool]$ProcessOverride, [bool]$MachineOverride,
    [bool]$ExistingProfile, [bool]$ExistingProcess) {
    foreach ($name in Get-EntryVariableNames) {
        # Do not overwrite even an empty or expandable prior value. No prior values are published.
        if ($null -eq $UserSnapshot[$name] -or $UserSnapshot[$name].Present) { throw 'Unexpected user environment override.' }
    }
    if ($ProcessOverride -or $MachineOverride -or $ExistingProfile -or $ExistingProcess) {
        throw 'Native entry requires an untouched disposable account and no prior Desktop process.'
    }
}
function Test-EntryProcessOverride {
    $variables = [Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
    foreach ($name in Get-EntryVariableNames) { if ($variables.Contains($name)) { return $true } }
    return $false
}
function Test-EntryMachineOverride {
    $key = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey('SYSTEM\CurrentControlSet\Control\Session Manager\Environment', $false)
    try {
        if ($null -eq $key) { throw 'Machine environment is unavailable.' }
        $names = @($key.GetValueNames())
        foreach ($name in Get-EntryVariableNames) { if ($names -icontains $name) { return $true } }
        return $false
    } finally { if ($null -ne $key) { $key.Dispose() } }
}
function Invoke-EntryBoundedHelper($Start, [int]$Milliseconds) {
    # Reuse the existing byte-bounded held-process supervisor; this selects no welcome behavior.
    . (Join-Path $PSScriptRoot 'desktop-live-welcome.ps1')
    Initialize-LiveSupervisor
    $child=$null
    try {
        $child=[DesktopLive.BoundedProcess]::new($Start,512,$Milliseconds)
        if (-not $child.Wait($Milliseconds)) {
            $child.Stop()
            if (-not $child.Wait(5000)) { $script:entryHelperExitUnconfirmed=$true; throw 'Native entry helper exit unconfirmed.' }
        }
        if (-not $child.Exited) { $script:entryHelperExitUnconfirmed=$true; throw 'Native entry helper exit unconfirmed.' }
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while (-not $child.Drained -and $watch.Elapsed.TotalSeconds -lt 2) { Start-Sleep -Milliseconds 20 }
        if (-not $child.Drained -or $child.TimedOut -or $child.Overflow -or $child.ExitCode -ne 0) {
            throw 'Native entry helper failed or exceeded its deadline/output bound.'
        }
        return $child.Output.Trim()
    } finally {
        if ($null -ne $child) {
            $child.Stop()
            if (-not $child.Wait(5000) -or -not $child.Exited) { $script:entryHelperExitUnconfirmed=$true }
            $child.Dispose()
        }
    }
}
function Send-EntryEnvironmentChange {
    # SendMessageTimeout's timeout is per window, not total. Bound the helper instead.
    $start = New-EntryActivationStart (Join-Path $PSScriptRoot 'desktop-entry-activate.ps1') 'OpenAI.Codex_test!Test'
    $start.Arguments = '-NoProfile -NonInteractive -File "' + (Join-Path $PSScriptRoot 'desktop-entry-activate.ps1') + '" -NotifyEnvironment'
    try {
        $text=Invoke-EntryBoundedHelper $start 10000
        if ($text -ceq 'sent') { return 'sent' }
    } catch { }
    return 'unconfirmed'
}
function New-EntryTransaction($Before) {
    return [pscustomobject]@{ Before=$Before; MayHaveChanged=$false; SetVerified=$false;
        SetBroadcast='not-attempted'; RestoreVerified=$false; RestoreBroadcast='not-attempted' }
}
function Set-EntryOverrides($State, $Values, [scriptblock]$Read, [scriptblock]$Write, [scriptblock]$Broadcast) {
    if (-not (Test-EntrySnapshotsEqual $State.Before (& $Read))) { throw 'Environment changed before mutation.' }
    $expected = @{}
    foreach ($name in Get-EntryVariableNames) {
        if ($Values[$name] -isnot [string] -or [string]::IsNullOrWhiteSpace($Values[$name]) -or
            $Values[$name].Contains([char]0)) { throw 'Invalid owned override path.' }
        $expected[$name] = [pscustomobject]@{ Present=$true; Kind=[Microsoft.Win32.RegistryValueKind]::String; Value=$Values[$name] }
    }
    # Mark before the first write: an operation may throw after changing state.
    $State.MayHaveChanged = $true
    foreach ($name in Get-EntryVariableNames) { $null = & $Write $name $expected[$name] }
    if (-not (Test-EntrySnapshotsEqual $expected (& $Read))) { throw 'Override readback mismatch.' }
    $State.SetVerified = $true
    $State.SetBroadcast = & $Broadcast
}
function Restore-EntryOverrides($State, [scriptblock]$Read, [scriptblock]$Write, [scriptblock]$Broadcast) {
    if ($State.MayHaveChanged) {
        $writeFailed = $false
        # Try both even if one write throws. Readback, never lack of an exception, proves restoration.
        foreach ($name in Get-EntryVariableNames) {
            try { $null = & $Write $name $State.Before[$name] } catch { $writeFailed = $true }
        }
        try { $State.RestoreVerified = Test-EntrySnapshotsEqual $State.Before (& $Read) }
        catch { $State.RestoreVerified = $false }
        try { $State.RestoreBroadcast = & $Broadcast } catch { $State.RestoreBroadcast = 'unconfirmed' }
        if ($writeFailed -or -not $State.RestoreVerified) { throw 'Persistent environment restoration was not fully confirmed.' }
    } else {
        $State.RestoreVerified = Test-EntrySnapshotsEqual $State.Before (& $Read)
        if (-not $State.RestoreVerified) { throw 'Untouched environment changed during probe.' }
    }
}
function New-EntryActivationStart([string]$Script, [string]$Aumid) {
    if ($Aumid -cnotmatch '^OpenAI\.Codex_[a-z0-9]+![A-Za-z0-9._-]+$' -or $Script.Contains('"')) {
        throw 'Invalid verified activation target.'
    }
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $start.Arguments = '-NoProfile -NonInteractive -File "' + $Script + '" -Aumid "' + $Aumid + '"'
    $start.UseShellExecute=$false; $start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true; $start.RedirectStandardError=$true
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','USERPROFILE','APPDATA','LOCALAPPDATA','TEMP','TMP',
        'COMSPEC','HOMEDRIVE','HOMEPATH','USERNAME','USERDOMAIN','ALLUSERSPROFILE','PROGRAMDATA',
        'PROGRAMFILES','PROGRAMFILES(X86)','COMMONPROGRAMFILES','COMMONPROGRAMFILES(X86)',
        'PROCESSOR_ARCHITECTURE','NUMBER_OF_PROCESSORS','OS','SESSIONNAME')) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if ($value) { $start.EnvironmentVariables[$name] = $value }
    }
    # Deliberately no CODEX_CLI_PATH, bridge config, CODEX_HOME, runner metadata, or tokens.
    return $start
}
function Invoke-EntryActivation([string]$Script, [string]$Aumid) {
    if (Test-EntryProcessOverride) { throw 'Activation caller acquired an override.' }
    $start = New-EntryActivationStart $Script $Aumid
    $text=Invoke-EntryBoundedHelper $start 20000
    $result = $text | ConvertFrom-Json
    if (@($result.PSObject.Properties).Count -ne 3 -or $result.hresult -isnot [int] -or
        $result.processId -isnot [int] -or $result.processId -le 0 -or $result.hresult -ne 0 -or
        $result.callerOverridesAbsent -isnot [bool] -or -not $result.callerOverridesAbsent) { throw 'Invalid activation helper result.' }
    return $result
}

function Get-EntryOutcome([string]$Case, [int]$BridgeStarts, [int]$VerifiedBridgeImages, [int]$NativeImages,
    [bool]$ActivationExited, [bool]$RegistryRestored, [bool]$CleanupError, [bool]$CaptureSucceeded) {
    if (-not $RegistryRestored -or -not $ActivationExited -or $CleanupError) { return 'cleanup-unconfirmed' }
    if (-not $CaptureSucceeded) { return 'capture-failed' }
    if ($Case -cin @('baseline','restore-before-activation')) {
        if ($BridgeStarts -gt 0 -or $VerifiedBridgeImages -gt 0) { return 'unexpected-bridge' }
        if ($NativeImages -gt 0) { return 'original-native-observed' }
        return 'native-not-observed'
    }
    if ($VerifiedBridgeImages -gt 0) { return 'bridge-image-observed' }
    if ($BridgeStarts -gt 0) { return 'bridge-start-event-only' }
    if ($NativeImages -gt 0) { return 'original-native-observed-no-bridge' }
    return 'inheritance-unknown'
}
function Initialize-EntryImageObserver {
    if ('DesktopEntryImages.Observer' -as [type]) { return }
    Add-Type -ReferencedAssemblies System.Management -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Management;
using System.Threading;
namespace DesktopEntryImages {
 public sealed class Image {
  public int ProcessId, ParentId; public long CreatedTicks, ParentCreatedTicks; public string Path;
 }
 public sealed class Observer : IDisposable {
  readonly object gate=new object(); readonly List<Image> images=new List<Image>();
  readonly ManagementEventWatcher watcher; readonly string bridge, native;
  bool stopped; int misses, active; bool full;
  public Observer(string bridgePath,string nativePath) {
   bridge=bridgePath; native=nativePath;
   watcher=new ManagementEventWatcher(new WqlEventQuery("SELECT * FROM Win32_ProcessStartTrace"));
   watcher.EventArrived+=Capture;
   try {watcher.Start();} catch {watcher.EventArrived-=Capture;watcher.Dispose();throw;}
  }
  void Capture(object sender,EventArrivedEventArgs args) {
   lock(gate) {if(stopped)return;active++;}
   try {
    string name=(string)args.NewEvent["ProcessName"];
    if(!String.Equals(name,System.IO.Path.GetFileName(bridge),StringComparison.OrdinalIgnoreCase) &&
       !String.Equals(name,System.IO.Path.GetFileName(native),StringComparison.OrdinalIgnoreCase))return;
    int id=Convert.ToInt32(args.NewEvent["ProcessID"]), parentId=Convert.ToInt32(args.NewEvent["ParentProcessID"]);
    using(var child=Process.GetProcessById(id)) using(var parent=Process.GetProcessById(parentId)) {
     var childHandle=child.Handle; var parentHandle=parent.Handle;
     string path=child.MainModule.FileName;
     if(!String.Equals(path,bridge,StringComparison.OrdinalIgnoreCase) &&
        !String.Equals(path,native,StringComparison.OrdinalIgnoreCase))return;
     var image=new Image {ProcessId=id,ParentId=parentId,Path=path,
      CreatedTicks=child.StartTime.ToUniversalTime().Ticks,ParentCreatedTicks=parent.StartTime.ToUniversalTime().Ticks};
     if(child.HasExited || parent.HasExited)throw new InvalidOperationException();
     lock(gate) { if(stopped)return; if(images.Count>=16) {full=true;return;} images.Add(image); }
    }
   } catch { lock(gate) { if(misses<65536)misses++; } }
   finally {lock(gate) {active--;Monitor.PulseAll(gate);}}
  }
  public Image[] Snapshot() {lock(gate)return images.ToArray();}
  public int Misses {get {lock(gate)return misses;}}
  public bool Full {get {lock(gate)return full;}}
  public void Dispose() {
   lock(gate) {if(stopped)return;stopped=true;}
   watcher.EventArrived-=Capture;
   try {try {watcher.Stop();} finally {watcher.Dispose();}}
   finally {
    var clock=Stopwatch.StartNew();
    lock(gate) {while(active>0) {
     int remaining=5000-(int)clock.ElapsedMilliseconds;
     if(remaining<=0)throw new InvalidOperationException("image observer callbacks unconfirmed");
     Monitor.Wait(gate,remaining);
    }}
   }
  }
 }
}
'@
}
