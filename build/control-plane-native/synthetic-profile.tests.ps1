# Offline, self-authored fixtures only. No catalog, network or executable is opened.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'invoke.ps1')
function Expect-SyntheticReject([scriptblock]$Action) {
    $rejected = $false
    try { & $Action } catch { $rejected = $true }
    if (-not $rejected) { throw 'FAIL: synthetic profile boundary accepted invalid input' }
}
function New-SyntheticSummary {
    return [pscustomobject]@{
        schema=1;scope='self-authored-models-loopback-only';status='passed';stage='complete';failure=''
        native_sha256='4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'
        binary_verified=$true;version_verified=$true
        config_reads=9;layer_reads=6;stable_layer_rereads=3;layer_version_changes=0
        catalog_models=2;threads=2;turns=4;assistant_messages=4;thread_reads=2
        thread_list_queries=2;thread_list_pages=4;thread_list_items=4;ownership_rejections=2
        alpha_requests=2;beta_requests=2;fixture_errors=0;unexpected_server_requests=0
        adapter_clean_exit=$true;cleanup_complete=$true;restart_cycles=1;resumed_threads=2
    }
}
Assert-ControlPlaneNativeSummary (New-SyntheticSummary) -AcceptanceProfile 'synthetic-models-4239'
foreach ($profile in @('', 'app-server-4239', 'SYNTHETIC-MODELS-4239', 'synthetic-models-4239 ')) {
    Expect-SyntheticReject { Assert-ControlPlaneNativeSummary (New-SyntheticSummary) -AcceptanceProfile $profile }
}
Expect-SyntheticReject {
    $report = New-SyntheticSummary
    $report.scope = 'synthetic-loopback-only'
    Assert-ControlPlaneNativeSummary $report -AcceptanceProfile 'synthetic-models-4239'
}
Expect-SyntheticReject {
    $report = New-SyntheticSummary
    $report.native_sha256 = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
    Assert-ControlPlaneNativeSummary $report -AcceptanceProfile 'synthetic-models-4239'
}
foreach ($field in @('config_reads','layer_reads','stable_layer_rereads','layer_version_changes',
    'catalog_models','threads','turns','assistant_messages','thread_reads','thread_list_queries',
    'thread_list_pages','thread_list_items','ownership_rejections','alpha_requests','beta_requests',
    'fixture_errors','unexpected_server_requests','restart_cycles','resumed_threads')) {
    Expect-SyntheticReject {
        $report = New-SyntheticSummary
        $report.$field++
        Assert-ControlPlaneNativeSummary $report -AcceptanceProfile 'synthetic-models-4239'
    }
}
foreach ($field in @('binary_verified','version_verified','adapter_clean_exit','cleanup_complete')) {
    Expect-SyntheticReject {
        $report = New-SyntheticSummary
        $report.$field = $false
        Assert-ControlPlaneNativeSummary $report -AcceptanceProfile 'synthetic-models-4239'
    }
}
foreach ($field in @('restart_cycles','resumed_threads')) {
    Expect-SyntheticReject {
        $report = New-SyntheticSummary
        $report.PSObject.Properties.Remove($field)
        Assert-ControlPlaneNativeSummary $report -AcceptanceProfile 'synthetic-models-4239'
    }
}
$argsWithoutCatalog = @(Get-ControlPlaneNativeCatalogArguments 'synthetic-models-4239' $false $null)
if ($argsWithoutCatalog.Count -ne 0) { throw 'FAIL: synthetic mode generated catalog arguments' }
foreach ($value in @($null, '', ' ', 'never-open-this-file')) {
    Expect-SyntheticReject { Get-ControlPlaneNativeCatalogArguments 'synthetic-models-4239' $true $value }
}
foreach ($profile in @('', 'app-server-4239')) {
    Expect-SyntheticReject { Get-ControlPlaneNativeCatalogArguments $profile $false $null }
    Expect-SyntheticReject { Get-ControlPlaneNativeCatalogArguments $profile $true '' }
    $legacy = @(Get-ControlPlaneNativeCatalogArguments $profile $true 'never-open-this-file')
    if ($legacy.Count -ne 2 -or $legacy[0] -cne '-catalog' -or $legacy[1] -cne 'never-open-this-file') {
        throw 'FAIL: legacy catalog arguments changed'
    }
}
Expect-SyntheticReject { Get-ControlPlaneNativeCatalogArguments 'SYNTHETIC-MODELS-4239' $false $null }
$tokens = $null; $errors = $null
$invoke = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'invoke.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: wrapper syntax' }
$body = @($invoke.FindAll({ param($n)
    $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Invoke-ControlPlaneNative'
}, $true))[0].Extent.Text
if (-not $body.Contains("`$PSBoundParameters.ContainsKey('CatalogPath')") -or
    -not $body.Contains('foreach ($arg in $catalogArguments) { $psi.ArgumentList.Add($arg) }') -or
    $body.IndexOf('Get-ControlPlaneNativeCatalogArguments') -gt $body.IndexOf('Get-Item')) {
    throw 'FAIL: catalog presence must be checked before any file access'
}
$ci = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'ci-4239.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: CI entry syntax' }
$admission = @($ci.FindAll({ param($n)
    $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Assert-Standalone4239CI'
}, $true))
if ($admission.Count -ne 1) { throw 'FAIL: CI admission guard missing' }
. ([scriptblock]::Create($admission[0].Extent.Text))
$context = @{
    GITHUB_ACTIONS='true';RUNNER_OS='Windows';GITHUB_EVENT_NAME='push'
    GITHUB_REPOSITORY='breakingtime126/magpie';GITHUB_REF='refs/heads/experiment/app-server-bridge-v0.1.3035'
    GITHUB_SHA=('a' * 40);RUNNER_TEMP=[IO.Path]::GetTempPath()
}
$saved = @{}
try {
    foreach ($name in $context.Keys) {
        $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        [Environment]::SetEnvironmentVariable($name, $context[$name], 'Process')
    }
    Assert-Standalone4239CI 'synthetic-models-4239'
    foreach ($name in $context.Keys) {
        [Environment]::SetEnvironmentVariable($name, 'unreviewed-context', 'Process')
        Expect-SyntheticReject { Assert-Standalone4239CI 'synthetic-models-4239' }
        [Environment]::SetEnvironmentVariable($name, $context[$name], 'Process')
    }
    foreach ($bad in @('', 'SYNTHETIC-MODELS-4239', 'synthetic-models-4239 ')) {
        Expect-SyntheticReject { Assert-Standalone4239CI $bad }
    }
} finally {
    foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
}
$branches = @($ci.FindAll({ param($n)
    $n -is [Management.Automation.Language.IfStatementAst] -and
    $n.Clauses[0].Item1.Extent.Text -ceq "`$AcceptanceProfile -ceq 'synthetic-models-4239'" -and
    $n.Clauses[0].Item2.Extent.Text.Contains('Invoke-ControlPlaneNative')
}, $true))
if ($branches.Count -ne 1) { throw 'FAIL: one explicit synthetic invocation branch required' }
$synthetic = $branches[0].Clauses[0].Item2.Extent.Text
$legacy = $branches[0].ElseClause.Extent.Text
foreach ($forbidden in @('CatalogPath', '$catalog', 'Save-StandalonePublicInput')) {
    if ($synthetic.Contains($forbidden)) { throw 'FAIL: synthetic branch accesses an external catalog' }
}
if (-not $synthetic.Contains('-AcceptanceProfile synthetic-models-4239') -or
    -not $legacy.Contains('-AcceptanceProfile app-server-4239') -or
    -not $legacy.Contains('-CatalogPath $catalog') -or
    -not $legacy.Contains('Save-StandalonePublicInput')) { throw 'FAIL: explicit profile branches changed' }
if (-not $ci.Extent.Text.Contains("`$report.scope = 'synthetic-models-4239-only'") -or
    -not $ci.Extent.Text.Contains("`$report.profile = 'synthetic-models-4239'")) { throw 'FAIL: report lacks distinct provenance' }
Write-Output 'PASS: self-authored profile scopes, catalog prohibition and isolated CI branches'
