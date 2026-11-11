# Read-only worker. Run only through original-connect-discovery.ps1, which owns
# a hard outer deadline. UIA's individual cross-process calls can block.
[CmdletBinding()]
param([int]$TargetProcessId,[long]$TargetStartTicks,[long]$TargetWindow,[string]$TargetExecutable)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
function Assert-DiscoveryContext {
    if ($PSVersionTable.PSEdition -cne 'Core' -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY -cne 'read-only-v1') { throw 'Discovery context unavailable.' }
}
# This pure classifier is AST-loaded by offline tests. Unknown labels never
# become selectors. Counts are observations, not action authorization.
function Get-DiscoveryLabel([string]$Name) {
    switch -CaseSensitive ($Name) {
        'Configure Codex launch selection' { return 'configure' }
        'Disconnect and restore Codex launch selection' { return 'restore' }
        'Disconnect and restore' { return 'confirm' }
        default { return '' }
    }
}
$result=[ordered]@{schema=1;scope='original-magpie-controls-read-only';status='blocked';
    reason='context';nodes=0;counts=$null;actions=0;connectVerified=$false;desktopVerified=$false}
$process=$null
try {
    Assert-DiscoveryContext
    $result.reason='identity'
    if ($TargetProcessId -le 0 -or $TargetWindow -eq 0 -or
        -not [IO.Path]::IsPathFullyQualified($TargetExecutable)) { throw 'Invalid target.' }
    $process=Get-Process -Id $TargetProcessId -ErrorAction Stop
    $null=$process.Handle
    function Assert-DiscoveryOwner {
        $process.Refresh()
        if ($process.HasExited -or $process.StartTime.ToUniversalTime().Ticks -ne $TargetStartTicks -or
            $process.MainWindowHandle.ToInt64() -ne $TargetWindow -or
            $process.MainModule.FileName -ine $TargetExecutable -or $process.MainWindowTitle -cne 'magpie') {
            throw 'Owned Magpie window changed.'
        }
    }
    Assert-DiscoveryOwner
    $result.reason='uia'
    Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes
    $root=[Windows.Automation.AutomationElement]::FromHandle([IntPtr]$TargetWindow)
    if ($null -eq $root -or $root.Current.ProcessId -ne $TargetProcessId) { throw 'Owned root unavailable.' }
    $walker=[Windows.Automation.TreeWalker]::ControlViewWalker
    $queue=[Collections.Generic.Queue[object]]::new()
    $queue.Enqueue([pscustomobject]@{element=$root;depth=0})
    $watch=[Diagnostics.Stopwatch]::StartNew();$nodes=0
    $counts=[ordered]@{}
    foreach ($label in @('configure','restore','confirm')) {
        $counts[$label]=[ordered]@{total=0;enabledVisible=0;invoke=0;toggle=0;button=0;checkbox=0;other=0}
    }
    while ($queue.Count -gt 0) {
        $result.reason='limit'
        if ($watch.ElapsedMilliseconds -gt 10000 -or $nodes -ge 2048) { throw 'Traversal limit.' }
        $result.reason='identity';Assert-DiscoveryOwner;$result.reason='uia'
        $item=$queue.Dequeue();$element=$item.element;$nodes++
        $current=$element.Current;$label=Get-DiscoveryLabel $current.Name
        if ($label) {
            $count=$counts[$label];$count.total++
            if ($current.IsEnabled -and -not $current.IsOffscreen) { $count.enabledVisible++ }
            if ($current.ControlType -eq [Windows.Automation.ControlType]::Button) { $count.button++ }
            elseif ($current.ControlType -eq [Windows.Automation.ControlType]::CheckBox) { $count.checkbox++ }
            else { $count.other++ }
            $pattern=$null
            if ($element.TryGetCurrentPattern([Windows.Automation.InvokePattern]::Pattern,[ref]$pattern)) { $count.invoke++ }
            $pattern=$null
            if ($element.TryGetCurrentPattern([Windows.Automation.TogglePattern]::Pattern,[ref]$pattern)) { $count.toggle++ }
        }
        $child=$walker.GetFirstChild($element)
        $result.reason='limit'
        if ($null -ne $child -and $item.depth -ge 20) { throw 'Depth limit.' }
        while ($null -ne $child) {
            if ($queue.Count+$nodes -ge 2048 -or $watch.ElapsedMilliseconds -gt 10000) { throw 'Traversal limit.' }
            $queue.Enqueue([pscustomobject]@{element=$child;depth=$item.depth+1})
            $result.reason='uia';$child=$walker.GetNextSibling($child);$result.reason='limit'
        }
    }
    $result.reason='identity';Assert-DiscoveryOwner
    $result.status='observed';$result.reason='none';$result.nodes=$nodes;$result.counts=$counts
} catch {
    # Never return exception text, arbitrary UI strings, process paths or logs.
} finally { if ($null -ne $process) { $process.Dispose() } }
$result | ConvertTo-Json -Depth 5 -Compress
