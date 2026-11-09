# Explicit disposable-VM synthetic chat only. Requires existing winapp/welcome helpers.
# No setup, credentials, clipboard, keyboard injection, arbitrary prompts or restart.
Set-StrictMode -Version Latest

function Assert-WinAppChatProfile([string]$Profile) {
    if ($Profile -cne 'bridge-readonly-synthetic-chat-4239') { throw 'Unreviewed chat profile.' }
}
function ConvertTo-WinAppChatSnapshot([string]$Text,[long]$WindowHandle) {
    try {
        $page=ConvertTo-WinAppWelcomeSnapshot $Text $WindowHandle -FixtureProfile bridge-owned-catalog-4239
        $v=ConvertFrom-LiveJSON $Text 262144
    } catch { throw 'Chat snapshot validation failed.' }
    $pending=[Collections.Generic.Stack[object]]::new()
    foreach($n in $v.windows[0].elements){$pending.Push($n)}
    $known=@{'Do anything'='Edit';'Send'='Button';'Send message'='Button';'New chat'='Button';'Synthetic Alpha [alpha] None'='Button';'Synthetic Beta [beta] None'='Button';'Synthetic Beta [beta]'='RadioButton'}
    $counts=@{}
    while($pending.Count){
        $n=$pending.Pop()
        if($n.PSObject.Properties['name'] -and $known.ContainsKey($n.name) -and $n.type -ceq $known[$n.name]){
            if(-not $counts.ContainsKey($n.name)){$counts[$n.name]=@{total=0;hidden=0}}
            $counts[$n.name].total++
            if($n.isOffscreen){$counts[$n.name].hidden++}
        }
        if($n.PSObject.Properties['children']){foreach($c in $n.children){$pending.Push($c)}}
    }
    # Preserve full-tree evidence, but do not require a future composer/send/
    # new-chat target to be visible before closing the current model popover.
    # The exact requested target is checked when it is actually selected.
    $page | Add-Member -NotePropertyName ChatTargets -NotePropertyValue $counts
    return $page
}
function Assert-WinAppChatCollapse([string]$Text,[long]$WindowHandle,$Target) {
    $v=ConvertFrom-LiveJSON $Text 8192
    Assert-WinAppFields $v @('elementId','pattern','requestedAction','performedAction','hwnd')
    if($v.elementId -cne $Target.Selector -or $v.hwnd -ne $WindowHandle -or
        $v.pattern -cne 'ExpandCollapsePattern' -or $v.requestedAction -cne 'collapse' -or $v.performedAction -cne 'collapse'){
        throw 'Chat collapse receipt mismatch.'
    }
}
function Get-WinAppChatTarget($Page,[ValidateSet('composer','send','new-chat','alpha-menu','beta-menu','beta-choice')]$Kind,[scriptblock]$Resolve=$null) {
    $spec=@{
        composer=@('Edit',@('Do anything'))
        send=@('Button',@('Send','Send message'))
        'new-chat'=@('Button',@('New chat'))
        'alpha-menu'=@('Button',@('Synthetic Alpha [alpha] None'))
        'beta-menu'=@('Button',@('Synthetic Beta [beta] None'))
        'beta-choice'=@('RadioButton',@('Synthetic Beta [beta]'))
    }[$Kind]
    # A semantic name is mandatory. An anonymous arrow, arbitrary single Edit,
    # invokable ancestor or coordinate cannot stand in for the requested target.
    if(-not $Page.PSObject.Properties['ChatTargets'] -or $Page.ChatTargets -isnot [System.Collections.IDictionary]){
        throw 'Chat target evidence unavailable.'
    }
    $total=0;$hidden=0
    foreach($name in $spec[1]){
        if($Page.ChatTargets.Contains($name)){
            $total+=$Page.ChatTargets[$name].total;$hidden+=$Page.ChatTargets[$name].hidden
        }
    }
    if($hidden -ne 0 -or $total -gt 1){throw 'Hidden or duplicated chat target.'}
    $matches=@($Page.Nodes | Where-Object { $_.Name -cin $spec[1] -and $_.Type -ceq $spec[0] })
    if ($matches.Count -ne 1) { throw 'Chat target absent or ambiguous.' }
    $n=$matches[0]
    if ($n.Type -cne $spec[0] -or -not $n.Enabled -or
        ($Kind -ne 'composer' -and -not $n.Invokable)) {
        throw 'Chat target unavailable.'
    }
    if($n.Selector -cnotmatch '\A(?:txt|btn|rdo)-[a-z0-9]{1,15}-[a-f0-9]{4}\z'){
        if(-not $n.PSObject.Properties['AutomationId'] -or $n.AutomationId -isnot [string] -or
            -not $n.AutomationId -or $n.Selector -cne $n.AutomationId -or $null -eq $Resolve){
            throw 'Chat target resolution unavailable.'
        }
        $n=Read-WinAppChatResolvedTarget (& $Resolve $Kind $n) $n $Kind
    }
    if (($Kind -eq 'composer' -and $n.Selector -cnotmatch '\Atxt-') -or
        ($Kind -eq 'beta-choice' -and $n.Selector -cnotmatch '\Ardo-') -or
        ($Kind -notin @('composer','beta-choice') -and $n.Selector -cnotmatch '\Abtn-')) { throw 'Chat selector role mismatch.' }
    return $n
}
# Same official promoted-ID -> runtime-slug lookup contract used by welcome.
# Unlike the welcome opener, chat may need to collapse an already expanded menu;
# require its actual state to stay identical rather than pretending collapsed.
function Read-WinAppChatResolvedTarget([string]$Text,$Original,[string]$Kind) {
    $v=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $v @('elementId','element','properties')
    Assert-WinAppFields $v.properties @('AutomationId')
    Assert-LiveStrings $v @('elementId');Assert-LiveStrings $v.properties @('AutomationId')
    $e=$v.element
    Assert-WinAppFields $e @('name','type','automationId','selector','isEnabled','isOffscreen','isInvokable') @(
        'className','x','y','width','height','value','toggleState','expandState','scrollDir','children','hasMoreChildren','invokableAncestor')
    Assert-LiveStrings $e @('name','type','automationId','selector')
    if($e.name -cne $Original.Name -or $e.type -cne $Original.Type -or
        $e.automationId -cne $Original.AutomationId -or $v.properties.AutomationId -cne $Original.AutomationId -or
        $v.elementId -cne $e.selector -or $e.isEnabled -isnot [bool] -or -not $e.isEnabled -or
        $e.isOffscreen -isnot [bool] -or $e.isOffscreen -or $e.isInvokable -isnot [bool] -or
        ($Kind -ne 'composer' -and -not $e.isInvokable)) {throw 'Chat resolved identity or state changed.'}
    $prefix=@{Edit='txt';Button='btn';RadioButton='rdo'}[$Original.Type]
    $identityName=($Original.AutomationId.ToLowerInvariant() -replace '[^a-z0-9]','')
    if(-not $identityName){$identityName=($Original.Name.ToLowerInvariant() -replace '[^a-z0-9]','')}
    if($identityName.Length -gt 15){$identityName=$identityName.Substring(0,15)}
    if(-not $prefix -or -not $identityName -or $e.selector -cnotmatch ('\A'+$prefix+'-'+$identityName+'-[a-f0-9]{4}\z')){throw 'Chat resolved selector unavailable.'}
    $expand=''
    if($Kind -cin @('alpha-menu','beta-menu')){
        if($Original.ExpandState -cnotin @('collapsed','expanded') -or -not $e.PSObject.Properties['expandState'] -or
            $e.expandState -cne $Original.ExpandState){throw 'Chat resolved menu state changed.'}
        $expand=$e.expandState
    }
    return [pscustomobject]@{Name=$e.name;Type=$e.type;Selector=$e.selector;AutomationId=$e.automationId;
        Enabled=$true;Invokable=$e.isInvokable;ExpandState=$expand;ToggleState=''}
}
function Assert-WinAppChatPage($Page) {
    foreach ($n in $Page.Nodes) {
        if ($n.Name -cin @('Finish Windows setup to continue','Sign in','Log in','Approve','Allow','Allow once','Run command') -or
            $n.Name -cmatch "Couldn.t update model settings") { throw 'Protected chat screen.' }
    }
}
function New-WinAppChatArguments([string]$Profile,[long]$WindowHandle,
    [ValidateSet('set','get','properties','invoke','expand','collapse','select','selected','resolve')]$Operation,
    [string]$Selector,[string]$Text='') {
    Assert-WinAppChatProfile $Profile
    if($WindowHandle -le 0){throw 'Chat window invalid.'}
    if($Operation -eq 'resolve'){
        $spec=@{composer=@('Edit',@('Do anything'));send=@('Button',@('Send','Send message'));
            'new-chat'=@('Button',@('New chat'));'alpha-menu'=@('Button',@('Synthetic Alpha [alpha] None'));
            'beta-menu'=@('Button',@('Synthetic Beta [beta] None'));'beta-choice'=@('RadioButton',@('Synthetic Beta [beta]'))}
        if(-not $spec.ContainsKey($Selector) -or $Text -cnotin $spec[$Selector][1]){throw 'Unreviewed chat resolution.'}
        return @('ui','get-property',$Text,'--type',$spec[$Selector][0],'--property','AutomationId',
            '-w',$WindowHandle.ToString([Globalization.CultureInfo]::InvariantCulture),'--json')
    }
    if ($WindowHandle -le 0 -or $Selector -cnotmatch '\A(?:txt|btn|rdo)-[a-z0-9]{1,15}-[a-f0-9]{4}\z') { throw 'Chat selector invalid.' }
    if ($Operation -eq 'set') {
        if ($Text -cnotmatch '\Asynthetic-(?:alpha|beta)-turn-[12]\z' -or $Selector -cnotmatch '\Atxt-') { throw 'Chat text invalid.' }
        $a=@('ui','set-value',$Selector,$Text)
    } else {
        if ($Text -cne '') { throw 'Unexpected chat text.' }
        switch ($Operation) {
            get { $a=@('ui','get-value',$Selector) }
            properties { $a=@('ui','get-property',$Selector) }
            selected { $a=@('ui','get-property',$Selector,'--property','IsSelected') }
            default {
                if ($Operation -eq 'select' -and $Selector -cnotmatch '\Ardo-') { throw 'Chat selection type invalid.' }
                if ($Operation -ne 'select' -and $Selector -cnotmatch '\Abtn-') { throw 'Chat action type invalid.' }
                $a=@('ui','invoke',$Selector,'--action',$Operation)
            }
        }
    }
    return @($a+@('-w',$WindowHandle.ToString([Globalization.CultureInfo]::InvariantCulture),'--json'))
}
function Read-WinAppChatValue([string]$Text,$Target) {
    $v=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $v @('elementId','text')
    if ($v.elementId -cne $Target.Selector -or $v.text -isnot [string]) { throw 'Chat value unavailable.' }
    return $v.text
}
function Assert-WinAppChatEditable([string]$Text,$Target) {
    $v=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $v @('elementId','element','properties')
    if ($v.elementId -cne $Target.Selector -or $v.element.selector -cne $Target.Selector -or
        $v.element.name -cne $Target.Name -or $v.element.type -cne 'Edit' -or
        $v.element.isEnabled -isnot [bool] -or -not $v.element.isEnabled -or
        $v.element.isOffscreen -isnot [bool] -or $v.element.isOffscreen -or
        $v.properties.IsPassword -isnot [string] -or $v.properties.IsPassword -cne 'False' -or
        $v.properties.IsReadOnly -isnot [string] -or $v.properties.IsReadOnly -cne 'False') { throw 'Composer permissions unavailable.' }
}
function Assert-WinAppChatSet([string]$Text,[long]$WindowHandle,$Target) {
    $v=ConvertFrom-LiveJSON $Text 8192
    Assert-WinAppFields $v @('elementId','hwnd')
    if ($v.elementId -cne $Target.Selector -or $v.hwnd -ne $WindowHandle) { throw 'Chat set receipt mismatch.' }
}
function Invoke-WinAppChatCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [System.Collections.IDictionary]$Diagnostic,[string]$Profile,[string]$Operation,[string]$Selector,[string]$Text='') {
    $args=New-WinAppChatArguments $Profile $WindowHandle $Operation $Selector $Text
    $start=New-WinAppWelcomeStart $Tool $WindowHandle -Operation read -FixtureProfile bridge-owned-catalog-4239
    $start.ArgumentList.Clear();foreach($a in $args){$start.ArgumentList.Add($a)}
    $record=[ordered]@{operation=$Operation;status='stopped';reason='identity_unavailable';exitCode=$null;
        stdoutBytes=0;processExitConfirmed=$null;timedOut=$false;outputLimitExceeded=$false}
    $Diagnostic.commands+=,$record;$child=$null
    try {
        & $CheckIdentity
        $record.reason='tool_start_uncertain'
        $child=[DesktopLive.BoundedProcess]::new($start,262144,10000)
        if (-not $child.Wait(10000)) { throw 'Chat tool timeout.' }
        $drain=[Diagnostics.Stopwatch]::StartNew()
        while(-not $child.Drained -and $drain.ElapsedMilliseconds -lt 2000){Start-Sleep -Milliseconds 20}
        $record.exitCode=$child.ExitCode;$record.stdoutBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
        if($child.TimedOut -or $child.Overflow -or -not $child.Drained -or $child.ExitCode -ne 0){throw 'Chat tool failed.'}
        & $CheckIdentity
        $record.status='observed';$record.reason='bounded_command_completed'
        return $child.Output
    } finally {
        if($null -ne $child){
            $record.timedOut=$child.TimedOut;$record.outputLimitExceeded=$child.Overflow
            $record.stdoutBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
            if($child.Exited){$record.exitCode=$child.ExitCode}
            Complete-WinAppProcess $child $record
            if(-not $record.processExitConfirmed -or $record.status -cne 'observed'){throw 'Chat command outcome uncertain.'}
        }
    }
}

