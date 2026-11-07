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
function Resolution([string]$Name,[string]$Type,[string]$Id,[string]$Slug,[string]$Toggle='') {
    $element=Node $Name $Type $Slug;$element.automationId=$Id
    if ($Type -ceq 'CheckBox') { $element.toggleState=$Toggle }
    [ordered]@{elementId=$Slug;element=$element;properties=[ordered]@{AutomationId=$Id}}
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
$auxiliary=ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123
Pass ($auxiliary.Decision -ceq 'welcome' -and @($auxiliary.Nodes | Where-Object { $_.Name -ceq 'PRIVATE_CANARY' -and $_.Interactive -and $_.Invokable }).Count -eq 1) 'auxiliary invokable controls remain interactive without authorizing input'
Reject { Get-WinAppWelcomeTarget $auxiliary 'PRIVATE_CANARY' 'Custom' } 'auxiliary invokable control is never an action target'
foreach ($type in @('Custom','Pane','Text')) {
    $bad=Page;$bad.windows[0].elements+=@(Node 'PRIVATE_CANARY' $type);$bad.windows[0].elementCount++
    Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'welcome') 'noninteractive auxiliary labels remain harmless'
    $bad.windows[0].elements[-1].isInvokable=$true
    $auxiliary=ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123
    Pass ($auxiliary.Decision -ceq 'welcome' -and @($auxiliary.Nodes | Where-Object { $_.Name -ceq 'PRIVATE_CANARY' -and $_.Interactive }).Count -eq 1) 'direct invokable auxiliary control stays interactive but untargeted'
}
$aux=Page;$aux.windows[0].elements+=@(Node 'Describe something else' 'Edit');$aux.windows[0].elementCount++
$auxiliary=ConvertTo-WinAppWelcomeSnapshot (Json $aux) 123
Pass ((Get-WelcomeDecision $auxiliary.Nodes) -ceq 'unreviewed_control' -and $auxiliary.Decision -ceq 'welcome') 'unchanged legacy policy rejects the same synthetic auxiliary control that the target-focused policy permits'
$aux.windows[0].elements+=@(Node 'PRIVATE_CANARY' 'Button' 'btn-privatecanary-9999');$aux.windows[0].elementCount++
$auxiliary=ConvertTo-WinAppWelcomeSnapshot (Json $aux) 123
Pass ($auxiliary.Decision -ceq 'welcome') 'unknown auxiliary button also leaves the fixed target contract intact'
Reject { Get-WinAppWelcomeTarget $auxiliary 'PRIVATE_CANARY' 'Button' } 'even a valid auxiliary button slug never becomes an action target'
Reject { Get-WinAppWelcomeTarget $auxiliary 'Describe something else' 'Edit' } 'auxiliary edit is not authorized'
Pass (@($auxiliary.Nodes | Where-Object { $_.Name -ceq 'Describe something else' -and $_.Interactive -and $_.Enabled }).Count -eq 1) 'enabled auxiliary edit was not disguised as noninteractive'
foreach ($variant in @('missing-heading','duplicate-heading','offscreen-heading','missing-target','duplicate-target','wrong-target-type','wrong-target-case','same-name-wrong-type')) {
    $bad=Page
    switch($variant) {
        'missing-heading' { $bad.windows[0].elements[0].name='Different page' }
        'duplicate-heading' { $bad.windows[0].elements+=@(Node 'Which best describes your work?' 'Text') }
        'offscreen-heading' { $bad.windows[0].elements[0].isOffscreen=$true }
        'missing-target' { $bad.windows[0].elements[1].name='Another role' }
        'duplicate-target' { $bad.windows[0].elements+=@(Node 'Engineering' 'RadioButton' 'rdo-engineering-9876') }
        'wrong-target-type' { $bad.windows[0].elements[1].type='Button' }
        'wrong-target-case' { $bad.windows[0].elements[1].name='engineering' }
        'same-name-wrong-type' { $bad.windows[0].elements+=@(Node 'Engineering' 'Button' 'btn-engineering-9876') }
    }
    $bad.windows[0].elementCount=$bad.windows[0].elements.Count
    Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -cne 'welcome') 'scene and exact target ambiguity still stop'
}
$bad=Page;$bad.windows[0].elements+=@(Node 'Engineering' 'Text');$bad.windows[0].elementCount++
Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'welcome') 'noninteractive label duplicate is not a second target'
foreach ($label in @('Sign in to ChatGPT','Permission required','Terms of service','Password','Security warning')) {
    $bad=Page;$bad.windows[0].elements+=@(Node $label 'Text' '' $false);$bad.windows[0].elements[-1].isOffscreen=$true;$bad.windows[0].elementCount++
    Pass ((ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123).Decision -ceq 'protected_screen') 'protected disabled offscreen noninteractive text still blocks'
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

# Pinned upstream typed queries return runtime slugs even when inspect promoted
# a unique AutomationId. These are synthetic schemas, not a diagnosis of 1503697d.
$promoted=Page;$promoted.windows[0].elements[2].automationId='PRIVATE_ID_CANARY';$promoted.windows[0].elements[2].selector='PRIVATE_ID_CANARY'
$promotedPage=ConvertTo-WinAppWelcomeSnapshot (Json $promoted) 123
$resolvedText=Json (Resolution 'Suggest personalized tasks' 'CheckBox' 'PRIVATE_ID_CANARY' 'chk-privateidcanary-1234' 'on')
$bindingDiagnostic=[ordered]@{}
$resolved=Get-WinAppWelcomeTarget $promotedPage 'Suggest personalized tasks' 'CheckBox' { $resolvedText } $bindingDiagnostic
Pass ($resolved.Selector -ceq 'chk-privateidcanary-1234' -and $bindingDiagnostic.bindings[0].selectorKind -ceq 'promoted_id' -and
    $bindingDiagnostic.bindings[0].resolutionAttempted -and $bindingDiagnostic.bindings[0].resolved) 'promoted inspect identity receives only a verified runtime slug'
Pass ((Json $bindingDiagnostic) -notmatch 'PRIVATE_ID_CANARY|chk-|Suggest personalized tasks') 'binding diagnostics contain only fixed categories and booleans'
foreach ($variant in @('name','type','automation-id','property-id','element-id','promoted-selector','disabled','offscreen','not-invokable','toggle-changed','property-type','missing-property','multiple-elements')) {
    $badResolved=Resolution 'Suggest personalized tasks' 'CheckBox' 'PRIVATE_ID_CANARY' 'chk-privateidcanary-1234' 'on'
    switch($variant) {
        'name' { $badResolved.element.name='Another control' }
        'type' { $badResolved.element.type='Button' }
        'automation-id' { $badResolved.element.automationId='other' }
        'property-id' { $badResolved.properties.AutomationId='other' }
        'element-id' { $badResolved.elementId='chk-other-1234' }
        'promoted-selector' { $badResolved.element.selector='PRIVATE_ID_CANARY';$badResolved.elementId='PRIVATE_ID_CANARY' }
        'disabled' { $badResolved.element.isEnabled=$false }
        'offscreen' { $badResolved.element.isOffscreen=$true }
        'not-invokable' { $badResolved.element.isInvokable=$false }
        'toggle-changed' { $badResolved.element.toggleState='off' }
        'property-type' { $badResolved.properties.AutomationId=@('PRIVATE_ID_CANARY') }
        'missing-property' { $badResolved.properties.Remove('AutomationId') }
        'multiple-elements' { $badResolved.element=@($badResolved.element,$badResolved.element) }
    }
    $bindingDiagnostic=[ordered]@{}
    Reject { Get-WinAppWelcomeTarget $promotedPage 'Suggest personalized tasks' 'CheckBox' { Json $badResolved } $bindingDiagnostic } 'typed read mismatch cannot authorize an action'
    Pass ($bindingDiagnostic.bindings[0].resolutionAttempted -and -not $bindingDiagnostic.bindings[0].resolved) 'failed resolution remains a single failed binding'
}
foreach ($variant in @('missing-selector','unsupported-selector','missing-id','disabled','not-invokable')) {
    $bad=Page;$bad.windows[0].elements[2].automationId='PRIVATE_ID_CANARY';$bad.windows[0].elements[2].selector='PRIVATE_ID_CANARY'
    switch($variant) {
        'missing-selector' { $bad.windows[0].elements[2].selector='' }
        'unsupported-selector' { $bad.windows[0].elements[2].selector='different-id' }
        'missing-id' { $bad.windows[0].elements[2].automationId='' }
        'disabled' { $bad.windows[0].elements[2].isEnabled=$false }
        'not-invokable' { $bad.windows[0].elements[2].isInvokable=$false }
    }
    $script:resolutionCalls=0;$bindingDiagnostic=[ordered]@{}
    Reject { Get-WinAppWelcomeTarget (ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123) 'Suggest personalized tasks' 'CheckBox' { $script:resolutionCalls++;$resolvedText } $bindingDiagnostic } 'incomplete identity or disabled target never broadens into a read query'
    Pass ($resolutionCalls -eq 0 -and -not $bindingDiagnostic.bindings[0].resolutionAttempted) 'no resolver retry for ineligible original target'
    Pass ($bindingDiagnostic.bindings[0].reason -cin @('selector_unavailable','target_disabled','target_not_invokable')) 'binding failure reason is fixed and actionable without raw UI identity'
}

# Construction only: inspect and each explicit pattern command retain the same
# HWND, clean child environment and new private coordination/cache paths.
$temporary=Join-Path ([IO.Path]::GetTempPath()) ('winapp-welcome-tests-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temporary)
$oldSystemRoot=$env:SystemRoot
if (-not $env:SystemRoot) { $env:SystemRoot=$temporary }
try {
    $tool=[pscustomobject]@{Executable=(Get-Process -Id $PID).Path;Directory=$temporary}
    foreach ($op in @('read','selected','toggle','select','continue','resolve-engineering','resolve-suggestions','resolve-continue')) {
        $selector=@{read='';selected='rdo-engineering-abcd';toggle='chk-suggestpersonal-1234';select='rdo-engineering-abcd';continue='btn-continue-5678';'resolve-engineering'='';'resolve-suggestions'='';'resolve-continue'=''}[$op]
        $start=New-WinAppWelcomeStart $tool 123 $op $selector
        $expected=switch($op) {
            read {'ui inspect -w 123 --depth 16 --json'}
            selected {'ui get-property rdo-engineering-abcd --property IsSelected -w 123 --json'}
            toggle {'ui invoke chk-suggestpersonal-1234 --action toggle -w 123 --json'}
            select {'ui invoke rdo-engineering-abcd --action select -w 123 --json'}
            continue {'ui invoke btn-continue-5678 --action invoke -w 123 --json'}
            'resolve-engineering' {'ui get-property Engineering --type RadioButton --property AutomationId -w 123 --json'}
            'resolve-suggestions' {'ui get-property Suggest personalized tasks --type CheckBox --property AutomationId -w 123 --json'}
            'resolve-continue' {'ui get-property Continue --type Button --property AutomationId -w 123 --json'}
        }
        Pass (($start.ArgumentList -join ' ') -ceq $expected) 'exact argv never selects automatic patterns, another window or coordinates'
        Pass ($start.Environment['WINAPP_CLI_TELEMETRY_OPTOUT'] -ceq '1' -and
            -not $start.Environment.ContainsKey('WINAPP_UI_WORKFLOW_ID') -and
            -not $start.Environment.ContainsKey('GITHUB_TOKEN')) 'clean official child environment'
        Pass (-not (Test-Path $start.Environment['WINAPP_UI_LOCK_DIRECTORY']) -and
            -not (Test-Path $start.Environment['WINAPP_CLI_CACHE_DIRECTORY'])) 'new nonexistent tool state paths'
    }
    foreach ($spec in @(@('skip','btn-skip-abcd','ui invoke btn-skip-abcd --action invoke -w 123 --json'),
        @('skip','lnk-skip-abcd','ui invoke lnk-skip-abcd --action invoke -w 123 --json'),
        @('resolve-skip-button','','ui get-property Skip --type Button --property AutomationId -w 123 --json'),
        @('resolve-skip-link','','ui get-property Skip --type Hyperlink --property AutomationId -w 123 --json'))) {
        $start=New-WinAppWelcomeStart $tool 123 $spec[0] $spec[1]
        Pass (($start.ArgumentList -join ' ') -ceq $spec[2]) 'Skip uses only fixed typed reads or direct Invoke, never coordinates'
    }
    foreach ($spec in @(@('confirm-skip','btn-gotochatgpt-abcd','ui invoke btn-gotochatgpt-abcd --action invoke -w 123 --json'),
        @('confirm-skip','lnk-gotochatgpt-abcd','ui invoke lnk-gotochatgpt-abcd --action invoke -w 123 --json'),
        @('resolve-confirm-button','','ui get-property Go to ChatGPT --type Button --property AutomationId -w 123 --json'),
        @('resolve-confirm-link','','ui get-property Go to ChatGPT --type Hyperlink --property AutomationId -w 123 --json'))) {
        $start=New-WinAppWelcomeStart $tool 123 $spec[0] $spec[1]
        Pass (($start.ArgumentList -join ' ') -ceq $spec[2]) 'confirmation uses only fixed typed reads or explicit Invoke'
    }
    foreach ($spec in @(@('open-model-menu','btn-55alphamedium-abcd','ui invoke btn-55alphamedium-abcd --action expand -w 123 --json'),
        @('resolve-model-short','','ui get-property 5.5 [alpha] --type Button --property AutomationId -w 123 --json'),
        @('resolve-model-effort','','ui get-property 5.5 [alpha] Medium --type Button --property AutomationId -w 123 --json'))) {
        $start=New-WinAppWelcomeStart $tool 123 $spec[0] $spec[1]
        Pass (($start.ArgumentList -join ' ') -ceq $spec[2]) 'model menu uses fixed typed reads and one explicit Button Expand only'
    }
    $start=New-WinAppWelcomeStart $tool 123 'resolve-model-effort' -FixtureProfile 'synthetic-models-4239'
    Pass (($start.ArgumentList -join ' ') -ceq 'ui get-property Synthetic Alpha [alpha] None --type Button --property AutomationId -w 123 --json') 'observed None opener uses one exact typed query'
    Reject { New-WinAppWelcomeStart $tool 123 'resolve-model-effort' 'Try again' -FixtureProfile 'synthetic-models-4239' } 'None resolver cannot accept a setup target'
    foreach ($spec in @(@('open-model-submenu','mnu-55alpha-abcd','ui invoke mnu-55alpha-abcd --action expand -w 123 --json'),
        @('resolve-submodel-button','','ui get-property 5.5 [alpha] --type Button --property AutomationId -w 123 --json'),
        @('resolve-submodel-menuitem','','ui get-property 5.5 [alpha] --type MenuItem --property AutomationId -w 123 --json'))) {
        $start=New-WinAppWelcomeStart $tool 123 $spec[0] $spec[1]
        Pass (($start.ArgumentList -join ' ') -ceq $spec[2]) 'submenu has only fixed typed alpha reads and explicit Expand'
    }
    foreach ($spec in @(@('focused','ui get-focused -w 123 --json'),
        @('model-tab','ui send-keys tab --via send-input -w 123 --json'),
        @('model-enter','ui send-keys enter --via send-input -w 123 --json'))) {
        $start=New-WinAppWelcomeStart $tool 123 $spec[0]
        Pass (($start.ArgumentList -join ' ') -ceq $spec[1]) 'keyboard uses one named key, original window and UIPI-respecting transport without target focus or system options'
        Reject { New-WinAppWelcomeStart $tool 123 $spec[0] 'PRIVATE_CANARY' } 'keyboard accepts no arbitrary target'
    }
    $start=New-WinAppWelcomeStart $tool 123 'search-alpha'
    Pass (($start.ArgumentList -join ' ') -ceq 'ui search [alpha] --max 4 -w 123 --json') 'one fixed unconstrained native search, not type/root filtering or input'
    Reject { New-WinAppWelcomeStart $tool 123 'search-alpha' 'PRIVATE_CANARY' } 'search never accepts an arbitrary selector'
    Reject { New-WinAppWelcomeStart $tool 123 'open-model-menu' '5.5 [alpha]' } 'model menu cannot invoke a text query'
    Reject { New-WinAppWelcomeStart $tool 123 'resolve-model-effort' 'Try again' } 'model menu resolver cannot target setup'
    Reject { New-WinAppWelcomeStart $tool 123 'confirm-skip' 'Go to ChatGPT' } 'confirmation never invokes text or promoted selectors'
    Reject { New-WinAppWelcomeStart $tool 123 'resolve-confirm-button' 'PRIVATE_CANARY' } 'confirmation resolver never accepts arbitrary query input'
    Reject { New-WinAppWelcomeStart $tool 123 'toggle' 'rdo-engineering-abcd' } 'operation cannot use another target type'
    Reject { New-WinAppWelcomeStart $tool 123 'toggle-off' 'chk-suggestpersonal-1234' } 'two-toggle convenience command is never admitted'
    Reject { New-WinAppWelcomeStart $tool 123 'toggle' 'PRIVATE_ID_CANARY' } 'promoted IDs never become action argv'
    Reject { New-WinAppWelcomeStart $tool 123 'resolve-suggestions' 'PRIVATE_ID_CANARY' } 'readonly resolver never accepts an arbitrary query argument'
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
Pass ($official[0].Extent.Text -ceq 'Invoke-WinAppWelcome $winAppTool $window.Handle $checkInspectionIdentity $welcomeDiagnostic $welcomeDeadline') 'official action receives held-window guard, not free selector input'

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
    if ($Operation -like 'resolve-*') {
        if ($script:mode -ceq 'resolution-failure') { throw 'Synthetic nonzero typed read.' }
        $spec=@{'resolve-engineering'=@('Engineering','RadioButton','engineering','rdo-engineering-abcd');
            'resolve-suggestions'=@('Suggest personalized tasks','CheckBox','suggestpersonal','chk-suggestpersonal-1234');
            'resolve-continue'=@('Continue','Button','continue','btn-continue-5678')}[$Operation]
        return Json (Resolution $spec[0] $spec[1] $spec[2] $spec[3] $(if($script:reads -eq 1){'on'}else{'off'}))
    }
    if ($Operation -ceq 'read') {
        $script:reads++
        if ($script:reads -eq 4) { return Json (Page -Next) }
        $toggle=$(if($script:reads -eq 1){'on'}else{'off'})
        if ($script:mode -ceq 'indeterminate') { $toggle='indeterminate' }
        if ($script:mode -ceq 'still-on') { $toggle='on' }
        $page=Page $toggle ($script:reads -ge 3)
        if ($script:mode -ceq 'auxiliary') {
            $page.windows[0].elements+=@((Node 'Describe something else' 'Edit'),(Node 'PRIVATE_CANARY' 'Button' 'btn-privatecanary-9999'))
            $page.windows[0].elementCount+=2
        }
        if ($script:mode -cin @('promoted','resolution-failure')) {
            for($i=1;$i -le 3;$i++) {
                $id=@('','engineering','suggestpersonal','continue')[$i]
                $page.windows[0].elements[$i].automationId=$id;$page.windows[0].elements[$i].selector=$id
            }
        }
        return Json $page
    }
    if ($Operation -ceq 'selected') { $script:selectedReads++;return Json (Selected ($script:selectedReads -gt 1)) }
    return Json (Action $Operation)
}
foreach ($mode in @('normal','auxiliary','promoted','resolution-failure','uncertain','identity','indeterminate','still-on','final-identity')) {
    $script:mode=$mode;$script:operations=@();$script:reads=0;$script:selectedReads=0
    $diagnostic=[ordered]@{};$result=Invoke-WinAppWelcome $null 123 {
        if ($script:mode -ceq 'final-identity' -and $script:reads -eq 4) { throw 'Synthetic final identity failure.' }
    } $diagnostic
    if ($mode -ceq 'normal') { $confirmedWelcome=$result }
    if ($mode -cin @('normal','auxiliary','promoted')) {
        Pass ($result.status -ceq 'observed' -and $result.actions -eq 3 -and $result.nextScreen -ceq 'unreviewed_screen') 'one complete fixed welcome transition'
        $expected='read,toggle,read,selected,select,read,selected,continue,read'
        if ($mode -ceq 'promoted') { $expected='read,resolve-suggestions,toggle,read,resolve-suggestions,resolve-engineering,selected,select,read,resolve-engineering,resolve-suggestions,selected,resolve-continue,continue,read' }
        Pass (($operations -join ',') -ceq $expected) 'conditional readonly resolution never adds an action or a retry'
    } elseif ($mode -ceq 'final-identity') {
        Pass ($result.status -ceq 'stopped' -and $result.actions -eq 3 -and $result.continueInvoked -and
            $result.nextScreen -ceq 'not_observed' -and $diagnostic.partialActionUncertain) 'late identity failure preserves valid stopped schema and action uncertainty'
        Pass (@($operations | Where-Object { $_ -ceq 'continue' }).Count -eq 1) 'late failure never repeats Continue'
    } else {
        Pass ($result.status -ceq 'stopped' -and $operations -cnotcontains 'continue') 'failure prevents Continue without retry'
        Pass (@($operations | Where-Object { $_ -ceq 'toggle' }).Count -le 1) 'toggle never repeated'
        if ($mode -ceq 'indeterminate') { Pass ($result.actions -eq 0) 'Indeterminate never reaches an action' }
        if ($mode -ceq 'uncertain') { Pass $diagnostic.partialActionUncertain 'unknown action remains uncertain' }
        if ($mode -ceq 'resolution-failure') { Pass ($result.actions -eq 0 -and -not $diagnostic.partialActionUncertain -and ($operations -join ',') -ceq 'read,resolve-suggestions') 'failed initial resolution stops before any action without retry' }
    }
    Pass ((Json $result) -notmatch 'PRIVATE_CANARY|rdo-|chk-|btn-') 'raw selector and UI data remain private'
}
# The second scene is a synthetic expectation from the screenshot, not captured
# UIA evidence. The live run must supply the actual supported Skip type/state.
function SkipPage([string]$Type='Button',[bool]$Enabled=$true) {
    $page=Page -Next
    $prefix=$(if ($Type -ceq 'Hyperlink') { 'lnk' } else { 'btn' })
    $page.windows[0].elements=@((Node 'Which best describes your work?' 'Text'),
        (Node 'Engineering' 'Text'),(Node 'Thinking' 'Text'),(Node 'Skip' $Type ($prefix+'-skip-abcd') $Enabled))
    $page.windows[0].elements[3].isInvokable=$true;$page.windows[0].elementCount=4
    return $page
}
$skipCalls=@($harness.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-WinAppSkip'},$true))
Pass ($skipCalls.Count -eq 1 -and $skipCalls[0].Extent.Text -ceq 'Invoke-WinAppSkip $winAppTool $window.Handle $checkInspectionIdentity $welcomeDeadline $liveWelcome $skipDiagnostic') 'Skip has one call and receives the same original deadline and window guard'
$skipGuard=$skipCalls[0].Parent
while ($null -ne $skipGuard -and $skipGuard -isnot [Management.Automation.Language.IfStatementAst]) { $skipGuard=$skipGuard.Parent }
Pass ($skipGuard.Clauses[0].Item1.Extent.Text.Contains('$liveAfterCaptured') -and
    $skipGuard.Clauses[0].Item1.Extent.Text.Contains('-not $welcomeDiagnostic.partialActionUncertain')) 'Skip cannot follow a failed capture or uncertain welcome'
function Invoke-WinAppWelcomeCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [Diagnostics.Stopwatch]$Deadline,[System.Collections.IDictionary]$Diagnostic,[string]$Operation,[string]$Selector='') {
    & $CheckIdentity;$script:skipOperations+=,$Operation
    if ($Operation -ceq 'skip') {
        $script:skipActions++
        if ($script:skipMode -ceq 'uncertain') { throw 'Synthetic uncertain Skip.' }
        return Json ([ordered]@{elementId=$Selector;pattern='InvokePattern';requestedAction='invoke';performedAction='invoke';hwnd=123})
    }
    if ($Operation -cne 'read') { throw 'Unexpected extra Skip operation.' }
    $script:skipReads++
    if ($script:skipActions -gt 0) {
        $after=Page -Next
        if ($script:skipMode -ceq 'next-protected') { $after.windows[0].elements[0].name='Sign in to ChatGPT' }
        return Json $after
    }
    $type=$(if ($script:skipMode -ceq 'link') { 'Hyperlink' } else { 'Button' })
    $page=SkipPage $type ($script:skipReads -gt 1 -and $script:skipMode -cne 'waiting')
    if ($script:skipMode -ceq 'ambiguous') { $page.windows[0].elements+=@(Node 'Skip' 'Hyperlink' 'lnk-skip-ffff');$page.windows[0].elementCount++ }
    if ($script:skipMode -ceq 'protected') {
        $page.windows[0].elements+=@(Node 'Password' 'Text' '' $false);$page.windows[0].elements[-1].isOffscreen=$true;$page.windows[0].elementCount++
    }
    return Json $page
}
foreach ($skipMode in @('normal','link','waiting','ambiguous','protected','uncertain','next-protected')) {
    $script:skipMode=$skipMode;$script:skipReads=0;$script:skipActions=0;$script:skipOperations=@()
    $diagnostic=[ordered]@{};$watch=[Diagnostics.Stopwatch]::StartNew()
    $skip=Invoke-WinAppSkip $null 123 {} $watch $confirmedWelcome $diagnostic
    Pass ($skipActions -le 1) 'Skip is never retried'
    if ($skipMode -ceq 'normal') { $confirmedSkip=$skip }
    Pass ((Json $skip) -notmatch 'btn-|lnk-|Password|Which best describes') 'Skip publishes only fixed metadata, not raw tree text'
    if ($skipMode -cin @('normal','link','next-protected')) {
        Pass ($skip.status -ceq 'observed' -and $skip.actions -eq 1 -and $skip.readinessReads -eq 2 -and
            ($skipOperations -join ',') -ceq 'read,read,skip,read') 'bounded readiness then one Skip and one post-read'
    } elseif ($skipMode -ceq 'uncertain') {
        Pass ($skip.status -ceq 'stopped' -and $skip.actions -eq 1 -and $diagnostic.partialActionUncertain -and
            ($skipOperations -join ',') -ceq 'read,read,skip') 'uncertain Skip has no post-action retry'
    } else {
        Pass ($skip.status -ceq 'stopped' -and $skip.actions -eq 0 -and -not $diagnostic.partialActionUncertain) 'unready ambiguous or protected scene never invokes Skip'
        if ($skipMode -ceq 'waiting') { Pass ($skip.readinessReads -eq 4) 'readiness has a fixed four-read maximum' }
    }
    $prior=[pscustomobject]@{status='observed';stage='first-welcome';reason='fixed';exitCode=0}
    $outcome=Get-WinAppSkipOutcome $prior $skip $true $diagnostic.partialActionUncertain
    Pass (($outcome.exitCode -eq 0) -eq ($skipMode -cin @('normal','link'))) 'protected or uncertain second stage cannot become a successful overall outcome'
}
$script:skipOperations=@();$diagnostic=[ordered]@{}
$stopped=Invoke-WinAppSkip $null 123 {} ([Diagnostics.Stopwatch]::StartNew()) $result $diagnostic
Pass ($stopped.actions -eq 0 -and $skipOperations.Count -eq 0) 'stopped prior welcome never starts a Skip read or action'
$prior=[pscustomobject]@{status='blocked';stage='earlier-failure';reason='fixed';exitCode=1}
Pass ((Get-WinAppSkipOutcome $prior $null $false $true).stage -ceq 'earlier-failure') 'earlier Desktop failure remains primary'
# Screenshot-derived fixed confirmation labels, not captured UIA evidence.
# No raw tree, selector or arbitrary UI text is published by the stage.
function ConfirmationPage([string]$Type='Button') {
    $page=Page -Next
    $prefix=$(if ($Type -ceq 'Hyperlink') { 'lnk' } else { 'btn' })
    $page.windows[0].elements=@((Node 'Skip setup?' 'Text'),
        (Node 'Keep setting up' 'Button' 'btn-keepsettingup-abcd'),
        (Node 'Go to ChatGPT' $Type ($prefix+'-gotochatgpt-abcd')))
    $page.windows[0].elements[2].isInvokable=$true;$page.windows[0].elementCount=3
    return $page
}
$confirmationCalls=@($harness.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-WinAppSkipConfirmation'},$true))
Pass ($confirmationCalls.Count -eq 1 -and $confirmationCalls[0].Extent.Text -ceq 'Invoke-WinAppSkipConfirmation $winAppTool $window.Handle $checkInspectionIdentity $welcomeDeadline $liveSkip $confirmationDiagnostic') 'one confirmation call shares original window guard and deadline'
$confirmationGuard=$confirmationCalls[0].Parent
while ($null -ne $confirmationGuard -and $confirmationGuard -isnot [Management.Automation.Language.IfStatementAst]) { $confirmationGuard=$confirmationGuard.Parent }
Pass ($confirmationGuard.Clauses[0].Item1.Extent.Text.Contains('$skipAfterCaptured') -and
    $confirmationGuard.Clauses[0].Item1.Extent.Text.Contains('-not $liveFixtureReport.partialActionUncertain')) 'confirmation follows only captured certain Skip'
foreach ($variant in @('missing-title','missing-cancel','duplicate-cancel','wrong-cancel-type','missing-target','duplicate-target','wrong-target-type','offscreen','disabled','no-pattern','protected')) {
    $page=ConfirmationPage
    switch ($variant) {
        'missing-title' { $page.windows[0].elements[0].name='Other scene' }
        'missing-cancel' { $page.windows[0].elements[1].name='Other control' }
        'duplicate-cancel' { $page.windows[0].elements+=@(Node 'Keep setting up' 'Hyperlink') }
        'wrong-cancel-type' { $page.windows[0].elements[1].type='Edit' }
        'missing-target' { $page.windows[0].elements[2].name='Other control' }
        'duplicate-target' { $page.windows[0].elements+=@(Node 'Go to ChatGPT' 'Hyperlink' 'lnk-gotochatgpt-abcd') }
        'wrong-target-type' { $page.windows[0].elements[2].type='Custom' }
        'offscreen' { $page.windows[0].elements[2].isOffscreen=$true }
        'disabled' { $page.windows[0].elements[2].isEnabled=$false }
        'no-pattern' { $page.windows[0].elements[2].isInvokable=$false }
        'protected' { $page.windows[0].elements+=@(Node 'Permission required' 'Text' '' $false);$page.windows[0].elements[-1].isOffscreen=$true }
    }
    $page.windows[0].elementCount=$page.windows[0].elements.Count
    Pass ((Get-WinAppSkipConfirmationObservation (ConvertTo-WinAppWelcomeSnapshot (Json $page) 123)).decision -cne 'ready') 'scene ambiguity unavailable target or protected text stops confirmation'
}
$confirmationPage=ConvertTo-WinAppWelcomeSnapshot (Json (ConfirmationPage)) 123
Reject { Get-WinAppWelcomeTarget $confirmationPage 'Go to ChatGPT' 'Button' } 'confirmation target requires admitted confirmation scene'
$confirmationPage.Decision='confirm_skip_ready'
Reject { Get-WinAppWelcomeTarget $confirmationPage 'Keep setting up' 'Button' } 'cancel control is scene evidence, never an action target'
function Invoke-WinAppWelcomeCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [Diagnostics.Stopwatch]$Deadline,[System.Collections.IDictionary]$Diagnostic,[string]$Operation,[string]$Selector='') {
    Pass ([object]::ReferenceEquals($Deadline,$script:confirmationWatch)) 'all confirmation commands share the original budget'
    & $CheckIdentity;$script:confirmationOperations+=,$Operation
    if ($Operation -ceq 'confirm-skip') {
        $script:confirmationActions++
        if ($script:confirmationMode -ceq 'uncertain') { throw 'Synthetic uncertain confirmation.' }
        return Json ([ordered]@{elementId=$Selector;pattern='InvokePattern';requestedAction='invoke';performedAction='invoke';hwnd=$(if($script:confirmationMode -ceq 'wrong-receipt'){124}else{123})})
    }
    if ($Operation -ceq 'resolve-confirm-button') {
        return Json (Resolution 'Go to ChatGPT' 'Button' 'confirmgo' 'btn-confirmgo-abcd')
    }
    if ($Operation -cne 'read') { throw 'Unexpected confirmation operation.' }
    if ($script:confirmationActions -gt 0) {
        $script:confirmationPostRead=$true
        $after=Page -Next
        if ($script:confirmationMode -ceq 'next-protected') { $after.windows[0].elements[0].name='Sign in to ChatGPT' }
        if ($script:confirmationMode -ceq 'unchanged') { $after=ConfirmationPage }
        return Json $after
    }
    $page=ConfirmationPage $(if ($script:confirmationMode -ceq 'link') { 'Hyperlink' } else { 'Button' })
    if ($script:confirmationMode -ceq 'duplicate-title') {
        $page.windows[0].elements+=@((Node 'Skip setup?' 'Group'),(Node 'Skip setup?' 'Pane'));$page.windows[0].elementCount+=2
    }
    if ($script:confirmationMode -ceq 'promoted') { $page.windows[0].elements[2].automationId='confirmgo';$page.windows[0].elements[2].selector='confirmgo' }
    if ($script:confirmationMode -ceq 'disabled') { $page.windows[0].elements[2].isEnabled=$false }
    return Json $page
}
foreach ($confirmationMode in @('normal','link','promoted','duplicate-title','disabled','uncertain','wrong-receipt','next-protected','unchanged','final-identity')) {
    $script:confirmationMode=$confirmationMode;$script:confirmationActions=0;$script:confirmationOperations=@();$script:confirmationPostRead=$false
    $script:confirmationWatch=[Diagnostics.Stopwatch]::StartNew();$diagnostic=[ordered]@{}
    $identity={if ($script:confirmationMode -ceq 'final-identity' -and $script:confirmationPostRead) { throw 'Synthetic changed identity.' }}
    $confirmation=Invoke-WinAppSkipConfirmation $null 123 $identity $confirmationWatch $confirmedSkip $diagnostic
    Pass ($confirmationActions -le 1) 'confirmation is never retried'
    if ($confirmationMode -ceq 'normal') { $confirmedConfirmation=$confirmation }
    if ($confirmationMode -ceq 'duplicate-title') {
        Pass ($confirmation.lastObservation.titleCount -eq 3 -and $confirmation.lastObservation.targetInteractiveCount -eq 1 -and
            $confirmation.status -ceq 'observed' -and $confirmationActions -eq 1) 'repeated non-action title names do not make the unique confirmation action ambiguous'
    }
    Pass ((Json $confirmation) -notmatch 'btn-|lnk-|Sign in|Skip setup|Go to ChatGPT') 'confirmation output contains fixed metadata only'
    if ($confirmationMode -cin @('normal','link','promoted','duplicate-title','next-protected','unchanged')) {
        $expected=$(if($confirmationMode -ceq 'promoted'){'read,resolve-confirm-button,confirm-skip,read'}else{'read,confirm-skip,read'})
        Pass ($confirmation.status -ceq 'observed' -and $confirmation.actions -eq 1 -and $confirmation.confirmationInvoked -and
            -not $diagnostic.partialActionUncertain -and ($confirmationOperations -join ',') -ceq $expected) 'one admitted confirmation then only one next-page read'
    } elseif ($confirmationMode -ceq 'disabled') {
        Pass ($confirmation.actions -eq 0 -and -not $diagnostic.partialActionUncertain -and ($confirmationOperations -join ',') -ceq 'read') 'disabled confirmation stops without polling or action'
    } else {
        Pass ($confirmation.status -ceq 'stopped' -and $confirmation.actions -eq 1 -and
            $confirmation.nextScreen -ceq 'not_observed' -and $diagnostic.partialActionUncertain) 'failed confirmation retains uncertainty without committing next-screen success'
        if ($confirmationMode -cne 'final-identity') { Pass (($confirmationOperations -join ',') -ceq 'read,confirm-skip') 'uncertain action or invalid receipt prevents later reads and retries' }
    }
    $prior=[pscustomobject]@{status='observed';stage='skip';reason='fixed';exitCode=0}
    $outcome=Get-WinAppSkipConfirmationOutcome $prior $confirmation $true $diagnostic.partialActionUncertain
    Pass (($outcome.exitCode -eq 0) -eq ($confirmationMode -cin @('normal','link','promoted','duplicate-title'))) 'only confirmed transition and capture can finish; next page remains unreviewed'
    Pass ((Get-WinAppSkipConfirmationOutcome $prior $confirmation $false $false).exitCode -eq 1) 'missing final pixels block completion'
}
$script:confirmationOperations=@();$diagnostic=[ordered]@{}
$stopped=Invoke-WinAppSkipConfirmation $null 123 {} $confirmationWatch $null $diagnostic
Pass ($stopped.actions -eq 0 -and $confirmationOperations.Count -eq 0) 'missing successful prior Skip never starts confirmation'
$prior=[pscustomobject]@{status='blocked';stage='earlier-failure';reason='fixed';exitCode=1}
Pass ((Get-WinAppSkipConfirmationOutcome $prior $null $false $true).stage -ceq 'earlier-failure') 'confirmation never hides an earlier failure'
# Synthetic UIA fields around exact public fixture names and screenshot labels.
function ModelPage([switch]$Short,[switch]$Opened,[switch]$MissingBeta) {
    $page=Page -Next
    $name=$(if($Short){'5.5 [alpha]'}else{'5.5 [alpha] Medium'})
    $slug=$(if($Short){'btn-55alpha-abcd'}else{'btn-55alphamedium-abcd'})
    $page.windows[0].elements=@((Node 'What should we build?' 'Text'),(Node $name 'Button' $slug),
        (Node "Couldn't check Windows setup" 'Text'),(Node 'Try again' 'Button' 'btn-tryagain-abcd'))
    $page.windows[0].elements[1].expandState=$(if($Opened){'expanded'}else{'collapsed'})
    if ($Opened) {
        $page.windows[0].elements+=@(Node 'GPT-5.5 [alpha]' 'Text')
        if (-not $MissingBeta) { $page.windows[0].elements+=@(Node '5.5 [beta]' 'MenuItem') }
    }
    $page.windows[0].elementCount=$page.windows[0].elements.Count
    return $page
}
function SubmenuPage([string]$Variant='normal') {
    $page=Page -Next
    $name=$(if($Variant -ceq 'combo'){"Medium`n5.5 [alpha]"}elseif($Variant -ceq 'othername'){'Choose 5.5 [alpha]'}else{'5.5 [alpha]'})
    $type=$(if($Variant -ceq 'menuitem'){'MenuItem'}else{'Button'})
    $slug=$(if($Variant -ceq 'menuitem'){'mnu-55alpha-bbbb'}elseif($Variant -ceq 'combo'){'btn-medium55alpha-bbbb'}else{'btn-55alpha-bbbb'})
    $node=Node $name $type $slug;$node.isInvokable=$true;$node.expandState='collapsed'
    if($Variant -ceq 'promoted'){$node.automationId='submodel';$node.selector='submodel'}
    if($Variant -ceq 'offscreen'){$node.isOffscreen=$true}
    if($Variant -ceq 'disabled'){$node.isEnabled=$false}
    if($Variant -ceq 'expanded'){$node.expandState='expanded'}
    if($Variant -ceq 'wrong-type'){$node.type='Custom'}
    $page.windows[0].elements=@((Node 'Medium' 'Text'),$node);$page.windows[0].elementCount=2
    if($Variant -ceq 'duplicate'){$page.windows[0].elements+=@(Node 'GPT-5.5 [alpha]' 'MenuItem' 'mnu-gpt55alpha-cccc');$page.windows[0].elementCount++}
    return $page
}
# Pinned UiFocusedResult and UiSendKeysResult; these are synthetic safety fixtures.
function Focused([string]$Name='Ordinary control',[string]$Type='Button',[string]$Id='btn-ordinary-abcd') {
    [ordered]@{hasFocus=$true;element=(Node $Name $Type $Id)}
}
function KeyReceipt([string]$Key) { [ordered]@{keys=$Key;via='send-input';actionCount=1;hwnd=123;warnings=@()} }
foreach ($name in @('5.5 [alpha]','GPT-5.5 [alpha]','5.5 [beta]','GPT-5.5 [beta]')) {
    $focus=Read-WinAppModelFocus (Json (Focused $name))
    Pass ($focus.Observation.activationAllowed -and $focus.Observation.kind -ceq 'model') 'exact catalog model names can activate only from actual focused controls'
}
foreach ($name in @('Select model','Choose model')) {
    $value=Focused $name;$value.element.isInvokable=$false
    Pass ((Read-WinAppModelFocus (Json $value)).Observation.activationAllowed) 'explicit model-menu focus supports one keyboard opening without pretending isInvokable means InvokePattern'
    $value.element.type='Edit'
    Pass (-not (Read-WinAppModelFocus (Json $value)).Observation.activationAllowed) 'menu wording on an edit is not an activation target'
}
foreach ($variant in @('hidden','disabled','text','slider','wrong-name','setup','protected','none')) {
    $value=Focused '5.5 [alpha]'
    switch($variant) {
        'hidden' { $value.element.isOffscreen=$true }
        'disabled' { $value.element.isEnabled=$false }
        'text' { $value.element.type='Text' }
        'slider' { $value.element.type='Slider' }
        'wrong-name' { $value.element.name='5.5 [alpha] Standard, 2 of 4.' }
        'setup' { $value.element.name='Continue' }
        'protected' { $value.element.name='Permission required';$value.element.isOffscreen=$true }
        'none' { $value=[ordered]@{hasFocus=$false} }
    }
    $focus=Read-WinAppModelFocus (Json $value)
    Pass (-not $focus.Observation.activationAllowed) 'hidden labels, wrong roles, unknown names, setup, permissions and no focus never authorize Enter'
    if ($variant -cin @('setup','protected')) { Pass ($focus.Observation.kind -ceq 'protected') 'protected focus stops even navigation' }
    Pass ((Json $focus.Observation) -notmatch 'btn-|Standard|Continue|Permission') 'public focus summary contains no raw identity or unrelated names'
}
foreach ($variant in @('has-focus-type','name-type','boolean-type','element-missing','element-unexpected')) {
    $value=Focused
    switch($variant) {
        'has-focus-type' { $value.hasFocus='true' }
        'name-type' { $value.element.name=@('5.5 [alpha]') }
        'boolean-type' { $value.element.isEnabled='true' }
        'element-missing' { $value.Remove('element') }
        'element-unexpected' { $value.hasFocus=$false }
    }
    Reject { Read-WinAppModelFocus (Json $value) } 'malformed focus cannot authorize keyboard input'
}
foreach ($key in @('tab','enter')) { Assert-WinAppModelKeyReceipt (Json (KeyReceipt $key)) 123 $key;Pass $true 'exact single-key receipt accepted' }
foreach ($variant in @('transport','hwnd','keys','count','warning','target')) {
    $value=KeyReceipt 'tab'
    switch($variant) {
        'transport' { $value.via='post-message' }
        'hwnd' { $value.hwnd=124 }
        'keys' { $value.keys='tab enter' }
        'count' { $value.actionCount=2 }
        'warning' { $value.warnings=@('PRIVATE_WARNING') }
        'target' { $value.target='PRIVATE_TARGET' }
    }
    Reject { Assert-WinAppModelKeyReceipt (Json $value) 123 'tab' } 'wrong transport, window, multi-key, warning or implicit focus receipt stops'
}
# Schema-derived search fixtures, not recorded output from a live search.
# The Text name/offscreen state below reproduces the a312 inspect evidence only.
function SearchResult([string]$Variant='offscreen') {
    $node=Node '5.5 [alpha] Standard, 2 of 4.' 'Text';$node.isOffscreen=$true
    $node.invokableAncestor=Node 'PRIVATE_ANCESTOR' 'Button' 'btn-private-abcd'
    $node.value='PRIVATE_VALUE';$node.automationId='PRIVATE_ID';$node.selector='txt-private-abcd'
    if ($Variant -ceq 'visible') { $node=Node '5.5 [alpha]' 'Button' 'btn-55alpha-abcd';$node.expandState='collapsed' }
    if ($Variant -ceq 'protected') { $node.name='Permission required [alpha]' }
    return [ordered]@{matchCount=1;hasMore=$false;matches=@($node)}
}
$evidence=Read-WinAppModelSearchEvidence (Json (SearchResult))
Pass ($evidence.reason -ceq 'offscreen_only' -and $evidence.nameEvidence.Count -eq 1 -and $evidence.nameEvidence[0].isOffscreen -and
    -not $evidence.nameEvidence[0].directInvokable -and (Json $evidence) -notmatch 'PRIVATE_|txt-|btn-|selector|Ancestor') 'search preserves only bounded related evidence, never IDs values or ancestor hints'
$visible=Read-WinAppModelSearchEvidence (Json (SearchResult 'visible'))
Pass ($visible.reason -ceq 'visible_related_names' -and $visible.nameEvidence[0].expandState -ceq 'collapsed') 'visible directly expandable search result is evidence only'
$blocked=Read-WinAppModelSearchEvidence (Json (SearchResult 'protected'))
Pass ($blocked.reason -ceq 'protected_screen' -and $blocked.nameEvidence.Count -eq 0) 'protected offscreen search match suppresses evidence'
foreach ($name in @('C:\private\[alpha]','/private/[alpha]',('x'*121+'[alpha]'),("Private`0[alpha]"))) {
    $bad=SearchResult;$bad.matches[0].name=$name
    $evidence=Read-WinAppModelSearchEvidence (Json $bad)
    Pass ($evidence.nameEvidenceLimited -and $evidence.nameEvidence.Count -eq 0) 'paths oversized and control-data names are not published'
}
$bounded=SearchResult;$bounded.matches=@(1..4 | ForEach-Object { Node ('Related '+$_+' [alpha]') 'Text' });$bounded.matchCount=4;$bounded.hasMore=$true
$evidence=Read-WinAppModelSearchEvidence (Json $bounded) 3
Pass ($evidence.nameEvidence.Count -eq 3 -and $evidence.nameEvidenceLimited -and $evidence.hasMore) 'search shares four-name allowance with existing inspect evidence'
Pass ((Read-WinAppModelSearchEvidence (Json $bounded) 0).nameEvidence.Count -eq 0) 'exhausted name allowance still retains fixed counts only'
$unrelated=SearchResult;$unrelated.matches[0].name='PRIVATE_UNRELATED'
Pass ((Read-WinAppModelSearchEvidence (Json $unrelated)).nameEvidence.Count -eq 0) 'an AutomationId-only match cannot publish unrelated Name'
foreach ($variant in @('count','too-many','has-more-type','has-more-count','matches-type','boolean','name-type','expand-type','unknown-field','children')) {
    $bad=SearchResult
    switch($variant) {
        'count' { $bad.matchCount=0 }
        'too-many' { $bad.matchCount=5 }
        'has-more-type' { $bad.hasMore='false' }
        'has-more-count' { $bad.hasMore=$true }
        'matches-type' { $bad.matches=$bad.matches[0] }
        'boolean' { $bad.matches[0].isInvokable='true' }
        'name-type' { $bad.matches[0].name=@('5.5 [alpha]') }
        'expand-type' { $bad.matches[0].expandState=$true }
        'unknown-field' { $bad.foreign='PRIVATE_CANARY' }
        'children' { $bad.matches[0].children=@(Node 'PRIVATE_CHILD' 'Button') }
    }
    Reject { Read-WinAppModelSearchEvidence (Json $bad) } 'malformed search contract cannot become trusted evidence'
}
Reject { Read-WinAppModelSearchEvidence '{"matchCount":0,"matchCount":1,"hasMore":false,"matches":[]}' } 'duplicate search fields rejected'
foreach ($variant in @('normal','combo','menuitem','offscreen','othername','disabled','expanded','wrong-type','duplicate')) {
    $page=ConvertTo-WinAppWelcomeSnapshot (Json (SubmenuPage $variant)) 123
    $observation=Get-WinAppModelSubmenuObservation $page
    Pass (($observation.decision -ceq 'ready') -eq ($variant -cin @('normal','combo','menuitem'))) 'only observed known visible collapsed submenu types admit an action'
    Pass ($observation.nameEvidence.Count -eq $(if($variant -ceq 'duplicate'){2}else{1}) -and $observation.nameEvidence[0].isOffscreen -eq ($variant -ceq 'offscreen')) 'same-tree evidence retains offscreen identity without authorizing it'
    if ($variant -ceq 'combo') { Pass ($observation.nameEvidence[0].nameMatch -ceq 'normalized') 'only whitespace is normalized for the screenshot-backed combination' }
}
$bounded=SubmenuPage;$bounded.windows[0].elements+=@((Node 'C:\private\[alpha]' 'Text'),(Node ('x'*121+'[alpha]') 'Text'))
1..4 | ForEach-Object { $bounded.windows[0].elements+=@(Node ('Related '+$_+' [alpha]') 'Text') }
$bounded.windows[0].elementCount=$bounded.windows[0].elements.Count
$observation=Get-WinAppModelSubmenuObservation (ConvertTo-WinAppWelcomeSnapshot (Json $bounded) 123)
Pass ($observation.nameEvidenceLimited -and $observation.nameEvidence.Count -le 4 -and (Json $observation) -notmatch 'private|x{121}') 'related evidence is bounded and omits paths and oversized names'
$protected=SubmenuPage;$protected.windows[0].elements+=@(Node 'Permission required' 'Text');$protected.windows[0].elementCount++
$observation=Get-WinAppModelSubmenuObservation (ConvertTo-WinAppWelcomeSnapshot (Json $protected) 123)
Pass ($observation.decision -ceq 'protected_screen' -and $observation.nameEvidence.Count -eq 0) 'protected scene does not expose related names or authorize submenu'
$menuCalls=@($harness.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-WinAppModelMenu'},$true))
Pass ($menuCalls.Count -eq 1 -and $menuCalls[0].Extent.Text -ceq 'Invoke-WinAppModelMenu $winAppTool $window.Handle $checkInspectionIdentity $welcomeDeadline $liveSkipConfirmation $modelMenuDiagnostic -FixtureProfile $DesktopFixtureProfile') 'model menu shares original window guard and deadline once'
$menuGuard=$menuCalls[0].Parent
while ($null -ne $menuGuard -and $menuGuard -isnot [Management.Automation.Language.IfStatementAst]) { $menuGuard=$menuGuard.Parent }
Pass ($menuGuard.Clauses[0].Item1.Extent.Text.Contains('$confirmationAfterCaptured') -and
    $menuGuard.Clauses[0].Item1.Extent.Text.Contains('-not $liveFixtureReport.partialActionUncertain')) 'model menu follows only captured certain confirmation'
foreach ($variant in @('missing-heading','wrong-name','wrong-type','duplicate-target','already-open','disabled','offscreen','no-pattern','protected')) {
    $page=ModelPage
    switch($variant) {
        'missing-heading' { $page.windows[0].elements[0].name='Other scene' }
        'wrong-name' { $page.windows[0].elements[1].name='5.5 unrelated' }
        'wrong-type' { $page.windows[0].elements[1].type='Hyperlink' }
        'duplicate-target' { $page.windows[0].elements+=@(Node '5.5 [alpha]' 'Button' 'btn-55alpha-ffff') }
        'already-open' { $page.windows[0].elements+=@(Node '5.5 [beta]' 'MenuItem') }
        'disabled' { $page.windows[0].elements[1].isEnabled=$false }
        'offscreen' { $page.windows[0].elements[1].isOffscreen=$true }
        'no-pattern' { $page.windows[0].elements[1].isInvokable=$false }
        'protected' { $page.windows[0].elements+=@(Node 'Permission required' 'Text' '' $false);$page.windows[0].elements[-1].isOffscreen=$true }
    }
    $page.windows[0].elementCount=$page.windows[0].elements.Count
    Pass ((Get-WinAppModelMenuObservation (ConvertTo-WinAppWelcomeSnapshot (Json $page) 123)).decision -cne 'ready') 'unreviewed ambiguous unavailable or protected model entry stops'
}
$menuPage=ConvertTo-WinAppWelcomeSnapshot (Json (ModelPage)) 123
Reject { Get-WinAppWelcomeTarget $menuPage '5.5 [alpha] Medium' 'Button' } 'model entry requires its own admitted scene'
$menuPage.Decision='model_menu_ready'
foreach ($state in @('','expanded','Collapsed','partiallyexpanded','leafnode','unknown')) {
    $bad=ModelPage;$bad.windows[0].elements[1].expandState=$state
    $snapshot=ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123
    Pass ((Get-WinAppModelMenuObservation $snapshot).decision -cne 'ready') 'only positive collapsed evidence admits model expansion'
    $snapshot.Decision='model_menu_ready'
    Reject { Get-WinAppWelcomeTarget $snapshot '5.5 [alpha] Medium' 'Button' } 'target getter independently rejects absent or noncollapsed expansion state'
}
$bad=ModelPage;$bad.windows[0].elements[1].Remove('expandState')
Pass ((Get-WinAppModelMenuObservation (ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123)).decision -ceq 'target_expand_state_unavailable') 'omitted expansion state is unknown, not collapsed'
foreach ($state in @($null,$true,@('collapsed'),[ordered]@{state='collapsed'},'Collapsed')) {
    $bad=ModelPage;$bad.windows[0].elements[1].automationId='modelmenu';$bad.windows[0].elements[1].selector='modelmenu'
    $snapshot=ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123;$snapshot.Decision='model_menu_ready'
    $resolved=Resolution '5.5 [alpha] Medium' 'Button' 'modelmenu' 'btn-modelmenu-abcd';$resolved.element.expandState=$state
    $resolvedText=Json $resolved
    Reject { Get-WinAppWelcomeTarget $snapshot '5.5 [alpha] Medium' 'Button' { $resolvedText } } 'resolved expansion state must be the exact lowercase string, not coerced JSON'
}
foreach ($name in @('Try again','5.5 [beta]','Send','Sign in')) {
    Reject { Get-WinAppWelcomeTarget $menuPage $name 'Button' } 'no setup model-selection send or account target is authorized'
}
function Invoke-WinAppWelcomeCommand($Tool,[long]$WindowHandle,[scriptblock]$CheckIdentity,
    [Diagnostics.Stopwatch]$Deadline,[System.Collections.IDictionary]$Diagnostic,[string]$Operation,[string]$Selector='') {
    Pass ([object]::ReferenceEquals($Deadline,$script:menuWatch)) 'menu commands retain the original total budget'
    & $CheckIdentity;$script:menuOperations+=,$Operation
    if ($Operation -ceq 'focused') {
        $script:focusReads++
        if ($script:menuMode -ceq 'keyboard-none') { return '{"hasFocus":false}' }
        if ($script:menuMode -ceq 'keyboard-bad-focus') { return '{"hasFocus":"true"}' }
        if ($script:menuMode -ceq 'keyboard-setup' -or ($script:menuMode -ceq 'keyboard-after-protected' -and $script:keyEntered)) { return Json (Focused 'Continue') }
        if ($script:menuMode -ceq 'keyboard-menu-entry') { return Json (Focused 'Select model') }
        $model=$script:menuMode -cin @('keyboard-immediate','keyboard-beta','keyboard-enter-failure','keyboard-after-protected','keyboard-final-identity') -or
            ($script:menuMode -ceq 'keyboard-one-tab' -and $script:keyTabs -eq 1)
        if ($model) { return Json (Focused $(if($script:menuMode -ceq 'keyboard-beta'){'5.5 [beta]'}else{'5.5 [alpha]'})) }
        $id=$(if($script:menuMode -ceq 'keyboard-unchanged'){'btn-ordinary-abcd'}else{'btn-ordinary-'+$script:focusReads})
        return Json (Focused 'Ordinary control' 'Button' $id)
    }
    if ($Operation -cin @('model-tab','model-enter')) {
        $script:menuActions++
        if ($Operation -ceq 'model-tab') {
            if ($script:menuMode -ceq 'keyboard-tab-failure') { throw 'Synthetic uncertain Tab.' }
            $script:keyTabs++;$receipt=KeyReceipt 'tab'
            if ($script:menuMode -ceq 'keyboard-bad-receipt') { $receipt.via='post-message' }
        } else {
            if ($script:menuMode -ceq 'keyboard-enter-failure') { throw 'Synthetic uncertain Enter.' }
            $script:keyEntered=$true;$receipt=KeyReceipt 'enter'
        }
        return Json $receipt
    }
    if ($Operation -ceq 'open-model-menu') {
        $script:menuActions++
        if ($script:menuMode -ceq 'uncertain') { throw 'Synthetic uncertain menu action.' }
        $action=$(if($script:menuMode -ceq 'wrong-pattern'){'invoke'}else{'expand'})
        $pattern=$(if($script:menuMode -ceq 'wrong-pattern'){'InvokePattern'}else{'ExpandCollapsePattern'})
        return Json ([ordered]@{elementId=$Selector;pattern=$pattern;requestedAction=$action;performedAction=$action;hwnd=123})
    }
    if ($Operation -ceq 'open-model-submenu') {
        $script:menuActions++
        if ($script:menuMode -ceq 'submenu-uncertain') { throw 'Synthetic uncertain submenu.' }
        return Json ([ordered]@{elementId=$Selector;pattern='ExpandCollapsePattern';requestedAction='expand';performedAction='expand';hwnd=123})
    }
    if ($Operation -ceq 'resolve-submodel-button') {
        $resolved=Resolution '5.5 [alpha]' 'Button' 'submodel' 'btn-submodel-bbbb';$resolved.element.expandState='collapsed'
        return Json $resolved
    }
    if ($Operation -ceq 'resolve-model-effort') {
        $resolved=Resolution '5.5 [alpha] Medium' 'Button' 'modelmenu' 'btn-modelmenu-abcd'
        if ($script:menuMode -cne 'resolution-missing-state') { $resolved.element.expandState=$(if($script:menuMode -ceq 'resolution-expanded'){'expanded'}else{'collapsed'}) }
        return Json $resolved
    }
    if ($Operation -cne 'read') { throw 'No selection send or setup operation allowed.' }
    if ($script:menuActions -gt 0) {
        $script:menuPostRead=$true
        if ($script:menuMode -like 'keyboard-*' -and -not $script:keyEntered) { return Json (SubmenuPage 'offscreen') }
        if ($script:menuActions -eq 1 -and $script:menuMode -like 'submenu-*') {
            return Json (SubmenuPage ($script:menuMode.Substring(8)))
        }
        $page=ModelPage -Opened -MissingBeta:($script:menuMode -ceq 'missing-beta')
        if ($script:menuMode -ceq 'next-protected') { $page.windows[0].elements[0].name='Sign in to ChatGPT' }
        return Json $page
    }
    $page=ModelPage -Short:($script:menuMode -ceq 'short')
    if ($script:menuMode -cin @('promoted','resolution-missing-state','resolution-expanded')) { $page.windows[0].elements[1].automationId='modelmenu';$page.windows[0].elements[1].selector='modelmenu' }
    if ($script:menuMode -ceq 'disabled') { $page.windows[0].elements[1].isEnabled=$false }
    return Json $page
}
foreach ($menuMode in @('normal','short','promoted','disabled','uncertain','wrong-pattern','resolution-missing-state','resolution-expanded','missing-beta','next-protected','final-identity','submenu-normal','submenu-combo','submenu-menuitem','submenu-promoted','submenu-offscreen','submenu-othername','submenu-uncertain','keyboard-immediate','keyboard-beta','keyboard-one-tab','keyboard-menu-entry','keyboard-limit','keyboard-unchanged','keyboard-none','keyboard-setup','keyboard-bad-focus','keyboard-tab-failure','keyboard-bad-receipt','keyboard-enter-failure','keyboard-after-protected','keyboard-final-identity')) {
    $script:menuMode=$menuMode;$script:menuActions=0;$script:menuOperations=@();$script:menuPostRead=$false;$script:focusReads=0;$script:keyTabs=0;$script:keyEntered=$false
    $script:menuWatch=[Diagnostics.Stopwatch]::StartNew();$diagnostic=[ordered]@{}
    $identity={if (($script:menuMode -ceq 'final-identity' -and $script:menuPostRead) -or ($script:menuMode -ceq 'keyboard-final-identity' -and $script:keyEntered -and $script:focusReads -ge 2)) { throw 'Synthetic identity change.' }}
    $menu=Invoke-WinAppModelMenu $null 123 $identity $menuWatch $confirmedConfirmation $diagnostic
    Pass ($menuActions -le 6 -and @($menuOperations | Where-Object { $_ -ceq 'open-model-menu' }).Count -le 1 -and
        @($menuOperations | Where-Object { $_ -ceq 'open-model-submenu' }).Count -le 1) 'each UIA menu level expands at most once; keyboard input is separately bounded'
    Pass ((Json $menu) -notmatch 'btn-|mnu-|Try again|Sign in|What should') 'menu evidence never exposes selectors or unrelated UI text'
    Pass (@($menuOperations | Where-Object { $_ -ceq 'search-alpha' }).Count -eq 0) 'keyboard alternative does not repeat the completed search diagnostic'
    Pass (@($menuOperations | Where-Object { $_ -ceq 'model-tab' }).Count -le 4 -and
        @($menuOperations | Where-Object { $_ -ceq 'model-enter' }).Count -le 1) 'at most four single Tabs and one explicitly admitted activation'
    if ($menuMode -like 'keyboard-*') {
        Pass ($menu.submenu.actions -eq 0 -and ($menuMode -ceq 'keyboard-bad-focus' -or $menu.submenu.keyboard.lastFocus -is [System.Collections.IDictionary])) 'keyboard evidence is separate from UIA submenu expansion'
        if ($menuMode -cin @('keyboard-immediate','keyboard-beta','keyboard-one-tab','keyboard-menu-entry')) {
            $middle=$(if($menuMode -ceq 'keyboard-one-tab'){'focused,model-tab,'}else{''})
            Pass ($menu.status -ceq 'observed' -and $menu.submenu.keyboard.activationSent -and -not $diagnostic.partialActionUncertain -and
                ($menuOperations -join ',') -ceq ('read,open-model-menu,read,'+$middle+'focused,model-enter,focused,read')) 'only a freshly confirmed model focus gets one Enter then focus and screen reads'
        } else {
            Pass ($menu.status -ceq 'stopped') 'unavailable protected unchanged exhausted or uncertain navigation stops'
            if ($menuMode -ceq 'keyboard-limit') { Pass ($keyTabs -eq 4 -and $focusReads -eq 5 -and -not $keyEntered -and -not $diagnostic.partialActionUncertain) 'every bounded Tab has a before/after focused read, never a blind Enter' }
            if ($menuMode -ceq 'keyboard-unchanged') { Pass ($keyTabs -eq 1 -and $focusReads -eq 2) 'no-effect Tab is not repeated' }
            if ($menuMode -cin @('keyboard-none','keyboard-setup','keyboard-bad-focus')) { Pass ($keyTabs -eq 0 -and -not $keyEntered) 'missing ownership evidence or protected focus cannot receive keys' }
            if ($menuMode -cin @('keyboard-tab-failure','keyboard-bad-receipt','keyboard-enter-failure')) {
                Pass ($diagnostic.partialActionUncertain -and $menuOperations[-1] -cin @('model-tab','model-enter')) 'uncertain key or bad receipt stops without fallback transport or another read'
            }
            if ($menuMode -ceq 'keyboard-after-protected') { Pass ($keyEntered -and $menuOperations[-1] -ceq 'focused' -and $diagnostic.partialActionUncertain) 'new protected focus after Enter prevents further reads or keys' }
        }
    } elseif ($menuMode -like 'submenu-*') {
        Pass ($menu.initialLabels.betaVisibleCount -eq 0 -and $menu.submenu.observation.nameEvidence.Count -le 4) 'first panel evidence remains distinct and bounded'
        if ($menuMode -cin @('submenu-normal','submenu-combo','submenu-menuitem','submenu-promoted')) {
            $expected=$(if($menuMode -ceq 'submenu-promoted'){'read,open-model-menu,read,resolve-submodel-button,open-model-submenu,read'}else{'read,open-model-menu,read,open-model-submenu,read'})
            Pass ($menu.status -ceq 'observed' -and $menu.actions -eq 2 -and $menu.submenu.expansionInvoked -and
                -not $diagnostic.partialActionUncertain -and ($menuOperations -join ',') -ceq $expected) 'same already-read tree admits one strict submenu expansion then one final read'
        } elseif ($menuMode -ceq 'submenu-uncertain') {
            Pass ($menu.status -ceq 'stopped' -and $menu.actions -eq 2 -and $diagnostic.partialActionUncertain -and
                ($menuOperations -join ',') -ceq 'read,open-model-menu,read,open-model-submenu') 'uncertain submenu stops without another action or read'
        } else {
            Pass ($menu.actions -eq 5 -and $menu.submenu.actions -eq 0 -and -not $diagnostic.partialActionUncertain -and
                ($menuOperations -join ',') -ceq ('read,open-model-menu,read,focused'+(',model-tab,focused'*4)) -and $menu.status -ceq 'stopped' -and
                $menu.submenu.keyboard.reason -ceq 'navigation_limit') 'unknown name or offscreen label permits bounded focus navigation but no guessed activation'
        }
    } elseif ($menuMode -cin @('normal','short','promoted','missing-beta','next-protected')) {
        $expected=$(if($menuMode -ceq 'promoted'){'read,resolve-model-effort,open-model-menu,read'}elseif($menuMode -ceq 'missing-beta'){'read,open-model-menu,read,focused'+(',model-tab,focused'*4)}else{'read,open-model-menu,read'})
        $expectedStatus=$(if($menuMode -ceq 'missing-beta'){'stopped'}else{'observed'})
        Pass ($menu.status -ceq $expectedStatus -and $menu.actions -eq $(if($menuMode -ceq 'missing-beta'){5}else{1}) -and $menu.expansionInvoked -and -not $diagnostic.partialActionUncertain -and
            ($menuOperations -join ',') -ceq $expected) 'one initial expansion retains explicit readonly or bounded keyboard outcome'
        if ($menuMode -ceq 'missing-beta') { Pass ($menu.labels.betaVisibleCount -eq 0) 'alpha entry alone is not beta evidence' }
    } elseif ($menuMode -cin @('resolution-missing-state','resolution-expanded')) {
        Pass ($menu.actions -eq 0 -and -not $diagnostic.partialActionUncertain -and
            ($menuOperations -join ',') -ceq 'read,resolve-model-effort' -and
            $diagnostic.bindings[0].reason -ceq 'resolution_expand_state_unavailable') 'missing or changed resolved expansion state stops before any action'
    } elseif ($menuMode -ceq 'disabled') {
        Pass ($menu.actions -eq 0 -and ($menuOperations -join ',') -ceq 'read') 'disabled entry never invokes'
    } else {
        Pass ($menu.status -ceq 'stopped' -and $menu.actions -eq 1 -and $diagnostic.partialActionUncertain -and $null -eq $menu.labels) 'uncertain menu or final identity never commits label acceptance'
        if ($menuMode -cin @('uncertain','wrong-pattern')) { Pass (($menuOperations -join ',') -ceq 'read,open-model-menu') 'uncertain expansion or wrong receipt stops without fallback or retry' }
    }
    $prior=[pscustomobject]@{status='observed';stage='confirmation';reason='fixed';exitCode=0}
    Pass (((Get-WinAppModelMenuOutcome $prior $menu $true $diagnostic.partialActionUncertain).exitCode -eq 0) -eq
        ($menuMode -cin @('normal','short','promoted','submenu-normal','submenu-combo','submenu-menuitem','submenu-promoted','keyboard-immediate','keyboard-beta','keyboard-one-tab','keyboard-menu-entry'))) 'only both known labels and confirmed capture complete the menu observation'
    Pass ((Get-WinAppModelMenuOutcome $prior $menu $false $false).exitCode -eq 1) 'missing menu pixels blocks completion'
}
$script:menuOperations=@();$diagnostic=[ordered]@{}
$stopped=Invoke-WinAppModelMenu $null 123 {} $menuWatch $null $diagnostic
Pass ($stopped.actions -eq 0 -and $menuOperations.Count -eq 0) 'missing confirmed onboarding never starts menu commands'
$prior=[pscustomobject]@{status='blocked';stage='earlier-failure';reason='fixed';exitCode=1}
Pass ((Get-WinAppModelMenuOutcome $prior $null $false $true).stage -ceq 'earlier-failure') 'menu never hides earlier failure'
# Self-authored labels are profile-local and never authorize a model choice.
$selfProfile='synthetic-models-4239'
Reject { Get-WinAppModelNames 'SYNTHETIC-MODELS-4239' } 'unknown model profile is rejected'
foreach ($label in @('Synthetic Alpha [alpha]','Synthetic Beta [beta]')) {
    $focus=Read-WinAppModelFocus (Json (Focused $label 'RadioButton')) -FixtureProfile $selfProfile
    Pass ($focus.Observation.kind -ceq 'model' -and -not $focus.Observation.activationAllowed) 'self-authored model selection is forbidden'
    Pass (-not (Read-WinAppModelFocus (Json (Focused $label))).Observation.activationAllowed) 'legacy focus does not admit new labels'
}
foreach ($label in @('Select model','Choose model')) {
    Pass ((Read-WinAppModelFocus (Json (Focused $label)) -FixtureProfile $selfProfile).Observation.activationAllowed) 'only existing menu focus may be activated'
}
foreach ($label in @('Sign in','Continue','Windows setup',"Couldn't check Windows setup",'Try again','Permission required')) {
    Pass (-not (Read-WinAppModelFocus (Json (Focused $label)) -FixtureProfile $selfProfile).Observation.activationAllowed) 'self-authored mode retains protected focus stops'
}
$selfPage=ModelPage
$selfPage.windows[0].elements[1].name='Synthetic Alpha [alpha]'
$selfPage.windows[0].elements[1].automationId='selfmodel'
$selfPage.windows[0].elements[1].selector='btn-selfmodel-abcd'
$selfSnapshot=ConvertTo-WinAppWelcomeSnapshot (Json $selfPage) 123 -FixtureProfile $selfProfile
Pass (@($selfSnapshot.Nodes | Where-Object { $_.Name -cin @("Couldn't check Windows setup",'Try again') }).Count -eq 2) 'real informational setup region remains in the synthetic page'
Pass ((Get-WinAppModelMenuObservation $selfSnapshot -FixtureProfile $selfProfile).decision -ceq 'ready') 'known informational Windows setup nodes do not block separate model-menu observation'
Pass ((Get-WinAppModelMenuObservation $selfSnapshot).decision -cne 'ready') 'legacy current-model menu rejects new label'
$selfSnapshot.Decision='model_menu_ready'
$target=Get-WinAppWelcomeTarget $selfSnapshot 'Synthetic Alpha [alpha]' 'Button' -FixtureProfile $selfProfile
Pass ($target.Name -ceq 'Synthetic Alpha [alpha]') 'existing expand target bound without model selection'
Reject { Get-WinAppWelcomeTarget $selfSnapshot 'Synthetic Beta [beta]' 'RadioButton' -FixtureProfile $selfProfile } 'beta selection is not a menu target'
Reject { Get-WinAppWelcomeTarget $selfSnapshot 'Try again' 'Button' -FixtureProfile $selfProfile } 'informational setup retry is never an action target'
$selfPage.windows[0].elements+=@(Node 'Synthetic Beta [beta]' 'RadioButton')
$selfPage.windows[0].elementCount++
$labels=Get-WinAppModelMenuLabels (ConvertTo-WinAppWelcomeSnapshot (Json $selfPage) 123 -FixtureProfile $selfProfile) -FixtureProfile $selfProfile
Pass ($labels.alphaVisibleCount -eq 1 -and $labels.betaVisibleCount -eq 1 -and -not $labels.protectedScreen) 'both exact self-authored labels are observed'
Pass ((Get-WinAppModelMenuLabels (ConvertTo-WinAppWelcomeSnapshot (Json $selfPage) 123)).alphaVisibleCount -eq 0) 'legacy label evidence excludes self-authored rows'
foreach ($variant in @('hidden','disabled','duplicate','protected')) {
    $bad=Json $selfPage | ConvertFrom-Json
    if ($variant -ceq 'hidden') { $bad.windows[0].elements[1].isOffscreen=$true }
    elseif ($variant -ceq 'disabled') { $bad.windows[0].elements[1].isEnabled=$false }
    elseif ($variant -ceq 'protected') { $bad.windows[0].elements+=@(Node 'Sign in' 'Button');$bad.windows[0].elementCount++ }
    else { $bad.windows[0].elements+=@($bad.windows[0].elements[1]);$bad.windows[0].elementCount++ }
    $labels=Get-WinAppModelMenuLabels (ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123 -FixtureProfile $selfProfile) -FixtureProfile $selfProfile
    Pass $labels.protectedScreen 'hidden disabled or duplicate synthetic labels stop the observation'
}
# Exact current-model opener observed in 8c9's pixels, not a new model-item name.
$nonePage=ModelPage
$nonePage.windows[0].elements[1].name='Synthetic Alpha [alpha] None'
$nonePage.windows[0].elements[1].automationId='selfnone'
$nonePage.windows[0].elements[1].selector='selfnone'
$noneSnapshot=ConvertTo-WinAppWelcomeSnapshot (Json $nonePage) 123 -FixtureProfile $selfProfile
$noneObservation=Get-WinAppModelMenuObservation $noneSnapshot -FixtureProfile $selfProfile
Pass ($noneObservation.decision -ceq 'ready' -and $noneObservation.labelKind -ceq 'model_and_none') 'observed None suffix identifies the menu without claiming Medium'
Pass ((Get-WinAppModelMenuObservation $noneSnapshot).decision -cne 'ready') 'legacy profile does not admit the None opener'
Pass ((Get-WinAppModelMenuLabels $noneSnapshot -FixtureProfile $selfProfile).alphaVisibleCount -eq 0) 'None opener is not counted as a model-list item'
Pass (-not (Read-WinAppModelFocus (Json (Focused 'Synthetic Alpha [alpha] None')) -FixtureProfile $selfProfile).Observation.activationAllowed) 'None label does not authorize keyboard model activation'
$noneSnapshot.Decision='model_menu_ready'
$noneResolved=Resolution 'Synthetic Alpha [alpha] None' 'Button' 'selfnone' 'btn-selfnone-abcd'
$noneResolved.element.expandState='collapsed'
$binding=[ordered]@{}
$noneTarget=Get-WinAppWelcomeTarget $noneSnapshot 'Synthetic Alpha [alpha] None' 'Button' { Json $noneResolved } $binding -FixtureProfile $selfProfile
Pass ($noneTarget.Selector -ceq 'btn-selfnone-abcd' -and $noneTarget.ExpandState -ceq 'collapsed' -and $binding.bindings[0].resolved) 'None opener retains exact resolved identity and collapsed state'
foreach ($variant in @('missing','expanded','disabled','offscreen','wrong-name','wrong-type')) {
    $bad=Json $noneResolved | ConvertFrom-Json
    switch ($variant) {
        'missing' { $bad.element.PSObject.Properties.Remove('expandState') }
        'expanded' { $bad.element.expandState='expanded' }
        'disabled' { $bad.element.isEnabled=$false }
        'offscreen' { $bad.element.isOffscreen=$true }
        'wrong-name' { $bad.element.name='Synthetic Alpha [alpha]' }
        'wrong-type' { $bad.element.type='MenuItem' }
    }
    Reject { Get-WinAppWelcomeTarget $noneSnapshot 'Synthetic Alpha [alpha] None' 'Button' { Json $bad } ([ordered]@{}) -FixtureProfile $selfProfile } 'None typed resolution cannot skip identity or collapsed-state checks'
}
foreach ($variant in @('duplicate','plain-and-none','disabled','offscreen','expanded','missing','wrong-type','near-name')) {
    $bad=Json $nonePage | ConvertFrom-Json
    switch ($variant) {
        'duplicate' { $bad.windows[0].elements+=@($bad.windows[0].elements[1]);$bad.windows[0].elementCount++ }
        'plain-and-none' { $bad.windows[0].elements+=@(Node 'Synthetic Alpha [alpha]' 'Button' 'btn-other-abcd');$bad.windows[0].elementCount++ }
        'disabled' { $bad.windows[0].elements[1].isEnabled=$false }
        'offscreen' { $bad.windows[0].elements[1].isOffscreen=$true }
        'expanded' { $bad.windows[0].elements[1].expandState='expanded' }
        'missing' { $bad.windows[0].elements[1].PSObject.Properties.Remove('expandState') }
        'wrong-type' { $bad.windows[0].elements[1].type='RadioButton' }
        'near-name' { $bad.windows[0].elements[1].name='Synthetic Alpha [alpha] None extra' }
    }
    $page=ConvertTo-WinAppWelcomeSnapshot (Json $bad) 123 -FixtureProfile $selfProfile
    Pass ((Get-WinAppModelMenuObservation $page -FixtureProfile $selfProfile).decision -cne 'ready') 'None opener remains exact unique visible enabled collapsed Button only'
}
Write-Output ('WINAPP_WELCOME_OFFLINE_CHECKS='+$checks)
