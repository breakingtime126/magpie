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
