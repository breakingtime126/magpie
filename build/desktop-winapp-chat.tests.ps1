# Pure fixtures: no Desktop/process/native RPC/setup/network/credentials.
$ErrorActionPreference='Stop';Set-StrictMode -Version Latest
foreach($f in @('desktop-live-welcome.ps1','desktop-winapp-inspect.ps1','desktop-winapp-welcome.ps1','desktop-winapp-chat.ps1')){
    $t=$null;$e=$null;$null=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $f),[ref]$t,[ref]$e)
    if($e.Count){throw 'Chat helper syntax failed.'};. (Join-Path $PSScriptRoot $f)
}
$script:checks=0
function Pass($v,$n){if(-not $v){throw ('FAIL: '+$n)};$script:checks++}
function Reject([scriptblock]$b,$n){$failed=$false;try{&$b}catch{$failed=$true};Pass $failed $n}
function N($name,$type,$selector){[pscustomobject]@{Name=$name;Type=$type;Selector=$selector;Enabled=$true;Invokable=$true}}
$profile='bridge-readonly-synthetic-chat-4239';$edit=N 'Do anything' Edit 'txt-doanything-abcd'
$page=[pscustomobject]@{Nodes=@($edit)}
Pass ((Get-WinAppChatTarget $page composer).Selector -ceq $edit.Selector) 'exact composer'
Reject {Get-WinAppChatTarget ([pscustomobject]@{Nodes=@((N 'Search' Edit 'txt-search-abcd'))}) composer} 'unique other Edit rejected'
Reject {Get-WinAppChatTarget ([pscustomobject]@{Nodes=@($edit,$edit)}) composer} 'duplicate composer rejected'
$edit.Enabled=$false;Reject {Get-WinAppChatTarget $page composer} 'disabled rejected';$edit.Enabled=$true
$edit.Selector='btn-doanything-abcd';Reject {Get-WinAppChatTarget $page composer} 'wrong prefix rejected';$edit.Selector='txt-doanything-abcd'
Reject {Get-WinAppChatTarget ([pscustomobject]@{Nodes=@((N '' Button 'btn-send-abcd'))}) send} 'anonymous arrow rejected'
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
$read={ [pscustomobject]@{Nodes=@((N 'Synthetic Alpha [alpha] None' Button 'btn-syntheticalpha-abcd'))} }
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
    if($script:state.menu){$nodes+=@((N 'Select model' Text 'lbl-selectmodel-abcd'),(N 'Synthetic Beta [beta]' RadioButton 'rdo-syntheticbeta-abcd'))}
    for($i=0;$i -lt $script:state.answers;$i++){$nodes+=,(N ('fixture-answer-'+$script:state.provider) Text 'lbl-answer-abcd')}
    [pscustomobject]@{Nodes=$nodes}
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
