# Import only function definitions. No network, verifier, native or Desktop run.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'ci-4239.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: standalone CI syntax' }
foreach ($name in @('Get-StandaloneIdentityHelpers', 'Assert-Standalone4239CI', 'Export-Standalone4239Native')) {
    $definitions = @($ast.FindAll({ param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($definitions.Count -ne 1) { throw 'FAIL: standalone CI function boundary' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
. (Get-StandaloneIdentityHelpers (Join-Path $PSScriptRoot '../test-desktop-cold-start.ps1'))
function Expect-StandaloneReject([scriptblock]$Action) {
    $rejected = $false
    try { & $Action } catch { $rejected = $true }
    if (-not $rejected) { throw 'FAIL: unreviewed standalone CI input accepted' }
}
foreach ($profile in @('', 'desktop-4239', 'APP-SERVER-4239', 'app-server-4239 ')) {
    Expect-StandaloneReject { Assert-Standalone4239CI $profile }
}
$context = @{
    GITHUB_ACTIONS='true';RUNNER_OS='Windows';GITHUB_EVENT_NAME='push'
    GITHUB_REPOSITORY='breakingtime126/magpie';GITHUB_REF='refs/heads/experiment/app-server-bridge-v0.1.3035'
    GITHUB_SHA=('a' * 40);RUNNER_TEMP=[IO.Path]::GetTempPath()
}
$saved = @{}
try {
    foreach ($name in $context.Keys) {
        $saved[$name] = [Environment]::GetEnvironmentVariable($name)
        [Environment]::SetEnvironmentVariable($name, $context[$name], 'Process')
    }
    Assert-Standalone4239CI 'app-server-4239'
    foreach ($name in $context.Keys) {
        [Environment]::SetEnvironmentVariable($name, 'unreviewed-context', 'Process')
        Expect-StandaloneReject { Assert-Standalone4239CI 'app-server-4239' }
        [Environment]::SetEnvironmentVariable($name, $context[$name], 'Process')
    }
} finally {
    foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
}
$identity = [pscustomobject]@{
    status='observed';manifest=[pscustomobject]@{version='26.1103.4239.0'}
    packageSha256='9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c'
    backend=[pscustomobject]@{sha256='4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'}
    archive=[pscustomobject]@{sha256='fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa'}
}
Assert-Desktop4239Identity $identity
foreach ($field in @('packageSha256','backend','archive','manifest')) {
    $bad = $identity | ConvertTo-Json -Depth 8 -Compress | ConvertFrom-Json
    if ($field -ceq 'packageSha256') { $bad.packageSha256 = '0' * 64 }
    elseif ($field -ceq 'manifest') { $bad.manifest.version = '26.1103.3517.0' }
    else { $bad.$field.sha256 = '0' * 64 }
    Expect-StandaloneReject { Assert-Desktop4239Identity $bad }
}
$temp = Join-Path ([IO.Path]::GetTempPath()) ('app-server-4239-fixture-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $temp)
try {
    $package = Join-Path $temp 'synthetic.msix'; $target = Join-Path $temp 'never-created.exe'
    [IO.File]::WriteAllText($package, 'synthetic non-executable package')
    Expect-StandaloneReject { Export-Standalone4239Native $package $target $identity }
    if (Test-Path -LiteralPath $target) { throw 'FAIL: extraction preceded exact package proof' }
} finally { Remove-Item -LiteralPath $temp -Recurse -Force }
$text = $ast.Extent.Text
foreach ($forbidden in @('Add-Appx', 'Start-Process', 'ChatGPT.exe', 'ExecutionPolicy', 'Set-Acl', 'reg.exe')) {
    if ($text.Contains($forbidden)) { throw 'FAIL: standalone CI crossed its process/permission boundary' }
}
foreach ($required in @('gui_verified=$false', '-AcceptanceProfile app-server-4239',
    "Get-PublicZipEntryDigest `$zip 'app/resources/codex.exe'", '[IO.FileMode]::CreateNew',
    'Assert-Standalone4239CI $AcceptanceProfile', '[IO.FileShare]::Read')) {
    if (-not $text.Contains($required)) { throw 'FAIL: missing standalone CI proof boundary' }
}
Write-Output 'PASS: standalone CI preparation guards and exact package rejection fixtures'
