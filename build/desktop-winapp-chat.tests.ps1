# Pure fixtures: no Desktop/process/native RPC/setup/network/credentials.
$ErrorActionPreference='Stop';Set-StrictMode -Version Latest
foreach($f in @('desktop-live-welcome.ps1','desktop-winapp-inspect.ps1','desktop-winapp-welcome.ps1','desktop-winapp-chat.ps1')){
    $t=$null;$e=$null;$null=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $f),[ref]$t,[ref]$e)
    if($e.Count){throw 'Chat helper syntax failed.'};. (Join-Path $PSScriptRoot $f)
}
$script:checks=0
function Pass($v,$n){if(-not $v){throw ('FAIL: '+$n)};$script:checks++}
function Reject([scriptblock]$b,$n){$failed=$false;try{&$b}catch{$failed=$true};Pass $failed $n}
function N($name,$type,$selector){[pscustomobject]@{Name=$name;Type=$type;Selector=$selector;Enabled=$true;Invokable=$true;ExpandState=$(if($name -cin @('Synthetic Alpha [alpha] None','Synthetic Beta [beta] None')){'collapsed'}else{''})}}
function ChatPage($nodes) {
    $counts=@{}
    foreach($n in $nodes){
        if(-not $counts.ContainsKey($n.Name)){$counts[$n.Name]=@{total=0;hidden=0}}
        $counts[$n.Name].total++
    }
    [pscustomobject]@{Nodes=@($nodes);ChatTargets=$counts}
}
$profile='bridge-readonly-synthetic-chat-4239';$edit=N 'Do anything' Edit 'txt-doanything-abcd'
$page=(ChatPage @($edit))
Pass ((Get-WinAppChatTarget $page composer).Selector -ceq $edit.Selector) 'exact composer'
Reject {Get-WinAppChatTarget (ChatPage @((N 'Search' Edit 'txt-search-abcd'))) composer} 'unique other Edit rejected'
Reject {Get-WinAppChatTarget (ChatPage @($edit,$edit)) composer} 'duplicate composer rejected'
$edit.Enabled=$false;Reject {Get-WinAppChatTarget $page composer} 'disabled rejected';$edit.Enabled=$true
$edit.Selector='btn-doanything-abcd';Reject {Get-WinAppChatTarget $page composer} 'wrong prefix rejected';$edit.Selector='txt-doanything-abcd'
Reject {Get-WinAppChatTarget (ChatPage @((N '' Button 'btn-send-abcd'))) send} 'anonymous arrow rejected'
Reject {New-WinAppChatArguments '' 7 set $edit.Selector 'synthetic-alpha-turn-1'} 'profile required'
foreach($text in @('hello','synthetic-alpha-turn-3','synthetic-beta-turn-1; run','https://example.com','',"synthetic-alpha-turn-1`n")){
    Reject {New-WinAppChatArguments $profile 7 set $edit.Selector $text} 'nonfixed input rejected'
}
foreach($p in @('alpha','beta')){foreach($n in 1,2){
    $text='synthetic-'+$p+'-turn-'+$n;$a=New-WinAppChatArguments $profile 7 set $edit.Selector $text
    Pass (($a -join '|') -ceq ('ui|set-value|txt-doanything-abcd|'+$text+'|-w|7|--json')) 'exact argv'
}}
Reject {New-WinAppChatArguments $profile 7 invoke $edit.Selector} 'no invoke Edit'
Reject {New-WinAppChatArguments $profile 7 get $edit.Selector 'payload'} 'no text on read'
Reject {New-WinAppChatArguments $profile 0 get $edit.Selector} 'invalid window'
function Editable($password='False',$readOnly='False'){
    [ordered]@{elementId=$edit.Selector;element=[ordered]@{selector=$edit.Selector;name=$edit.Name;type='Edit';isEnabled=$true;isOffscreen=$false};properties=[ordered]@{IsPassword=$password;IsReadOnly=$readOnly}}|ConvertTo-Json -Depth 6 -Compress
}
Assert-WinAppChatEditable (Editable) $edit;$script:checks++
foreach($value in @('True','false','', $null,$false)){
    Reject {Assert-WinAppChatEditable (Editable $value 'False') $edit} 'password must be exact native False string'
    Reject {Assert-WinAppChatEditable (Editable 'False' $value) $edit} 'readonly must be exact native False string'
}
Pass ((Read-WinAppChatValue '{"elementId":"txt-doanything-abcd","text":"synthetic-alpha-turn-1"}' $edit) -ceq 'synthetic-alpha-turn-1') 'value read exact'
Reject {Read-WinAppChatValue '{"elementId":"txt-other-abcd","text":""}' $edit} 'value identity mismatch'
Reject {Read-WinAppChatValue '{"elementId":"txt-doanything-abcd","text":null}' $edit} 'null text rejected'
foreach($name in @('Finish Windows setup to continue','Sign in','Approve',"Couldn't update model settings",('Couldn'+[char]0x2019+'t update model settings'))){
    Reject {Assert-WinAppChatPage ([pscustomobject]@{Nodes=@((N $name Text 'lbl-protected-abcd'))})} 'protected page'
}
# Absence of a real composer ends the slice without input or send, even when
# model name and main surface appear otherwise plausible.
$script:commands=0;$script:captures=0
$read={ ChatPage @((N 'Synthetic Alpha [alpha] None' Button 'btn-syntheticalpha-abcd')) }
$command={param($op,$selector,$text) $script:commands++;throw 'No command expected.'}
$capture={param($stage) $script:captures++}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $read $command $capture
Pass ($r.status -ceq 'blocked' -and $r.sendAttempts -eq 0 -and $script:commands -eq 0 -and $script:captures -eq 1) 'missing composer stops without mutation'
Pass (-not $r.rpcAttributionValidated -and -not $r.restartAttempted) 'no fake RPC/restart proof'
Write-Output ('SYNTHETIC_CHAT_HELPER_CHECKS='+$script:checks)
# Full state-machine fixture. It simulates UI only and must never set the RPC
# attribution flag. All live commands remain replaced by these pure callbacks.
$script:state=@{provider='alpha';answers=0;draft='';menu=$false;sends=0;newChats=0;failSend=$false}
$readHappy={
    $nodes=@((N ('Synthetic '+$(if($script:state.provider -eq 'alpha'){'Alpha [alpha]'}else{'Beta [beta]'})+' None') Button 'btn-currentmodel-abcd'),
        (N 'Do anything' Edit 'txt-doanything-abcd'),(N 'Send' Button 'btn-send-abcd'),(N 'New chat' Button 'btn-newchat-abcd'))
    $nodes[0].ExpandState=$(if($script:state.menu){'expanded'}else{'collapsed'})
    if($script:state.menu){$nodes+=@((N 'Select model' Text 'lbl-selectmodel-abcd'),(N 'Synthetic Beta [beta]' RadioButton 'rdo-syntheticbeta-abcd'))}
    for($i=0;$i -lt $script:state.answers;$i++){$nodes+=,(N ('fixture-answer-'+$script:state.provider) Text 'lbl-answer-abcd')}
    ChatPage $nodes
}
$commandHappy={param($op,$selector,$text)
    switch($op){
        properties { return (Editable) }
        get { return (@{elementId=$selector;text=$script:state.draft}|ConvertTo-Json -Compress) }
        set { $script:state.draft=$text;return (@{elementId=$selector;hwnd=7}|ConvertTo-Json -Compress) }
        selected {return (@{elementId=$selector;element=@{selector=$selector;name='Synthetic Beta [beta]';type='RadioButton';isEnabled=$true;isOffscreen=$false};properties=@{IsSelected='True'}}|ConvertTo-Json -Depth 6 -Compress)}
        invoke {
            if($selector -eq 'btn-send-abcd'){
                $script:state.sends++
                if($script:state.failSend){throw 'uncertain send'}
                $script:state.answers++;$script:state.draft=''
            }elseif($selector -eq 'btn-newchat-abcd'){$script:state.newChats++;$script:state.answers=0;$script:state.provider='alpha'}
            else{throw 'unknown fixture action'}
        }
        expand {$script:state.menu=$true}
        collapse {$script:state.menu=$false}
        select {$script:state.provider='beta'}
        default {throw 'unknown fixture operation'}
    }
    $pattern=@{invoke='InvokePattern';expand='ExpandCollapsePattern';collapse='ExpandCollapsePattern';select='SelectionItemPattern'}[$op]
    return (@{elementId=$selector;hwnd=7;requestedAction=$op;performedAction=$op;pattern=$pattern}|ConvertTo-Json -Compress)
}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $readHappy $commandHappy {param($stage)}
Pass ($r.status -ceq 'observed' -and $r.uiAnswers -eq 4 -and $r.sendAttempts -eq 4 -and $r.alphaTurns -eq 2 -and $r.betaTurns -eq 2 -and $r.newChats -eq 1) 'four UI-only turns'
Pass (-not $r.rpcAttributionValidated -and -not $r.restartAttempted) 'happy UI does not claim RPC or restart'
$script:state=@{provider='alpha';answers=0;draft='';menu=$false;sends=0;newChats=0;failSend=$true}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $readHappy $commandHappy {param($stage)}
Pass ($r.status -ceq 'blocked' -and $r.sendAttempts -eq 1 -and $script:state.sends -eq 1 -and $r.uiAnswers -eq 0 -and $r.partialActionUncertain) 'uncertain send not retried'
Pass ($script:state.newChats -eq 0) 'no later provider after uncertain send'
Write-Output ('SYNTHETIC_CHAT_HELPER_FINAL_CHECKS='+$script:checks)

