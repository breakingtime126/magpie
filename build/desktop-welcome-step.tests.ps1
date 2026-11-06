# Pure state-policy fixtures; optional native snapshot check under Windows PS5.1.
# No Desktop launch, UI access, network, accounts or input.
[CmdletBinding()]
param([switch]$CheckNativeInterop)
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'desktop-welcome-step.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw 'Helper parse failure.' }
$function = $ast.Find({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Get-WelcomeDecision' }, $true)
. ([scriptblock]::Create($function.Extent.Text))
function Node($name, $type, $interactive = $false, $enabled = $true) {
    [pscustomobject]@{ Name = $name; Type = $type; Interactive = $interactive; Enabled = $enabled }
}
$welcome = @((Node 'Which best describes your work?' 'Text'), (Node 'Engineering' 'RadioButton' $true),
    (Node 'Suggest personalized tasks' 'CheckBox' $true), (Node 'Continue' 'Button' $true $false),
    (Node 'Legal' 'RadioButton' $true), (Node 'Describe something else' 'Edit' $true $false))
function Expect($nodes, $expected) {
    $actual = Get-WelcomeDecision $nodes
    if ($actual -cne $expected) { throw "Expected $expected, got $actual." }
}
Expect $welcome 'welcome'
Expect @() 'unreviewed_screen'
Expect ($welcome + (Node 'Log in' 'Button' $true)) 'protected_screen'
Expect ($welcome + (Node 'By continuing you agree to terms of service' 'Text')) 'protected_screen'
Expect ($welcome + (Node 'Enable notifications' 'Button' $true)) 'unreviewed_text'
Expect ($welcome + (Node 'A new unreviewed decision' 'Text')) 'unreviewed_text'
Expect ($welcome + (Node 'Engineering' 'RadioButton' $true)) 'ambiguous_control'
Expect @($welcome | Where-Object { $_.Name -cne 'Suggest personalized tasks' }) 'ambiguous_control'
$wrongType = @($welcome | ForEach-Object { Node $_.Name $_.Type $_.Interactive $_.Enabled })
$wrongType[1].Type = 'Button'
Expect $wrongType 'ambiguous_control'
$wrongCase = @($welcome | ForEach-Object { Node $_.Name $_.Type $_.Interactive $_.Enabled })
$wrongCase[1].Name = 'engineering'
Expect $wrongCase 'unreviewed_text'
Expect @((Node 'What would you like help with?' 'Text'), (Node 'Continue' 'Button' $true)) 'unreviewed_screen'
# Chromium may expose noninteractive duplicate text labels. These do not add targets.
Expect ($welcome + (Node 'Which best describes your work?' 'Text') +
    (Node 'Engineering' 'Text') + (Node 'Suggest personalized tasks' 'Text') +
    (Node 'Continue' 'Text')) 'welcome'
Expect ($welcome + (Node 'Engineering' 'Button' $true)) 'ambiguous_control'
Expect ($welcome + (Node 'Continue' 'Button' $true)) 'ambiguous_control'
Expect ($welcome + (Node 'Suggest personalized tasks' 'CheckBox' $true)) 'ambiguous_control'
$enabledOther = @($welcome | ForEach-Object { Node $_.Name $_.Type $_.Interactive $_.Enabled })
$enabledOther[5].Enabled = $true
Expect $enabledOther 'unreviewed_control'
# Fixed guard classification is independent of raw exceptions.
$stopFunction = $ast.Find({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Stop-Welcome' }, $true)
. ([scriptblock]::Create($stopFunction.Extent.Text))
foreach ($reason in @('invalid_context', 'deadline_exceeded', 'identity_or_foreground_changed',
    'uia_tree_limit', 'uia_owner_mismatch', 'target_unavailable', 'suggestions_not_off',
    'selection_state_changed', 'unsupported_runtime')) {
    $result = [ordered]@{ reason = 'uia_provider_error' }
    $threw = $false
    try { Stop-Welcome $reason } catch { $threw = $true }
    if (-not $threw -or $result.reason -cne $reason) { throw 'Fixed failure reason was lost.' }
}
$observationFunction = $ast.Find({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Get-WelcomeTargetObservations' }, $true)
. ([scriptblock]::Create($observationFunction.Extent.Text))
$observations = Get-WelcomeTargetObservations ($welcome + (Node 'Engineering' 'Text'))
if ($observations.engineering.interactiveCount -ne 1 -or $observations.engineering.controlType -cne 'RadioButton' -or
    $observations.suggestions.controlType -cne 'CheckBox' -or $observations.continue.controlType -cne 'Button') {
    throw 'Known target diagnostics differ.'
}
$observations = Get-WelcomeTargetObservations ($welcome + (Node 'Engineering' 'Button' $true))
if ($observations.engineering.interactiveCount -ne 2 -or $observations.engineering.controlType -cne 'ambiguous') {
    throw 'Duplicate interactive target was not classified.'
}
$observations = Get-WelcomeTargetObservations @((Node 'Continue' 'Edit' $true), (Node 'Private canary label' 'Button' $true))
if ($observations.continue.controlType -cne 'Edit' -or $observations.engineering.controlType -cne 'absent' -or
    $observations.engineering.interactiveCount -ne 0 -or ($observations | ConvertTo-Json -Depth 5 -Compress) -match 'canary') {
    throw 'Target role/count or redaction failed.'
}
$lineageFunction = $ast.Find({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Test-WelcomeLineage' }, $true)
. ([scriptblock]::Create($lineageFunction.Extent.Text))
function Fact([long]$start, [int]$parent) {
    [pscustomobject]@{ Alive = $true; IdentityValid = $true; StartTicks = $start;
        ActualStartTicks = $start; ParentId = $parent; CurrentParentId = $parent }
}
function LineageFacts {
    return @{ 10 = (Fact 100 0); 20 = (Fact 200 10); 30 = (Fact 300 20) }
}
function ExpectLineage($facts, [int]$id, [bool]$expected) {
    if ((Test-WelcomeLineage $facts 10 $id) -ne $expected) { throw 'Ownership proof differs.' }
}
ExpectLineage (LineageFacts) 10 $true
ExpectLineage (LineageFacts) 20 $true
ExpectLineage (LineageFacts) 30 $true
ExpectLineage (LineageFacts) 0 $false
ExpectLineage (LineageFacts) 99 $false
foreach ($id in @(10, 20, 30)) {
    $facts = LineageFacts; $facts[$id].Alive = $false
    ExpectLineage $facts 30 $false
    $facts = LineageFacts; $facts[$id].IdentityValid = $false
    ExpectLineage $facts 30 $false
    $facts = LineageFacts; $facts[$id].ActualStartTicks++
    ExpectLineage $facts 30 $false
}
$facts = LineageFacts; $facts[20].CurrentParentId = 99
ExpectLineage $facts 30 $false
$facts = LineageFacts; $facts[20].ParentId = 99; $facts[20].CurrentParentId = 99
ExpectLineage $facts 30 $false
$facts = LineageFacts; $facts[20].StartTicks = 300; $facts[20].ActualStartTicks = 300
ExpectLineage $facts 30 $false
$facts = LineageFacts; $facts[20].ParentId = 30; $facts[20].CurrentParentId = 30
ExpectLineage $facts 30 $false
$facts = LineageFacts; $facts.Remove(20)
ExpectLineage $facts 30 $false
$facts = @{ 10 = (Fact 100 0) }
for ($i = 1; $i -le 8; $i++) { $facts[10 + $i] = Fact (100 + $i) (9 + $i) }
ExpectLineage $facts 18 $true
$facts[19] = Fact 109 18
ExpectLineage $facts 19 $false
$facts = LineageFacts
for ($i = 100; $i -lt 130; $i++) { $facts[$i] = Fact (1000 + $i) 10 }
ExpectLineage $facts 20 $false
foreach ($name in @('Test-WelcomeChromeOnly', 'Read-WelcomeReadySnapshot')) {
    $definition = $ast.Find({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq $name }, $true)
    . ([scriptblock]::Create($definition.Extent.Text))
}
$shell = @((Node 'ChatGPT' 'Window'), (Node '' 'Pane'), (Node '' 'Document'),
    (Node 'Minimize' 'Button' $true), (Node 'Close' 'Button' $true))
if (-not (Test-WelcomeChromeOnly $shell) -or (Test-WelcomeChromeOnly @()) -or
    (Test-WelcomeChromeOnly ($shell + (Node '' 'Button' $true))) -or
    (Test-WelcomeChromeOnly ($shell + (Node '' 'Edit' $true $false))) -or
    (Test-WelcomeChromeOnly ($shell + (Node 'Engineering' 'Text'))) -or
    (Test-WelcomeChromeOnly ($shell + (Node 'Private canary text' 'Text'))) -or
    (Test-WelcomeChromeOnly ($shell + (Node 'ChatGPT' 'Window')))) {
    throw 'Shell-only readiness condition differs.'
}
# A real full snapshot remains the only authority to proceed, after pending shell.
$readiness = [pscustomobject]@{ Reads = 0; Pauses = 0 }
$ready = Read-WelcomeReadySnapshot -ReadSnapshot {
    $readiness.Reads++
    if ($readiness.Reads -eq 1) { $shell } else { $welcome }
} -ElapsedSeconds { 0.0 } -Pause { $readiness.Pauses++ }
if ($ready.Decision -cne 'welcome' -or $readiness.Reads -ne 2 -or $readiness.Pauses -ne 1) {
    throw 'Pending-to-welcome readiness differs.'
}
$readiness = [pscustomobject]@{ Reads = 0; Pauses = 0 }
$ready = Read-WelcomeReadySnapshot -ReadSnapshot {
    $readiness.Reads++; $shell
} -ElapsedSeconds { 0.0 } -Pause { $readiness.Pauses++ }
if ($ready.Decision -cne 'accessibility_not_ready' -or $readiness.Reads -ne 4 -or $readiness.Pauses -ne 3) {
    throw 'Read-count readiness bound differs.'
}
$readiness = [pscustomobject]@{ Reads = 0; Pauses = 0 }
$ready = Read-WelcomeReadySnapshot -ReadSnapshot {
    $readiness.Reads++; $shell
} -ElapsedSeconds { 3.0 } -Pause { $readiness.Pauses++ }
if ($ready.Decision -cne 'accessibility_not_ready' -or $readiness.Reads -ne 1 -or $readiness.Pauses -ne 0) {
    throw 'Time readiness bound differs.'
}
foreach ($unsafe in @((Node 'Sign in' 'Button' $true), (Node 'Unknown content' 'Text'),
    (Node '' 'Button' $true), (Node 'Engineering' 'Text'))) {
    $readiness = [pscustomobject]@{ Reads = 0; Pauses = 0 }
    $ready = Read-WelcomeReadySnapshot -ReadSnapshot {
        $readiness.Reads++; $shell; $unsafe
    } -ElapsedSeconds { 0.0 } -Pause { $readiness.Pauses++ }
    if ($ready.Decision -ceq 'accessibility_not_ready' -or $readiness.Reads -ne 1 -or $readiness.Pauses -ne 0) {
        throw 'Unsafe content was retried.'
    }
}
$readiness = [pscustomobject]@{ Reads = 0; Pauses = 0 }
$ownershipFailed = $false
try {
    $null = Read-WelcomeReadySnapshot -ReadSnapshot {
        $readiness.Reads++
        if ($readiness.Reads -eq 2) { throw 'Synthetic ownership failure.' }
        $shell
    } -ElapsedSeconds { 0.0 } -Pause { $readiness.Pauses++ }
} catch { $ownershipFailed = $true }
if (-not $ownershipFailed -or $readiness.Reads -ne 2 -or $readiness.Pauses -ne 1) {
    throw 'Ownership failure was retried or swallowed.'
}
Write-Output 'Desktop welcome policy, ownership and readiness fixtures passed.'


if ($CheckNativeInterop) {
    try {
        if ($PSVersionTable.PSEdition -cne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or
            $PSVersionTable.PSVersion.Minor -ne 1 -or $env:OS -cne 'Windows_NT') {
            throw 'Native check requires Windows PowerShell 5.1.'
        }
        # Extract only the exact literal argument. Never execute the helper body,
        # its Add-Type command AST, or an expandable/evaluated script fragment.
        $definitions = @($ast.FindAll({ param($n)
            $n -is [Management.Automation.Language.CommandAst] -and
            $n.GetCommandName() -ceq 'Add-Type' -and $n.CommandElements.Count -eq 3 -and
            $n.CommandElements[1] -is [Management.Automation.Language.CommandParameterAst] -and
            $n.CommandElements[1].ParameterName -ceq 'TypeDefinition' -and
            $n.CommandElements[2] -is [Management.Automation.Language.StringConstantExpressionAst] -and
            $n.CommandElements[2].StringConstantType -eq [Management.Automation.Language.StringConstantType]::SingleQuotedHereString -and
            $n.CommandElements[2].Value.Contains('public static class WelcomeNative {')
        }, $true))
        if ($definitions.Count -ne 1) { throw 'Expected one constant native definition.' }
        $definition = $definitions[0].CommandElements[2].Value
        Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes 2>$null
        Add-Type -TypeDefinition $definition 2>$null
        # One supported read-only process snapshot. No window/UIA methods called.
        $parents = [WelcomeNative]::Parents()
        if ($parents.Count -lt 1 -or $parents.Count -gt 8192 -or -not $parents.ContainsKey([int]$PID)) {
            throw 'Invalid native snapshot.'
        }
    } catch {
        # Do not emit compiler/provider errors, process IDs, paths or the map.
        Write-Output 'Desktop welcome native interop check failed.'
        exit 1
    }
    Write-Output 'Desktop welcome native interop check passed.'
}
