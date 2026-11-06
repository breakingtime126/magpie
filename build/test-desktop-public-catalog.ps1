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
    'Assert-PublicCanonicalJSON', 'Get-PublicCatalogFailureCodes', 'Assert-PublicCatalogFailure', 'Get-PublicPluginShapes', 'Assert-PublicPluginShapeEvent', 'Get-PublicASTSkipReasons', 'Assert-PublicAnalysis', 'ConvertTo-PublicPluginEnvironment', 'Get-BoundedStreamDigest',
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
function Expect-Pass([string]$name, [scriptblock]$action) {
    try { $null = & $action } catch { throw ('FAIL: ' + $name) }
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
try {
    Expect-Pass 'actual Node package report roundtrips through PowerShell' {
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
process.stdout.write(JSON.stringify(inspectPackage(root)));
'@
        [IO.File]::WriteAllText($generatorPath, $generator, (New-Object Text.UTF8Encoding($false)))
        $nodeOutput = @(& node --max-old-space-size=512 $generatorPath $modulePath $fixtureRoot)
        if ($LASTEXITCODE -ne 0 -or $nodeOutput.Count -ne 1) { throw 'Node fixture failed.' }
        $json = [string]$nodeOutput[0]
        $nodeReport = $json | ConvertFrom-Json
        Assert-PublicCanonicalJSON $json $nodeReport
        Assert-PublicShape $nodeReport @('schema', 'status', 'runtimeKeyProven', 'archiveEntries', 'entries', 'publicPlugins')
        if ($nodeReport.publicPlugins.candidates.Count -ne 2 -or $nodeReport.publicPlugins.status -cne 'complete') {
            throw 'Node fixture did not establish public candidates.'
        }
        $handoff = ConvertTo-PublicPluginEnvironment $nodeReport.publicPlugins $packageHash
        if (($handoff | ConvertFrom-Json).keys[0] -cne 'plugins.browser@openai-curated-remote.auto_provision') {
            throw 'Node fixture key order mismatch.'
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
