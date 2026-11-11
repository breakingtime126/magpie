# Offline fixture and AST checks only: never run the worker's script body.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$asts=@{}
foreach ($name in @('original-connect-discovery.ps1','original-connect-discovery-worker.ps1')) {
    $tokens=$null;$errors=$null
    $asts[$name]=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $name),[ref]$tokens,[ref]$errors)
    if ($errors.Count) { throw 'Discovery syntax invalid.' }
}
$worker=$asts['original-connect-discovery-worker.ps1']
$definitions=@($worker.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Get-DiscoveryLabel'},$true))
if ($definitions.Count -ne 1) { throw 'Unique classifier missing.' }
. ([scriptblock]::Create($definitions[0].Extent.Text))
foreach ($pair in @(@('Configure Codex launch selection','configure'),@('Disconnect and restore Codex launch selection','restore'),@('Disconnect and restore','confirm'))) {
    if ((Get-DiscoveryLabel $pair[0]) -cne $pair[1]) { throw 'Exact label missing.' }
    foreach ($bad in @($pair[0].ToUpperInvariant(),(' '+$pair[0]),($pair[0]+' '),($pair[0]+' other'))) {
        if ((Get-DiscoveryLabel $bad) -cne '') { throw 'Ambiguous label accepted.' }
    }
}
foreach ($bad in @('Connect Codex to magpie','Continue','Sign in','Try again','', 'Configure Claude launch selection')) {
    if ((Get-DiscoveryLabel $bad) -cne '') { throw 'Unreviewed control accepted.' }
}
foreach ($ast in $asts.Values) {
    foreach ($call in @($ast.FindAll({param($n) $n -is [Management.Automation.Language.InvokeMemberExpressionAst]},$true))) {
        if ($call.Member.Extent.Text -cin @('Invoke','Toggle','SetFocus','Select','Expand','Collapse','SetValue','SendKeys','CloseMainWindow')) {
            throw 'Discovery must never act on the target.'
        }
    }
}
$source=$worker.Extent.Text
foreach ($required in @("'github-hosted'","'read-only-v1'",'Assert-DiscoveryOwner',
    '$nodes -ge 2048','$item.depth -ge 20','$watch.ElapsedMilliseconds -gt 10000','actions=0','connectVerified=$false')) {
    if (-not $source.Contains($required)) { throw 'Discovery bound missing.' }
}
$supervisor=$asts['original-connect-discovery.ps1'].Extent.Text
foreach ($required in @('$watch.ElapsedMilliseconds -gt 15000','$texts[$i].Length+$count -gt 8192',
    '$process.Kill()','$process.WaitForExit(3000)','$start.Environment.Clear()')) {
    if (-not $supervisor.Contains($required)) { throw 'Supervisor bound missing.' }
}
# Fixed diagnostic vocabulary and synthetic filtered/raw traversal fixtures.
foreach ($name in @('Get-DiscoveryLandmark','Get-DiscoveryRole','Read-DiscoveryView')) {
    $definitions=@($worker.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq $name},$true))
    if ($definitions.Count -ne 1) { throw 'Diagnostic helper missing.' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
if ((Get-DiscoveryLandmark 'Codex') -cne 'codex' -or
    (Get-DiscoveryLandmark 'Agents') -cne 'agents' -or
    (Get-DiscoveryLandmark 'Codex launch selection is not configured. Connect prepares future starts; it does not verify chat.') -cne 'notConfigured') { throw 'Fixed landmark missing.' }
foreach ($name in @('CODEX',' Codex','Codex other','arbitrary private text')) {
    if ((Get-DiscoveryLandmark $name) -cne '') { throw 'Unknown landmark escaped.' }
}
foreach ($role in @('Document','Pane','Button','CheckBox','Text')) {
    if ((Get-DiscoveryRole ('ControlType.'+$role)) -cne $role.ToLowerInvariant()) { throw 'Fixed role missing.' }
}
if ((Get-DiscoveryRole 'ControlType.Custom') -cne 'other') { throw 'Unknown role escaped.' }
$script:discoveryIdentityReads=0
function Assert-DiscoveryOwner { $script:discoveryIdentityReads++ }
$root=[pscustomobject]@{Current=[pscustomobject]@{Name='';IsControlElement=$true;IsContentElement=$false;ControlType=[pscustomobject]@{ProgrammaticName='ControlType.Pane'}}}
$script:rawFixtureNode=[pscustomobject]@{Current=[pscustomobject]@{Name='Codex';IsControlElement=$false;IsContentElement=$true;ControlType=[pscustomobject]@{ProgrammaticName='ControlType.Text'}}}
$controlWalker=[pscustomobject]@{}
$controlWalker | Add-Member ScriptMethod GetFirstChild {param($element) return $null}
$controlWalker | Add-Member ScriptMethod GetNextSibling {param($element) return $null}
$rawWalker=[pscustomobject]@{}
$rawWalker | Add-Member ScriptMethod GetFirstChild {param($element) if ($element.Current.Name -ceq '') { return $script:rawFixtureNode };return $null}
$rawWalker | Add-Member ScriptMethod GetNextSibling {param($element) return $null}
$result=@{reason='none'};$watch=[Diagnostics.Stopwatch]::StartNew();$budget=@{nodes=0}
$filtered=Read-DiscoveryView $controlWalker
$raw=Read-DiscoveryView $rawWalker
if ($filtered.nodes -ne 1 -or $filtered.surface.codex -ne 0 -or $raw.nodes -ne 2 -or
    $raw.surface.codex -ne 1 -or $raw.surface.content -ne 1 -or $raw.surface.control -ne 1 -or
    $budget.nodes -ne 3 -or $script:discoveryIdentityReads -ne 3) { throw 'Filtered/raw snapshot or shared budget mismatch.' }
$budget.nodes=2048;$rejected=$false
try { Read-DiscoveryView $rawWalker | Out-Null } catch { $rejected=$true }
if (-not $rejected -or $result.reason -cne 'limit') { throw 'Combined node budget not enforced.' }
if (-not $source.Contains('::RawViewWalker') -or -not $source.Contains('::ControlViewWalker') -or
    -not $source.Contains('$result.controlSurface=$control.surface;$result.rawView=$raw')) { throw 'Both complete views required.' }

# Caller placement and provenance contracts. Parse only, never launch GUI.
$tokens=$null;$errors=$null
$caller=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'package-native-magpie-windows.ps1'),[ref]$tokens,[ref]$errors)
if ($errors.Count) { throw 'Caller syntax invalid.' }
$calls=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
    $n.GetCommandName() -ceq 'Invoke-OriginalConnectDiscovery'},$true))
