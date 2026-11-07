# CI-only bounded welcome orchestration. Uses the signed official CLI, not a PS5 UIA child.
# The existing policy is imported without executing its native/helper body.
$welcomePolicyTokens=$null; $welcomePolicyErrors=$null
$welcomePolicyAst=[Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'desktop-welcome-step.ps1'),[ref]$welcomePolicyTokens,[ref]$welcomePolicyErrors)
if ($welcomePolicyErrors.Count) { throw 'Welcome policy syntax unavailable.' }
foreach ($welcomePolicyName in @('Get-WelcomeDecision','Get-WelcomeChromeRejection',
    'Get-WelcomeTargetObservations','Get-WelcomeInitialObservation')) {
    $welcomePolicyDefinition=@($welcomePolicyAst.FindAll({param($n)
        $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq $welcomePolicyName},$true))
    if ($welcomePolicyDefinition.Count -ne 1) { throw 'Welcome policy unavailable.' }
    . ([scriptblock]::Create($welcomePolicyDefinition[0].Extent.Text))
}

function Get-WinAppWelcomeDecision($Nodes,$Summary) {
    # Scene recognition is not a whole-page action allowlist. Other controls
    # remain interactive, but cannot become one of the three fixed targets.
    # Summary covers every returned node, including hidden/disabled text.
    if ($Summary.protectedMarkerCount -gt 0 -or $Summary.loginMarkerCount -gt 0) { return 'protected_screen' }
    if ($Summary.headingExactCount -eq 0) { return 'unreviewed_screen' }
    if ($Summary.headingExactCount -ne 1) { return 'ambiguous_control' }
    if ($Summary.headingVisibleCount -ne 1) { return 'unreviewed_screen' }
    foreach ($spec in @(@('Engineering','RadioButton'), @('Suggest personalized tasks','CheckBox'), @('Continue','Button'))) {
        $matches=@($Nodes | Where-Object { $_.Name -ceq $spec[0] -and $_.Type -ceq $spec[1] })
        $wrongType=@($Nodes | Where-Object { $_.Name -ceq $spec[0] -and $_.Interactive -and $_.Type -cne $spec[1] })
        if ($matches.Count -ne 1 -or $wrongType.Count -ne 0) { return 'ambiguous_control' }
    }
    return 'welcome'
}

