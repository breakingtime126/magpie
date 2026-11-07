# Offline synthetic tests. No download, Desktop, UIA, account, or input actions.
[CmdletBinding()]
param([switch]$BreakRedaction)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'desktop-live-welcome.ps1')
. (Join-Path $PSScriptRoot 'desktop-winapp-inspect.ps1')
$count=0
function Pass([bool]$Condition,[string]$Name) { if (-not $Condition) { throw ('FAIL: '+$Name) };$script:count++ }
function Reject([scriptblock]$Action,[string]$Name) { $failed=$false;try { & $Action | Out-Null } catch { $failed=$true };Pass $failed $Name }
foreach ($file in @('desktop-winapp-inspect.ps1','desktop-winapp-inspect.tests.ps1','test-desktop-cold-start.ps1')) {
    $tokens=$null;$errors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file),[ref]$tokens,[ref]$errors)
    Pass ($errors.Count -eq 0) 'inspection scripts parse'
    foreach ($binder in @($ast.FindAll({param($node)
        ($node -is [Management.Automation.Language.AssignmentStatementAst] -and $node.Left -is [Management.Automation.Language.VariableExpressionAst]) -or
        $node -is [Management.Automation.Language.ParameterAst] -or $node -is [Management.Automation.Language.ForEachStatementAst]
    },$true))) {
        if ($binder -is [Management.Automation.Language.ParameterAst]) { $variable=$binder.Name }
        elseif ($binder -is [Management.Automation.Language.ForEachStatementAst]) { $variable=$binder.Variable }
        else { $variable=$binder.Left }
        Pass (@('PID','HOME','Host','PSHOME','Error','true','false') -notcontains ($variable.VariablePath.UserPath -split ':')[-1]) 'no automatic variable rebinding'
    }
}
Assert-WinAppOptions $true 'observe' $true $true $false '' 'winapp-inspection-v0.7.1'
Reject { Assert-WinAppOptions $true 'observe' $true $true $true '' 'winapp-inspection-v0.7.1' } 'welcome and inspection are exclusive'
Reject { Assert-WinAppOptions $true 'observe' $true $true $false 'baseline' 'winapp-inspection-v0.7.1' } 'entry and inspection are exclusive'
Reject { Assert-WinAppOptions $true 'bridge' $true $true $false '' 'winapp-inspection-v0.7.1' } 'inspection mode is explicit'
Reject { Assert-WinAppOptions $true 'observe' $true $true $false '' '' } 'one-off marker required'
Reject { Assert-WinAppOptions $true 'observe' $false $true $false '' 'winapp-inspection-v0.7.1' } 'startup pin retained'
$signature=[pscustomobject]@{Status='Valid';SignatureType='Authenticode';SignerCertificate=[pscustomobject]@{Subject='CN=Microsoft Corporation, O=Microsoft Corporation, C=US'}}
Assert-WinAppSignature $signature
$signature.Status='NotSigned';Reject { Assert-WinAppSignature $signature } 'unsigned tool rejected';$signature.Status='Valid'
$signature.SignatureType='Catalog';Reject { Assert-WinAppSignature $signature } 'catalog-only signature rejected';$signature.SignatureType='Authenticode'
$signature.SignerCertificate.Subject='CN=Example, O=Example';Reject { Assert-WinAppSignature $signature } 'wrong publisher rejected'
# Schema-derived synthetic safety fixtures, NOT captured winapp or Desktop output.
# Pinned contract: microsoft/winappCli e3cd0b94f3e7b776ac51ba0564f4908094e979b5,
# plugins/winapp/skills/winapp-ui-automation/references/ui-json-envelope.md and
# src/winapp-CLI/WinApp.UIAutomation/Models/UiElement.cs. Actual serialization must
# still be accepted in the single VM invocation; no fixture establishes UI availability.
function New-Node([string]$Name,[string]$Type='Text',[bool]$Invokable=$false) {
    return [ordered]@{type=$Type;name=$Name;isEnabled=$true;isOffscreen=$false;isInvokable=$Invokable;x=0;y=0;width=10;height=10}
}
$nodes=@((New-Node 'Which best describes your work?'),(New-Node 'Engineering' 'RadioButton' $true),
    (New-Node 'Suggest personalized tasks' 'CheckBox' $true),(New-Node 'Continue' 'Button' $true),
    (New-Node 'PRIVATE_UI_SENTINEL'))
