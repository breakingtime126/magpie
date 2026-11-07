# Offline fixtures only. No official process, UI, credentials or network access.
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
foreach ($file in @('desktop-live-welcome.ps1','desktop-winapp-inspect.ps1','desktop-winapp-welcome.ps1')) {
    $tokens=$null;$errors=$null
    $null=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file),[ref]$tokens,[ref]$errors)
    if ($errors.Count) { throw 'FAIL: official welcome syntax' }
    . (Join-Path $PSScriptRoot $file)
}
$checks=0
function Pass($Value,[string]$Name) { if (-not $Value) { throw ('FAIL: '+$Name) };$script:checks++ }
function Reject([scriptblock]$Body,[string]$Name) { $failed=$false;try { &$Body } catch { $failed=$true };Pass $failed $Name }
function Node([string]$Name,[string]$Type,[string]$Selector='', [bool]$Enabled=$true) {
    [ordered]@{type=$Type;name=$Name;selector=$Selector;isEnabled=$Enabled;isOffscreen=$false;
        isInvokable=($Type -cin @('RadioButton','CheckBox','Button'));x=0;y=0;width=10;height=10}
}
function Page([string]$Toggle='on',[bool]$Continue=$false,[switch]$Next) {
    $nodes=@((Node 'Which best describes your work?' 'Text'),
        (Node 'Engineering' 'RadioButton' 'rdo-engineering-abcd'),
        (Node 'Suggest personalized tasks' 'CheckBox' 'chk-suggestpersonal-1234'),
        (Node 'Continue' 'Button' 'btn-continue-5678' $Continue))
    $nodes[2].toggleState=$Toggle
    if ($Next) { $nodes=@((Node 'Synthetic next page' 'Text')) }
    return [ordered]@{depth=16;interactive=$false;hideDisabled=$false;hideOffscreen=$false;
        windows=@([ordered]@{hwnd=123;elementCount=$nodes.Count;elements=$nodes;
            windowDpi=96;scale=1;dpiAwareness='per-monitor-aware';coordinateSpace='physical-screen-pixels'})}
}
function Json($Value) { $Value | ConvertTo-Json -Depth 50 -Compress }
function Selected([bool]$Value) {
    [ordered]@{elementId='rdo-engineering-abcd';element=(Node 'Engineering' 'RadioButton' 'rdo-engineering-abcd');
        properties=[ordered]@{IsSelected=$Value.ToString()}}
}
function Action([string]$Operation) {
    $selector=@{toggle='chk-suggestpersonal-1234';select='rdo-engineering-abcd';continue='btn-continue-5678'}[$Operation]
    $pattern=@{toggle='TogglePattern';select='SelectionItemPattern';continue='InvokePattern'}[$Operation]
    $action=$(if($Operation -ceq 'continue'){'invoke'}else{$Operation})
    [ordered]@{elementId=$selector;pattern=$pattern;requestedAction=$action;performedAction=$action;hwnd=123}
}
$page=ConvertTo-WinAppWelcomeSnapshot (Json (Page)) 123
Pass ($page.Decision -ceq 'welcome') 'schema-derived welcome accepted without treating disabled Continue as actionable'
$checkbox=Get-WinAppWelcomeTarget $page 'Suggest personalized tasks' 'CheckBox'
Pass ($checkbox.ToggleState -ceq 'on') 'toggle is a required explicit lowercase state'
Reject { Get-WinAppWelcomeTarget $page 'Continue' 'Button' } 'disabled Continue rejected before action'
$bad=Page;$bad.windows[0].elements[1].selector='rdo-other-abcd'
Reject { Get-WinAppWelcomeTarget (ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123) 'Engineering' 'RadioButton' } 'selector must agree with exact target type and name identity'
$bad=Page;$bad.windows[0].elements[1].selector='arbitrary-promoted-id';$bad.windows[0].elements[1].automationId='arbitrary-promoted-id'
Reject { Get-WinAppWelcomeTarget (ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123) 'Engineering' 'RadioButton' } 'plain AutomationId and text queries never become action selectors'
$bad=Page;$bad.windows[0].elements[0].hasMoreChildren=$true
Reject { ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123 } 'truncated page cannot authorize actions'
$bad=Page;$bad.windows[0].hwnd=124
Reject { ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123 } 'foreign envelope rejected'
$bad=Page;$bad.windows[0].elements[0].name='Sign in to ChatGPT'
Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'protected_screen') 'protected page stops'
$bad=Page;$bad.windows[0].elements[0].name='Permission needed';$bad.windows[0].elements[0].isOffscreen=$true
Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'protected_screen') 'protected text scanned even when offscreen'
$bad=Page;$bad.windows[0].elements+=@(Node 'PRIVATE_CANARY' 'Custom');$bad.windows[0].elements[-1].isInvokable=$true;$bad.windows[0].elementCount++
Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'unreviewed_text') 'unknown invokable custom controls are not treated as harmless prose'
foreach ($type in @('Custom','Pane','Text')) {
    $bad=Page;$bad.windows[0].elements+=@(Node 'PRIVATE_CANARY' $type);$bad.windows[0].elementCount++
    Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'welcome') 'noninteractive auxiliary labels remain harmless'
    $bad.windows[0].elements[-1].isInvokable=$true
    Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'unreviewed_text') 'direct invokable support is interactive regardless of type'
}
$bad=Page;$bad.windows[0].elements[1].isInvokable=$false
Reject { Get-WinAppWelcomeTarget (ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123) 'Engineering' 'RadioButton' } 'missing action support cannot authorize a command'
$radio=Get-WinAppWelcomeTarget $page 'Engineering' 'RadioButton'
Pass (Read-WinAppWelcomeSelected (Json (Selected $true)) $radio) 'exact True property accepted'
foreach ($value in @($null,'true','Off','unknown',$true,$false,@('True'),[ordered]@{value='True'})) {
    $bad=Selected $true;$bad.properties.IsSelected=$value
    Reject { Read-WinAppWelcomeSelected (Json $bad) $radio } 'unknown selection state never implies selected'
}
foreach ($field in @('type','name','selector')) {
    $bad=Selected $true;$bad.element[$field]=@($bad.element[$field])
    Reject { Read-WinAppWelcomeSelected (Json $bad) $radio } 'element identity fields cannot be array-coerced'
}
$good=Action 'toggle';Assert-WinAppWelcomeAction (Json $good) 123 $checkbox 'toggle'
foreach ($key in @('hwnd','pattern','requestedAction','performedAction','elementId')) {
    $bad=Action 'toggle';$bad[$key]=$(if($key -ceq 'hwnd'){124}else{'PRIVATE_CANARY'})
    Reject { Assert-WinAppWelcomeAction (Json $bad) 123 $checkbox 'toggle' } 'only exact explicit single-action receipt accepted'
}