$script:state=@{provider='alpha';answers=0;draft='';menu=$false;sends=0;newChats=0;failSend=$true}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $readHappy $commandHappy {param($stage) throw 'capture unavailable'}
Pass ($r.status -ceq 'blocked' -and $r.sendAttempts -eq 1 -and $r.partialActionUncertain -and $r.failureCapture -ceq 'unavailable') 'capture failure retains partial sends'
Write-Output ('SYNTHETIC_CHAT_CAPTURE_CHECKS='+$script:checks)

# A hidden future target must not prevent operating on the unique current model
# opener, but remains rejected if requested as the actual target.
$scopePage=ChatPage @((N 'Synthetic Alpha [alpha] None' Button 'btn-syntheticalpha-abcd'))
$scopePage.ChatTargets['Do anything']=@{total=2;hidden=1}
Pass ((Get-WinAppChatTarget $scopePage 'alpha-menu').Selector -ceq 'btn-syntheticalpha-abcd') 'future hidden composer does not block opener'
Reject {Get-WinAppChatTarget $scopePage composer} 'actual hidden duplicated composer still rejected'
$scopePage.ChatTargets['Synthetic Alpha [alpha] None']=@{total=2;hidden=1}
Reject {Get-WinAppChatTarget $scopePage 'alpha-menu'} 'actual opener hidden duplicate rejected'
Reject {Get-WinAppChatTarget ([pscustomobject]@{Nodes=@($edit)}) composer} 'missing full-tree evidence rejected'
Write-Output ('SYNTHETIC_CHAT_TARGET_SCOPE_CHECKS='+$script:checks)
# Match the already-used official promoted-ID lookup, including the currently
# expanded menu state. No coordinate/ancestor/default-action fallback.
$promoted=[pscustomobject]@{Name='Synthetic Alpha [alpha] None';Type='Button';Selector='model-picker';AutomationId='model-picker';Enabled=$true;Invokable=$true;ExpandState='expanded'}
$promotedPage=ChatPage @($promoted)
function ResolvedMenu([string]$State='expanded',[string]$Name='Synthetic Alpha [alpha] None',[string]$Selector='btn-modelpicker-abcd'){
    [ordered]@{elementId=$Selector;element=[ordered]@{name=$Name;type='Button';automationId='model-picker';selector=$Selector;
        isEnabled=$true;isOffscreen=$false;isInvokable=$true;expandState=$State};properties=@{AutomationId='model-picker'}}|ConvertTo-Json -Depth 6 -Compress
}
$script:resolveCount=0
$resolver={param($kind,$node) $script:resolveCount++;Pass ($kind -ceq 'alpha-menu' -and $node.AutomationId -ceq 'model-picker') 'fixed resolution identity';ResolvedMenu}
$target=Get-WinAppChatTarget $promotedPage 'alpha-menu' $resolver
Pass ($target.Selector -ceq 'btn-modelpicker-abcd' -and $target.ExpandState -ceq 'expanded' -and $script:resolveCount -eq 1) 'expanded promoted target resolved once'
Reject {Get-WinAppChatTarget $promotedPage 'alpha-menu'} 'promoted target requires resolver'
Reject {Read-WinAppChatResolvedTarget (ResolvedMenu 'collapsed') $promoted 'alpha-menu'} 'resolve preserves expanded state'
Reject {Read-WinAppChatResolvedTarget (ResolvedMenu 'expanded' 'Other') $promoted 'alpha-menu'} 'resolved name mismatch'
Reject {Read-WinAppChatResolvedTarget (ResolvedMenu 'expanded' 'Synthetic Alpha [alpha] None' 'btn-other-abcd') $promoted 'alpha-menu'} 'resolved slug identity mismatch'
$argv=New-WinAppChatArguments $profile 7 resolve alpha-menu 'Synthetic Alpha [alpha] None'
Pass (($argv -join '|') -ceq 'ui|get-property|Synthetic Alpha [alpha] None|--type|Button|--property|AutomationId|-w|7|--json') 'exact readonly resolution argv'
Reject {New-WinAppChatArguments $profile 7 resolve alpha-menu 'Other'} 'unknown resolve name rejected'
Reject {New-WinAppChatArguments $profile 7 resolve other 'Synthetic Alpha [alpha] None'} 'unknown resolve kind rejected'
# Fixed failure reason distinguishes snapshot validation from target checks;
# it never prints raw exceptions, user paths or UI content.
$r=Invoke-WinAppSyntheticChatCore $profile 7 {throw 'Chat snapshot validation failed.'} $command {param($stage)}
Pass ($r.reason -ceq 'snapshot_validation_failed' -and $r.sendAttempts -eq 0) 'snapshot failure classified'
$r=Invoke-WinAppSyntheticChatCore $profile 7 { $scopePage } $command {param($stage)}
Pass ($r.reason -ceq 'target_hidden_or_duplicated' -and $r.sendAttempts -eq 0) 'actual duplicate target classified'
Write-Output ('SYNTHETIC_CHAT_RESOLUTION_CHECKS='+$script:checks)
# Unit-isolate the additional raw-target scan from the separately tested complete
# winapp schema validator. The old chat scan throws on this future hidden Edit.
$originalWelcomeSnapshot=(Get-Command ConvertTo-WinAppWelcomeSnapshot).ScriptBlock
try {
    function ConvertTo-WinAppWelcomeSnapshot([string]$Text,[long]$WindowHandle,[string]$FixtureProfile='') {
        [pscustomobject]@{Nodes=@((N 'Synthetic Alpha [alpha] None' Button 'btn-syntheticalpha-abcd'))}
    }
    $scanJson='{"windows":[{"elements":[{"name":"Synthetic Alpha [alpha] None","type":"Button","isOffscreen":false},{"name":"Do anything","type":"Edit","isOffscreen":true}]}]}'
    $scanned=ConvertTo-WinAppChatSnapshot $scanJson 7
    Pass ($scanned.ChatTargets['Do anything'].hidden -eq 1) 'scan retains hidden future target evidence'
    Pass ((Get-WinAppChatTarget $scanned 'alpha-menu').Selector -ceq 'btn-syntheticalpha-abcd') 'raw future hidden target does not abort opener'
    Reject {Get-WinAppChatTarget $scanned composer} 'raw future target still fails when requested'
} finally {
    Set-Item -LiteralPath Function:ConvertTo-WinAppWelcomeSnapshot -Value $originalWelcomeSnapshot
}
Write-Output ('SYNTHETIC_CHAT_RAW_SCAN_CHECKS='+$script:checks)
# Exercise the core's real nested $resolve -> $Command scriptblock wiring, not
# just GetTarget directly. Collapse the open promoted-ID menu, then stop before
# any input/send because this fixture intentionally has no composer.
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@()}
$corePromotedRead={
    $script:corePromoted.reads++
    $opener=[pscustomobject]@{Name='Synthetic Alpha [alpha] None';Type='Button';Selector='model-picker';AutomationId='model-picker';Enabled=$true;Invokable=$true;ExpandState=$(if($script:corePromoted.menu){'expanded'}else{'collapsed'})}
    $nodes=@($opener)
    if($script:corePromoted.menu){$nodes+=,(N 'Select model' Text 'lbl-selectmodel-abcd')}
    ChatPage $nodes
}
$corePromotedCommand={param($operation,$selector,$text)
    $script:corePromoted.operations+=,$operation
    if($operation -ceq 'resolve'){
        if($selector -cne 'alpha-menu' -or $text -cne 'Synthetic Alpha [alpha] None'){throw 'Unexpected core resolution.'}
        return (ResolvedMenu $(if($script:corePromoted.menu){'expanded'}else{'collapsed'}))
    }
    if($operation -ceq 'collapse'){
        if($selector -cne 'btn-modelpicker-abcd' -or $text -cne '' -or -not $script:corePromoted.menu){throw 'Unexpected core collapse.'}
        $script:corePromoted.menu=$false
        return (@{elementId=$selector;hwnd=7;requestedAction='collapse';performedAction='collapse';pattern='ExpandCollapsePattern'}|ConvertTo-Json -Compress)
    }
    throw 'Unexpected core target operation.'
}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $corePromotedRead $corePromotedCommand {param($stage) $script:corePromoted.captures++}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse,resolve' -and -not $script:corePromoted.menu -and $script:corePromoted.reads -eq 3) 'core promoted resolution then collapse wiring'
Pass ($r.status -ceq 'blocked' -and $r.stage -ceq 'alpha-turn-1' -and $r.reason -ceq 'target_absent_or_ambiguous' -and $r.sendAttempts -eq 0 -and $r.uiAnswers -eq 0 -and -not $r.partialActionUncertain -and $script:corePromoted.captures -eq 1) 'core stops at missing composer with zero sends'
Write-Output ('SYNTHETIC_CHAT_CORE_RESOLUTION_CHECKS='+$script:checks)

