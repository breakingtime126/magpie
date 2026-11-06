# Offline, synthetic fixtures only. Import allowlisted function definitions, never execute the cold-start probe.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$sourcePath = Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($sourcePath, [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: cold-start syntax' }
$functions = @('Assert-PublicShape', 'Assert-PublicInteger', 'Assert-PublicHash', 'Get-PublicDigest',
    'Assert-PublicCanonicalJSON', 'Get-PublicCatalogFailureCodes', 'Assert-PublicCatalogFailure', 'ConvertFrom-PublicRPCMethodFacts', 'Get-PublicRPCMethods', 'Assert-PublicRequestTraceEvent', 'Assert-PublicAuthRequestEvent', 'Assert-PublicReadRequestEvent', 'Get-PublicPluginShapes', 'Assert-PublicPluginShapeEvent', 'Get-PublicASTSkipReasons', 'Assert-PublicAnalysis', 'Get-PublicMarketplaceRoot', 'Assert-PublicManifestPath', 'Get-PublicPluginManifestRoot', 'Assert-PublicMCPRelation', 'ConvertTo-PublicPluginEnvironment', 'Get-BoundedStreamDigest',
    'Assert-PublicRegularPath', 'Assert-PublicZipIndex', 'Get-PublicZipEntryDigest', 'Confirm-PublicCatalogProvenance',
    'Assert-PublicEnvironmentSize', 'Assert-PublicPluginEvent')
