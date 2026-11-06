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
    'Assert-PublicCanonicalJSON', 'ConvertTo-PublicPluginEnvironment', 'Get-BoundedStreamDigest',
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
const body = Buffer.from('const x="browser@openai-curated-remote";');
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
        $nodeOutput = @(& node $generatorPath $modulePath $fixtureRoot)
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
