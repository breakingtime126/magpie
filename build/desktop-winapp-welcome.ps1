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

# Explicit profile-local labels. Never add self-authored labels to legacy targets.
function Get-WinAppModelNames([string]$FixtureProfile='') {
    if ($FixtureProfile -cin @('synthetic-models-4239','bridge-owned-catalog-4239')) {
        return [pscustomobject]@{Menu=@('Synthetic Alpha [alpha]','Synthetic Alpha [alpha] None');Submenu=@('Synthetic Alpha [alpha]');
            Expanded=@('Synthetic Alpha [alpha]','Synthetic Alpha [alpha] None');Alpha=@('Synthetic Alpha [alpha]');Beta=@('Synthetic Beta [beta]');
            All=@('Synthetic Alpha [alpha]','Synthetic Beta [beta]')}
    }
    if ($FixtureProfile -cne '') { throw 'Unreviewed model-label profile.' }
    return [pscustomobject]@{Menu=@('5.5 [alpha]','5.5 [alpha] Medium');
        Submenu=@('5.5 [alpha]','GPT-5.5 [alpha]','Medium 5.5 [alpha]','Medium GPT-5.5 [alpha]');
        Expanded=@('5.5 [alpha]','5.5 [alpha] Medium','GPT-5.5 [alpha]','Medium 5.5 [alpha]','Medium GPT-5.5 [alpha]');
        Alpha=@('GPT-5.5 [alpha]','5.5 [alpha]');Beta=@('GPT-5.5 [beta]','5.5 [beta]');
        All=@('5.5 [alpha]','GPT-5.5 [alpha]','5.5 [beta]','GPT-5.5 [beta]')}
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

function ConvertTo-WinAppWelcomeSnapshot([string]$Text,[long]$WindowHandle,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    # Reuse the complete pinned schema/byte/depth validator. Raw names/selectors stay private.
    $summary=ConvertTo-WinAppSummary $Text $WindowHandle -RenderedDepth 16
    if ($summary.nodeCount -gt 500 -or $summary.truncatedNodeCount -ne 0) { throw 'Welcome tree incomplete.' }
    $value=ConvertFrom-LiveJSON $Text 262144
    $pending=[Collections.Generic.Stack[object]]::new(); $nodes=[Collections.Generic.List[object]]::new()
    foreach ($node in $value.windows[0].elements) { $pending.Push($node) }
    $alphaEvidence=[Collections.Generic.List[object]]::new();$alphaTotal=0;$alphaEvidenceLimited=$false
    $invalidSyntheticLabel=$false
    while ($pending.Count) {
        $node=$pending.Pop()
        if ($FixtureProfile -cne '' -and $node.PSObject.Properties['name'] -and
            $node.name -cin $modelNames.All -and ($node.isOffscreen -or -not $node.isEnabled)) { $invalidSyntheticLabel=$true }
        # Tiny authorized evidence from the same tree, including offscreen nodes.
        if ($node.PSObject.Properties['name'] -and $node.name.Contains('[alpha]')) {
            $alphaTotal++
            if ($alphaEvidence.Count -ge 4 -or $node.name.Length -gt 120 -or $node.name -match '\A[A-Za-z]:|[\\/\x00-\x08\x0b\x0c\x0e-\x1f]') { $alphaEvidenceLimited=$true }
            else {
                $normalized=($node.name -replace '\s+',' ').Trim()
                $known=$modelNames.Submenu
                $match=$(if ($node.name -cin $known) { 'exact' } elseif ($normalized -cin $known) { 'normalized' } else { 'other' })
                $type=$(if ($node.type -cin @('Button','MenuItem','Hyperlink','Text','Group','Pane','Custom','ComboBox','RadioButton')) { $node.type } else { 'other' })
                $state='unavailable'
                if ($node.PSObject.Properties['expandState'] -and $node.expandState -cin @('collapsed','expanded')) { $state=$node.expandState }
                $alphaEvidence.Add([pscustomobject]@{name=$node.name;nameMatch=$match;controlType=$type;isOffscreen=$node.isOffscreen;
                    enabled=$node.isEnabled;directInvokable=$node.isInvokable;expandState=$state})
            }
        }
        if (-not $node.isOffscreen) {
            $name='';$selector='';$automationId='';$toggle='';$expand=''
            if ($node.PSObject.Properties['name']) { $name=$node.name }
            if ($node.PSObject.Properties['selector']) { $selector=$node.selector }
            if ($node.PSObject.Properties['automationId']) { $automationId=$node.automationId }
            if ($node.PSObject.Properties['toggleState']) { $toggle=$node.toggleState }
            if ($node.PSObject.Properties['expandState']) { $expand=$node.expandState }
            $nodes.Add([pscustomobject]@{Name=$name;Type=$node.type;Enabled=$node.isEnabled;
                Interactive=($node.isInvokable -or $node.type -cin @('Button','RadioButton','CheckBox','Edit','ComboBox','Hyperlink','MenuItem','ListItem','TabItem','Slider','Spinner','SplitButton'));
                Invokable=$node.isInvokable;Selector=$selector;AutomationId=$automationId;ToggleState=$toggle;ExpandState=$expand})
        }
        if ($node.PSObject.Properties['children']) { foreach ($child in $node.children) { $pending.Push($child) } }
    }
    $decision=Get-WinAppWelcomeDecision $nodes.ToArray() $summary
    if ($invalidSyntheticLabel) { $decision='protected_screen' }
    if ($summary.errorMarkerCount -gt 0) { throw 'Welcome error marker observed.' }
    return [pscustomobject]@{Nodes=$nodes.ToArray();Decision=$decision;Summary=$summary;AlphaEvidence=$alphaEvidence.ToArray();AlphaTotal=$alphaTotal;AlphaEvidenceLimited=$alphaEvidenceLimited}
}
function Get-WinAppWelcomeSelectorKind($Node) {
    if (-not $Node.Selector) { return 'missing' }
    $prefix=@{RadioButton='rdo';CheckBox='chk';Button='btn';Hyperlink='lnk';MenuItem='mnu'}[$Node.Type]
    $identityName=($Node.AutomationId.ToLowerInvariant() -replace '[^a-z0-9]','')
    if (-not $identityName) { $identityName=($Node.Name.ToLowerInvariant() -replace '[^a-z0-9]','') }
    if ($identityName.Length -gt 15) { $identityName=$identityName.Substring(0,15) }
    if ($prefix -and $Node.Selector -cmatch ('\A'+$prefix+'-'+$identityName+'-[a-f0-9]{4}\z')) { return 'runtime_slug' }
    if ($Node.AutomationId -and $Node.Selector -ceq $Node.AutomationId) { return 'promoted_id' }
    return 'unsupported'
}
function Read-WinAppWelcomeResolvedTarget([string]$Text,$Original,[System.Collections.IDictionary]$Binding,[string]$FixtureProfile='',
    [string]$SelectionProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $selectionBinding=$SelectionProfile -cne ''
    if ($selectionBinding) {
        Assert-WinAppModelSelectionProfile $SelectionProfile $FixtureProfile
        if ($Original.Name -cnotin @('Synthetic Alpha [alpha]','Synthetic Beta [beta]') -or
            $Original.Type -cnotin @('RadioButton','MenuItem')) { throw 'Unreviewed selection binding.' }
    }
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
    $expand=''
    if (-not $selectionBinding -and ($Original.Name -replace '\s+',' ').Trim() -cin $modelNames.Expanded -and $Original.Type -cin @('Button','MenuItem')) {
        $Binding.reason='resolution_expand_state_unavailable'
        Assert-LiveStrings $element @('expandState')
        if ($Original.ExpandState -cne 'collapsed' -or $element.expandState -cne 'collapsed') { throw 'Resolved model menu state changed.' }
        $expand=$element.expandState
    }
    $resolved=[pscustomobject]@{Name=$element.name;Type=$element.type;AutomationId=$element.automationId;
        Selector=$element.selector;Enabled=$true;Invokable=$true;Interactive=$true;ToggleState=$toggle;ExpandState=$expand}
    $Binding.reason='resolution_selector_invalid'
    if ((Get-WinAppWelcomeSelectorKind $resolved) -cne 'runtime_slug') { throw 'Resolved runtime slug unavailable.' }
    $Binding.reason='runtime_slug_resolved';$Binding.resolved=$true
    return $resolved
}
function Get-WinAppWelcomeTarget($Snapshot,[string]$Name,[string]$Type,[scriptblock]$Resolve=$null,
    [System.Collections.IDictionary]$Diagnostic=$null,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $binding=[ordered]@{target='other';reason='target_not_authorized';enabled=$null;visible=$null;
        directInvokable=$null;automationIdPresent=$false;selectorKind='not_observed';resolutionAttempted=$false;resolved=$false}
    if ($null -ne $Diagnostic) {
        if (-not $Diagnostic.Contains('bindings')) { $Diagnostic.bindings=@() }
        $Diagnostic.bindings+=,$binding
    }
    $submenu=($Snapshot.Decision -ceq 'model_submenu_ready')
    if ($submenu) {
        if ($Name.Length -gt 120 -or $Name -match '\A[A-Za-z]:|[\\/\x00-\x08\x0b\x0c\x0e-\x1f]') { throw 'Unreviewed model submenu name.' }
        if (($FixtureProfile -cne '' -and $Name -cnotin $modelNames.Submenu) -or
            ($Name -replace '\s+',' ').Trim() -cnotin $modelNames.Submenu -or
            $Type -cnotin @('Button','MenuItem')) { throw 'Unreviewed model submenu target.' }
    } elseif (@('Engineering|RadioButton','Suggest personalized tasks|CheckBox','Continue|Button','Skip|Button','Skip|Hyperlink','Go to ChatGPT|Button','Go to ChatGPT|Hyperlink') -cnotcontains ($Name+'|'+$Type) -and
        -not ($Name -cin $modelNames.Menu -and $Type -ceq 'Button')) {
        throw 'Unreviewed welcome action target.'
    }
    $binding.target=@{Engineering='engineering';'Suggest personalized tasks'='suggestions';Continue='continue';Skip='skip';'Go to ChatGPT'='confirm_skip';'5.5 [alpha]'='model_menu';'5.5 [alpha] Medium'='model_menu'}[$Name]
    if ($Name -cin $modelNames.Menu) { $binding.target='model_menu' }
    if ($submenu) { $binding.target='model_submenu' }
    $binding.reason='scene_unavailable'
    $expectedScene=$(if ($submenu) { 'model_submenu_ready' } elseif ($Name -cin $modelNames.Menu) { 'model_menu_ready' } elseif ($Name -ceq 'Go to ChatGPT') { 'confirm_skip_ready' } elseif ($Name -ceq 'Skip') { 'skip_ready' } else { 'welcome' })
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
    if ($expectedScene -cin @('model_menu_ready','model_submenu_ready')) {
        $binding.reason='target_not_collapsed'
        if ($node.ExpandState -cne 'collapsed') { throw 'Model menu expansion state unavailable.' }
    }
    if ($binding.selectorKind -ceq 'runtime_slug') { $binding.reason='runtime_slug_observed';return $node }
    $binding.reason='selector_unavailable'
    # Only the upstream promoted-ID form can request one typed readonly lookup.
    # A missing/mismatched identity is not a reason to broaden the query.
    if ($binding.selectorKind -cne 'promoted_id' -or $null -eq $Resolve) { throw 'Exact welcome selector unavailable.' }
    $binding.resolutionAttempted=$true;$binding.reason='resolution_read_unavailable'
    $text=& $Resolve $node
    return Read-WinAppWelcomeResolvedTarget $text $node $binding -FixtureProfile $FixtureProfile
}
function New-WinAppWelcomeStart($Tool,[long]$WindowHandle,
    [ValidateSet('read','selected','toggle','select','continue','resolve-engineering','resolve-suggestions','resolve-continue','skip','resolve-skip-button','resolve-skip-link','confirm-skip','resolve-confirm-button','resolve-confirm-link','open-model-menu','resolve-model-short','resolve-model-effort','open-model-submenu','resolve-submodel-button','resolve-submodel-menuitem','search-alpha','focused','model-tab','model-enter','resolve-selection-alpha','resolve-selection-beta','resolve-selection-beta-menu','resolve-selection-alpha-menuitem','resolve-selection-beta-menuitem')][string]$Operation,[string]$Selector='',[string]$FixtureProfile='',[string]$SelectionProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    if ($Operation -clike 'resolve-selection-*') {
        Assert-WinAppModelSelectionProfile $SelectionProfile $FixtureProfile
    } elseif ($SelectionProfile) { Assert-WinAppModelSelectionProfile $SelectionProfile $FixtureProfile }
    $directory=Join-Path $Tool.Directory ('welcome-'+[guid]::NewGuid().ToString('N'))
    if (Test-Path -LiteralPath $directory) { throw 'Welcome tool scratch not fresh.' }
    [void][IO.Directory]::CreateDirectory($directory)
    $localTool=[pscustomobject]@{Executable=$Tool.Executable;Directory=$directory}
    $start=New-WinAppStart $localTool $WindowHandle -RenderedDepth 16
    if ($Operation -cne 'read') {
        $arguments=@('ui')
        if ($Operation -cin @('focused','model-tab','model-enter')) {
            if ($Selector) { throw 'Model keyboard operations do not accept selectors.' }
            if ($Operation -ceq 'focused') { $arguments+=@('get-focused') }
            else { $arguments+=@('send-keys',$(if($Operation -ceq 'model-tab'){'tab'}else{'enter'}),'--via','send-input') }
        } elseif ($Operation -clike 'resolve-selection-*') {
            if ($Selector) { throw 'Selection resolution does not accept an arbitrary target.' }
            $spec=@{'resolve-selection-alpha'=@('Synthetic Alpha [alpha]','RadioButton');
                'resolve-selection-beta'=@('Synthetic Beta [beta]','RadioButton');
                'resolve-selection-beta-menu'=@('Synthetic Beta [beta] None','Button');
                'resolve-selection-alpha-menuitem'=@('Synthetic Alpha [alpha]','MenuItem');
                'resolve-selection-beta-menuitem'=@('Synthetic Beta [beta]','MenuItem')}[$Operation]
            $arguments+=@('get-property',$spec[0],'--type',$spec[1],'--property','AutomationId')
        } elseif ($Operation -ceq 'search-alpha') {
            if ($Selector) { throw 'Model evidence search does not accept a selector.' }
            $arguments+=@('search','[alpha]','--max','4')
        } elseif ($Operation -cin @('resolve-engineering','resolve-suggestions','resolve-continue','resolve-skip-button','resolve-skip-link','resolve-confirm-button','resolve-confirm-link','resolve-model-short','resolve-model-effort','resolve-submodel-button','resolve-submodel-menuitem')) {
            if ($Selector) { throw 'Readonly target resolution does not accept a selector.' }
            $spec=@{'resolve-engineering'=@('Engineering','RadioButton');
                'resolve-suggestions'=@('Suggest personalized tasks','CheckBox');'resolve-continue'=@('Continue','Button');
                'resolve-skip-button'=@('Skip','Button');'resolve-skip-link'=@('Skip','Hyperlink');
                'resolve-confirm-button'=@('Go to ChatGPT','Button');'resolve-confirm-link'=@('Go to ChatGPT','Hyperlink');
                'resolve-model-short'=@('5.5 [alpha]','Button');'resolve-model-effort'=@('5.5 [alpha] Medium','Button');
                'resolve-submodel-button'=@('5.5 [alpha]','Button');'resolve-submodel-menuitem'=@('5.5 [alpha]','MenuItem')}[$Operation]
            if ($FixtureProfile -cin @('synthetic-models-4239','bridge-owned-catalog-4239')) {
                if ($Operation -ceq 'resolve-model-effort') { $spec=@($modelNames.Menu[1],'Button') }
                elseif ($Operation -cin @('resolve-model-short','resolve-submodel-button','resolve-submodel-menuitem')) {
                    $spec=@($modelNames.Alpha[0],$spec[1])
                }
            }
            $arguments+=@('get-property',$spec[0],'--type',$spec[1],'--property','AutomationId')
        } else {
            $prefix=@{selected='rdo';toggle='chk';select='rdo';continue='btn';skip='(?:btn|lnk)';'confirm-skip'='(?:btn|lnk)';'open-model-menu'='btn';'open-model-submenu'='(?:btn|mnu)'}[$Operation]
            if ($SelectionProfile -ceq 'bridge-owned-model-selection-4239' -and $Operation -cin @('selected','select')) { $prefix='(?:rdo|mnu)' }
            if ($Selector -cnotmatch ('\A'+$prefix+'-[a-z0-9]{1,15}-[a-f0-9]{4}\z')) { throw 'Invalid welcome selector.' }
            if ($Operation -ceq 'selected') { $arguments+=@('get-property',$Selector,'--property','IsSelected') }
            else {
                $action=$(if ($Operation -cin @('open-model-menu','open-model-submenu')) { 'expand' } elseif ($Operation -cin @('continue','skip','confirm-skip')) { 'invoke' } else { $Operation })
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
    [ValidateSet('read','selected','toggle','select','continue','resolve-engineering','resolve-suggestions','resolve-continue','skip','resolve-skip-button','resolve-skip-link','confirm-skip','resolve-confirm-button','resolve-confirm-link','open-model-menu','resolve-model-short','resolve-model-effort','open-model-submenu','resolve-submodel-button','resolve-submodel-menuitem','search-alpha','focused','model-tab','model-enter','resolve-selection-alpha','resolve-selection-beta','resolve-selection-beta-menu','resolve-selection-alpha-menuitem','resolve-selection-beta-menuitem')][string]$Operation,[string]$Selector='',[string]$FixtureProfile='',[string]$SelectionProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $remaining=30000-[int]$Deadline.ElapsedMilliseconds
    if ($remaining -le 0) { throw 'Welcome deadline exceeded.' }
    $record=[ordered]@{operation=$Operation;status='stopped';reason='identity_unavailable';exitCode=$null;
        stdoutBytes=0;processExitConfirmed=$null;timedOut=$false;outputLimitExceeded=$false}
    $Diagnostic.commands+=,$record; $child=$null
    try {
        & $CheckIdentity
        $start=New-WinAppWelcomeStart $Tool $WindowHandle $Operation $Selector -FixtureProfile $FixtureProfile -SelectionProfile $SelectionProfile
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
    $pattern=@{toggle='TogglePattern';select='SelectionItemPattern';invoke='InvokePattern';expand='ExpandCollapsePattern'}[$Action]
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
    if ($title.Count -lt 1 -or $cancel.Count -ne 1 -or $cancel[0].Type -cnotin @('Button','Hyperlink')) { return $observation }
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

# Only the catalog-backed synthetic alpha entry visible in 0d3's screenshot.
# Neither setup controls nor any model item is an authorized action target.
function Get-WinAppModelMenuObservation($Page,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $nodes=$Page.Nodes
    $targets=@($nodes | Where-Object { $_.Name -cin $modelNames.Menu -and $_.Interactive })
    $type='absent';$enabled=$null;$invokable=$null;$label='absent';$expand='unavailable'
    if ($targets.Count -gt 1) { $type='ambiguous';$label='ambiguous' }
    elseif ($targets.Count -eq 1) {
        $type=$(if ($targets[0].Type -ceq 'Button') { 'button' } else { 'other' })
        $enabled=$targets[0].Enabled;$invokable=$targets[0].Invokable
        $label=$(if ($targets[0].Name -ceq $modelNames.Menu[0]) { 'model_only' }
            elseif ($FixtureProfile -cin @('synthetic-models-4239','bridge-owned-catalog-4239')) { 'model_and_none' } else { 'model_and_medium' })
        if ($targets[0].ExpandState -cin @('collapsed','expanded')) { $expand=$targets[0].ExpandState }
    }
    $observation=[ordered]@{decision='scene_changed';headingCount=@($nodes | Where-Object { $_.Name -ceq 'What should we build?' }).Count;
        targetInteractiveCount=$targets.Count;targetType=$type;labelKind=$label;enabled=$enabled;directInvokable=$invokable;expandState=$expand}
    if ($Page.Decision -ceq 'protected_screen') { $observation.decision='protected_screen';return $observation }
    if ($observation.headingCount -lt 1) { return $observation }
    if ((Get-WinAppModelMenuLabels $Page -FixtureProfile $FixtureProfile).betaVisibleCount -gt 0) { $observation.decision='menu_already_visible';return $observation }
    if ($type -cin @('ambiguous','other')) { $observation.decision='ambiguous_target';return $observation }
    $observation.decision='target_not_ready'
    if ($expand -cne 'collapsed') {
        $observation.decision=$(if ($expand -ceq 'expanded') { 'target_already_expanded' } else { 'target_expand_state_unavailable' });return $observation
    }
    if ($targets.Count -eq 1 -and $enabled -and $invokable) { $observation.decision='ready' }
    return $observation
}
function Get-WinAppModelMenuLabels($Page,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    # Exact public catalog display names, plus the observed Desktop prefix removal.
    # These are visible label counts, not a selection or semantic menu-root proof.
    $alpha=@($Page.Nodes | Where-Object { $_.Name -cin $modelNames.Alpha })
    $beta=@($Page.Nodes | Where-Object { $_.Name -cin $modelNames.Beta })
    $labelNodes=@($alpha)+@($beta)
    $disabled=@()
    if ($FixtureProfile -cne '') { $disabled=@($labelNodes | Where-Object { -not $_.Enabled }) }
    return [ordered]@{alphaVisibleCount=$alpha.Count;betaVisibleCount=$beta.Count;
        alphaInteractiveCount=@($alpha | Where-Object { $_.Interactive }).Count;
        betaInteractiveCount=@($beta | Where-Object { $_.Interactive }).Count;
        protectedScreen=($Page.Decision -ceq 'protected_screen' -or
            ($FixtureProfile -cin @('synthetic-models-4239','bridge-owned-catalog-4239') -and ($alpha.Count -gt 1 -or $beta.Count -gt 1 -or
                $disabled.Count -gt 0)))}
}
function Get-WinAppModelSubmenuObservation($Page,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $known=$modelNames.Submenu
    $targets=@($Page.Nodes | Where-Object { ($_.Name -cin $known -or ($FixtureProfile -ceq '' -and ($_.Name -replace '\s+',' ').Trim() -cin $known)) -and $_.Interactive })
    $observation=[ordered]@{decision='target_unavailable';targetInteractiveCount=$targets.Count;controlType='absent';
        enabled=$null;directInvokable=$null;expandState='unavailable';alphaNameCount=$Page.AlphaTotal;
        nameEvidenceLimited=$Page.AlphaEvidenceLimited;nameEvidence=@()}
    if ($Page.Decision -ceq 'protected_screen') { $observation.decision='protected_screen';return $observation }
    $observation.nameEvidence=$Page.AlphaEvidence
    if ($targets.Count -ne 1) { return $observation }
    $target=$targets[0]
    if ($target.Name.Length -gt 120 -or $target.Name -match '\A[A-Za-z]:|[\\/\x00-\x08\x0b\x0c\x0e-\x1f]') { return $observation }
    $observation.controlType=$(if ($target.Type -cin @('Button','MenuItem')) { $target.Type } else { 'other' })
    $observation.enabled=$target.Enabled;$observation.directInvokable=$target.Invokable
    if ($target.ExpandState -cin @('collapsed','expanded')) { $observation.expandState=$target.ExpandState }
    if ($target.Type -cin @('Button','MenuItem') -and $target.Enabled -and $target.Invokable -and $target.ExpandState -ceq 'collapsed') {
        $observation.decision='ready'
    }
    return $observation
}
function Read-WinAppModelSearchEvidence([string]$Text,[ValidateRange(0,4)][int]$NameLimit=4) {
    # Pinned UiSearchResult/UiElement JSON. This is evidence, never a target source.
    $value=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $value @('matchCount','hasMore','matches')
    Assert-LiveInteger $value.matchCount 0 4
    if ($value.hasMore -isnot [bool] -or $value.matches -isnot [array] -or
        $value.matches.Count -ne $value.matchCount -or ($value.hasMore -and $value.matchCount -ne 4)) { throw 'Invalid model search result.' }
    $evidence=[Collections.Generic.List[object]]::new();$related=0;$visible=0;$limited=$value.hasMore;$protected=$false
    foreach ($node in $value.matches) {
        Assert-WinAppFields $node @('type','isEnabled','isOffscreen','isInvokable','x','y','width','height') @(
            'name','automationId','className','selector','value','toggleState','expandState','scrollDir','invokableAncestor')
        Assert-LiveStrings $node @('type')
        foreach ($key in @('isEnabled','isOffscreen','isInvokable')) {
            if ($node.$key -isnot [bool]) { throw 'Invalid model search state.' }
        }
        foreach ($key in @('name','expandState')) {
            if ($node.PSObject.Properties[$key] -and $node.$key -isnot [string]) { throw 'Invalid model search string.' }
        }
        # Never follow, return, or interpret invokableAncestor, selectors or values.
        if (-not $node.PSObject.Properties['name']) { continue }
        if ($node.name -match '(?i)\b(sign\s*in|log\s*in|password|verification code|sign up|payment|subscribe|terms of|privacy policy|authorize|permission|connect your|allow access|access denied|administrator|security warning|certificate|captcha|error|something went wrong|failed to|could not start|unable to start|unexpected failure)\b') { $protected=$true }
        if (-not $node.name.Contains('[alpha]')) { continue }
        $related++;if (-not $node.isOffscreen) { $visible++ }
        if ($evidence.Count -ge $NameLimit -or $node.name.Length -gt 120 -or $node.name -match '\A[A-Za-z]:|[\\/\x00-\x08\x0b\x0c\x0e-\x1f]') { $limited=$true;continue }
        $type=$(if ($node.type -cin @('Button','MenuItem','Hyperlink','Text','Group','Pane','Custom','ComboBox','RadioButton')) { $node.type } else { 'other' })
        $state='unavailable'
        if ($node.PSObject.Properties['expandState'] -and $node.expandState -cin @('collapsed','expanded')) { $state=$node.expandState }
        $evidence.Add([pscustomobject]@{name=$node.name;controlType=$type;isOffscreen=$node.isOffscreen;
            enabled=$node.isEnabled;directInvokable=$node.isInvokable;expandState=$state})
    }
    if ($protected) { $evidence.Clear() }
    $reason=$(if ($protected) { 'protected_screen' } elseif ($related -eq 0) { 'no_related_names' }
        elseif ($visible -eq 0) { 'offscreen_only' } else { 'visible_related_names' })
    return [ordered]@{status='observed';reason=$reason;matchCount=$value.matchCount;hasMore=$value.hasMore;
        alphaNameCount=$related;alphaVisibleCount=$visible;nameEvidenceLimited=$limited;nameEvidence=$evidence.ToArray()}
}
function Read-WinAppModelFocus([string]$Text,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $value=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $value @('hasFocus') @('element')
    if ($value.hasFocus -isnot [bool]) { throw 'Invalid focused result.' }
    $observation=[ordered]@{hasFocus=$value.hasFocus;kind='unavailable';model='none';controlType='absent';
        enabled=$null;isOffscreen=$null;directInvokable=$null;activationAllowed=$false}
    if (-not $value.hasFocus) {
        if ($value.PSObject.Properties['element']) { throw 'Unexpected focused element.' }
        return [pscustomobject]@{Observation=$observation;Key=''}
    }
    if (-not $value.PSObject.Properties['element']) { throw 'Missing focused element.' }
    $node=$value.element
    Assert-WinAppFields $node @('type','isEnabled','isOffscreen','isInvokable','x','y','width','height') @(
        'name','automationId','className','selector','value','toggleState','expandState','scrollDir')
    Assert-LiveStrings $node @('type')
    foreach ($key in @('isEnabled','isOffscreen','isInvokable')) {
        if ($node.$key -isnot [bool]) { throw 'Invalid focused state.' }
    }
    foreach ($key in @('name','selector')) {
        if ($node.PSObject.Properties[$key] -and $node.$key -isnot [string]) { throw 'Invalid focused string.' }
    }
    $name='';$identity=''
    if ($node.PSObject.Properties['name']) { $name=$node.name }
    if ($node.PSObject.Properties['selector']) { $identity=$node.selector }
    $observation.kind='other';$observation.enabled=$node.isEnabled;$observation.isOffscreen=$node.isOffscreen
    $observation.directInvokable=$node.isInvokable
    $observation.controlType=$(if ($node.type -cin @('Button','MenuItem','Hyperlink','RadioButton','ListItem','Slider','Text','Edit','Pane','Custom')) { $node.type } else { 'other' })
    if ($name -match '(?i)\b(sign\s*in|log\s*in|password|verification code|sign up|payment|subscribe|terms of|privacy policy|authorize|permission|connect your|allow access|access denied|administrator|security warning|certificate|captcha|setup|error|something went wrong|failed to|could not start|unable to start|unexpected failure)\b' -or
        $name -cin @('Continue','Try again')) { $observation.kind='protected' }
    elseif ($name -cin @('Select model','Choose model')) {
        $observation.kind='model_menu';$observation.model='model_menu'
        $observation.activationAllowed=$node.isEnabled -and -not $node.isOffscreen -and $node.type -cin @('Button','MenuItem')
    }
    elseif ($name -cin $modelNames.All) {
        $observation.kind='model';$observation.model=$(if($name.EndsWith('[alpha]')){'alpha'}else{'beta'})
        # Keyboard activation does not require or imply an InvokePattern. The
        # current focused visible control itself, not a hidden label/ancestor, is the target.
        $observation.activationAllowed=$FixtureProfile -ceq '' -and $node.isEnabled -and -not $node.isOffscreen -and
            $node.type -cin @('Button','MenuItem','Hyperlink','RadioButton','ListItem')
    }
    return [pscustomobject]@{Observation=$observation;Key=$identity}
}
function Assert-WinAppModelKeyReceipt([string]$Text,[long]$WindowHandle,[ValidateSet('tab','enter')][string]$Key) {
    $value=ConvertFrom-LiveJSON $Text 8192
    Assert-WinAppFields $value @('keys','via','actionCount','hwnd','warnings')
    Assert-LiveStrings $value @('keys','via')
    Assert-LiveInteger $value.actionCount 1 1;Assert-LiveInteger $value.hwnd 1 ([long]::MaxValue)
    if ($value.keys -cne $Key -or $value.via -cne 'send-input' -or $value.hwnd -ne $WindowHandle -or
        $value.warnings -isnot [array] -or $value.warnings.Count -ne 0) { throw 'Keyboard receipt unavailable.' }
}
function Invoke-WinAppModelMenu($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,[Diagnostics.Stopwatch]$Deadline,
    $PriorConfirmation,[System.Collections.IDictionary]$Diagnostic,[string]$FixtureProfile='') {
    $modelNames=Get-WinAppModelNames $FixtureProfile
    $result=[ordered]@{schema=1;scope=$(if ($FixtureProfile -ceq '') { 'synthetic-model-menu-only' } else { 'self-authored-model-menu-only' });status='stopped';reason='prior_confirmation_unavailable';
        actions=0;expansionInvoked=$false;nextScreen='not_observed';lastObservation=$null;labels=$null;initialLabels=$null;
        submenu=[ordered]@{status='not-run';reason='not-required';actions=0;expansionInvoked=$false;observation=$null;search=$null;keyboard=$null}}
    $Diagnostic.commands=@();$Diagnostic.bindings=@();$Diagnostic.partialActionUncertain=$false
    $Diagnostic.stdoutBytes=0;$Diagnostic.outputLimitExceeded=$false
    try {
        if ($null -eq $PriorConfirmation -or $PriorConfirmation.status -cne 'observed' -or $PriorConfirmation.actions -ne 1 -or
            -not $PriorConfirmation.confirmationInvoked -or $PriorConfirmation.nextScreen -cne 'unreviewed_screen' -or $null -eq $Deadline) { throw 'Prior confirmation unavailable.' }
        Assert-WinAppFiles $Tool
        $result.reason='menu_read_unavailable'
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read' -FixtureProfile $FixtureProfile
        $page=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle -FixtureProfile $FixtureProfile
        $result.lastObservation=Get-WinAppModelMenuObservation $page -FixtureProfile $FixtureProfile
        if ($result.lastObservation.decision -cne 'ready') { $result.reason=$result.lastObservation.decision;throw 'Model menu target unavailable.' }
        $result.reason='target_binding_unavailable';$page.Decision='model_menu_ready'
        $name=$(if ($result.lastObservation.labelKind -ceq 'model_only') { $modelNames.Menu[0] } else { $modelNames.Menu[1] })
        $resolve={param($target)
            $operation=$(if ($target.Name -ceq $modelNames.Menu[0]) { 'resolve-model-short' } else { 'resolve-model-effort' })
            Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic $operation -FixtureProfile $FixtureProfile
        }
        $target=Get-WinAppWelcomeTarget $page $name 'Button' $resolve $Diagnostic -FixtureProfile $FixtureProfile
        $result.reason='menu_action_unavailable';$result.actions=1;$Diagnostic.partialActionUncertain=$true
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'open-model-menu' $target.Selector -FixtureProfile $FixtureProfile
        $result.reason='menu_receipt_invalid';Assert-WinAppWelcomeAction $text $WindowHandle $target 'expand'
        $result.expansionInvoked=$true;$Diagnostic.partialActionUncertain=$false;$result.reason='next_read_unavailable'
        Start-Sleep -Milliseconds 500
        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read' -FixtureProfile $FixtureProfile
        $after=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle -FixtureProfile $FixtureProfile
        $labels=Get-WinAppModelMenuLabels $after -FixtureProfile $FixtureProfile
        $result.initialLabels=$labels
        if (-not $labels.protectedScreen -and ($labels.alphaVisibleCount -eq 0 -or $labels.betaVisibleCount -eq 0)) {
            $observation=Get-WinAppModelSubmenuObservation $after -FixtureProfile $FixtureProfile
            $result.submenu.status='stopped';$result.submenu.reason=$observation.decision;$result.submenu.observation=$observation
            if ($observation.decision -ceq 'ready') {
                $known=$modelNames.Submenu
                $node=@($after.Nodes | Where-Object { ($_.Name -cin $known -or ($FixtureProfile -ceq '' -and ($_.Name -replace '\s+',' ').Trim() -cin $known)) -and $_.Interactive })[0]
                $after.Decision='model_submenu_ready';$result.reason='submenu_binding_unavailable'
                $resolve={param($target)
                    $operation=$(if ($target.Type -ceq 'Button') { 'resolve-submodel-button' } else { 'resolve-submodel-menuitem' })
                    Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic $operation -FixtureProfile $FixtureProfile
                }
                $target=Get-WinAppWelcomeTarget $after $node.Name $node.Type $resolve $Diagnostic -FixtureProfile $FixtureProfile
                $result.reason='submenu_action_unavailable';$result.actions++;$result.submenu.actions=1;$Diagnostic.partialActionUncertain=$true
                $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'open-model-submenu' $target.Selector -FixtureProfile $FixtureProfile
                $result.reason='submenu_receipt_invalid';Assert-WinAppWelcomeAction $text $WindowHandle $target 'expand'
                $result.submenu.expansionInvoked=$true;$Diagnostic.partialActionUncertain=$false;$result.reason='submenu_read_unavailable'
                Start-Sleep -Milliseconds 500
                $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read' -FixtureProfile $FixtureProfile
                $after=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle -FixtureProfile $FixtureProfile
                $labels=Get-WinAppModelMenuLabels $after -FixtureProfile $FixtureProfile
                $result.submenu.status='observed';$result.submenu.reason='single_submenu_expansion_attempted'
            } else {
                # Search already established the ControlView limitation. Instead, inspect
                # real keyboard focus and navigate at most four single Tabs in this same panel.
                $keyboard=[ordered]@{status='stopped';reason='focus_unavailable';tabsSent=0;activationSent=$false;activatedModel='none';lastFocus=$null}
                $result.submenu.keyboard=$keyboard;$previousKey=''
                for ($step=0;$step -le 4;$step++) {
                    $result.reason='keyboard_focus_unavailable'
                    $focus=Read-WinAppModelFocus (Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'focused' -FixtureProfile $FixtureProfile) -FixtureProfile $FixtureProfile
                    $keyboard.lastFocus=$focus.Observation
                    if (-not $focus.Observation.hasFocus) { $keyboard.reason='focus_unavailable';break }
                    $Diagnostic.partialActionUncertain=$false
                    if ($focus.Observation.kind -ceq 'protected') { $keyboard.reason='protected_focus';break }
                    if ($FixtureProfile -cne '' -and $focus.Observation.kind -ceq 'model') { $keyboard.reason='model_selection_not_authorized';break }
                    if ($step -gt 0 -and $previousKey -and $focus.Key -ceq $previousKey) { $keyboard.reason='focus_unchanged';break }
                    if ($focus.Observation.activationAllowed) {
                        $result.reason='keyboard_activation_unavailable';$result.actions++;$Diagnostic.partialActionUncertain=$true
                        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'model-enter' -FixtureProfile $FixtureProfile
                        Assert-WinAppModelKeyReceipt $text $WindowHandle 'enter'
                        $keyboard.activationSent=$true;$keyboard.activatedModel=$focus.Observation.model
                        $result.reason='keyboard_after_focus_unavailable'
                        $afterFocus=Read-WinAppModelFocus (Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'focused' -FixtureProfile $FixtureProfile) -FixtureProfile $FixtureProfile
                        $keyboard.lastFocus=$afterFocus.Observation
                        if (-not $afterFocus.Observation.hasFocus -or $afterFocus.Observation.kind -ceq 'protected') { throw 'Keyboard destination unavailable.' }
                        $result.reason='keyboard_after_read_unavailable'
                        $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'read' -FixtureProfile $FixtureProfile
                        $after=ConvertTo-WinAppWelcomeSnapshot $text $WindowHandle -FixtureProfile $FixtureProfile;$labels=Get-WinAppModelMenuLabels $after -FixtureProfile $FixtureProfile
                        $Diagnostic.partialActionUncertain=$false;$keyboard.status='observed';$keyboard.reason='single_model_key_activation'
                        break
                    }
                    if ($step -eq 4) { $keyboard.reason='navigation_limit';break }
                    $previousKey=$focus.Key;$result.reason='keyboard_tab_unavailable';$result.actions++;$Diagnostic.partialActionUncertain=$true
                    $text=Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $Deadline $Diagnostic 'model-tab' -FixtureProfile $FixtureProfile
                    Assert-WinAppModelKeyReceipt $text $WindowHandle 'tab'
                    $keyboard.tabsSent++ # Clear uncertainty only after the next owned-focus read.
                }
                $result.submenu.reason=$keyboard.reason
            }
        }
        $next=$(if ($labels.protectedScreen) { 'protected_screen' }
            elseif ($labels.alphaVisibleCount -gt 0 -and $labels.betaVisibleCount -gt 0) { 'model_labels_observed' } else { 'model_labels_missing' })
        Assert-WinAppFiles $Tool; & $CheckIdentity
        if ($Deadline.ElapsedMilliseconds -ge 30000) { throw 'Model menu deadline exhausted.' }
        $result.labels=$labels;$result.nextScreen=$next;$result.status='observed';$result.reason=$(if ($result.submenu.expansionInvoked) { 'menu_and_submenu_expansion_attempted' } else { 'single_menu_expansion_attempted' })
        if ($null -ne $result.submenu.keyboard) {
            $result.status=$result.submenu.keyboard.status;$result.reason=$result.submenu.keyboard.reason
            if ($result.submenu.keyboard.reason -ceq 'protected_focus') { $result.nextScreen='protected_screen' }
        }
    } catch { $Diagnostic.partialActionUncertain=$Diagnostic.partialActionUncertain -or $result.actions -gt 0 }
    foreach ($command in $Diagnostic.commands) {
        $Diagnostic.stdoutBytes+=$command.stdoutBytes
        $Diagnostic.outputLimitExceeded=$Diagnostic.outputLimitExceeded -or $command.outputLimitExceeded
    }
    return [pscustomobject]$result
}
function Get-WinAppModelMenuOutcome($Prior,$Menu,[bool]$Captured,[bool]$Uncertain) {
    if ($Prior.exitCode -ne 0) { return $Prior }
    if ($null -eq $Menu -or $Menu.status -cne 'observed' -or -not $Captured -or $Uncertain -or
        $Menu.nextScreen -cne 'model_labels_observed') {
        return [pscustomobject]@{status='blocked';stage='model-menu-stopped';reason='The bounded model-menu observation stopped, is uncertain, lacks both exact synthetic labels or has no verified capture. Prior onboarding evidence is retained.';exitCode=1}
    }
    return [pscustomobject]@{status='observed';stage='synthetic-model-menu-observed';reason='Bounded menu expansion and reads found exact alpha/beta label candidates; screenshot review is required. No model selection was verified; no message, Windows setup or real-provider capability was tested.';exitCode=0}
}

# Separate, explicit current-draft selection slice. The menu-only path above
# never calls this function or gains new action targets.
function Assert-WinAppModelSelectionProfile([string]$Profile,[string]$FixtureProfile) {
    if ($Profile -cne 'bridge-owned-model-selection-4239' -or $FixtureProfile -cne 'bridge-owned-catalog-4239') {
        throw 'Explicit owned model-selection profile required.'
    }
}
function ConvertTo-WinAppModelSelectionSnapshot([string]$Text,[long]$WindowHandle,[System.Collections.IDictionary]$Result=$null) {
    # Only fixed-name statistics leave this function. Until base validation and
    # the complete scan succeed, zero counts do not mean a target was absent.
    $snapshot=[ordered]@{stage='base_schema';baseValidated=$false;statsComplete=$false;headingCount=0;protectedScreen=$false;modelSettingsErrorCount=0;
        names=[ordered]@{}}
    $names=[ordered]@{'Synthetic Alpha [alpha]'='alpha';'Synthetic Beta [beta]'='beta';
        'Synthetic Alpha [alpha] None'='currentAlpha';'Synthetic Beta [beta] None'='currentBeta'}
    foreach ($key in $names.Values) {
        $snapshot.names[$key]=[ordered]@{total=0;hidden=0;disabled=0;directInvokable=0;radioButton=0;menuItem=0;button=0;text=0;other=0}
    }
    if ($null -ne $Result) { $Result.snapshotObservation=$snapshot;$Result.reason='selection_snapshot_base_unavailable' }
    $page=ConvertTo-WinAppWelcomeSnapshot $Text $WindowHandle -FixtureProfile 'bridge-owned-catalog-4239'
    $snapshot.baseValidated=$true
    $snapshot.headingCount=@($page.Nodes | Where-Object { $_.Name -ceq 'What should we build?' }).Count
    $snapshot.protectedScreen=$page.Decision -ceq 'protected_screen'
    $snapshot.stage='name_scan'
    if ($null -ne $Result) { $Result.reason='selection_snapshot_names_unavailable' }
    $value=ConvertFrom-LiveJSON $Text 262144
    $pending=[Collections.Generic.Stack[object]]::new()
    foreach ($node in $value.windows[0].elements) { $pending.Push($node) }
    while ($pending.Count) {
        $node=$pending.Pop()
        # Exact public toast, including its observed typographic apostrophe.
        # Do not export arbitrary text or infer success from a changed checkmark.
        if ($node.PSObject.Properties['name'] -and $node.name -cin @("Couldn't update model settings","Couldn’t update model settings")) {
            $snapshot.modelSettingsErrorCount++
        }
        if ($node.PSObject.Properties['name'] -and $node.name -cin @($names.Keys)) {
            $count=$snapshot.names[$names[$node.name]]
            $count.total++
            if ($node.isOffscreen) { $count.hidden++ }
            if (-not $node.isEnabled) { $count.disabled++ }
            if ($node.isInvokable) { $count.directInvokable++ }
            switch -CaseSensitive ($node.type) {
                'RadioButton' { $count.radioButton++ }
                'MenuItem' { $count.menuItem++ }
                'Button' { $count.button++ }
                'Text' { $count.text++ }
                default { $count.other++ }
            }
        }
        if ($node.PSObject.Properties['children']) { foreach ($child in $node.children) { $pending.Push($child) } }
    }
    $snapshot.statsComplete=$true
    if ($snapshot.modelSettingsErrorCount -gt 0) {
        $snapshot.stage='model_settings_error'
        if ($null -ne $Result) { $Result.reason='selection_model_settings_error' }
        throw 'The application could not update model settings.'
    }
    $snapshot.stage='name_guard'
    if ($null -ne $Result) { $Result.reason='selection_snapshot_target_guard_unavailable' }
    # Only the two pending model targets are gated here. Current-model Button
    # state is checked if it later becomes an actual Expand action target.
    foreach ($key in @('alpha','beta')) {
        $count=$snapshot.names[$key]
        if ($count.total -gt 1 -or $count.hidden -gt 0 -or $count.disabled -gt 0) {
            throw 'Selection target is hidden disabled or duplicated.'
        }
    }
    $snapshot.stage='scene'
    if ($null -ne $Result) { $Result.reason='selection_snapshot_scene_unavailable' }
    if ($snapshot.protectedScreen -or $snapshot.headingCount -ne 1) { throw 'Selection scene unavailable.' }
    $snapshot.stage='ready'
    $page | Add-Member -NotePropertyName SelectionSnapshot -NotePropertyValue $snapshot
    return $page
}
function Get-WinAppModelSelectionTarget($Page,[string]$Name,[string]$Type,[scriptblock]$Resolve,
    [string]$SelectionProfile,[string]$FixtureProfile,[System.Collections.IDictionary]$Observation=$null) {
    Assert-WinAppModelSelectionProfile $SelectionProfile $FixtureProfile
    if (@('Synthetic Alpha [alpha]|RadioButton','Synthetic Beta [beta]|RadioButton',
        'Synthetic Alpha [alpha]|MenuItem','Synthetic Beta [beta]|MenuItem',
        'Synthetic Alpha [alpha] None|Button','Synthetic Beta [beta] None|Button') -cnotcontains ($Name+'|'+$Type)) {
        throw 'Unreviewed model-selection target.'
    }
    if ($Page.Decision -ceq 'protected_screen') { throw 'Protected selection scene.' }
    $matches=@($Page.Nodes | Where-Object { $_.Name -ceq $Name })
    if ($null -ne $Observation) {
        $Observation.target=@{'Synthetic Alpha [alpha]'='alpha';'Synthetic Beta [beta]'='beta';
            'Synthetic Alpha [alpha] None'='current_alpha';'Synthetic Beta [beta] None'='current_beta'}[$Name]
        $Observation.matches=$matches.Count;$Observation.controlType='absent';$Observation.enabled=$false
        $Observation.selectorKind='not_observed';$Observation.selectionState='not_read';$Observation.patternConfirmed=$false
        if ($matches.Count -gt 1) { $Observation.controlType='ambiguous' }
        elseif ($matches.Count -eq 1) {
            $Observation.controlType=$(if ($matches[0].Type -cin @('RadioButton','Button','MenuItem')) { $matches[0].Type } else { 'other' })
            $Observation.enabled=[bool]$matches[0].Enabled;$Observation.selectorKind=Get-WinAppWelcomeSelectorKind $matches[0]
        }
    }
    if ($matches.Count -ne 1 -or $matches[0].Type -cne $Type -or -not $matches[0].Enabled -or -not $matches[0].Invokable) {
        throw 'Selection target must be an exact unique enabled direct control.'
    }
    $target=$matches[0]
    if ($Type -ceq 'Button') {
        $key=$(if ($Name -ceq 'Synthetic Alpha [alpha] None') { 'currentAlpha' } else { 'currentBeta' })
        $counts=$Page.SelectionSnapshot.names[$key]
        if ($counts.total -ne 1 -or $counts.hidden -ne 0 -or $counts.disabled -ne 0) { throw 'Expand target is hidden disabled or duplicated.' }
    }
    if ($Type -ceq 'Button' -and $target.ExpandState -cne 'collapsed') { throw 'Selection opener is not collapsed.' }
    $kind=Get-WinAppWelcomeSelectorKind $target
    if ($kind -ceq 'promoted_id') {
        if ($null -eq $Resolve) { throw 'Selection resolver unavailable.' }
        $binding=[ordered]@{}
        $text=& $Resolve $target
        $resolvedExpand=''
        if ($Type -ceq 'Button') {
            $resolved=ConvertFrom-LiveJSON $text 262144
            if (-not $resolved.element.PSObject.Properties['expandState'] -or $resolved.element.expandState -cne 'collapsed') {
                throw 'Resolved selection opener state changed.'
            }
            $resolvedExpand=$resolved.element.expandState
        }
        $bindingProfile=$(if ($Type -cin @('RadioButton','MenuItem')) { $SelectionProfile } else { '' })
        $target=Read-WinAppWelcomeResolvedTarget $text $target $binding -FixtureProfile $FixtureProfile -SelectionProfile $bindingProfile
        if ($Type -ceq 'Button') { $target.ExpandState=$resolvedExpand }
    } elseif ($kind -cne 'runtime_slug') { throw 'Exact selection runtime identity unavailable.' }
    if ($Type -ceq 'Button' -and $target.ExpandState -cne 'collapsed') { throw 'Resolved selection opener is not collapsed.' }
    return $target
}
function Read-WinAppModelSelected([string]$Text,$Target,[System.Collections.IDictionary]$Observation=$null) {
    if ($null -ne $Observation) { $Observation.selectionState='unavailable';$Observation.patternConfirmed=$false }
    if ($Target.Type -cnotin @('RadioButton','MenuItem') -or $Target.Name -cnotin @('Synthetic Alpha [alpha]','Synthetic Beta [beta]')) {
        throw 'Unreviewed selected-state target.'
    }
    $value=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $value @('elementId','element','properties')
    Assert-WinAppFields $value.properties @('IsSelected')
    Assert-LiveStrings $value @('elementId')
    Assert-LiveStrings $value.properties @('IsSelected')
    $element=$value.element
    if ($element -isnot [pscustomobject]) { throw 'Selected-state element unavailable.' }
    Assert-LiveStrings $element @('type','name','selector')
    if ($element.type -cne $Target.Type -or $element.name -cne $Target.Name -or
        $element.isEnabled -isnot [bool] -or -not $element.isEnabled -or
        $element.isOffscreen -isnot [bool] -or $element.isOffscreen -or
        $value.elementId -cne $element.selector -or $element.selector -cne $Target.Selector -or
        @('True','False') -cnotcontains $value.properties.IsSelected) { throw 'Selected-state identity or pattern unavailable.' }
    if ($null -ne $Observation) {
        $Observation.selectionState=$(if ($value.properties.IsSelected -ceq 'True') { 'selected' } else { 'unselected' })
        $Observation.patternConfirmed=$true
    }
    # Exact strings are required: [bool]'False' would incorrectly be true.
    return $value.properties.IsSelected -ceq 'True'
}
function Invoke-WinAppModelSelection($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    $PriorMenu,[System.Collections.IDictionary]$Diagnostic,[string]$SelectionProfile,[string]$FixtureProfile,
    [scriptblock]$Capture) {
    $result=[ordered]@{schema=1;scope='bridge-owned-current-draft-selection-only';status='stopped';reason='selection_profile_unavailable';
        selectionAttempts=0;confirmedSelections=0;menuReopens=0;initialAlphaSelected=$false;
        betaSelected=$false;alphaSelected=$false;betaLabelMatched=$false;alphaLabelMatched=$false;betaCaptured=$false;alphaCaptured=$false;
        lastObservation=[ordered]@{target='none';matches=0;controlType='absent';enabled=$false;selectorKind='not_observed';selectionState='not_read';patternConfirmed=$false};snapshotObservation=$null;failureCapture='not_attempted'}
    $Diagnostic.commands=@();$Diagnostic.partialActionUncertain=$false;$Diagnostic.stdoutBytes=0;$Diagnostic.outputLimitExceeded=$false
    $deadline=[Diagnostics.Stopwatch]::StartNew()
    try {
        Assert-WinAppModelSelectionProfile $SelectionProfile $FixtureProfile
        $result.reason='prior_menu_unavailable'
        if ($null -eq $PriorMenu -or $PriorMenu.status -cne 'observed' -or $PriorMenu.nextScreen -cne 'model_labels_observed' -or
            $null -eq $Capture) { throw 'Verified prior menu and capture callback required.' }
        Assert-WinAppFiles $Tool
        function Selection-Command([string]$Operation,[string]$Selector='') {
            Invoke-WinAppWelcomeCommand $Tool $WindowHandle $CheckIdentity $deadline $Diagnostic $Operation $Selector `
                -FixtureProfile $FixtureProfile -SelectionProfile $SelectionProfile
        }
        function Selection-Page {
            $result.reason='selection_read_unavailable';$result.snapshotObservation=$null
            $text=Selection-Command 'read'
            ConvertTo-WinAppModelSelectionSnapshot $text $WindowHandle $result
        }
        function Selection-Target($Page,[string]$Name,[string]$Type) {
            if ($Type -ceq 'Model') {
                $matches=@($Page.Nodes | Where-Object { $_.Name -ceq $Name })
                $Type='RadioButton'
                if ($matches.Count -eq 1 -and $matches[0].Type -cin @('RadioButton','MenuItem')) { $Type=$matches[0].Type }
            }
            $resolve={param($original)
                $operation=$(if ($original.Type -cin @('RadioButton','MenuItem')) {
                    if ($original.Type -ceq 'MenuItem') {
                        if ($original.Name -ceq 'Synthetic Alpha [alpha]') { 'resolve-selection-alpha-menuitem' } else { 'resolve-selection-beta-menuitem' }
                    } elseif ($original.Name -ceq 'Synthetic Alpha [alpha]') { 'resolve-selection-alpha' } else { 'resolve-selection-beta' }
                } elseif ($original.Name -ceq 'Synthetic Alpha [alpha] None') { 'resolve-model-effort' } else { 'resolve-selection-beta-menu' })
                Selection-Command $operation
            }
            Get-WinAppModelSelectionTarget $Page $Name $Type $resolve $SelectionProfile $FixtureProfile $result.lastObservation
        }
        function Selection-Pair($Page,[bool]$AlphaExpected) {
            $alpha=Selection-Target $Page 'Synthetic Alpha [alpha]' 'Model'
            $a=Read-WinAppModelSelected (Selection-Command 'selected' $alpha.Selector) $alpha $result.lastObservation
            $beta=Selection-Target $Page 'Synthetic Beta [beta]' 'Model'
            $b=Read-WinAppModelSelected (Selection-Command 'selected' $beta.Selector) $beta $result.lastObservation
            if ($a -ne $AlphaExpected -or $b -eq $AlphaExpected) { throw 'SelectionItem state does not match the expected pair.' }
            return [pscustomobject]@{Alpha=$alpha;Beta=$beta}
        }
        $result.reason='initial_selection_unavailable'
        $page=Selection-Page
        $result.reason='initial_selection_unavailable'
        $pair=Selection-Pair $page $true
        $result.initialAlphaSelected=$true
        foreach ($owner in @('beta','alpha')) {
            $target=$(if ($owner -ceq 'beta') { $pair.Beta } else { $pair.Alpha })
            # Confirm the exact destination still exposes SelectionItem before
            # this single attempt. A receipt alone never proves selected state.
            $result.reason='preselection_state_unavailable'
            $result.lastObservation.target=$owner
            if (Read-WinAppModelSelected (Selection-Command 'selected' $target.Selector) $target $result.lastObservation) { throw 'Destination unexpectedly already selected.' }
            $result.reason='selection_action_unavailable';$result.selectionAttempts++;$Diagnostic.partialActionUncertain=$true
            $text=Selection-Command 'select' $target.Selector
            $result.reason='selection_receipt_invalid';Assert-WinAppWelcomeAction $text $WindowHandle $target 'select'
            $result.reason='selected_label_unavailable'
            $page=Selection-Page
            $result.reason='selected_label_unavailable'
            $name=$(if ($owner -ceq 'beta') { 'Synthetic Beta [beta] None' } else { 'Synthetic Alpha [alpha] None' })
            $buttons=@($page.Nodes | Where-Object { $_.Type -ceq 'Button' -and $_.Name -cin @('Synthetic Alpha [alpha] None','Synthetic Beta [beta] None') })
            if ($buttons.Count -ne 1 -or $buttons[0].Name -cne $name) {
                throw 'Current draft model button did not match the selection.'
            }
            $result[($owner+'LabelMatched')]=$true
            if ($buttons[0].ExpandState -ceq 'collapsed') {
                $result.reason='selection_reopen_unavailable'
                $opener=Selection-Target $page $name 'Button'
                $result.menuReopens++
                $text=Selection-Command 'open-model-menu' $opener.Selector
                Assert-WinAppWelcomeAction $text $WindowHandle $opener 'expand'
                $page=Selection-Page
            } elseif ($buttons[0].ExpandState -cne 'expanded') { throw 'Selected menu expansion state unavailable.' }
            $result.reason='selected_label_unavailable'
            $current=@($page.Nodes | Where-Object { $_.Type -ceq 'Button' -and $_.Name -cin @('Synthetic Alpha [alpha] None','Synthetic Beta [beta] None') })
            if ($current.Count -ne 1 -or $current[0].Name -cne $name) {
                throw 'Current draft label changed during selected-state verification.'
            }
            $result.reason='selected_pair_unavailable'
            $pair=Selection-Pair $page ($owner -ceq 'alpha')
            $result[($owner+'Selected')]=$true;$result.confirmedSelections++
            $result.reason='selection_capture_unavailable'
            & $CheckIdentity; & $Capture $owner; & $CheckIdentity
            $result[($owner+'Captured')]=$true
            # The toast may arrive after the first post-select read. Recheck
            # after the screenshot, before another selection or a successful result.
            $null=Selection-Page
            $Diagnostic.partialActionUncertain=$false
        }
        Assert-WinAppFiles $Tool; & $CheckIdentity
        if ($deadline.ElapsedMilliseconds -ge 30000) { throw 'Selection deadline exceeded.' }
        $result.status='observed';$result.reason='both_current_draft_selections_verified'
    } catch {
        $Diagnostic.partialActionUncertain=$Diagnostic.partialActionUncertain -or $result.selectionAttempts -gt $result.confirmedSelections
        if ($result.reason -ceq 'selection_model_settings_error' -and $result.selectionAttempts -gt 0) {
            # Preserve the failed application outcome and its fixed screenshot.
            # This is a read/capture only, never an action retry or the next select.
            $failedOwner=$(if ($result.selectionAttempts -eq 1) { 'beta' } else { 'alpha' })
            $result.failureCapture='unavailable'
            try {
                & $CheckIdentity; & $Capture $failedOwner; & $CheckIdentity
                $result.failureCapture=$failedOwner
            } catch { }
        }
    }
    foreach ($command in $Diagnostic.commands) {
        $Diagnostic.stdoutBytes+=$command.stdoutBytes
        $Diagnostic.outputLimitExceeded=$Diagnostic.outputLimitExceeded -or $command.outputLimitExceeded
    }
    $value=ConvertFrom-LiveJSON ($result | ConvertTo-Json -Depth 8 -Compress)
    Assert-ModelSelectionResult $value
    return $value
}
function Get-WinAppModelSelectionOutcome($Prior,$Selection,[bool]$Uncertain) {
    if ($Prior.exitCode -ne 0) { return $Prior }
    if ($null -eq $Selection -or $Selection.status -cne 'observed' -or $Uncertain) {
        return [pscustomobject]@{status='blocked';stage='model-selection-stopped';reason='The explicit current-draft selection slice stopped or was uncertain. No persistence or conversation result is established.';exitCode=1}
    }
    Assert-ModelSelectionResult $Selection
    return [pscustomobject]@{status='observed';stage='synthetic-model-selection-observed';reason='Beta then Alpha current-draft selection matched SelectionItem state and current button labels with captures. No persistence, thread migration, message, Windows setup or real-provider use was tested.';exitCode=0}
}
