# Windows PowerShell 5.1 Desktop only. Test-only first-screen UIA slice. Parent MUST enforce a 30-second process deadline.
# No raw UI text, paths, exceptions or user data are emitted.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][long]$WindowHandle,
    [Parameter(Mandatory)][int]$OwnerProcessId,
    [Parameter(Mandatory)][long]$OwnerStartUtcTicks,
    [Parameter(Mandatory)][string]$OwnerExecutable,
    [Parameter(Mandatory)][string]$PackageFullName,
    [Parameter(Mandatory)][int]$SessionId
)

# Pure policy, separated for offline fixtures. Inputs are visible UIA properties,
# never screenshots, OCR guesses or coordinate-derived target identities.
function Get-WelcomeDecision($Nodes) {
    $names = @($Nodes | ForEach-Object { $_.Name })
    if (@($names | Where-Object { $_ -match '(?i)\b(sign in|log in|sign up|payment|subscribe|terms of|privacy policy|authorize|permission|connect your|allow access)\b' }).Count) {
        return 'protected_screen'
    }
    if (@($names | Where-Object { $_ -ceq 'Which best describes your work?' }).Count -lt 1) {
        return 'unreviewed_screen'
    }
    $allowed = @('Engineering', 'Data science', 'Product', 'Design', 'Marketing', 'Sales',
        'Finance', 'Operations', 'People & HR', 'Legal', 'Student',
        'Suggest personalized tasks', 'Continue', 'Minimize', 'Maximize', 'Restore', 'Close')
    $knownText = $allowed + @('', 'ChatGPT', 'Hey!', 'Which best describes your work?',
        'Describe something else',
        "Welcome to the ChatGPT desktop app. I'm going to ask you a few quick questions to help me tailor things for how you work.")
    foreach ($node in $Nodes) {
        $normalized = ($node.Name -replace '[\u2018\u2019]', "'" -replace '\s+', ' ').Trim()
        if ($knownText -cnotcontains $normalized) { return 'unreviewed_text' }
        if ($node.Interactive -and $node.Enabled -and $allowed -cnotcontains $node.Name) { return 'unreviewed_control' }
    }
    foreach ($spec in @(@('Engineering','RadioButton'), @('Suggest personalized tasks','CheckBox'), @('Continue','Button'))) {
        $matches = @($Nodes | Where-Object { $_.Name -ceq $spec[0] -and $_.Type -ceq $spec[1] })
        $wrongType = @($Nodes | Where-Object { $_.Name -ceq $spec[0] -and $_.Interactive -and $_.Type -cne $spec[1] })
        if ($matches.Count -ne 1 -or $wrongType.Count -ne 0) { return 'ambiguous_control' }
    }
    return 'welcome'
}

# Diagnostic facts from the existing snapshot only. They never authorize an action.
function Get-WelcomeTargetObservations($Nodes) {
    $observations = [ordered]@{}
    foreach ($spec in @(@('engineering','Engineering'),
        @('suggestions','Suggest personalized tasks'), @('continue','Continue'))) {
        $matches = @($Nodes | Where-Object { $_.Interactive -and $_.Name -ceq $spec[1] })
        if ($matches.Count -gt 500) { throw 'Invalid bounded target snapshot.' }
        $type = 'absent'
        if ($matches.Count -gt 1) { $type = 'ambiguous' }
        elseif ($matches.Count -eq 1) {
            $type = 'other'
            if (@('Button','RadioButton','CheckBox','Edit','ComboBox','Hyperlink',
                'MenuItem','ListItem','TabItem','Slider','Spinner','SplitButton') -ccontains $matches[0].Type) {
                $type = $matches[0].Type
            }
        }
        $observations[$spec[0]] = [ordered]@{ interactiveCount = $matches.Count; controlType = $type }
    }
    return $observations
}