# A successful action receipt can precede rendering, or the renderer may never
# close. These pure callbacks distinguish both without repeating an action.
$deferredCollapse={param($operation,$selector,$text)
    $receipt=& $corePromotedCommand $operation $selector $text
    if($operation -ceq 'collapse'){$script:corePromoted.menu=$true;$script:corePromoted.pendingClose=2}
    return $receipt
}
$delayedCloseRead={
    if($script:corePromoted.pendingClose -gt 0){
        $script:corePromoted.pendingClose--
        if($script:corePromoted.pendingClose -eq 0){$script:corePromoted.menu=$false}
    }
    & $corePromotedRead
}
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $delayedCloseRead $deferredCollapse {param($stage) $script:corePromoted.captures++}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse,resolve' -and $script:corePromoted.reads -eq 4 -and -not $script:corePromoted.menu) 'delayed rendered closure uses readonly recheck and one collapse'
Pass ($r.stage -ceq 'alpha-turn-1' -and $r.reason -ceq 'target_absent_or_ambiguous' -and $r.sendAttempts -eq 0 -and -not $r.partialActionUncertain) 'delayed closure does not bypass composer checks'
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $corePromotedRead $deferredCollapse {param($stage) $script:corePromoted.captures++}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse' -and $script:corePromoted.reads -ge 2 -and $script:corePromoted.reads -le 9 -and $script:corePromoted.menu) 'persistent popover has bounded reads and no repeated action'
Pass ($r.stage -ceq 'alpha-entry' -and $r.reason -ceq 'model_popover_not_closed' -and $r.sendAttempts -eq 0 -and $r.uiAnswers -eq 0 -and -not $r.partialActionUncertain -and $script:corePromoted.captures -eq 1) 'persistent popover stops before any input or send with exact reason'
$protectedAfterCollapse={
    if($script:corePromoted.pendingClose -gt 0){return (ChatPage @((N 'Sign in' Text 'lbl-signin-abcd')))}
    & $corePromotedRead
}
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $protectedAfterCollapse $deferredCollapse {param($stage) $script:corePromoted.captures++}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse' -and $r.reason -ceq 'protected_screen' -and $r.sendAttempts -eq 0) 'protected screen during closure stops immediately'
$snapshotFailureAfterCollapse={
    if($script:corePromoted.pendingClose -gt 0){throw 'Chat snapshot validation failed.'}
    & $corePromotedRead
}
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $snapshotFailureAfterCollapse $deferredCollapse {param($stage) $script:corePromoted.captures++}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse' -and $r.reason -ceq 'snapshot_validation_failed' -and $r.sendAttempts -eq 0) 'invalid snapshot during closure is not retried'
Write-Output ('SYNTHETIC_CHAT_POPOVER_CHECKS='+$script:checks)

