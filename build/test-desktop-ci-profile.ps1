# Offline, synthetic fixtures only. Import allowlisted helpers, never execute the probe body.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: cold-start syntax' }
foreach ($name in @('Assert-DesktopModelSelectionProfile', 'Assert-DesktopFixtureProfile', 'Assert-DesktopCIOptions', 'Assert-DesktopCompatibilityPackage', 'Assert-Desktop4239Options', 'Assert-Desktop4239Identity', 'Get-DesktopStaticEntries', 'Get-DesktopAccessibilityArguments', 'Assert-DesktopPackageIdentityOptions', 'New-DesktopBridgeSettings',
    'Wait-DesktopRegistration', 'Get-DesktopNoAuthStatusEvidence', 'Assert-PublicShape', 'Assert-PublicInteger',
    'Assert-PublicOwnedCatalogEvent', 'Assert-PublicModelCatalogEvent', 'Assert-PublicThreadListEvent', 'Get-PublicThreadListShapeFields', 'Get-PublicThreadListErrorClasses',
    'Assert-PublicRequestTraceEvent', 'Assert-PublicReadTargetEvent', 'Get-PublicReadTargetLabels', 'Assert-PublicReadRequestEvent', 'Get-DesktopFeatureListEvidence', 'Get-DesktopRPCErrorEvidence', 'Get-DesktopConfigReadEvidence',
    'New-DesktopReadTargetEnvironment', 'Complete-DesktopActivationProcess', 'Get-DesktopNativeRunDecision', 'Get-DesktopCombinedOutcome', 'ConvertTo-DesktopNativeControlEvidence', 'Assert-PublicHash', 'Assert-PublicCanonicalJSON', 'ConvertTo-DesktopReadCallerEvidence', 'ConvertFrom-DesktopReadCallerOutput')) {
    $definitions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($definitions.Count -ne 1) { throw 'FAIL: missing or duplicated CI configuration helper' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
# Load only the wrapper's pure schema validator; never launch a native process here.
$wrapperTokens = $null
$wrapperErrors = $null
$wrapperAst = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'control-plane-native/invoke.ps1'), [ref]$wrapperTokens, [ref]$wrapperErrors)
if ($wrapperErrors.Count -ne 0) { throw 'FAIL: native control wrapper syntax' }
$wrapperValidator = @($wrapperAst.FindAll({
    param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Assert-ControlPlaneNativeSummary'
}, $true))
if ($wrapperValidator.Count -ne 1) { throw 'FAIL: missing or duplicated native summary validator' }
. ([scriptblock]::Create($wrapperValidator[0].Extent.Text))
$script:passed = 0
function Expect-Pass([string]$Name, [scriptblock]$Action) {
    try { $null = & $Action } catch { throw ('FAIL: ' + $Name) }
    $script:passed++
}
function Expect-Reject([string]$Name, [scriptblock]$Action) {
    $rejected = $false
    try { $null = & $Action } catch {
        if ($_.Exception.Message.Contains('PRIVATE_CANARY')) { throw ('FAIL: leaked input in ' + $Name) }
        $rejected = $true
    }
    if (-not $rejected) { throw ('FAIL: accepted ' + $Name) }
    $script:passed++
}
function Assert-Keys($Value, [string[]]$Expected) {
    $actual = @($Value.PSObject.Properties | ForEach-Object { $_.Name } | Sort-Object)
    if (($actual -join '|') -cne (($Expected | Sort-Object) -join '|')) { throw 'Unexpected profile fields.' }
}
function New-Options {
    return @{
        Mode = 'observe'; StartupProbe = $true; LaunchCompatibility = $true; StaticLauncher = $true
        Actions = 'true'; RunnerEnvironment = 'github-hosted'; Repository = 'breakingtime126/magpie'
        Ref = 'refs/heads/experiment/app-server-bridge-v0.1.3035'
    }
}
function New-Backends {
    return @(
        [ordered]@{ id = 'alpha'; provider = 'alpha_provider'; command = 'fixture-codex.exe'; home = 'fixture-alpha-home'; codex_home = 'fixture-alpha-codex' },
        [ordered]@{ id = 'beta'; provider = 'beta_provider'; command = 'fixture-codex.exe'; home = 'fixture-beta-home'; codex_home = 'fixture-beta-codex' }
    )
}
function New-ModelCatalogEvent {
    return ('{"seq":2,"pid":1,"kind":"rpc","direction":"from_bridge","method":"model/list","correlation":1,"class":"response",' +
        '"modelCatalog":{"schema":1,"status":"observed","counts":{"items":2,"alpha":1,"alphaHidden":0,"alphaDefault":1,"beta":1,"betaHidden":0,"betaDefault":0,"unknown":0,"hasMore":false}}}') | ConvertFrom-Json
}
Expect-Pass 'model catalog fixed counts only' { Assert-PublicModelCatalogEvent (New-ModelCatalogEvent) }
Expect-Pass 'legacy event without model catalog unchanged' { Assert-PublicModelCatalogEvent ([pscustomobject]@{kind='rpc'}) }
foreach ($status in @('unavailable', 'limit')) {
    Expect-Pass ('model catalog honest missing evidence ' + $status) {
        $event = New-ModelCatalogEvent; $event.modelCatalog = [pscustomobject]@{schema=1;status=$status}
        Assert-PublicModelCatalogEvent $event
    }
}
Expect-Pass 'model catalog unreviewed tuple is partial' {
    $event = New-ModelCatalogEvent; $event.modelCatalog.status='partial'
    $event.modelCatalog.counts.beta=0; $event.modelCatalog.counts.unknown=1
    Assert-PublicModelCatalogEvent $event
}
Expect-Pass 'model catalog pagination is partial' {
    $event = New-ModelCatalogEvent; $event.modelCatalog.status='partial'; $event.modelCatalog.counts.hasMore=$true
    Assert-PublicModelCatalogEvent $event
}
foreach ($mutation in @(
    {param($e) $e.method='account/read'}, {param($e) $e.direction='to_bridge'},
    {param($e) $e.class='unmatched_response'}, {param($e) $e.correlation=0},
    {param($e) $e.modelCatalog.schema=2}, {param($e) $e.modelCatalog.status='PRIVATE_CANARY'},
    {param($e) $e.modelCatalog.counts.items=33}, {param($e) $e.modelCatalog.counts.items=1},
    {param($e) $e.modelCatalog.counts.alpha=-1}, {param($e) $e.modelCatalog.counts.betaHidden=2},
    {param($e) $e.modelCatalog.counts.alphaDefault='1'}, {param($e) $e.modelCatalog.counts.hasMore=1},
    {param($e) $e.modelCatalog.counts.hasMore=$true}, {param($e) $e.modelCatalog.status='partial'},
    {param($e) $e.modelCatalog.status='unavailable'},
    {param($e) $e.modelCatalog.counts | Add-Member -NotePropertyName raw -NotePropertyValue 'PRIVATE_CANARY'},
    {param($e) $e.modelCatalog | Add-Member -NotePropertyName name -NotePropertyValue 'PRIVATE_CANARY'},
    {param($e) $e | Add-Member -NotePropertyName body -NotePropertyValue 'PRIVATE_CANARY'}
)) {
    Expect-Reject 'model catalog invalid context/shape cannot publish' {
        $event=New-ModelCatalogEvent; & $mutation $event; Assert-PublicModelCatalogEvent $event
    }
}
$version = 'codex-cli 0.167.0-alpha.3.2'
$hash = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
Expect-Pass 'explicit observed compatibility under existing CI guards' {
    $options = New-Options
    Assert-DesktopCIOptions @options
}
foreach ($mode in @('baseline', 'bridge')) {
    Expect-Reject ('compatibility outside observe: ' + $mode) {
        $options = New-Options; $options.Mode = $mode
        Assert-DesktopCIOptions @options
    }
    Expect-Pass ('unchanged no-opt-in mode: ' + $mode) {
        $options = New-Options; $options.Mode = $mode
        $options.StartupProbe = $false; $options.LaunchCompatibility = $false; $options.StaticLauncher = $false
        Assert-DesktopCIOptions @options
    }
}
Expect-Reject 'compatibility without separate startup opt-in' {
    $options = New-Options; $options.StartupProbe = $false
    Assert-DesktopCIOptions @options
}
Expect-Pass 'startup-only observe remains accepted' {
    $options = New-Options; $options.LaunchCompatibility = $false
    Assert-DesktopCIOptions @options
}
foreach ($field in @('Actions', 'RunnerEnvironment', 'Repository', 'Ref')) {
    foreach ($value in @('', 'PRIVATE_CANARY')) {
        Expect-Reject ('unreviewed CI context: ' + $field) {
            $options = New-Options; $options[$field] = $value
            Assert-DesktopCIOptions @options
        }
    }
}
Expect-Reject 'self-hosted runner' {
    $options = New-Options; $options.RunnerEnvironment = 'self-hosted'
    Assert-DesktopCIOptions @options
}
Expect-Reject 'main branch' {
    $options = New-Options; $options.Ref = 'refs/heads/main'
    Assert-DesktopCIOptions @options
}
Expect-Reject 'fork repository' {
    $options = New-Options; $options.Repository = 'another-owner/magpie'
    Assert-DesktopCIOptions @options
}
$packageVersion = '26.1103.3517.0'
$packageHash = 'ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643'
Expect-Pass 'compatibility requires the exact Desktop caller package' {
    Assert-DesktopCompatibilityPackage $true $packageVersion $packageHash
}
foreach ($badVersion in @('', '26.1103.3517.1', 'PRIVATE_CANARY')) {
    Expect-Reject 'mismatching Desktop package version' {
        Assert-DesktopCompatibilityPackage $true $badVersion $packageHash
    }
}
foreach ($badHash in @('', ('0' * 64), $packageHash.ToUpperInvariant(), 'PRIVATE_CANARY')) {
    Expect-Reject 'mismatching Desktop package hash' {
        Assert-DesktopCompatibilityPackage $true $packageVersion $badHash
    }
}
Expect-Pass 'new package gate does not broaden or change startup-only and baseline gates' {
    Assert-DesktopCompatibilityPackage $false '' ''
}
Expect-Pass 'default config has no opt-ins or pins' {
    $settings = New-DesktopBridgeSettings (New-Backends) $false $false '' ''
    $json = $settings | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    Assert-Keys $json @('backends')
    if ($json.backends.Count -ne 2) { throw 'Expected two backends.' }
    foreach ($backend in $json.backends) {
        Assert-Keys $backend @('id', 'provider', 'command', 'home', 'codex_home')
    }
}
Expect-Pass 'startup-only config is not silently broadened' {
    $settings = New-DesktopBridgeSettings (New-Backends) $true $false $version $hash
    $json = $settings | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    Assert-Keys $json @('backends', 'startup_probe')
    if ($json.startup_probe -cne 'compare-unrestricted-v0.167.0-alpha.3.2') { throw 'Wrong startup mode.' }
    foreach ($backend in $json.backends) {
        Assert-Keys $backend @('id', 'provider', 'command', 'home', 'codex_home', 'startup_probe_sha256')
        if ($backend.startup_probe_sha256 -cne $hash) { throw 'Missing exact pin.' }
    }
}
Expect-Pass 'compatibility config contains only reviewed exact modes and both pins' {
    $backends = New-Backends
    $settings = New-DesktopBridgeSettings $backends $true $true $version $hash 'fixture-control-home'
    $json = $settings | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $expected = [ordered]@{
        startup_probe = 'compare-unrestricted-v0.167.0-alpha.3.2'
        desktop_plugin_override = 'codex-app-tools-v0.167.0-alpha.3.2'
        attestation = 'unavailable-v0.167.0-alpha.3.2'
        gateway_oauth = 'primary-read-only-v0.167.0-alpha.3.2'
        gateway_oauth_provider_name = 'CI alpha launch-only'
        config_requirements_read = 'compare-null-v0.167.0-alpha.3.2'
        auth_status = 'primary-no-auth-v0.167.0-alpha.3.2'
        experimental_feature_list = 'compare-v0.167.0-alpha.3.2'
        control_plane = 'routing-v0.167.0-alpha.3.2'
    }
    Assert-Keys $json (@('backends', 'control_home') + @($expected.Keys))
    if ($json.control_home -cne 'fixture-control-home') { throw 'Missing separate bridge-owned home.' }
    foreach ($key in $expected.Keys) {
        if ($json.$key -cne $expected[$key]) { throw 'Wrong exact compatibility mode.' }
    }
    if ($json.backends.Count -ne 2) { throw 'Expected both backends.' }
    for ($index = 0; $index -lt 2; $index++) {
        $backend = $json.backends[$index]
        Assert-Keys $backend @('id', 'provider', 'command', 'home', 'codex_home', 'startup_probe_sha256')
        if ($backend.startup_probe_sha256 -cne $hash) { throw 'Missing exact pin.' }
        foreach ($key in @('id', 'provider', 'command', 'home', 'codex_home')) {
            if ($backend.$key -cne $backends[$index][$key]) { throw 'Backend configuration was changed.' }
        }
        if ($backends[$index].Contains('startup_probe_sha256')) { throw 'Input configuration was mutated.' }
    }
    if ($json.backends[0].home -ceq $json.backends[1].home -or
        $json.backends[0].codex_home -ceq $json.backends[1].codex_home) { throw 'Backend homes must remain separate.' }
}
Expect-Reject 'builder refuses compatibility without startup pinning' {
    New-DesktopBridgeSettings (New-Backends) $false $true $version $hash
}
foreach ($compatibility in @($false, $true)) {
    foreach ($badVersion in @('', 'codex-cli 0.166.0', 'codex-cli 0.167.0-alpha.3.3', 'PRIVATE_CANARY')) {
        Expect-Reject 'nonmatching official version' {
            New-DesktopBridgeSettings (New-Backends) $true $compatibility $badVersion $hash
        }
    }
    foreach ($badHash in @('', ('0' * 64), $hash.ToUpperInvariant(), 'PRIVATE_CANARY')) {
        Expect-Reject 'nonmatching executable hash' {
            New-DesktopBridgeSettings (New-Backends) $true $compatibility $version $badHash
        }
    }
    foreach ($count in @(0, 1, 3)) {
        Expect-Reject 'profile requires both exact backends' {
            $all = @(New-Backends) + @([ordered]@{ id = 'extra' })
            $subset = @($all | Select-Object -First $count)
            New-DesktopBridgeSettings $subset $true $compatibility $version $hash
        }
    }
}

# Synthetic metadata only. The actual result body never enters these fixtures.
function New-NoAuthRequest([int]$PidValue, [int]$Correlation, [bool]$IncludeToken) {
    return [pscustomobject]@{
        kind = 'rpc'; pid = $PidValue; correlation = $Correlation; direction = 'to_bridge'
        method = 'getAuthStatus'; class = 'request'; authParamsValid = $true
        includeTokenRequested = $IncludeToken; refreshTokenRequested = $false
    }
}
function New-NoAuthResponse([int]$PidValue, [int]$Correlation) {
    return [pscustomobject]@{
        kind = 'rpc'; pid = $PidValue; correlation = $Correlation; direction = 'from_bridge'
        method = 'getAuthStatus'; class = 'response'
    }
}
Expect-Pass 'native no-auth evidence includes unchanged token-request intent' {
    $records = @(
        (New-NoAuthRequest 1 1 $false), (New-NoAuthRequest 1 2 $true),
        (New-NoAuthResponse 1 2), (New-NoAuthResponse 1 1),
        (New-NoAuthRequest 2 1 $true), (New-NoAuthResponse 2 1)
    )
    $evidence = Get-DesktopNoAuthStatusEvidence $records
    if ($evidence.requests -ne 3 -or $evidence.validatedResponses -ne 3 -or
        $evidence.includeTokenRequests -ne 2 -or $evidence.includeTokenValidatedResponses -ne 2) {
        throw 'Wrong no-auth evidence counts.'
    }
}
Expect-Pass 'correlated native auth RPC failure is not mislabeled missing correlation' {
    $request=New-NoAuthRequest 8648 88 $true
    $failure=[pscustomobject]@{seq=183;pid=8648;kind='rpc_error';direction='from_bridge';method='getAuthStatus';correlation=88;code=-32000;class='unknown'}
    $message=''
    try { $null=Get-DesktopNoAuthStatusEvidence @($request,$failure) } catch { $message=$_.Exception.Message }
    if ($message -cne 'No-auth status received a correlated RPC error (code -32000).') { throw 'Correlated auth RPC error was hidden or converted into success.' }
    foreach ($field in @('raw','message','data')) {
        $bad=($failure | ConvertTo-Json -Compress) | ConvertFrom-Json
        $bad | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Expect-Reject 'auth RPC failure rejects unsanitized extra fields' { Get-DesktopNoAuthStatusEvidence @($request,$bad) }
    }
    $failure.correlation=89
    $message=''
    try { $null=Get-DesktopNoAuthStatusEvidence @($request,$failure) } catch { $message=$_.Exception.Message }
    if ($message -cne 'No-auth status evidence has an unmatched RPC error.') { throw 'Wrong request auth error was treated as correlated.' }
}
Expect-Reject 'no observed native status requests' { Get-DesktopNoAuthStatusEvidence @() }
Expect-Reject 'only false includeToken does not verify actual Desktop requirement' {
    Get-DesktopNoAuthStatusEvidence @((New-NoAuthRequest 1 1 $false), (New-NoAuthResponse 1 1))
}
Expect-Reject 'outstanding native response' { Get-DesktopNoAuthStatusEvidence @((New-NoAuthRequest 1 1 $true)) }
Expect-Reject 'unmatched native response' { Get-DesktopNoAuthStatusEvidence @((New-NoAuthResponse 1 1)) }
Expect-Reject 'duplicate request correlation' {
    Get-DesktopNoAuthStatusEvidence @((New-NoAuthRequest 1 1 $true), (New-NoAuthRequest 1 1 $true))
}
Expect-Reject 'reused completed correlation' {
    Get-DesktopNoAuthStatusEvidence @((New-NoAuthRequest 1 1 $true), (New-NoAuthResponse 1 1),
        (New-NoAuthRequest 1 1 $true), (New-NoAuthResponse 1 1))
}
Expect-Reject 'response from another process' {
    Get-DesktopNoAuthStatusEvidence @((New-NoAuthRequest 1 1 $true), (New-NoAuthResponse 2 1))
}
foreach ($class in @('unsupported_method', 'unknown', 'duplicate_id', 'correlation_limit')) {
    Expect-Reject ('no-auth failure class: ' + $class) {
        $response = New-NoAuthResponse 1 1; $response.kind = 'rpc_error'; $response.class = $class
        Get-DesktopNoAuthStatusEvidence @((New-NoAuthRequest 1 1 $true), $response)
    }
}
foreach ($field in @('authParamsValid', 'includeTokenRequested', 'refreshTokenRequested', 'kind', 'pid', 'correlation')) {
    Expect-Reject ('no-auth missing field: ' + $field) {
        $request = New-NoAuthRequest 1 1 $true; $request.PSObject.Properties.Remove($field)
        Get-DesktopNoAuthStatusEvidence @($request, (New-NoAuthResponse 1 1))
    }
}
foreach ($field in @('authParamsValid', 'includeTokenRequested', 'refreshTokenRequested')) {
    Expect-Reject ('no-auth nonboolean field: ' + $field) {
        $request = New-NoAuthRequest 1 1 $true; $request.$field = 'PRIVATE_CANARY'
        Get-DesktopNoAuthStatusEvidence @($request, (New-NoAuthResponse 1 1))
    }
}
Expect-Reject 'invalid params' {
    $request = New-NoAuthRequest 1 1 $true; $request.authParamsValid = $false
    Get-DesktopNoAuthStatusEvidence @($request, (New-NoAuthResponse 1 1))
}
Expect-Reject 'explicit refresh remains outside diagnostic scope' {
    $request = New-NoAuthRequest 1 1 $true; $request.refreshTokenRequested = $true
    Get-DesktopNoAuthStatusEvidence @($request, (New-NoAuthResponse 1 1))
}
Expect-Reject 'other-method correlation saturation precludes no-auth evidence' {
    Get-DesktopNoAuthStatusEvidence @(
        (New-NoAuthRequest 1 1 $true), (New-NoAuthResponse 1 1),
        [pscustomobject]@{ class = 'correlation_limit'; method = 'model/list' }
    )
}