$tree=[ordered]@{depth=8;interactive=$false;hideDisabled=$false;hideOffscreen=$false;windows=@(
    [ordered]@{hwnd=123;title='PRIVATE_TITLE_SENTINEL';windowDpi=96;scale=1;dpiAwareness='per-monitor-aware';coordinateSpace='physical-screen-pixels';elementCount=5;elements=$nodes})}
$text=$tree|ConvertTo-Json -Depth 20 -Compress
if ($BreakRedaction) {
    $definition=(Get-Command ConvertTo-WinAppSummary).Definition.Replace('return $summary',"`$summary.rawName='PRIVATE_UI_SENTINEL'; return `$summary")
    Set-Item Function:ConvertTo-WinAppSummary ([scriptblock]::Create($definition))
}
$summary=ConvertTo-WinAppSummary $text 123
Pass (($summary|ConvertTo-Json -Compress) -notmatch 'PRIVATE_') 'raw UI text never enters public summary'
Pass ($summary.nodeCount -eq 5 -and $summary.headingExactCount -eq 1 -and $summary.engineeringCount -eq 1 -and $summary.continueInvokableCount -eq 1) 'exact fixed target counts'
Reject { ConvertTo-WinAppSummary $text 124 } 'different reported HWND stops without strict identity claim'
Reject { ConvertTo-WinAppSummary '{"depth":8,"depth":8}' 123 } 'duplicate output fields rejected'
Reject { ConvertTo-WinAppSummary ('x'*262145) 123 } 'output byte limit'
Reject { ConvertTo-WinAppSummary '{invalid' 123 } 'malformed JSON rejected'
Reject { ConvertTo-WinAppSummary 'null' 123 } 'null envelope rejected'
Reject { ConvertTo-WinAppSummary ($text.Replace('"windowDpi":96','"windowDpi":null')) 123 } 'null DPI rejected'
Reject { ConvertTo-WinAppSummary ($text.Replace('"scale":1','"scale":2')) 123 } 'DPI scale mismatch rejected'
Reject { ConvertTo-WinAppSummary ($text.Replace('"isEnabled":true','"isEnabled":"true"')) 123 } 'boolean string rejected'
$tree.windows[0].elementCount=6;Reject { ConvertTo-WinAppSummary ($tree|ConvertTo-Json -Depth 20 -Compress) 123 } 'node count mismatch';$tree.windows[0].elementCount=5
$tree.windows[0].elements[0].name=' Which   best describes your work? '
$summary=ConvertTo-WinAppSummary ($tree|ConvertTo-Json -Depth 20 -Compress) 123
Pass ($summary.headingExactCount -eq 0 -and $summary.headingWhitespaceNormalizedCount -eq 1) 'normalized count is separate from exact'
$tree.windows[0].elements[0].name='Sign in'
$loginText=$tree|ConvertTo-Json -Depth 20 -Compress
Pass ((ConvertTo-WinAppSummary $loginText 123).loginMarkerCount -eq 1) 'login label is count-only'
$tree.windows[0].elements[0].name='Which best describes your work?'
$tree.windows[0].elements[0].unknown='PRIVATE_FIELD_SENTINEL'
Reject { ConvertTo-WinAppSummary ($tree|ConvertTo-Json -Depth 20 -Compress) 123 } 'unknown node field rejected'
$tree.windows[0].elements[0].Remove('unknown')
$deep=New-Node 'leaf'
for($i=0;$i -lt 9;$i++) { $parent=New-Node 'parent';$parent.children=@($deep);$deep=$parent }
$tree.windows[0].elements=@($deep);$tree.windows[0].elementCount=10
Reject { ConvertTo-WinAppSummary ($tree|ConvertTo-Json -Depth 30 -Compress) 123 } 'rendered depth bound'