foreach ($name in $functions) {
    $definitions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($definitions.Count -ne 1) { throw 'FAIL: missing or duplicated validation function' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
$script:passed = 0
function Expect-Pass([string]$name, [scriptblock]$action, [switch]$ReportFixtureStage) {
    try { $null = & $action }
    catch {
        # This opt-in reports only fixed synthetic-fixture stages, never exception text or source values.
        if ($ReportFixtureStage) {
            $allowed=@('fixture_setup', 'node_execution', 'json_decoding', 'canonical_json',
                'public_report_shape', 'candidate_expectations', 'catalog_validation', 'candidate_relations',
                'zip_creation', 'zip_entry_completeness', 'provenance')
            $stage='unclassified'
            if ($allowed -ccontains $script:nodeFixtureStage) { $stage=$script:nodeFixtureStage }
            throw ('FAIL: ' + $name + '; fixed stage: ' + $stage)
        }
        throw ('FAIL: ' + $name)
    }
    $script:passed++
}
function Expect-Reject([string]$name, [scriptblock]$action) {
    $rejected = $false
    try { $null = & $action }
    catch {
        if ($_.Exception.Message.Contains('PRIVATE_CANARY')) { throw ('FAIL: leaked input in ' + $name) }
        $rejected = $true
    }
    if (-not $rejected) { throw ('FAIL: accepted ' + $name) }
    $script:passed++
}
Expect-Pass 'synthetic fixture failure details expose only fixed stages' {
    foreach ($stage in @('zip_creation', 'PRIVATE_CANARY')) {
        $script:nodeFixtureStage=$stage
        $message=''
        try { Expect-Pass 'synthetic fixture' { throw 'PRIVATE_CANARY' } -ReportFixtureStage }
        catch { $message=$_.Exception.Message }
        $expected='FAIL: synthetic fixture; fixed stage: unclassified'
        if ($stage -ceq 'zip_creation') { $expected='FAIL: synthetic fixture; fixed stage: zip_creation' }
        if ($message -cne $expected) { throw 'Synthetic failure stage was not safely classified.' }
    }
}

function New-AuthRequestEvent([bool]$Valid = $true) {
    $record = [pscustomobject][ordered]@{
        seq = 1; pid = 2; kind = 'rpc'; direction = 'to_bridge'; method = 'getAuthStatus'
        correlation = 3; class = 'request'; authParamsValid = $Valid
    }
    if ($Valid) {
        $record | Add-Member -NotePropertyName includeTokenRequested -NotePropertyValue $false
        $record | Add-Member -NotePropertyName refreshTokenRequested -NotePropertyValue $false
    }
    return $record
}
Expect-Pass 'strict authentication request flags and invalid-only shape' {
    foreach ($include in @($false, $true)) {
        foreach ($refresh in @($false, $true)) {
            $record = New-AuthRequestEvent
            $record.includeTokenRequested = $include
            $record.refreshTokenRequested = $refresh
            $text = $record | ConvertTo-Json -Compress
            Assert-PublicCanonicalJSON $text $record
            if (@(Assert-PublicAuthRequestEvent $record).Count -ne 0) {
                throw 'Authentication validation published unexpected output.'
            }
        }
    }
    Assert-PublicAuthRequestEvent (New-AuthRequestEvent $false)
}
Expect-Pass 'ordinary observer metadata without request flags remains unchanged' {
    Assert-PublicAuthRequestEvent ([pscustomobject]@{
        seq = 1; pid = 2; kind = 'rpc'; direction = 'from_bridge'; method = 'getAuthStatus'
        correlation = 3; class = 'response'
    })
}
foreach ($field in @('authParamsValid', 'includeTokenRequested', 'refreshTokenRequested')) {
    foreach ($value in @($null, 0, 1, 'false', 'PRIVATE_CANARY', @($false), [pscustomobject]@{ secret = 'PRIVATE_CANARY' })) {
        Expect-Reject ('authentication non-boolean ' + $field) {
            $record = New-AuthRequestEvent; $record.$field = $value
            Assert-PublicAuthRequestEvent $record
        }
    }
    Expect-Reject ('authentication missing ' + $field) {
        $record = New-AuthRequestEvent; $record.PSObject.Properties.Remove($field)
        Assert-PublicAuthRequestEvent $record
    }
    Expect-Reject ('authentication wrong field casing ' + $field) {
        $record = New-AuthRequestEvent
        $value = $record.$field
        $record.PSObject.Properties.Remove($field)
        $record | Add-Member -NotePropertyName $field.ToUpperInvariant() -NotePropertyValue $value
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($field in @('includeTokenRequested', 'refreshTokenRequested')) {
    Expect-Reject ('invalid authentication params with a requested flag: ' + $field) {
        $record = New-AuthRequestEvent $false
        $record | Add-Member -NotePropertyName $field -NotePropertyValue $false
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($row in @(
    @('kind', 'rpc_error'), @('kind', 'argv_config'), @('direction', 'from_bridge'),
    @('direction', 'stderr'), @('method', 'account/read'), @('method', 'GetAuthStatus'),
    @('method', 'getAuthStatus/PRIVATE_CANARY'), @('class', 'response'),
    @('class', 'notification'), @('class', 'duplicate_id'), @('class', 'correlation_limit'),
    @('class', 'unrecognized'), @('class', 'oversized'))) {
    Expect-Reject 'authentication flags outside the exact client request' {
        $record = New-AuthRequestEvent; $record.($row[0]) = $row[1]
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($field in @('seq', 'pid', 'correlation')) {
    foreach ($value in @(0, -1, 4294967297, 1.5, '1', 'PRIVATE_CANARY')) {
        Expect-Reject ('authentication invalid identity: ' + $field) {
            $record = New-AuthRequestEvent; $record.$field = $value
            Assert-PublicAuthRequestEvent $record
        }
    }
}
foreach ($field in @('params', 'result', 'error', 'token', 'includeToken', 'refreshToken',
    'configKey', 'requestAttestation', 'PRIVATE_CANARY')) {
    Expect-Reject ('authentication unexpected field: ' + $field) {
        $record = New-AuthRequestEvent
        $record | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($json in @(
    '{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"getAuthStatus","correlation":3,"class":"request","authParamsValid":false,"authParamsValid":true,"includeTokenRequested":false,"refreshTokenRequested":false}',
    '{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"getAuthStatus","correlation":3,"class":"request","authParamsValid":true,"includeTokenRequested":true,"\u0069ncludeTokenRequested":false,"refreshTokenRequested":false}')) {
    Expect-Reject 'duplicate authentication metadata is never published' {
        $record = $json | ConvertFrom-Json
        Assert-PublicCanonicalJSON $json $record
        Assert-PublicAuthRequestEvent $record
    }
}


function New-TraceRequestEvent([string]$Method = 'getAuthStatus', [bool]$Valid = $true) {
    if ($Method -ceq 'getAuthStatus' -and $Valid) { $record = New-AuthRequestEvent }
    else {
        $record = [pscustomobject][ordered]@{
            seq = 1; pid = 2; kind = 'rpc'; direction = 'to_bridge'; method = $Method
            correlation = 3; class = 'request'
        }
    }
    $record | Add-Member -NotePropertyName tracePresent -NotePropertyValue $true
    $record | Add-Member -NotePropertyName traceValid -NotePropertyValue $Valid
    return $record
}
Expect-Pass 'native trace metadata contains only fixed presence and validity' {
    foreach ($method in @('getAuthStatus', 'configRequirements/read')) {
        foreach ($valid in @($false, $true)) {
            $record = New-TraceRequestEvent $method $valid
            Assert-PublicCanonicalJSON ($record | ConvertTo-Json -Compress) $record
            if (@(Assert-PublicAuthRequestEvent $record).Count -ne 0) {
                throw 'Native trace validation published unexpected output.'
            }
        }
    }
    $record = New-TraceRequestEvent
    $record.authParamsValid = $false
    $record.PSObject.Properties.Remove('includeTokenRequested')
    $record.PSObject.Properties.Remove('refreshTokenRequested')
    Assert-PublicAuthRequestEvent $record
}
foreach ($field in @('tracePresent', 'traceValid')) {
    foreach ($value in @($null, 0, 1, 'true', 'PRIVATE_CANARY', @($true), [pscustomobject]@{ secret='PRIVATE_CANARY' })) {
        Expect-Reject ('native trace non-boolean: ' + $field) {
            $record = New-TraceRequestEvent; $record.$field = $value
            Assert-PublicAuthRequestEvent $record
        }
    }
    Expect-Reject ('native trace missing companion: ' + $field) {
        $record = New-TraceRequestEvent; $record.PSObject.Properties.Remove($field)
        Assert-PublicAuthRequestEvent $record
    }
    Expect-Reject ('native trace wrong casing: ' + $field) {
        $record = New-TraceRequestEvent
        $value = $record.$field; $record.PSObject.Properties.Remove($field)
        $record | Add-Member -NotePropertyName $field.ToUpperInvariant() -NotePropertyValue $value
        Assert-PublicAuthRequestEvent $record
    }
}
Expect-Reject 'absent trace must not invent an observed carrier' {
    $record = New-TraceRequestEvent; $record.tracePresent = $false
    Assert-PublicAuthRequestEvent $record
}
Expect-Reject 'invalid native trace must not claim authentication flags' {
    $record = New-TraceRequestEvent; $record.traceValid = $false
    Assert-PublicAuthRequestEvent $record
}
Expect-Reject 'valid native trace must not hide auth parameter validity' {
    $record = New-TraceRequestEvent; $record.PSObject.Properties.Remove('authParamsValid')
    Assert-PublicAuthRequestEvent $record
}
foreach ($row in @(
    @('kind', 'rpc_error'), @('direction', 'from_bridge'), @('direction', 'stderr'),
    @('method', 'account/read'), @('method', 'GetAuthStatus'), @('method', 'getAuthStatus/PRIVATE_CANARY'),
    @('class', 'response'), @('class', 'notification'), @('class', 'duplicate_id'),
    @('class', 'correlation_limit'), @('class', 'unrecognized'))) {
    Expect-Reject 'native trace metadata outside its exact request scope' {
        $record = New-TraceRequestEvent; $record.($row[0]) = $row[1]
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($field in @('trace', 'traceparent', 'tracestate', 'traceID', 'traceLength', 'traceHash',
    'params', 'result', 'error', 'PRIVATE_CANARY')) {
    Expect-Reject ('native trace private field: ' + $field) {
        $record = New-TraceRequestEvent
        $record | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($field in @('seq', 'pid', 'correlation')) {
    foreach ($value in @(0, -1, 4294967297, 1.5, 'PRIVATE_CANARY')) {
        Expect-Reject ('native trace invalid identity: ' + $field) {
            $record = New-TraceRequestEvent 'configRequirements/read'
            $record.$field = $value
            Assert-PublicAuthRequestEvent $record
        }
    }
}
foreach ($field in @('authParamsValid', 'includeTokenRequested', 'refreshTokenRequested')) {
    Expect-Reject ('requirements trace must not carry authentication flags: ' + $field) {
        $record = New-TraceRequestEvent 'configRequirements/read'
        $record | Add-Member -NotePropertyName $field -NotePropertyValue $false
        Assert-PublicAuthRequestEvent $record
    }
}
foreach ($json in @(
    '{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"configRequirements/read","correlation":3,"class":"request","tracePresent":true,"traceValid":false,"traceValid":true}',
    '{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"configRequirements/read","correlation":3,"class":"request","tracePresent":true,"traceValid":true,"trace\u0056alid":false}')) {
    Expect-Reject 'duplicate native trace metadata never reaches publication' {
        $record = $json | ConvertFrom-Json
        Assert-PublicCanonicalJSON $json $record
        Assert-PublicAuthRequestEvent $record
    }
}


function New-ReadRequestEvent([string]$Method = 'config/read', [bool]$Valid = $true) {
    $record = [pscustomobject][ordered]@{
        seq = 1; pid = 2; kind = 'rpc'; direction = 'to_bridge'; method = $Method
        correlation = 3; class = 'request'
    }
    if ($Method -ceq 'config/read') {
        $record | Add-Member -NotePropertyName configParamsValid -NotePropertyValue $Valid
        if ($Valid) {
            $record | Add-Member -NotePropertyName configIncludeLayers -NotePropertyValue $false
            $record | Add-Member -NotePropertyName configCwdProvided -NotePropertyValue $false
        }
    } else {
        $record | Add-Member -NotePropertyName featureListParamsValid -NotePropertyValue $Valid
        if ($Valid) {
            foreach ($field in @('featureListCursorProvided', 'featureListLimitProvided', 'featureListThreadIdProvided')) {
                $record | Add-Member -NotePropertyName $field -NotePropertyValue $false
            }
        }
    }
    return $record
}
foreach ($method in @('config/read', 'experimentalFeature/list')) {
    foreach ($valid in @($false, $true)) {
        foreach ($trace in @($false, $true)) {
            Expect-Pass 'read-request exact booleans with optional valid native trace' {
                $record = New-ReadRequestEvent $method $valid
                if ($trace) {
                    $record | Add-Member -NotePropertyName tracePresent -NotePropertyValue $true
                    $record | Add-Member -NotePropertyName traceValid -NotePropertyValue $true
                }
                foreach ($value in @($false, $true)) {
                    foreach ($property in @($record.PSObject.Properties)) {
                        if ($property.Name -cmatch '(Provided|Layers)$') { $property.Value = $value }
                    }
                    Assert-PublicCanonicalJSON ($record | ConvertTo-Json -Compress) $record
                    if (@(Assert-PublicReadRequestEvent $record).Count -ne 0) {
                        throw 'Read-request validation published output.'
                    }
                }
            }
        }
    }
    Expect-Pass 'invalid read-request trace remains trace-only' {
        $record = [pscustomobject][ordered]@{
            seq = 1; pid = 2; kind = 'rpc'; direction = 'to_bridge'; method = $method
            correlation = 3; class = 'request'; tracePresent = $true; traceValid = $false
        }
        Assert-PublicReadRequestEvent $record
    }
    $fields = @('configParamsValid', 'configIncludeLayers', 'configCwdProvided')
    if ($method -ceq 'experimentalFeature/list') {
        $fields = @('featureListParamsValid', 'featureListCursorProvided', 'featureListLimitProvided', 'featureListThreadIdProvided')
    }
    foreach ($field in $fields) {
        foreach ($value in @($null, 0, 1, 'false', 'PRIVATE_CANARY', @($false), [pscustomobject]@{ secret = 'PRIVATE_CANARY' })) {
            Expect-Reject ('read-request non-boolean: ' + $field) {
                $record = New-ReadRequestEvent $method; $record.$field = $value
                Assert-PublicReadRequestEvent $record
            }
        }
        Expect-Reject ('read-request missing field: ' + $field) {
            $record = New-ReadRequestEvent $method; $record.PSObject.Properties.Remove($field)
            Assert-PublicReadRequestEvent $record
        }
        Expect-Reject ('read-request wrong field casing: ' + $field) {
            $record = New-ReadRequestEvent $method; $value = $record.$field
            $record.PSObject.Properties.Remove($field)
            $record | Add-Member -NotePropertyName $field.ToUpperInvariant() -NotePropertyValue $value
            Assert-PublicReadRequestEvent $record
        }
    }
    foreach ($field in $fields[1..($fields.Count - 1)]) {
        Expect-Reject ('invalid request must not emit partial flags: ' + $field) {
            $record = New-ReadRequestEvent $method $false
            $record | Add-Member -NotePropertyName $field -NotePropertyValue $false
            Assert-PublicReadRequestEvent $record
        }
    }
    foreach ($row in @(@('kind', 'rpc_error'), @('direction', 'from_bridge'), @('direction', 'stderr'),
        @('method', 'account/read'), @('method', 'Config/read'), @('class', 'response'),
        @('class', 'notification'), @('class', 'duplicate_id'), @('class', 'correlation_limit'))) {
        Expect-Reject 'read-request metadata outside its request scope' {
            $record = New-ReadRequestEvent $method; $record.($row[0]) = $row[1]
            Assert-PublicReadRequestEvent $record
        }
    }
    foreach ($field in @('params', 'cwd', 'cursor', 'limit', 'threadId', 'result', 'error',
        'authParamsValid', 'configKey', 'PRIVATE_CANARY')) {
        Expect-Reject ('read-request private or mixed field: ' + $field) {
            $record = New-ReadRequestEvent $method
            $record | Add-Member -NotePropertyName $field -NotePropertyValue 'PRIVATE_CANARY'
            Assert-PublicReadRequestEvent $record
        }
    }
    Expect-Reject 'read-request metadata on invalid native trace' {
        $record = New-ReadRequestEvent $method
        $record | Add-Member -NotePropertyName tracePresent -NotePropertyValue $true
        $record | Add-Member -NotePropertyName traceValid -NotePropertyValue $false
        Assert-PublicReadRequestEvent $record
    }
    Expect-Reject 'valid native read trace needs parameter validity' {
        $record = [pscustomobject]@{
            seq = 1; pid = 2; kind = 'rpc'; direction = 'to_bridge'; method = $method
            correlation = 3; class = 'request'; tracePresent = $true; traceValid = $true
        }
        Assert-PublicReadRequestEvent $record
    }
    foreach ($field in @('seq', 'pid', 'correlation')) {
        foreach ($value in @(0, -1, 4294967297, 1.5, '1', 'PRIVATE_CANARY')) {
            Expect-Reject 'read-request invalid sequence or correlation' {
                $record = New-ReadRequestEvent $method; $record.$field = $value
                Assert-PublicReadRequestEvent $record
            }
        }
    }
}
Expect-Reject 'mixed config and feature-list parameter families' {
    $record = New-ReadRequestEvent
    $record | Add-Member -NotePropertyName featureListParamsValid -NotePropertyValue $false
    Assert-PublicReadRequestEvent $record
}
foreach ($json in @(
    '{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"config/read","correlation":3,"class":"request","configParamsValid":false,"configParamsValid":true,"configIncludeLayers":false,"configCwdProvided":false}',
    '{"seq":1,"pid":2,"kind":"rpc","direction":"to_bridge","method":"experimentalFeature/list","correlation":3,"class":"request","featureListParamsValid":true,"featureListCursorProvided":true,"featureListCursor\u0050rovided":false,"featureListLimitProvided":false,"featureListThreadIdProvided":false}')) {
    Expect-Reject 'duplicate read-request metadata cannot reach publication' {
        $record = $json | ConvertFrom-Json
        Assert-PublicCanonicalJSON $json $record
        Assert-PublicReadRequestEvent $record
    }
}

function Fixture-Digest([byte[]]$bytes) {
    $sha = [Security.Cryptography.SHA256]::Create()
    try { return ([BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant() }
    finally { $sha.Dispose() }
}
function New-Catalog {
    $keys = @('plugins.Alpha@public-market.auto_provision', 'plugins.Alpha@public-market.enabled')
    return [pscustomobject][ordered]@{
        schema = 1; status = 'complete'; archiveSha256 = ('a' * 64)
        keysSha256 = Fixture-Digest ([Text.Encoding]::UTF8.GetBytes(($keys -join "`n")))
        coverage = [pscustomobject]@{ jsFiles = 1; jsComplete = 1; jsSkipped = 0; sourceBytes = 64; manifestFiles = 0 }
        sources = @([pscustomobject]@{ kind = 'asar_js'; path = '.vite/build/public-identity.js'; sha256 = ('b' * 64) })
        analysis = [pscustomobject]@{
            parser = 'acorn_8_15_0'
            methods = @(
                [pscustomobject]@{ name='getConfigOverrides'; definitions=0; unresolved=0 },
                [pscustomobject]@{ name='startupCommand'; definitions=0; unresolved=0 }
            )
            constructions = @()
            skipped = @()
        }
        manifestAnalysis = [pscustomobject]@{ pluginEntries=0; pluginManifests=0; mcpFiles=0; skippedEntries=0 }
        candidates = @($keys | ForEach-Object { [pscustomobject]@{ key = $_; source = 0; derivation = 'id_literal' } })
    }
}
function New-ObserverStart([int]$pidValue = 100) {
    return [pscustomobject]@{ seq = 1; pid = $pidValue; kind = 'observer_start'; class = 'test_only' }
}
function New-CatalogEvent($catalog, [int]$pidValue = 100) {
    return [pscustomobject]@{ seq = 2; pid = $pidValue; kind = 'public_plugin_catalog'; publicPluginCatalogSHA256 = $catalog.keysSha256 }
}
function New-Reference([int]$pidValue = 100) {
    return [pscustomobject]@{ seq = 3; pid = $pidValue; kind = 'argv_config'; publicPluginRef = 1; configValue = 'bool_true' }
}
$packageHash = 'c' * 64
$publicFailureCodes = @('public_source_limit', 'public_source_not_packed', 'public_manifest_invalid',
    'public_manifest_limit', 'public_catalog_limit', 'public_source_path', 'public_archive_changed',
    'public_ast_bytes_limit', 'public_ast_token_limit', 'public_ast_node_limit',
    'public_ast_parse_depth_limit', 'public_ast_walk_depth_limit', 'public_ast_walk_node_limit',
    'public_ast_deadline', 'public_ast_range_error')
Expect-Pass 'fixed public failure reasons retain only blocked metadata' {
    if ((@(Get-PublicCatalogFailureCodes) -join '|') -cne ($publicFailureCodes -join '|')) {
        throw 'Public failure whitelist changed.'
    }
    foreach ($reason in $publicFailureCodes + @('invalid_utf8')) {
        $failure = [pscustomobject]@{ schema=1; status='blocked'; reason=$reason }
        $output = @(Assert-PublicCatalogFailure $failure @('invalid_utf8'))
        if ($output.Count -ne 0) { throw 'Failure validation published data.' }
    }
}
$invalidFailures = [ordered]@{
    'syntax error is not a blocked reason' = { param($f) $f.reason = 'syntax_error' }
    'unknown public failure' = { param($f) $f.reason = 'PRIVATE_CANARY' }
    'case changed public failure' = { param($f) $f.reason = 'PUBLIC_AST_TOKEN_LIMIT' }
    'nonstring public failure' = { param($f) $f.reason = @('public_ast_token_limit') }
    'failure with candidate data' = { param($f) $f | Add-Member -NotePropertyName candidates -NotePropertyValue @('PRIVATE_CANARY') }
    'failure with raw exception' = { param($f) $f | Add-Member -NotePropertyName message -NotePropertyValue PRIVATE_CANARY }
    'failure with source position' = { param($f) $f | Add-Member -NotePropertyName position -NotePropertyValue 123 }
    'failure with nonblocked status' = { param($f) $f.status = 'partial' }
    'failure with string schema' = { param($f) $f.schema = '1' }
    'failure missing reason' = { param($f) $f.PSObject.Properties.Remove('reason') }
}
foreach ($name in $invalidFailures.Keys) {
    $mutation = $invalidFailures[$name]
    Expect-Reject $name {
        $f = [pscustomobject]@{ schema=1; status='blocked'; reason='public_ast_token_limit' }
        & $mutation $f
        Assert-PublicCatalogFailure $f @('invalid_utf8')
    }
}
Expect-Pass 'complete pinned RPC dictionary is shared by the sanitizer' {
    $bytes = [IO.File]::ReadAllBytes((Join-Path $PSScriptRoot 'desktop-rpc-observer/rpc_method_facts.json'))
    if ($bytes -contains [byte]10 -or $bytes -contains [byte]13) { throw 'RPC dictionary line endings changed.' }
    $facts = ConvertFrom-PublicRPCMethodFacts $bytes
    $expected = @($facts.methods) + @('initialized', 'attestation/generate', 'unknown')
    $actual = @(Get-PublicRPCMethods -DictionaryPath (Join-Path $PSScriptRoot 'desktop-rpc-observer/rpc_method_facts.json'))
    if ($facts.commit -cne 'c40729ea5cf8e947481b130c59807273eda2f8e9' -or
        $facts.counts.client -ne 183 -or $facts.counts.stable -ne 106 -or
        $facts.counts.experimental -ne 74 -or $facts.counts.deprecated -ne 3 -or
        $actual.Count -ne 186 -or ($actual -join '|') -cne ($expected -join '|')) {
        throw 'Complete RPC dictionary changed.'
    }
    foreach ($name in @('thread/resume', 'server/diagnostics', 'getAuthStatus',
        'getConversationSummary', 'gitDiffToRemote', 'mock/experimentalMethod')) {
        if ($actual -cnotcontains $name) { throw 'Source-declared method missing.' }
    }
    foreach ($name in @('GetAuthStatus', 'getAuthStatus/PRIVATE_CANARY', 'PRIVATE_CANARY')) {
        if ($actual -ccontains $name) { throw 'Unreviewed method name accepted.' }
    }
}
foreach ($variant in @('empty', 'canary', 'oversized', 'changed-name', 'extra-newline')) {
    Expect-Reject ('changed RPC dictionary: ' + $variant) {
        $bytes = [IO.File]::ReadAllBytes((Join-Path $PSScriptRoot 'desktop-rpc-observer/rpc_method_facts.json'))
        switch ($variant) {
            'empty' { $bytes = [byte[]]@() }
            'canary' { $bytes = [Text.Encoding]::UTF8.GetBytes('PRIVATE_CANARY') }
            'oversized' { $bytes = [Text.Encoding]::UTF8.GetBytes(('PRIVATE_CANARY' * 16384)) }
            'changed-name' {
                $text = [Text.Encoding]::UTF8.GetString($bytes).Replace('thread/resume', 'PRIVATE_CANARY')
                $bytes = [Text.Encoding]::UTF8.GetBytes($text)
            }
            'extra-newline' { $bytes = [byte[]]($bytes + [byte]10) }
        }
        ConvertFrom-PublicRPCMethodFacts $bytes
    }
}
Expect-Pass 'SHA256 known vector' {
    if ((Get-PublicDigest ([Text.Encoding]::UTF8.GetBytes('abc'))) -cne
        'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad') { throw 'digest' }
}
Expect-Pass 'canonical ordered handoff and shared source index' {
    $catalog = New-Catalog
    $envJSON = ConvertTo-PublicPluginEnvironment $catalog $packageHash
    $expected = '{"schema":1,"packageSha256":"' + $packageHash + '","archiveSha256":"' +
        $catalog.archiveSha256 + '","keysSha256":"' + $catalog.keysSha256 +
        '","keys":["plugins.Alpha@public-market.auto_provision","plugins.Alpha@public-market.enabled"]}'
    if ($envJSON -cne $expected) { throw 'handoff bytes' }
}
Expect-Pass 'partial coverage remains explicit' {
    $catalog = New-Catalog
    $catalog.status = 'partial'; $catalog.coverage.jsFiles = 2; $catalog.coverage.jsSkipped = 1
    $catalog.analysis.skipped = @([pscustomobject]@{ reason='syntax_error'; count=1 })
    ConvertTo-PublicPluginEnvironment $catalog $packageHash
}
Expect-Pass 'manifest identity candidates preserve source relationship' {
    $c = New-Catalog
    $c.coverage.jsFiles = 0; $c.coverage.jsComplete = 0; $c.coverage.manifestFiles = 1
    $c.sources[0] = [pscustomobject]@{
        kind = 'marketplace_manifest'
        path = 'app/resources/plugins/public-market/.agents/plugins/marketplace.json'
        sha256 = ('b' * 64)
    }
    foreach ($candidate in $c.candidates) { $candidate.derivation = 'manifest_identity' }
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'key order is ordinal rather than cultural' {
    $c = New-Catalog
    $c.candidates[0].key = 'plugins.Zeta@public-market.enabled'
    $c.candidates[1].key = 'plugins.alpha@public-market.enabled'
    $c.keysSha256 = Fixture-Digest ([Text.Encoding]::UTF8.GetBytes(($c.candidates.key -join [char]10)))
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
$invalidCatalogs = [ordered]@{
    'unknown field' = { param($c) $c | Add-Member -NotePropertyName extra -NotePropertyValue PRIVATE_CANARY }
    'case changed field' = { param($c) $c.PSObject.Properties.Remove('schema'); $c | Add-Member -NotePropertyName Schema -NotePropertyValue 1 }
    'string schema' = { param($c) $c.schema = '1' }
    'floating schema' = { param($c) $c.schema = 1.0 }
    'uppercase status' = { param($c) $c.status = 'COMPLETE' }
    'uppercase digest' = { param($c) $c.archiveSha256 = 'A' * 64 }
    'digest trailing newline' = { param($c) $c.archiveSha256 += [char]10 }
    'key digest mismatch' = { param($c) $c.keysSha256 = '0' * 64 }
    'digest includes trailing LF' = { param($c) $c.keysSha256 = Fixture-Digest ([Text.Encoding]::UTF8.GetBytes(($c.candidates.key -join [char]10) + [char]10)) }
    'incomplete provenance' = { param($c) $c.sources = @() }
    'duplicate source path' = { param($c) $c.sources += $c.sources[0] }
    'case alias source path' = { param($c) $c.sources += [pscustomobject]@{ kind='asar_js'; path='.vite/build/PUBLIC-IDENTITY.js'; sha256=('a'*64) } }
    'path traversal' = { param($c) $c.sources[0].path = '.vite/build/../PRIVATE_CANARY.js' }
    'path trailing newline' = { param($c) $c.sources[0].path += [char]10 }
    'unsupported kind' = { param($c) $c.sources[0].kind = 'ASAR_JS' }
    'derivation kind mismatch' = { param($c) $c.candidates[0].derivation = 'manifest_identity' }
    'missing candidate source' = { param($c) $c.candidates[0].PSObject.Properties.Remove('source') }
    'out of range source' = { param($c) $c.candidates[0].source = 1 }
    'string source index' = { param($c) $c.candidates[0].source = '0' }
    'duplicate candidate key' = { param($c) $c.candidates[1].key = $c.candidates[0].key }
    'unsorted candidate keys' = { param($c) $c.candidates = @($c.candidates[1], $c.candidates[0]) }
    'nonpublic key' = { param($c) $c.candidates[0].key = 'plugins.PRIVATE_CANARY.enabled' }
    'key trailing newline' = { param($c) $c.candidates[0].key += [char]10 }
    'unsupported leaf' = { param($c) $c.candidates[0].key = 'plugins.Alpha@public-market.secret' }
    'empty catalog' = { param($c) $c.candidates = @() }
    'scalar candidates' = { param($c) $c.candidates = $c.candidates[0] }
    'coverage arithmetic mismatch' = { param($c) $c.coverage.jsComplete = 0 }
    'partial without gap' = { param($c) $c.status = 'partial' }
    'complete with gap' = { param($c) $c.coverage.jsFiles = 2; $c.coverage.jsSkipped = 1 }
    'coverage byte limit' = { param($c) $c.coverage.sourceBytes = 134217729 }
    'coverage source mismatch' = { param($c) $c.coverage.jsFiles = 0; $c.coverage.jsComplete = 0 }
}
foreach ($name in $invalidCatalogs.Keys) {
    $mutation = $invalidCatalogs[$name]
    Expect-Reject $name { $c = New-Catalog; & $mutation $c; ConvertTo-PublicPluginEnvironment $c $packageHash }
}
function New-MCPCatalog([string]$derivation = 'legacy_mcp_default') {
    $c = New-Catalog
    $c.coverage.jsFiles=0; $c.coverage.jsComplete=0; $c.coverage.manifestFiles=1
    $c.manifestAnalysis.pluginEntries=1; $c.manifestAnalysis.pluginManifests=1
    $root = 'app/resources/plugins/public-market/fixture'
    $pluginPath = $root + '/.codex-plugin/plugin.json'
    $mcpPath = $root + '/.mcp.json'
    if ($derivation -ceq 'agent_mcp_default') { $pluginPath=$root + '/plugin.json'; $mcpPath=$root + '/mcp.json' }
    if ($derivation -ceq 'legacy_mcp_path') { $mcpPath=$root + '/config/servers.json' }
    $c.sources = @(
        [pscustomobject]@{ kind='marketplace_manifest'; path='app/resources/plugins/public-market/.agents/plugins/marketplace.json'; sha256=('a'*64) },
        [pscustomobject]@{ kind='plugin_manifest'; path=$pluginPath; sha256=('b'*64) }
    )
    $sourceIndex=1
    if ($derivation -cne 'legacy_mcp_inline') {
        $c.sources += [pscustomobject]@{ kind='mcp_manifest'; path=$mcpPath; sha256=('c'*64) }
        $sourceIndex=2; $c.manifestAnalysis.mcpFiles=1
    }
    $c.candidates = @([pscustomobject]@{
        key='plugins.fixture@public-market.mcp_servers.PublicServer.enabled'
        derivation=$derivation; source=$sourceIndex; marketplaceSource=0; pluginSource=1
    })
    $c.keysSha256 = Fixture-Digest ([Text.Encoding]::UTF8.GetBytes($c.candidates[0].key))
    return $c
}
Expect-Pass 'all reviewed MCP derivations retain bound source chains' {
    foreach ($derivation in @('legacy_mcp_inline', 'legacy_mcp_default', 'legacy_mcp_path', 'agent_mcp_default')) {
        $c = New-MCPCatalog $derivation
        $handoff = ConvertTo-PublicPluginEnvironment $c $packageHash
        if (($handoff | ConvertFrom-Json).keys[0] -cne $c.candidates[0].key) { throw 'Nested handoff changed key.' }
    }
}
Expect-Pass 'all native legacy manifest locations use the same plugin root' {
    foreach ($directory in @('.codex-plugin', '.claude-plugin', '.cursor-plugin')) {
        $c = New-MCPCatalog
        $c.sources[1].path='app/resources/plugins/public-market/fixture/' + $directory + '/plugin.json'
        ConvertTo-PublicPluginEnvironment $c $packageHash
    }
}
Expect-Pass 'agent roots named like legacy manifest directories remain unambiguous' {
    foreach ($directory in @('.codex-plugin', '.claude-plugin', '.cursor-plugin')) {
        $c=New-MCPCatalog 'agent_mcp_default'
        $root='app/resources/plugins/public-market/' + $directory
        $c.sources[1].path=$root + '/plugin.json'; $c.sources[2].path=$root + '/mcp.json'
        ConvertTo-PublicPluginEnvironment $c $packageHash
    }
}
Expect-Pass 'manifest incompleteness is separate from complete JavaScript coverage' {
    $c = New-MCPCatalog
    $c.manifestAnalysis.skippedEntries=1
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'manifest entry and total JSON bounds are inclusive' {
    $c=New-MCPCatalog
    $c.manifestAnalysis.pluginEntries=2048; $c.manifestAnalysis.skippedEntries=2048
    $c.manifestAnalysis.pluginManifests=31; $c.manifestAnalysis.mcpFiles=32
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'marketplace root plugin and eight-component MCP path are bounded' {
    $c = New-MCPCatalog 'legacy_mcp_path'
    $c.sources[1].path='app/resources/plugins/public-market/.codex-plugin/plugin.json'
    $c.sources[2].path='app/resources/plugins/public-market/a/b/c/d/e/f/g/servers.json'
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'eight-component local plugin source is bounded' {
    $c = New-MCPCatalog
    $root='app/resources/plugins/public-market/a/b/c/d/e/f/g/h'
    $c.sources[1].path=$root + '/.codex-plugin/plugin.json'; $c.sources[2].path=$root + '/.mcp.json'
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'nested key at exact 128-byte boundary' {
    $c = New-MCPCatalog
    $c.candidates[0].key='plugins.' + ('p'*30) + '@' + ('m'*30) + '.mcp_servers.' + ('s'*38) + '.enabled'
    if ($c.candidates[0].key.Length -ne 128) { throw 'Bad key boundary fixture.' }
    $c.keysSha256=Fixture-Digest ([Text.Encoding]::UTF8.GetBytes($c.candidates[0].key))
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
$invalidMCP = [ordered]@{
    'missing manifest analysis' = { param($c) $c.PSObject.Properties.Remove('manifestAnalysis') }
    'extra manifest analysis field' = { param($c) $c.manifestAnalysis | Add-Member -NotePropertyName values -NotePropertyValue PRIVATE_CANARY }
    'string manifest count' = { param($c) $c.manifestAnalysis.pluginEntries='1' }
    'fractional manifest count' = { param($c) $c.manifestAnalysis.mcpFiles=1.0 }
    'manifest entry cap' = { param($c) $c.manifestAnalysis.pluginEntries=2049 }
    'manifest source cap' = { param($c) $c.manifestAnalysis.pluginManifests=65 }
    'MCP file cap' = { param($c) $c.manifestAnalysis.mcpFiles=65 }
    'skipped entries exceed entries' = { param($c) $c.manifestAnalysis.skippedEntries=2 }
    'negative skipped entries' = { param($c) $c.manifestAnalysis.skippedEntries=-1 }
    'global JSON count' = { param($c) $c.manifestAnalysis.pluginManifests=32; $c.manifestAnalysis.mcpFiles=32 }
    'incomplete plugin source count' = { param($c) $c.manifestAnalysis.pluginManifests=0 }
    'incomplete MCP source count' = { param($c) $c.manifestAnalysis.mcpFiles=0 }
    'nested candidate without entry coverage' = { param($c) $c.manifestAnalysis.pluginEntries=0 }
    'orphan plugin source' = {
        param($c)
        $c.sources += [pscustomobject]@{ kind='plugin_manifest'; path='app/resources/plugins/public-market/unused/.codex-plugin/plugin.json'; sha256=('d'*64) }
        $c.manifestAnalysis.pluginManifests=2
    }
    'orphan MCP source' = {
        param($c)
        $c.sources += [pscustomobject]@{ kind='mcp_manifest'; path='app/resources/plugins/public-market/unused/servers.json'; sha256=('d'*64) }
        $c.manifestAnalysis.mcpFiles=2
    }
    'nested missing marketplace link' = { param($c) $c.candidates[0].PSObject.Properties.Remove('marketplaceSource') }
    'nested missing plugin link' = { param($c) $c.candidates[0].PSObject.Properties.Remove('pluginSource') }
    'nested extra raw field' = { param($c) $c.candidates[0] | Add-Member -NotePropertyName command -NotePropertyValue PRIVATE_CANARY }
    'string marketplace link' = { param($c) $c.candidates[0].marketplaceSource='0' }
    'fractional plugin link' = { param($c) $c.candidates[0].pluginSource=1.0 }
    'out of range marketplace link' = { param($c) $c.candidates[0].marketplaceSource=3 }
    'wrong marketplace kind' = { param($c) $c.candidates[0].marketplaceSource=1 }
    'wrong plugin kind' = { param($c) $c.candidates[0].pluginSource=0 }
    'wrong MCP kind' = { param($c) $c.candidates[0].source=1 }
    'unknown nested derivation' = { param($c) $c.candidates[0].derivation='PRIVATE_CANARY' }
    'case nested derivation' = { param($c) $c.candidates[0].derivation='LEGACY_MCP_DEFAULT' }
    'nested derived from literal' = { param($c) $c.candidates[0].derivation='key_literal' }
    'inline not from plugin source' = { param($c) $c.candidates[0].derivation='legacy_mcp_inline' }
    'agent derivation with mismatched root and default file' = { param($c) $c.candidates[0].derivation='agent_mcp_default' }
    'legacy derivation with agent manifest' = { param($c) $c.sources[1].path='app/resources/plugins/public-market/fixture/plugin.json' }
    'different marketplace source' = { param($c) $c.sources[0].path='app/resources/plugins/other-market/.agents/plugins/marketplace.json' }
    'case changed relation root' = { param($c) $c.sources[1].path='app/resources/plugins/Public-market/fixture/.codex-plugin/plugin.json' }
    'MCP outside plugin root' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/other/.mcp.json' }
    'MCP prefix sibling not child' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture-other/.mcp.json' }
    'wrong legacy default file' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/mcp.json' }
    'manifest traversal' = { param($c) $c.sources[1].path='app/resources/plugins/public-market/../fixture/.codex-plugin/plugin.json' }
    'manifest current component' = { param($c) $c.sources[1].path='app/resources/plugins/public-market/./fixture/.codex-plugin/plugin.json' }
    'manifest backslash' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture\servers.json' }
    'manifest variable' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/$HOME.json' }
    'manifest percent escape' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/%2e.json' }
    'manifest nonASCII component' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/' + [char]233 + '.json' }
    'manifest URI' = { param($c) $c.sources[2].path='https://PRIVATE_CANARY/servers.json' }
    'manifest component bound' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/' + ('a'*65) + '.json' }
    'manifest path bound' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/' + ((@(('a'*60)) * 8) -join '/') + '/servers.json' }
    'manifest unsafe trailing dot' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/a./servers.json' }
    'MCP wrong extension' = { param($c) $c.sources[2].path='app/resources/plugins/public-market/fixture/servers.txt' }
    'plugin wrong manifest filename' = { param($c) $c.sources[1].path='app/resources/plugins/public-market/fixture/.codex-plugin/other.json' }
    'nine-component local plugin source' = {
        param($c)
        $root='app/resources/plugins/public-market/a/b/c/d/e/f/g/h/i'
        $c.sources[1].path=$root + '/.codex-plugin/plugin.json'; $c.sources[2].path=$root + '/.mcp.json'
    }
    'nine-component MCP reference' = {
        param($c)
        $c.candidates[0].derivation='legacy_mcp_path'
        $c.sources[2].path='app/resources/plugins/public-market/fixture/a/b/c/d/e/f/g/h/servers.json'
    }
    'nested key over 128 bytes' = { param($c) $c.candidates[0].key='plugins.' + ('p'*30) + '@' + ('m'*30) + '.mcp_servers.' + ('s'*39) + '.enabled' }
    'nested unsafe server segment' = { param($c) $c.candidates[0].key='plugins.fixture@public-market.mcp_servers._private.enabled' }
    'nested dot in server segment' = { param($c) $c.candidates[0].key='plugins.fixture@public-market.mcp_servers.server.name.enabled' }
    'nested auto provision not reviewed' = { param($c) $c.candidates[0].key='plugins.fixture@public-market.mcp_servers.PublicServer.auto_provision' }
    'nested app field not reviewed' = { param($c) $c.candidates[0].key='plugins.fixture@public-market.apps.PublicServer.enabled' }
    'direct candidate with relation fields' = { param($c) $c.candidates[0].key='plugins.fixture@public-market.enabled' }
}
foreach ($name in $invalidMCP.Keys) {
    $mutation=$invalidMCP[$name]
    Expect-Reject $name {
        $c=New-MCPCatalog; & $mutation $c
        $c.keysSha256=Fixture-Digest ([Text.Encoding]::UTF8.GetBytes(($c.candidates.key -join [char]10)))
        ConvertTo-PublicPluginEnvironment $c $packageHash
    }
}
Expect-Reject 'direct key cannot derive from MCP source' {
    $c=New-MCPCatalog
    $c.candidates=@([pscustomobject]@{ key='plugins.fixture@public-market.enabled'; derivation='manifest_identity'; source=2 })
    $c.keysSha256=Fixture-Digest ([Text.Encoding]::UTF8.GetBytes($c.candidates[0].key))
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Reject 'agent default uses exact mcp filename' {
    $c=New-MCPCatalog 'agent_mcp_default'; $c.sources[2].path='app/resources/plugins/public-market/fixture/.mcp.json'
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
function New-Construction {
    return [pscustomobject]@{
        source=0; method='getConfigOverrides'; form='template'
        shape='direct_enabled'; value='bool_true'; count=1
    }
}
Expect-Pass 'all fixed construction labels remain metadata only' {
    $c = New-Catalog
    $c.analysis.methods[0].definitions = 1
    $c.analysis.constructions = @(
        foreach ($shape in @('direct_enabled', 'direct_auto_provision', 'mcp_server_enabled', 'app_enabled',
            'app_destructive_enabled', 'app_open_world_enabled', 'app_default_tools_enabled', 'app_tool_enabled')) {
            foreach ($form in @('literal', 'template', 'concat')) {
                foreach ($value in @('bool_true', 'bool_false', 'dynamic', 'other')) {
                    [pscustomobject]@{ source=0; method='getConfigOverrides'; form=$form; shape=$shape; value=$value; count=1 }
                }
            }
        }
    )
    $before = ConvertTo-PublicPluginEnvironment (New-Catalog) $packageHash
    $after = ConvertTo-PublicPluginEnvironment $c $packageHash
    if ($before -cne $after) { throw 'Construction diagnostics changed the candidate handoff.' }
}
Expect-Pass 'analysis aggregate bounds are independent and inclusive' {
    $c = New-Catalog
    foreach ($method in $c.analysis.methods) { $method.definitions=2000000; $method.unresolved=2000000 }
    $first = New-Construction; $first.count=2000000
    $second = New-Construction; $second.form='concat'; $second.count=2000000
    $c.analysis.constructions = @($first, $second)
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'local parser skips retain independent catalog bytes and explicit coverage' {
    $expected = @('syntax_error', 'public_ast_token_limit', 'public_ast_node_limit',
        'public_ast_parse_depth_limit', 'public_ast_walk_depth_limit',
        'public_ast_walk_node_limit', 'public_ast_deadline')
    if ((@(Get-PublicASTSkipReasons) -join '|') -cne ($expected -join '|')) { throw 'Skip reason whitelist changed.' }
    $c = New-Catalog
    $c.status='partial'; $c.coverage.jsFiles=8; $c.coverage.jsSkipped=7
    $c.analysis.skipped = @($expected | ForEach-Object { [pscustomobject]@{ reason=$_; count=1 } })
    $before = ConvertTo-PublicPluginEnvironment (New-Catalog) $packageHash
    $after = ConvertTo-PublicPluginEnvironment $c $packageHash
    if ($before -cne $after) { throw 'Skipped sources changed the independently retained catalog.' }
}
Expect-Pass 'all JavaScript skipped still permits independently verified manifest candidates' {
    $c = New-Catalog
    $c.status='partial'; $c.coverage.jsFiles=2048; $c.coverage.jsComplete=0; $c.coverage.jsSkipped=2048
    $c.coverage.manifestFiles=1; $c.coverage.sourceBytes=65536
    $c.sources[0].kind='marketplace_manifest'
    $c.sources[0].path='app/resources/plugins/public-market/.agents/plugins/marketplace.json'
    foreach ($candidate in $c.candidates) { $candidate.derivation='manifest_identity' }
    $c.analysis.skipped = @([pscustomobject]@{ reason='syntax_error'; count=2048 })
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
$invalidSkips = [ordered]@{
    'missing skipped diagnostics' = { param($c) $c.analysis.PSObject.Properties.Remove('skipped') }
    'scalar skipped diagnostics' = { param($c) $c.analysis.skipped=$c.analysis.skipped[0] }
    'empty skips with incomplete coverage' = { param($c) $c.analysis.skipped=@() }
    'nonempty skips with complete coverage' = {
        param($c) $c.status='complete'; $c.coverage.jsFiles=1; $c.coverage.jsSkipped=0
    }
    'unknown skip reason' = { param($c) $c.analysis.skipped[0].reason='PRIVATE_CANARY' }
    'case skip reason' = { param($c) $c.analysis.skipped[0].reason='SYNTAX_ERROR' }
    'array skip reason' = { param($c) $c.analysis.skipped[0].reason=@('syntax_error') }
    'bytes failure cannot be skipped' = { param($c) $c.analysis.skipped[0].reason='public_ast_bytes_limit' }
    'range failure cannot be skipped' = { param($c) $c.analysis.skipped[0].reason='public_ast_range_error' }
    'source failure cannot be skipped' = { param($c) $c.analysis.skipped[0].reason='public_source_limit' }
    'UTF8 failure cannot be skipped' = { param($c) $c.analysis.skipped[0].reason='invalid_utf8' }
    'archive failure cannot be skipped' = { param($c) $c.analysis.skipped[0].reason='archive_changed' }
    'zero skipped count' = { param($c) $c.analysis.skipped[0].count=0 }
    'string skipped count' = { param($c) $c.analysis.skipped[0].count='1' }
    'floating skipped count' = { param($c) $c.analysis.skipped[0].count=1.0 }
    'skipped count bound' = { param($c) $c.analysis.skipped[0].count=2049 }
    'skipped total differs from coverage' = { param($c) $c.analysis.skipped[0].count=2 }
    'duplicate skipped reasons' = { param($c) $c.analysis.skipped += $c.analysis.skipped[0] }
    'unordered skipped reasons' = {
        param($c)
        $c.analysis.skipped=@([pscustomobject]@{reason='public_ast_token_limit';count=1},[pscustomobject]@{reason='syntax_error';count=1})
    }
    'skipped aggregate bound' = {
        param($c)
        $c.analysis.skipped=@([pscustomobject]@{reason='syntax_error';count=2048},[pscustomobject]@{reason='public_ast_token_limit';count=1})
    }
    'skipped row bound' = { param($c) $c.analysis.skipped=@(1..8 | ForEach-Object {[pscustomobject]@{reason='syntax_error';count=1}}) }
    'skip cannot carry source names' = {
        param($c) $c.analysis.skipped[0] | Add-Member -NotePropertyName source -NotePropertyValue PRIVATE_CANARY
    }
    'skip cannot carry candidates' = {
        param($c) $c.analysis.skipped[0] | Add-Member -NotePropertyName candidates -NotePropertyValue @('PRIVATE_CANARY')
    }
}
foreach ($name in $invalidSkips.Keys) {
    $mutation = $invalidSkips[$name]
    Expect-Reject $name {
        $c = New-Catalog
        $c.status='partial'; $c.coverage.jsFiles=2; $c.coverage.jsSkipped=1
        $c.analysis.skipped=@([pscustomobject]@{reason='syntax_error';count=1})
        & $mutation $c
        ConvertTo-PublicPluginEnvironment $c $packageHash
    }
}
$invalidAnalysis = [ordered]@{
    'definitions aggregate bound' = { param($c) foreach ($m in $c.analysis.methods) { $m.definitions=2000001 } }
    'unresolved aggregate bound' = { param($c) foreach ($m in $c.analysis.methods) { $m.definitions=1; $m.unresolved=2000001 } }
    'construction aggregate bound' = {
        param($c)
        $first = New-Construction; $first.count=2000001
        $second = New-Construction; $second.form='concat'; $second.count=2000001
        $c.analysis.constructions = @($first, $second)
    }
    'missing analysis' = { param($c) $c.PSObject.Properties.Remove('analysis') }
    'unknown analysis field' = { param($c) $c.analysis | Add-Member -NotePropertyName text -NotePropertyValue PRIVATE_CANARY }
    'unknown parser' = { param($c) $c.analysis.parser = 'legacy' }
    'case parser' = { param($c) $c.analysis.parser = 'ACORN_8_15_0' }
    'method count' = { param($c) $c.analysis.methods = @($c.analysis.methods[0]) }
    'unordered methods' = { param($c) $c.analysis.methods = @($c.analysis.methods[1], $c.analysis.methods[0]) }
    'unresolved without definition' = { param($c) $c.analysis.methods[1].unresolved = 1 }
    'construction without definition' = { param($c) $c.analysis.methods[0].definitions = 0 }
    'duplicate method' = { param($c) $c.analysis.methods[1].name = 'getConfigOverrides' }
    'unknown method' = { param($c) $c.analysis.methods[1].name = 'PRIVATE_CANARY' }
    'case method' = { param($c) $c.analysis.methods[1].name = 'StartupCommand' }
    'extra method field' = { param($c) $c.analysis.methods[0] | Add-Member -NotePropertyName source -NotePropertyValue PRIVATE_CANARY }
    'string definitions' = { param($c) $c.analysis.methods[0].definitions = '1' }
    'negative definitions' = { param($c) $c.analysis.methods[0].definitions = -1 }
    'definitions bound' = { param($c) $c.analysis.methods[0].definitions = 4000001 }
    'fractional unresolved' = { param($c) $c.analysis.methods[0].unresolved = 0.5 }
    'unresolved bound' = { param($c) $c.analysis.methods[0].unresolved = 4000001 }
    'scalar constructions' = { param($c) $c.analysis.constructions = New-Construction }
    'construction row limit' = { param($c) $c.analysis.constructions = @(1..129 | ForEach-Object { New-Construction }) }
    'duplicate construction tuple' = { param($c) $c.analysis.constructions += $c.analysis.constructions[0] }
    'unknown construction field' = { param($c) $c.analysis.constructions[0] | Add-Member -NotePropertyName identifier -NotePropertyValue PRIVATE_CANARY }
    'string construction source' = { param($c) $c.analysis.constructions[0].source = '0' }
    'missing construction source' = { param($c) $c.analysis.constructions[0].PSObject.Properties.Remove('source') }
    'construction source out of range' = { param($c) $c.analysis.constructions[0].source = 1 }
    'construction from manifest' = {
        param($c)
        $c.sources[0].kind = 'marketplace_manifest'
        $c.sources[0].path = 'app/resources/plugins/public-market/.agents/plugins/marketplace.json'
        $c.coverage.manifestFiles = 1
        foreach ($candidate in $c.candidates) { $candidate.derivation = 'manifest_identity' }
    }
    'unknown construction method' = { param($c) $c.analysis.constructions[0].method = 'PRIVATE_CANARY' }
    'unknown construction form' = { param($c) $c.analysis.constructions[0].form = 'call' }
    'unknown construction shape' = { param($c) $c.analysis.constructions[0].shape = 'PRIVATE_CANARY' }
    'case construction shape' = { param($c) $c.analysis.constructions[0].shape = 'DIRECT_ENABLED' }
    'raw construction value' = { param($c) $c.analysis.constructions[0].value = 'PRIVATE_CANARY' }
    'numeric construction value' = { param($c) $c.analysis.constructions[0].value = 1 }
    'zero construction count' = { param($c) $c.analysis.constructions[0].count = 0 }
    'floating construction count' = { param($c) $c.analysis.constructions[0].count = 1.0 }
    'construction count bound' = { param($c) $c.analysis.constructions[0].count = 4000001 }
}
foreach ($name in $invalidAnalysis.Keys) {
    $mutation = $invalidAnalysis[$name]
    Expect-Reject $name {
        $c = New-Catalog
        $c.analysis.methods[0].definitions = 1
        $c.analysis.constructions = @(New-Construction)
        & $mutation $c
        ConvertTo-PublicPluginEnvironment $c $packageHash
    }
}
Expect-Reject 'catalog handoff byte limit' {
    $c = New-Catalog
    $c.candidates = @(0..127 | ForEach-Object { [pscustomobject]@{
        key = ('plugins.p{0:D3}' -f $_) + ('x' * 44) + '@' + ('m' * 63) + '.enabled'
        source = 0; derivation = 'key_literal'
    } })
    $c.keysSha256 = Fixture-Digest ([Text.Encoding]::UTF8.GetBytes(($c.candidates.key -join [char]10)))
    ConvertTo-PublicPluginEnvironment $c $packageHash
}
Expect-Pass 'exact child environment character bound' { Assert-PublicEnvironmentSize @{ A = ('x' * 29996) } }
Expect-Reject 'child environment exceeds character bound' { Assert-PublicEnvironmentSize @{ A = ('x' * 29997) } }
Expect-Reject 'child environment NUL' { Assert-PublicEnvironmentSize @{ A = ('x' + [char]0) } }
Expect-Reject 'duplicate JSON property' {
    $json = '{"schema":1,"schema":1}'
    Assert-PublicCanonicalJSON $json ($json | ConvertFrom-Json)
}
Expect-Pass 'canonical metadata JSON' {
    $json = '{"seq":1,"pid":100,"kind":"public_plugin_catalog","publicPluginCatalogSHA256":"' + ('a' * 64) + '"}'
    Assert-PublicCanonicalJSON $json ($json | ConvertFrom-Json)
}
Expect-Pass 'one catalog per PID, repeated argv references allowed' {
    $c = New-Catalog; $seen = @{}
    Assert-PublicPluginEvent (New-ObserverStart) $c $seen
    Assert-PublicPluginEvent (New-CatalogEvent $c) $c $seen
    Assert-PublicPluginEvent (New-Reference) $c $seen
    Assert-PublicPluginEvent (New-Reference) $c $seen
    Assert-PublicPluginEvent (New-ObserverStart 101) $c $seen
    Assert-PublicPluginEvent (New-CatalogEvent $c 101) $c $seen
    Assert-PublicPluginEvent (New-Reference 101) $c $seen
}
Expect-Pass 'old fixed and unknown behavior without catalog' {
    Assert-PublicPluginEvent ([pscustomobject]@{ kind='argv_config'; configKey='features.foo'; configValue='other' }) $null @{}
    Assert-PublicPluginEvent ([pscustomobject]@{ kind='argv_config'; class='unknown' }) $null @{}
}
Expect-Reject 'reference before catalog' { Assert-PublicPluginEvent (New-Reference) (New-Catalog) @{} }
Expect-Reject 'catalog with no verified table' { Assert-PublicPluginEvent (New-CatalogEvent (New-Catalog)) $null @{} }
Expect-Reject 'per PID digest mismatch' {
    $c = New-Catalog; $e = New-CatalogEvent $c; $e.publicPluginCatalogSHA256 = '0' * 64
    $seen = @{}; Assert-PublicPluginEvent (New-ObserverStart) $c $seen
    Assert-PublicPluginEvent $e $c $seen
}
Expect-Reject 'duplicate catalog for PID' {
    $c = New-Catalog; $seen = @{}; $e = New-CatalogEvent $c
    Assert-PublicPluginEvent (New-ObserverStart) $c $seen
    Assert-PublicPluginEvent $e $c $seen; Assert-PublicPluginEvent $e $c $seen
}
Expect-Reject 'catalog before observer start' { $c = New-Catalog; Assert-PublicPluginEvent (New-CatalogEvent $c) $c @{} }
Expect-Reject 'duplicate observer start' {
    $c = New-Catalog; $seen = @{}
    Assert-PublicPluginEvent (New-ObserverStart) $c $seen
    Assert-PublicPluginEvent (New-ObserverStart) $c $seen
}
Expect-Reject 'argv before required catalog even without references' {
    $c = New-Catalog; $seen = @{}
    Assert-PublicPluginEvent (New-ObserverStart) $c $seen
    Assert-PublicPluginEvent ([pscustomobject]@{ seq=2; pid=100; kind='argv'; flag='app-server' }) $c $seen
}
Expect-Reject 'child start before required catalog' {
    $c = New-Catalog; $seen = @{}
    Assert-PublicPluginEvent (New-ObserverStart) $c $seen
    Assert-PublicPluginEvent ([pscustomobject]@{ seq=2; pid=100; kind='child_start'; child_pid=200 }) $c $seen
}
$invalidEvents = [ordered]@{
    'zero ref' = { param($e) $e.publicPluginRef = 0 }
    'ref above bound' = { param($e) $e.publicPluginRef = 3 }
    'string ref' = { param($e) $e.publicPluginRef = '1' }
    'float ref' = { param($e) $e.publicPluginRef = 1.0 }
    'fraction ref' = { param($e) $e.publicPluginRef = 1.5 }
    'unknown PID ref' = { param($e) $e.pid = 101 }
    'raw key with ref' = { param($e) $e | Add-Member -NotePropertyName configKey -NotePropertyValue PRIVATE_CANARY }
    'unknown fields with ref' = { param($e) $e | Add-Member -NotePropertyName class -NotePropertyValue unknown }
    'raw value with ref' = { param($e) $e.configValue = 'PRIVATE_CANARY' }
    'enum with ref' = { param($e) $e.configValue = 'enum_api' }
    'case label with ref' = { param($e) $e.configValue = 'BOOL_TRUE' }
    'digest with ref' = { param($e) $e | Add-Member -NotePropertyName publicPluginCatalogSHA256 -NotePropertyValue ('a' * 64) }
    'reference outside argv' = { param($e) $e.kind = 'rpc' }
    'case changed ref name' = { param($e) $e.PSObject.Properties.Remove('publicPluginRef'); $e | Add-Member -NotePropertyName PublicPluginRef -NotePropertyValue 1 }
}
foreach ($name in $invalidEvents.Keys) {
    $mutation = $invalidEvents[$name]
    Expect-Reject $name {
        $c = New-Catalog; $seen = @{}
        Assert-PublicPluginEvent (New-ObserverStart) $c $seen
        Assert-PublicPluginEvent (New-CatalogEvent $c) $c $seen
        $e = New-Reference; & $mutation $e; Assert-PublicPluginEvent $e $c $seen
    }
}
function New-UnknownPluginShape {
    return [pscustomobject]@{
        seq=3; pid=100; kind='argv_config'; class='unknown'; configShape='other_key'; configFamily='other'
        configLiteral='bool_true'; configLexicalRoot='plugins'; configKeyClasses=@('at_sign')
        configPluginShape='direct_enabled'
    }
}
Expect-Pass 'runtime shapes expose no dynamic identity or raw value' {
    foreach ($shape in @(Get-PublicPluginShapes)) {
        foreach ($literal in @('bool_true', 'bool_false', 'other')) {
            $event = New-UnknownPluginShape
            $event.configPluginShape = $shape
            $event.configLiteral = $literal
            $event.configKeyClasses = @('at_sign', 'ascii_space', 'ascii_control')
            Assert-PublicPluginShapeEvent $event
        }
    }
}
Expect-Pass 'runtime shape omission preserves old event API' {
    Assert-PublicPluginShapeEvent ([pscustomobject]@{ kind='argv_config'; class='unknown' })
    Assert-PublicPluginShapeEvent ([pscustomobject]@{ kind='argv_config'; configKey='known'; configValue='other' })
    Assert-PublicPluginShapeEvent ([pscustomobject]@{ kind='argv_config'; publicPluginRef=1; configValue='bool_true' })
}
$invalidShapes = [ordered]@{
    'runtime unknown shape' = { param($e) $e.configPluginShape = 'PRIVATE_CANARY' }
    'runtime case shape' = { param($e) $e.configPluginShape = 'DIRECT_ENABLED' }
    'runtime nonstring shape' = { param($e) $e.configPluginShape = @('direct_enabled') }
    'runtime wrong kind' = { param($e) $e.kind = 'rpc' }
    'runtime wrong class' = { param($e) $e.class = 'request' }
    'runtime nonstring kind' = { param($e) $e.kind = @('argv_config') }
    'runtime raw key field' = { param($e) $e | Add-Member -NotePropertyName configKey -NotePropertyValue PRIVATE_CANARY }
    'runtime catalog ref mixed' = { param($e) $e | Add-Member -NotePropertyName publicPluginRef -NotePropertyValue 1 }
    'runtime raw value field' = { param($e) $e | Add-Member -NotePropertyName configValue -NotePropertyValue PRIVATE_CANARY }
    'runtime wrong root' = { param($e) $e.configLexicalRoot = 'other' }
    'runtime wrong family' = { param($e) $e.configFamily = 'plugins' }
    'runtime no assignment' = { param($e) $e.configShape = 'no_equals' }
    'runtime quoted key' = { param($e) $e.configShape = 'quoted_key' }
    'runtime no at-sign class' = { param($e) $e.configKeyClasses = @('ascii_space') }
    'runtime extra class' = { param($e) $e.configKeyClasses = @('at_sign', 'slash') }
    'runtime duplicate class' = { param($e) $e.configKeyClasses = @('at_sign', 'at_sign') }
    'runtime unordered classes' = { param($e) $e.configKeyClasses = @('at_sign', 'ascii_control', 'ascii_space') }
    'runtime string classes' = { param($e) $e.configKeyClasses = 'at_sign' }
    'runtime raw literal' = { param($e) $e.configLiteral = 'PRIVATE_CANARY' }
    'runtime missing classification' = { param($e) $e.PSObject.Properties.Remove('configLiteral') }
}
foreach ($name in $invalidShapes.Keys) {
    $mutation = $invalidShapes[$name]
    Expect-Reject $name { $e = New-UnknownPluginShape; & $mutation $e; Assert-PublicPluginShapeEvent $e }
}
$temporary = Join-Path ([IO.Path]::GetTempPath()) ('public-catalog-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporary | Out-Null
function New-PackageFixture([string]$variant) {
    $zipPath = Join-Path $temporary ([guid]::NewGuid().ToString('N') + '.zip')
    $archiveBytes = [Text.Encoding]::UTF8.GetBytes('synthetic archive fixture only')
    $manifestBytes = [Text.Encoding]::UTF8.GetBytes('{"name":"public-market","plugins":[]}')
    $manifestPath = 'app/resources/plugins/public-market/.agents/plugins/marketplace.json'
    $zip = [IO.Compression.ZipFile]::Open($zipPath, [IO.Compression.ZipArchiveMode]::Create)
    try {
        $paths = @('app/resources/app.asar', $manifestPath)
        if ($variant -eq 'duplicate') { $paths += 'app/resources/app.asar' }
        if ($variant -eq 'case-alias') { $paths += 'app/resources/APP.ASAR' }
        if ($variant -eq 'traversal') { $paths += 'app/resources/../PRIVATE_CANARY' }
        if ($variant -eq 'backslash') { $paths += 'app\resources\app.asar' }
        if ($variant -eq 'ancestor-link') { $paths += 'app/resources/' }
        if ($variant -eq 'ancestor-file') { $paths += 'app/resources' }
        if ($variant -eq 'missing') { $paths = @($manifestPath) }
        foreach ($entryPath in $paths) {
            $entry = $zip.CreateEntry($entryPath)
            if (($variant -eq 'symlink' -and $entryPath -ceq 'app/resources/app.asar') -or
                ($variant -eq 'ancestor-link' -and $entryPath -ceq 'app/resources/')) { $entry.ExternalAttributes = -1610612736 }
            if ($variant -eq 'reparse' -and $entryPath -ceq 'app/resources/app.asar') { $entry.ExternalAttributes = 1024 }
            $bytes = $archiveBytes
            if ($entryPath -ceq $manifestPath) { $bytes = $manifestBytes }
            $stream = $entry.Open()
            try { $stream.Write($bytes, 0, $bytes.Length) } finally { $stream.Dispose() }
        }
    } finally { $zip.Dispose() }
    $c = New-Catalog
    $c.archiveSha256 = Fixture-Digest $archiveBytes
    $c.coverage.manifestFiles = 1
    $c.sources += [pscustomobject]@{ kind='marketplace_manifest'; path=$manifestPath; sha256=(Fixture-Digest $manifestBytes) }
    if ($variant -eq 'archive-hash') { $c.archiveSha256 = '0' * 64 }
    if ($variant -eq 'manifest-hash') { $c.sources[1].sha256 = '0' * 64 }
    $hash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($variant -eq 'package-hash') { $hash = '0' * 64 }
    return [pscustomobject]@{ catalog=$c; path=$zipPath; sha256=$hash }
}
function New-MCPPackageFixture([string]$derivation, [string]$variant = 'valid') {
    $c=New-MCPCatalog $derivation
    $archiveBytes=[Text.Encoding]::UTF8.GetBytes('synthetic archive fixture only')
    $c.archiveSha256=Fixture-Digest $archiveBytes
    $contents=New-Object 'System.Collections.Generic.Dictionary[string,byte[]]' ([StringComparer]::Ordinal)
    $contents['app/resources/app.asar']=$archiveBytes
    for ($i=0; $i -lt $c.sources.Count; $i++) {
        $source=$c.sources[$i]
        $body='{"mcpServers":{"PublicServer":{"command":"PRIVATE_CANARY","env":{"secret":"PRIVATE_CANARY"}}}}'
        if ($source.kind -ceq 'marketplace_manifest') { $body='{"name":"public-market","plugins":[{"name":"fixture","source":"./fixture"}]}' }
        if ($source.kind -ceq 'plugin_manifest' -and $derivation -ceq 'legacy_mcp_default') { $body='{"name":"not-the-identity"}' }
        if ($source.kind -ceq 'plugin_manifest' -and $derivation -ceq 'legacy_mcp_path') { $body='{"name":"not-the-identity","mcpServers":"./config/servers.json"}' }
        if ($source.kind -ceq 'plugin_manifest' -and $derivation -ceq 'agent_mcp_default') {
            $body='{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"namespace-only"}'
        }
        if ($source.kind -ceq 'mcp_manifest' -and $derivation -ceq 'agent_mcp_default') {
            $body='{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"PublicServer":{"type":"stdio","command":"PRIVATE_CANARY"}}}'
        }
        $bytes=[Text.Encoding]::UTF8.GetBytes($body)
        $source.sha256=Fixture-Digest $bytes
        $contents[$source.path]=$bytes
    }
    if ($variant -ceq 'missing-marketplace') { [void]$contents.Remove($c.sources[0].path) }
    if ($variant -ceq 'missing-plugin') { [void]$contents.Remove($c.sources[1].path) }
    if ($variant -ceq 'missing-mcp') { [void]$contents.Remove($c.sources[2].path) }
    if ($variant -ceq 'marketplace-hash') { $c.sources[0].sha256='0'*64 }
    if ($variant -ceq 'plugin-hash') { $c.sources[1].sha256='0'*64 }
    if ($variant -ceq 'mcp-hash') { $c.sources[2].sha256='0'*64 }
    if ($variant -ceq 'plugin-case-alias') { $contents[$c.sources[1].path.Replace('plugin.json', 'PLUGIN.JSON')]=[byte[]]@(1) }
    if ($variant -ceq 'mcp-case-alias') { $contents[$c.sources[2].path.Replace('.mcp.json', '.MCP.JSON')]=[byte[]]@(1) }
    $zipPath=Join-Path $temporary ([guid]::NewGuid().ToString('N') + '.zip')
    $zip=[IO.Compression.ZipFile]::Open($zipPath, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($entryPath in $contents.Keys) {
            $entry=$zip.CreateEntry($entryPath)
            if (($variant -ceq 'plugin-link' -and $entryPath -ceq $c.sources[1].path) -or
                ($variant -ceq 'mcp-link' -and $entryPath -ceq $c.sources[2].path)) { $entry.ExternalAttributes=-1610612736 }
            $bytes=$contents[$entryPath]
            $stream=$entry.Open()
            try { $stream.Write($bytes, 0, $bytes.Length) } finally { $stream.Dispose() }
        }
    } finally { $zip.Dispose() }
    $hash=(Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    return [pscustomobject]@{ catalog=$c; path=$zipPath; sha256=$hash }
}
try {
    Expect-Pass 'actual Node package report roundtrips through PowerShell' {
        $script:nodeFixtureStage='fixture_setup'
        $modulePath = Join-Path $PSScriptRoot 'inspect-desktop-static-metadata.mjs'
        $fixtureRoot = Join-Path $temporary 'OpenAI.Codex_26.1103.3517.0_x64__abcdefghijklm'
        $generatorPath = Join-Path $temporary 'node-public-report-fixture.mjs'
        $generator = @'
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
const { inspectPackage } = await import(pathToFileURL(process.argv[2]).href);
const root = process.argv[3];
const body = Buffer.from('const modern=/[a&&b]/v; const x="browser@openai-curated-remote";');
const header = Buffer.from(JSON.stringify({files:{'.vite':{files:{build:{files:{
  'public-plugin-fixture.js':{size:body.length,offset:'0'}
}}}}}}));
const padded = Math.ceil(header.length / 4) * 4;
const archive = Buffer.alloc(16 + padded + body.length);
archive.writeUInt32LE(4, 0);
archive.writeUInt32LE(8 + padded, 4);
archive.writeUInt32LE(4 + padded, 8);
archive.writeInt32LE(header.length, 12);
header.copy(archive, 16);
body.copy(archive, 16 + padded);
fs.mkdirSync(path.join(root, 'app', 'resources'), {recursive:true});
fs.writeFileSync(path.join(root, 'app', 'resources', 'app.asar'), archive);
const marketRoot=path.join(root,'app','resources','plugins','public-market');
const writeJSON=(relative,value)=>{
  const file=path.join(marketRoot,relative);
  fs.mkdirSync(path.dirname(file),{recursive:true});
  fs.writeFileSync(file,JSON.stringify(value));
};
writeJSON('.agents/plugins/marketplace.json',{name:'public-market',plugins:[{name:'fixture',source:'./fixture'},{name:'zagent',source:'./.codex-plugin'}]});
writeJSON('fixture/.codex-plugin/plugin.json',{name:'not-the-identity',mcpServers:'./servers.json'});
writeJSON('fixture/servers.json',{mcpServers:{PublicServer:{command:'PRIVATE_CANARY',env:{secret:'PRIVATE_CANARY'}}}});
writeJSON('.codex-plugin/plugin.json',{$schema:'https://agent-plugins.org/schemas/1.0.0/plugin.schema.json',name:'namespace-only'});
writeJSON('.codex-plugin/mcp.json',{$schema:'https://agent-plugins.org/schemas/1.0.0/mcp.schema.json',mcpServers:{AgentServer:{type:'stdio',command:'PRIVATE_CANARY'}}});
process.stdout.write(JSON.stringify(inspectPackage(root)));
'@
        [IO.File]::WriteAllText($generatorPath, $generator, (New-Object Text.UTF8Encoding($false)))
        $script:nodeFixtureStage='node_execution'
        $nodeOutput = @(& node --max-old-space-size=512 $generatorPath $modulePath $fixtureRoot)
        if ($LASTEXITCODE -ne 0 -or $nodeOutput.Count -ne 1) { throw 'Node fixture failed.' }
        $json = [string]$nodeOutput[0]
        $script:nodeFixtureStage='json_decoding'
        $nodeReport = $json | ConvertFrom-Json
        $script:nodeFixtureStage='canonical_json'
        Assert-PublicCanonicalJSON $json $nodeReport
        $script:nodeFixtureStage='public_report_shape'
        Assert-PublicShape $nodeReport @('schema', 'status', 'runtimeKeyProven', 'archiveEntries', 'entries', 'publicPlugins')
        $script:nodeFixtureStage='candidate_expectations'
        if ($nodeReport.publicPlugins.candidates.Count -ne 8 -or $nodeReport.publicPlugins.status -cne 'complete') {
            throw 'Node fixture did not establish public candidates.'
        }
        $script:nodeFixtureStage='catalog_validation'
        $handoff = ConvertTo-PublicPluginEnvironment $nodeReport.publicPlugins $packageHash
        $script:nodeFixtureStage='candidate_relations'
        if (($handoff | ConvertFrom-Json).keys[0] -cne 'plugins.browser@openai-curated-remote.auto_provision') {
            throw 'Node fixture key order mismatch.'
        }
        $nested=@($nodeReport.publicPlugins.candidates | Where-Object { $_.derivation -ceq 'legacy_mcp_path' })
        if ($nested.Count -ne 1 -or $nested[0].key -cne 'plugins.fixture@public-market.mcp_servers.PublicServer.enabled' -or
            $json.Contains('PRIVATE_CANARY')) { throw 'Node fixture relation or privacy mismatch.' }
        $agent=@($nodeReport.publicPlugins.candidates | Where-Object { $_.derivation -ceq 'agent_mcp_default' })
        if ($agent.Count -ne 1 -or $agent[0].key -cne 'plugins.zagent@public-market.mcp_servers.AgentServer.enabled') {
            throw 'Node ambiguous agent root relation mismatch.'
        }
        $script:nodeFixtureStage='zip_creation'
        $expectedEntries=@(
            'app/resources/app.asar',
            'app/resources/plugins/public-market/.agents/plugins/marketplace.json',
            'app/resources/plugins/public-market/fixture/.codex-plugin/plugin.json',
            'app/resources/plugins/public-market/fixture/servers.json',
            'app/resources/plugins/public-market/.codex-plugin/plugin.json',
            'app/resources/plugins/public-market/.codex-plugin/mcp.json'
        )
        $includedEntries=New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
        $zipPath=Join-Path $temporary 'node-original.zip'
        $zip=[IO.Compression.ZipFile]::Open($zipPath, [IO.Compression.ZipArchiveMode]::Create)
        try {
            foreach ($file in @(Get-ChildItem -LiteralPath $fixtureRoot -Force -File -Recurse)) {
                $relative=$file.FullName.Substring($fixtureRoot.Length + 1).Replace('\', '/')
                if (-not $includedEntries.Add($relative)) { throw 'Duplicate synthetic ZIP entry.' }
                $entry=$zip.CreateEntry($relative)
                $stream=$entry.Open()
                $inputStream=[IO.File]::OpenRead($file.FullName)
                try { $inputStream.CopyTo($stream) } finally { $inputStream.Dispose(); $stream.Dispose() }
            }
        } finally { $zip.Dispose() }
        $script:nodeFixtureStage='zip_entry_completeness'
        if ($includedEntries.Count -ne $expectedEntries.Count) { throw 'Synthetic ZIP entry count mismatch.' }
        foreach ($expectedEntry in $expectedEntries) {
            if (-not $includedEntries.Contains($expectedEntry)) { throw 'Synthetic ZIP is missing a required source.' }
        }
        $script:nodeFixtureStage='provenance'
        $hash=(Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
        Confirm-PublicCatalogProvenance $nodeReport.publicPlugins $zipPath $hash
    } -ReportFixtureStage
    Expect-Pass 'all MCP relation source bytes bind to original ZIP without publishing values' {
        foreach ($derivation in @('legacy_mcp_inline', 'legacy_mcp_default', 'legacy_mcp_path', 'agent_mcp_default')) {
            $fixture=New-MCPPackageFixture $derivation
            $null=ConvertTo-PublicPluginEnvironment $fixture.catalog $fixture.sha256
            $output=@(Confirm-PublicCatalogProvenance $fixture.catalog $fixture.path $fixture.sha256)
            if ($output.Count -ne 0) { throw 'Provenance validation published data.' }
        }
    }
    foreach ($variant in @('missing-marketplace', 'missing-plugin', 'missing-mcp', 'marketplace-hash', 'plugin-hash',
        'mcp-hash', 'plugin-case-alias', 'mcp-case-alias', 'plugin-link', 'mcp-link')) {
        Expect-Reject ('MCP ZIP ' + $variant) {
            $fixture=New-MCPPackageFixture 'legacy_mcp_default' $variant
            $null=ConvertTo-PublicPluginEnvironment $fixture.catalog $fixture.sha256
            Confirm-PublicCatalogProvenance $fixture.catalog $fixture.path $fixture.sha256
        }
    }
    Expect-Pass 'original ZIP archive and manifest digests' {
        $fixture = New-PackageFixture 'valid'
        Confirm-PublicCatalogProvenance $fixture.catalog $fixture.path $fixture.sha256
    }
    foreach ($variant in @('duplicate', 'case-alias', 'missing', 'symlink', 'reparse', 'traversal', 'backslash', 'ancestor-link', 'ancestor-file', 'archive-hash', 'manifest-hash', 'package-hash')) {
        Expect-Reject ('ZIP ' + $variant) {
            $fixture = New-PackageFixture $variant
            Confirm-PublicCatalogProvenance $fixture.catalog $fixture.path $fixture.sha256
        }
    }
    Expect-Reject 'bounded stream length mismatch' {
        $bytes = [Text.Encoding]::UTF8.GetBytes('fixture')
        $stream = New-Object IO.MemoryStream(,$bytes)
        try { Get-BoundedStreamDigest $stream 6 16 ([Diagnostics.Stopwatch]::StartNew()) }
        finally { $stream.Dispose() }
    }
    Expect-Reject 'bounded stream size rejected before read' {
        $stream = New-Object IO.MemoryStream
        try { Get-BoundedStreamDigest $stream 17 16 ([Diagnostics.Stopwatch]::StartNew()) }
        finally { $stream.Dispose() }
    }
} finally { Remove-Item -LiteralPath $temporary -Force -Recurse }
Write-Output ('PUBLIC_CATALOG_PS_TESTS_PASSED=' + $script:passed)