# Feature fixtures contain only the existing public metadata shape, never a page body.
# The production comparator, not this observer evidence, validates whole-page equality.
function New-FeatureRequest([int]$PidValue, [int]$Correlation, [bool]$Limit, [bool]$Cursor = $false) {
    return [pscustomobject]@{
        seq = 1; pid = $PidValue; kind = 'rpc'; direction = 'to_bridge'
        method = 'experimentalFeature/list'; correlation = $Correlation; class = 'request'
        tracePresent = $true; traceValid = $true; featureListParamsValid = $true
        featureListCursorProvided = $Cursor; featureListLimitProvided = $Limit
        featureListThreadIdProvided = $false
    }
}
function New-FeatureResponse([int]$PidValue, [int]$Correlation) {
    return [pscustomobject]@{
        seq = 2; pid = $PidValue; kind = 'rpc'; direction = 'from_bridge'
        method = 'experimentalFeature/list'; correlation = $Correlation; class = 'response'
    }
}
Expect-Pass 'native feature evidence counts limits and preserves cursor-present pages across processes' {
    $records = @(
        (New-FeatureRequest 1 1 $false), (New-FeatureRequest 1 2 $true $true),
        (New-FeatureResponse 1 2), (New-FeatureResponse 1 1),
        (New-FeatureRequest 2 1 $true), (New-FeatureResponse 2 1)
    )
    for ($index = 0; $index -lt $records.Count; $index++) { $records[$index].seq = $index + 1 }
    $evidence = Get-DesktopFeatureListEvidence $records
    Assert-Keys ([pscustomobject]$evidence) @('requests', 'validatedResponses', 'limitRequests',
        'limitValidatedResponses', 'cursorRequests', 'cursorValidatedResponses')
    if ($evidence.requests -ne 3 -or $evidence.validatedResponses -ne 3 -or
        $evidence.limitRequests -ne 2 -or $evidence.limitValidatedResponses -ne 2 -or
        $evidence.cursorRequests -ne 1 -or $evidence.cursorValidatedResponses -ne 1) {
        throw 'Wrong feature-list evidence counts.'
    }
}
Expect-Pass 'valid feature request without optional trace metadata' {
    $request = New-FeatureRequest 1 1 $true
    $request.PSObject.Properties.Remove('tracePresent'); $request.PSObject.Properties.Remove('traceValid')
    $evidence = Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    if ($evidence.requests -ne 1 -or $evidence.cursorRequests -ne 0) { throw 'Wrong feature counts.' }
}
Expect-Pass 'cursor-present page without limit remains supported beside a limit-present request' {
    $evidence = Get-DesktopFeatureListEvidence @(
        (New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 1),
        (New-FeatureRequest 1 2 $false $true), (New-FeatureResponse 1 2)
    )
    if ($evidence.limitValidatedResponses -ne 1 -or $evidence.cursorValidatedResponses -ne 1) {
        throw 'Pagination presence was not preserved.'
    }
}
Expect-Pass 'ordinary unrelated sanitized events do not count as feature evidence' {
    $evidence = Get-DesktopFeatureListEvidence @(
        [pscustomobject]@{ seq = 1; pid = 1; kind = 'child_start' },
        (New-NoAuthRequest 1 2 $true), (New-NoAuthResponse 1 2),
        (New-FeatureRequest 1 3 $true), (New-FeatureResponse 1 3)
    )
    if ($evidence.requests -ne 1 -or $evidence.validatedResponses -ne 1) { throw 'Counted unrelated events.' }
}
Expect-Reject 'no observed native feature request' { Get-DesktopFeatureListEvidence @() }
Expect-Reject 'only absent or null limit does not cover actual Desktop request shape' {
    Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $false), (New-FeatureResponse 1 1))
}
Expect-Reject 'outstanding feature reply' { Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true)) }
Expect-Reject 'unmatched feature reply' { Get-DesktopFeatureListEvidence @((New-FeatureResponse 1 1)) }
Expect-Reject 'feature reply before its request' {
    Get-DesktopFeatureListEvidence @((New-FeatureResponse 1 1), (New-FeatureRequest 1 1 $true))
}
Expect-Reject 'duplicate feature request correlation' {
    Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), (New-FeatureRequest 1 1 $true))
}
Expect-Reject 'reused completed feature correlation' {
    Get-DesktopFeatureListEvidence @(
        (New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 1),
        (New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 1)
    )
}
Expect-Reject 'duplicate completed feature reply' {
    Get-DesktopFeatureListEvidence @(
        (New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 1), (New-FeatureResponse 1 1)
    )
}
Expect-Reject 'feature response from another process' {
    Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), (New-FeatureResponse 2 1))
}
Expect-Reject 'feature response has another correlation' {
    Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 2))
}
foreach ($field in @('featureListParamsValid', 'featureListCursorProvided', 'featureListLimitProvided',
    'featureListThreadIdProvided', 'seq', 'pid', 'correlation', 'kind', 'direction', 'method', 'class')) {
    Expect-Reject ('feature request missing field: ' + $field) {
        $request = New-FeatureRequest 1 1 $true; $request.PSObject.Properties.Remove($field)
        Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    }
}
foreach ($field in @('featureListParamsValid', 'featureListCursorProvided',
    'featureListLimitProvided', 'featureListThreadIdProvided', 'tracePresent', 'traceValid')) {
    foreach ($badValue in @('PRIVATE_CANARY', 0, $null)) {
        Expect-Reject ('feature request nonboolean field: ' + $field) {
            $request = New-FeatureRequest 1 1 $true; $request.$field = $badValue
            Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
        }
    }
}
Expect-Reject 'invalid feature params cannot become absent flags' {
    $request = New-FeatureRequest 1 1 $true; $request.featureListParamsValid = $false
    foreach ($field in @('featureListCursorProvided', 'featureListLimitProvided', 'featureListThreadIdProvided')) {
        $request.PSObject.Properties.Remove($field)
    }
    Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
}
Expect-Reject 'non-null native thread scope remains unsupported' {
    $request = New-FeatureRequest 1 1 $true; $request.featureListThreadIdProvided = $true
    Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
}
foreach ($field in @('tracePresent', 'traceValid')) {
    Expect-Reject ('incomplete feature trace metadata: ' + $field) {
        $request = New-FeatureRequest 1 1 $true; $request.PSObject.Properties.Remove($field)
        Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    }
    Expect-Reject ('false feature trace metadata: ' + $field) {
        $request = New-FeatureRequest 1 1 $true; $request.$field = $false
        Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    }
}
Expect-Reject 'valid sanitized invalid-trace event lacks admissible request shape' {
    $request = New-FeatureRequest 1 1 $true; $request.traceValid = $false
    foreach ($field in @('featureListParamsValid', 'featureListCursorProvided',
        'featureListLimitProvided', 'featureListThreadIdProvided')) {
        $request.PSObject.Properties.Remove($field)
    }
    Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
}
foreach ($field in @('seq', 'pid', 'correlation')) {
    foreach ($badValue in @(0, -1, 4294967297L, 1.5, 'PRIVATE_CANARY')) {
        Expect-Reject ('invalid feature request correlation field: ' + $field) {
            $request = New-FeatureRequest 1 1 $true; $request.$field = $badValue
            Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
        }
        Expect-Reject ('invalid feature response correlation field: ' + $field) {
            $response = New-FeatureResponse 1 1; $response.$field = $badValue
            Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
        }
    }
}
foreach ($field in @('seq', 'pid', 'correlation', 'kind', 'direction', 'method', 'class')) {
    Expect-Reject ('feature response missing field: ' + $field) {
        $response = New-FeatureResponse 1 1; $response.PSObject.Properties.Remove($field)
        Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
    }
}
foreach ($method in @('config/read', 'experimentalFeature/List', 'experimentalFeature/list/extra', 'unknown')) {
    Expect-Reject ('wrong feature request method: ' + $method) {
        $request = New-FeatureRequest 1 1 $true; $request.method = $method
        Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    }
    Expect-Reject ('wrong feature response method: ' + $method) {
        $response = New-FeatureResponse 1 1; $response.method = $method
        Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
    }
}
foreach ($direction in @('from_bridge', 'stderr', 'TO_BRIDGE')) {
    Expect-Reject ('wrong feature request direction: ' + $direction) {
        $request = New-FeatureRequest 1 1 $true; $request.direction = $direction
        Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    }
}
foreach ($direction in @('to_bridge', 'stderr', 'FROM_BRIDGE')) {
    Expect-Reject ('wrong feature response direction: ' + $direction) {
        $response = New-FeatureResponse 1 1; $response.direction = $direction
        Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
    }
}
foreach ($class in @('unsupported_method', 'unknown', 'duplicate_id', 'correlation_limit')) {
    Expect-Reject ('feature native error class: ' + $class) {
        $response = New-FeatureResponse 1 1; $response.kind = 'rpc_error'; $response.class = $class
        Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
    }
}
foreach ($class in @('request', 'notification', 'unknown', 'Response')) {
    Expect-Reject ('feature nonsuccess response class: ' + $class) {
        $response = New-FeatureResponse 1 1; $response.class = $class
        Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
    }
}
foreach ($class in @('duplicate_id', 'correlation_limit')) {
    Expect-Reject ('unrelated method ambiguous correlation fails feature evidence: ' + $class) {
        Get-DesktopFeatureListEvidence @(
            (New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 1),
            [pscustomobject]@{ class = $class; method = 'model/list' }
        )
    }
}
foreach ($field in @('cursor', 'limit', 'threadId', 'result', 'code', 'authParamsValid')) {
    Expect-Reject ('feature request refuses non-shape data: ' + $field) {
        $request = New-FeatureRequest 1 1 $true
        $request | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Get-DesktopFeatureListEvidence @($request, (New-FeatureResponse 1 1))
    }
    Expect-Reject ('feature response refuses data beyond metadata: ' + $field) {
        $response = New-FeatureResponse 1 1
        $response | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Get-DesktopFeatureListEvidence @((New-FeatureRequest 1 1 $true), $response)
    }
}
# The published summary stops at 256 records; evidence must consume the full snapshot.
$prefix = @((New-FeatureRequest 1 1 $true), (New-FeatureResponse 1 1))
for ($index = 3; $index -le 300; $index++) {
    $prefix += [pscustomobject]@{ seq = $index; pid = 1; kind = 'stream_end'; direction = 'stderr'; class = 'eof' }
}
Expect-Pass 'feature requests after the summary cutoff are still counted' {
    $request = New-FeatureRequest 1 2 $true $true; $request.seq = 301
    $response = New-FeatureResponse 1 2; $response.seq = 302
    $evidence = Get-DesktopFeatureListEvidence ($prefix + @($request, $response))
    if ($evidence.requests -ne 2 -or $evidence.cursorValidatedResponses -ne 1) { throw 'Truncated evidence.' }
}
Expect-Reject 'outstanding feature request beyond the summary cutoff fails' {
    $request = New-FeatureRequest 1 2 $true $true; $request.seq = 301
    Get-DesktopFeatureListEvidence ($prefix + @($request))
}
Expect-Reject 'native feature error beyond the summary cutoff fails' {
    $request = New-FeatureRequest 1 2 $true; $request.seq = 301
    $response = New-FeatureResponse 1 2; $response.seq = 302; $response.kind = 'rpc_error'; $response.class = 'unknown'
    Get-DesktopFeatureListEvidence ($prefix + @($request, $response))
}
Expect-Pass 'runtime feature evidence consumes the full sanitized snapshot once' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Get-DesktopFeatureListEvidence'
    }, $true))
    if ($calls.Count -ne 1 -or $calls[0].CommandElements.Count -ne 2 -or
        $calls[0].CommandElements[1] -isnot [Management.Automation.Language.VariableExpressionAst] -or
        $calls[0].CommandElements[1].VariablePath.UserPath -cne 'allEvents') {
        throw 'Feature evidence must consume the full sanitized snapshot.'
    }
}

# Error totals are a counts-only account of the full capture, not successful API coverage.
function New-RPCErrorEvent {
    return [pscustomobject]@{
        seq = 1; pid = 1; kind = 'rpc_error'; direction = 'from_bridge'
        method = 'project/list'; correlation = 1; code = -32601; class = 'unsupported_method'
    }
}
$fixturePublicMethods = @('thread/list', 'project/list', 'fs/readFile', 'config/read', 'unknown')
Expect-Pass 'full-snapshot RPC errors retain unsupported and other errors after preview cutoff' {
    $events = @()
    for ($index = 1; $index -le 300; $index++) {
        $events += [pscustomobject]@{ seq = $index; pid = 1; kind = 'stream_end'; direction = 'stderr'; class = 'eof' }
    }
    $unsupported = New-RPCErrorEvent; $unsupported.seq = 301
    $unmatched = New-RPCErrorEvent; $unmatched.seq = 302; $unmatched.class = 'unknown'
    foreach ($field in @('method', 'correlation', 'code')) { $unmatched.PSObject.Properties.Remove($field) }
    $evidence = Get-DesktopRPCErrorEvidence ($events + @($unsupported, $unmatched)) $fixturePublicMethods
    Assert-Keys ([pscustomobject]$evidence) @('rpcErrors', 'unsupportedMethodErrors', 'otherRpcErrors', 'threadListErrors')
    if ($evidence.rpcErrors -ne 2 -or $evidence.unsupportedMethodErrors -ne 1 -or $evidence.otherRpcErrors -ne 1) {
        throw 'RPC error counts were truncated or changed.'
    }
}
foreach ($field in @('class', 'direction', 'method')) {
    foreach ($bad in @('PRIVATE_CANARY', 0, $null)) {
        Expect-Reject ('RPC-error summary refuses unknown labels and wrong types: ' + $field) {
            $event = New-RPCErrorEvent; $event.$field = $bad
            Get-DesktopRPCErrorEvidence @($event) $fixturePublicMethods
        }
    }
}
foreach ($field in @('seq', 'pid', 'correlation', 'code')) {
    foreach ($bad in @('PRIVATE_CANARY', 1.5, 4294967297L, $null)) {
        Expect-Reject ('RPC-error summary requires bounded native integers: ' + $field) {
            $event = New-RPCErrorEvent; $event.$field = $bad
            Get-DesktopRPCErrorEvidence @($event) $fixturePublicMethods
        }
    }
}
foreach ($field in @('method', 'correlation')) {
    Expect-Reject ('RPC-error summary cannot keep half a correlation: ' + $field) {
        $event = New-RPCErrorEvent; $event.PSObject.Properties.Remove($field)
        Get-DesktopRPCErrorEvidence @($event) $fixturePublicMethods
    }
}
foreach ($field in @('result', 'message', 'config', 'path')) {
    Expect-Reject ('RPC-error summary refuses payloads: ' + $field) {
        $event = New-RPCErrorEvent
        $event | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Get-DesktopRPCErrorEvidence @($event) $fixturePublicMethods
    }
}
Expect-Pass 'runtime error summary precedes strict profile validators and consumes full snapshot' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Get-DesktopRPCErrorEvidence'
    }, $true))
    if ($calls.Count -ne 1 -or $calls[0].CommandElements.Count -ne 3 -or
        $calls[0].CommandElements[1] -isnot [Management.Automation.Language.VariableExpressionAst] -or
        $calls[0].CommandElements[1].VariablePath.UserPath -cne 'allEvents' -or
        $calls[0].CommandElements[2] -isnot [Management.Automation.Language.VariableExpressionAst] -or
        $calls[0].CommandElements[2].VariablePath.UserPath -cne 'methods') {
        throw 'RPC-error summary must consume the full snapshot and pinned public method vocabulary.'
    }
    foreach ($name in @('Get-DesktopNoAuthStatusEvidence', 'Get-DesktopFeatureListEvidence', 'Get-DesktopConfigReadEvidence')) {
        $validators = @($ast.FindAll({
            param($node)
            $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq $name
        }, $true))
        if ($validators.Count -ne 1 -or $calls[0].Extent.EndOffset -ge $validators[0].Extent.StartOffset) {
            throw 'A strict compatibility failure can hide the full RPC-error summary.'
        }
    }
}