function ConvertTo-WinAppWelcomeSnapshot([string]$Text,[long]$WindowHandle) {
    # Reuse the complete pinned schema/byte/depth validator. Raw names/selectors stay private.
    $summary=ConvertTo-WinAppSummary $Text $WindowHandle -RenderedDepth 16
    if ($summary.nodeCount -gt 500 -or $summary.truncatedNodeCount -ne 0) { throw 'Welcome tree incomplete.' }
    $value=ConvertFrom-LiveJSON $Text 262144
    $pending=[Collections.Generic.Stack[object]]::new(); $nodes=[Collections.Generic.List[object]]::new()
    foreach ($node in $value.windows[0].elements) { $pending.Push($node) }
    while ($pending.Count) {
        $node=$pending.Pop()
        if (-not $node.isOffscreen) {
            $name='';$selector='';$automationId='';$toggle=''
            if ($node.PSObject.Properties['name']) { $name=$node.name }
            if ($node.PSObject.Properties['selector']) { $selector=$node.selector }
            if ($node.PSObject.Properties['automationId']) { $automationId=$node.automationId }
            if ($node.PSObject.Properties['toggleState']) { $toggle=$node.toggleState }
            $nodes.Add([pscustomobject]@{Name=$name;Type=$node.type;Enabled=$node.isEnabled;
                Interactive=($node.isInvokable -or $node.type -cin @('Button','RadioButton','CheckBox','Edit','ComboBox','Hyperlink','MenuItem','ListItem','TabItem','Slider','Spinner','SplitButton'));
                Invokable=$node.isInvokable;Selector=$selector;AutomationId=$automationId;ToggleState=$toggle})
        }
        if ($node.PSObject.Properties['children']) { foreach ($child in $node.children) { $pending.Push($child) } }
    }
    $decision=Get-WinAppWelcomeDecision $nodes.ToArray() $summary
    if ($summary.errorMarkerCount -gt 0) { throw 'Welcome error marker observed.' }
    return [pscustomobject]@{Nodes=$nodes.ToArray();Decision=$decision}
}
function Get-WinAppWelcomeTarget($Snapshot,[string]$Name,[string]$Type) {
    if (@('Engineering|RadioButton','Suggest personalized tasks|CheckBox','Continue|Button') -cnotcontains ($Name+'|'+$Type)) {
        throw 'Unreviewed welcome action target.'
    }
    if ($Snapshot.Decision -cne 'welcome') { throw 'Welcome page not reviewed.' }
    $matches=@($Snapshot.Nodes | Where-Object { $_.Name -ceq $Name -and $_.Type -ceq $Type })
    if ($matches.Count -ne 1 -or -not $matches[0].Enabled -or -not $matches[0].Invokable) { throw 'Welcome target unavailable.' }
    $node=$matches[0]
    $prefix=@{RadioButton='rdo';CheckBox='chk';Button='btn'}[$Type]
    $identityName=($node.AutomationId.ToLowerInvariant() -replace '[^a-z0-9]','')
    if (-not $identityName) { $identityName=($node.Name.ToLowerInvariant() -replace '[^a-z0-9]','') }
    if ($identityName.Length -gt 15) { $identityName=$identityName.Substring(0,15) }
    $slug=$node.Selector -cmatch ('\A'+$prefix+'-'+$identityName+'-[a-f0-9]{4}\z')
    if (-not $slug) { throw 'Exact welcome selector unavailable.' }
    return $node
}
function New-WinAppWelcomeStart($Tool,[long]$WindowHandle,
    [ValidateSet('read','selected','toggle','select','continue')][string]$Operation,[string]$Selector='') {
    $directory=Join-Path $Tool.Directory ('welcome-'+[guid]::NewGuid().ToString('N'))
    if (Test-Path -LiteralPath $directory) { throw 'Welcome tool scratch not fresh.' }
    [void][IO.Directory]::CreateDirectory($directory)
    $localTool=[pscustomobject]@{Executable=$Tool.Executable;Directory=$directory}
    $start=New-WinAppStart $localTool $WindowHandle -RenderedDepth 16
    if ($Operation -cne 'read') {
        $prefix=@{selected='rdo';toggle='chk';select='rdo';continue='btn'}[$Operation]
        if ($Selector -cnotmatch ('\A'+$prefix+'-[a-z0-9]{1,15}-[a-f0-9]{4}\z')) { throw 'Invalid welcome selector.' }
        $arguments=@('ui')
        if ($Operation -ceq 'selected') { $arguments+=@('get-property',$Selector,'--property','IsSelected') }
        else {
            $action=$(if ($Operation -ceq 'continue') { 'invoke' } else { $Operation })
            $arguments+=@('invoke',$Selector,'--action',$action)
        }
        $arguments+=@('-w',$WindowHandle.ToString([Globalization.CultureInfo]::InvariantCulture),'--json')
        $start.ArgumentList.Clear(); foreach ($argument in $arguments) { $start.ArgumentList.Add($argument) }
    }
    return $start
}
function Invoke-WinAppWelcomeCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [Diagnostics.Stopwatch]$Deadline,[System.Collections.IDictionary]$Diagnostic,
    [ValidateSet('read','selected','toggle','select','continue')][string]$Operation,[string]$Selector='') {
    $remaining=30000-[int]$Deadline.ElapsedMilliseconds
    if ($remaining -le 0) { throw 'Welcome deadline exceeded.' }
    $record=[ordered]@{operation=$Operation;status='stopped';reason='identity_unavailable';exitCode=$null;
        stdoutBytes=0;processExitConfirmed=$null;timedOut=$false;outputLimitExceeded=$false}
    $Diagnostic.commands+=,$record; $child=$null
    try {
        & $CheckIdentity
        $start=New-WinAppWelcomeStart $Tool $WindowHandle $Operation $Selector
        $remaining=30000-[int]$Deadline.ElapsedMilliseconds
        if ($remaining -le 0) { throw 'Welcome deadline exceeded.' }
        $record.reason='tool_start_uncertain';$record.processExitConfirmed=$false
        $child=[DesktopLive.BoundedProcess]::new($start,262144,$remaining)
        $record.reason='tool_timeout'
        if (-not $child.Wait($remaining)) { throw 'Welcome tool timeout.' }
        $drain=[Diagnostics.Stopwatch]::StartNew()
        while (-not $child.Drained -and $drain.ElapsedMilliseconds -lt 2000 -and $Deadline.ElapsedMilliseconds -lt 30000) { Start-Sleep -Milliseconds 20 }
        if ($child.TimedOut -or $Deadline.ElapsedMilliseconds -ge 30000) { throw 'Welcome tool timeout.' }
        $record.reason='tool_output_unavailable'
        $record.exitCode=$child.ExitCode;$record.stdoutBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
        if ($child.Overflow -or -not $child.Drained -or $child.ExitCode -ne 0) { throw 'Welcome tool failed.' }
        $record.reason='identity_changed'; & $CheckIdentity
        $record.status='observed';$record.reason='bounded_command_completed'
        return $child.Output
    } finally {
        if ($null -ne $child) {
            $record.timedOut=$child.TimedOut -or $Deadline.ElapsedMilliseconds -ge 30000;$record.outputLimitExceeded=$child.Overflow
            $record.stdoutBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
            if ($child.Exited) { $record.exitCode=$child.ExitCode }
            Complete-WinAppProcess $child $record
            if (-not $record.processExitConfirmed -or $record.status -cne 'observed') {
                throw 'Welcome tool exit or command unconfirmed.'
            }
        }
    }
}
function Assert-WinAppWelcomeAction([string]$Text,[long]$WindowHandle,$Target,[string]$Action) {
    $value=ConvertFrom-LiveJSON $Text 8192
    Assert-WinAppFields $value @('elementId','pattern','requestedAction','performedAction','hwnd')
    Assert-LiveStrings $value @('elementId','pattern','requestedAction','performedAction')
    Assert-LiveInteger $value.hwnd 1 ([long]::MaxValue)
    $pattern=@{toggle='TogglePattern';select='SelectionItemPattern';invoke='InvokePattern'}[$Action]
    $identity=($value.elementId -ceq $Target.Selector)
    if ($value.hwnd -ne $WindowHandle -or -not $identity -or $value.pattern -cne $pattern -or
        $value.requestedAction -cne $Action -or $value.performedAction -cne $Action) { throw 'Welcome action result mismatch.' }
}
function Read-WinAppWelcomeSelected([string]$Text,$Target) {
    $value=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $value @('elementId','element','properties')
    Assert-WinAppFields $value.properties @('IsSelected')
    Assert-LiveStrings $value @('elementId')
    Assert-LiveStrings $value.properties @('IsSelected')
    # The readonly result must still name the exact current radio, not a text/ancestor fallback.
    $element=$value.element
    if ($element -isnot [pscustomobject]) { throw 'Welcome element unavailable.' }
    Assert-LiveStrings $element @('type','name','selector')
    if ($element.type -cne 'RadioButton' -or $element.name -cne 'Engineering' -or
        $element.isEnabled -isnot [bool] -or -not $element.isEnabled -or
        $element.isOffscreen -isnot [bool] -or $element.isOffscreen -or
        $value.elementId -cne $element.selector -or
        $element.selector -cne $Target.Selector -or
        @('True','False') -cnotcontains $value.properties.IsSelected) { throw 'Welcome selection state unavailable.' }
    return $value.properties.IsSelected -ceq 'True'
}
function Invoke-WinAppWelcome($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,[System.Collections.IDictionary]$Diagnostic) {
    $result=[ordered]@{schema=1;scope='synthetic-welcome-only';status='stopped';reason='uia_provider_error';
        stage='welcome_read';actions=0;suggestionsDisabled=$false;engineeringSelected=$false;
        continueInvoked=$false;nextScreen='not_observed';targetObservations=$null;initialReadObservation=$null}
    $Diagnostic.stage='official_tool_welcome';$Diagnostic.reason='welcome_stopped';$Diagnostic.commands=@()
    $Diagnostic.partialActionUncertain=$false;$Diagnostic.exitCode=$null;$Diagnostic.stdoutBytes=0;$Diagnostic.outputLimitExceeded=$false
    $deadline=[Diagnostics.Stopwatch]::StartNew()
    try {
        Assert-WinAppFiles $Tool
        function Read-Page {
            $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic 'read'
            return ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle
        }
        function Require-Page($Page) {
            $result.targetObservations=Get-WelcomeTargetObservations $Page.Nodes
            if ($Page.Decision -cne 'welcome') {
                $result.reason=$Page.Decision
                if ($result.stage -ceq 'welcome_read') {
                    $result.initialReadObservation=Get-WelcomeInitialObservation $Page.Nodes 1
                }
                throw 'Welcome page changed.'
            }
        }
        function Perform($Target,[string]$Operation,[string]$Action) {
            $result.actions++;$Diagnostic.partialActionUncertain=$true
            $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic $Operation $Target.Selector
            Assert-WinAppWelcomeAction $text $WindowHandle $Target $Action
            $Diagnostic.partialActionUncertain=$false
        }
        $page=Read-Page;Require-Page $page
        $result.stage='suggestions_off'
        $checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox'
        if (@('on','off') -cnotcontains $checkbox.ToggleState) { $result.reason='suggestions_not_off';throw 'Unknown toggle state.' }
        if ($checkbox.ToggleState -ceq 'on') { Perform $checkbox 'toggle' 'toggle' }
        $page=Read-Page;Require-Page $page
        $checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox'
        if ($checkbox.ToggleState -cne 'off') { $result.reason='suggestions_not_off';throw 'Suggestions remain enabled.' }
        $result.suggestionsDisabled=$true;$result.stage='engineering_select'
        $radio=Get-WinAppWelcomeTarget $page 'Engineering' 'RadioButton'
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic 'selected' $radio.Selector
        if (-not (Read-WinAppWelcomeSelected $text $radio)) { Perform $radio 'select' 'select' }
        $page=Read-Page;Require-Page $page
        $radio=Get-WinAppWelcomeTarget $page 'Engineering' 'RadioButton'
        $checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox'
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic 'selected' $radio.Selector
        if (-not (Read-WinAppWelcomeSelected $text $radio) -or $checkbox.ToggleState -cne 'off') {
            $result.reason='selection_state_changed';throw 'Welcome selection state changed.'
        }
        $result.engineeringSelected=$true;$result.stage='continue_invoke'
        $button=Get-WinAppWelcomeTarget $page 'Continue' 'Button';Perform $button 'continue' 'invoke'
        $result.continueInvoked=$true;$result.stage='next_screen_read'
        Start-Sleep -Milliseconds 1000
        $after=Read-Page
        $nextScreen=$(if ($after.Decision -ceq 'protected_screen') { 'protected_screen' }
            elseif ($after.Decision -ceq 'welcome') { 'welcome_still_visible' } else { 'unreviewed_screen' })
        Assert-WinAppFiles $Tool; & $CheckIdentity
        if ($deadline.ElapsedMilliseconds -ge 30000) { throw 'Welcome deadline exceeded.' }
        $result.nextScreen=$nextScreen;$result.status='observed';$result.reason='single_transition_attempted';$result.stage='complete'
        $Diagnostic.stage='complete';$Diagnostic.reason='official_welcome_result_validated'
    } catch {
        $Diagnostic.partialActionUncertain=$Diagnostic.partialActionUncertain -or $result.actions -gt 0
    }
    foreach ($command in $Diagnostic.commands) {
        $Diagnostic.stdoutBytes += $command.stdoutBytes
        $Diagnostic.outputLimitExceeded=$Diagnostic.outputLimitExceeded -or $command.outputLimitExceeded
    }
    # Match the existing helper's recursively parsed fixed-result schema.
    $value=ConvertFrom-LiveJSON ($result | ConvertTo-Json -Depth 8 -Compress)
    Assert-WelcomeResult $value
    return $value
}
