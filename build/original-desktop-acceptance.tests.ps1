# Offline caller contracts. Does not run the coordinator, Windows APIs or UIA.
[CmdletBinding()]
param([string]$CallerPath='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-connect-action-core.ps1')
function Assert([bool]$Condition,[string]$Message) { if (-not $Condition) { throw ('FAIL: '+$Message) } }
function Parse([string]$Name) {
    $path=$Name;if (-not [IO.Path]::IsPathRooted($path)) { $path=Join-Path $PSScriptRoot $Name }
    $tokens=$null;$errors=$null;$ast=[Management.Automation.Language.Parser]::ParseFile($path,[ref]$tokens,[ref]$errors)
    Assert ($errors.Count -eq 0) ('parse '+$Name);return $ast
}
if (-not $CallerPath) { $CallerPath=Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1' }
$caller=Parse $CallerPath
# Automatic PowerShell variables are case-insensitive and cannot be reused for fixture paths.
$assignments=@($caller.FindAll({param($node) $node -is [Management.Automation.Language.AssignmentStatementAst]},$true))
foreach ($assignment in $assignments) {
    if ($assignment.Left -is [Management.Automation.Language.VariableExpressionAst]) {
        Assert ($assignment.Left.VariablePath.UserPath -inotmatch '^(HOME|PID|PSHOME|Host|Error|ExecutionContext|PSVersionTable|PSScriptRoot|PSCommandPath)$') 'fixture variables must not overwrite PowerShell automatic variables'
    }
}
$package=Parse 'package-native-magpie-windows.ps1'
$sequence=Parse 'original-desktop-sequence.ps1'
foreach ($name in @('Get-AcceptanceSupplierFixture','Test-AcceptanceJournal')) {
    $function=@($caller.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name},$false))
    Assert ($function.Count -eq 1) ('unique function '+$name)
    Invoke-Expression $function[0].Extent.Text
}
$fixture=Get-AcceptanceSupplierFixture
Assert ($fixture.noAutoGroups -eq $true -and $fixture.providers.Count -eq 1) 'ordinary single provider fixture'
$p=$fixture.providers[0]
Assert (($p.Keys | Sort-Object) -join ',' -ceq 'id,key,models,name,responses') 'no account, refresh, plugin, decision or capability metadata'
Assert ($p.responses -ceq 'https://aihub.friendtimes.net/v1' -and $p.key -ceq 'PUBLIC-INERT-NOT-A-CREDENTIAL') 'ordinary endpoint and public inert key only'
Assert ($p.models.Count -eq 1 -and $p.models[0] -ceq 'original-ui-fixture-model') 'explicit inert model pick without Fetch'
$root=Join-Path ([IO.Path]::GetTempPath()) ('original-caller-test-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
try {
    $path=Join-Path $root 'journal.json'
    Assert (-not (Test-AcceptanceJournal $path)) 'truly absent journal'
    [IO.File]::WriteAllText($path,'{}')
    Assert (Test-AcceptanceJournal $path) 'existing journal'
    $rejected=$false;try { Test-AcceptanceJournal $root } catch { $rejected=$true }
    Assert $rejected 'directory is not an absent journal'
} finally { [IO.Directory]::Delete($root,$true) }
$commands=@($caller.FindAll({param($node) $node -is [Management.Automation.Language.CommandAst]},$true) | ForEach-Object {$_.GetCommandName()})
foreach ($name in @('Set-EntryOverrides','Restore-EntryOverrides','Write-EntryUserEnvironment','Invoke-WebRequest','Invoke-RestMethod','Set-ExecutionPolicy','Remove-Item')) {
    Assert ($commands -cnotcontains $name) ('no fallback action '+$name)
}
$text=$caller.Extent.Text
Assert ($text.Contains("[original-connect-desktop-v1]") -and $text.Contains("'original-ui-v1'")) 'exact caller opt-in'
Assert ($text.Contains('if (-not $sequenceEntered -and $null -eq $report.sequence') -and
    $text.IndexOf('$sequenceEntered=$true') -lt $text.IndexOf('$report.sequence=Invoke-OriginalDesktopSequence')) 'unexpected post-dispatch orchestration failure cannot use pre-sequence cleanup'
Assert ($text.Contains('Invoke-OriginalDesktopSequence -Connect') -and $text.Contains('Invoke-OriginalConnectAction -Mode Restore')) 'caller routes original Connect and Restore through bounded actions'
Assert ($text.IndexOf('$report.stage=''owned-window-evidence''') -gt $text.IndexOf('$safeCleanup=$report.sequence.cleanupAllowed')) 'captures only after recovery'
Assert ($text.Contains('if ($safeCleanup)') -and $text.Contains('if ($null -ne $context -and $context.Evidence.activated)')) 'cleanup requires recovery and held activation authority'
Assert ($text.Contains('restoredDesktopStartVerified=$false') -and $text.Contains('backendHandshakeVerified=$false') -and $text.Contains('zeroEgressProven=$false')) 'bounded claims retained'
Assert (-not $text.Contains("RemoveAll") -and -not $text.Contains(".Delete(")) 'coordinator preserves recovery files'
Assert ($package.Extent.Text.Contains('$DiscoverOriginalConnect -or $env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY')) 'discovery and action gates exclusive'
Assert ($package.Extent.Text.Contains('-AcceptOriginalConnect') -or $package.Extent.Text.Contains('[switch]$AcceptOriginalConnect')) 'package action option exists'
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '..\.github\workflows\experimental-codex-connectivity-preview.yml'))
Assert ($workflow.Contains('[original-connect-desktop-v1]') -and $workflow.Contains('MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE')) 'workflow action opt-in'
Assert (-not $workflow.Contains('secrets.')) 'acceptance workflow has no provider secrets'
Write-Output 'ORIGINAL_DESKTOP_ACCEPTANCE_CALLER_TESTS_PASSED'