# The fixed rejection buckets are diagnostic counts, never an acceptance gate.
Expect-Pass 'thread-list buckets cover only correlated from-bridge errors in the full snapshot' {
    $events = @()
    for ($index = 1; $index -le 300; $index++) {
        $events += [pscustomobject]@{ seq = $index; pid = 1; kind = 'stream_end'; direction = 'stderr'; class = 'eof' }
    }
    $labels = @(Get-PublicThreadListErrorClasses) + @('unknown', 'unsupported_method')
    foreach ($label in $labels) {
        $event = New-RPCErrorEvent
        $event.seq = 301 + $events.Count; $event.method = 'thread/list'; $event.code = -32602; $event.class = $label
        $events += $event
    }
    $unmatched = New-RPCErrorEvent; $unmatched.class = 'unknown'
    foreach ($field in @('method', 'correlation')) { $unmatched.PSObject.Properties.Remove($field) }
    $events += $unmatched
    $wrongDirection = New-RPCErrorEvent; $wrongDirection.method = 'thread/list'
    $wrongDirection.direction = 'to_bridge'; $wrongDirection.class = 'unknown'; $events += $wrongDirection
    $evidence = Get-DesktopRPCErrorEvidence $events $fixturePublicMethods
    $expectedKeys = @(Get-PublicThreadListErrorClasses) + @('unknown', 'other')
    Assert-Keys ([pscustomobject]$evidence.threadListErrors) $expectedKeys
    foreach ($label in $expectedKeys) {
        if ($evidence.threadListErrors[$label] -isnot [int] -or $evidence.threadListErrors[$label] -ne 1) {
            throw 'Thread-list fixed rejection buckets were truncated or broadened.'
        }
    }
    if ($evidence.rpcErrors -ne 12 -or $evidence.unsupportedMethodErrors -ne 1 -or $evidence.otherRpcErrors -ne 11) {
        throw 'Thread-list buckets changed the existing total counters.'
    }
}
foreach ($label in @(Get-PublicThreadListErrorClasses)) {
    foreach ($row in @(@('method', 'project/list'), @('direction', 'to_bridge'), @('kind', 'rpc'),
        @('code', -32000), @('code', '-32602'), @('code', $null))) {
        Expect-Reject 'thread-list rejection labels require their exact error scope and code' {
            $event = New-RPCErrorEvent; $event.method = 'thread/list'; $event.code = -32602; $event.class = $label
            $event.($row[0]) = $row[1]
            Assert-PublicThreadListEvent $event
        }
    }
    foreach ($variant in @($label.ToUpperInvariant(), ($label + '_PRIVATE_CANARY'), ('PRIVATE_CANARY_' + $label))) {
        Expect-Reject 'thread-list rejection labels have no casing or prefix aliases' {
            $event = New-RPCErrorEvent; $event.method = 'thread/list'; $event.code = -32602; $event.class = $variant
            Get-DesktopRPCErrorEvidence @($event) $fixturePublicMethods
        }
    }
    foreach ($field in @('method', 'correlation', 'code')) {
        Expect-Reject 'thread-list rejection cannot omit required correlation or code' {
            $event = New-RPCErrorEvent; $event.method = 'thread/list'; $event.code = -32602; $event.class = $label
            $event.PSObject.Properties.Remove($field)
            Get-DesktopRPCErrorEvidence @($event) $fixturePublicMethods
        }
    }
}
function New-ThreadListShapeEvent {
    return [pscustomobject][ordered]@{
        seq = 1; pid = 1; kind = 'rpc'; direction = 'to_bridge'; method = 'thread/list'; correlation = 1; class = 'request'
        threadListProjectIdShape = 'absent'; threadListParentIdShape = 'absent'
        threadListAncestorIdShape = 'absent'; threadListAttachedIdShape = 'absent'
        threadListSectionIdShape = 'absent'; threadListSortKey = 'absent'
    }
}
foreach ($field in @(Get-PublicThreadListShapeFields)) {
    foreach ($label in @('absent', 'null', 'string', 'invalid')) {
        Expect-Pass 'thread-list field types remain distinct fixed labels' {
            $event = New-ThreadListShapeEvent; $event.$field = $label
            if (@(Assert-PublicReadRequestEvent $event).Count -ne 0) { throw 'Thread-list validator emitted output.' }
        }
    }
    foreach ($value in @($null, $false, 0, @('null'), [pscustomobject]@{ value = 'null' },
        'PRIVATE_CANARY', 'NULL', 'null_PRIVATE_CANARY', '')) {
        Expect-Reject 'thread-list shapes refuse values, wrong types and casing aliases' {
            $event = New-ThreadListShapeEvent; $event.$field = $value
            Assert-PublicReadRequestEvent $event
        }
    }
    Expect-Reject 'thread-list shapes require all five fields' {
        $event = New-ThreadListShapeEvent; $event.PSObject.Properties.Remove($field)
        Assert-PublicReadRequestEvent $event
    }
    Expect-Reject 'thread-list shape field names are case sensitive' {
        $event = New-ThreadListShapeEvent; $event.PSObject.Properties.Remove($field)
        $event | Add-Member -NotePropertyName $field.ToUpperInvariant() -NotePropertyValue 'null'
        Assert-PublicReadRequestEvent $event
    }
}
foreach ($row in @(@('method', 'project/list'), @('method', 'thread/List'), @('direction', 'from_bridge'),
    @('kind', 'rpc_error'), @('class', 'response'), @('class', 'notification'), @('class', 'duplicate_id'),
    @('class', 'correlation_limit'), @('class', 'thread_list_project_id'), @('correlation', 0), @('pid', $null))) {
    Expect-Reject 'thread-list shapes cannot escape exact client request scope' {
        $event = New-ThreadListShapeEvent; $event.($row[0]) = $row[1]
        Assert-PublicReadRequestEvent $event
    }
}
foreach ($field in @('projectId', 'sectionId', 'sortKey', 'message', 'params', 'modelProviders', 'tracePresent', 'code', 'configParamsValid')) {
    Expect-Reject 'thread-list shapes cannot carry raw values or mixed metadata families' {
        $event = New-ThreadListShapeEvent
        $event | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Assert-PublicReadRequestEvent $event
    }
}
foreach ($label in @('absent', 'null', 'created_at', 'updated_at', 'recency_at', 'section_position', 'other-string', 'invalid')) {
    foreach ($sectionShape in @('absent', 'null', 'string', 'invalid')) {
        Expect-Pass 'thread-list sort labels and section shapes are observations, not acceptance rules' {
            $event = New-ThreadListShapeEvent; $event.threadListSortKey = $label; $event.threadListSectionIdShape = $sectionShape
            if (@(Assert-PublicReadRequestEvent $event).Count -ne 0) { throw 'Thread-list validator emitted output.' }
        }
    }
}
foreach ($value in @($null, $false, 0, @('created_at'), [pscustomobject]@{ value = 'created_at' },
    'string', 'PRIVATE_CANARY', 'SECTION_POSITION', 'section_position_PRIVATE_CANARY', 'other_string', '')) {
    Expect-Reject 'thread-list sort metadata rejects raw values, wrong types and aliases' {
        $event = New-ThreadListShapeEvent; $event.threadListSortKey = $value
        Assert-PublicReadRequestEvent $event
    }
}
Expect-Reject 'thread-list sort metadata is required alongside all shape fields' {
    $event = New-ThreadListShapeEvent; $event.PSObject.Properties.Remove('threadListSortKey')
    Assert-PublicReadRequestEvent $event
}
Expect-Reject 'thread-list sort field name is case sensitive' {
    $event = New-ThreadListShapeEvent; $event.PSObject.Properties.Remove('threadListSortKey')
    $event | Add-Member -NotePropertyName ThreadListSortKey -NotePropertyValue 'created_at'
    Assert-PublicReadRequestEvent $event
}
Expect-Reject 'thread-list sort label alone is not a complete metadata event' {
    $event = New-ThreadListShapeEvent
    foreach ($field in @(Get-PublicThreadListShapeFields)) { $event.PSObject.Properties.Remove($field) }
    Assert-PublicReadRequestEvent $event
}
# Configuration fixtures contain only shape flags, never native config or result bodies.
function New-ConfigReadRequest([int]$PidValue, [int]$Correlation, [bool]$IncludeLayers = $false) {
    return [pscustomobject]@{
        seq = 1; pid = $PidValue; kind = 'rpc'; direction = 'to_bridge'
        method = 'config/read'; correlation = $Correlation; class = 'request'
        tracePresent = $true; traceValid = $true; configParamsValid = $true
        configIncludeLayers = $IncludeLayers; configCwdProvided = $false
    }
}
function New-ConfigReadResponse([int]$PidValue, [int]$Correlation) {
    return [pscustomobject]@{
        seq = 2; pid = $PidValue; kind = 'rpc'; direction = 'from_bridge'
        method = 'config/read'; correlation = $Correlation; class = 'response'
    }
}
Expect-Pass 'bridge-owned config read counts interleaved replies across processes' {
    $evidence = Get-DesktopConfigReadEvidence @(
        (New-ConfigReadRequest 1 1), (New-ConfigReadRequest 1 2),
        (New-ConfigReadResponse 1 2), (New-ConfigReadResponse 1 1),
        (New-ConfigReadRequest 2 1), (New-ConfigReadResponse 2 1)
    )
    Assert-Keys ([pscustomobject]$evidence) @('requests', 'validatedResponses', 'layerRequests',
        'layerValidatedResponses', 'withoutLayerRequests', 'withoutLayerValidatedResponses')
    if ($evidence.requests -ne 3 -or $evidence.validatedResponses -ne 3 -or
        $evidence.layerRequests -ne 0 -or $evidence.layerValidatedResponses -ne 0 -or
        $evidence.withoutLayerRequests -ne 3 -or $evidence.withoutLayerValidatedResponses -ne 3) {
        throw 'Wrong config-read counts.'
    }
}
Expect-Pass 'valid config read without optional trace metadata' {
    $request = New-ConfigReadRequest 1 1
    $request.PSObject.Properties.Remove('tracePresent'); $request.PSObject.Properties.Remove('traceValid')
    $evidence = Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    if ($evidence.requests -ne 1 -or $evidence.validatedResponses -ne 1) { throw 'Wrong config-read counts.' }
}
Expect-Pass 'ordinary unrelated sanitized events do not count as config-read evidence' {
    $evidence = Get-DesktopConfigReadEvidence @(
        [pscustomobject]@{ seq = 1; pid = 1; kind = 'child_start' },
        (New-NoAuthRequest 1 2 $true), (New-NoAuthResponse 1 2),
        (New-ConfigReadRequest 1 3), (New-ConfigReadResponse 1 3)
    )
    if ($evidence.requests -ne 1 -or $evidence.validatedResponses -ne 1) { throw 'Counted unrelated events.' }
}
Expect-Pass 'layer requests and replies stay paired across interleaved processes' {
    $evidence = Get-DesktopConfigReadEvidence @(
        (New-ConfigReadRequest 1 1 $true), (New-ConfigReadRequest 1 2),
        (New-ConfigReadResponse 1 2), (New-ConfigReadResponse 1 1),
        (New-ConfigReadRequest 2 1 $true), (New-ConfigReadResponse 2 1)
    ) -RequireLayers
    Assert-Keys ([pscustomobject]$evidence) @('requests', 'validatedResponses', 'layerRequests',
        'layerValidatedResponses', 'withoutLayerRequests', 'withoutLayerValidatedResponses')
    if ($evidence.requests -ne 3 -or $evidence.validatedResponses -ne 3 -or
        $evidence.layerRequests -ne 2 -or $evidence.layerValidatedResponses -ne 2 -or
        $evidence.withoutLayerRequests -ne 1 -or $evidence.withoutLayerValidatedResponses -ne 1) {
        throw 'Config-read layer counts were not correlated.'
    }
}
Expect-Pass 'global layer request succeeds without optional trace metadata' {
    $request = New-ConfigReadRequest 1 1 $true
    $request.PSObject.Properties.Remove('tracePresent'); $request.PSObject.Properties.Remove('traceValid')
    $evidence = Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1)) -RequireLayers
    if ($evidence.requests -ne 1 -or $evidence.validatedResponses -ne 1 -or
        $evidence.layerRequests -ne 1 -or $evidence.layerValidatedResponses -ne 1 -or
        $evidence.withoutLayerRequests -ne 0 -or $evidence.withoutLayerValidatedResponses -ne 0) {
        throw 'Wrong layer-only config-read counts.'
    }
}
Expect-Reject 'reviewed profile cannot claim layers from only layers-disabled requests' {
    Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1)) -RequireLayers
}
Expect-Reject 'no observed native config-read request' { Get-DesktopConfigReadEvidence @() }
Expect-Reject 'outstanding config-read reply' { Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1)) }
Expect-Reject 'unmatched config-read reply' { Get-DesktopConfigReadEvidence @((New-ConfigReadResponse 1 1)) }
Expect-Reject 'config-read reply before its request' {
    Get-DesktopConfigReadEvidence @((New-ConfigReadResponse 1 1), (New-ConfigReadRequest 1 1))
}
Expect-Reject 'duplicate config-read request correlation' {
    Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), (New-ConfigReadRequest 1 1))
}
Expect-Reject 'reused completed config-read correlation' {
    Get-DesktopConfigReadEvidence @(
        (New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1),
        (New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1)
    )
}
Expect-Reject 'duplicate completed config-read reply' {
    Get-DesktopConfigReadEvidence @(
        (New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1), (New-ConfigReadResponse 1 1)
    )
}
Expect-Reject 'config-read response from another process' {
    Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), (New-ConfigReadResponse 2 1))
}
Expect-Reject 'config-read response has another correlation' {
    Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 2))
}
foreach ($field in @('configParamsValid', 'configIncludeLayers', 'configCwdProvided', 'seq', 'pid', 'correlation', 'kind', 'direction', 'method', 'class')) {
    Expect-Reject ('config-read request missing field: ' + $field) {
        $request = New-ConfigReadRequest 1 1; $request.PSObject.Properties.Remove($field)
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
}
foreach ($field in @('configParamsValid', 'configIncludeLayers', 'configCwdProvided', 'tracePresent', 'traceValid')) {
    foreach ($badValue in @('PRIVATE_CANARY', 0, $null)) {
        Expect-Reject ('config-read request nonboolean field: ' + $field) {
            $request = New-ConfigReadRequest 1 1; $request.$field = $badValue
            Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
        }
    }
}
Expect-Reject 'invalid config-read params cannot become absent flags' {
    $request = New-ConfigReadRequest 1 1; $request.configParamsValid = $false
    foreach ($field in @('configIncludeLayers', 'configCwdProvided')) {
        $request.PSObject.Properties.Remove($field)
    }
    Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
}
foreach ($includeLayers in @($false, $true)) {
    Expect-Reject ('configuration cwd scope remains unsupported with layers: ' + $includeLayers) {
        $request = New-ConfigReadRequest 1 1 $includeLayers; $request.configCwdProvided = $true
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
}
foreach ($field in @('tracePresent', 'traceValid')) {
    Expect-Reject ('incomplete config-read trace metadata: ' + $field) {
        $request = New-ConfigReadRequest 1 1; $request.PSObject.Properties.Remove($field)
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
    Expect-Reject ('false config-read trace metadata: ' + $field) {
        $request = New-ConfigReadRequest 1 1; $request.$field = $false
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
}
Expect-Reject 'valid sanitized invalid-trace event lacks admissible request shape' {
    $request = New-ConfigReadRequest 1 1; $request.traceValid = $false
    foreach ($field in @('configParamsValid', 'configIncludeLayers', 'configCwdProvided')) {
        $request.PSObject.Properties.Remove($field)
    }
    Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
}
foreach ($field in @('seq', 'pid', 'correlation')) {
    foreach ($badValue in @(0, -1, 4294967297L, 1.5, 'PRIVATE_CANARY')) {
        Expect-Reject ('invalid config-read request correlation field: ' + $field) {
            $request = New-ConfigReadRequest 1 1; $request.$field = $badValue
            Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
        }
        Expect-Reject ('invalid config-read response correlation field: ' + $field) {
            $response = New-ConfigReadResponse 1 1; $response.$field = $badValue
            Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
        }
    }
}
foreach ($field in @('seq', 'pid', 'correlation', 'kind', 'direction', 'method', 'class')) {
    Expect-Reject ('config-read response missing field: ' + $field) {
        $response = New-ConfigReadResponse 1 1; $response.PSObject.Properties.Remove($field)
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
    }
}
foreach ($method in @('experimentalFeature/list', 'Config/read', 'config/read/extra', 'unknown')) {
    Expect-Reject ('wrong config-read request method: ' + $method) {
        $request = New-ConfigReadRequest 1 1; $request.method = $method
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
    Expect-Reject ('wrong config-read response method: ' + $method) {
        $response = New-ConfigReadResponse 1 1; $response.method = $method
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
    }
}
foreach ($direction in @('from_bridge', 'stderr', 'TO_BRIDGE')) {
    Expect-Reject ('wrong config-read request direction: ' + $direction) {
        $request = New-ConfigReadRequest 1 1; $request.direction = $direction
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
}
foreach ($direction in @('to_bridge', 'stderr', 'FROM_BRIDGE')) {
    Expect-Reject ('wrong config-read response direction: ' + $direction) {
        $response = New-ConfigReadResponse 1 1; $response.direction = $direction
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
    }
}
foreach ($class in @('unsupported_method', 'unknown', 'duplicate_id', 'correlation_limit')) {
    Expect-Reject ('config-read native error class: ' + $class) {
        $response = New-ConfigReadResponse 1 1; $response.kind = 'rpc_error'; $response.class = $class
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
    }
}
foreach ($class in @('request', 'notification', 'unknown', 'Response')) {
    Expect-Reject ('config-read nonsuccess response class: ' + $class) {
        $response = New-ConfigReadResponse 1 1; $response.class = $class
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
    }
}
foreach ($class in @('duplicate_id', 'correlation_limit')) {
    Expect-Reject ('unrelated method ambiguous correlation fails config-read evidence: ' + $class) {
        Get-DesktopConfigReadEvidence @(
            (New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1),
            [pscustomobject]@{ class = $class; method = 'model/list' }
        )
    }
}
foreach ($field in @('cwd', 'includeLayers', 'result', 'config', 'code', 'authParamsValid')) {
    Expect-Reject ('config-read request refuses non-shape data: ' + $field) {
        $request = New-ConfigReadRequest 1 1
        $request | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1))
    }
    Expect-Reject ('config-read response refuses data beyond metadata: ' + $field) {
        $response = New-ConfigReadResponse 1 1
        $response | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1), $response)
    }
}
# Enabling layers changes only the accepted flag, not any validation or failure boundary.
foreach ($field in @('configParamsValid', 'configIncludeLayers', 'configCwdProvided')) {
    Expect-Reject ('layer request cannot omit shape metadata: ' + $field) {
        $request = New-ConfigReadRequest 1 1 $true; $request.PSObject.Properties.Remove($field)
        Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1)) -RequireLayers
    }
}
Expect-Reject 'layer request cannot bypass invalid parameter metadata' {
    $request = New-ConfigReadRequest 1 1 $true; $request.configParamsValid = $false
    Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1)) -RequireLayers
}
Expect-Reject 'layer request cannot publish unknown metadata' {
    $request = New-ConfigReadRequest 1 1 $true
    $request | Add-Member -NotePropertyName config -NotePropertyValue 'PRIVATE_CANARY'
    Get-DesktopConfigReadEvidence @($request, (New-ConfigReadResponse 1 1)) -RequireLayers
}
foreach ($class in @('unsupported_method', 'unknown', 'duplicate_id', 'correlation_limit')) {
    Expect-Reject ('layer request cannot count an error as coverage: ' + $class) {
        $response = New-ConfigReadResponse 1 1; $response.kind = 'rpc_error'; $response.class = $class
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1 $true), $response) -RequireLayers
    }
}
Expect-Reject 'successful layers-disabled reply cannot cover an outstanding layer request' {
    Get-DesktopConfigReadEvidence @(
        (New-ConfigReadRequest 1 1), (New-ConfigReadRequest 1 2 $true), (New-ConfigReadResponse 1 1)
    ) -RequireLayers
}
Expect-Reject 'layer request cannot reuse a completed layers-disabled correlation' {
    Get-DesktopConfigReadEvidence @(
        (New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1),
        (New-ConfigReadRequest 1 1 $true), (New-ConfigReadResponse 1 1)
    ) -RequireLayers
}
# The published summary stops at 256 records; evidence must consume the full snapshot.
$prefix = @((New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1))
for ($index = 3; $index -le 300; $index++) {
    $prefix += [pscustomobject]@{ seq = $index; pid = 1; kind = 'stream_end'; direction = 'stderr'; class = 'eof' }
}
Expect-Pass 'config-read requests after the summary cutoff are still counted' {
    $request = New-ConfigReadRequest 1 2 $true; $request.seq = 301
    $response = New-ConfigReadResponse 1 2; $response.seq = 302
    $evidence = Get-DesktopConfigReadEvidence ($prefix + @($request, $response)) -RequireLayers
    if ($evidence.requests -ne 2 -or $evidence.validatedResponses -ne 2 -or
        $evidence.layerRequests -ne 1 -or $evidence.layerValidatedResponses -ne 1 -or
        $evidence.withoutLayerRequests -ne 1 -or $evidence.withoutLayerValidatedResponses -ne 1) {
        throw 'Truncated config-read layer evidence.'
    }
}
Expect-Reject 'outstanding config-read layer request beyond the summary cutoff fails' {
    $request = New-ConfigReadRequest 1 2 $true; $request.seq = 301
    Get-DesktopConfigReadEvidence ($prefix + @($request)) -RequireLayers
}
Expect-Reject 'config-read layer error beyond the summary cutoff fails' {
    $request = New-ConfigReadRequest 1 2 $true; $request.seq = 301
    $response = New-ConfigReadResponse 1 2; $response.seq = 302; $response.kind = 'rpc_error'; $response.class = 'unknown'
    Get-DesktopConfigReadEvidence ($prefix + @($request, $response)) -RequireLayers
}
# Config-read evidence admits only explicitly profiled, already-fixed owned metadata.
function New-OwnedConfigReadResponse {
    $response=New-ConfigReadResponse 1 1
    $response | Add-Member -NotePropertyName ownedModelCatalog -NotePropertyValue ([pscustomobject]@{
        schema=1;pathMatches=$true;digestMatches=$true;validated=$true})
    return $response
}
Expect-Pass 'owned catalog metadata preserves config read layer evidence' {
    $evidence=Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1 $true),(New-OwnedConfigReadResponse)) -RequireLayers -FixtureProfile 'bridge-owned-catalog-4239'
    if ($evidence.requests -ne 1 -or $evidence.validatedResponses -ne 1 -or $evidence.layerValidatedResponses -ne 1) { throw 'Owned config-read evidence lost correlation or layers.' }
}
foreach ($profile in @('','synthetic-models-4239','BRIDGE-OWNED-CATALOG-4239','PRIVATE_CANARY')) {
    Expect-Reject 'owned config response needs exact explicit profile' {
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1 $true),(New-OwnedConfigReadResponse)) -RequireLayers -FixtureProfile $profile
    }
}
foreach ($mutation in @(
    {param($e) $e | Add-Member -NotePropertyName raw -NotePropertyValue 'PRIVATE_CANARY'},
    {param($e) $e.ownedModelCatalog | Add-Member -NotePropertyName path -NotePropertyValue 'PRIVATE_CANARY'},
    {param($e) $e.ownedModelCatalog.pathMatches='PRIVATE_CANARY'},
    {param($e) $e.ownedModelCatalog.schema=2},
    {param($e) $e.correlation=2}
)) {
    Expect-Reject 'owned config evidence rejects unknown raw malformed or unmatched metadata' {
        $response=New-OwnedConfigReadResponse; & $mutation $response
        Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1 $true),$response) -RequireLayers -FixtureProfile 'bridge-owned-catalog-4239'
    }
}
Expect-Reject 'owned metadata cannot supply missing layer request' {
    Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1),(New-OwnedConfigReadResponse)) -RequireLayers -FixtureProfile 'bridge-owned-catalog-4239'
}
Expect-Reject 'owned metadata cannot supply missing request correlation' {
    Get-DesktopConfigReadEvidence @((New-OwnedConfigReadResponse)) -RequireLayers -FixtureProfile 'bridge-owned-catalog-4239'
}
Expect-Pass 'owned metadata mismatch remains observation not positive catalog evidence' {
    $response=New-OwnedConfigReadResponse; $response.ownedModelCatalog.pathMatches=$false
    $evidence=Get-DesktopConfigReadEvidence @((New-ConfigReadRequest 1 1 $true),$response) -RequireLayers -FixtureProfile 'bridge-owned-catalog-4239'
    if ($evidence.layerValidatedResponses -ne 1) { throw 'Config-read transport evidence changed.' }
    # Owned-catalog success is independently counted only when all three booleans are true.
}
Expect-Pass 'runtime config-read evidence consumes the full sanitized snapshot once' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Get-DesktopConfigReadEvidence'
    }, $true))
    if ($calls.Count -ne 1 -or $calls[0].CommandElements.Count -ne 5 -or
        $calls[0].CommandElements[1] -isnot [Management.Automation.Language.VariableExpressionAst] -or
        $calls[0].CommandElements[1].VariablePath.UserPath -cne 'allEvents' -or
        $calls[0].CommandElements[2] -isnot [Management.Automation.Language.CommandParameterAst] -or
        $calls[0].CommandElements[2].ParameterName -cne 'RequireLayers' -or
        $null -ne $calls[0].CommandElements[2].Argument -or
        $calls[0].CommandElements[3] -isnot [Management.Automation.Language.CommandParameterAst] -or
        $calls[0].CommandElements[3].ParameterName -cne 'FixtureProfile' -or
        $null -ne $calls[0].CommandElements[3].Argument -or
        $calls[0].CommandElements[4] -isnot [Management.Automation.Language.VariableExpressionAst] -or
        $calls[0].CommandElements[4].VariablePath.UserPath -cne 'DesktopFixtureProfile') {
        throw 'ConfigRead evidence must consume the full sanitized snapshot and require actual layers.'
    }
}

