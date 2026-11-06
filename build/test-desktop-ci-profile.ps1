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
foreach ($name in @('Assert-DesktopCIOptions', 'Assert-DesktopCompatibilityPackage', 'New-DesktopBridgeSettings',
    'Get-DesktopNoAuthStatusEvidence', 'Assert-PublicShape', 'Assert-PublicInteger',
    'Assert-PublicRequestTraceEvent', 'Assert-PublicReadTargetEvent', 'Get-PublicReadTargetLabels', 'Assert-PublicReadRequestEvent', 'Get-DesktopFeatureListEvidence', 'Get-DesktopConfigReadEvidence',
    'New-DesktopReadTargetEnvironment', 'Complete-DesktopActivationProcess', 'ConvertTo-DesktopNativeControlEvidence', 'Assert-PublicHash', 'Assert-PublicCanonicalJSON', 'ConvertTo-DesktopReadCallerEvidence', 'ConvertFrom-DesktopReadCallerOutput')) {
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

# Configuration fixtures contain only shape flags, never native config or result bodies.
function New-ConfigReadRequest([int]$PidValue, [int]$Correlation) {
    return [pscustomobject]@{
        seq = 1; pid = $PidValue; kind = 'rpc'; direction = 'to_bridge'
        method = 'config/read'; correlation = $Correlation; class = 'request'
        tracePresent = $true; traceValid = $true; configParamsValid = $true
        configIncludeLayers = $false; configCwdProvided = $false
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
    Assert-Keys ([pscustomobject]$evidence) @('requests', 'validatedResponses')
    if ($evidence.requests -ne 3 -or $evidence.validatedResponses -ne 3) { throw 'Wrong config-read counts.' }
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
foreach ($field in @('configIncludeLayers', 'configCwdProvided')) {
    Expect-Reject ('configuration scope remains unsupported: ' + $field) {
        $request = New-ConfigReadRequest 1 1; $request.$field = $true
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
# The published summary stops at 256 records; evidence must consume the full snapshot.
$prefix = @((New-ConfigReadRequest 1 1), (New-ConfigReadResponse 1 1))
for ($index = 3; $index -le 300; $index++) {
    $prefix += [pscustomobject]@{ seq = $index; pid = 1; kind = 'stream_end'; direction = 'stderr'; class = 'eof' }
}
Expect-Pass 'config-read requests after the summary cutoff are still counted' {
    $request = New-ConfigReadRequest 1 2; $request.seq = 301
    $response = New-ConfigReadResponse 1 2; $response.seq = 302
    $evidence = Get-DesktopConfigReadEvidence ($prefix + @($request, $response))
    if ($evidence.requests -ne 2 -or $evidence.validatedResponses -ne 2) { throw 'Truncated evidence.' }
}
Expect-Reject 'outstanding config-read request beyond the summary cutoff fails' {
    $request = New-ConfigReadRequest 1 2; $request.seq = 301
    Get-DesktopConfigReadEvidence ($prefix + @($request))
}
Expect-Reject 'native config-read error beyond the summary cutoff fails' {
    $request = New-ConfigReadRequest 1 2; $request.seq = 301
    $response = New-ConfigReadResponse 1 2; $response.seq = 302; $response.kind = 'rpc_error'; $response.class = 'unknown'
    Get-DesktopConfigReadEvidence ($prefix + @($request, $response))
}
Expect-Pass 'runtime config-read evidence consumes the full sanitized snapshot once' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Get-DesktopConfigReadEvidence'
    }, $true))
    if ($calls.Count -ne 1 -or $calls[0].CommandElements.Count -ne 2 -or
        $calls[0].CommandElements[1] -isnot [Management.Automation.Language.VariableExpressionAst] -or
        $calls[0].CommandElements[1].VariablePath.UserPath -cne 'allEvents') {
        throw 'ConfigRead evidence must consume the full sanitized snapshot.'
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
Expect-Pass 'runtime root construction has one explicit harness provenance seam' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'New-DesktopReadTargetEnvironment'
    }, $true))
    $expected = @('New-DesktopReadTargetEnvironment', '-Backends', '$backends', '-Workspace',
        '$work', '-VerifiedPackage', '$app.InstallLocation', '-ControlHome', '$controlHome')
    if ($calls.Count -ne 1 -or $calls[0].CommandElements.Count -ne $expected.Count) {
        throw 'Runtime root references lost their explicit provenance.'
    }
    for ($i = 0; $i -lt $expected.Count; $i++) {
        if ($calls[0].CommandElements[$i].Extent.Text -cne $expected[$i]) {
            throw 'Runtime root references lost their explicit provenance.'
        }
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
        config_reads = 3; catalog_models = 2; threads = 2; turns = 4; assistant_messages = 4
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
foreach ($field in @('config_reads', 'catalog_models', 'threads', 'turns', 'assistant_messages', 'thread_reads',
    'ownership_rejections', 'alpha_requests', 'beta_requests', 'fixture_errors', 'unexpected_server_requests')) {
    foreach ($bad in @(-1, 99, 1.5, 'PRIVATE_CANARY', $null)) {
        Expect-Reject ('native success requires exact fixture count: ' + $field) {
            $value = New-NativeControlSuccess; $value.$field = $bad
            ConvertTo-DesktopNativeControlEvidence $value
        }
    }
    Expect-Reject ('native count cannot hide an incomplete or extra operation: ' + $field) {
        $value = New-NativeControlSuccess; $value.$field++
        ConvertTo-DesktopNativeControlEvidence $value
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
Expect-Pass 'native slice has one explicit guarded invocation after Desktop cleanup' {
    $calls = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.CommandAst] -and
        $node.GetCommandName() -ceq 'Invoke-ControlPlaneNative'
    }, $true))
    if ($calls.Count -ne 1) { throw 'Native slice must be invoked exactly once.' }
    $guard = $calls[0].Parent
    while ($null -ne $guard -and $guard -isnot [Management.Automation.Language.IfStatementAst]) { $guard = $guard.Parent }
    # The closest if rejects missing activation-exit confirmation or any cleanup error.
    if ($null -eq $guard -or $guard.Clauses[0].Item1.Extent.Text -cne
        '-not $activationExitConfirmed -or $report.Contains(''cleanupErrorType'')') {
        throw 'Native slice bypassed confirmed activation-process exit.'
    }
    $outer = $guard.Parent
    while ($null -ne $outer -and $outer -isnot [Management.Automation.Language.IfStatementAst]) { $outer = $outer.Parent }
    if ($null -eq $outer -or $outer.Clauses[0].Item1.Extent.Text -cne '$EnableReviewedLaunchCompatibility -and $exitCode -eq 0') {
        throw 'Native slice bypassed explicit profile or Desktop success.'
    }
    $cleanup = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.TryStatementAst] -and $null -ne $node.Finally -and
        $node.Finally.Extent.Text -match 'CloseMainWindow'
    }, $true))
    if ($cleanup.Count -ne 1 -or $cleanup[0].Extent.EndOffset -ge $calls[0].Extent.StartOffset) {
        throw 'Native slice started before actual Desktop cleanup.'
    }
}


Expect-Pass 'native wrapper accepts the exact independent success projection' {
    Assert-ControlPlaneNativeSummary (New-NativeControlSuccess)
}
foreach ($failure in @('native_error', 'config_shape_mismatch', 'ownership_changed', 'control_home_mismatch', 'supervision_failed')) {
    Expect-Pass ('native wrapper keeps a fixed failure without converting it to success: ' + $failure) {
        $value = New-NativeControlSuccess; $value.status = 'failed'; $value.stage = 'supervisor'; $value.failure = $failure
        $value.cleanup_complete = $false
        Assert-ControlPlaneNativeSummary $value
    }
}
foreach ($field in @('scope', 'status', 'stage', 'failure', 'native_sha256')) {
    Expect-Reject ('native failure report cannot publish arbitrary labels: ' + $field) {
        $value = New-NativeControlSuccess; $value.status = 'failed'; $value.failure = 'native_error'
        $value.$field = 'PRIVATE_CANARY'
        Assert-ControlPlaneNativeSummary $value
    }
}
foreach ($field in @('config_reads', 'catalog_models', 'threads', 'turns', 'assistant_messages', 'thread_reads',
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

Write-Output ('DESKTOP_CI_PROFILE_TESTS_PASSED=' + $script:passed)