# Pure ownership proof over already-read process facts. Every ancestor must be
# live and identity-verified, with unchanged parent/start facts, up to the root.
function Test-WelcomeLineage($Facts, [int]$RootId, [int]$NodeId) {
    if ($RootId -le 0 -or $NodeId -le 0 -or $Facts.Count -gt 32) { return $false }
    $seen = @{}
    for ($depth = 0; $depth -le 8; $depth++) {
        if ($seen.ContainsKey($NodeId) -or -not $Facts.ContainsKey($NodeId)) { return $false }
        $seen[$NodeId] = $true
        $fact = $Facts[$NodeId]
        if (-not $fact.Alive -or -not $fact.IdentityValid -or $fact.StartTicks -le 0 -or
            $fact.StartTicks -ne $fact.ActualStartTicks) { return $false }
        if ($NodeId -eq $RootId) { return $true }
        if ($fact.ParentId -le 0 -or $fact.ParentId -ne $fact.CurrentParentId -or
            -not $Facts.ContainsKey([int]$fact.ParentId)) { return $false }
        if ($Facts[[int]$fact.ParentId].StartTicks -ge $fact.StartTicks) { return $false }
        $NodeId = [int]$fact.ParentId
    }
    return $false
}

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$result = [ordered]@{ schema = 1; scope = 'synthetic-welcome-only'; status = 'stopped';
    reason = 'uia_provider_error'; stage = 'initialization'; actions = 0; suggestionsDisabled = $false;
    engineeringSelected = $false; continueInvoked = $false; nextScreen = 'not_observed'; targetObservations = $null }