# Optional read-caller metadata is source-only and must match an independently trusted ASAR identity.
$readArchiveHash = 'a' * 64
function New-ReadCallerReport {
    return [pscustomobject]@{
        schema = 1; status = 'complete'; archiveSha256 = $readArchiveHash
        filesSelected = 1; filesComplete = 1; sourceBytes = 100
        sources = @([pscustomobject]@{
            path = '.vite/build/PRIVATE_CANARY.js'; sha256 = ('b' * 64)
            analysis = [pscustomobject]@{
                schema = 1; status = 'source_syntax_only'; sourceParsedCompletely = $true
                runtimeBindingProven = $false; responseValueProven = $false
                consumerCoverage = 'direct_await_and_sibling_const_only'
                calls = @(
                    [pscustomobject]@{
                        method = 'config/read'; offsetUtf16 = 10; relation = 'literal_first_argument_only'
                        responseFields = @([pscustomobject]@{ relation = 'await_member'; path = @('config', 'model') })
                    },
                    [pscustomobject]@{
                        method = 'fs/readFile'; offsetUtf16 = 20; relation = 'literal_first_argument_only'
                        responseFields = @([pscustomobject]@{ relation = 'const_binding'; path = @('dataBase64') })
                        pathSyntax = 'call'; pathMarkers = @('config_toml', 'dot_codex'); pathAuthorityProven = $false
                    }
                )
            }
        })
    }
}
function Read-CallerFixture($value, [string]$ExpectedHash = $readArchiveHash, [int]$ExitCode = 0) {
    $text = 'DESKTOP_READ_CALLERS_METADATA=' + ($value | ConvertTo-Json -Depth 16 -Compress) + "`n"
    return ConvertFrom-DesktopReadCallerOutput $text '' $ExitCode $ExpectedHash
}
Expect-Pass 'read-caller report requires provenance and projects away source names' {
    $safe = Read-CallerFixture (New-ReadCallerReport)
    if ($safe.archiveSha256 -cne $readArchiveHash -or $safe.callCount -ne 2 -or
        $safe.filesComplete -ne 1 -or $safe.sources[0].source -ne 0 -or
        ($safe | ConvertTo-Json -Depth 16 -Compress).Contains('PRIVATE_CANARY')) {
        throw 'Invalid sanitized caller projection.'
    }
    Assert-Keys ([pscustomobject]$safe.sources[0]) @('source', 'sha256', 'analysis')
    if ($safe.sources[0].analysis.runtimeBindingProven -or $safe.sources[0].analysis.responseValueProven -or
        $safe.sources[0].analysis.calls[1].pathAuthorityProven) { throw 'Unsupported static proof claim.' }
}
Expect-Pass 'read-caller partial source carries only its fixed unavailable reason' {
    $value = New-ReadCallerReport
    $value.status = 'partial'; $value.filesSelected = 2
    $value.sources += [pscustomobject]@{
        path = '.vite/build/z.js'; sha256 = ('c' * 64)
        analysis = [pscustomobject]@{ schema = 1; status = 'unavailable'; reason = 'syntax_error' }
    }
    $safe = Read-CallerFixture $value
    if ($safe.status -cne 'partial' -or $safe.filesComplete -ne 1 -or $safe.callCount -ne 2) {
        throw 'Partial evidence changed meaning.'
    }
}
Expect-Pass 'zero selected source syntax facts are not converted to a runtime absence claim' {
    $value = New-ReadCallerReport
    $value.filesSelected = 0; $value.filesComplete = 0; $value.sourceBytes = 0; $value.sources = @()
    $safe = Read-CallerFixture $value
    if ($safe.callCount -ne 0 -or $safe.sources.Count -ne 0) { throw 'Invented static evidence.' }
}
foreach ($reason in @('input_limit', 'token_limit', 'depth_limit', 'deadline', 'call_limit',
    'node_limit', 'local_node_limit', 'field_limit', 'syntax_error', 'analysis_failed',
    'source_limit', 'source_read_failed', 'archive_mismatch', 'archive_changed',
    'package_path', 'report_limit', 'invalid_arguments', 'disposable_windows_ci_required')) {
    Expect-Pass ('fixed unavailable read-caller result: ' + $reason) {
        $safe = Read-CallerFixture ([pscustomobject]@{ schema = 1; status = 'unavailable'; reason = $reason }) $readArchiveHash 2
        Assert-Keys ([pscustomobject]$safe) @('schema', 'status', 'reason')
    }
}
foreach ($badHash in @('', ('c' * 64), ('A' * 64), 'PRIVATE_CANARY')) {
    Expect-Reject 'read-caller report does not trust its own archive identity' {
        Read-CallerFixture (New-ReadCallerReport) $badHash
    }
}
foreach ($field in @('schema', 'status', 'archiveSha256', 'filesSelected', 'filesComplete', 'sourceBytes', 'sources')) {
    Expect-Reject ('read-caller missing package field: ' + $field) {
        $value = New-ReadCallerReport; $value.PSObject.Properties.Remove($field); Read-CallerFixture $value
    }
}
foreach ($field in @('sourceParsedCompletely', 'runtimeBindingProven', 'responseValueProven')) {
    Expect-Reject ('read-caller wrong proof boolean: ' + $field) {
        $value = New-ReadCallerReport
        $value.sources[0].analysis.$field = -not $value.sources[0].analysis.$field
        Read-CallerFixture $value
    }
    Expect-Reject ('read-caller nonboolean proof flag: ' + $field) {
        $value = New-ReadCallerReport; $value.sources[0].analysis.$field = 'PRIVATE_CANARY'; Read-CallerFixture $value
    }
}
foreach ($field in @('path', 'sha256', 'analysis')) {
    Expect-Reject ('read-caller missing source field: ' + $field) {
        $value = New-ReadCallerReport; $value.sources[0].PSObject.Properties.Remove($field); Read-CallerFixture $value
    }
}
foreach ($path in @('C:/PRIVATE_CANARY.js', '../PRIVATE_CANARY.js', '.vite/build/../PRIVATE_CANARY.js',
    '.vite/build/nested/PRIVATE_CANARY.js', '.vite/build/a.js?PRIVATE_CANARY', ('.vite/build/' + ('a' * 192) + '.js'))) {
    Expect-Reject 'read-caller rejects non-allowlisted source path' {
        $value = New-ReadCallerReport; $value.sources[0].path = $path; Read-CallerFixture $value
    }
}
foreach ($field in @('filesSelected', 'filesComplete', 'sourceBytes')) {
    foreach ($badNumber in @(-1, 1.5, 'PRIVATE_CANARY')) {
        Expect-Reject ('invalid read-caller count: ' + $field) {
            $value = New-ReadCallerReport; $value.$field = $badNumber; Read-CallerFixture $value
        }
    }
}
foreach ($pair in @(@('filesSelected', 2049), @('filesComplete', 2), @('sourceBytes', 33554433))) {
    Expect-Reject ('read-caller count bound: ' + $pair[0]) {
        $value = New-ReadCallerReport; $value.($pair[0]) = $pair[1]; Read-CallerFixture $value
    }
}
Expect-Reject 'complete caller report cannot hide an incomplete source' {
    $value = New-ReadCallerReport
    $value.sources[0].analysis = [pscustomobject]@{ schema = 1; status = 'unavailable'; reason = 'syntax_error' }
    Read-CallerFixture $value
}
Expect-Reject 'partial caller report must contain an unavailable source' {
    $value = New-ReadCallerReport; $value.status = 'partial'; Read-CallerFixture $value
}
Expect-Reject 'duplicate or unordered source references are invalid' {
    $value = New-ReadCallerReport; $value.filesSelected = 2; $value.filesComplete = 2
    $value.sources += $value.sources[0]; Read-CallerFixture $value
}
foreach ($field in @('method', 'offsetUtf16', 'relation', 'responseFields')) {
    Expect-Reject ('missing read-caller call metadata: ' + $field) {
        $value = New-ReadCallerReport; $value.sources[0].analysis.calls[0].PSObject.Properties.Remove($field)
        Read-CallerFixture $value
    }
}
foreach ($badOffset in @(-1, 16777216, 1.5, 'PRIVATE_CANARY')) {
    Expect-Reject 'read-caller UTF16 offset bound' {
        $value = New-ReadCallerReport; $value.sources[0].analysis.calls[0].offsetUtf16 = $badOffset
        Read-CallerFixture $value
    }
}
Expect-Reject 'read-caller duplicate or backwards offsets are invalid' {
    $value = New-ReadCallerReport; $value.sources[0].analysis.calls[1].offsetUtf16 = 10; Read-CallerFixture $value
}
Expect-Reject 'read-caller call budget is shared across sources' {
    $value = New-ReadCallerReport; $value.filesSelected = 2; $value.filesComplete = 2
    $other = (New-ReadCallerReport).sources[0]; $other.path = '.vite/build/z.js'
    $value.sources += $other
    foreach ($source in $value.sources) {
        $source.analysis.calls = @()
        for ($index = 0; $index -lt 65; $index++) {
            $source.analysis.calls += [pscustomobject]@{
                method = 'project/list'; offsetUtf16 = $index; relation = 'literal_first_argument_only'; responseFields = @()
            }
        }
    }
    Read-CallerFixture $value
}
foreach ($badMethod in @('Config/read', 'unknown', 'PRIVATE_CANARY')) {
    Expect-Reject 'read-caller method whitelist' {
        $value = New-ReadCallerReport; $value.sources[0].analysis.calls[0].method = $badMethod; Read-CallerFixture $value
    }
}
foreach ($badPath in @(@(), @('config', 'PRIVATE_CANARY'), @('config', 'data', 'name', 'roots'))) {
    Expect-Reject 'read-caller response field depth and vocabulary' {
        $value = New-ReadCallerReport; $value.sources[0].analysis.calls[0].responseFields[0].path = $badPath
        Read-CallerFixture $value
    }
}
Expect-Reject 'read-caller duplicate response field relation' {
    $value = New-ReadCallerReport
    $value.sources[0].analysis.calls[0].responseFields += $value.sources[0].analysis.calls[0].responseFields[0]
    Read-CallerFixture $value
}
foreach ($badMarker in @(@('PRIVATE_CANARY'), @('dot_codex', 'config_toml'), @('config_toml', 'config_toml'))) {
    Expect-Reject 'read-caller fixed path markers must be ordered and unique' {
        $value = New-ReadCallerReport; $value.sources[0].analysis.calls[1].pathMarkers = $badMarker
        Read-CallerFixture $value
    }
}
Expect-Reject 'unresolved caller path has no marker evidence' {
    $value = New-ReadCallerReport; $value.sources[0].analysis.calls[1].pathSyntax = 'unresolved'; Read-CallerFixture $value
}
Expect-Reject 'static caller path authority is never proven' {
    $value = New-ReadCallerReport; $value.sources[0].analysis.calls[1].pathAuthorityProven = $true; Read-CallerFixture $value
}
foreach ($scope in @('package', 'source', 'analysis', 'call', 'field')) {
    Expect-Reject ('raw caller data beyond exact schema: ' + $scope) {
        $value = New-ReadCallerReport
        $target = switch ($scope) {
            'package' { $value }
            'source' { $value.sources[0] }
            'analysis' { $value.sources[0].analysis }
            'call' { $value.sources[0].analysis.calls[0] }
            'field' { $value.sources[0].analysis.calls[0].responseFields[0] }
        }
        $target | Add-Member -NotePropertyName raw -NotePropertyValue 'PRIVATE_CANARY'
        Read-CallerFixture $value
    }
}
Expect-Reject 'read-caller unavailable reason is a fixed label' {
    Read-CallerFixture ([pscustomobject]@{ schema = 1; status = 'unavailable'; reason = 'PRIVATE_CANARY' }) $readArchiveHash 2
}
Expect-Reject 'read-caller parser rejects duplicate JSON keys' {
    ConvertFrom-DesktopReadCallerOutput 'DESKTOP_READ_CALLERS_METADATA={"schema":1,"status":"unavailable","reason":"syntax_error","reason":"deadline"}' '' 2 $readArchiveHash
}
foreach ($text in @('PRIVATE_CANARY', ('x' * 65601), "DESKTOP_READ_CALLERS_METADATA={}`nPRIVATE_CANARY",
    "DESKTOP_READ_CALLERS_METADATA={}`n`n")) {
    Expect-Reject 'read-caller output framing and bound' { ConvertFrom-DesktopReadCallerOutput $text '' 0 $readArchiveHash }
}
Expect-Reject 'read-caller stderr is captured but never published' {
    ConvertFrom-DesktopReadCallerOutput 'DESKTOP_READ_CALLERS_METADATA={}' 'PRIVATE_CANARY' 0 $readArchiveHash
}
foreach ($exitCode in @(1, 3, -1)) {
    Expect-Reject 'unknown read-caller process exit cannot become success' {
        Read-CallerFixture (New-ReadCallerReport) $readArchiveHash $exitCode
    }
}
Expect-Reject 'successful caller metadata cannot accompany failure exit' { Read-CallerFixture (New-ReadCallerReport) $readArchiveHash 2 }
Expect-Reject 'unavailable caller metadata cannot accompany success exit' {
    Read-CallerFixture ([pscustomobject]@{ schema = 1; status = 'unavailable'; reason = 'deadline' })
}


# The observer root input is only a private, escaped harness declaration.
function New-RootBackends {
    return @(
        [ordered]@{ home = 'C:\fixture\alpha'; codex_home = 'C:\fixture\alpha-codex' },
        [ordered]@{ home = 'C:\fixture\beta'; codex_home = 'C:\fixture\beta-codex' }
    )
}
Expect-Pass 'root references retain exact supplied roots without inferring user state' {
    $roots = New-DesktopReadTargetEnvironment (New-RootBackends) 'C:\fixture\work' 'C:\fixture\verified-package' | ConvertFrom-Json
    Assert-Keys $roots @('primaryHome', 'primaryCodexHome', 'secondaryHome', 'secondaryCodexHome', 'workspace', 'verifiedPackage')
    if ($roots.primaryHome -cne 'C:\fixture\alpha' -or $roots.primaryCodexHome -cne 'C:\fixture\alpha-codex' -or
        $roots.secondaryHome -cne 'C:\fixture\beta' -or $roots.secondaryCodexHome -cne 'C:\fixture\beta-codex' -or
        $roots.workspace -cne 'C:\fixture\work' -or $roots.verifiedPackage -cne 'C:\fixture\verified-package') {
        throw 'Root reference provenance was changed.'
    }
}
foreach ($root in @('C:\', 'C:/fixture/work', '\\server\share\fixture', 'C:\fixture\with space\')) {
    Expect-Pass 'ordinary absolute root is serialized as escaped JSON' {
        $roots = New-DesktopReadTargetEnvironment (New-RootBackends) $root 'C:\fixture\package' | ConvertFrom-Json
        if ($roots.workspace -cne $root) { throw 'Root reference was changed.' }
    }
}
foreach ($root in @('', 'PRIVATE_CANARY', 'C:relative', '\rooted', '\\server', '\\?\C:\fixture',
    '\\.\pipe\fixture', 'C:\fixture\..\PRIVATE_CANARY', 'C:\fixture\.\other', 'C:\fixture\\other',
    'C:\fixture\value:stream', 'C:\fixture\trailing.', 'C:\fixture\trailing ', 'C:\fixture\CON.txt',
    'C:\fixture\SHORT~1', 'C:\fixture\private*', ('C:\' + ('a' * 4094)), ("C:\fixture\" + [char]0x00e9))) {
    foreach ($field in @('home', 'codex_home', 'workspace', 'verifiedPackage')) {
        Expect-Reject ('invalid root declaration: ' + $field) {
            $backends = New-RootBackends; $workspace = 'C:\fixture\work'; $packageRoot = 'C:\fixture\package'
            if ($field -ceq 'workspace') { $workspace = $root }
            elseif ($field -ceq 'verifiedPackage') { $packageRoot = $root }
            else { $backends[0][$field] = $root }
            New-DesktopReadTargetEnvironment $backends $workspace $packageRoot
        }
    }
}
Expect-Reject 'aggregate root JSON bound' {
    $backends = New-RootBackends
    foreach ($backend in $backends) {
        $backend.home = 'C:\' + ('a' * 4000); $backend.codex_home = 'C:\' + ('b' * 4000)
    }
    New-DesktopReadTargetEnvironment $backends ('C:\' + ('c' * 4000)) ('C:\' + ('d' * 4000))
}
Expect-Reject 'root references cannot omit a disposable backend' {
    New-DesktopReadTargetEnvironment @((New-RootBackends)[0]) 'C:\fixture\work' 'C:\fixture\package'
}
$runtimeRootContract = {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'New-DesktopReadTargetEnvironment'
    }, $true))
    $expected = @('New-DesktopReadTargetEnvironment', '-Backends', '$backends', '-Workspace',
        '$(if ($useLiveFixtures) { $liveWorkspace } else { $work })',
        '-VerifiedPackage', '$app.InstallLocation', '-ControlHome', '$controlHome')
    if ($calls.Count -ne 1 -or $calls[0].CommandElements.Count -ne $expected.Count) {
        throw 'Runtime root references lost their explicit provenance.'
    }
    for ($i = 0; $i -lt $expected.Count; $i++) {
        if ($calls[0].CommandElements[$i].Extent.Text -cne $expected[$i]) {
            throw 'Runtime root references lost their explicit provenance.'
        }
    }
    $fixtureAssignments = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -ceq '$useLiveFixtures'
    }, $true))
    if ($fixtureAssignments.Count -ne 1 -or
        $fixtureAssignments[0].Operator -ne [Management.Automation.Language.TokenKind]::Equals -or
        $fixtureAssignments[0].Right.Extent.Text -cne '$EnableReviewedLiveWelcome -or $EnableReadOnlyWinAppInspection' -or
        -not [object]::ReferenceEquals($fixtureAssignments[0].Parent, $ast.EndBlock) -or
        $fixtureAssignments[0].Extent.EndOffset -ge $calls[0].Extent.StartOffset) {
        throw 'Live fixture selection lost its unique explicit opt-in assignment.'
    }
}
Expect-Pass 'runtime root construction has one explicit harness provenance seam' $runtimeRootContract
$runtimeWorkspaceContract = {
    # The argument above is exact syntax, not an arbitrary expression allowance.
    # Exercise it without invoking the harness, provider, filesystem or a process.
    $call = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'New-DesktopReadTargetEnvironment'
    }, $true))[0]
    $selectWorkspace = [scriptblock]::Create($call.CommandElements[4].Extent.Text)
    $fixtureAssignment = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -ceq '$useLiveFixtures'
    }, $true))[0]
    $selectFixtures = [scriptblock]::Create($fixtureAssignment.Right.Extent.Text)
    $work = 'C:\fixture\original-owned-work'
    $liveWorkspace = 'C:\fixture\live-owned-workspace'
    # Both flags still select one owned root here, not a fallback or duplicate.
    # This tests selection only; the inspection option validator rejects both flags.
    foreach ($case in @(
        @($false, $false, $false), @($true, $false, $true),
        @($false, $true, $true), @($true, $true, $true)
    )) {
        $EnableReviewedLiveWelcome = $case[0]
        $EnableReadOnlyWinAppInspection = $case[1]
        $fixtureSelection = @(& $selectFixtures)
        if ($fixtureSelection.Count -ne 1 -or $fixtureSelection[0] -isnot [bool] -or
            $fixtureSelection[0] -ne $case[2]) { throw 'Fixture selection changed the explicit opt-in modes.' }
        $useLiveFixtures = $fixtureSelection[0]
        $selected = @(& $selectWorkspace)
        $expectedRoot = $(if ($case[2]) { $liveWorkspace } else { $work })
        if ($selected.Count -ne 1 -or $selected[0] -cne $expectedRoot) {
            throw 'Opt-in changed the wrong workspace provenance branch.'
        }
    }
    # One validator-owned assignment, only inside the explicit opt-in branch,
    # before the one declaration seam. JSON cannot supply arbitrary path authority.
    $lastEnd = -1
    foreach ($spec in @(
        @('$liveOwned', 'ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness'),
        @('$liveWorkspace', '$liveOwned.Workspace')
    )) {
        $assignments = @($ast.FindAll({
            param($node)
            $node -is [Management.Automation.Language.AssignmentStatementAst] -and
            $node.Left.Extent.Text -ceq $spec[0]
        }, $true))
        if ($assignments.Count -ne 1 -or $assignments[0].Right.Extent.Text -cne $spec[1] -or
            $assignments[0].Extent.StartOffset -le $lastEnd -or
            $assignments[0].Extent.EndOffset -ge $call.Extent.StartOffset) {
            throw 'Live workspace bypassed its unique validated provenance assignment.'
        }
        $guard = $assignments[0].Parent
        while ($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]) { $guard = $guard.Parent }
        if ($null -eq $guard -or $guard.Clauses.Count -ne 1 -or
            $guard.Clauses[0].Item1.Extent.Text -cne '$useLiveFixtures' -or
            $fixtureAssignment.Extent.EndOffset -ge $guard.Extent.StartOffset -or
            $guard.Clauses[0].Item2.Extent.StartOffset -gt $assignments[0].Extent.StartOffset -or
            $guard.Clauses[0].Item2.Extent.EndOffset -lt $assignments[0].Extent.EndOffset) {
            throw 'Live workspace provenance is outside the explicit opt-in branch.'
        }
        $lastEnd = $assignments[0].Extent.EndOffset
    }
}
Expect-Pass 'runtime workspace alternatives retain their exact owned provenance' $runtimeWorkspaceContract
# Mutate source only, never execute the harness. The same contracts must reject
# welcome-only drift, arbitrary root authority and duplicate provenance seams.
foreach ($mutation in @(
    @('$useLiveFixtures = $EnableReviewedLiveWelcome -or $EnableReadOnlyWinAppInspection',
        '$useLiveFixtures = $EnableReviewedLiveWelcome'),
    @('$(if ($useLiveFixtures) { $liveWorkspace } else { $work })',
        '$(if ($EnableReviewedLiveWelcome) { $liveWorkspace } else { $work })'),
    @('        if ($useLiveFixtures) {', '        if ($EnableReviewedLiveWelcome) {'),
    @('$liveWorkspace = $liveOwned.Workspace', '$liveWorkspace = $liveConfig.workspace'),
    @('$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness',
        '$liveOwned = $liveConfig'),
    @('$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness',
        '$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -Readiness $liveFixtureReport.readiness'),
    @('$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness',
        '$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile'),
    @('$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness',
        '$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $liveConfig.profile -Readiness $liveFixtureReport.readiness'),
    @('$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness',
        '$liveOwned = ConvertTo-LiveConfiguration $liveConfig $liveScratch $officialBackend.FullName -Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveConfig.readiness'),
    @('$useLiveFixtures = $EnableReviewedLiveWelcome -or $EnableReadOnlyWinAppInspection',
        ('$useLiveFixtures = $EnableReviewedLiveWelcome -or $EnableReadOnlyWinAppInspection' + "`n" + '$useLiveFixtures = $true')),
    @('New-DesktopReadTargetEnvironment `', 'New-DesktopReadTargetEnvironment; New-DesktopReadTargetEnvironment `')
)) {
    if ($mutation[0] -ceq '        if ($useLiveFixtures) {') {
        # The first indented guard owns fixture setup. Replace only that guard,
        # not later report guards or workspace subexpressions, on LF or CRLF.
        $guardOffset = $ast.Extent.Text.IndexOf($mutation[0], [StringComparison]::Ordinal)
        if ($guardOffset -lt 0) { throw 'FAIL: fixture setup guard mutation target missing' }
        $mutatedSource = $ast.Extent.Text.Remove($guardOffset, $mutation[0].Length).Insert($guardOffset, $mutation[1])
    } else {
        $mutatedSource = $ast.Extent.Text.Replace($mutation[0], $mutation[1])
    }
    if ($mutatedSource -ceq $ast.Extent.Text) { throw 'FAIL: root provenance mutation did not change source' }
    $mutationTokens = $null; $mutationErrors = $null
    $mutatedAst = [Management.Automation.Language.Parser]::ParseInput($mutatedSource, [ref]$mutationTokens, [ref]$mutationErrors)
    if ($mutationErrors.Count -ne 0) { throw 'FAIL: root provenance mutation must remain valid syntax' }
    Expect-Reject 'runtime root provenance contract rejects source mutation' {
        $ast = $mutatedAst
        & $runtimeRootContract
        & $runtimeWorkspaceContract
    }
}
Expect-Pass 'uninformative optional caller scan is not executed again' {
    $runtimeStrings = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.StringConstantExpressionAst] -and
        $node.Value -ceq 'build/inspect-desktop-read-callers.mjs'
    }, $true))
    if ($runtimeStrings.Count -ne 0) { throw 'Optional caller scan must remain uninvoked.' }
}