if ($calls.Count -ne 1 -or $calls[0].Extent.Text -cne
    'Invoke-OriginalConnectDiscovery -OwnedProcess $process -ExpectedExecutable $psi.FileName') { throw 'Exact retained process required.' }
$cursor=$calls[0].Parent;$foundOption=$false;$foundOwnership=$false
while ($null -ne $cursor) {
    if ($cursor -is [Management.Automation.Language.IfStatementAst] -and
        $cursor.Clauses[0].Item1.Extent.Text -ceq '$DiscoverOriginalConnect') { $foundOption=$true }
    if ($cursor -is [Management.Automation.Language.TryStatementAst] -and
        $null -ne $cursor.Finally -and $cursor.Finally.Extent.Text.Contains('$process.Dispose()') -and
        $cursor.Body.Extent.Text.Contains('$process.Start()')) { $foundOwnership=$true }
    $cursor=$cursor.Parent
}
if (-not $foundOption -or -not $foundOwnership) { throw 'Discovery must remain opt-in inside owned launch lifetime.' }
$callerText=$caller.Extent.Text
foreach ($required in @("param([switch]`$DiscoverOriginalConnect)","'[original-connect-discovery-v1]'",
    "'github-hosted'", "'original-connect-discovery.tests.ps1'", "'original-connect-discovery.json'",
    "`$runSettings = Join-Path `$run 'data/settings.json'", "`$settings.lang = 'en'",
    '"lang":"zh"', "nativeActions=0", "controlsAccepted=`$false")) {
    if (-not $callerText.Contains($required)) { throw 'Caller opt-in/isolation contract missing.' }
}
$copyAt=$callerText.IndexOf('Copy-Item -LiteralPath $package -Destination $run -Recurse')
$englishAt=$callerText.IndexOf("`$settings.lang = 'en'")
$startAt=$callerText.IndexOf('$process.Start()')
$captureAt=$callerText.IndexOf("`$bitmap.Save((Join-Path `$evidence 'original-magpie.png')")
if ($copyAt -lt 0 -or $englishAt -le $copyAt -or $startAt -le $englishAt -or
    $calls[0].Extent.StartOffset -le $captureAt) { throw 'Copy/language/launch/capture/discovery ordering changed.' }
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-codex-connectivity-preview.yml'))
foreach ($required in @("contains(github.event.head_commit.message, '[original-connect-discovery-v1]')",
    "github.event_name == 'push'", "'read-only-v1' || ''",'build/original-connect-discovery*.ps1',
    "-DiscoverOriginalConnect:(`$env:ORIGINAL_CONNECT_DISCOVERY -ceq 'true')")) {
    if (-not $workflow.Contains($required)) { throw 'Workflow discovery gate missing.' }
}
Write-Output 'ORIGINAL_CONNECT_DISCOVERY_OFFLINE_TESTS_PASSED'