# Construction only: inspect and each explicit pattern command retain the same
# HWND, clean child environment and new private coordination/cache paths.
$temporary=Join-Path ([IO.Path]::GetTempPath()) ('winapp-welcome-tests-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temporary)
$oldSystemRoot=$env:SystemRoot
if (-not $env:SystemRoot) { $env:SystemRoot=$temporary }
try {
    $tool=[pscustomobject]@{Executable=(Get-Process -Id $PID).Path;Directory=$temporary}
    foreach ($op in @('read','selected','toggle','select','continue')) {
        $selector=@{read='';selected='rdo-engineering-abcd';toggle='chk-suggestpersonal-1234';select='rdo-engineering-abcd';continue='btn-continue-5678'}[$op]
        $start=New-WinAppWelcomeStart $tool 123 $op $selector
        $expected=switch($op) {
            read {'ui inspect -w 123 --depth 16 --json'}
            selected {'ui get-property rdo-engineering-abcd --property IsSelected -w 123 --json'}
            toggle {'ui invoke chk-suggestpersonal-1234 --action toggle -w 123 --json'}
            select {'ui invoke rdo-engineering-abcd --action select -w 123 --json'}
            continue {'ui invoke btn-continue-5678 --action invoke -w 123 --json'}
        }
        Pass (($start.ArgumentList -join ' ') -ceq $expected) 'exact argv never selects automatic patterns, another window or coordinates'
        Pass ($start.Environment['WINAPP_CLI_TELEMETRY_OPTOUT'] -ceq '1' -and
            -not $start.Environment.ContainsKey('WINAPP_UI_WORKFLOW_ID') -and
            -not $start.Environment.ContainsKey('GITHUB_TOKEN')) 'clean official child environment'
        Pass (-not (Test-Path $start.Environment['WINAPP_UI_LOCK_DIRECTORY']) -and
            -not (Test-Path $start.Environment['WINAPP_CLI_CACHE_DIRECTORY'])) 'new nonexistent tool state paths'
    }
    Reject { New-WinAppWelcomeStart $tool 123 'toggle' 'rdo-engineering-abcd' } 'operation cannot use another target type'
    Reject { New-WinAppWelcomeStart $tool 123 'toggle-off' 'chk-suggestpersonal-1234' } 'two-toggle convenience command is never admitted'
} finally { $env:SystemRoot=$oldSystemRoot;Remove-Item -LiteralPath $temporary -Recurse -Force }

$tokens=$null;$errors=$null
$harness=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'),[ref]$tokens,[ref]$errors)
Pass ($errors.Count -eq 0) 'harness syntax'
$official=@($harness.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-WinAppWelcome'},$true))
$legacy=@($harness.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-LiveWelcome'},$true))
Pass ($official.Count -eq 1 -and $legacy.Count -eq 1) 'one official call and one preserved legacy call, not stacked actions'
$branch=$official[0].Parent
while ($null -ne $branch -and $branch -isnot [Management.Automation.Language.IfStatementAst]) { $branch=$branch.Parent }
Pass ($null -ne $branch -and $branch.Clauses[0].Item1.Extent.Text -ceq '$EnableReviewedWelcome4239' -and
    $legacy[0].Extent.StartOffset -gt $branch.ElseClause.Extent.StartOffset -and
    $legacy[0].Extent.EndOffset -lt $branch.ElseClause.Extent.EndOffset) 'official and legacy actions are mutually exclusive'
Pass ($official[0].Extent.Text -ceq 'Invoke-WinAppWelcome $winAppTool $window.Handle $checkInspectionIdentity $welcomeDiagnostic') 'official action receives held-window guard, not free selector input'

# Exercise the real fixed orchestration using a local synthetic command substitute.
# No actual CLI process is created. Shared process/deadline tests remain in the existing suites.
function Assert-WinAppFiles($Tool) { }
function Start-Sleep { param([int]$Milliseconds) }
function Invoke-WinAppWelcomeCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [Diagnostics.Stopwatch]$Deadline,[System.Collections.IDictionary]$Diagnostic,[string]$Operation,[string]$Selector='') {
    & $CheckIdentity
    $script:operations+=,$Operation
    if ($script:mode -ceq 'uncertain' -and $Operation -ceq 'toggle') { throw 'Synthetic uncertain action.' }
    if ($script:mode -ceq 'identity' -and $script:operations.Count -eq 2) { throw 'Synthetic changed identity.' }
    if ($Operation -ceq 'read') {
        $script:reads++
        if ($script:reads -eq 4) { return Json (Page -Next) }
        $toggle=$(if($script:reads -eq 1){'on'}else{'off'})
        if ($script:mode -ceq 'indeterminate') { $toggle='indeterminate' }
        if ($script:mode -ceq 'still-on') { $toggle='on' }
        return Json (Page $toggle ($script:reads -ge 3))
    }
    if ($Operation -ceq 'selected') { $script:selectedReads++;return Json (Selected ($script:selectedReads -gt 1)) }
    return Json (Action $Operation)
}
foreach ($mode in @('normal','uncertain','identity','indeterminate','still-on','final-identity')) {
    $script:mode=$mode;$script:operations=@();$script:reads=0;$script:selectedReads=0
    $diagnostic=[ordered]@{};$result=Invoke-WinAppWelcome $null 123 {
        if ($script:mode -ceq 'final-identity' -and $script:reads -eq 4) { throw 'Synthetic final identity failure.' }
    } $diagnostic
    if ($mode -ceq 'normal') {
        Pass ($result.status -ceq 'observed' -and $result.actions -eq 3 -and $result.nextScreen -ceq 'unreviewed_screen') 'one complete fixed welcome transition'
        Pass (($operations -join ',') -ceq 'read,toggle,read,selected,select,read,selected,continue,read') 'each explicit action once with required state reads'
    } elseif ($mode -ceq 'final-identity') {
        Pass ($result.status -ceq 'stopped' -and $result.actions -eq 3 -and $result.continueInvoked -and
            $result.nextScreen -ceq 'not_observed' -and $diagnostic.partialActionUncertain) 'late identity failure preserves valid stopped schema and action uncertainty'
        Pass (@($operations | Where-Object { $_ -ceq 'continue' }).Count -eq 1) 'late failure never repeats Continue'
    } else {
        Pass ($result.status -ceq 'stopped' -and $operations -cnotcontains 'continue') 'failure prevents Continue without retry'
        Pass (@($operations | Where-Object { $_ -ceq 'toggle' }).Count -le 1) 'toggle never repeated'
        if ($mode -ceq 'indeterminate') { Pass ($result.actions -eq 0) 'Indeterminate never reaches an action' }
        if ($mode -ceq 'uncertain') { Pass $diagnostic.partialActionUncertain 'unknown action remains uncertain' }
    }
    Pass ((Json $result) -notmatch 'PRIVATE_CANARY|rdo-|chk-|btn-') 'raw selector and UI data remain private'
}
Write-Output ('WINAPP_WELCOME_OFFLINE_CHECKS='+$checks)