# ReadPage returns the existing validated complete welcome snapshot. Command
# delegates only to Invoke-WinAppChatCommand. These seams permit pure negative
# fixtures, not arbitrary live target or prompt overrides. Parent owns evidence
# capture and independent native-request attribution; UI text is not RPC proof.
function Invoke-WinAppSyntheticChatCore([string]$Profile,[long]$WindowHandle,
    [scriptblock]$ReadPage,[scriptblock]$Command,[scriptblock]$Capture) {
    Assert-WinAppChatProfile $Profile
    $r=[ordered]@{schema=1;scope='readonly-synthetic-chat-ui-only';status='blocked';stage='initial';
        reason='chat_not_started';sendAttempts=0;uiAnswers=0;newChats=0;alphaTurns=0;betaTurns=0;
        partialActionUncertain=$false;restartAttempted=$false;rpcAttributionValidated=$false;failureCapture='not-attempted'}
    $total=[Diagnostics.Stopwatch]::StartNew();$mutating=$false
    $resolve={param($kind,$node) & $Command resolve $kind $node.Name}
    try {
        foreach($provider in @('alpha','beta')) {
            $r.stage=$provider+'-entry';$page=& $ReadPage;Assert-WinAppChatPage $page
            if($provider -eq 'beta') {
                $n=Get-WinAppChatTarget $page 'new-chat' $resolve;$mutating=$true
                $receipt=& $Command invoke $n.Selector '';Assert-WinAppWelcomeAction $receipt $WindowHandle $n invoke
                $mutating=$false;$r.newChats++;$page=& $ReadPage;Assert-WinAppChatPage $page
            }
            # Existing settings slice ends at Alpha. Close an already-open model
            # popover explicitly, never blind Escape or clicking other content.
            $current=Get-WinAppChatTarget $page 'alpha-menu' $resolve
            if($provider -eq 'beta') {
                if(@($page.Nodes|Where-Object {$_.Name -ceq 'Select model'}).Count -eq 0){
                    $mutating=$true;$receipt=& $Command expand $current.Selector '';Assert-WinAppWelcomeAction $receipt $WindowHandle $current expand;$mutating=$false
                    $page=& $ReadPage;Assert-WinAppChatPage $page
                }
                $choice=Get-WinAppChatTarget $page 'beta-choice' $resolve;$mutating=$true
                $receipt=& $Command select $choice.Selector '';Assert-WinAppWelcomeAction $receipt $WindowHandle $choice select;$mutating=$false
                if(-not(Read-WinAppModelSelected (& $Command selected $choice.Selector '') $choice)){throw 'Beta selection not confirmed.'}
                $page=& $ReadPage;Assert-WinAppChatPage $page;$current=Get-WinAppChatTarget $page 'beta-menu' $resolve
            }
            if(@($page.Nodes|Where-Object {$_.Name -ceq 'Select model'}).Count -gt 0){
                $mutating=$true;$receipt=& $Command collapse $current.Selector '';Assert-WinAppChatCollapse $receipt $WindowHandle $current;$mutating=$false
                # UIA's action receipt is not evidence of rendered closure. Wait
                # only by re-reading the same validated window; never collapse
                # again or type while the popover remains visible. Each ReadPage
                # retains its existing bounded tool call and identity checks.
                $closeWait=[Diagnostics.Stopwatch]::StartNew()
                for($closeRead=0;$closeRead -lt 8;$closeRead++){
                    if($total.ElapsedMilliseconds -gt 120000){throw 'Chat slice deadline.'}
                    $page=& $ReadPage;Assert-WinAppChatPage $page
                    if($total.ElapsedMilliseconds -gt 120000){throw 'Chat slice deadline.'}
                    if(@($page.Nodes|Where-Object {$_.Name -ceq 'Select model'}).Count -eq 0){
                        $closed=Get-WinAppChatTarget $page ($provider+'-menu') $resolve
                        if($closed.Selector -cne $current.Selector -or $closed.Name -cne $current.Name -or
                            $closed.ExpandState -cnotin @('collapsed','expanded')){throw 'Chat model menu state unavailable.'}
                        if($total.ElapsedMilliseconds -gt 120000){throw 'Chat slice deadline.'}
                        if($closed.ExpandState -ceq 'collapsed'){break}
                    }
                    if($closeRead -eq 7 -or $closeWait.ElapsedMilliseconds -ge 2000){throw 'Model popover did not close.'}
                    Start-Sleep -Milliseconds 100
                }
            }
            foreach($round in 1,2){
                if($total.ElapsedMilliseconds -gt 120000){throw 'Chat slice deadline.'}
                $r.stage=$provider+'-turn-'+$round;$page=& $ReadPage;Assert-WinAppChatPage $page
                $answer='fixture-answer-'+$provider
                $before=@($page.Nodes|Where-Object {$_.Type -ceq 'Text' -and $_.Name -ceq $answer}).Count
                if($before -ne ($round-1)){throw 'Unexpected chat history.'}
                $edit=Get-WinAppChatTarget $page composer $resolve
                Assert-WinAppChatEditable (& $Command properties $edit.Selector '') $edit
                if((Read-WinAppChatValue (& $Command get $edit.Selector '') $edit) -cne ''){throw 'Composer not empty.'}
                $prompt='synthetic-'+$provider+'-turn-'+$round;$mutating=$true
                Assert-WinAppChatSet (& $Command set $edit.Selector $prompt) $WindowHandle $edit
                if((Read-WinAppChatValue (& $Command get $edit.Selector '') $edit) -cne $prompt){throw 'Composer value not verified.'}
                $mutating=$false;$page=& $ReadPage;Assert-WinAppChatPage $page
                $send=Get-WinAppChatTarget $page send $resolve;$r.sendAttempts++;$mutating=$true
                Assert-WinAppWelcomeAction (& $Command invoke $send.Selector '') $WindowHandle $send invoke
                $mutating=$false;$wait=[Diagnostics.Stopwatch]::StartNew();$seen=$false
                do {
                    $page=& $ReadPage;Assert-WinAppChatPage $page
                    $count=@($page.Nodes|Where-Object {$_.Type -ceq 'Text' -and $_.Name -ceq $answer}).Count
                    if($count -eq $round){$seen=$true;break}
                    if($count -gt $round){throw 'Ambiguous assistant result.'}
                    Start-Sleep -Milliseconds 200
                }while($wait.ElapsedMilliseconds -lt 15000 -and $total.ElapsedMilliseconds -lt 120000)
                if(-not $seen){throw 'Assistant text not observed.'}
                $r.uiAnswers++;$r[$provider+'Turns']++; [void](& $Capture ($provider+'-turn-'+$round))
            }
        }
        $r.status='observed';$r.stage='complete';$r.reason='four_ui_text_results_observed_not_rpc_validation'
    }catch{
        # No raw exception/provider output is published. Stop at first failure,
        # retaining completed counters; never retry a possibly performed send.
        $r.reason=switch -Exact ($_.Exception.Message) {
            'Chat target absent or ambiguous.' {'target_absent_or_ambiguous'}
            'Chat target evidence unavailable.' {'target_evidence_unavailable'}
            'Chat target resolution unavailable.' {'target_resolution_unavailable'}
            'Chat resolved identity or state changed.' {'target_resolution_changed'}
            'Chat resolved menu state changed.' {'menu_resolution_state_changed'}
            'Chat resolved selector unavailable.' {'target_resolution_selector_unavailable'}
            'Hidden or duplicated chat target.' {'target_hidden_or_duplicated'}
            'Chat snapshot validation failed.' {'snapshot_validation_failed'}
            'Chat target unavailable.' {'target_unavailable'}
            'Composer permissions unavailable.' {'composer_not_proven_editable'}
            'Composer not empty.' {'composer_not_empty'}
            'Composer value not verified.' {'composer_readback_mismatch'}
            'Protected chat screen.' {'protected_screen'}
            'Model popover did not close.' {'model_popover_not_closed'}
            'Chat model menu state unavailable.' {'model_menu_state_unavailable'}
            'Assistant text not observed.' {'assistant_text_not_observed'}
            'Unexpected chat history.' {'unexpected_history'}
            default {'chat_target_or_result_unverified'}
        };$r.partialActionUncertain=$mutating
        try { [void](& $Capture 'chat-first-blocker');$r.failureCapture='observed' }
        catch { $r.failureCapture='unavailable' }
    }
    return [pscustomobject]$r
}