$owner = $null
$heldProviders = @{}
# Only our own fixed labels reach the report. Never classify provider exception text.
function Stop-Welcome([string]$Reason) {
    $result.reason = $Reason
    throw 'Welcome helper stopped.'
}
try {
    if ($PSVersionTable.PSEdition -cne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or
        $PSVersionTable.PSVersion.Minor -ne 1) { Stop-Welcome 'unsupported_runtime' }
    if ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_OS -cne 'Windows' -or
        $WindowHandle -le 0 -or $OwnerProcessId -le 0 -or $OwnerStartUtcTicks -le 0 -or
        $SessionId -le 0 -or -not [IO.Path]::IsPathRooted($OwnerExecutable) -or
        [string]::IsNullOrWhiteSpace($PackageFullName)) { Stop-Welcome 'invalid_context' }
    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
    Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public static class WelcomeNative {
 [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint p);
 [DllImport("kernel32.dll", CharSet=CharSet.Unicode)] static extern int GetPackageFullName(IntPtr p, ref uint n, StringBuilder s);
 [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)]
 struct ProcessEntry {
   public uint Size, Usage, ProcessId; public UIntPtr DefaultHeap;
   public uint ModuleId, Threads, ParentId; public int BasePriority; public uint Flags;
   [MarshalAs(UnmanagedType.ByValTStr, SizeConst=260)] public string Exe;
 }
 [DllImport("kernel32.dll", SetLastError=true)] static extern IntPtr CreateToolhelp32Snapshot(uint flags, uint pid);
 [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true, ExactSpelling=true)] static extern bool Process32FirstW(IntPtr h, ref ProcessEntry e);
 [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true, ExactSpelling=true)] static extern bool Process32NextW(IntPtr h, ref ProcessEntry e);
 [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr h);
 public static Dictionary<int,int> Parents() {
   IntPtr h=CreateToolhelp32Snapshot(2,0);
   if(h==new IntPtr(-1)) throw new Exception();
   try {
     var e=new ProcessEntry(); e.Size=(uint)Marshal.SizeOf(typeof(ProcessEntry));
     var result=new Dictionary<int,int>();
     if(!Process32FirstW(h,ref e)) throw new Exception();
     do {
       if(result.Count>=8192 || e.ProcessId>Int32.MaxValue || e.ParentId>Int32.MaxValue) throw new Exception();
       result.Add((int)e.ProcessId,(int)e.ParentId);
     } while(Process32NextW(h,ref e));
     if(Marshal.GetLastWin32Error()!=18) throw new Exception();
     return result;
   } finally { CloseHandle(h); }
 }
 public static string Package(IntPtr p) {
   uint n=0; if(GetPackageFullName(p,ref n,null)!=122 || n<1 || n>4096) throw new Exception();
   var b=new StringBuilder((int)n); if(GetPackageFullName(p,ref n,b)!=0) throw new Exception(); return b.ToString();
 }
}
'@
    $result.stage = 'ownership'
    $owner = Get-Process -Id $OwnerProcessId
    # Hold the original OS handle throughout; never target a replacement process.
    $heldHandle = $owner.Handle
    $deadline = [Diagnostics.Stopwatch]::StartNew()
    $heldProviders[$OwnerProcessId] = [pscustomobject]@{
        Process = $owner; Handle = $heldHandle; StartTicks = $OwnerStartUtcTicks; ParentId = 0
    }
    function Assert-ProviderLineage([int]$ProviderId) {
        try {
            if ($ProviderId -le 0) { Stop-Welcome 'uia_owner_mismatch' }
            $parents = [WelcomeNative]::Parents()
            $nodeId = $ProviderId; $seenChain = @{}
            for ($depth = 0; $depth -le 8; $depth++) {
                if ($deadline.Elapsed.TotalSeconds -ge 20) { Stop-Welcome 'deadline_exceeded' }
                if ($seenChain.ContainsKey($nodeId) -or -not $parents.ContainsKey($nodeId)) { Stop-Welcome 'uia_owner_mismatch' }
                $seenChain[$nodeId] = $true
                if (-not $heldProviders.ContainsKey($nodeId)) {
                    if ($heldProviders.Count -ge 32) { Stop-Welcome 'uia_owner_mismatch' }
                    $process = Get-Process -Id $nodeId
                    # Store immediately so every acquired Process is disposed, even on failure.
                    $record = [pscustomobject]@{ Process = $process; Handle = [IntPtr]::Zero; StartTicks = 0L; ParentId = $parents[$nodeId] }
                    $heldProviders[$nodeId] = $record
                    $record.Handle = $process.Handle
                    $record.StartTicks = $process.StartTime.ToUniversalTime().Ticks
                }
                if ($nodeId -eq $OwnerProcessId) { break }
                $nodeId = [int]$parents[$nodeId]
                if ($nodeId -le 0) { Stop-Welcome 'uia_owner_mismatch' }
            }
            # Second snapshot after handles were pinned detects changed parent edges.
            $parents = [WelcomeNative]::Parents()
            $facts = @{}
            foreach ($entry in $heldProviders.GetEnumerator()) {
                if ($deadline.Elapsed.TotalSeconds -ge 20) { Stop-Welcome 'deadline_exceeded' }
                $record = $entry.Value; $process = $record.Process; $process.Refresh()
                if ($process.HasExited -or -not $parents.ContainsKey([int]$entry.Key)) { Stop-Welcome 'uia_owner_mismatch' }
                $facts[[int]$entry.Key] = [pscustomobject]@{
                    Alive = $true; StartTicks = $record.StartTicks
                    ActualStartTicks = $process.StartTime.ToUniversalTime().Ticks
                    ParentId = $record.ParentId; CurrentParentId = $parents[[int]$entry.Key]
                    IdentityValid = ($process.SessionId -eq $SessionId -and
                        [string]::Equals($process.MainModule.FileName, $OwnerExecutable, [StringComparison]::OrdinalIgnoreCase) -and
                        [WelcomeNative]::Package($record.Handle) -ceq $PackageFullName)
                }
            }
            # A cached ancestor or provider is never trusted just because it was seen before.
            foreach ($id in @($heldProviders.Keys)) {
                if (-not (Test-WelcomeLineage $facts $OwnerProcessId ([int]$id))) { Stop-Welcome 'uia_owner_mismatch' }
            }
        } catch {
            if ($result.reason -ceq 'deadline_exceeded') { throw 'Welcome helper stopped.' }
            Stop-Welcome 'uia_owner_mismatch'
        }
    }
    function Assert-OwnedForeground([switch]$RootOnly) {
        if ($deadline.Elapsed.TotalSeconds -ge 20) { Stop-Welcome 'deadline_exceeded' }
        $owner.Refresh()
        $windowPid = [uint32]0
        [void][WelcomeNative]::GetWindowThreadProcessId([IntPtr]$WindowHandle, [ref]$windowPid)
        if ($owner.HasExited -or $owner.StartTime.ToUniversalTime().Ticks -ne $OwnerStartUtcTicks -or
            $owner.SessionId -ne $SessionId -or (Get-Process -Id $PID).SessionId -ne $SessionId -or
            -not [string]::Equals($owner.MainModule.FileName, $OwnerExecutable, [StringComparison]::OrdinalIgnoreCase) -or
            [WelcomeNative]::Package($heldHandle) -cne $PackageFullName -or
            $windowPid -ne $OwnerProcessId -or -not [WelcomeNative]::IsWindowVisible([IntPtr]$WindowHandle) -or
            [WelcomeNative]::GetForegroundWindow().ToInt64() -ne $WindowHandle) { Stop-Welcome 'identity_or_foreground_changed' }
        if (-not $RootOnly) { Assert-ProviderLineage $OwnerProcessId }
    }
    function Read-VisibleNodes {
        Assert-OwnedForeground
        $root = [Windows.Automation.AutomationElement]::FromHandle([IntPtr]$WindowHandle)
        $walker = [Windows.Automation.TreeWalker]::ControlViewWalker
        $queue = New-Object 'System.Collections.Generic.Queue[object]'
        $queue.Enqueue(@($root, 0))
        $nodes = New-Object 'System.Collections.Generic.List[object]'
        $seen = 0
        while ($queue.Count -gt 0) {
            Assert-OwnedForeground -RootOnly
            $item = $queue.Dequeue(); $element = $item[0]; $depth = [int]$item[1]
            $seen++
            if ($seen -gt 500 -or $depth -gt 20) { Stop-Welcome 'uia_tree_limit' }
            $current = $element.Current
            if (-not $current.IsOffscreen) {
                if (-not $heldProviders.ContainsKey([int]$current.ProcessId)) { Assert-ProviderLineage ([int]$current.ProcessId) }
                $type = $current.ControlType.ProgrammaticName.Replace('ControlType.', '')
                $interactive = $type -in @('Button','RadioButton','CheckBox','Edit','ComboBox','Hyperlink','MenuItem','ListItem','TabItem','Slider','Spinner','SplitButton')
                $nodes.Add([pscustomobject]@{ Name = $current.Name; Type = $type;
                    Interactive = $interactive; Enabled = $current.IsEnabled; Element = $element })
            }
            $child = $walker.GetFirstChild($element)
            while ($null -ne $child) {
                if ($queue.Count + $seen -ge 500) { Stop-Welcome 'uia_tree_limit' }
                $queue.Enqueue(@($child, ($depth + 1)))
                $child = $walker.GetNextSibling($child)
            }
        }
        Assert-OwnedForeground
        return $nodes.ToArray()
    }
    function Get-ReviewedWelcome {
        $nodes = @(Read-VisibleNodes)
        $result.targetObservations = Get-WelcomeTargetObservations $nodes
        $decision = Get-WelcomeDecision $nodes
        if ($decision -cne 'welcome') { Stop-Welcome $decision }
        return $nodes
    }
    function Get-Target($Nodes, [string]$Name, [string]$Type) {
        $wrongType = @($Nodes | Where-Object { $_.Name -ceq $Name -and $_.Interactive -and $_.Type -cne $Type })
        if ($wrongType.Count -ne 0) { Stop-Welcome 'target_unavailable' }
        $target = @($Nodes | Where-Object { $_.Name -ceq $Name -and $_.Type -ceq $Type })
        if ($target.Count -ne 1 -or -not $target[0].Enabled) { Stop-Welcome 'target_unavailable' }
        Assert-ProviderLineage ([int]$target[0].Element.Current.ProcessId)
        return $target[0].Element
    }
    $result.stage = 'welcome_read'
    $nodes = @(Get-ReviewedWelcome)
    $result.stage = 'suggestions_off'
    $checkbox = Get-Target $nodes 'Suggest personalized tasks' 'CheckBox'
    $toggle = $checkbox.GetCurrentPattern([Windows.Automation.TogglePattern]::Pattern)
    if ($toggle.Current.ToggleState -eq [Windows.Automation.ToggleState]::Indeterminate) { Stop-Welcome 'suggestions_not_off' }
    if ($toggle.Current.ToggleState -eq [Windows.Automation.ToggleState]::On) {
        Assert-OwnedForeground
        Assert-ProviderLineage ([int]$checkbox.Current.ProcessId)
        Assert-OwnedForeground -RootOnly
        $result.actions++; $toggle.Toggle() # One attempt only; a thrown call is an uncertain action.
    }
    Start-Sleep -Milliseconds 300
    $nodes = @(Get-ReviewedWelcome)
    $toggle = (Get-Target $nodes 'Suggest personalized tasks' 'CheckBox').GetCurrentPattern([Windows.Automation.TogglePattern]::Pattern)
    if ($toggle.Current.ToggleState -ne [Windows.Automation.ToggleState]::Off) { Stop-Welcome 'suggestions_not_off' }
    $result.suggestionsDisabled = $true
    $result.stage = 'engineering_select'
    $radioElement = Get-Target $nodes 'Engineering' 'RadioButton'
    $radio = $radioElement.GetCurrentPattern([Windows.Automation.SelectionItemPattern]::Pattern)
    if (-not $radio.Current.IsSelected) {
        Assert-OwnedForeground
        Assert-ProviderLineage ([int]$radioElement.Current.ProcessId)
        Assert-OwnedForeground -RootOnly
        $result.actions++; $radio.Select()
    }
    Start-Sleep -Milliseconds 300
    $nodes = @(Get-ReviewedWelcome)
    $radioElement = Get-Target $nodes 'Engineering' 'RadioButton'
    $radio = $radioElement.GetCurrentPattern([Windows.Automation.SelectionItemPattern]::Pattern)
    $toggle = (Get-Target $nodes 'Suggest personalized tasks' 'CheckBox').GetCurrentPattern([Windows.Automation.TogglePattern]::Pattern)
    if (-not $radio.Current.IsSelected -or $toggle.Current.ToggleState -ne [Windows.Automation.ToggleState]::Off) { Stop-Welcome 'selection_state_changed' }
    $result.engineeringSelected = $true
    $result.stage = 'continue_invoke'
    $buttonElement = Get-Target $nodes 'Continue' 'Button'
    $button = $buttonElement.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern)
    Assert-OwnedForeground
    Assert-ProviderLineage ([int]$buttonElement.Current.ProcessId)
    Assert-OwnedForeground -RootOnly
    $result.actions++; $button.Invoke()
    $result.continueInvoked = $true
    $result.stage = 'next_screen_read'
    # Inspect once, never retry Continue or act on an unreviewed second page.
    Start-Sleep -Milliseconds 1000
    $after = @(Read-VisibleNodes)
    $decision = Get-WelcomeDecision $after
    $result.nextScreen = switch ($decision) {
        'protected_screen' { 'protected_screen' }
        'welcome' { 'welcome_still_visible' }
        default { 'unreviewed_screen' }
    }
    $result.stage = 'complete'
    $result.status = 'observed'
    $result.reason = 'single_transition_attempted'
} catch {
    # Unknown provider/runtime exceptions retain uia_provider_error and fixed stage.
    # An action may already have occurred; attempts are never retried.
} finally {
    foreach ($entry in $heldProviders.Values) {
        try { $entry.Process.Dispose() } catch { $result.status = 'stopped'; $result.reason = 'owner_release_failed' }
    }
    if ($null -ne $owner -and -not $heldProviders.ContainsKey($OwnerProcessId)) {
        try { $owner.Dispose() } catch { $result.status = 'stopped'; $result.reason = 'owner_release_failed' }
    }
}
$result | ConvertTo-Json -Depth 5 -Compress
