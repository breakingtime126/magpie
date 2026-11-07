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
    return [pscustomobject]@{Nodes=$nodes.ToArray();Decision=$decision;Summary=$summary}
}
function Get-WinAppWelcomeSelectorKind($Node) {
    if (-not $Node.Selector) { return 'missing' }
    $prefix=@{RadioButton='rdo';CheckBox='chk';Button='btn';Hyperlink='lnk'}[$Node.Type]
    $identityName=($Node.AutomationId.ToLowerInvariant() -replace '[^a-z0-9]','')
    if (-not $identityName) { $identityName=($Node.Name.ToLowerInvariant() -replace '[^a-z0-9]','') }
    if ($identityName.Length -gt 15) { $identityName=$identityName.Substring(0,15) }
    if ($prefix -and $Node.Selector -cmatch ('\A'+$prefix+'-'+$identityName+'-[a-f0-9]{4}\z')) { return 'runtime_slug' }
    if ($Node.AutomationId -and $Node.Selector -ceq $Node.AutomationId) { return 'promoted_id' }
    return 'unsupported'
}
function Read-WinAppWelcomeResolvedTarget([string]$Text,$Original,[System.Collections.IDictionary]$Binding) {
    $Binding.reason='resolution_schema_invalid'
    $value=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $value @('elementId','element','properties')
    Assert-LiveStrings $value @('elementId')
    Assert-WinAppFields $value.properties @('AutomationId')
    Assert-LiveStrings $value.properties @('AutomationId')
    $element=$value.element
    Assert-WinAppFields $element @('name','type','automationId','selector','isEnabled','isOffscreen','isInvokable') @(
        'className','x','y','width','height','value','toggleState','expandState','scrollDir','children','hasMoreChildren','invokableAncestor')
    Assert-LiveStrings $element @('name','type','automationId','selector')
    $Binding.reason='resolution_identity_mismatch'
    if ($element.name -cne $Original.Name -or $element.type -cne $Original.Type -or
        $element.automationId -cne $Original.AutomationId -or $value.properties.AutomationId -cne $Original.AutomationId -or
        $value.elementId -cne $element.selector) { throw 'Resolved welcome identity mismatch.' }
    $Binding.reason='resolution_state_unavailable'
    if ($element.isEnabled -isnot [bool] -or -not $element.isEnabled -or
        $element.isOffscreen -isnot [bool] -or $element.isOffscreen -or
        $element.isInvokable -isnot [bool] -or -not $element.isInvokable) { throw 'Resolved welcome state unavailable.' }
    $toggle=$Original.ToggleState
    if ($Original.Type -ceq 'CheckBox') {
        Assert-LiveStrings $element @('toggleState')
        if (@('on','off') -cnotcontains $element.toggleState -or $element.toggleState -cne $toggle) { throw 'Resolved toggle state changed.' }
    }
    $resolved=[pscustomobject]@{Name=$element.name;Type=$element.type;AutomationId=$element.automationId;
        Selector=$element.selector;Enabled=$true;Invokable=$true;Interactive=$true;ToggleState=$toggle}
    $Binding.reason='resolution_selector_invalid'
    if ((Get-WinAppWelcomeSelectorKind $resolved) -cne 'runtime_slug') { throw 'Resolved runtime slug unavailable.' }
    $Binding.reason='runtime_slug_resolved';$Binding.resolved=$true
    return $resolved
}
function Get-WinAppWelcomeTarget($Snapshot,[string]$Name,[string]$Type,[scriptblock]$Resolve=$null,
    [System.Collections.IDictionary]$Diagnostic=$null) {
    $binding=[ordered]@{target='other';reason='target_not_authorized';enabled=$null;visible=$null;
        directInvokable=$null;automationIdPresent=$false;selectorKind='not_observed';resolutionAttempted=$false;resolved=$false}
    if ($null -ne $Diagnostic) {
        if (-not $Diagnostic.Contains('bindings')) { $Diagnostic.bindings=@() }
        $Diagnostic.bindings+=,$binding
    }
    if (@('Engineering|RadioButton','Suggest personalized tasks|CheckBox','Continue|Button','Skip|Button','Skip|Hyperlink','Go to ChatGPT|Button','Go to ChatGPT|Hyperlink') -cnotcontains ($Name+'|'+$Type)) {
        throw 'Unreviewed welcome action target.'
    }
    $binding.target=@{Engineering='engineering';'Suggest personalized tasks'='suggestions';Continue='continue';Skip='skip';'Go to ChatGPT'='confirm_skip'}[$Name]
    $binding.reason='scene_unavailable'
    $expectedScene=$(if ($Name -ceq 'Go to ChatGPT') { 'confirm_skip_ready' } elseif ($Name -ceq 'Skip') { 'skip_ready' } else { 'welcome' })
    if ($Snapshot.Decision -cne $expectedScene) { throw 'Welcome page not reviewed.' }
    $matches=@($Snapshot.Nodes | Where-Object { $_.Name -ceq $Name -and $_.Type -ceq $Type })
    $binding.reason='target_not_unique'
    if ($matches.Count -ne 1) { throw 'Welcome target unavailable.' }
    $node=$matches[0];$binding.enabled=$node.Enabled;$binding.visible=$true;$binding.directInvokable=$node.Invokable
    $binding.automationIdPresent=[bool]$node.AutomationId;$binding.selectorKind=Get-WinAppWelcomeSelectorKind $node
    $binding.reason='target_disabled'
    if (-not $node.Enabled) { throw 'Welcome target unavailable.' }
    $binding.reason='target_not_invokable'
    if (-not $node.Invokable) { throw 'Welcome target unavailable.' }
    if ($binding.selectorKind -ceq 'runtime_slug') { $binding.reason='runtime_slug_observed';return $node }
    $binding.reason='selector_unavailable'
    # Only the upstream promoted-ID form can request one typed readonly lookup.
    # A missing/mismatched identity is not a reason to broaden the query.
    if ($binding.selectorKind -cne 'promoted_id' -or $null -eq $Resolve) { throw 'Exact welcome selector unavailable.' }
    $binding.resolutionAttempted=$true;$binding.reason='resolution_read_unavailable'
    $text=& $Resolve $node
    return Read-WinAppWelcomeResolvedTarget $text $node $binding
}
function New-WinAppWelcomeStart($Tool,[long]$WindowHandle,
    [ValidateSet('read','selected','toggle','select','continue','resolve-engineering','resolve-suggestions','resolve-continue','skip','resolve-skip-button','resolve-skip-link','confirm-skip','resolve-confirm-button','resolve-confirm-link')][string]$Operation,[string]$Selector='') {
    $directory=Join-Path $Tool.Directory ('welcome-'+[guid]::NewGuid().ToString('N'))
    if (Test-Path -LiteralPath $directory) { throw 'Welcome tool scratch not fresh.' }
    [void][IO.Directory]::CreateDirectory($directory)
    $localTool=[pscustomobject]@{Executable=$Tool.Executable;Directory=$directory}
    $start=New-WinAppStart $localTool $WindowHandle -RenderedDepth 16
    if ($Operation -cne 'read') {
        $arguments=@('ui')
        if ($Operation -cin @('resolve-engineering','resolve-suggestions','resolve-continue','resolve-skip-button','resolve-skip-link','resolve-confirm-button','resolve-confirm-link')) {
            if ($Selector) { throw 'Readonly target resolution does not accept a selector.' }
            $spec=@{'resolve-engineering'=@('Engineering','RadioButton');
                'resolve-suggestions'=@('Suggest personalized tasks','CheckBox');'resolve-continue'=@('Continue','Button');
                'resolve-skip-button'=@('Skip','Button');'resolve-skip-link'=@('Skip','Hyperlink');
                'resolve-confirm-button'=@('Go to ChatGPT','Button');'resolve-confirm-link'=@('Go to ChatGPT','Hyperlink')}[$Operation]
            $arguments+=@('get-property',$spec[0],'--type',$spec[1],'--property','AutomationId')
        } else {
            $prefix=@{selected='rdo';toggle='chk';select='rdo';continue='btn';skip='(?:btn|lnk)';'confirm-skip'='(?:btn|lnk)'}[$Operation]
            if ($Selector -cnotmatch ('\A'+$prefix+'-[a-z0-9]{1,15}-[a-f0-9]{4}\z')) { throw 'Invalid welcome selector.' }
            if ($Operation -ceq 'selected') { $arguments+=@('get-property',$Selector,'--property','IsSelected') }
            else {
                $action=$(if ($Operation -cin @('continue','skip','confirm-skip')) { 'invoke' } else { $Operation })
                $arguments+=@('invoke',$Selector,'--action',$action)
            }
        }
        $arguments+=@('-w',$WindowHandle.ToString([Globalization.CultureInfo]::InvariantCulture),'--json')
        $start.ArgumentList.Clear(); foreach ($argument in $arguments) { $start.ArgumentList.Add($argument) }
    }
    return $start
}
function Invoke-WinAppWelcomeCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [Diagnostics.Stopwatch]$Deadline,[System.Collections.IDictionary]$Diagnostic,
    [ValidateSet('read','selected','toggle','select','continue','resolve-engineering','resolve-suggestions','resolve-continue','skip','resolve-skip-button','resolve-skip-link','confirm-skip','resolve-confirm-button','resolve-confirm-link')][string]$Operation,[string]$Selector='') {
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
function Invoke-WinAppWelcome($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,[System.Collections.IDictionary]$Diagnostic,
    [Diagnostics.Stopwatch]$SharedDeadline=$null) {
    $result=[ordered]@{schema=1;scope='synthetic-welcome-only';status='stopped';reason='uia_provider_error';
        stage='welcome_read';actions=0;suggestionsDisabled=$false;engineeringSelected=$false;
        continueInvoked=$false;nextScreen='not_observed';targetObservations=$null;initialReadObservation=$null}
    $Diagnostic.stage='official_tool_welcome';$Diagnostic.reason='welcome_stopped';$Diagnostic.commands=@();$Diagnostic.bindings=@()
    $Diagnostic.partialActionUncertain=$false;$Diagnostic.exitCode=$null;$Diagnostic.stdoutBytes=0;$Diagnostic.outputLimitExceeded=$false
    $deadline=$SharedDeadline
    if ($null -eq $deadline) { $deadline=[Diagnostics.Stopwatch]::StartNew() }
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
        $resolveTarget={param($target)
            $operation=@{Engineering='resolve-engineering';'Suggest personalized tasks'='resolve-suggestions';Continue='resolve-continue'}[$target.Name]
            Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic $operation
        }
        $page=Read-Page;Require-Page $page
        $result.stage='suggestions_off'
        $checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox' $resolveTarget $Diagnostic
        if (@('on','off') -cnotcontains $checkbox.ToggleState) { $result.reason='suggestions_not_off';throw 'Unknown toggle state.' }
        if ($checkbox.ToggleState -ceq 'on') { Perform $checkbox 'toggle' 'toggle' }
        $page=Read-Page;Require-Page $page
        $checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox' $resolveTarget $Diagnostic
        if ($checkbox.ToggleState -cne 'off') { $result.reason='suggestions_not_off';throw 'Suggestions remain enabled.' }
        $result.suggestionsDisabled=$true;$result.stage='engineering_select'
        $radio=Get-WinAppWelcomeTarget $page 'Engineering' 'RadioButton' $resolveTarget $Diagnostic
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic 'selected' $radio.Selector
        if (-not (Read-WinAppWelcomeSelected $text $radio)) { Perform $radio 'select' 'select' }
        $page=Read-Page;Require-Page $page
        $radio=Get-WinAppWelcomeTarget $page 'Engineering' 'RadioButton' $resolveTarget $Diagnostic
        $checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox' $resolveTarget $Diagnostic
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic 'selected' $radio.Selector
        if (-not (Read-WinAppWelcomeSelected $text $radio) -or $checkbox.ToggleState -cne 'off') {
            $result.reason='selection_state_changed';throw 'Welcome selection state changed.'
        }
        $result.engineeringSelected=$true;$result.stage='continue_invoke'
        $button=Get-WinAppWelcomeTarget $page 'Continue' 'Button' $resolveTarget $Diagnostic;Perform $button 'continue' 'invoke'
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

function Get-WinAppSkipObservation($Page) {
    $nodes=$Page.Nodes
    $named=@($nodes | Where-Object { $_.Name -ceq 'Skip' })
    $targets=@($named | Where-Object { $_.Interactive })
    $type='absent';$enabled=$null;$invokable=$null
    if ($targets.Count -gt 1) { $type='ambiguous' }
    elseif ($targets.Count -eq 1) {
        $type=$(if ($targets[0].Type -ceq 'Button') { 'button' } elseif ($targets[0].Type -ceq 'Hyperlink') { 'hyperlink' } else { 'other' })
        $enabled=$targets[0].Enabled;$invokable=$targets[0].Invokable
    }
    $observation=[ordered]@{decision='scene_changed';headingCount=$Page.Summary.headingExactCount;
        engineeringCount=@($nodes | Where-Object { $_.Name -ceq 'Engineering' }).Count;
        thinkingCount=@($nodes | Where-Object { $_.Name -ceq 'Thinking' }).Count;
        skipVisibleNameCount=$named.Count;skipInteractiveCount=$targets.Count;skipType=$type;enabled=$enabled;directInvokable=$invokable}
    if ($Page.Decision -ceq 'protected_screen') { $observation.decision='protected_screen';return $observation }
    $oldControls=@($nodes | Where-Object { $_.Interactive -and $_.Name -cin @('Suggest personalized tasks','Continue') })
    if ($Page.Summary.headingExactCount -ne 1 -or $Page.Summary.headingVisibleCount -ne 1 -or
        $observation.engineeringCount -lt 1 -or $oldControls.Count -ne 0) { return $observation }
    if ($type -cin @('ambiguous','other')) { $observation.decision='ambiguous_target';return $observation }
    if ($targets.Count -eq 1 -and $enabled -and $invokable) { $observation.decision='ready';return $observation }
    if ($named.Count -gt 0 -or $observation.thinkingCount -gt 0) { $observation.decision='waiting' }
    return $observation
}
function Invoke-WinAppSkip($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,[Diagnostics.Stopwatch]$Deadline,
    $PriorWelcome,[System.Collections.IDictionary]$Diagnostic) {
    $result=[ordered]@{schema=1;scope='synthetic-skip-only';status='stopped';reason='prior_welcome_unavailable';
        actions=0;readinessReads=0;skipInvoked=$false;nextScreen='not_observed';lastObservation=$null}
    $Diagnostic.commands=@();$Diagnostic.bindings=@();$Diagnostic.partialActionUncertain=$false
    $Diagnostic.stdoutBytes=0;$Diagnostic.outputLimitExceeded=$false
    try {
        Assert-WelcomeResult $PriorWelcome
        if ($PriorWelcome.status -cne 'observed' -or $PriorWelcome.nextScreen -cne 'unreviewed_screen' -or $null -eq $Deadline) { throw 'Prior welcome unavailable.' }
        Assert-WinAppFiles $Tool
        $result.reason='readiness_unavailable';$ready=$false
        for ($read=0;$read -lt 4;$read++) {
            $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read'
            $page=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle
            $result.readinessReads++;$result.lastObservation=Get-WinAppSkipObservation $page
            if ($result.lastObservation.decision -ceq 'ready') { $ready=$true;break }
            if ($result.lastObservation.decision -cne 'waiting') { $result.reason=$result.lastObservation.decision;throw 'Skip scene unavailable.' }
            if ($read -eq 3 -or $Deadline.ElapsedMilliseconds -ge 29500) { break }
            Start-Sleep -Milliseconds 500
        }
        if (-not $ready) { $result.reason='skip_not_ready';throw 'Skip readiness exhausted.' }
        $result.reason='target_binding_unavailable';$page.Decision='skip_ready'
        $type=$(if ($result.lastObservation.skipType -ceq 'button') { 'Button' } else { 'Hyperlink' })
        $resolve={param($target)
            $operation=$(if ($target.Type -ceq 'Button') { 'resolve-skip-button' } else { 'resolve-skip-link' })
            Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic $operation
        }
        $target=Get-WinAppWelcomeTarget $page 'Skip' $type $resolve $Diagnostic
        $result.reason='skip_action_unavailable';$result.actions=1;$Diagnostic.partialActionUncertain=$true
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'skip' $target.Selector
        $result.reason='skip_receipt_invalid';Assert-WinAppWelcomeAction $text $WindowHandle $target 'invoke'
        $result.skipInvoked=$true;$Diagnostic.partialActionUncertain=$false;$result.reason='next_read_unavailable'
        Start-Sleep -Milliseconds 500
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read'
        $after=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle
        $next=$(if ($after.Decision -ceq 'protected_screen') { 'protected_screen' }
            elseif (@($after.Nodes | Where-Object { $_.Name -ceq 'Skip' -and $_.Interactive }).Count -gt 0) { 'skip_still_visible' } else { 'unreviewed_screen' })
        Assert-WinAppFiles $Tool; & $CheckIdentity
        if ($Deadline.ElapsedMilliseconds -ge 30000) { throw 'Skip deadline exhausted.' }
        $result.nextScreen=$next;$result.status='observed';$result.reason='single_skip_attempted'
    } catch { $Diagnostic.partialActionUncertain=$Diagnostic.partialActionUncertain -or $result.actions -gt 0 }
    foreach ($command in $Diagnostic.commands) {
        $Diagnostic.stdoutBytes+=$command.stdoutBytes
        $Diagnostic.outputLimitExceeded=$Diagnostic.outputLimitExceeded -or $command.outputLimitExceeded
    }
    return [pscustomobject]$result
}
function Get-WinAppSkipOutcome($Prior,$Skip,[bool]$Captured,[bool]$Uncertain) {
    if ($Prior.exitCode -ne 0) { return $Prior }
    if ($null -eq $Skip -or $Skip.status -cne 'observed' -or -not $Captured -or $Uncertain -or
        $Skip.nextScreen -cne 'unreviewed_screen') {
        return [pscustomobject]@{status='blocked';stage='welcome-skip-stopped';reason='The bounded Skip stage stopped, reached a protected screen, remained unchanged or lacks confirmed capture. Prior welcome evidence is retained; no next-page acceptance is claimed.';exitCode=1}
    }
    return [pscustomobject]@{status='observed';stage='welcome-skip-transition-observed';reason='One confirmed welcome sequence and one Skip invocation completed with next-screen pixels. The resulting page is unreviewed; no composer, model or conversation success is established.';exitCode=0}
}

# This is only the observed ordinary Skip confirmation, not login or consent.
function Get-WinAppSkipConfirmationObservation($Page) {
    $nodes=$Page.Nodes
    $title=@($nodes | Where-Object { $_.Name -ceq 'Skip setup?' })
    $cancel=@($nodes | Where-Object { $_.Name -ceq 'Keep setting up' -and $_.Interactive })
    $targets=@($nodes | Where-Object { $_.Name -ceq 'Go to ChatGPT' -and $_.Interactive })
    $type='absent';$enabled=$null;$invokable=$null
    if ($targets.Count -gt 1) { $type='ambiguous' }
    elseif ($targets.Count -eq 1) {
        $type=$(if ($targets[0].Type -ceq 'Button') { 'button' } elseif ($targets[0].Type -ceq 'Hyperlink') { 'hyperlink' } else { 'other' })
        $enabled=$targets[0].Enabled;$invokable=$targets[0].Invokable
    }
    $observation=[ordered]@{decision='scene_changed';titleCount=$title.Count;cancelInteractiveCount=$cancel.Count;
        targetInteractiveCount=$targets.Count;targetType=$type;enabled=$enabled;directInvokable=$invokable}
    if ($Page.Decision -ceq 'protected_screen') { $observation.decision='protected_screen';return $observation }
    if ($title.Count -ne 1 -or $cancel.Count -ne 1 -or $cancel[0].Type -cnotin @('Button','Hyperlink')) { return $observation }
    if ($type -cin @('ambiguous','other')) { $observation.decision='ambiguous_target';return $observation }
    $observation.decision='target_not_ready'
    if ($targets.Count -eq 1 -and $enabled -and $invokable) { $observation.decision='ready' }
    return $observation
}
function Invoke-WinAppSkipConfirmation($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,[Diagnostics.Stopwatch]$Deadline,
    $PriorSkip,[System.Collections.IDictionary]$Diagnostic) {
    $result=[ordered]@{schema=1;scope='synthetic-skip-confirmation-only';status='stopped';reason='prior_skip_unavailable';
        actions=0;confirmationInvoked=$false;nextScreen='not_observed';lastObservation=$null}
    $Diagnostic.commands=@();$Diagnostic.bindings=@();$Diagnostic.partialActionUncertain=$false
    $Diagnostic.stdoutBytes=0;$Diagnostic.outputLimitExceeded=$false
    try {
        if ($null -eq $PriorSkip -or $PriorSkip.status -cne 'observed' -or $PriorSkip.actions -ne 1 -or
            -not $PriorSkip.skipInvoked -or $PriorSkip.nextScreen -cne 'unreviewed_screen' -or $null -eq $Deadline) { throw 'Prior Skip unavailable.' }
        Assert-WinAppFiles $Tool
        $result.reason='confirmation_read_unavailable'
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read'
        $page=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle
        $result.lastObservation=Get-WinAppSkipConfirmationObservation $page
        if ($result.lastObservation.decision -cne 'ready') { $result.reason=$result.lastObservation.decision;throw 'Skip confirmation unavailable.' }
        $result.reason='target_binding_unavailable';$page.Decision='confirm_skip_ready'
        $type=$(if ($result.lastObservation.targetType -ceq 'button') { 'Button' } else { 'Hyperlink' })
        $resolve={param($target)
            $operation=$(if ($target.Type -ceq 'Button') { 'resolve-confirm-button' } else { 'resolve-confirm-link' })
            Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic $operation
        }
        $target=Get-WinAppWelcomeTarget $page 'Go to ChatGPT' $type $resolve $Diagnostic
        $result.reason='confirmation_action_unavailable';$result.actions=1;$Diagnostic.partialActionUncertain=$true
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'confirm-skip' $target.Selector
        $result.reason='confirmation_receipt_invalid';Assert-WinAppWelcomeAction $text $WindowHandle $target 'invoke'
        $result.confirmationInvoked=$true;$Diagnostic.partialActionUncertain=$false;$result.reason='next_read_unavailable'
        Start-Sleep -Milliseconds 500
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read'
        $after=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle
        $next=$(if ($after.Decision -ceq 'protected_screen') { 'protected_screen' }
            elseif (@($after.Nodes | Where-Object { $_.Name -ceq 'Skip setup?' -or
                ($_.Name -ceq 'Go to ChatGPT' -and $_.Interactive) }).Count -gt 0) { 'confirmation_still_visible' } else { 'unreviewed_screen' })
        Assert-WinAppFiles $Tool; & $CheckIdentity
        if ($Deadline.ElapsedMilliseconds -ge 30000) { throw 'Skip confirmation deadline exhausted.' }
        $result.nextScreen=$next;$result.status='observed';$result.reason='single_confirmation_attempted'
    } catch { $Diagnostic.partialActionUncertain=$Diagnostic.partialActionUncertain -or $result.actions -gt 0 }
    foreach ($command in $Diagnostic.commands) {
        $Diagnostic.stdoutBytes+=$command.stdoutBytes
        $Diagnostic.outputLimitExceeded=$Diagnostic.outputLimitExceeded -or $command.outputLimitExceeded
    }
    return [pscustomobject]$result
}
function Get-WinAppSkipConfirmationOutcome($Prior,$Confirmation,[bool]$Captured,[bool]$Uncertain) {
    if ($Prior.exitCode -ne 0) { return $Prior }
    if ($null -eq $Confirmation -or $Confirmation.status -cne 'observed' -or -not $Captured -or $Uncertain -or
        $Confirmation.nextScreen -cne 'unreviewed_screen') {
        return [pscustomobject]@{status='blocked';stage='welcome-skip-confirmation-stopped';reason='The ordinary Skip confirmation stopped, reached a protected screen, remained unchanged or lacks confirmed capture. Prior welcome and Skip evidence is retained.';exitCode=1}
    }
    return [pscustomobject]@{status='observed';stage='welcome-skip-confirmation-observed';reason='Welcome, Skip and its ordinary confirmation completed with next-screen pixels. The resulting page is unreviewed; no model, conversation or normal-entry acceptance is established.';exitCode=0}
}