# A disappearing title alone is insufficient: the same model button must be
# observed collapsed, and uncertain action receipts must not enter the wait.
$noTitleExpanded={
    $page=& $corePromotedRead
    if($script:corePromoted.pendingClose -gt 0){$page.Nodes=@($page.Nodes|Where-Object {$_.Name -cne 'Select model'})}
    return $page
}
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $noTitleExpanded $deferredCollapse {param($stage)}
Pass (@($script:corePromoted.operations|Where-Object {$_ -ceq 'collapse'}).Count -eq 1 -and $r.reason -ceq 'model_popover_not_closed' -and $r.sendAttempts -eq 0) 'missing title with expanded button never permits typing'
$badCollapseReceipt={param($operation,$selector,$text)
    if($operation -ceq 'collapse'){$script:corePromoted.operations+=,$operation;return '{}'}
    & $corePromotedCommand $operation $selector $text
}
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $corePromotedRead $badCollapseReceipt {param($stage)}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse' -and $script:corePromoted.reads -eq 1 -and $r.partialActionUncertain -and $r.sendAttempts -eq 0) 'uncertain collapse receipt stops without polling or repeating'
Write-Output ('SYNTHETIC_CHAT_CLOSURE_STATE_CHECKS='+$script:checks)

$changedClosedTarget={
    if($script:corePromoted.pendingClose -gt 0){return (ChatPage @((N 'Synthetic Alpha [alpha] None' Button 'btn-different-abcd')))}
    & $corePromotedRead
}
$script:corePromoted=@{menu=$true;reads=0;captures=0;operations=@();pendingClose=0}
$r=Invoke-WinAppSyntheticChatCore $profile 7 $changedClosedTarget $deferredCollapse {param($stage)}
Pass (($script:corePromoted.operations -join ',') -ceq 'resolve,collapse' -and $r.reason -ceq 'model_menu_state_unavailable' -and $r.sendAttempts -eq 0) 'changed model target during closure stops without input'
Write-Output ('SYNTHETIC_CHAT_CLOSURE_IDENTITY_CHECKS='+$script:checks)