Expect-Reject 'control-plane profile needs an explicit real bridge home' {
    New-DesktopBridgeSettings (New-Backends) $true $true $version $hash
}
foreach ($backendHome in @('fixture-alpha-home', 'fixture-alpha-codex', 'fixture-beta-home', 'fixture-beta-codex')) {
    Expect-Reject 'bridge home must not alias a native home' {
        New-DesktopBridgeSettings (New-Backends) $true $true $version $hash $backendHome
    }
}
Expect-Pass 'observer receives a separate explicit control root only when supplied' {
    $roots = New-DesktopReadTargetEnvironment (New-RootBackends) 'C:\fixture\work' 'C:\fixture\package' 'C:\control-home' | ConvertFrom-Json
    Assert-Keys $roots @('primaryHome', 'primaryCodexHome', 'secondaryHome', 'secondaryCodexHome', 'workspace', 'verifiedPackage', 'controlHome')
    if ($roots.controlHome -cne 'C:\control-home') { throw 'Control-root provenance changed.' }
}
foreach ($root in @('C:\fixture\alpha', 'C:\fixture\alpha-codex', 'C:\fixture\beta', 'C:\fixture\beta-codex', 'C:\fixture\work', 'C:\fixture\package')) {
    Expect-Reject 'observer control root cannot equal another root' {
        New-DesktopReadTargetEnvironment (New-RootBackends) 'C:\fixture\work' 'C:\fixture\package' $root
    }
}


# Synthetic native successes have exact counts, pins and cleanup flags.
function New-NativeControlSuccess {
    return [pscustomobject]@{
        schema = 1; scope = 'synthetic-loopback-only'; status = 'passed'; stage = 'complete'; failure = ''
        native_sha256 = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
        binary_verified = $true; version_verified = $true; adapter_clean_exit = $true; cleanup_complete = $true
        config_reads = 9; layer_reads = 6; stable_layer_rereads = 3; layer_version_changes = 0
        catalog_models = 2; threads = 2; turns = 4; assistant_messages = 4
        thread_list_queries = 2; thread_list_pages = 4; thread_list_items = 4
        thread_reads = 2; ownership_rejections = 2; alpha_requests = 2; beta_requests = 2
        fixture_errors = 0; unexpected_server_requests = 0
    }
}
Expect-Pass 'native success is sanitized as separate exact synthetic evidence' {
    $value = New-NativeControlSuccess
    $safe = ConvertTo-DesktopNativeControlEvidence $value
    Assert-Keys ([pscustomobject]$safe) @($value.PSObject.Properties | ForEach-Object { $_.Name })
    foreach ($field in $safe.Keys) {
        if ($safe[$field] -cne $value.$field) { throw 'Native evidence projection changed a value.' }
    }
}
foreach ($field in @('binary_verified', 'version_verified', 'adapter_clean_exit', 'cleanup_complete')) {
    foreach ($bad in @($false, 1, 'true', $null, 'PRIVATE_CANARY')) {
        Expect-Reject ('native success requires exact true flag: ' + $field) {
            $value = New-NativeControlSuccess; $value.$field = $bad
            ConvertTo-DesktopNativeControlEvidence $value
        }
    }
}
foreach ($field in @('config_reads', 'layer_reads', 'stable_layer_rereads', 'layer_version_changes',
    'catalog_models', 'threads', 'turns', 'assistant_messages', 'thread_reads',
    'thread_list_queries', 'thread_list_pages', 'thread_list_items',
    'ownership_rejections', 'alpha_requests', 'beta_requests', 'fixture_errors', 'unexpected_server_requests')) {
    foreach ($bad in @(-1, 99, 1.5, 'PRIVATE_CANARY', $null)) {
        Expect-Reject ('native success requires exact fixture count: ' + $field) {
            $value = New-NativeControlSuccess; $value.$field = $bad
            ConvertTo-DesktopNativeControlEvidence $value
        }
    }
    foreach ($delta in @(-1, 1)) {
        Expect-Reject ('native count cannot hide an incomplete or extra operation: ' + $field) {
            $value = New-NativeControlSuccess; $value.$field += $delta
            ConvertTo-DesktopNativeControlEvidence $value
        }
    }
}
foreach ($field in @('schema', 'scope', 'status', 'stage', 'failure', 'native_sha256')) {
    foreach ($bad in @('PRIVATE_CANARY', $null, 0, @('passed'))) {
        Expect-Reject ('native report rejects non-contract field value: ' + $field) {
            $value = New-NativeControlSuccess; $value.$field = $bad
            ConvertTo-DesktopNativeControlEvidence $value
        }
    }
}
foreach ($field in @((New-NativeControlSuccess).PSObject.Properties | ForEach-Object { $_.Name })) {
    Expect-Reject ('native report cannot omit a field: ' + $field) {
        $value = New-NativeControlSuccess; $value.PSObject.Properties.Remove($field)
        ConvertTo-DesktopNativeControlEvidence $value
    }
    Expect-Reject ('native report keys are case-sensitive: ' + $field) {
        $value = New-NativeControlSuccess; $saved = $value.$field; $value.PSObject.Properties.Remove($field)
        $value | Add-Member -NotePropertyName $field.ToUpperInvariant() -NotePropertyValue $saved
        ConvertTo-DesktopNativeControlEvidence $value
    }
}
foreach ($field in @('result', 'config', 'threadId', 'path', 'error', 'stderr')) {
    Expect-Reject ('native report refuses unreviewed data: ' + $field) {
        $value = New-NativeControlSuccess
        $value | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        ConvertTo-DesktopNativeControlEvidence $value
    }
}
Expect-Reject 'native failure cannot become success' {
    $value = New-NativeControlSuccess; $value.status = 'failed'
    ConvertTo-DesktopNativeControlEvidence $value
}
function Assert-NativeSliceAdmission([scriptblock]$ProfileCondition, [scriptblock]$EligibilityCondition) {
    # Execute only the exact reviewed Boolean conditions and the pure decision
    # helper. A counter stands in for native invocation; no harness body runs.
    foreach ($EnableReviewedLaunchCompatibility in @($false, $true)) {
        foreach ($EnableReadOnlyWinAppInspection in @($false, $true)) {
            foreach ($EnableReviewedWelcome4239 in @($false, $true)) {
                foreach ($captureState in @($false, $true)) {
                    foreach ($activationState in @($false, $true)) {
                        foreach ($cleanupError in @($false, $true)) {
                            $nativeRunDecision = Get-DesktopNativeRunDecision $EnableReviewedLaunchCompatibility `
                                $captureState $activationState $cleanupError
                            $profileAllowed = @(& $ProfileCondition)
                            $eligible = @(& $EligibilityCondition)
                            if ($profileAllowed.Count -ne 1 -or $profileAllowed[0] -isnot [bool] -or
                                $eligible.Count -ne 1 -or $eligible[0] -isnot [bool]) {
                                throw 'Native admission conditions must each return one Boolean.'
                            }
                            $invocations = 0
                            if ($profileAllowed[0]) {
                                if ($eligible[0]) { $invocations++ }
                            }
                            $expected = $EnableReviewedLaunchCompatibility -and -not $EnableReadOnlyWinAppInspection -and -not $EnableReviewedWelcome4239 -and
                                $captureState -and $activationState -and -not $cleanupError
                            if ($invocations -ne [int]$expected -or
                                (($EnableReadOnlyWinAppInspection -or $EnableReviewedWelcome4239) -and $invocations -ne 0)) {
                                throw 'Native admission bypassed inspection exclusion or capture/cleanup safety.'
                            }
                        }
                    }
                }
            }
        }
    }
}
Expect-Pass 'native slice has one explicit guarded invocation after Desktop cleanup' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Invoke-ControlPlaneNative'
    }, $true))
    if ($calls.Count -ne 1) { throw 'Native slice must be invoked exactly once.' }
    $guard = $calls[0].Parent
    while ($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]) { $guard = $guard.Parent }
    if ($null -eq $guard -or $guard.Clauses.Count -ne 1 -or
        $guard.Clauses[0].Item1.Extent.Text -cne '$nativeRunDecision.run' -or
        $guard.Clauses[0].Item2.Extent.StartOffset -gt $calls[0].Extent.StartOffset -or
        $guard.Clauses[0].Item2.Extent.EndOffset -lt $calls[0].Extent.EndOffset) {
        throw 'Native slice bypassed the independent capture/cleanup eligibility decision.'
    }
    $outer = $guard.Parent
    while ($null -ne $outer -and $outer -isnot [Management.Automation.Language.IfStatementAst]) { $outer = $outer.Parent }
    if ($null -eq $outer -or $outer.Clauses.Count -ne 1 -or
        $outer.Clauses[0].Item1.Extent.Text -cne '$EnableReviewedLaunchCompatibility -and -not $EnableReadOnlyWinAppInspection -and -not $EnableReviewedWelcome4239' -or
        $outer.Clauses[0].Item2.Extent.StartOffset -gt $guard.Extent.StartOffset -or
        $outer.Clauses[0].Item2.Extent.EndOffset -lt $guard.Extent.EndOffset) {
        throw 'Native slice bypassed the explicit profile gate.'
    }
    $cleanup = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.TryStatementAst] -and $null -ne $node.Finally -and
        $node.Finally.Extent.Text -match 'CloseMainWindow'
    }, $true))
    if ($cleanup.Count -ne 1 -or $cleanup[0].Extent.EndOffset -ge $calls[0].Extent.StartOffset) {
        throw 'Native slice started before actual Desktop cleanup.'
    }
    Assert-NativeSliceAdmission ([scriptblock]::Create($outer.Clauses[0].Item1.Extent.Text)) `
        ([scriptblock]::Create($guard.Clauses[0].Item1.Extent.Text))
}
Expect-Reject 'read-only inspection cannot admit native turns when its exclusion is removed' {
    Assert-NativeSliceAdmission { $EnableReviewedLaunchCompatibility -and -not $EnableReviewedWelcome4239 } { $nativeRunDecision.run }
}
Expect-Reject '4239 welcome cannot admit native turns when its exclusion is removed' {
    Assert-NativeSliceAdmission { $EnableReviewedLaunchCompatibility -and -not $EnableReadOnlyWinAppInspection } { $nativeRunDecision.run }
}
Expect-Reject 'native admission cannot bypass the capture and cleanup decision' {
    Assert-NativeSliceAdmission { $EnableReviewedLaunchCompatibility -and -not $EnableReadOnlyWinAppInspection -and -not $EnableReviewedWelcome4239 } { $true }
}


Expect-Pass 'native wrapper accepts the exact independent success projection' {
    Assert-ControlPlaneNativeSummary (New-NativeControlSuccess)
}
foreach ($field in @('thread_list_queries', 'thread_list_pages', 'thread_list_items')) {
    Expect-Reject 'native wrapper requires each thread-list count' {
        $value = New-NativeControlSuccess; $value.PSObject.Properties.Remove($field)
        Assert-ControlPlaneNativeSummary $value
    }
    foreach ($bad in @($null, $false, '2', 0.5, @('PRIVATE_CANARY'))) {
        Expect-Reject 'native wrapper requires numeric integer thread-list counts' {
            $value = New-NativeControlSuccess; $value.$field = $bad
            Assert-ControlPlaneNativeSummary $value
        }
    }
    foreach ($delta in @(-1, 1)) {
        Expect-Reject 'native wrapper requires exact successful thread-list counts' {
            $value = New-NativeControlSuccess; $value.$field += $delta
            Assert-ControlPlaneNativeSummary $value
        }
    }
}
foreach ($failure in @('native_error', 'config_shape_mismatch', 'ownership_changed', 'control_home_mismatch', 'supervision_failed')) {
    Expect-Pass ('native wrapper keeps a fixed failure without converting it to success: ' + $failure) {
        $value = New-NativeControlSuccess; $value.status = 'failed'; $value.stage = 'supervisor'; $value.failure = $failure
        $value.cleanup_complete = $false
        Assert-ControlPlaneNativeSummary $value
    }
}
Expect-Pass 'native wrapper preserves the bounded thread-list failure stage and label' {
    $value = New-NativeControlSuccess; $value.status = 'failed'; $value.stage = 'thread_list'; $value.failure = 'thread_list_invalid'
    Assert-ControlPlaneNativeSummary $value
}
foreach ($field in @('scope', 'status', 'stage', 'failure', 'native_sha256')) {
    Expect-Reject ('native failure report cannot publish arbitrary labels: ' + $field) {
        $value = New-NativeControlSuccess; $value.status = 'failed'; $value.failure = 'native_error'
        $value.$field = 'PRIVATE_CANARY'
        Assert-ControlPlaneNativeSummary $value
    }
}
foreach ($field in @('config_reads', 'layer_reads', 'stable_layer_rereads', 'layer_version_changes',
    'catalog_models', 'threads', 'turns', 'assistant_messages', 'thread_reads',
    'thread_list_queries', 'thread_list_pages', 'thread_list_items',
    'ownership_rejections', 'alpha_requests', 'beta_requests', 'fixture_errors', 'unexpected_server_requests')) {
    foreach ($bad in @(-1, 2049, 0.5, 'PRIVATE_CANARY')) {
        Expect-Reject ('native failed-report count remains bounded: ' + $field) {
            $value = New-NativeControlSuccess; $value.status = 'failed'; $value.failure = 'native_error'; $value.$field = $bad
            Assert-ControlPlaneNativeSummary $value
        }
    }
}
Expect-Reject 'native failure report cannot lose its failure reason' {
    $value = New-NativeControlSuccess; $value.status = 'failed'
    Assert-ControlPlaneNativeSummary $value
}
Expect-Reject 'native failure report cannot publish raw diagnostics' {
    $value = New-NativeControlSuccess; $value.status = 'failed'; $value.failure = 'native_error'
    $value | Add-Member -NotePropertyName stderr -NotePropertyValue 'PRIVATE_CANARY'
    Assert-ControlPlaneNativeSummary $value
}

Expect-Reject 'native success rejects the obsolete clean-shutdown claim' {
    $value = New-NativeControlSuccess
    $value.PSObject.Properties.Remove('adapter_clean_exit')
    $value | Add-Member -NotePropertyName clean_shutdown -NotePropertyValue $true
    ConvertTo-DesktopNativeControlEvidence $value
}
Expect-Reject 'native wrapper rejects the obsolete clean-shutdown claim' {
    $value = New-NativeControlSuccess
    $value.PSObject.Properties.Remove('adapter_clean_exit')
    $value | Add-Member -NotePropertyName clean_shutdown -NotePropertyValue $true
    Assert-ControlPlaneNativeSummary $value
}


# No processes are launched or killed by these synthetic lifecycle fixtures.
function New-ActivationProcessFixture([bool[]]$WaitResults = @($true)) {
    $queue = New-Object Collections.Queue
    foreach ($item in $WaitResults) { $queue.Enqueue($item) }
    $process = [pscustomobject]@{
        Id = 42; Handle = [IntPtr]1
        StartTime = [DateTime]::Parse('2026-11-06T01:02:03.0000000Z', [Globalization.CultureInfo]::InvariantCulture,
            [Globalization.DateTimeStyles]::RoundtripKind)
        HasExited = $false; CloseCalls = 0; KillCalls = 0
        WaitResults = $queue; WaitMilliseconds = (New-Object 'Collections.Generic.List[int]')
        CloseThrows = $false; KillThrows = $false; ExitOnCloseError = $false; ExitOnKillError = $false
        WaitWithoutExit = $false
    }
    $process | Add-Member -MemberType ScriptMethod -Name CloseMainWindow -Value {
        $this.CloseCalls++
        if ($this.CloseThrows) {
            if ($this.ExitOnCloseError) { $this.HasExited = $true }
            throw 'PRIVATE_CANARY'
        }
        return $true
    }
    $process | Add-Member -MemberType ScriptMethod -Name WaitForExit -Value {
        param([int]$Milliseconds)
        $this.WaitMilliseconds.Add($Milliseconds)
        $result = $false
        if ($this.WaitResults.Count -gt 0) { $result = [bool]$this.WaitResults.Dequeue() }
        if ($result -and -not $this.WaitWithoutExit) { $this.HasExited = $true }
        return $result
    }
    $process | Add-Member -MemberType ScriptMethod -Name Kill -Value {
        $this.KillCalls++
        if ($this.KillThrows) {
            if ($this.ExitOnKillError) { $this.HasExited = $true }
            throw 'PRIVATE_CANARY'
        }
    }
    return $process
}
function Invoke-ActivationFixture($Process, [scriptblock]$ReadPackage = { param($ProcessId) 'fixture.package' }) {
    return Complete-DesktopActivationProcess -Process $Process -ExpectedPid 42 -ObservedCreatedUtc '2026-11-06T01:02:03.0000000Z' -ExpectedPackage 'fixture.package' -ReadPackageIdentity $ReadPackage
}
Expect-Pass 'exact activation exits after bounded close with no forced termination' {
    $process = New-ActivationProcessFixture
    $result = @(Invoke-ActivationFixture $process)
    if ($result.Count -ne 1 -or $result[0] -isnot [bool] -or -not $result[0] -or
        $process.CloseCalls -ne 1 -or $process.KillCalls -ne 0 -or
        ($process.WaitMilliseconds -join ',') -cne '5000') { throw 'Wrong activation close outcome.' }
}
Expect-Pass 'unresponsive exact activation is terminated through its pinned object and confirmed' {
    $process = New-ActivationProcessFixture @($false, $true)
    if (-not (Invoke-ActivationFixture $process) -or $process.CloseCalls -ne 1 -or $process.KillCalls -ne 1 -or
        ($process.WaitMilliseconds -join ',') -cne '5000,5000') { throw 'Wrong activation termination outcome.' }
}
Expect-Pass 'already exited verified activation needs no termination' {
    $process = New-ActivationProcessFixture; $process.HasExited = $true
    if (-not (Invoke-ActivationFixture $process) -or $process.CloseCalls -ne 0 -or $process.KillCalls -ne 0 -or
        $process.WaitMilliseconds.Count -ne 0) { throw 'Exited activation was targeted again.' }
}
Expect-Reject 'unconfirmed activation termination never completes the native gate' {
    $process = New-ActivationProcessFixture @($false, $false)
    Invoke-ActivationFixture $process
}
Expect-Reject 'wait success without an exited process remains unknown' {
    $process = New-ActivationProcessFixture; $process.WaitWithoutExit = $true
    Invoke-ActivationFixture $process
}
foreach ($case in @('missing', 'pid', 'handle', 'created', 'package', 'package-missing', 'package-error')) {
    Expect-Pass ('unverified activation cannot be signaled: ' + $case) {
        $process = New-ActivationProcessFixture; $target = $process
        $reader = { param($ProcessId) 'fixture.package' }
        switch ($case) {
            'missing' { $target = $null }
            'pid' { $process.Id = 43 }
            'handle' { $process.Handle = [IntPtr]::Zero }
            'created' { $process.StartTime = $process.StartTime.AddSeconds(1) }
            'package' { $reader = { param($ProcessId) 'PRIVATE_CANARY' } }
            'package-missing' { $reader = { param($ProcessId) $null } }
            'package-error' { $reader = { param($ProcessId) throw 'PRIVATE_CANARY' } }
        }
        $rejected = $false
        try { $null = Invoke-ActivationFixture $target $reader }
        catch {
            if ($_.Exception.Message -cne 'The verified Desktop activation process exit could not be confirmed.') {
                throw 'Activation error was not safely classified.'
            }
            $rejected = $true
        }
        if (-not $rejected -or $process.CloseCalls -ne 0 -or $process.KillCalls -ne 0 -or
            $process.WaitMilliseconds.Count -ne 0) { throw 'Unverified activation was signaled.' }
    }
}
foreach ($phase in @('close', 'kill')) {
    Expect-Reject ('activation operation error is not exit confirmation: ' + $phase) {
        $process = New-ActivationProcessFixture @($false, $true)
        if ($phase -ceq 'close') { $process.CloseThrows = $true } else { $process.KillThrows = $true }
        Invoke-ActivationFixture $process
    }
    Expect-Pass ('verified concurrent exit during operation is observed on the same object: ' + $phase) {
        $process = New-ActivationProcessFixture @($false, $true)
        if ($phase -ceq 'close') { $process.CloseThrows = $true; $process.ExitOnCloseError = $true }
        else { $process.KillThrows = $true; $process.ExitOnKillError = $true }
        if (-not (Invoke-ActivationFixture $process)) { throw 'Verified concurrent exit was lost.' }
    }
}
Expect-Pass 'activation helper never reacquires a PID or kills a process by name' {
    $function = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -ceq 'Complete-DesktopActivationProcess'
    }, $true))[0]
    $forbidden = @($function.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -in @('Get-Process', 'Stop-Process', 'taskkill', 'taskkill.exe')
    }, $true))
    if ($forbidden.Count -ne 0) { throw 'Activation helper can retarget an unverified process.' }
}


Expect-Pass 'reviewed activation cleanup uses the original retained process identity, not WMI precision' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Complete-DesktopActivationProcess'
    }, $true))
    if ($calls.Count -ne 1) { throw 'Expected one activation completion seam.' }
    $elements = $calls[0].CommandElements
    $expected = @('Complete-DesktopActivationProcess', '-Process', '$ownedActivationProcess',
        '-ExpectedPid', '$activationPid', '-ObservedCreatedUtc', '$ownedActivationCreatedUtc')
    for ($i = 0; $i -lt $expected.Count; $i++) {
        if ($elements[$i].Extent.Text -cne $expected[$i]) { throw 'Activation cleanup lost the original start identity.' }
    }
    $retained = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -ceq '$ownedActivationProcess' -and $node.Right.Extent.Text -ceq '$started'
    }, $true))
    $timestamps = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -ceq '$ownedActivationCreatedUtc' -and
        $node.Right.Extent.Text -ceq '$ownedActivationProcess.StartTime.ToUniversalTime().ToString(''o'')'
    }, $true))
    if ($retained.Count -ne 1 -or $timestamps.Count -ne 1 -or
        $retained[0].Extent.EndOffset -ge $timestamps[0].Extent.StartOffset -or
        $timestamps[0].Extent.EndOffset -ge $calls[0].Extent.StartOffset) {
        throw 'Activation cleanup does not retain the original exact creation time.'
    }
}