# Synthetic nested schema, not a reconstruction of the private actual tree.
$deep16=New-Node 'Engineering';$deep16.hasMoreChildren=$true
for($i=0;$i -lt 16;$i++) { $parent=New-Node 'PRIVATE_DEEP_SENTINEL';$parent.children=@($deep16);$deep16=$parent }
$tree16=[ordered]@{depth=16;interactive=$false;hideDisabled=$false;hideOffscreen=$false;windows=@(
    [ordered]@{hwnd=123;windowDpi=96;scale=1;dpiAwareness='per-monitor-aware';coordinateSpace='physical-screen-pixels';elementCount=17;elements=@($deep16)})}
$text16=$tree16|ConvertTo-Json -Depth 50 -Compress
$summary16=ConvertTo-WinAppSummary $text16 123 -RenderedDepth 16
Pass ($summary16.nodeCount -eq 17 -and $summary16.maxRenderedDepth -eq 16 -and
    $summary16.engineeringCount -eq 1 -and $summary16.truncatedNodeCount -eq 1 -and
    $summary16.requiredTargetStatus -ceq 'missing') 'selected depth16 reaches boundary without inventing target completeness'
Pass (($summary16|ConvertTo-Json -Compress) -notmatch 'PRIVATE_') 'expanded output remains redacted'
Reject { ConvertTo-WinAppSummary $text16 123 } 'default8 rejects depth16 envelope'
Reject { ConvertTo-WinAppSummary $text 123 -RenderedDepth 16 } 'depth16 rejects depth8 envelope'
$parent=New-Node 'parent';$parent.children=@($deep16)
$tree16.windows[0].elements=@($parent);$tree16.windows[0].elementCount=18
Reject { ConvertTo-WinAppSummary ($tree16|ConvertTo-Json -Depth 50 -Compress) 123 -RenderedDepth 16 } 'depth17 rejected at selected depth16'
Reject { ConvertTo-WinAppSummary ('x'*262145) 123 -RenderedDepth 16 } 'depth16 keeps byte bound'
foreach($badDepth in @(0,9,17,128)) {
    Reject { ConvertTo-WinAppSummary $text 123 -RenderedDepth $badDepth } 'unreviewed rendered depth rejected'
}
$depthAssignments=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
    $n.Left.Extent.Text -ceq '$winAppRenderedDepth'},$true))
Pass ($depthAssignments.Count -eq 1 -and $depthAssignments[0].Right.Extent.Text -ceq '$(if ($EnableRendererAccessibility) { 16 } else { 8 })') 'expanded depth remains scoped to reviewed accessibility profile'
foreach($callName in @('New-WinAppStart','Invoke-WinAppInspection')) {
    $calls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq $callName},$true))
    $expectedSuffix='-RenderedDepth $winAppRenderedDepth'
    if ($callName -ceq 'Invoke-WinAppInspection') { $expectedSuffix+=' -NativeOriginal:$EnableNativeReadOnlyInspection' }
    Pass ($calls.Count -eq 1 -and $calls[0].Extent.Text.EndsWith($expectedSuffix)) 'launch and validation use the same selected depth'
}

$unconfirmed=[pscustomobject]@{Exited=$false;Stopped=$false}
$unconfirmed|Add-Member ScriptMethod Stop { $this.Stopped=$true }
$unconfirmed|Add-Member ScriptMethod Wait { param($milliseconds) return $false }
$unconfirmed|Add-Member ScriptMethod Dispose { }
$cleanup=[ordered]@{status='observed';reason='read_only_target_labels_observed';processExitConfirmed=$false}
Complete-WinAppProcess $unconfirmed $cleanup
Pass ($unconfirmed.Stopped -and $cleanup.status -ceq 'stopped' -and $cleanup.reason -ceq 'tool_exit_unconfirmed' -and -not $cleanup.processExitConfirmed) 'unconfirmed exit cannot succeed'
$unconfirmed.Exited=$true
$unconfirmed|Add-Member ScriptMethod Dispose { throw 'synthetic disposal failure' } -Force
$cleanup=[ordered]@{status='observed';reason='read_only_target_labels_observed';processExitConfirmed=$false}
Complete-WinAppProcess $unconfirmed $cleanup
Pass ($cleanup.status -ceq 'stopped' -and $cleanup.reason -ceq 'tool_cleanup_unconfirmed') 'disposal failure cannot succeed'

