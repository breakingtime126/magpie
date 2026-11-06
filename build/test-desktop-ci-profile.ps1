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
    'Assert-PublicRequestTraceEvent', 'Assert-PublicReadRequestEvent', 'Get-DesktopFeatureListEvidence',
    'Assert-PublicHash', 'Assert-PublicCanonicalJSON', 'ConvertTo-DesktopReadCallerEvidence', 'ConvertFrom-DesktopReadCallerOutput')) {
    $definitions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($definitions.Count -ne 1) { throw 'FAIL: missing or duplicated CI configuration helper' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
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
    $settings = New-DesktopBridgeSettings $backends $true $true $version $hash
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
    }
    Assert-Keys $json (@('backends') + @($expected.Keys))
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

Write-Output ('DESKTOP_CI_PROFILE_TESTS_PASSED=' + $script:passed)