# Native eligibility depends on validated capture and exact activation exit, not
# whether later Desktop compatibility requests succeeded.
foreach ($reviewed in @($false, $true)) {
    foreach ($captureState in @($false, $true)) {
        foreach ($activationState in @($false, $true)) {
            foreach ($cleanupError in @($false, $true)) {
                Expect-Pass 'native eligibility preserves every capture and cleanup boundary' {
                    $decision = Get-DesktopNativeRunDecision $reviewed $captureState $activationState $cleanupError
                    Assert-Keys ([pscustomobject]$decision) @('run', 'reason')
                    $expectedRun = $reviewed -and $captureState -and $activationState -and -not $cleanupError
                    if ($decision.run -isnot [bool] -or $decision.run -ne $expectedRun) { throw 'Wrong native eligibility.' }
                    $expectedReason = ''
                    if (-not $reviewed) { $expectedReason = 'profile_not_enabled' }
                    elseif (-not $captureState) { $expectedReason = 'desktop_capture_not_validated' }
                    elseif (-not $activationState -or $cleanupError) { $expectedReason = 'desktop_activation_exit_not_confirmed' }
                    if ($decision.reason -cne $expectedReason) { throw 'Wrong native eligibility reason.' }
                }
            }
        }
    }
}
function New-DesktopOutcomeFixture([bool]$Failed = $false) {
    if ($Failed) {
        return [pscustomobject]@{
            status = 'blocked'; stage = 'original-desktop-gate'
            reason = 'The actual Desktop request remains unsupported.'; exitCode = 13
        }
    }
    return [pscustomobject]@{
        status = 'observed'; stage = 'instrumented-policy-trace-captured'
        reason = 'Desktop capture and its strict gates passed.'; exitCode = 0
    }
}
foreach ($nativeState in @('passed', 'failed', 'not-run')) {
    $notRunReasons = @('')
    if ($nativeState -ceq 'not-run') {
        $notRunReasons = @('desktop_capture_not_validated', 'desktop_activation_exit_not_confirmed')
    }
    foreach ($notRunReason in $notRunReasons) {
        Expect-Pass ('original Desktop failure is unchanged by native outcome: ' + $nativeState) {
            $desktopState = New-DesktopOutcomeFixture $true
            $before = $desktopState | ConvertTo-Json -Compress
            $combined = Get-DesktopCombinedOutcome $desktopState $nativeState $notRunReason
            Assert-Keys ([pscustomobject]$combined) @('status', 'stage', 'reason', 'exitCode')
            foreach ($field in @('status', 'stage', 'reason', 'exitCode')) {
                if ($combined[$field] -cne $desktopState.$field) { throw 'A native result replaced the original Desktop failure.' }
            }
            if (($desktopState | ConvertTo-Json -Compress) -cne $before) { throw 'Original Desktop evidence was mutated.' }
        }
    }
}
Expect-Pass 'validated capture allows independent native proof after Desktop compatibility fails' {
    $decision = Get-DesktopNativeRunDecision $true $true $true $false
    if (-not $decision.run) { throw 'Desktop failure incorrectly suppressed independent native execution.' }
    $combined = Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture $true) 'passed'
    if ($combined.exitCode -ne 13 -or $combined.status -cne 'blocked') { throw 'Native success cleared a Desktop failure.' }
}
Expect-Pass 'both Desktop and native success keep a zero combined exit' {
    $combined = Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture) 'passed'
    if ($combined.status -cne 'observed' -or $combined.exitCode -ne 0 -or
        $combined.stage -cne 'desktop-capture-and-synthetic-native-control-completed') { throw 'Wrong combined success.' }
}
Expect-Pass 'native failure after Desktop success remains a failed combined job' {
    $combined = Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture) 'failed'
    if ($combined.status -cne 'blocked' -or $combined.exitCode -ne 1 -or
        $combined.stage -cne 'synthetic-native-control-plane') { throw 'Native failure was hidden.' }
}
foreach ($pair in @(@('desktop_capture_not_validated', 'desktop-capture'),
    @('desktop_activation_exit_not_confirmed', 'desktop-cleanup'))) {
    Expect-Pass ('missing safe precondition keeps native not-run and job failed: ' + $pair[0]) {
        $combined = Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture) 'not-run' $pair[0]
        if ($combined.status -cne 'blocked' -or $combined.exitCode -ne 1 -or $combined.stage -cne $pair[1]) {
            throw 'A missing native precondition was silently accepted.'
        }
    }
}
foreach ($nativeState in @('PRIVATE_CANARY', 'Passed', 'observed', '')) {
    Expect-Reject 'unknown native outcome cannot change the combined status' {
        Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture) $nativeState
    }
}
foreach ($badReason in @('', 'PRIVATE_CANARY', 'profile_not_enabled')) {
    Expect-Reject 'native not-run reason is explicit and bounded' {
        Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture) 'not-run' $badReason
    }
}
Expect-Reject 'executed native outcome cannot carry a not-run reason' {
    Get-DesktopCombinedOutcome (New-DesktopOutcomeFixture) 'passed' 'desktop_capture_not_validated'
}
foreach ($field in @('status', 'stage', 'reason', 'exitCode')) {
    Expect-Reject ('Desktop outcome cannot omit its original field: ' + $field) {
        $desktopState = New-DesktopOutcomeFixture; $desktopState.PSObject.Properties.Remove($field)
        Get-DesktopCombinedOutcome $desktopState 'passed'
    }
}
foreach ($badCode in @(-1, 256, 1.5, 'PRIVATE_CANARY', $null)) {
    Expect-Reject 'combined outcome requires a valid original exit code' {
        $desktopState = New-DesktopOutcomeFixture; $desktopState.exitCode = $badCode
        Get-DesktopCombinedOutcome $desktopState 'passed'
    }
}
Expect-Reject 'an inconsistent Desktop success cannot hide its nonzero exit' {
    $desktopState = New-DesktopOutcomeFixture; $desktopState.exitCode = 1
    Get-DesktopCombinedOutcome $desktopState 'passed'
}
Expect-Reject 'an inconsistent Desktop failure cannot claim zero exit' {
    $desktopState = New-DesktopOutcomeFixture $true; $desktopState.exitCode = 0
    Get-DesktopCombinedOutcome $desktopState 'passed'
}
Expect-Pass 'capture validity is assigned only after full trace validation and before strict compatibility gates' {
    $assignments = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -ceq '$captureValidated' -and $node.Right.Extent.Text -ceq '$true'
    }, $true))
    $initializers = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -ceq '$captureValidated' -and $node.Right.Extent.Text -ceq '$false'
    }, $true))
    $loops = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.ForStatementAst] -and
        $node.Extent.Text -match '^for \(\$slot = 1;'
    }, $true))
    if ($assignments.Count -ne 1 -or $initializers.Count -ne 1 -or $loops.Count -ne 1 -or
        $initializers[0].Extent.EndOffset -ge $loops[0].Extent.StartOffset -or
        $loops[0].Extent.EndOffset -ge $assignments[0].Extent.StartOffset) {
        throw 'Capture validity was set before the complete bounded trace-validation loop.'
    }
    foreach ($name in @('Assert-PublicCanonicalJSON', 'Assert-PublicReadRequestEvent', 'Assert-PublicPluginEvent')) {
        $validators = @($loops[0].FindAll({
            param($node)
            $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq $name
        }, $true))
        if ($validators.Count -ne 1) { throw 'Full snapshot validation lost a required sanitizer.' }
    }
    foreach ($name in @('Get-DesktopNoAuthStatusEvidence', 'Get-DesktopFeatureListEvidence', 'Get-DesktopConfigReadEvidence')) {
        $strictCalls = @($ast.FindAll({
            param($node)
            $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq $name
        }, $true))
        if ($strictCalls.Count -ne 1 -or $assignments[0].Extent.EndOffset -ge $strictCalls[0].Extent.StartOffset) {
            throw 'Capture validity depends on a later Desktop compatibility result.'
        }
    }
    foreach ($message in @('No visible window belonging to the verified package appeared.',
        'No uniquely named configured child start event was observed under the verified Desktop package.',
        'Instrumented launch produced no readable metadata trace.')) {
        $gates = @($ast.FindAll({
            param($node)
            $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq 'Block' -and
            $node.CommandElements.Count -eq 2 -and
            $node.CommandElements[1] -is [Management.Automation.Language.StringConstantExpressionAst] -and
            $node.CommandElements[1].Value -ceq $message
        }, $true))
        if ($gates.Count -ne 1 -or $gates[0].Extent.EndOffset -ge $assignments[0].Extent.StartOffset) {
            throw 'Capture validity bypasses a required GUI/identity/snapshot gate.'
        }
    }
}
Expect-Pass 'native runtime decision consumes capture and cleanup flags, not Desktop success' {
    $decisions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Get-DesktopNativeRunDecision'
    }, $true))
    $expected = @('Get-DesktopNativeRunDecision', '-Reviewed', '$EnableReviewedLaunchCompatibility',
        '-CaptureValidated', '$captureValidated', '-ActivationExited', '$activationExitConfirmed',
        '-CleanupError', '$report.Contains(''cleanupErrorType'')')
    if ($decisions.Count -ne 1 -or $decisions[0].CommandElements.Count -ne $expected.Count) {
        throw 'Native eligibility lost its explicit runtime seam.'
    }
    for ($i = 0; $i -lt $expected.Count; $i++) {
        if ($decisions[0].CommandElements[$i].Extent.Text -cne $expected[$i]) {
            throw 'Native eligibility was coupled to Desktop compatibility.'
        }
    }
    $combines = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Get-DesktopCombinedOutcome'
    }, $true))
    $nativeCalls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Invoke-ControlPlaneNative'
    }, $true))
    if ($combines.Count -ne 1 -or $nativeCalls.Count -ne 1 -or
        $decisions[0].Extent.EndOffset -ge $nativeCalls[0].Extent.StartOffset -or
        $nativeCalls[0].Extent.EndOffset -ge $combines[0].Extent.StartOffset) {
        throw 'Combined status was not derived after the independent native result.'
    }
}

function New-RegistrationFixture([string]$Status = 'Ok') {
    return [pscustomobject]@{Name='OpenAI.Codex'; Version='26.1103.3517.0'; Publisher='CN=Synthetic';
        PackageFullName='OpenAI.Codex_26.1103.3517.0_x64__synthetic';
        PackageFamilyName='OpenAI.Codex_synthetic'; InstallLocation='C:\fixture\package'; Status=$Status}
}
function Invoke-RegistrationFixture($Snapshots, [switch]$ReadFails, [switch]$DelayFails, [switch]$LateRead) {
    $state=@{ snapshots=$Snapshots; index=0; elapsed=0L; delays=0 }
    $result=Wait-DesktopRegistration 'OpenAI.Codex' '26.1103.3517.0' 'CN=Synthetic' -ReadPackages {
        if ($ReadFails) { throw 'PRIVATE_CANARY' }
        $index=[Math]::Min($state.index,$state.snapshots.Count-1); $state.index++
        if ($LateRead) { $state.elapsed=60000L }
        $snapshot=$state.snapshots[$index]
        if ($null -ne $snapshot) { $snapshot }
    } -ReadElapsedMilliseconds { $state.elapsed } -Delay {
        param($Milliseconds)
        if ($DelayFails) { throw 'PRIVATE_CANARY' }
        if ($Milliseconds -lt 1 -or $Milliseconds -gt 2000) { throw 'invalid delay' }
        $state.elapsed+=$Milliseconds; $state.delays++
    }
    return [pscustomobject]@{ result=$result; delays=$state.delays; elapsed=$state.elapsed }
}
Expect-Pass 'registration accepts strict Ok without a wait' {
    $f=Invoke-RegistrationFixture @((New-RegistrationFixture))
    if ($f.result.status -cne 'ready' -or $f.result.attempts -ne 1 -or $f.delays -ne 0 -or
        $f.result.package.Status -cne 'Ok') { throw 'Immediate readiness changed.' }
}
foreach ($pending in @('DeploymentInProgress','Servicing','DeploymentInProgress, Servicing')) {
    Expect-Pass 'only reviewed pending registration can reach strict Ok' {
        $f=Invoke-RegistrationFixture @((New-RegistrationFixture $pending),(New-RegistrationFixture))
        if ($f.result.status -cne 'ready' -or $f.result.attempts -ne 2 -or $f.delays -ne 1 -or
            $f.elapsed -ne 2000 -or $f.result.package.Status -cne 'Ok') { throw 'Pending readiness transition failed.' }
    }
}
Expect-Pass 'persistent pending registration stops at bounded deadline without a package' {
    $f=Invoke-RegistrationFixture @((New-RegistrationFixture 'Servicing'))
    if ($f.result.reason -cne 'registration_readiness_timeout' -or $f.result.status -cne 'blocked' -or
        $null -ne $f.result.package -or $f.elapsed -ne 60000 -or $f.result.attempts -ne 30) { throw 'Unbounded registration wait.' }
}
foreach ($field in @('Name','Version','Publisher','PackageFullName','PackageFamilyName','InstallLocation')) {
    Expect-Pass 'registration rejects identity changes during pending wait' {
        $changed=New-RegistrationFixture; $changed.$field='changed'
        $f=Invoke-RegistrationFixture @((New-RegistrationFixture 'Servicing'),$changed)
        if ($f.result.reason -cne 'registration_identity_mismatch' -or $f.result.status -cne 'blocked' -or
            $null -ne $f.result.package -or $f.result.attempts -ne 2 -or $f.delays -ne 1) { throw 'Identity change was accepted.' }
    }
}
foreach ($invalid in @('','Unknown','Ok, Servicing','Servicing, DeploymentInProgress','Disabled','LicenseIssue','ok')) {
    Expect-Pass 'unreviewed registration status fails without waiting' {
        $f=Invoke-RegistrationFixture @((New-RegistrationFixture $invalid))
        if ($f.result.reason -cne 'registration_status_unreviewed' -or $f.result.status -cne 'blocked' -or
            $null -ne $f.result.package -or $f.delays -ne 0) { throw 'Unknown status was treated as pending.' }
    }
}
Expect-Pass 'missing registration fails without waiting' {
    $f=Invoke-RegistrationFixture @($null)
    if ($f.result.reason -cne 'registration_missing' -or $f.delays -ne 0) { throw 'Missing registration was retried.' }
}
Expect-Pass 'ambiguous registration fails without waiting' {
    $f=Invoke-RegistrationFixture (,@((New-RegistrationFixture),(New-RegistrationFixture)))
    if ($f.result.reason -cne 'registration_ambiguous' -or $f.delays -ne 0) { throw 'Ambiguous registration was accepted.' }
}
Expect-Pass 'registration read exceptions are fixed failures rather than empty results' {
    $f=Invoke-RegistrationFixture @((New-RegistrationFixture)) -ReadFails
    if ($f.result.reason -cne 'registration_read_failed' -or $f.result.status -cne 'blocked' -or
        $null -ne $f.result.package -or ($f | ConvertTo-Json -Depth 5) -match 'PRIVATE_CANARY') { throw 'Read failure changed or leaked.' }
}
Expect-Pass 'registration delay exceptions stop rather than retry' {
    $f=Invoke-RegistrationFixture @((New-RegistrationFixture 'Servicing')) -DelayFails
    if ($f.result.reason -cne 'registration_wait_failed' -or $f.result.attempts -ne 1) { throw 'Wait failure was ignored.' }
}
Expect-Pass 'Ok returned after deadline does not bypass readiness bound' {
    $f=Invoke-RegistrationFixture @((New-RegistrationFixture)) -LateRead
    if ($f.result.reason -cne 'registration_readiness_timeout' -or $null -ne $f.result.package) { throw 'Late read bypassed deadline.' }
}
Expect-Pass 'runtime registration has one read-only readiness seam after provisioning' {
    $waits=@($ast.FindAll({param($node) $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq 'Wait-DesktopRegistration'},$true))
    $installs=@($ast.FindAll({param($node) $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq 'Add-AppxProvisionedPackage'},$true))
    if ($waits.Count -ne 1 -or $installs.Count -ne 1 -or $installs[0].Extent.EndOffset -ge $waits[0].Extent.StartOffset) { throw 'Provisioning was repeated or readiness misplaced.' }
    $elements=$waits[0].CommandElements
    $expected=@('Wait-DesktopRegistration','-ExpectedName','$report.manifest.name','-ExpectedVersion','$report.manifest.version',
        '-ExpectedPublisher','$report.manifest.publisher','-ReadPackages',"{ Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction Stop }",
        '-ReadElapsedMilliseconds','{ $registrationWatch.ElapsedMilliseconds }','-Delay','{ param($Milliseconds) Start-Sleep -Milliseconds $Milliseconds }')
    if ($elements.Count -ne $expected.Count) { throw 'Readiness lost explicit authority.' }
    for ($index=0;$index -lt $expected.Count;$index++) {
        if ($elements[$index].Extent.Text -cne $expected[$index]) { throw 'Readiness callback authority changed.' }
    }
}

