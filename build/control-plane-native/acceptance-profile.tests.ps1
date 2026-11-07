# Offline schema and argument fixtures only; never invoke a native executable.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'invoke.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: acceptance wrapper syntax' }
$validator = @($ast.FindAll({ param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -ceq 'Assert-ControlPlaneNativeSummary'
}, $true))
if ($validator.Count -ne 1) { throw 'FAIL: one standalone summary validator required' }
. ([scriptblock]::Create($validator[0].Extent.Text))
function New-AcceptanceSummary([string]$Hash) {
    return [pscustomobject]@{
        schema=1; scope='synthetic-loopback-only'; status='passed'; stage='complete'; failure=''
        native_sha256=$Hash; binary_verified=$true; version_verified=$true
        config_reads=9; layer_reads=6; stable_layer_rereads=3; layer_version_changes=0
        catalog_models=2; threads=2; turns=4; assistant_messages=4; thread_reads=2
        thread_list_queries=2; thread_list_pages=4; thread_list_items=4; ownership_rejections=2
        alpha_requests=2; beta_requests=2; fixture_errors=0; unexpected_server_requests=0
        adapter_clean_exit=$true; cleanup_complete=$true
    }
}
function Expect-Reject([scriptblock]$Action) {
    $rejected = $false
    try { & $Action } catch { $rejected = $true }
    if (-not $rejected) { throw 'FAIL: mismatched profile or weakened summary accepted' }
}
$old = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
$current = '4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'
Assert-ControlPlaneNativeSummary (New-AcceptanceSummary $old)
Assert-ControlPlaneNativeSummary (New-AcceptanceSummary $current) -AcceptanceProfile 'app-server-4239'
Expect-Reject { Assert-ControlPlaneNativeSummary (New-AcceptanceSummary $current) }
Expect-Reject { Assert-ControlPlaneNativeSummary (New-AcceptanceSummary $old) -AcceptanceProfile 'app-server-4239' }
foreach ($bad in @('4239', 'desktop-4239', 'APP-SERVER-4239', 'app-server-4239 ', $old, $current)) {
    Expect-Reject { Assert-ControlPlaneNativeSummary (New-AcceptanceSummary $current) -AcceptanceProfile $bad }
}
foreach ($field in @('layer_version_changes', 'thread_list_pages', 'turns', 'assistant_messages', 'ownership_rejections')) {
    Expect-Reject {
        $value = New-AcceptanceSummary $current
        $value.$field = 1
        Assert-ControlPlaneNativeSummary $value -AcceptanceProfile 'app-server-4239'
    }
}
foreach ($field in @('binary_verified', 'version_verified', 'adapter_clean_exit', 'cleanup_complete')) {
    Expect-Reject {
        $value = New-AcceptanceSummary $current
        $value.$field = $false
        Assert-ControlPlaneNativeSummary $value -AcceptanceProfile 'app-server-4239'
    }
}
$invoke = @($ast.FindAll({ param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Invoke-ControlPlaneNative'
}, $true))
if ($invoke.Count -ne 1) { throw 'FAIL: one explicit acceptance wrapper required' }
$body = $invoke[0].Extent.Text
foreach ($literal in @("[string]`$AcceptanceProfile = ''", "`$psi.ArgumentList.Add('-acceptance-profile')",
    '$psi.ArgumentList.Add($AcceptanceProfile)', 'Assert-ControlPlaneNativeSummary $report -AcceptanceProfile $AcceptanceProfile')) {
    if (-not $body.Contains($literal)) { throw 'FAIL: acceptance profile did not reach launch and validation' }
}
Write-Output 'PASS: standalone acceptance profile schema and argument fixtures'