Initialize-LiveSupervisor
$scratch=Join-Path ([IO.Path]::GetTempPath()) ('winapp-offline-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($scratch)
$savedRoot=$env:SystemRoot;$savedWorkflow=$env:WINAPP_UI_WORKFLOW_ID;$savedSecret=$env:WINAPP_TEST_PRIVATE_SENTINEL
try {
    if (-not $env:SystemRoot) { $env:SystemRoot='/' }
    $env:WINAPP_UI_WORKFLOW_ID='must-not-inherit';$env:WINAPP_TEST_PRIVATE_SENTINEL='must-not-inherit'
    $hostExecutable=(Get-Process -Id $PID).Path
    $tool=[pscustomobject]@{Executable=$hostExecutable;Directory=$scratch}
    $start=New-WinAppStart $tool 123
    Pass (($start.ArgumentList -join ' ') -ceq 'ui inspect -w 123 --depth 8 --json') 'only requested inspect command'
    $expandedStart=New-WinAppStart $tool 123 -RenderedDepth 16
    Pass (($expandedStart.ArgumentList -join ' ') -ceq 'ui inspect -w 123 --depth 16 --json') 'expanded depth changes only the requested output depth'
    Pass ($start.Environment['WINAPP_CLI_TELEMETRY_OPTOUT'] -ceq '1') 'telemetry upload opt-out'
    Pass ($start.Environment['WINAPP_UI_LOCK_DIRECTORY'] -ceq (Join-Path $scratch 'coordination')) 'fresh coordination override'
    Pass (-not $start.Environment.ContainsKey('WINAPP_UI_WORKFLOW_ID') -and -not $start.Environment.ContainsKey('WINAPP_TEST_PRIVATE_SENTINEL')) 'explicit child environment only'
    if ($savedRoot) { Pass ($start.Environment['USERPROFILE'] -ceq $env:USERPROFILE) 'no profile spoofing' }
    [void][IO.Directory]::CreateDirectory((Join-Path $scratch 'coordination'))
    Reject { New-WinAppStart $tool 123 } 'existing coordination directory rejected'
    function New-Child([string]$Body) {
        $file=Join-Path $scratch ([guid]::NewGuid().ToString('N')+'.ps1')
        [IO.File]::WriteAllText($file,$Body)
        return (New-LiveStart -Executable $hostExecutable -Directory $scratch -Welcome -Arguments @('-NoProfile','-NonInteractive','-File',$file))
    }
    $script:constructorChild=$null;$constructorFailed=$false
    try {
        [DesktopLive.BoundedProcess]::TestFailureAfterStart((New-Child 'Start-Sleep -Seconds 60'),[Action[int]]{
            param($childId)
            $script:constructorChild=[Diagnostics.Process]::GetProcessById($childId)
            $null=$script:constructorChild.Handle
        })
    } catch { $constructorFailed=$_.Exception.ToString().Contains('owned process exit confirmed') }
    try {
        Pass ($constructorFailed -and $null -ne $script:constructorChild -and $script:constructorChild.WaitForExit(1000)) 'post-start constructor failure reaps its exact held child'
    } finally {
        if ($null -ne $script:constructorChild) {
            if (-not $script:constructorChild.HasExited) { $script:constructorChild.Kill();[void]$script:constructorChild.WaitForExit(5000) }
            $script:constructorChild.Dispose()
        }
    }
    Reject { [DesktopLive.BoundedProcess]::new((New-Child 'exit 0'),0,1000) } 'invalid byte limit rejected before start'
    Reject { [DesktopLive.BoundedProcess]::new((New-Child 'exit 0'),1024,0) } 'invalid deadline rejected before start'
    $emit="[Console]::Out.WriteLine('"+$text.Replace("'","''")+"')"
    $result=Invoke-WinAppInspection (New-Child $emit) 123 { }
    Pass ($result.status -ceq 'observed' -and $result.actions -eq 0 -and $result.attempts -eq 1 -and $result.afterIdentity -and $result.processExitConfirmed) 'one real child read without actions'
    $result=Invoke-WinAppInspection (New-Child $emit) 123 { throw 'synthetic identity error' }
    Pass ($result.attempts -eq 0 -and $result.status -ceq 'stopped') 'identity failure prevents invocation'
    $script:identityCalls=0
    $result=Invoke-WinAppInspection (New-Child $emit) 123 { $script:identityCalls++;if($script:identityCalls -eq 2){throw 'synthetic identity change'} }
    Pass ($result.reason -ceq 'identity_changed' -and $null -eq $result.summary) 'changed owner prevents accepted summary'
    $result=Invoke-WinAppInspection (New-Child ("[Console]::Out.WriteLine('"+$loginText.Replace("'","''")+"')")) 123 { }
    Pass ($result.reason -ceq 'login_marker_observed' -and $result.status -ceq 'stopped') 'login observation stops'
    $protectedText=$text.Replace('PRIVATE_UI_SENTINEL','Allow access')
    $result=Invoke-WinAppInspection (New-Child ("[Console]::Out.WriteLine('"+$protectedText.Replace("'","''")+"')")) 123 { }
    Pass ($result.reason -ceq 'protected_marker_observed' -and $result.status -ceq 'stopped') 'permission observation stops'
    $missingText=$text.Replace('Engineering','Different label')
    $result=Invoke-WinAppInspection (New-Child ("[Console]::Out.WriteLine('"+$missingText.Replace("'","''")+"')")) 123 { }
    Pass ($result.reason -ceq 'required_targets_missing' -and $result.status -ceq 'stopped' -and $result.summary.requiredTargetStatus -ceq 'missing') 'missing target names never claim observed controls'
    $offscreenText=$text.Replace('"isOffscreen":false','"isOffscreen":true')
    $result=Invoke-WinAppInspection (New-Child ("[Console]::Out.WriteLine('"+$offscreenText.Replace("'","''")+"')")) 123 { }
    Pass ($result.reason -ceq 'required_targets_offscreen' -and $result.status -ceq 'stopped') 'offscreen target labels do not count as visible'
    $result=Invoke-WinAppInspection (New-Child "[Console]::Out.WriteLine('PRIVATE_ERROR_SENTINEL');exit 2") 123 { }
    Pass ($result.reason -ceq 'tool_exit_nonzero' -and ($result|ConvertTo-Json -Depth 10 -Compress) -notmatch 'PRIVATE_') 'nonzero raw error discarded'
    $result=Invoke-WinAppInspection (New-Child "[Console]::Out.Write('x'*262145)") 123 { }
    Pass ($result.reason -ceq 'tool_output_limit') 'real child stdout overflow stops'
    $result=Invoke-WinAppInspection (New-Child "[Console]::Error.Write('x'*262145)") 123 { }
    Pass ($result.reason -ceq 'tool_output_limit') 'real child stderr overflow stops'
    $result=Invoke-WinAppInspection (New-Child 'Start-Sleep -Seconds 40') 123 { }
    Pass ($result.reason -ceq 'tool_timeout' -and $result.attempts -eq 1) 'real child deadline stops without retry'
} finally {
    $env:SystemRoot=$savedRoot;$env:WINAPP_UI_WORKFLOW_ID=$savedWorkflow;$env:WINAPP_TEST_PRIVATE_SENTINEL=$savedSecret
    Remove-Item -LiteralPath $scratch -Recurse -Force
}
Write-Output ('PASS: '+$count+' official-tool observation fixture assertions.')