Expect-Pass 'package-only mode is explicit and does not change ordinary defaults' {
    Assert-DesktopPackageIdentityOptions $false 'baseline' $false $false $false $false '' ''
    Assert-DesktopPackageIdentityOptions $true 'observe' $true $true $false $false '' 'package-identity-only-v1'
}
foreach ($options in @(
    @($true,'baseline',$true,$true,$false,$false,'','package-identity-only-v1'),
    @($true,'observe',$false,$true,$false,$false,'','package-identity-only-v1'),
    @($true,'observe',$true,$false,$false,$false,'','package-identity-only-v1'),
    @($true,'observe',$true,$true,$true,$false,'','package-identity-only-v1'),
    @($true,'observe',$true,$true,$false,$true,'','package-identity-only-v1'),
    @($true,'observe',$true,$true,$false,$false,'baseline','package-identity-only-v1'),
    @($true,'observe',$true,$true,$false,$false,'','')
)) {
    Expect-Reject 'package-only mode rejects missing or conflicting opt-ins' { Assert-DesktopPackageIdentityOptions @options }
}
Expect-Pass 'package identity always stops before the unchanged pin and installation gates' {
    $reads = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Get-DesktopPackageIdentityEvidence'}, $true))
    if ($reads.Count -ne 3) { throw 'Package identity modes lost their three explicit trust reads.' }
    $reads = @($reads | Where-Object {
        $branch=$_.Parent
        while ($null -ne $branch -and $branch -isnot [Management.Automation.Language.IfStatementAst]) { $branch=$branch.Parent }
        $null -ne $branch -and $branch.Clauses[0].Item1.Extent.Text -ceq '$InspectPackageIdentityOnly'
    })
    if ($reads.Count -ne 1) { throw 'Package identity runtime seam is not unique.' }
    $expected = @('Get-DesktopPackageIdentityEvidence','$package','$report.download.packageSha256',
        '$signatureExit','$report.signature.authenticodeStatus','$report.signature.signerSubject')
    if ($reads[0].CommandElements.Count -ne $expected.Count) { throw 'Package identity authority changed.' }
    for ($i=0; $i -lt $expected.Count; $i++) {
        if ($reads[0].CommandElements[$i].Extent.Text -cne $expected[$i]) { throw 'Package identity authority changed.' }
    }
    $gate = $reads[0].Parent
    while ($null -ne $gate -and $gate -isnot [Management.Automation.Language.IfStatementAst]) { $gate=$gate.Parent }
    if ($null -eq $gate -or $gate.Clauses.Count -ne 1 -or
        $gate.Clauses[0].Item1.Extent.Text -cne '$InspectPackageIdentityOnly') { throw 'Package identity lost its explicit mode.' }
    $last = $gate.Clauses[0].Item2.Statements[-1]
    if ($last.Extent.Text -cne "Block 'Read-only package identity mode stops before installation or execution; package remains unapproved.'") {
        throw 'Package identity could fall through after success or failure.'
    }
    $pins = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and
        $n.Clauses[0].Item1.Extent.Text -ceq '$report.download.packageSha256 -ne ''ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643'''}, $true))
    $trust = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Block' -and $n.CommandElements.Count -eq 2 -and
        $n.CommandElements[1].Extent.Text -ceq "'Official package trust verification failed; package will not be installed or run.'"}, $true))
    $installs = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Add-AppxProvisionedPackage'}, $true))
    if ($pins.Count -ne 1 -or $trust.Count -ne 1 -or $installs.Count -ne 1 -or
        $trust[0].Extent.EndOffset -ge $gate.Extent.StartOffset -or
        $gate.Extent.EndOffset -ge $pins[0].Extent.StartOffset -or
        $pins[0].Extent.EndOffset -ge $installs[0].Extent.StartOffset) { throw 'Package read/pin/install ordering changed.' }
    $exits = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and
        $n.Clauses[0].Item1.Extent.Text -ceq '$InspectPackageIdentityOnly' -and
        $n.Clauses[0].Item2.Statements[-1].Extent.Text -ceq 'exit 1'}, $true))
    $native = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Invoke-ControlPlaneNative'}, $true))
    if ($exits.Count -ne 1 -or $native.Count -ne 1 -or
        $exits[0].Extent.StartOffset -le $gate.Extent.EndOffset -or
        $exits[0].Extent.EndOffset -ge $native[0].Extent.StartOffset) { throw 'Static-only outcome can reach native turns.' }
}
Expect-Pass 'explicit prelaunch not-started evidence cannot manufacture activation exit' {
    $proofs = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$report.desktopActivationCleanup'}, $true))
    if ($proofs.Count -ne 1 -or $proofs[0].Right.Extent.Text -cne "'not-started'") { throw 'Not-started evidence changed.' }
    $guard=$proofs[0].Parent
    while ($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]) { $guard=$guard.Parent }
    $expected = '($InspectPackageIdentityOnly -and $activationPid -eq 0 -and $null -eq $ownedActivationProcess -and ' +
        '$null -eq $subscription -and $observed.Count -eq 0 -and -not $report.Contains(''registration'') -and ' +
        '$null -eq $liveFixture -and $null -eq $winAppTool) -or ' +
        '($EnableReviewedBuild4239 -and -not $aliasActivationAttempted -and $activationPid -eq 0 -and ' +
        '$null -eq $ownedActivationProcess -and $ownedActivationCreatedUtc -ceq '''' -and $null -eq $subscription -and ' +
        '$observed.Count -eq 0 -and -not $report.Contains(''activation'') -and $null -eq $liveFixture -and ' +
        '$null -eq $liveFixtureReport)'
    $actual = $guard.Clauses[0].Item1.Extent.Text -replace '\s+', ' '
    if ($actual -cne $expected) { throw 'Not-started evidence lost a pre-launch proof.' }
    $condition=[scriptblock]::Create($actual)
    foreach ($change in @('none','disabled','pid','process','subscription','observed','registration','fixture','tool')) {
        $EnableReviewedBuild4239=$false; $aliasActivationAttempted=$false
        $InspectPackageIdentityOnly=$true; $activationPid=0; $ownedActivationProcess=$null
        $subscription=$null; $observed=@{}; $report=[ordered]@{}; $liveFixture=$null; $winAppTool=$null
        switch ($change) {
            'disabled' { $InspectPackageIdentityOnly=$false }
            'pid' { $activationPid=1 }
            'process' { $ownedActivationProcess=[pscustomobject]@{} }
            'subscription' { $subscription=[pscustomobject]@{} }
            'observed' { $observed['1']=$true }
            'registration' { $report.registration=[pscustomobject]@{} }
            'fixture' { $liveFixture=[pscustomobject]@{} }
            'tool' { $winAppTool=[pscustomobject]@{} }
        }
        if ((& $condition) -ne ($change -ceq 'none')) { throw 'Uncertain activation was labeled not-started.' }
    }
    foreach ($change in @('none','disabled','attempted','pid','process','created','subscription','observed','activation','fixture','fixture-report')) {
        $InspectPackageIdentityOnly=$false; $EnableReviewedBuild4239=$true; $aliasActivationAttempted=$false
        $activationPid=0; $ownedActivationProcess=$null; $ownedActivationCreatedUtc=''
        $subscription=$null; $observed=@{}; $report=[ordered]@{registration=[pscustomobject]@{}}
        $liveFixture=$null; $liveFixtureReport=$null; $winAppTool=[pscustomobject]@{}
        switch ($change) {
            'disabled' { $EnableReviewedBuild4239=$false }
            'attempted' { $aliasActivationAttempted=$true }
            'pid' { $activationPid=1 }
            'process' { $ownedActivationProcess=[pscustomobject]@{} }
            'created' { $ownedActivationCreatedUtc='uncertain' }
            'subscription' { $subscription=[pscustomobject]@{} }
            'observed' { $observed['1']=$true }
            'activation' { $report.activation=[pscustomobject]@{} }
            'fixture' { $liveFixture=[pscustomobject]@{} }
            'fixture-report' { $liveFixtureReport=[ordered]@{status='not-run'} }
        }
        if ((& $condition) -ne ($change -ceq 'none')) { throw '4239 uncertain activation was labeled not-started.' }
    }
    $markers=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$aliasActivationAttempted'},$true))
    if ($markers.Count -ne 2 -or $markers[0].Right.Extent.Text -cne '$false' -or
        $markers[1].Right.Extent.Text -cne '$true' -or $markers[1].Extent.EndOffset -ge $guard.Extent.StartOffset) {
        throw 'Alias activation attempt provenance changed.'
    }
    $normalized=$ast.Extent.Text.Replace("`r`n","`n")
    if (-not $normalized.Contains("`$aliasActivationAttempted = `$true`n        `$started = [Diagnostics.Process]::Start(`$start)")) {
        throw 'Alias activation can start before recording the attempt.'
    }
    $fabricated = @($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$activationExitConfirmed' -and $n.Right.Extent.Text -ceq '$true'}, $true))
    if ($fabricated.Count -ne 0) { throw 'Activation exit was fabricated.' }
}
# New build admission is a CI input profile, never a wider protocol/policy mode.
Expect-Pass '4239 mode requires exactly read-only bridge inspection and all existing opt-ins' {
    for ($mask=0; $mask -lt 128; $mask++) {
        $flags=@(); for($bit=0; $bit -lt 6; $bit++) { $flags += [bool]($mask -band (1 -shl $bit)) }
        $entry=$(if ($mask -band 64) { 'baseline' } else { '' })
        $accepted=$true
        try { Assert-Desktop4239Options $true 'observe' $flags[0] $flags[1] $flags[2] $flags[3] $flags[4] $flags[5] $entry 'bridge-ui-4239-v1' }
        catch { $accepted=$false }
        if ($accepted -ne ($mask -eq 7)) { throw '4239 accepted a conflicting or missing mode.' }
    }
    Assert-Desktop4239Options $false 'baseline' $false $false $false $false $false $false '' ''
}
foreach ($badMode in @('baseline','bridge')) {
    Expect-Reject '4239 outside observed bridge mode' { Assert-Desktop4239Options $true $badMode $true $true $true $false $false $false '' 'bridge-ui-4239-v1' }
}
Expect-Reject '4239 marker required' { Assert-Desktop4239Options $true 'observe' $true $true $true $false $false $false '' '' }
Expect-Pass '4239 welcome requires the separate explicit bounded-action combination' {
    for ($mask=0; $mask -lt 512; $mask++) {
        $flags=@(); for($bit=0; $bit -lt 9; $bit++) { $flags += [bool]($mask -band (1 -shl $bit)) }
        $entry=$(if ($flags[7]) { 'baseline' } else { '' })
        $accepted=$true
        try { Assert-Desktop4239Options $flags[0] 'observe' $flags[1] $flags[2] $flags[3] $flags[4] $flags[5] $flags[6] $entry 'bridge-ui-4239-v1' $true 'bridge-welcome-4239-v1' $flags[8] }
        catch { $accepted=$false }
        if ($accepted -ne ($mask -eq 279)) { throw 'Welcome accepted conflicting or missing authority.' }
    }
}
foreach ($badMode in @('baseline','bridge')) {
    Expect-Reject 'welcome never becomes a normal-entry or baseline action' {
        Assert-Desktop4239Options $true $badMode $true $true $false $true $false $false '' 'bridge-ui-4239-v1' $true 'bridge-welcome-4239-v1' $true
    }
}
foreach ($badMarker in @('', 'bridge-ui-4239-v1', 'BRIDGE-WELCOME-4239-V1')) {
    Expect-Reject 'welcome requires its exact separate marker' {
        Assert-Desktop4239Options $true 'observe' $true $true $false $true $false $false '' 'bridge-ui-4239-v1' $true $badMarker $true
    }
}
Expect-Reject 'welcome marker alone cannot expand the existing read-only profile' {
    Assert-Desktop4239Options $true 'observe' $true $true $true $false $false $false '' 'bridge-ui-4239-v1' $false 'bridge-welcome-4239-v1' $true
}
Expect-Reject 'welcome still requires the reviewed build marker' {
    Assert-Desktop4239Options $true 'observe' $true $true $false $true $false $false '' '' $true 'bridge-welcome-4239-v1' $true
}
Expect-Pass 'welcome action opt-in is default-off and native four-turn status is explicit' {
    $parameter=@($ast.ParamBlock.Parameters | Where-Object { $_.Name.VariablePath.UserPath -ceq 'EnableReviewedWelcome4239' })
    if ($parameter.Count -ne 1 -or $null -ne $parameter[0].DefaultValue -or
        $parameter[0].StaticType -ne [Management.Automation.SwitchParameter]) { throw 'Welcome action opt-in is not default-off.' }
    $status=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$report.nativeControlPlane' -and
        $n.Right.Extent.Text -ceq "[ordered]@{status='not-run';reason='welcome_transition_only'}"},$true))
    if ($status.Count -ne 1) { throw 'Welcome must not imply independent native inference ran.' }
    $guard=$status[0].Parent
    while ($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]) { $guard=$guard.Parent }
    if ($null -eq $guard -or $guard.Clauses[0].Item1.Extent.Text -cne '$EnableReviewedWelcome4239') {
        throw 'Welcome native status escaped its selected profile.'
    }
}
$newPackage='9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c'
$newBackend='4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'
$newArchive='fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa'
Expect-Pass 'new exact tuple is accepted only by selected diagnostic profile' {
    Assert-DesktopCompatibilityPackage $true '26.1103.4239.0' $newPackage $true
    $e=[pscustomobject]@{status='observed';manifest=[pscustomobject]@{version='26.1103.4239.0'};packageSha256=$newPackage;backend=[pscustomobject]@{sha256=$newBackend};archive=[pscustomobject]@{sha256=$newArchive}}
    Assert-Desktop4239Identity $e
    foreach ($field in @('status','version','package','backend','archive')) {
        $bad=$e|ConvertTo-Json -Depth 6|ConvertFrom-Json
        switch($field) {
            status {$bad.status='unavailable'}
            version {$bad.manifest.version='26.1103.3517.0'}
            package {$bad.packageSha256=$packageHash}
            backend {$bad.backend.sha256=$hash}
            archive {$bad.archive.sha256='0'*64}
        }
        $rejected=$false; try { Assert-Desktop4239Identity $bad } catch { $rejected=$true }
        if (-not $rejected) { throw 'Cross-paired or unavailable signed identity admitted.' }
    }
}
Expect-Reject 'old default cannot admit new package' { Assert-DesktopCompatibilityPackage $true '26.1103.4239.0' $newPackage }
Expect-Reject 'new profile cannot admit old package' { Assert-DesktopCompatibilityPackage $true $packageVersion $packageHash $true }
Expect-Reject 'new version cannot pair with old package' { Assert-DesktopCompatibilityPackage $true '26.1103.4239.0' $packageHash $true }
Expect-Reject 'new package cannot pair with old version' { Assert-DesktopCompatibilityPackage $true $packageVersion $newPackage $true }
Expect-Reject 'new profile never disables compatibility' { Assert-DesktopCompatibilityPackage $false '26.1103.4239.0' $newPackage $true }
Expect-Pass '4239 bridge settings change only the exact native digest' {
    $old=New-DesktopBridgeSettings (New-Backends) $true $true $version $hash 'fixture-control-home'
    $new=New-DesktopBridgeSettings (New-Backends) $true $true $version $newBackend 'fixture-control-home' $true
    if (@($new.backends).Count -ne 2) { throw 'Backend count changed.' }
    foreach($b in $new.backends) {
        if ($b.startup_probe_sha256 -cne $newBackend) { throw 'New backend lost its exact pin.' }
        $b.startup_probe_sha256=$hash
    }
    if (($old|ConvertTo-Json -Depth 6 -Compress) -cne ($new|ConvertTo-Json -Depth 6 -Compress)) { throw '4239 changed a protocol/policy mode.' }
}
foreach($badHash in @($hash,('0'*64),'')) {
    Expect-Reject 'same-version wrong binary rejected in 4239 profile' { New-DesktopBridgeSettings (New-Backends) $true $true $version $badHash 'fixture-control-home' $true }
}
Expect-Reject 'old settings never accept new binary by version alone' { New-DesktopBridgeSettings (New-Backends) $true $true $version $newBackend 'fixture-control-home' }
Expect-Reject '4239 settings cannot skip startup policy proof' { New-DesktopBridgeSettings (New-Backends) $false $true $version $newBackend 'fixture-control-home' $true }
Expect-Reject '4239 settings cannot skip compatibility gate' { New-DesktopBridgeSettings (New-Backends) $true $false $version $newBackend 'fixture-control-home' $true }
Expect-Pass 'new identity proof and exact native pins precede owned runtime seams' {
    $options=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Assert-Desktop4239Options'},$true))
    $expected=@('Assert-Desktop4239Options','$EnableReviewedBuild4239','$Mode','$EnableReviewedStartupProbe',
        '$EnableReviewedLaunchCompatibility','$EnableReadOnlyWinAppInspection','$EnableReviewedLiveWelcome',
        '$EnableNativeReadOnlyInspection','$InspectPackageIdentityOnly','$NativeEntryCase','$env:MAGPIE_REVIEWED_BUILD',
        '$EnableReviewedWelcome4239','$env:MAGPIE_WELCOME_4239','$EnableRendererAccessibility')
    if ($options.Count -ne 1 -or $options[0].CommandElements.Count -ne $expected.Count) { throw '4239 lost its one explicit option gate.' }
    for($i=0;$i -lt $expected.Count;$i++) {
        if ($options[0].CommandElements[$i].Extent.Text -cne $expected[$i]) { throw '4239 option authority changed.' }
    }
    $proof=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Assert-Desktop4239Identity'},$true))
    $installs=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Add-AppxProvisionedPackage'},$true))
    if ($proof.Count -ne 1 -or $proof[0].Extent.Text -cne 'Assert-Desktop4239Identity $report.reviewedBuildIdentity' -or $proof[0].Extent.EndOffset -ge $installs[0].Extent.StartOffset) { throw '4239 trust proof lost its unique pre-install seam.' }
    if ($options[0].Extent.EndOffset -ge $proof[0].Extent.StartOffset) { throw '4239 options no longer guard package admission.' }
    $guard=$proof[0].Parent;while($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]){$guard=$guard.Parent}
    if ($guard.Clauses[0].Item1.Extent.Text -cne '$EnableReviewedBuild4239 -or $EnableReviewedEntry4239') { throw '4239 proof escaped the two independent opt-ins.' }
    $selectedCalls=@{
        'New-DesktopBridgeSettings'='-Build4239 $EnableReviewedBuild4239 -Entry4239 $EnableReviewedEntry4239 -EntryCompatibility $EnableReviewedEntryCompatibility'
        'Assert-DesktopCompatibilityPackage'='-Build4239 ($EnableReviewedBuild4239 -or $EnableReviewedEntry4239)'
        'Wait-LiveReady'='-Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile'
        'Complete-LiveFixture'='-Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile'
        'ConvertTo-LiveConfiguration'='-Build4239 $EnableReviewedBuild4239 -FixtureProfile $DesktopFixtureProfile -Readiness $liveFixtureReport.readiness'
    }
    foreach($name in $selectedCalls.Keys) {
        $call=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq $name},$true))
        if ($call.Count -ne 1 -or -not $call[0].Extent.Text.EndsWith($selectedCalls[$name])) { throw 'Selected build lost its exact runtime propagation.' }
    }
}

Expect-Pass '4239 native entry preserves independent authority and every image oracle' {
    $entryCalls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Assert-EntryOptions'},$true))
    if ($entryCalls.Count -ne 1 -or -not $entryCalls[0].Extent.Text.EndsWith('-WorkflowRef $env:GITHUB_WORKFLOW_REF -Entry4239 $EnableReviewedEntry4239 -ProfileMarker $env:MAGPIE_NATIVE_ENTRY_PROFILE -EntryCompatibility $EnableReviewedEntryCompatibility')) {
        throw 'Entry options lost their explicit workflow and profile authority.'
    }
    $guard=$entryCalls[0].Parent;while($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]){$guard=$guard.Parent}
    if ($guard.Clauses[0].Item1.Extent.Text -cne '$NativeEntryCase -or $EnableReviewedEntry4239 -or $EnableReviewedEntryCompatibility') { throw 'An entry flag without a case can evade admission.' }
    $other=@($entryCalls[0].CommandElements | Where-Object { $_.Extent.Text.StartsWith('($EnableReviewedStartupProbe') })
    $otherText='($EnableReviewedStartupProbe -or $EnableReviewedLaunchCompatibility -or $useLiveFixtures -or $InspectStaticLauncher -or $EnableReviewedBuild4239 -or $EnableNativeReadOnlyInspection -or $InspectPackageIdentityOnly)'
    if ($other.Count -ne 1 -or ($other[0].Extent.Text -replace '\s+',' ') -cne $otherText) { throw 'Entry accepted another experiment opt-in.' }
    $hashCalls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Get-EntryNativeHash'},$true))
    if ($hashCalls.Count -ne 1 -or $hashCalls[0].Extent.Text -cne 'Get-EntryNativeHash -Entry4239 $EnableReviewedEntry4239') { throw 'Entry native identity selection is not unique.' }
    $hashAssignments=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$expectedHash' -and $n.Right.Extent.Text -ceq '$entryNativeHash'},$true))
    if ($hashAssignments.Count -ne 2) { throw 'Both live and event image oracles must use the selected exact native hash.' }
    $nativeGate=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and
        $n.Clauses[0].Item1.Extent.Text.StartsWith('$NativeEntryCase -and ($versionText')},$true))
    if ($nativeGate.Count -ne 1 -or $nativeGate[0].Clauses[0].Item1.Extent.Text -notmatch '-cne\s+\$entryNativeHash\)') {
        throw 'Installed native version check lost its selected digest.'
    }
    if ($hashCalls[0].Extent.StartOffset -le $entryCalls[0].Extent.EndOffset) { throw 'Native hash was selected before entry admission.' }
}

Expect-Pass 'static build selection preserves exact old and new source entry sets' {
    $old=@('.vite/build/early-bootstrap-339-fe39e0119f57b81e.js','.vite/build/early-bootstrap-824-d144f7c160c094ea.js','.vite/build/early-bootstrap-715-fa9cbf44c39465d5.js')
    $new=@('.vite/build/early-bootstrap-339-ce026819bf837ba7.js','.vite/build/early-bootstrap-824-d144f7c160c094ea.js','.vite/build/early-bootstrap-199-e3316cddb4ea6a6b.js')
    if (((Get-DesktopStaticEntries) -join '|') -cne ($old -join '|') -or
        ((Get-DesktopStaticEntries $true) -join '|') -cne ($new -join '|')) { throw 'Static selected source identities changed.' }
    $calls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Get-DesktopStaticEntries'},$true))
    if ($calls.Count -ne 1 -or $calls[0].Extent.Text -cne 'Get-DesktopStaticEntries -Build4239 $EnableReviewedBuild4239') {
        throw 'Static output allowlist lost selected profile.'
    }
    $argumentAssignments=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$readStatic.Arguments'},$true))
    if ($argumentAssignments.Count -ne 2 -or $argumentAssignments[0].Right.Extent.Text -cne '''--max-old-space-size=512 "'' + $inspector + ''" --package-root "'' + $app.InstallLocation + ''"''' -or
        $argumentAssignments[1].Operator -ne [Management.Automation.Language.TokenKind]::PlusEquals -or
        $argumentAssignments[1].Right.Extent.Text -cne ''' --reviewed-build desktop-4239''') { throw 'Static CLI path/profile authority changed.' }
    $guard=$argumentAssignments[1].Parent;while($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]){$guard=$guard.Parent}
    if ($guard.Clauses[0].Item1.Extent.Text -cne '$EnableReviewedBuild4239') { throw 'Static profile escaped explicit opt-in.' }
}

Expect-Pass 'renderer accessibility is empty by default and one fixed argument when selected' {
    foreach ($build in @($false,$true)) {
        foreach ($entry in @($false,$true)) {
            if ((Get-DesktopAccessibilityArguments $false $build $entry) -cne '') { throw 'Disabled option changed launch arguments.' }
        }
    }
    if ((Get-DesktopAccessibilityArguments $true $true $false) -cne '--force-renderer-accessibility') {
        throw 'Selected option did not return the sole reviewed switch.'
    }
}
foreach ($pair in @(@($false,$false),@($false,$true),@($true,$true))) {
    Expect-Reject 'renderer accessibility cannot enter another launch profile' {
        Get-DesktopAccessibilityArguments $true $pair[0] $pair[1]
    }
}
Expect-Pass 'renderer accessibility has one default-off preflight and only feeds the alias launch' {
    $parameter=@($ast.ParamBlock.Parameters | Where-Object { $_.Name.VariablePath.UserPath -ceq 'EnableRendererAccessibility' })
    if ($parameter.Count -ne 1 -or $null -ne $parameter[0].DefaultValue -or
        $parameter[0].StaticType -ne [Management.Automation.SwitchParameter]) { throw 'Accessibility opt-in is not default-off.' }
    $calls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Get-DesktopAccessibilityArguments'},$true))
    if ($calls.Count -ne 1 -or $calls[0].Extent.Text -cne 'Get-DesktopAccessibilityArguments $EnableRendererAccessibility $EnableReviewedBuild4239 $EnableReviewedEntry4239') {
        throw 'Accessibility lost explicit profile selection.'
    }
    $authority=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'Assert-Desktop4239Options'},$true))
    $arguments=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$start.Arguments'},$true))
    if ($authority.Count -ne 1 -or $arguments.Count -ne 1 -or
        $arguments[0].Right.Extent.Text -cne '$desktopAccessibilityArguments' -or
        $authority[0].Extent.EndOffset -ge $calls[0].Extent.StartOffset -or
        $calls[0].Extent.EndOffset -ge $arguments[0].Extent.StartOffset) { throw 'Accessibility authority or sole argv assignment changed.' }
    $alias=$ast.Extent.Text.IndexOf('$start.FileName = $alias; $start.UseShellExecute = $false',[StringComparison]::Ordinal)
    $start=$ast.Extent.Text.IndexOf('$started = [Diagnostics.Process]::Start($start)',[StringComparison]::Ordinal)
    if ($alias -lt 0 -or $start -lt 0 -or $arguments[0].Extent.StartOffset -le $alias -or
        $arguments[0].Extent.EndOffset -ge $start) { throw 'Accessibility escaped the registered-alias launch.' }
    $assignments=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$desktopAccessibilityArguments'},$true))
    if ($assignments.Count -ne 1) { throw 'Accessibility arguments can be overwritten.' }
}

Expect-Pass 'normal-entry compatibility remains default-off and settings-only' {
    $parameter=@($ast.ParamBlock.Parameters | Where-Object { $_.Name.VariablePath.UserPath -ceq 'EnableReviewedEntryCompatibility' })
    if ($parameter.Count -ne 1 -or $null -ne $parameter[0].DefaultValue -or
        $parameter[0].StaticType -ne [Management.Automation.SwitchParameter]) { throw 'Entry compatibility is not default-off.' }
    $settings=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
        $n.GetCommandName() -ceq 'New-DesktopBridgeSettings'},$true))
    if ($settings.Count -ne 1 -or -not $settings[0].Extent.Text.Contains('-LaunchCompatibility ($EnableReviewedLaunchCompatibility -or $EnableReviewedEntryCompatibility)')) {
        throw 'Actual entry configuration did not select the reviewed modes.'
    }
    $homes=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$controlHome' -and $n.Right.Extent.Text.StartsWith('Join-Path')},$true))
    if ($homes.Count -ne 1) { throw 'Separate control home creation is not unique.' }
    $guard=$homes[0].Parent;while($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]){$guard=$guard.Parent}
    if ($guard.Clauses[0].Item1.Extent.Text -cne '$EnableReviewedLaunchCompatibility -or $EnableReviewedEntryCompatibility') { throw 'Entry modes lack their fresh owned control home.' }
    $reported=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$report.bridge.launchCompatibilityEnabled'},$true))
    if ($reported.Count -ne 1 -or $reported[0].Right.Extent.Text -cne '($EnableReviewedLaunchCompatibility -or $EnableReviewedEntryCompatibility)') { throw 'Report does not reflect actual selected modes.' }
    $globalWrites=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$EnableReviewedLaunchCompatibility'},$true))
    if ($globalWrites.Count -ne 0) { throw 'Entry escalated to the separate UI/fixture compatibility workflow.' }
    $workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-desktop-native-entry-4239.yml'))
    if (-not $workflow.Contains('MAGPIE_NATIVE_ENTRY_PROFILE: user-aumid-4239-compat-v1') -or
        -not $workflow.Contains('-NativeEntryCase treatment -EnableReviewedEntry4239 -EnableReviewedEntryCompatibility') -or
        $workflow.Contains('-EnableReviewedLaunchCompatibility')) { throw 'Workflow does not select the independent entry compatibility contract.' }
    if (-not $ast.Extent.Text.Contains("startupCompatibility='not-verified-review-screenshot'") -or
        -not $ast.Extent.Text.Contains("'magpie-codex-bridge: app-server bridge failed'")) { throw 'Startup result lost its honest review limit or existing fixed failure marker.' }
}

# Pure option checks: no supplied catalog, not even an empty one, in self-authored mode.
Assert-DesktopFixtureProfile 'synthetic-models-4239' $false 'observe' $true $true $true $false $false $false '' 'desktop-synthetic-models-v1'
foreach ($argsBad in @(
    @('synthetic-models-4239',$true,'observe',$true,$true,$true,$false,$false,$false,'','desktop-synthetic-models-v1'),
    @('synthetic-models-4239',$false,'observe',$false,$true,$true,$false,$false,$false,'','desktop-synthetic-models-v1'),
    @('synthetic-models-4239',$false,'observe',$true,$false,$true,$false,$false,$false,'','desktop-synthetic-models-v1'),
    @('synthetic-models-4239',$false,'observe',$true,$true,$true,$false,$false,$false,'',''),
    @('',$false,'observe',$true,$true,$true,$false,$false,$false,'','desktop-synthetic-models-v1'))) {
    $rejected=$false
    try { Assert-DesktopFixtureProfile @argsBad } catch { $rejected=$true }
    if (-not $rejected) { throw 'Self-authored Desktop mixed-profile/catalog input accepted.' }
}
$profileCall=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
    $n.GetCommandName() -ceq 'Assert-DesktopFixtureProfile'},$true))
if ($profileCall.Count -ne 1 -or -not $profileCall[0].Extent.Text.Contains("`$PSBoundParameters.ContainsKey('NativeControlCatalog')")) {
    throw 'Desktop fixture profile must reject argument presence before setup.'
}
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-desktop-policy-trace.yml'))
if (-not $workflow.Contains("if: env.MAGPIE_DESKTOP_SYNTHETIC_MODELS == ''") -or
    -not $workflow.Contains('-DesktopFixtureProfile synthetic-models-4239') -or
    -not $ast.Extent.Text.Contains("'-fixture-profile','synthetic-models-4239'")) {
    throw 'Self-authored Desktop must bypass legacy download and select its exact helper mode.'
}
# New mode requires both independent explicit markers; no catalog argument is legal.
$ownedArgs=@('bridge-owned-catalog-4239',$false,'observe',$true,$true,$true,$false,$false,$false,'','desktop-synthetic-models-v1','bridge-owned-catalog-v1')
Expect-Pass 'owned catalog exact profile' { Assert-DesktopFixtureProfile @ownedArgs }
foreach ($index in @(1,3,4,5,6,7,8,10,11)) {
    $badArgs=$ownedArgs.Clone()
    if ($index -cin @(1,6,7,8)) { $badArgs[$index]=$true }
    elseif ($index -cin @(10,11)) { $badArgs[$index]='' }
    else { $badArgs[$index]=$false }
    Expect-Reject 'owned profile rejects each missing or mixed gate' { Assert-DesktopFixtureProfile @badArgs }
}
$badArgs=$ownedArgs.Clone();$badArgs[0]='synthetic-models-4239'
Expect-Reject 'old profile rejects owned marker' { Assert-DesktopFixtureProfile @badArgs }
$ownedEvent='{"seq":1,"pid":2,"kind":"rpc","direction":"from_bridge","method":"config/read","correlation":3,"class":"response","ownedModelCatalog":{"schema":1,"pathMatches":true,"digestMatches":true,"validated":true}}' | ConvertFrom-Json
Expect-Pass 'owned response fixed booleans' { Assert-PublicOwnedCatalogEvent $ownedEvent 'bridge-owned-catalog-4239' }
Expect-Reject 'owned response forbidden outside profile' { Assert-PublicOwnedCatalogEvent $ownedEvent }
foreach ($field in @('pathMatches','digestMatches','validated')) {
    $bad=($ownedEvent | ConvertTo-Json -Depth 5 -Compress) | ConvertFrom-Json
    $bad.ownedModelCatalog.$field='PRIVATE_CANARY'
    Expect-Reject 'owned response no raw strings' { Assert-PublicOwnedCatalogEvent $bad 'bridge-owned-catalog-4239' }
}
$ownedPathEvent='{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"fs/readFile","correlation":3,"class":"request","tracePresent":true,"traceValid":true,"fsReadParamsShapeValid":true,"ownedCatalogPathMatches":false}' | ConvertFrom-Json
Expect-Pass 'owned file request only shape and path equality' {
    Assert-PublicOwnedCatalogEvent $ownedPathEvent 'bridge-owned-catalog-4239'
    Assert-PublicReadRequestEvent $ownedPathEvent
}
$ownedPathEvent | Add-Member -NotePropertyName fsReadBasename -NotePropertyValue 'other'
Expect-Reject 'owned file request cannot also carry generic path metadata' { Assert-PublicOwnedCatalogEvent $ownedPathEvent 'bridge-owned-catalog-4239' }
if (-not $workflow.Contains('-DesktopFixtureProfile bridge-owned-catalog-4239') -or
    -not $workflow.Contains('PUSH_OWNED_CATALOG') -or
    -not $ast.Extent.Text.Contains("`$bridgeSettings.bridge_model_catalog_json = `$liveOwned.OwnedCatalogPath") -or
    -not $ast.Extent.Text.Contains("`$start.EnvironmentVariables['MAGPIE_CI_OWNED_MODEL_CATALOG']")) { throw 'Owned catalog fixture wiring missing.' }
# Selection is independent of the unchanged fixture/menu-only profile.
Expect-Pass 'model selection explicit additional profile' {
    Assert-DesktopModelSelectionProfile 'bridge-owned-model-selection-4239' 'bridge-owned-catalog-4239' 'bridge-model-selection-v1'
}
Expect-Pass 'default does not enable selection' { Assert-DesktopModelSelectionProfile '' 'bridge-owned-catalog-4239' '' }
foreach ($selectionArgs in @(
    @('','bridge-owned-catalog-4239','bridge-model-selection-v1'),
    @('bridge-owned-model-selection-4239','bridge-owned-catalog-4239',''),
    @('bridge-owned-model-selection-4239','synthetic-models-4239','bridge-model-selection-v1'),
    @('BRIDGE-OWNED-MODEL-SELECTION-4239','bridge-owned-catalog-4239','bridge-model-selection-v1'),
    @('bridge-owned-model-selection-4239','bridge-owned-catalog-4239','PRIVATE_CANARY')
)) { Expect-Reject 'selection rejects every mixed or missing authority' { Assert-DesktopModelSelectionProfile @selectionArgs } }
Expect-Pass 'model selection reaches only one explicit post-menu seam' {
    $gate=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Assert-DesktopModelSelectionProfile'},$true))
    $selection=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-WinAppModelSelection'},$true))
    if ($gate.Count -ne 1 -or $gate[0].Extent.Text -cne 'Assert-DesktopModelSelectionProfile $DesktopModelSelectionProfile $DesktopFixtureProfile $env:MAGPIE_MODEL_SELECTION' -or
        $selection.Count -ne 1 -or $gate[0].Extent.EndOffset -ge $selection[0].Extent.StartOffset -or
        -not $selection[0].Extent.Text.EndsWith('$liveModelMenu $selectionDiagnostic $DesktopModelSelectionProfile $DesktopFixtureProfile $captureSelection')) {
        throw 'Selection lost unique explicit parameter propagation.'
    }
    $conditional=$selection[0].Parent;$conditions=@()
    while ($null -ne $conditional) {
        if ($conditional -is [Management.Automation.Language.IfStatementAst]) { $conditions+=@($conditional.Clauses | ForEach-Object { $_.Item1.Extent.Text }) }
        $conditional=$conditional.Parent
    }
    if (-not ($conditions -ccontains "`$DesktopModelSelectionProfile -ceq 'bridge-owned-model-selection-4239'") -or
        -not (($conditions -join ' ').Contains('$modelMenuAfterCaptured')) -or
        -not (($conditions -join ' ').Contains('$liveFixtureReport.partialActionUncertain'))) { throw 'Selection escaped confirmed captured menu scope.' }
    $captures=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.AssignmentStatementAst] -and
        $n.Left.Extent.Text -ceq '$captureWelcomeWindow'},$true))
    $captureParameter='param([ValidateSet(''official-window-after-welcome.png'',''official-window-after-skip.png'',''official-window-after-skip-confirmation.png'',''official-window-after-model-menu.png'',''official-window-after-beta-selection.png'',''official-window-after-alpha-selection.png'')][string]$FileName)'
    if ($captures.Count -ne 1 -or -not $captures[0].Right.Extent.Text.Contains($captureParameter)) {
        throw 'Selection captures must be admitted by the exact existing fixed-name capture boundary.'
    }
    foreach ($captureFile in @('official-window-after-beta-selection.png','official-window-after-alpha-selection.png')) {
        $artifactPath='${{ runner.temp }}/desktop-cold-start-report/'+$captureFile
        if ([regex]::Matches($workflow,[regex]::Escape($artifactPath)).Count -ne 1) {
            throw 'Each fixed selection screenshot must be delivered exactly once by the existing artifact list.'
        }
    }
    if (-not $workflow.Contains('PUSH_MODEL_SELECTION') -or -not $workflow.Contains('MAGPIE_MODEL_SELECTION:') -or
        -not $workflow.Contains('-DesktopModelSelectionProfile $selectionProfile')) { throw 'Selection workflow authority missing.' }
}
Write-Output ('DESKTOP_CI_PROFILE_TESTS_PASSED=' + $script:passed)

