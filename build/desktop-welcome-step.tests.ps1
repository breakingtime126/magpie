# Pure state-policy fixtures. No Desktop, UIA, network, accounts or input.
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
Write-Output 'Desktop welcome policy fixtures passed.'
