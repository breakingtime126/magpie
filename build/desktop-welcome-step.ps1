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

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$result = [ordered]@{ schema = 1; scope = 'synthetic-welcome-only'; status = 'stopped';
    reason = 'uia_provider_error'; stage = 'initialization'; actions = 0; suggestionsDisabled = $false;
    engineeringSelected = $false; continueInvoked = $false; nextScreen = 'not_observed'; targetObservations = $null }
$owner = $null
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
using System.Runtime.InteropServices;
public static class WelcomeNative {
 [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint p);
 [DllImport("kernel32.dll", CharSet=CharSet.Unicode)] static extern int GetPackageFullName(IntPtr p, ref uint n, StringBuilder s);
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
    function Assert-OwnedForeground {
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
            Assert-OwnedForeground
            $item = $queue.Dequeue(); $element = $item[0]; $depth = [int]$item[1]
            $seen++
            if ($seen -gt 500 -or $depth -gt 20) { Stop-Welcome 'uia_tree_limit' }
            $current = $element.Current
            if (-not $current.IsOffscreen) {
                if ($current.ProcessId -ne $OwnerProcessId) { Stop-Welcome 'uia_owner_mismatch' }
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
        $result.actions++; $toggle.Toggle() # One attempt only; a thrown call is an uncertain action.
    }
    Start-Sleep -Milliseconds 300
    $nodes = @(Get-ReviewedWelcome)
    $toggle = (Get-Target $nodes 'Suggest personalized tasks' 'CheckBox').GetCurrentPattern([Windows.Automation.TogglePattern]::Pattern)
    if ($toggle.Current.ToggleState -ne [Windows.Automation.ToggleState]::Off) { Stop-Welcome 'suggestions_not_off' }
    $result.suggestionsDisabled = $true
    $result.stage = 'engineering_select'
    $radio = (Get-Target $nodes 'Engineering' 'RadioButton').GetCurrentPattern([Windows.Automation.SelectionItemPattern]::Pattern)
    if (-not $radio.Current.IsSelected) {
        Assert-OwnedForeground
        $result.actions++; $radio.Select()
    }
    Start-Sleep -Milliseconds 300
    $nodes = @(Get-ReviewedWelcome)
    $radio = (Get-Target $nodes 'Engineering' 'RadioButton').GetCurrentPattern([Windows.Automation.SelectionItemPattern]::Pattern)
    $toggle = (Get-Target $nodes 'Suggest personalized tasks' 'CheckBox').GetCurrentPattern([Windows.Automation.TogglePattern]::Pattern)
    if (-not $radio.Current.IsSelected -or $toggle.Current.ToggleState -ne [Windows.Automation.ToggleState]::Off) { Stop-Welcome 'selection_state_changed' }
    $result.engineeringSelected = $true
    $result.stage = 'continue_invoke'
    $button = (Get-Target $nodes 'Continue' 'Button').GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern)
    Assert-OwnedForeground
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
    if ($null -ne $owner) {
        try { $owner.Dispose() } catch { $result.status = 'stopped'; $result.reason = 'owner_release_failed' }
    }
}
$result | ConvertTo-Json -Depth 5 -Compress
