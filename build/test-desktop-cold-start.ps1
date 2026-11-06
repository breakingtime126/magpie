# Official Store-package baseline and registered-alias bridge launch. No sign-in or package modification.
# Run solely in the disposable, branch-guarded GitHub hosted job.
[CmdletBinding()]
param(
    [ValidateSet('baseline', 'bridge', 'observe')][string]$Mode = 'baseline',
    [string]$BridgeExecutable = '',
    [string]$ObserverExecutable = '',
    [string]$NativeControlExecutable = '',
    [string]$NativeControlCatalog = '',
    [switch]$EnableReviewedStartupProbe,
    [switch]$EnableReviewedLaunchCompatibility,
    [switch]$InspectStaticLauncher
)

# Pure validation helpers are AST-loaded by the focused tests, without running the probe.
function Assert-PublicShape($value, [string[]]$names) {
    if ($null -eq $value -or $value -isnot [pscustomobject]) { throw 'Invalid public catalog object.' }
    $actual = @($value.PSObject.Properties | ForEach-Object { $_.Name })
    if ($actual.Count -ne $names.Count) { throw 'Unexpected public catalog shape.' }
    foreach ($name in $actual) {
        if ($names -cnotcontains $name) { throw 'Unexpected public catalog field.' }
    }
}
function Assert-PublicInteger($value, [long]$minimum, [long]$maximum) {
    if (($value -isnot [int] -and $value -isnot [long]) -or
        $value -lt $minimum -or $value -gt $maximum) { throw 'Invalid public catalog integer.' }
}
function Assert-PublicHash($value) {
    if ($value -isnot [string] -or $value -cnotmatch '^[0-9a-f]{64}\z') { throw 'Invalid public catalog digest.' }
}
function Get-PublicDigest([byte[]]$bytes) {
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try { return [BitConverter]::ToString($algorithm.ComputeHash($bytes)).Replace('-', '').ToLowerInvariant() }
    finally { $algorithm.Dispose() }
}
function Assert-PublicCanonicalJSON([string]$text, $value) {
    # All accepted metadata strings are ASCII. Exact roundtrip also rejects duplicate JSON fields.
    if (($value | ConvertTo-Json -Depth 16 -Compress) -cne $text) {
        throw 'Noncanonical or duplicate metadata JSON fields.'
    }
}
function Get-PublicCatalogFailureCodes {
    return @('public_source_limit', 'public_source_not_packed', 'public_manifest_invalid',
        'public_manifest_limit', 'public_catalog_limit', 'public_source_path', 'public_archive_changed',
        'public_ast_bytes_limit', 'public_ast_token_limit', 'public_ast_node_limit',
        'public_ast_parse_depth_limit', 'public_ast_walk_depth_limit', 'public_ast_walk_node_limit',
        'public_ast_deadline', 'public_ast_range_error')
}
function Assert-PublicCatalogFailure($failure, [string[]]$legacyCodes) {
    Assert-PublicShape $failure @('schema', 'status', 'reason')
    Assert-PublicInteger $failure.schema 1 1
    if ($failure.status -isnot [string] -or $failure.status -cne 'blocked' -or
        $failure.reason -isnot [string] -or
        ($legacyCodes + @(Get-PublicCatalogFailureCodes)) -cnotcontains $failure.reason) {
        throw 'Unreviewed public catalog failure.'
    }
}
function ConvertFrom-PublicRPCMethodFacts([byte[]]$bytes) {
    if ($null -eq $bytes -or $bytes.Length -lt 1 -or $bytes.Length -gt 16384 -or
        (Get-PublicDigest $bytes) -cne '2b7407b2015839aa6de58d6a5b0cfc601c058ad3be0447870182da063362f2e7') {
        throw 'Invalid pinned RPC method dictionary.'
    }
    # Hash verification precedes parsing: names can only come from the fixed
    # source-derived artifact, never a runtime method or an external download.
    $text = [Text.Encoding]::UTF8.GetString($bytes)
    try { $facts = $text | ConvertFrom-Json } catch { throw 'Invalid pinned RPC method dictionary.' }
    Assert-PublicCanonicalJSON $text $facts
    Assert-PublicShape $facts @('schema', 'repository', 'commit', 'sources', 'counts', 'methods')
    if ($facts.methods -isnot [System.Array] -or $facts.methods.Count -ne 183) {
        throw 'Invalid pinned RPC method count.'
    }
    $previous = ''
    foreach ($method in $facts.methods) {
        if ($method -isnot [string] -or $method.Length -lt 1 -or $method.Length -gt 41 -or
            [StringComparer]::Ordinal.Compare($previous, $method) -ge 0) {
            throw 'Invalid pinned RPC method ordering.'
        }
        $previous = $method
    }
    return $facts
}
function Get-PublicRPCMethods([string]$DictionaryPath = '') {
    try {
        $path = $DictionaryPath
        if ([string]::IsNullOrEmpty($path)) {
            $path = Join-Path $PSScriptRoot 'desktop-rpc-observer/rpc_method_facts.json'
        }
        $info = Get-Item -LiteralPath $path
        if ($info.PSIsContainer -or ($info.Attributes -band [IO.FileAttributes]::ReparsePoint) -or
            $info.Length -lt 1 -or $info.Length -gt 16384) { throw 'Invalid dictionary file.' }
        $facts = ConvertFrom-PublicRPCMethodFacts ([IO.File]::ReadAllBytes($path))
    } catch { throw 'Invalid pinned RPC method dictionary.' }
    return @($facts.methods) + @('initialized', 'attestation/generate', 'unknown')
}
function Assert-PublicRequestTraceEvent($record) {
    $hasPresent = [bool]$record.PSObject.Properties['tracePresent']
    $hasValid = [bool]$record.PSObject.Properties['traceValid']
    if (-not $hasPresent -and -not $hasValid) { return }
    if (-not $hasPresent -or -not $hasValid -or
        $record.tracePresent -isnot [bool] -or -not $record.tracePresent -or
        $record.traceValid -isnot [bool]) { throw 'Invalid native trace shape metadata.' }
    $names = @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class', 'tracePresent', 'traceValid')
    Assert-PublicInteger $record.seq 1 4294967296
    Assert-PublicInteger $record.pid 1 4294967296
    Assert-PublicInteger $record.correlation 1 4294967296
    foreach ($name in @('kind', 'direction', 'method', 'class')) {
        if ($record.$name -isnot [string]) { throw 'Invalid native trace metadata label.' }
    }
    if ($record.kind -cne 'rpc' -or $record.direction -cne 'to_bridge' -or
        @('getAuthStatus', 'configRequirements/read', 'config/read', 'experimentalFeature/list', 'project/list', 'fs/readFile') -cnotcontains $record.method -or
        $record.class -cne 'request') { throw 'Native trace metadata occurred outside its request scope.' }
    if ($record.method -ceq 'getAuthStatus' -and $record.traceValid) {
        if (-not $record.PSObject.Properties['authParamsValid'] -or $record.authParamsValid -isnot [bool]) {
            throw 'Valid traced authentication envelope needs parameter validity.'
        }
        $names += 'authParamsValid'
        if ($record.authParamsValid) { $names += @('includeTokenRequested', 'refreshTokenRequested') }
    }
    if ($record.traceValid -and $record.method -ceq 'config/read') {
        if (-not $record.PSObject.Properties['configParamsValid'] -or $record.configParamsValid -isnot [bool]) {
            throw 'Valid traced configuration envelope needs parameter validity.'
        }
        $names += 'configParamsValid'
        if ($record.configParamsValid) { $names += @('configIncludeLayers', 'configCwdProvided') }
    }
    if ($record.traceValid -and $record.method -ceq 'experimentalFeature/list') {
        if (-not $record.PSObject.Properties['featureListParamsValid'] -or $record.featureListParamsValid -isnot [bool]) {
            throw 'Valid traced feature-list envelope needs parameter validity.'
        }
        $names += 'featureListParamsValid'
        if ($record.featureListParamsValid) {
            $names += @('featureListCursorProvided', 'featureListLimitProvided', 'featureListThreadIdProvided')
        }
    }
    if ($record.traceValid -and $record.method -ceq 'project/list') {
        if (-not $record.PSObject.Properties['projectListParamsValid'] -or $record.projectListParamsValid -isnot [bool]) {
            throw 'Valid traced project envelope needs parameter validity.'
        }
        $names += 'projectListParamsValid'
        if ($record.projectListParamsValid) {
            $names += @('projectListCursorProvided', 'projectListLimitProvided', 'projectListSortKey', 'projectListSortDirection')
        }
    }
    if ($record.traceValid -and $record.method -ceq 'fs/readFile') {
        if (-not $record.PSObject.Properties['fsReadParamsShapeValid'] -or $record.fsReadParamsShapeValid -isnot [bool]) {
            throw 'Valid traced file-read envelope needs parameter shape validity.'
        }
        $names += 'fsReadParamsShapeValid'
        if ($record.fsReadParamsShapeValid) { $names += @('fsReadPathForm', 'fsReadBasename', 'fsReadLexicalRoot') }
    }
    Assert-PublicShape $record $names
}
function Assert-PublicAuthRequestEvent($record) {
    Assert-PublicRequestTraceEvent $record
    $hasValid = [bool]$record.PSObject.Properties['authParamsValid']
    $hasInclude = [bool]$record.PSObject.Properties['includeTokenRequested']
    $hasRefresh = [bool]$record.PSObject.Properties['refreshTokenRequested']
    if (-not $hasValid -and -not $hasInclude -and -not $hasRefresh) { return }
    if (-not $hasValid -or $record.authParamsValid -isnot [bool]) {
        throw 'Invalid authentication request metadata validity.'
    }
    $names = @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class', 'authParamsValid')
    if ($record.authParamsValid) { $names += @('includeTokenRequested', 'refreshTokenRequested') }
    if ($record.PSObject.Properties['tracePresent']) { $names += @('tracePresent', 'traceValid') }
    Assert-PublicShape $record $names
    Assert-PublicInteger $record.seq 1 4294967296
    Assert-PublicInteger $record.pid 1 4294967296
    Assert-PublicInteger $record.correlation 1 4294967296
    foreach ($name in @('kind', 'direction', 'method', 'class')) {
        if ($record.$name -isnot [string]) { throw 'Invalid authentication request metadata label.' }
    }
    if ($record.kind -cne 'rpc' -or $record.direction -cne 'to_bridge' -or
        $record.method -cne 'getAuthStatus' -or $record.class -cne 'request') {
        throw 'Authentication flags occurred outside a client request.'
    }
    if ($record.authParamsValid -and
        ($record.includeTokenRequested -isnot [bool] -or $record.refreshTokenRequested -isnot [bool])) {
        throw 'Invalid authentication request flag type.'
    }
}
# Fixed, source-reviewed thread/list labels only. Shapes do not validate a
# request, identify a project/thread, or authorize routing any filter.
function Get-PublicThreadListShapeFields {
    return @('threadListProjectIdShape', 'threadListParentIdShape',
        'threadListAncestorIdShape', 'threadListAttachedIdShape')
}
function Get-PublicThreadListErrorClasses {
    return @('thread_list_project_id', 'thread_list_parent_id', 'thread_list_ancestor_id',
        'thread_list_attached_id', 'thread_list_sort_key', 'thread_list_sort_direction',
        'thread_list_provider_filter', 'thread_list_cursor')
}
function Assert-PublicThreadListEvent($record) {
    $fields = @(Get-PublicThreadListShapeFields)
    $hasShape = $false
    foreach ($field in $fields) { if ($record.PSObject.Properties[$field]) { $hasShape = $true } }
    $hasRejection = $record.PSObject.Properties['class'] -and
        (Get-PublicThreadListErrorClasses) -ccontains $record.class
    if (-not $hasShape -and -not $hasRejection) { return }
    $names = @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class')
    if ($hasRejection) { $names += 'code' } else { $names += $fields }
    Assert-PublicShape $record $names
    foreach ($field in @('seq', 'pid', 'correlation')) { Assert-PublicInteger $record.$field 1 4294967296 }
    foreach ($field in @('kind', 'direction', 'method', 'class')) {
        if ($record.$field -isnot [string]) { throw 'Invalid thread-list metadata label type.' }
    }
    if ($record.method -cne 'thread/list') { throw 'Thread-list metadata occurred outside its method.' }
    if ($hasRejection) {
        Assert-PublicInteger $record.code -32602 -32602
        if ($record.kind -cne 'rpc_error' -or $record.direction -cne 'from_bridge') {
            throw 'Thread-list rejection occurred outside a correlated bridge error.'
        }
    } else {
        if ($record.kind -cne 'rpc' -or $record.direction -cne 'to_bridge' -or $record.class -cne 'request') {
            throw 'Thread-list shapes occurred outside a client request.'
        }
        foreach ($field in $fields) {
            if ($record.$field -isnot [string] -or @('absent', 'null', 'string', 'invalid') -cnotcontains $record.$field) {
                throw 'Invalid thread-list filter shape.'
            }
        }
    }
}
function Assert-PublicReadRequestEvent($record) {
    Assert-PublicThreadListEvent $record
    Assert-PublicRequestTraceEvent $record
    Assert-PublicReadTargetEvent $record
    $configFields = @('configParamsValid', 'configIncludeLayers', 'configCwdProvided')
    $featureFields = @('featureListParamsValid', 'featureListCursorProvided',
        'featureListLimitProvided', 'featureListThreadIdProvided')
    $hasConfig = $false
    $hasFeature = $false
    foreach ($field in $configFields) {
        if ($record.PSObject.Properties[$field]) { $hasConfig = $true }
    }
    foreach ($field in $featureFields) {
        if ($record.PSObject.Properties[$field]) { $hasFeature = $true }
    }
    if (-not $hasConfig -and -not $hasFeature) { return }
    if ($hasConfig -and $hasFeature) { throw 'Mixed request metadata families.' }
    $fields = $featureFields
    $method = 'experimentalFeature/list'
    if ($hasConfig) { $fields = $configFields; $method = 'config/read' }
    $validity = $fields[0]
    if (-not $record.PSObject.Properties[$validity] -or $record.$validity -isnot [bool]) {
        throw 'Invalid read-request parameter validity.'
    }
    $names = @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class', $validity)
    if ($record.$validity) { $names += $fields[1..($fields.Count - 1)] }
    if ($record.PSObject.Properties['tracePresent']) { $names += @('tracePresent', 'traceValid') }
    Assert-PublicShape $record $names
    foreach ($field in @('seq', 'pid', 'correlation')) { Assert-PublicInteger $record.$field 1 4294967296 }
    foreach ($field in @('kind', 'direction', 'method', 'class')) {
        if ($record.$field -isnot [string]) { throw 'Invalid read-request metadata label.' }
    }
    if ($record.kind -cne 'rpc' -or $record.direction -cne 'to_bridge' -or
        $record.method -cne $method -or $record.class -cne 'request') {
        throw 'Read-request metadata occurred outside its exact request scope.'
    }
    if ($record.$validity) {
        foreach ($field in $fields[1..($fields.Count - 1)]) {
            if ($record.$field -isnot [bool]) { throw 'Invalid read-request boolean.' }
        }
    }
}
function Get-PublicReadTargetLabels([string]$Field) {
    switch -CaseSensitive ($Field) {
        'projectListSortKey' { return @('absent', 'position', 'recencyAt') }
        'projectListSortDirection' { return @('absent', 'asc', 'desc') }
        'fsReadPathForm' { return @('empty', 'drive_absolute', 'unc_absolute', 'drive_relative', 'rooted', 'relative', 'device') }
        'fsReadBasename' { return @('config.toml', 'AGENTS.md', 'SKILL.md', 'auth.json', 'other') }
        'fsReadLexicalRoot' { return @('unconfigured', 'unclassified', 'outside', 'ambiguous', 'primary_home',
            'primary_codex_home', 'secondary_home', 'secondary_codex_home', 'workspace', 'verified_package', 'control_home') }
        default { throw 'Unknown read-target metadata field.' }
    }
}
function Assert-PublicReadTargetEvent($record) {
    Assert-PublicRequestTraceEvent $record
    $project = @('projectListParamsValid', 'projectListCursorProvided', 'projectListLimitProvided',
        'projectListSortKey', 'projectListSortDirection')
    $file = @('fsReadParamsShapeValid', 'fsReadPathForm', 'fsReadBasename', 'fsReadLexicalRoot')
    $hasProject = $false
    $hasFile = $false
    foreach ($field in $project) { if ($record.PSObject.Properties[$field]) { $hasProject = $true } }
    foreach ($field in $file) { if ($record.PSObject.Properties[$field]) { $hasFile = $true } }
    if (-not $hasProject -and -not $hasFile) { return }
    if ($hasProject -and $hasFile) { throw 'Mixed read-target metadata families.' }
    $fields = $project
    $method = 'project/list'
    if ($hasFile) { $fields = $file; $method = 'fs/readFile' }
    $validity = $fields[0]
    if (-not $record.PSObject.Properties[$validity] -or $record.$validity -isnot [bool]) {
        throw 'Invalid read-target parameter validity.'
    }
    $names = @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class', $validity)
    if ($record.$validity) { $names += $fields[1..($fields.Count - 1)] }
    if ($record.PSObject.Properties['tracePresent']) { $names += @('tracePresent', 'traceValid') }
    Assert-PublicShape $record $names
    foreach ($field in @('seq', 'pid', 'correlation')) { Assert-PublicInteger $record.$field 1 4294967296 }
    foreach ($field in @('kind', 'direction', 'method', 'class')) {
        if ($record.$field -isnot [string]) { throw 'Invalid read-target metadata label.' }
    }
    if ($record.kind -cne 'rpc' -or $record.direction -cne 'to_bridge' -or
        $record.method -cne $method -or $record.class -cne 'request') {
        throw 'Read-target metadata occurred outside its exact request scope.'
    }
    if (-not $record.$validity) { return }
    foreach ($field in $fields[1..($fields.Count - 1)]) {
        if ($field -cin @('projectListCursorProvided', 'projectListLimitProvided')) {
            if ($record.$field -isnot [bool]) { throw 'Invalid project-list presence boolean.' }
        } elseif ($record.$field -isnot [string] -or @(Get-PublicReadTargetLabels $field) -cnotcontains $record.$field) {
            throw 'Invalid fixed read-target classification.'
        }
    }
    if ($hasProject -and $record.projectListSortKey -ceq 'absent' -and $record.projectListSortDirection -cne 'absent') {
        throw 'Project-list sort direction requires a sort key.'
    }
}
function Get-PublicPluginShapes {
    return @('direct_enabled', 'direct_auto_provision', 'mcp_server_enabled', 'app_enabled',
        'app_destructive_enabled', 'app_open_world_enabled', 'app_default_tools_enabled', 'app_tool_enabled')
}
function Assert-PublicPluginShapeEvent($record) {
    if (-not $record.PSObject.Properties['configPluginShape']) { return }
    Assert-PublicShape $record @('seq', 'pid', 'kind', 'class', 'configShape', 'configFamily',
        'configLiteral', 'configLexicalRoot', 'configKeyClasses', 'configPluginShape')
    Assert-PublicInteger $record.seq 1 4294967296
    Assert-PublicInteger $record.pid 1 4294967296
    foreach ($name in @('kind', 'class', 'configShape', 'configFamily', 'configLiteral', 'configLexicalRoot')) {
        if ($record.$name -isnot [string]) { throw 'Invalid plugin path shape label type.' }
    }
    if ($record.kind -cne 'argv_config' -or $record.class -cne 'unknown' -or
        $record.configShape -cne 'other_key' -or $record.configFamily -cne 'other' -or
        $record.configLexicalRoot -cne 'plugins' -or $record.configPluginShape -isnot [string] -or
        @(Get-PublicPluginShapes) -cnotcontains $record.configPluginShape -or
        $record.configLiteral -isnot [string] -or @('bool_true', 'bool_false', 'other') -cnotcontains $record.configLiteral) {
        throw 'Invalid unknown plugin path shape metadata.'
    }
    $classes = $record.configKeyClasses
    if ($classes -isnot [System.Array] -or $classes.Count -lt 1 -or $classes.Count -gt 3 -or
        $classes[0] -cne 'at_sign') { throw 'Plugin path shape has inconsistent key classes.' }
    $allowed = @('at_sign', 'ascii_space', 'ascii_control')
    $previous = -1
    foreach ($label in $classes) {
        if ($label -isnot [string]) { throw 'Invalid plugin path shape class type.' }
        $index = [Array]::IndexOf($allowed, $label)
        if ($index -le $previous) { throw 'Plugin path shape has an invalid or duplicate key class.' }
        $previous = $index
    }
}
function Get-PublicASTSkipReasons {
    return @('syntax_error', 'public_ast_token_limit', 'public_ast_node_limit',
        'public_ast_parse_depth_limit', 'public_ast_walk_depth_limit',
        'public_ast_walk_node_limit', 'public_ast_deadline')
}
function Assert-PublicAnalysis($analysis, $sources, $skippedFiles) {
    Assert-PublicShape $analysis @('parser', 'methods', 'constructions', 'skipped')
    Assert-PublicInteger $skippedFiles 0 2048
    if ($analysis.skipped -isnot [System.Array] -or $analysis.skipped.Count -gt 7) {
        throw 'Invalid public skipped-file diagnostic count.'
    }
    $skipReasons = @(Get-PublicASTSkipReasons)
    $previousSkip = -1
    [long]$skipTotal = 0
    foreach ($skipped in $analysis.skipped) {
        Assert-PublicShape $skipped @('reason', 'count')
        if ($skipped.reason -isnot [string]) { throw 'Invalid public skipped-file reason type.' }
        $reasonIndex = [Array]::IndexOf($skipReasons, $skipped.reason)
        if ($reasonIndex -le $previousSkip) { throw 'Unknown, unordered or duplicate public skipped-file reason.' }
        Assert-PublicInteger $skipped.count 1 2048
        $skipTotal += $skipped.count
        if ($skipTotal -gt 2048) { throw 'Public skipped-file aggregate bound exceeded.' }
        $previousSkip = $reasonIndex
    }
    if ($skipTotal -ne $skippedFiles) { throw 'Public skipped-file counts do not match scan coverage.' }
    if ($analysis.parser -isnot [string] -or $analysis.parser -cne 'acorn_8_15_0') {
        throw 'Unreviewed public JavaScript parser.'
    }
    $methodNames = @('getConfigOverrides', 'startupCommand')
    if ($analysis.methods -isnot [System.Array] -or $analysis.methods.Count -ne 2) {
        throw 'Invalid public method diagnostic count.'
    }
    $seenMethods = @{}
    $methodIndex = 0
    [long]$definitionTotal = 0
    [long]$unresolvedTotal = 0
    foreach ($method in $analysis.methods) {
        Assert-PublicShape $method @('name', 'definitions', 'unresolved')
        if ($method.name -isnot [string] -or $methodNames -cnotcontains $method.name -or
            $seenMethods.ContainsKey($method.name) -or $method.name -cne $methodNames[$methodIndex]) {
            throw 'Invalid, unordered or duplicate public method diagnostic.'
        }
        Assert-PublicInteger $method.definitions 0 4000000
        Assert-PublicInteger $method.unresolved 0 4000000
        if ($method.definitions -eq 0 -and $method.unresolved -ne 0) {
            throw 'Unresolved constructions have no public method definition.'
        }
        $definitionTotal += $method.definitions
        $unresolvedTotal += $method.unresolved
        if ($definitionTotal -gt 4000000 -or $unresolvedTotal -gt 4000000) {
            throw 'Public method diagnostic aggregate bound exceeded.'
        }
        $seenMethods[$method.name] = $method.definitions
        $methodIndex++
    }
    if ($analysis.constructions -isnot [System.Array] -or $analysis.constructions.Count -gt 128) {
        throw 'Invalid public construction diagnostic count.'
    }
    $shapes = @(Get-PublicPluginShapes)
    [long]$constructionTotal = 0
    $seenConstructions = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    foreach ($construction in $analysis.constructions) {
        Assert-PublicShape $construction @('source', 'method', 'form', 'shape', 'value', 'count')
        Assert-PublicInteger $construction.source 0 ($sources.Count - 1)
        Assert-PublicInteger $construction.count 1 4000000
        $constructionTotal += $construction.count
        if ($constructionTotal -gt 4000000) { throw 'Public construction aggregate bound exceeded.' }
        if ($sources[$construction.source].kind -cne 'asar_js' -or
            $construction.method -isnot [string] -or $methodNames -cnotcontains $construction.method -or
            $construction.form -isnot [string] -or @('literal', 'template', 'concat') -cnotcontains $construction.form -or
            $construction.shape -isnot [string] -or $shapes -cnotcontains $construction.shape -or
            $construction.value -isnot [string] -or @('bool_true', 'bool_false', 'dynamic', 'other') -cnotcontains $construction.value) {
            throw 'Invalid public construction classification or source.'
        }
        if ($seenMethods[$construction.method] -eq 0) { throw 'Construction has no public method definition.' }
        $identity = @($construction.source, $construction.method, $construction.form,
            $construction.shape, $construction.value) -join '|'
        if (-not $seenConstructions.Add($identity)) { throw 'Duplicate public construction diagnostic.' }
    }
}
function Get-PublicMarketplaceRoot([string]$path) {
    if ($path -cnotmatch '^(app/resources/plugins/[A-Za-z0-9][A-Za-z0-9_-]{0,63})/(\.agents/plugins/(api_)?marketplace\.json|\.(claude|cursor)-plugin/marketplace\.json)\z') {
        throw 'Unreviewed public marketplace source path.'
    }
    return $Matches[1]
}
function Assert-PublicManifestPath([string]$path) {
    if ($path.Length -gt 512 -or $path -cnotmatch '^app/resources/plugins/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*\z') {
        throw 'Unreviewed public declaration source path.'
    }
    foreach ($part in $path.Split('/')) {
        if ($part.Length -gt 64 -or $part -ceq '.' -or $part -ceq '..' -or $part.EndsWith('.')) {
            throw 'Unsafe public declaration path component.'
        }
    }
}
function Get-PublicPluginManifestRoot([string]$path, [string]$derivation = '') {
    Assert-PublicManifestPath $path
    $suffixes=@('/.codex-plugin/plugin.json', '/.claude-plugin/plugin.json', '/.cursor-plugin/plugin.json')
    if ($derivation -ceq 'agent_mcp_default') { $suffixes=@('/plugin.json') }
    elseif ($derivation -ceq '') { $suffixes += '/plugin.json' }
    elseif (@('legacy_mcp_inline', 'legacy_mcp_default', 'legacy_mcp_path') -cnotcontains $derivation) {
        throw 'Unreviewed public MCP derivation.'
    }
    foreach ($suffix in $suffixes) {
        if ($path.EndsWith($suffix, [StringComparison]::Ordinal)) {
            return $path.Substring(0, $path.Length - $suffix.Length)
        }
    }
    throw 'Unreviewed public plugin manifest path.'
}
function Assert-PublicMCPRelation($candidate, $sources) {
    Assert-PublicInteger $candidate.marketplaceSource 0 ($sources.Count - 1)
    Assert-PublicInteger $candidate.pluginSource 0 ($sources.Count - 1)
    $marketplace = $sources[$candidate.marketplaceSource]
    $plugin = $sources[$candidate.pluginSource]
    $declaration = $sources[$candidate.source]
    if ($marketplace.kind -cne 'marketplace_manifest' -or $plugin.kind -cne 'plugin_manifest') {
        throw 'Invalid public MCP relation source kinds.'
    }
    $marketplaceRoot = Get-PublicMarketplaceRoot $marketplace.path
    $pluginRoot = Get-PublicPluginManifestRoot $plugin.path $candidate.derivation
    if ($pluginRoot -cne $marketplaceRoot) {
        if (-not $pluginRoot.StartsWith(($marketplaceRoot + '/'), [StringComparison]::Ordinal) -or
            $pluginRoot.Substring($marketplaceRoot.Length + 1).Split('/').Count -gt 8) {
            throw 'Public plugin source is outside its marketplace root.'
        }
    }
    if ($candidate.derivation -ceq 'legacy_mcp_inline') {
        if ($candidate.source -ne $candidate.pluginSource) { throw 'Inline MCP declaration must use its plugin manifest source.' }
    } else {
        if ($declaration.kind -cne 'mcp_manifest' -or
            -not $declaration.path.StartsWith(($pluginRoot + '/'), [StringComparison]::Ordinal) -or
            $declaration.path.Substring($pluginRoot.Length + 1).Split('/').Count -gt 8) {
            throw 'Public MCP declaration is outside its plugin root.'
        }
        if (($candidate.derivation -ceq 'legacy_mcp_default' -and $declaration.path -cne ($pluginRoot + '/.mcp.json')) -or
            ($candidate.derivation -ceq 'agent_mcp_default' -and $declaration.path -cne ($pluginRoot + '/mcp.json'))) {
            throw 'Public MCP default declaration path mismatch.'
        }
    }
}
function ConvertTo-PublicPluginEnvironment($catalog, [string]$packageSha256) {
    Assert-PublicShape $catalog @('schema', 'status', 'archiveSha256', 'keysSha256', 'coverage', 'sources', 'candidates', 'analysis', 'manifestAnalysis')
    Assert-PublicInteger $catalog.schema 1 1
    Assert-PublicHash $packageSha256
    Assert-PublicHash $catalog.archiveSha256
    Assert-PublicHash $catalog.keysSha256
    if ($catalog.status -isnot [string] -or @('complete', 'partial') -cnotcontains $catalog.status) { throw 'Unsupported public catalog status.' }
    Assert-PublicShape $catalog.coverage @('jsFiles', 'jsComplete', 'jsSkipped', 'sourceBytes', 'manifestFiles')
    foreach ($name in @('jsFiles', 'jsComplete', 'jsSkipped')) { Assert-PublicInteger $catalog.coverage.$name 0 2048 }
    Assert-PublicInteger $catalog.coverage.sourceBytes 1 134217728
    Assert-PublicInteger $catalog.coverage.manifestFiles 0 16
    if ($catalog.coverage.jsComplete + $catalog.coverage.jsSkipped -ne $catalog.coverage.jsFiles -or
        ($catalog.status -ceq 'complete' -and $catalog.coverage.jsSkipped -ne 0) -or
        ($catalog.status -ceq 'partial' -and $catalog.coverage.jsSkipped -eq 0)) {
        throw 'Inconsistent public catalog coverage.'
    }
    Assert-PublicShape $catalog.manifestAnalysis @('pluginEntries', 'pluginManifests', 'mcpFiles', 'skippedEntries')
    Assert-PublicInteger $catalog.manifestAnalysis.pluginEntries 0 2048
    Assert-PublicInteger $catalog.manifestAnalysis.pluginManifests 0 64
    Assert-PublicInteger $catalog.manifestAnalysis.mcpFiles 0 64
    Assert-PublicInteger $catalog.manifestAnalysis.skippedEntries 0 $catalog.manifestAnalysis.pluginEntries
    if ($catalog.coverage.manifestFiles + $catalog.manifestAnalysis.pluginManifests + $catalog.manifestAnalysis.mcpFiles -gt 64) {
        throw 'Public JSON source count exceeds its bound.'
    }
    if ($catalog.sources -isnot [System.Array] -or $catalog.sources.Count -lt 1 -or $catalog.sources.Count -gt 64 -or
        $catalog.candidates -isnot [System.Array] -or $catalog.candidates.Count -lt 1 -or $catalog.candidates.Count -gt 128) {
        throw 'Public catalog is empty or exceeds its bound.'
    }
    $seenSources = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    $jsSources = 0
    $manifestSources = 0
    $pluginSources = 0
    $mcpSources = 0
    foreach ($source in $catalog.sources) {
        Assert-PublicShape $source @('kind', 'path', 'sha256')
        Assert-PublicHash $source.sha256
        if ($source.path -isnot [string] -or $source.path.Length -gt 512 -or $seenSources.Contains($source.path)) {
            throw 'Invalid or duplicate public source path.'
        }
        [void]$seenSources.Add($source.path)
        if ($source.kind -isnot [string]) { throw 'Invalid public source kind type.' }
        if ($source.kind -ceq 'asar_js') {
            if ($source.path -cnotmatch '^\.vite/build/[A-Za-z0-9][A-Za-z0-9_.-]{0,190}\.js\z') {
                throw 'Unreviewed public JavaScript source path.'
            }
            $jsSources++
        } elseif ($source.kind -ceq 'marketplace_manifest') {
            $null = Get-PublicMarketplaceRoot $source.path
            $manifestSources++
        } elseif ($source.kind -ceq 'plugin_manifest') {
            $null = Get-PublicPluginManifestRoot $source.path
            $pluginSources++
        } elseif ($source.kind -ceq 'mcp_manifest') {
            Assert-PublicManifestPath $source.path
            if (-not $source.path.EndsWith('.json', [StringComparison]::Ordinal)) { throw 'Unreviewed public MCP manifest extension.' }
            $mcpSources++
        } else { throw 'Unreviewed public source kind.' }
    }
    if ($jsSources -gt $catalog.coverage.jsComplete -or $manifestSources -gt $catalog.coverage.manifestFiles -or
        $pluginSources -gt $catalog.manifestAnalysis.pluginManifests -or $mcpSources -gt $catalog.manifestAnalysis.mcpFiles) {
        throw 'Public source provenance is incomplete.'
    }
    Assert-PublicAnalysis $catalog.analysis $catalog.sources $catalog.coverage.jsSkipped
    $keys = @()
    $relatedSources = New-Object 'System.Collections.Generic.HashSet[int]'
    $previous = $null
    foreach ($candidate in $catalog.candidates) {
        if ($null -eq $candidate -or $candidate -isnot [pscustomobject] -or
            $null -eq $candidate.PSObject.Properties['key'] -or $candidate.key -isnot [string]) {
            throw 'Invalid public plugin candidate.'
        }
        $nested = $candidate.key -cmatch '^plugins\.[A-Za-z0-9][A-Za-z0-9_-]{0,63}@[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.mcp_servers\.[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.enabled\z'
        $direct = $candidate.key -cmatch '^plugins\.[A-Za-z0-9][A-Za-z0-9_-]{0,63}@[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.(enabled|auto_provision)\z'
        if ($candidate.key.Length -gt 128 -or (-not $nested -and -not $direct) -or
            ($null -ne $previous -and [StringComparer]::Ordinal.Compare($previous, $candidate.key) -ge 0)) {
            throw 'Invalid, duplicate or unordered public plugin key.'
        }
        if ($nested) { Assert-PublicShape $candidate @('key', 'source', 'derivation', 'marketplaceSource', 'pluginSource') }
        else { Assert-PublicShape $candidate @('key', 'source', 'derivation') }
        Assert-PublicInteger $candidate.source 0 ($catalog.sources.Count - 1)
        if ($candidate.derivation -isnot [string]) { throw 'Invalid public derivation type.' }
        $sourceKind = $catalog.sources[$candidate.source].kind
        if ($nested) {
            if ($catalog.manifestAnalysis.pluginEntries -eq 0) { throw 'Public MCP candidate has no marketplace entry coverage.' }
            Assert-PublicMCPRelation $candidate $catalog.sources
            [void]$relatedSources.Add([int]$candidate.pluginSource)
            [void]$relatedSources.Add([int]$candidate.source)
        } elseif (-not (($sourceKind -ceq 'asar_js' -and @('id_literal', 'key_literal') -ccontains $candidate.derivation) -or
            ($sourceKind -ceq 'marketplace_manifest' -and $candidate.derivation -ceq 'manifest_identity'))) {
            throw 'Invalid public candidate provenance.'
        }
        $keys += $candidate.key
        $previous = $candidate.key
    }
    for ($index=0; $index -lt $catalog.sources.Count; $index++) {
        if (@('plugin_manifest', 'mcp_manifest') -ccontains $catalog.sources[$index].kind -and
            -not $relatedSources.Contains($index)) { throw 'Public MCP source lacks a validated candidate relation.' }
    }
    $digest = Get-PublicDigest ([Text.Encoding]::UTF8.GetBytes(($keys -join "`n")))
    if ($digest -cne $catalog.keysSha256) { throw 'Public catalog key digest mismatch.' }
    $environment = [ordered]@{
        schema = 1
        packageSha256 = $packageSha256
        archiveSha256 = $catalog.archiveSha256
        keysSha256 = $digest
        keys = @($keys)
    } | ConvertTo-Json -Depth 4 -Compress
    if ([Text.Encoding]::UTF8.GetByteCount($environment) -gt 16384) { throw 'Public catalog environment exceeds its bound.' }
    return $environment
}
function Get-BoundedStreamDigest([IO.Stream]$stream, [long]$expected, [long]$maximum, [Diagnostics.Stopwatch]$watch) {
    if ($expected -lt 1 -or $expected -gt $maximum) { throw 'Public source size is outside its bound.' }
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try {
        $buffer = New-Object byte[] 65536
        [long]$total = 0
        while (($count = $stream.Read($buffer, 0, $buffer.Length)) -ne 0) {
            $total += $count
            if ($total -gt $expected -or $watch.ElapsedMilliseconds -gt 120000) {
                throw 'Public source streaming bound exceeded.'
            }
            [void]$algorithm.TransformBlock($buffer, 0, $count, $buffer, 0)
        }
        if ($total -ne $expected -or $watch.ElapsedMilliseconds -gt 120000) { throw 'Public source length or time mismatch.' }
        [void]$algorithm.TransformFinalBlock((New-Object byte[] 0), 0, 0)
        return [BitConverter]::ToString($algorithm.Hash).Replace('-', '').ToLowerInvariant()
    } finally { $algorithm.Dispose() }
}
function Assert-PublicRegularPath([string]$path) {
    if (-not [IO.Path]::IsPathRooted($path) -or [IO.Path]::GetFullPath($path) -cne $path) {
        throw 'Public package path is not canonical.'
    }
    $root = [IO.Path]::GetPathRoot($path)
    $current = $root
    $parts = $path.Substring($root.Length).Split([char[]]@([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar),
        [StringSplitOptions]::RemoveEmptyEntries)
    foreach ($part in $parts) {
        $current = Join-Path $current $part
        $item = Get-Item -Force -LiteralPath $current -ErrorAction Stop
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Public package path is redirected.' }
    }
    if ($item.PSIsContainer) { throw 'Public package is not a regular file.' }
}
function Assert-PublicZipIndex($zip) {
    if ($zip.Entries.Count -gt 100000) { throw 'Public package ZIP entry count exceeds its bound.' }
    $seen = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    $directories = New-Object 'System.Collections.Generic.Dictionary[string,bool]' ([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $zip.Entries) {
        $name = $entry.FullName
        if ([string]::IsNullOrEmpty($name) -or $name.Length -gt 1024 -or
            $name.StartsWith('/') -or $name -match '[\\:\x00-\x1f\x7f]') {
            throw 'Public package ZIP path is not canonical.'
        }
        $directory = $name.EndsWith('/')
        $canonical = $name
        if ($directory) { $canonical = $name.Substring(0, $name.Length - 1) }
        foreach ($part in $canonical.Split('/')) {
            if ($part.Length -eq 0 -or $part -ceq '.' -or $part -ceq '..' -or $part -match '[. ]\z') {
                throw 'Public package ZIP path has an unsafe component.'
            }
        }
        if (-not $seen.Add($canonical)) { throw 'Public package ZIP paths are ambiguous.' }
        $directories.Add($canonical, $directory)
        $unixType = ($entry.ExternalAttributes -shr 16) -band 61440
        if (($entry.ExternalAttributes -band 1024) -ne 0 -or
            ($directory -and $unixType -ne 0 -and $unixType -ne 16384) -or
            (-not $directory -and $unixType -ne 0 -and $unixType -ne 32768) -or
            (-not $directory -and ($entry.ExternalAttributes -band 16) -ne 0)) {
            throw 'Public package ZIP contains a link or nonregular entry.'
        }
    }
    foreach ($name in $directories.Keys) {
        $parent = $name
        while (($separator = $parent.LastIndexOf('/')) -ge 0) {
            $parent = $parent.Substring(0, $separator)
            if ($directories.ContainsKey($parent) -and -not $directories[$parent]) {
                throw 'Public package ZIP file conflicts with a descendant path.'
            }
        }
    }
}
function Get-PublicZipEntryDigest($zip, [string]$path, [long]$maximum, [Diagnostics.Stopwatch]$watch) {
    # Exact and case-insensitive uniqueness both matter on the installed Windows filesystem.
    $matches = @($zip.Entries | Where-Object { [StringComparer]::OrdinalIgnoreCase.Equals($_.FullName, $path) })
    if ($matches.Count -ne 1 -or $matches[0].FullName -cne $path) { throw 'Public source ZIP entry is missing or ambiguous.' }
    $entry = $matches[0]
    $unixType = ($entry.ExternalAttributes -shr 16) -band 61440
    if (($unixType -ne 0 -and $unixType -ne 32768) -or
        ($entry.ExternalAttributes -band 1040) -ne 0 -or $entry.CompressedLength -gt $maximum) {
        throw 'Public source ZIP entry is not a regular bounded file.'
    }
    if ($entry.Length -lt 1 -or $entry.Length -gt $maximum) { throw 'Public source ZIP size exceeds its bound.' }
    $stream = $entry.Open()
    try { return Get-BoundedStreamDigest $stream $entry.Length $maximum $watch }
    finally { $stream.Dispose() }
}
function Confirm-PublicCatalogProvenance($catalog, [string]$packagePath, [string]$packageSha256) {
    $packageStream = $null
    $zip = $null
    try {
        Assert-PublicRegularPath $packagePath
        $watch = [Diagnostics.Stopwatch]::StartNew()
        # Hold the same original download read-only against writes through hash and ZIP inspection.
        $packageStream = [IO.File]::Open($packagePath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        $actualPackage = Get-BoundedStreamDigest $packageStream $packageStream.Length 4294967296 $watch
        if ($actualPackage -cne $packageSha256) { throw 'Original package digest changed.' }
        $packageStream.Position = 0
        $zip = New-Object IO.Compression.ZipArchive($packageStream, [IO.Compression.ZipArchiveMode]::Read, $true)
        Assert-PublicZipIndex $zip
        $actualArchive = Get-PublicZipEntryDigest $zip 'app/resources/app.asar' 2147483648 $watch
        if ($actualArchive -cne $catalog.archiveSha256) { throw 'Installed archive does not match the original package.' }
        foreach ($source in $catalog.sources) {
            if (@('marketplace_manifest', 'plugin_manifest', 'mcp_manifest') -ccontains $source.kind) {
                $actualManifest = Get-PublicZipEntryDigest $zip $source.path 262144 $watch
                if ($actualManifest -cne $source.sha256) { throw 'Installed manifest does not match the original package.' }
            }
        }
    } catch { throw 'Public catalog provenance could not be verified against the original signed package.' }
    finally {
        if ($zip) { $zip.Dispose() }
        if ($packageStream) { $packageStream.Dispose() }
    }
}
function Assert-PublicEnvironmentSize($environment) {
    [long]$characters = 1
    foreach ($name in $environment.Keys) {
        $value = $environment[$name]
        if ($name -isnot [string] -or $value -isnot [string] -or $name.Contains('=') -or
            $name.IndexOf([char]0) -ge 0 -or $value.IndexOf([char]0) -ge 0) {
            throw 'Invalid child environment entry.'
        }
        $characters += $name.Length + $value.Length + 2
        if ($characters -gt 30000) { throw 'Child environment exceeds the Windows diagnostic bound.' }
    }
}
function Assert-PublicPluginEvent($record, $catalog, $seenCatalogs) {
    if ($null -ne $catalog) {
        Assert-PublicInteger $record.pid 1 4294967296
        Assert-PublicInteger $record.seq 1 4294967296
        $startKey = 'start:' + [string]$record.pid
        if ($record.kind -ceq 'observer_start') {
            Assert-PublicShape $record @('seq', 'pid', 'kind', 'class')
            if ($record.class -cne 'test_only' -or $seenCatalogs.ContainsKey($startKey)) {
                throw 'Invalid or duplicate observer start for public catalog.'
            }
            $seenCatalogs[$startKey] = $true
            return
        }
        if ($record.kind -ceq 'public_plugin_catalog') {
            if (-not $seenCatalogs.ContainsKey($startKey)) { throw 'Public catalog precedes observer start.' }
        } elseif (-not $seenCatalogs.ContainsKey([string]$record.pid)) {
            throw 'Observer event precedes its required public catalog.'
        }
    }
    $hasDigest = [bool]$record.PSObject.Properties['publicPluginCatalogSHA256']
    $hasReference = [bool]$record.PSObject.Properties['publicPluginRef']
    if ($record.kind -ceq 'public_plugin_catalog') {
        Assert-PublicShape $record @('seq', 'pid', 'kind', 'publicPluginCatalogSHA256')
        Assert-PublicInteger $record.pid 1 4294967296
        Assert-PublicInteger $record.seq 1 4294967296
        if ($null -eq $catalog) { throw 'Unexpected public plugin catalog event.' }
        Assert-PublicHash $record.publicPluginCatalogSHA256
        if ($record.publicPluginCatalogSHA256 -cne $catalog.keysSha256 -or $seenCatalogs.ContainsKey([string]$record.pid)) {
            throw 'Duplicate or mismatched per-process public catalog digest.'
        }
        $seenCatalogs[[string]$record.pid] = $true
    } elseif ($hasReference) {
        Assert-PublicShape $record @('seq', 'pid', 'kind', 'publicPluginRef', 'configValue')
        if ($record.kind -cne 'argv_config' -or $null -eq $catalog -or -not $seenCatalogs.ContainsKey([string]$record.pid)) {
            throw 'Public plugin reference has no verified per-process catalog.'
        }
        Assert-PublicInteger $record.pid 1 4294967296
        Assert-PublicInteger $record.seq 1 4294967296
        Assert-PublicInteger $record.publicPluginRef 1 $catalog.candidates.Count
        if ($record.configValue -isnot [string] -or @('bool_true', 'bool_false', 'other') -cnotcontains $record.configValue) {
            throw 'Invalid public plugin value classification.'
        }
    } elseif ($hasDigest) { throw 'Public catalog digest occurred outside its own event.' }
}

# These helpers are AST-loaded by offline fixtures; importing them never launches Desktop.
function Assert-DesktopCIOptions(
    [string]$Mode, [bool]$StartupProbe, [bool]$LaunchCompatibility, [bool]$StaticLauncher,
    [string]$Actions, [string]$RunnerEnvironment, [string]$Repository, [string]$Ref
) {
    if ($Actions -ne 'true' -or $RunnerEnvironment -ne 'github-hosted' -or
        $Repository -ne 'breakingtime126/magpie' -or
        $Ref -ne 'refs/heads/experiment/app-server-bridge-v0.1.3035') {
        throw 'This probe is restricted to the authorized disposable GitHub hosted branch job.'
    }
    if (($StartupProbe -or $StaticLauncher -or $LaunchCompatibility) -and $Mode -ne 'observe') {
        throw 'Reviewed startup inspection is limited to the explicitly instrumented CI mode.'
    }
    if ($LaunchCompatibility -and -not $StartupProbe) {
        throw 'Reviewed launch compatibility requires the separate exact-version startup opt-in.'
    }
}
function Assert-DesktopCompatibilityPackage([bool]$LaunchCompatibility, [string]$DesktopVersion, [string]$PackageHash) {
    if (-not $LaunchCompatibility) { return }
    if ($DesktopVersion -cne '26.1103.3517.0' -or
        $PackageHash -cne 'ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643') {
        throw 'Reviewed launch compatibility package mismatch; the exact signed Desktop 26.1103.3517.0 package is required.'
    }
}
function New-DesktopBridgeSettings(
    $Backends, [bool]$StartupProbe, [bool]$LaunchCompatibility,
    [string]$VersionText, [string]$BackendHash, [string]$ControlHome = ''
) {
    if ($LaunchCompatibility -and -not $StartupProbe) {
        throw 'Reviewed launch compatibility requires the separate exact-version startup opt-in.'
    }
    # Copy only the caller's fresh test configuration; no filesystem or environment access.
    $copies = @()
    foreach ($backendEntry in $Backends) {
        $copy = [ordered]@{}
        foreach ($key in $backendEntry.Keys) { $copy[$key] = $backendEntry[$key] }
        $copies += $copy
    }
    $settings = [ordered]@{ backends = $copies }
    if ($StartupProbe) {
        $expectedVersion = 'codex-cli 0.167.0-alpha.3.2'
        $expectedBackendHash = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
        if ($VersionText -cne $expectedVersion -or $BackendHash -cne $expectedBackendHash -or $copies.Count -ne 2) {
            throw 'Reviewed startup inspection requires two exact verified official backends.'
        }
        foreach ($backendEntry in $copies) { $backendEntry.startup_probe_sha256 = $expectedBackendHash }
        $settings.startup_probe = 'compare-unrestricted-v0.167.0-alpha.3.2'
    }
    if ($LaunchCompatibility) {
        if ([string]::IsNullOrWhiteSpace($ControlHome)) { throw 'Reviewed control-plane mode requires a fresh bridge-owned home.' }
        foreach ($backendEntry in $copies) {
            if ($ControlHome -ieq $backendEntry.home -or $ControlHome -ieq $backendEntry.codex_home) {
                throw 'Bridge-owned home must be separate from every native home.'
            }
        }
        $settings.control_home = $ControlHome
        # Separate CI-only consent: enabling startup inspection alone never selects these modes.
        $settings.desktop_plugin_override = 'codex-app-tools-v0.167.0-alpha.3.2'
        $settings.attestation = 'unavailable-v0.167.0-alpha.3.2'
        $settings.gateway_oauth = 'primary-read-only-v0.167.0-alpha.3.2'
        $settings.gateway_oauth_provider_name = 'CI alpha launch-only'
        $settings.config_requirements_read = 'compare-null-v0.167.0-alpha.3.2'
        $settings.auth_status = 'primary-no-auth-v0.167.0-alpha.3.2'
        $settings.experimental_feature_list = 'compare-v0.167.0-alpha.3.2'
        $settings.control_plane = 'routing-v0.167.0-alpha.3.2'
    }
    return $settings
}

# Observer-only references. Callers supply only the homes/workspace they just created
# and the package root established by signed install + registration verification.
# This serializes lexical labels, not filesystem authority, and is never published.
function New-DesktopReadTargetEnvironment($Backends, [string]$Workspace, [string]$VerifiedPackage, [string]$ControlHome = '') {
    if (@($Backends).Count -ne 2) { throw 'Read-target references require both disposable backends.' }
    $roots = [ordered]@{
        primaryHome = $Backends[0].home
        primaryCodexHome = $Backends[0].codex_home
        secondaryHome = $Backends[1].home
        secondaryCodexHome = $Backends[1].codex_home
        workspace = $Workspace
        verifiedPackage = $VerifiedPackage
    }
    if (-not [string]::IsNullOrEmpty($ControlHome)) {
        if ($ControlHome -ieq $Workspace -or @($roots.Values) -icontains $ControlHome) {
            throw 'Bridge-owned read-target root must remain separate.'
        }
        $roots.controlHome = $ControlHome
    }
    foreach ($value in $roots.Values) {
        if ($value -isnot [string] -or $value.Length -lt 1 -or
            [Text.Encoding]::UTF8.GetByteCount($value) -gt 4096 -or $value -cmatch '[^\x20-\x7e]') {
            throw 'Invalid disposable read-target root.'
        }
        $path = $value.Replace('/', '\')
        if ($path -cmatch '^[A-Za-z]:\\') {
            $tail = $path.Substring(3)
        } elseif ($path.StartsWith('\\') -and -not $path.StartsWith('\\?\') -and -not $path.StartsWith('\\.\')) {
            $tail = $path.Substring(2)
            if ($tail.TrimEnd('\').Split('\').Count -lt 2) { throw 'Invalid disposable read-target root.' }
        } else { throw 'Invalid disposable read-target root.' }
        if ($tail.EndsWith('\')) { $tail = $tail.Substring(0, $tail.Length - 1) }
        if ($tail.Length -eq 0 -and $path.Length -ne 3) { throw 'Invalid disposable read-target root.' }
        if ($tail.Length -gt 0) {
            foreach ($part in $tail.Split('\')) {
                if ($part.Length -eq 0 -or $part -ceq '.' -or $part -ceq '..' -or
                    $part -cmatch '[<>:"|?*~]' -or $part.EndsWith('.') -or $part.EndsWith(' ') -or
                    $part -match '^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)') {
                    throw 'Invalid disposable read-target root.'
                }
            }
        }
    }
    $json = $roots | ConvertTo-Json -Depth 3 -Compress
    if ([Text.Encoding]::UTF8.GetByteCount($json) -gt 16384) { throw 'Disposable read-target references exceed their bound.' }
    return $json
}

# Read only already-sanitized request/response metadata, never authentication payloads.
# A response here is meaningful solely with the exact no-auth bridge mode enabled:
# that production validator accepts only a genuine native null/null/false result.
function Get-DesktopNoAuthStatusEvidence([object[]]$Records) {
    $pending = @{}
    $seen = @{}
    $requests = 0
    $responses = 0
    $tokenRequests = 0
    $tokenResponses = 0
    foreach ($event in $Records) {
        if ($event.PSObject.Properties['class'] -and
            $event.class -in @('duplicate_id', 'correlation_limit')) {
            throw 'No-auth status evidence has ambiguous correlation.'
        }
        if (-not $event.PSObject.Properties['method'] -or $event.method -cne 'getAuthStatus') { continue }
        if (-not $event.PSObject.Properties['kind'] -or $event.kind -cne 'rpc' -or
            -not $event.PSObject.Properties['pid'] -or
            -not $event.PSObject.Properties['correlation'] -or
            -not $event.PSObject.Properties['direction'] -or
            -not $event.PSObject.Properties['class']) {
            throw 'No-auth status evidence lacks request correlation.'
        }
        if (($event.pid -isnot [int] -and $event.pid -isnot [long]) -or $event.pid -lt 1 -or
            ($event.correlation -isnot [int] -and $event.correlation -isnot [long]) -or $event.correlation -lt 1) {
            throw 'No-auth status evidence has invalid correlation.'
        }
        $key = [string]$event.pid + ':' + [string]$event.correlation
        if ($event.direction -ceq 'to_bridge' -and $event.class -ceq 'request') {
            if ($seen.ContainsKey($key) -or
                -not $event.PSObject.Properties['authParamsValid'] -or $event.authParamsValid -isnot [bool] -or -not $event.authParamsValid -or
                -not $event.PSObject.Properties['includeTokenRequested'] -or $event.includeTokenRequested -isnot [bool] -or
                -not $event.PSObject.Properties['refreshTokenRequested'] -or $event.refreshTokenRequested -isnot [bool] -or $event.refreshTokenRequested) {
                throw 'No-auth status evidence has an unsupported request.'
            }
            $seen[$key] = $true
            $pending[$key] = [bool]$event.includeTokenRequested
            $requests++
            if ($event.includeTokenRequested) { $tokenRequests++ }
        } elseif ($event.direction -ceq 'from_bridge' -and $event.class -ceq 'response') {
            if (-not $pending.ContainsKey($key)) { throw 'No-auth status evidence has an unmatched result.' }
            if ($pending[$key]) { $tokenResponses++ }
            [void]$pending.Remove($key)
            $responses++
        } else {
            throw 'No-auth status did not return a validated native success.'
        }
    }
    if ($pending.Count -ne 0 -or $requests -eq 0 -or $requests -ne $responses -or
        $tokenRequests -eq 0 -or $tokenRequests -ne $tokenResponses) {
        throw 'No-auth status diagnostic lacks complete token-request evidence.'
    }
    return [ordered]@{
        requests = $requests; validatedResponses = $responses
        includeTokenRequests = $tokenRequests; includeTokenValidatedResponses = $tokenResponses
    }
}

# Only full, already-sanitized snapshots belong here, never native params or results.
# Correlated success is meaningful only with the pinned feature-list compare mode:
# its production validator requires fresh native whole-page equality from both children.
function Get-DesktopFeatureListEvidence([object[]]$Records) {
    $pending = @{}
    $seen = @{}
    $requests = 0
    $responses = 0
    $limitRequests = 0
    $limitResponses = 0
    $cursorRequests = 0
    $cursorResponses = 0
    foreach ($event in $Records) {
        if ($event.PSObject.Properties['class'] -and
            $event.class -in @('duplicate_id', 'correlation_limit')) {
            throw 'Feature-list evidence has ambiguous correlation.'
        }
        if (-not $event.PSObject.Properties['method']) { continue }
        if ($event.method -isnot [string]) { throw 'Feature-list evidence has invalid method metadata.' }
        if ($event.method -cne 'experimentalFeature/list') { continue }
        foreach ($field in @('seq', 'pid', 'correlation')) {
            if (-not $event.PSObject.Properties[$field]) { throw 'Feature-list evidence lacks request correlation.' }
            Assert-PublicInteger $event.$field 1 4294967296
        }
        foreach ($field in @('kind', 'direction', 'class')) {
            if (-not $event.PSObject.Properties[$field] -or $event.$field -isnot [string]) {
                throw 'Feature-list evidence lacks request correlation.'
            }
        }
        if ($event.kind -cne 'rpc') { throw 'Feature-list did not return a validated native success.' }
        $key = [string]$event.pid + ':' + [string]$event.correlation
        if ($event.direction -ceq 'to_bridge' -and $event.class -ceq 'request') {
            # Reuse the existing diagnostic shape checks; do not widen the sanitizer.
            Assert-PublicReadRequestEvent $event
            foreach ($field in @('featureListParamsValid', 'featureListCursorProvided',
                'featureListLimitProvided', 'featureListThreadIdProvided')) {
                if (-not $event.PSObject.Properties[$field] -or $event.$field -isnot [bool]) {
                    throw 'Feature-list evidence lacks valid request flags.'
                }
            }
            if ($seen.ContainsKey($key) -or -not $event.featureListParamsValid -or
                $event.featureListThreadIdProvided -or
                ($event.PSObject.Properties['tracePresent'] -and -not $event.traceValid)) {
                throw 'Feature-list evidence has an unsupported request.'
            }
            $seen[$key] = $true
            $pending[$key] = @{
                limit = [bool]$event.featureListLimitProvided
                cursor = [bool]$event.featureListCursorProvided
            }
            $requests++
            if ($event.featureListLimitProvided) { $limitRequests++ }
            if ($event.featureListCursorProvided) { $cursorRequests++ }
        } elseif ($event.direction -ceq 'from_bridge' -and $event.class -ceq 'response') {
            Assert-PublicShape $event @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class')
            if (-not $pending.ContainsKey($key)) { throw 'Feature-list evidence has an unmatched result.' }
            if ($pending[$key].limit) { $limitResponses++ }
            if ($pending[$key].cursor) { $cursorResponses++ }
            [void]$pending.Remove($key)
            $responses++
        } else {
            throw 'Feature-list did not return a validated native success.'
        }
    }
    if ($pending.Count -ne 0 -or $requests -eq 0 -or $requests -ne $responses -or
        $limitRequests -eq 0 -or $limitRequests -ne $limitResponses -or $cursorRequests -ne $cursorResponses) {
        throw 'Feature-list diagnostic lacks complete limit-present request evidence.'
    }
    return [ordered]@{
        requests = $requests; validatedResponses = $responses
        limitRequests = $limitRequests; limitValidatedResponses = $limitResponses
        cursorRequests = $cursorRequests; cursorValidatedResponses = $cursorResponses
    }
}


# Count errors in the full sanitized snapshot before strict profile checks run.
# These observed counts are not a complete lifecycle or an API acceptance gate.
function Get-DesktopRPCErrorEvidence([object[]]$Records, [string[]]$PublicMethods) {
    $errors = 0
    $unsupported = 0
    $other = 0
    $threadListErrors = [ordered]@{}
    foreach ($class in @((Get-PublicThreadListErrorClasses)) + @('unknown', 'other')) {
        $threadListErrors[$class] = 0
    }
    $classes = @('startup_probe_unavailable', 'unsupported_authentication_capability',
        'initialization_required', 'config_requirements_disabled', 'config_requirements_failed',
        'gateway_oauth_disabled', 'unsupported_method', 'invalid_capabilities',
        'invalid_initialization', 'already_initialized', 'unknown') + @(Get-PublicThreadListErrorClasses)
    foreach ($event in $Records) {
        if (-not $event.PSObject.Properties['kind'] -or $event.kind -cne 'rpc_error') { continue }
        Assert-PublicThreadListEvent $event
        $names = @('seq', 'pid', 'kind', 'direction', 'class')
        $hasMethod = [bool]$event.PSObject.Properties['method']
        $hasCorrelation = [bool]$event.PSObject.Properties['correlation']
        if ($hasMethod -ne $hasCorrelation) { throw 'RPC-error evidence has incomplete correlation metadata.' }
        if ($hasMethod) { $names += @('method', 'correlation') }
        if ($event.PSObject.Properties['code']) { $names += 'code' }
        Assert-PublicShape $event $names
        Assert-PublicInteger $event.seq 1 4294967296
        Assert-PublicInteger $event.pid 1 4294967296
        if ($event.kind -isnot [string] -or $event.direction -isnot [string] -or
            @('to_bridge', 'from_bridge') -cnotcontains $event.direction -or
            $event.class -isnot [string] -or $classes -cnotcontains $event.class) {
            throw 'RPC-error evidence has invalid fixed metadata.'
        }
        if ($hasMethod) {
            if ($event.method -isnot [string] -or $PublicMethods -cnotcontains $event.method) {
                throw 'RPC-error evidence has an unreviewed method label.'
            }
            Assert-PublicInteger $event.correlation 1 4294967296
        }
        if ($event.PSObject.Properties['code']) { Assert-PublicInteger $event.code -4294967296 4294967296 }
        if ($hasMethod -and $event.method -ceq 'thread/list' -and $event.direction -ceq 'from_bridge') {
            $bucket = 'other'
            if ($threadListErrors.Contains($event.class)) { $bucket = $event.class }
            $threadListErrors[$bucket]++
        }
        $errors++
        if ($event.class -ceq 'unsupported_method') { $unsupported++ } else { $other++ }
    }
    return [ordered]@{ rpcErrors = $errors; unsupportedMethodErrors = $unsupported; otherRpcErrors = $other; threadListErrors = $threadListErrors }
}

# Only the full sanitized snapshot is evidence, never native config or response bodies.
# Success relies on the production bridge-owned view and its fresh paired native
# requirements:null checks. The observer does not prove the view's consumer support.
function Get-DesktopConfigReadEvidence([object[]]$Records, [switch]$RequireLayers) {
    $pending = @{}
    $seen = @{}
    $requests = 0
    $responses = 0
    $layerRequests = 0
    $layerResponses = 0
    $withoutLayerRequests = 0
    $withoutLayerResponses = 0
    foreach ($event in $Records) {
        if ($event.PSObject.Properties['class'] -and
            $event.class -in @('duplicate_id', 'correlation_limit')) {
            throw 'Configuration-read evidence has ambiguous correlation.'
        }
        if (-not $event.PSObject.Properties['method']) { continue }
        if ($event.method -isnot [string]) { throw 'Configuration-read evidence has invalid method metadata.' }
        if ($event.method -cne 'config/read') { continue }
        foreach ($field in @('seq', 'pid', 'correlation')) {
            if (-not $event.PSObject.Properties[$field]) { throw 'Configuration-read evidence lacks request correlation.' }
            Assert-PublicInteger $event.$field 1 4294967296
        }
        foreach ($field in @('kind', 'direction', 'class')) {
            if (-not $event.PSObject.Properties[$field] -or $event.$field -isnot [string]) {
                throw 'Configuration-read evidence lacks request correlation.'
            }
        }
        if ($event.kind -cne 'rpc') { throw 'Configuration-read did not return a validated bridge-owned success.' }
        $key = [string]$event.pid + ':' + [string]$event.correlation
        if ($event.direction -ceq 'to_bridge' -and $event.class -ceq 'request') {
            Assert-PublicReadRequestEvent $event
            foreach ($field in @('configParamsValid', 'configIncludeLayers', 'configCwdProvided')) {
                if (-not $event.PSObject.Properties[$field] -or $event.$field -isnot [bool]) {
                    throw 'Configuration-read evidence lacks valid request flags.'
                }
            }
            if ($seen.ContainsKey($key) -or -not $event.configParamsValid -or
                $event.configCwdProvided -or
                ($event.PSObject.Properties['tracePresent'] -and -not $event.traceValid)) {
                throw 'Configuration-read evidence has an unsupported request.'
            }
            $seen[$key] = $true
            $pending[$key] = $event.configIncludeLayers
            $requests++
            if ($event.configIncludeLayers) { $layerRequests++ } else { $withoutLayerRequests++ }
        } elseif ($event.direction -ceq 'from_bridge' -and $event.class -ceq 'response') {
            Assert-PublicShape $event @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'class')
            if (-not $pending.ContainsKey($key)) { throw 'Configuration-read evidence has an unmatched result.' }
            if ($pending[$key]) { $layerResponses++ } else { $withoutLayerResponses++ }
            [void]$pending.Remove($key)
            $responses++
        } else {
            throw 'Configuration-read did not return a validated bridge-owned success.'
        }
    }
    if ($pending.Count -ne 0 -or $requests -eq 0 -or $requests -ne $responses -or
        $layerRequests -ne $layerResponses -or $withoutLayerRequests -ne $withoutLayerResponses) {
        throw 'Configuration-read diagnostic lacks complete global request evidence.'
    }
    if ($RequireLayers -and $layerRequests -eq 0) {
        throw 'Configuration-read diagnostic lacks observed layer-request evidence.'
    }
    return [ordered]@{
        requests = $requests; validatedResponses = $responses
        layerRequests = $layerRequests; layerValidatedResponses = $layerResponses
        withoutLayerRequests = $withoutLayerRequests; withoutLayerValidatedResponses = $withoutLayerResponses
    }
}

# Pin the exact observed activation process through its OS handle before identity
# checks. This confirms only that process exited, never a whole package tree.
# The injected package reader lets offline tests exercise every decision without
# launching or terminating anything.
function Complete-DesktopActivationProcess(
    $Process, [int]$ExpectedPid, [string]$ObservedCreatedUtc,
    [string]$ExpectedPackage, [scriptblock]$ReadPackageIdentity
) {
    try {
        if ($null -eq $Process -or $ExpectedPid -le 0 -or
            [string]::IsNullOrWhiteSpace($ObservedCreatedUtc) -or
            [string]::IsNullOrWhiteSpace($ExpectedPackage) -or $null -eq $ReadPackageIdentity) {
            throw 'Missing activation identity.'
        }
        # Acquiring and retaining this handle prevents later operations from
        # reopening an unrelated process that reused the numeric PID.
        $pinnedHandle = $Process.Handle
        if ($pinnedHandle -eq [IntPtr]::Zero -or $Process.Id -ne $ExpectedPid -or
            $Process.StartTime.ToUniversalTime().ToString('o') -cne $ObservedCreatedUtc) {
            throw 'Activation identity mismatch.'
        }
        $packageIdentity = & $ReadPackageIdentity $ExpectedPid
        if ($packageIdentity -isnot [string] -or $packageIdentity -cne $ExpectedPackage) {
            throw 'Activation package identity mismatch.'
        }
        if ($Process.HasExited) { return $true }
        try { [void]$Process.CloseMainWindow() }
        catch { if ($Process.HasExited) { return $true }; throw }
        if ($Process.WaitForExit(5000)) {
            if (-not $Process.HasExited) { throw 'Activation exit is unknown.' }
            return $true
        }
        # The same handle-pinned Process object is the only termination target.
        # Never resolve the PID again or terminate by executable/package name.
        try { [void]$Process.Kill() }
        catch { if ($Process.HasExited) { return $true }; throw }
        if (-not $Process.WaitForExit(5000) -or -not $Process.HasExited) {
            throw 'Activation termination is unconfirmed.'
        }
        return $true
    } catch {
        throw 'The verified Desktop activation process exit could not be confirmed.'
    }
}

# Eligibility is capture safety, not successful Desktop compatibility. A valid
# bounded snapshot is not a claim of complete observer/process lifecycle.
function Get-DesktopNativeRunDecision(
    [bool]$Reviewed, [bool]$CaptureValidated, [bool]$ActivationExited, [bool]$CleanupError
) {
    if (-not $Reviewed) { return [ordered]@{ run = $false; reason = 'profile_not_enabled' } }
    if (-not $CaptureValidated) { return [ordered]@{ run = $false; reason = 'desktop_capture_not_validated' } }
    if (-not $ActivationExited -or $CleanupError) {
        return [ordered]@{ run = $false; reason = 'desktop_activation_exit_not_confirmed' }
    }
    return [ordered]@{ run = $true; reason = '' }
}

# Combine only already-classified outcomes. Original Desktop failures are copied
# unchanged, even when the separate native slice succeeds or fails differently.
function Get-DesktopCombinedOutcome($DesktopOutcome, [string]$NativeStatus, [string]$NotRunReason = '') {
    Assert-PublicShape $DesktopOutcome @('status', 'stage', 'reason', 'exitCode')
    Assert-PublicInteger $DesktopOutcome.exitCode 0 255
    foreach ($field in @('status', 'stage', 'reason')) {
        if ($DesktopOutcome.$field -isnot [string]) { throw 'Invalid Desktop outcome.' }
    }
    if (@('observed', 'blocked') -cnotcontains $DesktopOutcome.status -or
        (($DesktopOutcome.exitCode -eq 0) -ne ($DesktopOutcome.status -ceq 'observed')) -or
        @('passed', 'failed', 'not-run') -cnotcontains $NativeStatus) {
        throw 'Invalid combined diagnostic outcome.'
    }
    if ($NativeStatus -ceq 'not-run') {
        if (@('desktop_capture_not_validated', 'desktop_activation_exit_not_confirmed') -cnotcontains $NotRunReason) {
            throw 'Invalid native diagnostic eligibility reason.'
        }
    } elseif ($NotRunReason -cne '') { throw 'Unexpected native diagnostic eligibility reason.' }
    $combined = [ordered]@{
        status = $DesktopOutcome.status; stage = $DesktopOutcome.stage
        reason = $DesktopOutcome.reason; exitCode = $DesktopOutcome.exitCode
    }
    if ($DesktopOutcome.exitCode -ne 0) { return $combined }
    if ($NativeStatus -ceq 'passed') {
        $combined.stage = 'desktop-capture-and-synthetic-native-control-completed'
    } elseif ($NativeStatus -ceq 'failed') {
        $combined.status = 'blocked'
        $combined.stage = 'synthetic-native-control-plane'
        $combined.reason = 'The isolated native control-plane check did not produce complete validated success.'
        $combined.exitCode = 1
    } else {
        $combined.status = 'blocked'
        $combined.stage = 'desktop-capture'
        $combined.reason = 'The Desktop capture was not fully validated; the separate native check was not started.'
        if ($NotRunReason -ceq 'desktop_activation_exit_not_confirmed') {
            $combined.stage = 'desktop-cleanup'
            $combined.reason = 'The observed Desktop activation process exit was not confirmed; the separate native check was not started.'
        }
        $combined.exitCode = 1
    }
    return $combined
}

# A passing native slice is separate synthetic-loopback evidence, never Desktop,
# real-service or account proof. Retain only the exact reviewed success schema.
function ConvertTo-DesktopNativeControlEvidence($value) {
    $counts = [ordered]@{
        config_reads = 9; layer_reads = 6; stable_layer_rereads = 3; layer_version_changes = 1
        catalog_models = 2; threads = 2; turns = 4
        assistant_messages = 4; thread_reads = 2; ownership_rejections = 2
        alpha_requests = 2; beta_requests = 2; fixture_errors = 0; unexpected_server_requests = 0
    }
    $flags = @('binary_verified', 'version_verified', 'adapter_clean_exit', 'cleanup_complete')
    Assert-PublicShape $value (@('schema', 'scope', 'status', 'stage', 'failure', 'native_sha256') + $flags + @($counts.Keys))
    Assert-PublicInteger $value.schema 1 1
    if ($value.scope -isnot [string] -or $value.scope -cne 'synthetic-loopback-only' -or
        $value.status -isnot [string] -or $value.status -cne 'passed' -or
        $value.stage -isnot [string] -or $value.stage -cne 'complete' -or
        $value.failure -isnot [string] -or $value.failure -cne '' -or
        $value.native_sha256 -isnot [string] -or
        $value.native_sha256 -cne '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39') {
        throw 'Native control-plane evidence did not pass its exact pinned fixture.'
    }
    foreach ($field in $flags) {
        if ($value.$field -isnot [bool] -or -not $value.$field) {
            throw 'Native control-plane evidence lacks verified isolation or cleanup.'
        }
    }
    foreach ($field in $counts.Keys) { Assert-PublicInteger $value.$field $counts[$field] $counts[$field] }
    $safe = [ordered]@{
        schema = 1; scope = 'synthetic-loopback-only'; status = 'passed'; stage = 'complete'
        failure = ''; native_sha256 = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
    }
    foreach ($field in $flags) { $safe[$field] = $true }
    foreach ($field in $counts.Keys) { $safe[$field] = $counts[$field] }
    return $safe
}

# This optional static pass is independent of the existing catalog's resource budget.
# The expected ASAR identity must already be verified against the signed original MSIX.
function ConvertTo-DesktopReadCallerEvidence($value, [string]$ExpectedArchiveHash) {
    Assert-PublicHash $ExpectedArchiveHash
    $failureCodes = @('input_limit', 'token_limit', 'depth_limit', 'deadline', 'call_limit',
        'node_limit', 'local_node_limit', 'field_limit', 'syntax_error', 'analysis_failed',
        'source_limit', 'source_read_failed', 'archive_mismatch', 'archive_changed',
        'package_path', 'report_limit', 'invalid_arguments', 'disposable_windows_ci_required')
    if (-not $value.PSObject.Properties['status'] -or $value.status -isnot [string]) {
        throw 'Invalid read-caller status type.'
    }
    if ($value.PSObject.Properties['status'] -and $value.status -ceq 'unavailable') {
        Assert-PublicShape $value @('schema', 'status', 'reason')
        Assert-PublicInteger $value.schema 1 1
        if ($value.reason -isnot [string] -or $failureCodes -cnotcontains $value.reason) {
            throw 'Unreviewed read-caller failure.'
        }
        return [ordered]@{ schema = 1; status = 'unavailable'; reason = $value.reason }
    }
    Assert-PublicShape $value @('schema', 'status', 'archiveSha256', 'filesSelected',
        'filesComplete', 'sourceBytes', 'sources')
    Assert-PublicInteger $value.schema 1 1
    Assert-PublicHash $value.archiveSha256
    if ($value.archiveSha256 -cne $ExpectedArchiveHash -or $value.status -isnot [string] -or
        @('complete', 'partial') -cnotcontains $value.status) {
        throw 'Read-caller source identity or status mismatch.'
    }
    Assert-PublicInteger $value.filesSelected 0 2048
    Assert-PublicInteger $value.filesComplete 0 $value.filesSelected
    Assert-PublicInteger $value.sourceBytes 0 33554432
    if ($value.sources -isnot [System.Array] -or $value.sources.Count -ne $value.filesSelected) {
        throw 'Invalid read-caller source coverage.'
    }
    $methods = @('config/read', 'experimentalFeature/list', 'project/list', 'fs/readFile')
    $fields = @('config', 'origins', 'layers', 'model', 'model_provider', 'model_providers', 'features',
        'data', 'nextCursor', 'id', 'name', 'roots', 'metadata', 'position',
        'createdAt', 'updatedAt', 'recencyAt', 'dataBase64')
    $markers = @('config_toml', 'agents_md', 'skill_md', 'auth_json', 'dot_codex')
    $pathSyntax = @('unresolved', 'template', 'call', 'identifier', 'member', 'binary',
        'conditional', 'literal_absolute', 'literal_relative', 'other')
    $previousSource = ''
    $sources = @()
    $complete = 0
    $calls = 0
    foreach ($source in $value.sources) {
        Assert-PublicShape $source @('path', 'sha256', 'analysis')
        Assert-PublicHash $source.sha256
        if ($source.path -isnot [string] -or
            $source.path -cnotmatch '^\.vite/build/[A-Za-z0-9][A-Za-z0-9_.-]{0,190}\.js\z' -or
            [StringComparer]::Ordinal.Compare($previousSource, $source.path) -ge 0) {
            throw 'Invalid read-caller source reference.'
        }
        $previousSource = $source.path
        $analysis = $source.analysis
        if (-not $analysis.PSObject.Properties['status'] -or $analysis.status -isnot [string]) {
            throw 'Invalid read-caller source status type.'
        }
        if ($analysis.PSObject.Properties['status'] -and $analysis.status -ceq 'unavailable') {
            Assert-PublicShape $analysis @('schema', 'status', 'reason')
            Assert-PublicInteger $analysis.schema 1 1
            if ($analysis.reason -isnot [string] -or
                @('syntax_error', 'local_node_limit', 'field_limit') -cnotcontains $analysis.reason) {
                throw 'Invalid partial read-caller reason.'
            }
            $safeAnalysis = [ordered]@{ schema = 1; status = 'unavailable'; reason = $analysis.reason }
        } else {
            Assert-PublicShape $analysis @('schema', 'status', 'sourceParsedCompletely',
                'runtimeBindingProven', 'responseValueProven', 'consumerCoverage', 'calls')
            Assert-PublicInteger $analysis.schema 1 1
            if ($analysis.status -cne 'source_syntax_only' -or
                $analysis.sourceParsedCompletely -isnot [bool] -or -not $analysis.sourceParsedCompletely -or
                $analysis.runtimeBindingProven -isnot [bool] -or $analysis.runtimeBindingProven -or
                $analysis.responseValueProven -isnot [bool] -or $analysis.responseValueProven -or
                $analysis.consumerCoverage -isnot [string] -or
                $analysis.consumerCoverage -cne 'direct_await_and_sibling_const_only' -or
                $analysis.calls -isnot [System.Array] -or $analysis.calls.Count -gt 128) {
                throw 'Invalid read-caller analysis scope.'
            }
            $complete++
            $safeCalls = @()
            $previousOffset = -1
            foreach ($call in $analysis.calls) {
                $names = @('method', 'offsetUtf16', 'relation', 'responseFields')
                if ($call.PSObject.Properties['method'] -and $call.method -ceq 'fs/readFile') {
                    $names += @('pathSyntax', 'pathMarkers', 'pathAuthorityProven')
                }
                Assert-PublicShape $call $names
                Assert-PublicInteger $call.offsetUtf16 0 16777215
                $calls++
                if ($call.method -isnot [string] -or $methods -cnotcontains $call.method -or
                    $call.relation -isnot [string] -or $call.relation -cne 'literal_first_argument_only' -or
                    $call.offsetUtf16 -le $previousOffset -or
                    $call.responseFields -isnot [System.Array] -or $call.responseFields.Count -gt 32 -or
                    $calls -gt 128) { throw 'Invalid read-caller call metadata.' }
                $previousOffset = $call.offsetUtf16
                $safeFields = @()
                $seenFields = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
                foreach ($field in $call.responseFields) {
                    Assert-PublicShape $field @('relation', 'path')
                    if ($field.relation -isnot [string] -or
                        @('await_member', 'const_binding') -cnotcontains $field.relation -or
                        $field.path -isnot [System.Array] -or $field.path.Count -lt 1 -or $field.path.Count -gt 3) {
                        throw 'Invalid read-caller field relation.'
                    }
                    foreach ($part in $field.path) {
                        if ($part -isnot [string] -or $fields -cnotcontains $part) {
                            throw 'Unreviewed read-caller response field.'
                        }
                    }
                    if (-not $seenFields.Add($field.relation + ':' + ($field.path -join '.'))) {
                        throw 'Duplicate read-caller field relation.'
                    }
                    $safeFields += [ordered]@{ relation = $field.relation; path = @($field.path) }
                }
                $safeCall = [ordered]@{
                    method = $call.method; offsetUtf16 = $call.offsetUtf16
                    relation = 'literal_first_argument_only'; responseFields = $safeFields
                }
                if ($call.method -ceq 'fs/readFile') {
                    if ($call.pathSyntax -isnot [string] -or $pathSyntax -cnotcontains $call.pathSyntax -or
                        $call.pathAuthorityProven -isnot [bool] -or $call.pathAuthorityProven -or
                        $call.pathMarkers -isnot [System.Array] -or $call.pathMarkers.Count -gt 5) {
                        throw 'Invalid read-caller path classification.'
                    }
                    $previousMarker = -1
                    foreach ($marker in $call.pathMarkers) {
                        if ($marker -isnot [string]) { throw 'Invalid read-caller path marker type.' }
                        $markerIndex = [Array]::IndexOf($markers, $marker)
                        if ($markerIndex -le $previousMarker) { throw 'Unreviewed or unordered read-caller path marker.' }
                        $previousMarker = $markerIndex
                    }
                    if ($call.pathSyntax -ceq 'unresolved' -and $call.pathMarkers.Count -ne 0) {
                        throw 'Unresolved read-caller path has unsupported markers.'
                    }
                    $safeCall.pathSyntax = $call.pathSyntax
                    $safeCall.pathMarkers = @($call.pathMarkers)
                    $safeCall.pathAuthorityProven = $false
                }
                $safeCalls += $safeCall
            }
            $safeAnalysis = [ordered]@{
                schema = 1; status = 'source_syntax_only'; sourceParsedCompletely = $true
                runtimeBindingProven = $false; responseValueProven = $false
                consumerCoverage = 'direct_await_and_sibling_const_only'
                calls = $safeCalls
            }
        }
        # Do not publish raw source filenames or filesystem paths.
        $sources += [ordered]@{ source = $sources.Count; sha256 = $source.sha256; analysis = $safeAnalysis }
    }
    if ($complete -ne $value.filesComplete -or
        ($value.status -ceq 'complete' -and $complete -ne $value.filesSelected) -or
        ($value.status -ceq 'partial' -and $complete -eq $value.filesSelected)) {
        throw 'Inconsistent read-caller completeness.'
    }
    return [ordered]@{
        schema = 1; status = $value.status; archiveSha256 = $ExpectedArchiveHash
        filesSelected = $value.filesSelected; filesComplete = $complete; sourceBytes = $value.sourceBytes
        callCount = $calls; sources = $sources
    }
}
function ConvertFrom-DesktopReadCallerOutput(
    [string]$Text, [string]$ErrorText, [int]$ExitCode, [string]$ExpectedArchiveHash
) {
    $prefix = 'DESKTOP_READ_CALLERS_METADATA='
    if ($Text.Length -gt 65600 -or $ErrorText.Length -ne 0 -or @(0, 2) -notcontains $ExitCode) {
        throw 'Unexpected read-caller process output.'
    }
    $line = $Text
    if ($line.EndsWith("`n")) {
        $line = $line.Substring(0, $line.Length - 1)
        if ($line.EndsWith("`r")) { $line = $line.Substring(0, $line.Length - 1) }
    }
    if (-not $line.StartsWith($prefix, [StringComparison]::Ordinal) -or
        $line.Contains("`r") -or $line.Contains("`n")) { throw 'Invalid read-caller output framing.' }
    $body = $line.Substring($prefix.Length)
    if ([Text.Encoding]::UTF8.GetByteCount($body) -gt 65536) { throw 'Read-caller output size exceeded.' }
    try {
        $value = $body | ConvertFrom-Json
        Assert-PublicCanonicalJSON $body $value
        $safe = ConvertTo-DesktopReadCallerEvidence $value $ExpectedArchiveHash
    } catch { throw 'Invalid read-caller metadata.' }
    if (($safe.status -ceq 'unavailable' -and $ExitCode -ne 2) -or
        ($safe.status -cne 'unavailable' -and $ExitCode -ne 0)) {
        throw 'Read-caller process status mismatch.'
    }
    return $safe
}

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Assert-DesktopCIOptions -Mode $Mode -StartupProbe $EnableReviewedStartupProbe `
    -LaunchCompatibility $EnableReviewedLaunchCompatibility -StaticLauncher $InspectStaticLauncher `
    -Actions $env:GITHUB_ACTIONS -RunnerEnvironment $env:RUNNER_ENVIRONMENT `
    -Repository $env:GITHUB_REPOSITORY -Ref $env:GITHUB_REF
$out = Join-Path $env:RUNNER_TEMP 'desktop-cold-start-report'
$work = Join-Path $env:RUNNER_TEMP ('desktop-cold-start-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $out, $work | Out-Null
$report = [ordered]@{
    schema = 1; sourceCommit = $env:GITHUB_SHA; mode = $Mode
    status = 'blocked'; stage = 'preflight'; startedUtc = [DateTime]::UtcNow.ToString('o')
    limits = @('No account login or user credentials.', 'Latest official package, not pinned historical Desktop.',
        'Bridge phase tests launch only; explicit CI compatibility opt-ins are recorded separately, never evidence of account login.', 'Window visibility is not model or account acceptance.')
}
$activationPid = 0
$activationExitConfirmed = $false
$captureValidated = $false
$ownedActivationProcess = $null
$ownedActivationCreatedUtc = ''
$observed = @{}
$eventSource = 'MagpieDesktopProcessStart'
$subscription = $null
$exitCode = 1
$publicPluginCatalog = $null
$publicPluginEnvironment = $null
function Block([string]$reason) {
    $report.reason = $reason
    throw $reason
}
function Save-Report {
    $report.finishedUtc = [DateTime]::UtcNow.ToString('o')
    $json = $report | ConvertTo-Json -Depth 12
    $json | Set-Content -LiteralPath (Join-Path $out 'report.json') -Encoding UTF8
    Write-Output ('DESKTOP_COLD_START_REPORT=' + ($report | ConvertTo-Json -Depth 12 -Compress))
    @"
## Official Desktop cold-start probe
Mode: $Mode
Commit: $env:GITHUB_SHA
Status: $($report.status)
Stage: $($report.stage)

No login, real account, bridge deployment, or user computer access.
See the report artifact for public package, session, and process evidence.
"@ | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY -Encoding UTF8
}
try {
    if ($EnableReviewedLaunchCompatibility) {
        $report.captureValidated = $false
        $report.captureValidationLimit = 'This flag covers the full bounded, sanitized metadata snapshots and preceding package/window/child identity checks. It does not mean Desktop compatibility succeeded or the complete process lifecycle was observed.'
        $report.desktopActivationExited = $false
        $report.desktopActivationExitLimit = 'This confirms only the exact observed activation process exited. It does not establish that every Desktop package descendant exited.'
        $report.nativeControlPlane = [ordered]@{
            schema = 1; scope = 'synthetic-loopback-only'; status = 'not-run'; reason = 'desktop_capture_not_completed'
        }
        $expectedNativeControl = Join-Path $env:RUNNER_TEMP ('magpie-control-native-' + $env:GITHUB_SHA.Substring(0, 8) + '.exe')
        $expectedNativeCatalog = Join-Path $env:RUNNER_TEMP 'magpie-control-native-models.json'
        if ($NativeControlExecutable -cne $expectedNativeControl -or $NativeControlCatalog -cne $expectedNativeCatalog) {
            Block 'The exact commit-built native control-plane helper and pinned catalog are required.'
        }
        try {
            Assert-PublicRegularPath $NativeControlExecutable
            Assert-PublicRegularPath $NativeControlCatalog
        } catch { Block 'The native control-plane helper or catalog is not an intact regular file.' }
    } elseif (-not [string]::IsNullOrEmpty($NativeControlExecutable) -or -not [string]::IsNullOrEmpty($NativeControlCatalog)) {
        Block 'Native control-plane checks require the existing explicit reviewed compatibility profile.'
    }
    # Presence checks only. Never read, copy, print, or delete any authentication value.
    foreach ($name in @('OPENAI_API_KEY', 'OPENAI_ACCESS_TOKEN', 'CODEX_API_KEY', 'CODEX_AUTH_JSON',
        'CODEX_HOME', 'CODEX_CLI_PATH', 'MAGPIE_APP_SERVER_BRIDGE_CONFIG',
        'MAGPIE_CI_REAL_BRIDGE', 'MAGPIE_CI_TRACE_FILE', 'MAGPIE_CI_OBSERVER_ACTIVE',
        'MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES', 'MAGPIE_CI_READ_TARGET_ROOTS')) {
        if ([Environment]::GetEnvironmentVariable($name)) { Block ('Unexpected existing auth/override environment name: ' + $name) }
    }
    if (Test-Path -LiteralPath (Join-Path $env:USERPROFILE '.codex\auth.json')) { Block 'Unexpected existing Codex authentication file.' }
    $existing = @(Get-AppxPackage -Name 'OpenAI.Codex')
    if ($existing.Count -ne 0) { Block 'Unexpected existing OpenAI.Codex registration; baseline must be fresh.' }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    Add-Type -AssemblyName System.Drawing
    Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
namespace DesktopColdProbe {
  [ComImport, Guid("2e941141-7f97-4756-ba1d-9decde894a3d"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
  interface IApplicationActivationManager {
    [PreserveSig] int ActivateApplication([MarshalAs(UnmanagedType.LPWStr)] string app,
      [MarshalAs(UnmanagedType.LPWStr)] string args, uint options, out uint processId);
    [PreserveSig] int ActivateForFile(IntPtr app, IntPtr items, IntPtr verb, out uint processId);
    [PreserveSig] int ActivateForProtocol(IntPtr app, IntPtr items, out uint processId);
  }
  [ComImport, Guid("45BA127D-10A8-46EA-8AB7-56EA9078943C")]
  class ApplicationActivationManager {}
  public sealed class WindowInfo {
    public long Handle; public uint ProcessId; public int Left; public int Top; public int Width; public int Height;
  }
  public static class Native {
    [DllImport("kernel32.dll")] public static extern uint WTSGetActiveConsoleSessionId();
    [DllImport("user32.dll", SetLastError=true)] public static extern IntPtr OpenInputDesktop(uint flags, bool inherit, uint access);
    [DllImport("user32.dll")] public static extern bool CloseDesktop(IntPtr h);
    [DllImport("user32.dll")] public static extern int GetSystemMetrics(int index);
    [DllImport("user32.dll")] static extern bool EnumWindows(EnumCallback cb, IntPtr param);
    delegate bool EnumCallback(IntPtr h, IntPtr param);
    [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr h, out Rect rect);
    [StructLayout(LayoutKind.Sequential)] struct Rect { public int Left, Top, Right, Bottom; }
    [DllImport("kernel32.dll", SetLastError=true)] static extern IntPtr OpenProcess(uint access, bool inherit, uint pid);
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr h);
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode)] static extern int GetPackageFullName(IntPtr process, ref uint length, StringBuilder name);
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern IntPtr CreateFileW(string path, uint access, uint share, IntPtr security, uint disposition, uint flags, IntPtr template);
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern bool DeviceIoControl(IntPtr file, uint code, IntPtr input, uint inputLength,
      byte[] output, uint outputLength, out uint returned, IntPtr overlapped);
    public static uint AliasTag(string path) {
      // Query only the documented reparse tag; never parse unsupported APPEXECLINK payload strings.
      IntPtr h = CreateFileW(path, 8, 7, IntPtr.Zero, 3, 0x02200000, IntPtr.Zero);
      if (h == new IntPtr(-1)) throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error());
      try {
        var bytes = new byte[16384]; uint returned;
        if (!DeviceIoControl(h, 0x000900A8, IntPtr.Zero, 0, bytes, (uint)bytes.Length, out returned, IntPtr.Zero))
          throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error());
        if (returned < 8) throw new InvalidOperationException("Truncated reparse metadata.");
        return BitConverter.ToUInt32(bytes, 0);
      } finally { CloseHandle(h); }
    }
    public static string PackageFor(uint pid) {
      IntPtr h = OpenProcess(0x1000, false, pid);
      if (h == IntPtr.Zero) return null;
      try {
        uint len = 0; int rc = GetPackageFullName(h, ref len, null);
        if (rc != 122 || len == 0 || len > 32768) return null;
        var name = new StringBuilder((int)len);
        return GetPackageFullName(h, ref len, name) == 0 ? name.ToString() : null;
      } finally { CloseHandle(h); }
    }
    public static int Activate(string aumid, out uint pid) {
      var obj = new ApplicationActivationManager();
      try { return ((IApplicationActivationManager)obj).ActivateApplication(aumid, "", 0, out pid); }
      finally { Marshal.ReleaseComObject(obj); }
    }
    public static WindowInfo[] Windows() {
      var found = new List<WindowInfo>();
      EnumWindows(delegate(IntPtr h, IntPtr p) {
        uint pid; Rect r; GetWindowThreadProcessId(h, out pid);
        if (IsWindowVisible(h) && GetWindowRect(h, out r) && r.Right-r.Left > 50 && r.Bottom-r.Top > 50)
          found.Add(new WindowInfo { Handle=h.ToInt64(), ProcessId=pid, Left=r.Left, Top=r.Top,
            Width=r.Right-r.Left, Height=r.Bottom-r.Top });
        return true;
      }, IntPtr.Zero);
      return found.ToArray();
    }
  }
}
'@
    $processSession = (Get-Process -Id $PID).SessionId
    $inputDesktop = [DesktopColdProbe.Native]::OpenInputDesktop(0, $false, 1)
    $inputError = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
    $hasInput = $inputDesktop -ne [IntPtr]::Zero
    if ($hasInput) { [void][DesktopColdProbe.Native]::CloseDesktop($inputDesktop) }
    $report.session = [ordered]@{
        os = (Get-CimInstance Win32_OperatingSystem).Caption
        processSessionId = $processSession
        activeConsoleSessionId = [DesktopColdProbe.Native]::WTSGetActiveConsoleSessionId()
        userInteractive = [Environment]::UserInteractive
        inputDesktopOpened = $hasInput
        inputDesktopError = $(if ($hasInput) { 0 } else { $inputError })
        screenWidth = [DesktopColdProbe.Native]::GetSystemMetrics(0)
        screenHeight = [DesktopColdProbe.Native]::GetSystemMetrics(1)
    }

    $report.stage = 'download-and-signature'
    $package = Join-Path $work 'ChatGPT-x64.msix'
    $license = Join-Path $work 'ChatGPT-License.xml'
    $packageUrl = 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-x64.msix'
    $licenseUrl = 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-License.xml'
    Invoke-WebRequest -UseBasicParsing -Uri $packageUrl -OutFile $package
    Invoke-WebRequest -UseBasicParsing -Uri $licenseUrl -OutFile $license
    $report.download = [ordered]@{
        packageUrl = $packageUrl; licenseUrl = $licenseUrl
        packageSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $package).Hash.ToLowerInvariant()
        licenseSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $license).Hash.ToLowerInvariant()
        packageBytes = (Get-Item -LiteralPath $package).Length
    }
    $sdkBin = Join-Path ([Environment]::GetEnvironmentVariable('ProgramFiles(x86)')) 'Windows Kits\10\bin'
    $signtool = Get-ChildItem -LiteralPath $sdkBin -Filter signtool.exe -Recurse |
        Where-Object { $_.FullName -match '\\x64\\signtool.exe$' } |
        Sort-Object FullName -Descending | Select-Object -First 1
    if (-not $signtool) { Block 'Windows SDK signature verifier is unavailable.' }
    $signatureOutput = @(& $signtool.FullName verify /pa /v $package 2>&1)
    $signatureExit = $LASTEXITCODE
    $signatureOutput | Set-Content -LiteralPath (Join-Path $out 'signature.txt') -Encoding UTF8
    $signature = Get-AuthenticodeSignature -LiteralPath $package
    $report.signature = [ordered]@{
        signtoolExitCode = $signatureExit
        authenticodeStatus = [string]$signature.Status
        signerSubject = $(if ($signature.SignerCertificate) { $signature.SignerCertificate.Subject } else { $null })
        signerThumbprint = $(if ($signature.SignerCertificate) { $signature.SignerCertificate.Thumbprint } else { $null })
    }
    if ($signatureExit -ne 0) { Block 'Official package trust verification failed; package will not be installed or run.' }
    if ($report.download.packageSha256 -ne 'ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643') {
        if ($EnableReviewedLaunchCompatibility) {
            Block 'Reviewed launch compatibility package hash mismatch; inspect the changed floating download before running.'
        }
        Block 'Official floating download changed since reviewed baseline; inspect new package before running.'
    }
    $zip = [IO.Compression.ZipFile]::OpenRead($package)
    try {
        $entry = $zip.GetEntry('AppxManifest.xml')
        if (-not $entry) { Block 'Package has no root AppxManifest.xml.' }
        $reader = New-Object IO.StreamReader($entry.Open())
        try { [xml]$manifest = $reader.ReadToEnd() } finally { $reader.Dispose() }
    } finally { $zip.Dispose() }
    $identity = $manifest.Package.Identity
    $apps = @($manifest.SelectNodes("//*[local-name()='Applications']/*[local-name()='Application']"))
    $aliases = @($manifest.SelectNodes("//*[local-name()='ExecutionAlias']") | ForEach-Object { $_.GetAttribute('Alias') })
    $report.manifest = [ordered]@{
        name = $identity.GetAttribute('Name'); publisher = $identity.GetAttribute('Publisher')
        version = $identity.GetAttribute('Version'); architecture = $identity.GetAttribute('ProcessorArchitecture')
        applications = @($apps | ForEach-Object { [ordered]@{
            id = $_.GetAttribute('Id'); executable = $_.GetAttribute('Executable'); entryPoint = $_.GetAttribute('EntryPoint')
            aliases = @($_.SelectNodes(".//*[local-name()='ExecutionAlias']") | ForEach-Object { $_.GetAttribute('Alias') })
        } })
        aliases = $aliases
        dependencies = @($manifest.SelectNodes("//*[local-name()='Dependencies']/*") | ForEach-Object { $_.OuterXml })
    }
    if ($report.manifest.name -ne 'OpenAI.Codex' -or $report.manifest.architecture -ne 'x64') {
        Block 'Unexpected official package identity or architecture; inspect evidence before proceeding.'
    }
    Assert-DesktopCompatibilityPackage -LaunchCompatibility $EnableReviewedLaunchCompatibility `
        -DesktopVersion $report.manifest.version -PackageHash $report.download.packageSha256
    # Exact Desktop executable observed in the verified 26.1103.3517.0 package.
    # The two other applications are native Chrome host and command runner, not Desktop.
    $desktopApps = @($apps | Where-Object {
        $_.GetAttribute('Executable').Replace('\', '/') -eq 'app/ChatGPT.exe' -and
        $_.GetAttribute('EntryPoint') -eq 'Windows.FullTrustApplication'
    })
    if ($desktopApps.Count -ne 1) { Block 'Verified Desktop application selection is not unique.' }
    $desktopApp = $desktopApps[0]
    $report.selectedApplication = $desktopApp.GetAttribute('Id')
    $report.stage = 'interactive-session-gate'
    if (-not $hasInput -or -not [Environment]::UserInteractive -or
        $report.session.activeConsoleSessionId -ne $processSession -or
        $report.session.screenWidth -le 0 -or $report.session.screenHeight -le 0) {
        Block 'This runner does not expose the required interactive input desktop; GUI cold start not attempted.'
    }

    $aliasRoot = Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps'
    $aliasesBefore = @()
    if (Test-Path -LiteralPath $aliasRoot) {
        $aliasesBefore = @(Get-ChildItem -LiteralPath $aliasRoot -Filter 'chatgpt-desktop.exe' -Force -Recurse -Depth 1)
    }
    $report.aliasEntriesBeforeInstall = $aliasesBefore.Count
    if ($Mode -ne 'baseline' -and $aliasesBefore.Count -ne 0) {
        Block 'A Desktop alias already exists before installation; ownership would be ambiguous.'
    }
    $report.stage = 'licensed-install'
    # Exactly the official offline license path. Never skip licensing, resign, or relax security.
    $provisioned = Add-AppxProvisionedPackage -Online -PackagePath $package -LicensePath $license -Regions all
    $report.provisioning = [ordered]@{ restartNeeded = [bool]$provisioned.RestartNeeded }
    if ($provisioned.RestartNeeded) { Block 'Official provisioning requires restart; no session workaround attempted.' }
    $registered = @(Get-AppxPackage -Name 'OpenAI.Codex')
    if ($registered.Count -eq 0) {
        Block 'Official provisioning did not register the app for this runner session; sign-out/relogin is unavailable and was not fabricated.'
    }
    if ($registered.Count -ne 1) { Block 'Ambiguous registration after official provisioning.' }
    $app = $registered[0]
    $report.registration = [ordered]@{
        name = $app.Name; publisher = $app.Publisher; version = [string]$app.Version
        packageFullName = $app.PackageFullName; packageFamilyName = $app.PackageFamilyName
        status = [string]$app.Status
    }
    if ($app.Name -ne $report.manifest.name -or [string]$app.Version -ne $report.manifest.version -or
        $app.Publisher -ne $report.manifest.publisher -or [string]$app.Status -ne 'Ok') {
        Block 'Registered package does not match the verified download.'
    }
    if ($InspectStaticLauncher) {
        $report.stage = 'read-only-static-launch-metadata'
        $node = (Get-Command node.exe -CommandType Application -ErrorAction Stop).Source
        $inspector = Join-Path $env:GITHUB_WORKSPACE 'build/inspect-desktop-static-metadata.mjs'
        if ($node.Contains('"') -or $inspector.Contains('"') -or $app.InstallLocation.Contains('"')) {
            Block 'Invalid static-inspector executable or package path.'
        }
        $readStatic = New-Object Diagnostics.ProcessStartInfo
        $readStatic.FileName = $node
        $readStatic.Arguments = '--max-old-space-size=512 "' + $inspector + '" --package-root "' + $app.InstallLocation + '"'
        $readStatic.UseShellExecute = $false
        $readStatic.CreateNoWindow = $true
        $readStatic.RedirectStandardOutput = $true
        $readStatic.RedirectStandardError = $true
        $readStatic.WorkingDirectory = $work
        $readStatic.EnvironmentVariables.Clear()
        foreach ($name in @('SystemRoot', 'WINDIR', 'TEMP', 'TMP')) {
            $value = [Environment]::GetEnvironmentVariable($name)
            if ($value) { $readStatic.EnvironmentVariables[$name] = $value }
        }
        $readStatic.EnvironmentVariables['HOME'] = $work
        $readStatic.EnvironmentVariables['USERPROFILE'] = $work
        $readStatic.EnvironmentVariables['GITHUB_ACTIONS'] = 'true'
        $readStatic.EnvironmentVariables['RUNNER_OS'] = 'Windows'
        $staticProcess = [Diagnostics.Process]::Start($readStatic)
        $staticOut = $staticProcess.StandardOutput.ReadToEndAsync()
        $staticErr = $staticProcess.StandardError.ReadToEndAsync()
        if (-not $staticProcess.WaitForExit(120000)) {
            $staticProcess.Kill()
            [void]$staticProcess.WaitForExit(2000)
            Block 'Read-only static inspection timed out.'
        }
        if (-not $staticOut.Wait(2000) -or -not $staticErr.Wait(2000)) {
            Block 'Static inspector output did not close promptly.'
        }
        $staticText = $staticOut.GetAwaiter().GetResult().Trim()
        $staticErrorText = $staticErr.GetAwaiter().GetResult()
        if ($staticText.Length -gt 65600 -or $staticErrorText.Length -ne 0 -or
            -not $staticText.StartsWith('DESKTOP_STATIC_METADATA=') -or $staticText.Contains("`n")) {
            Block 'Unexpected static-inspector output.'
        }
        try {
            $staticBody = $staticText.Substring('DESKTOP_STATIC_METADATA='.Length)
            if ([Text.Encoding]::UTF8.GetByteCount($staticBody) -gt 65536) { throw 'Static report size exceeded.' }
            $staticReport = $staticBody | ConvertFrom-Json
            Assert-PublicCanonicalJSON $staticBody $staticReport
        } catch { Block 'Invalid, noncanonical or duplicate static-inspector metadata.' }
        function Assert-StaticShape($value, [string[]]$names) {
            if ($null -eq $value -or $value -isnot [pscustomobject]) { Block 'Invalid static metadata object.' }
            $actual = @($value.PSObject.Properties | ForEach-Object { $_.Name })
            if ($actual.Count -ne $names.Count) { Block 'Unexpected static metadata shape.' }
            foreach ($name in $actual) {
                if ($names -cnotcontains $name) { Block 'Unexpected static metadata field.' }
            }
        }
        function Assert-StaticCount($value, [long]$maximum) {
            if (($value -isnot [int] -and $value -isnot [long]) -or $value -lt 0 -or $value -gt $maximum) {
                Block 'Invalid static metadata count.'
            }
        }
        $staticCodes = @('invalid_utf8', 'truncated_archive', 'archive_too_large', 'unsupported_asar_prefix', 'invalid_header_size', 'header_too_large', 'truncated_header', 'invalid_header_pickle', 'invalid_json_size', 'invalid_header_padding', 'invalid_header_json', 'invalid_header_root', 'invalid_header_entry', 'invalid_unpacked_flag', 'invalid_link_entry', 'invalid_directory', 'header_too_deep', 'too_many_entries', 'invalid_entry_path', 'entry_path_too_long', 'invalid_file_size', 'invalid_file_offset', 'file_outside_archive', 'token_limit', 'unterminated_comment', 'unterminated_string', 'template_depth_limit', 'unterminated_template', 'ambiguous_slash', 'unterminated_regex', 'unsupported_lexical_syntax', 'anchor_limit', 'candidate_limit', 'unsupported_method_signature', 'method_limit_or_truncated', 'archive_not_regular', 'archive_changed', 'package_root_not_absolute', 'unexpected_package_directory', 'package_root_redirected', 'package_path_redirected', 'package_path_not_directory', 'disposable_windows_ci_required', 'invalid_arguments', 'report_limit', 'io_or_inspection_failed', 'archive_missing', 'package_path_missing')
        if ($staticReport.PSObject.Properties['status'] -and $staticReport.status -ceq 'blocked') {
            Assert-StaticShape $staticReport @('schema', 'status', 'reason')
            if ($staticReport.schema -ne 1 -or $staticCodes -cnotcontains $staticReport.reason) {
                Block 'Unreviewed static-inspector failure.'
            }
            $report.staticLauncher = [ordered]@{ schema = 1; status = 'blocked'; reason = $staticReport.reason }
            Block ('Static inspection blocked: ' + $staticReport.reason)
        }
        Assert-StaticShape $staticReport @('schema', 'status', 'runtimeKeyProven', 'archiveEntries', 'entries', 'publicPlugins')
        if ($staticProcess.ExitCode -ne 0 -or $staticReport.schema -ne 1 -or
            $staticReport.status -cne 'static_candidates_only' -or
            $staticReport.runtimeKeyProven -isnot [bool] -or $staticReport.runtimeKeyProven -ne $false) {
            Block 'Static inspection could not read the reviewed package safely.'
        }
        Assert-StaticCount $staticReport.archiveEntries 100000
        $expectedEntries = @('.vite/build/early-bootstrap-339-fe39e0119f57b81e.js',
            '.vite/build/early-bootstrap-824-d144f7c160c094ea.js',
            '.vite/build/early-bootstrap-715-fa9cbf44c39465d5.js')
        if (@($staticReport.entries).Count -ne 3) { Block 'Unexpected static-inspector entry count.' }
        $seenStaticEntries = @{}
        foreach ($entry in $staticReport.entries) {
            if ($expectedEntries -cnotcontains $entry.entry -or $seenStaticEntries.ContainsKey($entry.entry)) {
                Block 'Static metadata names an unreviewed source entry.'
            }
            $seenStaticEntries[$entry.entry] = $true
            if ($entry.status -cne 'static_candidates_only') {
                Assert-StaticShape $entry @('entry', 'status')
                if ((@('missing_entry', 'link_not_read', 'unpacked_not_read', 'entry_too_large',
                    'source_total_limit') + $staticCodes) -cnotcontains $entry.status) {
                    Block 'Unreviewed static entry status.'
                }
                continue
            }
            Assert-StaticShape $entry @('entry', 'status', 'lexicalStatus', 'rawOccurrences',
                'methods', 'candidatesLimited', 'candidates')
            if ((@('complete') + $staticCodes) -cnotcontains $entry.lexicalStatus -or
                $entry.candidatesLimited -isnot [bool]) { Block 'Invalid static lexical status.' }
            Assert-StaticShape $entry.rawOccurrences @('getConfigOverrides', 'startupCommand', 'code_mode_host')
            foreach ($property in $entry.rawOccurrences.PSObject.Properties) {
                Assert-StaticCount $property.Value 16777216
            }
            Assert-StaticShape $entry.methods @('getConfigOverrides', 'startupCommand')
            foreach ($property in $entry.methods.PSObject.Properties) {
                Assert-StaticShape $property.Value @('lexicalOccurrences', 'completeDefinitions', 'unsupportedDefinitions')
                foreach ($count in $property.Value.PSObject.Properties) { Assert-StaticCount $count.Value 128 }
            }
            if (@($entry.candidates).Count -gt 128) { Block 'Static candidate count exceeded its bound.' }
            if ($entry.lexicalStatus -cne 'complete' -and @($entry.candidates).Count -ne 0) {
                Block 'Incomplete static analysis emitted candidates.'
            }
            foreach ($candidate in $entry.candidates) {
                Assert-StaticShape $candidate @('key', 'type', 'evidence', 'method', 'occurrences')
                Assert-StaticCount $candidate.occurrences 128
                if ($candidate.key -isnot [string] -or $candidate.key.Length -gt 128 -or
                    $candidate.key -cnotmatch '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){0,8}$' -or
                    @('static_other', 'dynamic_expression', 'boolean_literal', 'number_literal',
                        'string_shaped_literal', 'empty_literal') -cnotcontains $candidate.type -or
                    @('adjacent_c_literal', 'literal_assignment_in_method') -cnotcontains $candidate.evidence -or
                    ($null -ne $candidate.method -and @('getConfigOverrides', 'startupCommand') -cnotcontains $candidate.method)) {
                    Block 'Static inspection emitted unreviewed candidate metadata.'
                }
            }
        }
        if ($staticReport.publicPlugins.PSObject.Properties['status'] -and $staticReport.publicPlugins.status -ceq 'blocked') {
            Assert-PublicCatalogFailure $staticReport.publicPlugins $staticCodes
            $report.publicPluginCatalog = [ordered]@{ status = 'blocked'; reason = $staticReport.publicPlugins.reason }
            Block ('Public catalog inspection blocked: ' + $staticReport.publicPlugins.reason)
        }
        if ($staticReport.publicPlugins.PSObject.Properties['candidates'] -and
            $staticReport.publicPlugins.candidates -is [System.Array] -and $staticReport.publicPlugins.candidates.Count -eq 0) {
            $report.publicPluginCatalog = [ordered]@{ status = 'no-public-candidates'; runtimeIdentityKnown = $false }
            Block 'No public plugin candidates were established; runtime identity remains unknown.'
        }
        # Do not publish candidate text or pass it to Desktop until independently bound to signed original ZIP bytes.
        $publicPluginEnvironment = ConvertTo-PublicPluginEnvironment $staticReport.publicPlugins $report.download.packageSha256
        Confirm-PublicCatalogProvenance $staticReport.publicPlugins $package $report.download.packageSha256
        $publicPluginCatalog = $staticReport.publicPlugins
        $report.publicPluginCatalog = [ordered]@{
            status = $publicPluginCatalog.status; keysSha256 = $publicPluginCatalog.keysSha256
            candidateCount = $publicPluginCatalog.candidates.Count
            manifestAnalysis = [ordered]@{
                pluginEntries = $publicPluginCatalog.manifestAnalysis.pluginEntries
                pluginManifests = $publicPluginCatalog.manifestAnalysis.pluginManifests
                mcpFiles = $publicPluginCatalog.manifestAnalysis.mcpFiles
                skippedEntries = $publicPluginCatalog.manifestAnalysis.skippedEntries
            }
            provenance = 'original-signed-msix-entry-sha256'
            runtimeMatchesObserved = $false
            limit = 'Status describes selected JavaScript coverage only; manifest gaps are counted separately. Public declarations do not establish native selection, loadability, authentication or authorization. A miss is not proof of absence.'
        }
        # Only bounded, validated public source paths, hashes and keys are now eligible for publication.
        $report.staticLauncher = $staticReport
        $staticJSON = $staticReport | ConvertTo-Json -Depth 12
        [IO.File]::WriteAllText((Join-Path $out 'static-launcher-metadata.json'), $staticJSON,
            (New-Object Text.UTF8Encoding($false)))

        # The prior full-file caller pass exhausted its node bound. Keep its offline
        # parser/schema regressions, but do not repeat an uninformative runtime scan.
        # The mandatory signed-package public-plugin provenance pass above is unchanged.
        $report.staticReadCallers = [ordered]@{
            schema = 1; status = 'not-run'; reason = 'prior_node_limit_no_new_evidence'
        }
        $report.staticReadCallersLimit = 'The optional full-file read-caller pass was not run. Its earlier node limit provides no caller or absence evidence; source-only regressions remain enabled.'

    }
    $controlHome = ''
    $bridgeName = $null
    if ($Mode -ne 'baseline') {
        $report.stage = 'registered-alias-and-bridge-configuration'
        $expectedBridge = Join-Path $env:RUNNER_TEMP ('magpie-ci-' + $env:GITHUB_SHA.Substring(0, 8) + '.exe')
        if ($BridgeExecutable -ne $expectedBridge -or -not (Test-Path -LiteralPath $BridgeExecutable -PathType Leaf)) {
            Block 'The exact commit-built bridge executable is unavailable.'
        }
        $bridgeName = [IO.Path]::GetFileName($BridgeExecutable)
        $desktopAliases = @($desktopApp.SelectNodes(".//*[local-name()='ExecutionAlias']") |
            ForEach-Object { $_.GetAttribute('Alias') })
        if ($desktopAliases -notcontains 'chatgpt-desktop.exe') { Block 'No reviewed official Desktop execution alias exists.' }
        # APPEXECLINK is not a regular file/symlink. Enumerate directory entries rather than
        # treating Test-Path on a guessed family-qualified path as the registration contract.
        $aliasEntries = @()
        $aliasWait = [Diagnostics.Stopwatch]::StartNew()
        do {
            $aliasEntries = @(Get-ChildItem -LiteralPath $aliasRoot -Filter 'chatgpt-desktop.exe' -Force -Recurse -Depth 1)
            if ($aliasEntries.Count -gt 0) { break }
            Start-Sleep -Seconds 1
        } while ($aliasWait.Elapsed.TotalSeconds -lt 60)
        $report.aliasObservationSeconds = [Math]::Round($aliasWait.Elapsed.TotalSeconds, 2)
        $report.aliasEntriesAfterInstall = @($aliasEntries | ForEach-Object {
            [ordered]@{ relativePath = $_.FullName.Substring($aliasRoot.Length).TrimStart('\'); attributes = [string]$_.Attributes }
        })
        $qualified = Join-Path $aliasRoot ($app.PackageFamilyName + '\chatgpt-desktop.exe')
        $rootAlias = Join-Path $aliasRoot 'chatgpt-desktop.exe'
        $selectedAliases = @($aliasEntries | Where-Object { $_.FullName -eq $qualified })
        $aliasQualified = $true
        if ($selectedAliases.Count -eq 0) {
            $selectedAliases = @($aliasEntries | Where-Object { $_.FullName -eq $rootAlias })
            $aliasQualified = $false
        }
        if ($selectedAliases.Count -ne 1) { Block 'No unique newly registered Desktop execution alias was enumerated.' }
        $alias = $selectedAliases[0].FullName
        $aliasTag = [DesktopColdProbe.Native]::AliasTag($alias)
        $report.aliasReparseTag = ('0x{0:X8}' -f $aliasTag)
        if ($aliasTag -ne 0x8000001BL) { Block 'Registered entry is not the documented CreateProcess application-alias tag.' }
        # The alias was absent before the signed install and is declared by its Desktop manifest.
        # Verify runtime package identity after launch; do not infer ownership from undocumented payload strings.
        # Discover the intact installed backend, never copy credentials or execute an extracted EXE.
        $officialBackends = @(Get-ChildItem -LiteralPath $app.InstallLocation -Filter codex.exe -File -Recurse)
        $report.backendCandidates = @($officialBackends | ForEach-Object { $_.FullName.Substring($app.InstallLocation.Length).TrimStart('\') })
        # This pinned package also contains app\Codex.exe, a second packaged executable.
        # Use the resource backend path and verify its CLI identity without opening an account.
        $resourceBackend = Join-Path $app.InstallLocation 'app\resources\codex.exe'
        $backendMatches = @($officialBackends | Where-Object { $_.FullName -eq $resourceBackend })
        if ($backendMatches.Count -ne 1) { Block 'The reviewed resource backend is unavailable.' }
        $officialBackend = $backendMatches[0]
        $versionHome = Join-Path $work 'version-home'
        $versionCodex = Join-Path $work 'version-codex'
        New-Item -ItemType Directory -Path $versionHome, $versionCodex | Out-Null
        $versionStart = New-Object Diagnostics.ProcessStartInfo
        $versionStart.FileName = $officialBackend.FullName; $versionStart.Arguments = '--version'
        $versionStart.WorkingDirectory = $versionHome
        $versionStart.UseShellExecute = $false; $versionStart.CreateNoWindow = $true
        $versionStart.RedirectStandardOutput = $true; $versionStart.RedirectStandardError = $true
        $versionStart.EnvironmentVariables.Clear()
        foreach ($name in @('SystemRoot', 'WINDIR', 'TEMP', 'TMP')) {
            $value = [Environment]::GetEnvironmentVariable($name)
            if ($value) { $versionStart.EnvironmentVariables[$name] = $value }
        }
        foreach ($name in @('HOME', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA')) {
            $versionStart.EnvironmentVariables[$name] = $versionHome
        }
        $versionStart.EnvironmentVariables['CODEX_HOME'] = $versionCodex
        $versionProcess = [Diagnostics.Process]::Start($versionStart)
        $versionOut = $versionProcess.StandardOutput.ReadToEndAsync()
        $versionErr = $versionProcess.StandardError.ReadToEndAsync()
        if (-not $versionProcess.WaitForExit(10000)) { $versionProcess.Kill(); Block 'Official backend version check timed out.' }
        if (-not $versionOut.Wait(2000) -or -not $versionErr.Wait(2000)) { Block 'Official version output did not close promptly.' }
        $versionText = $versionOut.GetAwaiter().GetResult().Trim()
        $versionError = $versionErr.GetAwaiter().GetResult()
        if ($versionProcess.ExitCode -ne 0 -or $versionError.Length -ne 0 -or
            $versionText -notmatch '^codex-cli [0-9][0-9A-Za-z.+-]{0,80}$') {
            Block 'Resource executable did not identify itself as the official Codex CLI.'
        }
        $report.officialBackendVersion = $versionText
        $backends = @()
        foreach ($id in @('alpha', 'beta')) {
            $backendHome = Join-Path $work ($id + '-home')
            $codexHome = Join-Path $work ($id + '-codex')
            New-Item -ItemType Directory -Path $backendHome, $codexHome | Out-Null
            # Fail-local endpoint only, no API keys and no authorization to infer or sign in.
            $toml = @"
model_provider = "$($id)_provider"
model = "gpt-5.5"
[features]
apps = false
plugins = false
[model_providers.$($id)_provider]
name = "CI $id launch-only"
base_url = "http://127.0.0.1:9/v1"
wire_api = "responses"
requires_openai_auth = false
"@
            [IO.File]::WriteAllText((Join-Path $codexHome 'config.toml'), $toml, (New-Object Text.UTF8Encoding($false)))
            $backends += [ordered]@{
                id = $id; provider = ($id + '_provider'); command = $officialBackend.FullName
                home = $backendHome; codex_home = $codexHome
            }
        }
        $bridgeConfig = Join-Path $work 'launch-only-bridge.json'
        $actualBackendHash = ''
        if ($EnableReviewedStartupProbe) {
            $actualBackendHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $officialBackend.FullName).Hash.ToLowerInvariant()
        }
        if ($EnableReviewedLaunchCompatibility) {
            $controlHome = Join-Path $env:RUNNER_TEMP ('desktop-control-home-' + [guid]::NewGuid().ToString('N'))
            try { New-Item -ItemType Directory -Path $controlHome | Out-Null }
            catch { Block 'The separate bridge-owned home could not be created.' }
        }
        $bridgeSettings = New-DesktopBridgeSettings -Backends $backends -StartupProbe $EnableReviewedStartupProbe `
            -LaunchCompatibility $EnableReviewedLaunchCompatibility -VersionText $versionText -BackendHash $actualBackendHash -ControlHome $controlHome
        [IO.File]::WriteAllText($bridgeConfig, ($bridgeSettings | ConvertTo-Json -Depth 5),
            (New-Object Text.UTF8Encoding($false)))
        $report.bridge = [ordered]@{
            fileName = $bridgeName
            sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $BridgeExecutable).Hash.ToLowerInvariant()
            backendRelativePath = $officialBackend.FullName.Substring($app.InstallLocation.Length).TrimStart('\')
            backendSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $officialBackend.FullName).Hash.ToLowerInvariant()
            aliasName = 'chatgpt-desktop.exe'; aliasPackageQualified = $aliasQualified
            configuration = 'Two empty homes, loopback discard endpoints, no auth or startup compatibility opt-in.'
            processObservationLimit = 'Start event identifies the uniquely named configured bridge and package parent; a short-lived image path may not be queryable.'
        }
        $report.bridge.startupInspectionEnabled = [bool]$EnableReviewedStartupProbe
        if ($EnableReviewedStartupProbe) {
            $report.bridge.configuration = 'Two empty homes, loopback discard endpoints, exact-version SHA-pinned startup inspection only; no authentication or JSONL policy opt-in.'
            $report.bridge.startupInspectionLimits = 'Both official one-shot results must match completely and be genuinely unrestricted. Non-null requirements remain unsupported, never replaced by null. No login or inference; no zero-egress claim.'
        }
        $report.bridge.launchCompatibilityEnabled = [bool]$EnableReviewedLaunchCompatibility
        if ($EnableReviewedLaunchCompatibility) {
            $report.bridge.configuration = 'Two empty homes and loopback discard endpoints; exact-version SHA-pinned startup and separately selected launch compatibility modes.'
            $report.bridge.compatibilityModes = [ordered]@{
                desktop_plugin_override = $bridgeSettings.desktop_plugin_override
                attestation = $bridgeSettings.attestation
                gateway_oauth = $bridgeSettings.gateway_oauth
                gateway_oauth_provider_name = $bridgeSettings.gateway_oauth_provider_name
                config_requirements_read = $bridgeSettings.config_requirements_read
                auth_status = $bridgeSettings.auth_status
                experimental_feature_list = $bridgeSettings.experimental_feature_list
                control_plane = $bridgeSettings.control_plane
            }
            $report.bridge.launchCompatibilityLimits = @(
                'Only the exact reviewed Desktop plugin override is forwarded. No plugin installation, copy, cache link or shared backend working directory is configured.',
                'Gateway OAuth is a primary native read only. False or null is not logged in; no login, credential import, inference or extra diagnostic RPC is authorized.',
                'Attestation remains unavailable. An attestation failure is not evidence of a Windows token failure.',
                'Auth status preserves includeToken requests but accepts only a genuine primary no-auth null/null/false result in these empty disposable homes. It is not real-account support or a guarantee against receiving credentials if misused.',
                'Requirements succeed only after fresh native reads from both backends actually return requirements:null. Errors, missing or non-null results are never replaced with null.',
                'Feature pages succeed only after fresh unscoped reads from both native backends match completely. Cursor and limit are preserved; non-null thread scope, errors and unequal pages fail closed.',
                'Global config/read returns bridge-owned routing facts only, after fresh paired requirements:null checks. It never reads native config, fabricates policy or auth fields, or accepts cwd or raw layers. project/list and fs/readFile remain unsupported. GUI usability is unverified.',
                'Configured modes and observed responses are separate evidence. No successful initialization, plugin loadability, account, model or zero-egress claim.'
            )
        }
        $check = New-Object Diagnostics.ProcessStartInfo
        $check.FileName = $BridgeExecutable; $check.Arguments = '--check'
        $check.UseShellExecute = $false; $check.CreateNoWindow = $true
        $check.EnvironmentVariables.Clear()
        foreach ($name in @('SystemRoot', 'WINDIR', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA', 'TEMP', 'TMP')) {
            $value = [Environment]::GetEnvironmentVariable($name)
            if ($value) { $check.EnvironmentVariables[$name] = $value }
        }
        $check.EnvironmentVariables['MAGPIE_APP_SERVER_BRIDGE_CONFIG'] = $bridgeConfig
        $checker = [Diagnostics.Process]::Start($check)
        if (-not $checker.WaitForExit(10000)) { $checker.Kill(); Block 'Bridge configuration validation timed out.' }
        if ($checker.ExitCode -ne 0) { Block 'Launch-only bridge configuration did not validate.' }
        $report.bridge.configurationValidated = $true
        $launchExecutable = $BridgeExecutable
        $launchName = $bridgeName
        if ($Mode -eq 'observe') {
            $expectedObserver = Join-Path $env:RUNNER_TEMP ('magpie-observer-' + $env:GITHUB_SHA.Substring(0, 8) + '.exe')
            if ($ObserverExecutable -ne $expectedObserver -or -not (Test-Path -LiteralPath $ObserverExecutable -PathType Leaf)) {
                Block 'The exact commit-built test observer is unavailable.'
            }
            $launchExecutable = $ObserverExecutable
            $launchName = [IO.Path]::GetFileName($ObserverExecutable)
            $tracePrefix = Join-Path $work 'rpc-metadata'
            $report.observer = [ordered]@{
                fileName = $launchName
                sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $ObserverExecutable).Hash.ToLowerInvariant()
                scope = 'Instrumented launch only; argv and wire bytes are forwarded unchanged to the exact commit-built bridge with separately recorded opt-ins.'
            }
        }
    }
    $aumid = $app.PackageFamilyName + '!' + $desktopApp.GetAttribute('Id')
    $report.aumid = $aumid
    $report.stage = 'normal-package-activation'
    $subscription = Register-CimIndicationEvent -Query 'SELECT * FROM Win32_ProcessStartTrace' -SourceIdentifier $eventSource
    [uint32]$launched = 0
    if ($Mode -eq 'baseline') {
        $hr = [DesktopColdProbe.Native]::Activate($aumid, [ref]$launched)
        $activationPid = [int]$launched
        $report.activation = [ordered]@{ method = 'AUMID'; hresult = ('0x{0:X8}' -f $hr); processId = $activationPid }
        if ($hr -ne 0 -or $activationPid -le 0) { Block 'Normal package activation failed.' }
    } else {
        $start = New-Object Diagnostics.ProcessStartInfo
        $start.FileName = $alias; $start.UseShellExecute = $false
        $start.WorkingDirectory = $work
        # Explicit non-secret Windows environment only; no runner tokens or shell-wide override.
        $start.EnvironmentVariables.Clear()
        foreach ($name in @('SystemRoot', 'WINDIR', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA',
            'TEMP', 'TMP', 'COMSPEC', 'HOMEDRIVE', 'HOMEPATH', 'USERNAME', 'USERDOMAIN',
            'ALLUSERSPROFILE', 'PROGRAMDATA', 'PROGRAMFILES', 'PROGRAMFILES(X86)',
            'COMMONPROGRAMFILES', 'COMMONPROGRAMFILES(X86)', 'PROCESSOR_ARCHITECTURE',
            'NUMBER_OF_PROCESSORS', 'OS', 'SESSIONNAME')) {
            $value = [Environment]::GetEnvironmentVariable($name)
            if ($value) { $start.EnvironmentVariables[$name] = $value }
        }
        $start.EnvironmentVariables['PATH'] = (Join-Path $env:SystemRoot 'System32') + ';' + $env:SystemRoot +
            ';' + (Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0')
        $start.EnvironmentVariables['CODEX_CLI_PATH'] = $launchExecutable
        if ($Mode -eq 'observe') {
            $start.EnvironmentVariables['MAGPIE_CI_REAL_BRIDGE'] = $BridgeExecutable
            $start.EnvironmentVariables['MAGPIE_CI_TRACE_FILE'] = $tracePrefix
            $start.EnvironmentVariables['MAGPIE_CI_READ_TARGET_ROOTS'] = New-DesktopReadTargetEnvironment `
                -Backends $backends -Workspace $work -VerifiedPackage $app.InstallLocation -ControlHome $controlHome
            if ($null -ne $publicPluginEnvironment) {
                $start.EnvironmentVariables['MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES'] = $publicPluginEnvironment
            }
        }
        $start.EnvironmentVariables['MAGPIE_APP_SERVER_BRIDGE_CONFIG'] = $bridgeConfig
        Assert-PublicEnvironmentSize $start.EnvironmentVariables
        $started = [Diagnostics.Process]::Start($start)
        if (-not $started) { Block 'Registered alias did not return a process.' }
        $activationPid = $started.Id
        if ($EnableReviewedLaunchCompatibility) {
            # Keep the original process and its full-precision identity. WMI's
            # CreationDate is only report metadata, never a cleanup equality key.
            $ownedActivationProcess = $started
            $null = $ownedActivationProcess.Handle
            $ownedActivationCreatedUtc = $ownedActivationProcess.StartTime.ToUniversalTime().ToString('o')
        }
        $report.activation = [ordered]@{ method = 'newly-registered-application-alias'; processId = $activationPid }
    }
    $windows = @()
    $observed = @{}
    for ($i = 0; $i -lt 60; $i++) {
        Start-Sleep -Seconds 1
        foreach ($p in @(Get-CimInstance Win32_Process -Filter "SessionId=$processSession")) {
            if ([DesktopColdProbe.Native]::PackageFor([uint32]$p.ProcessId) -eq $app.PackageFullName) {
                $observed[[string]$p.ProcessId] = [ordered]@{
                    processId = [int]$p.ProcessId; parentProcessId = [int]$p.ParentProcessId
                    name = $p.Name; sessionId = [int]$p.SessionId
                    createdUtc = $p.CreationDate.ToUniversalTime().ToString('o')
                    packageIdentityVerified = $true
                }
            }
        }
        $windows = @([DesktopColdProbe.Native]::Windows() | Where-Object {
            $observed.ContainsKey([string]$_.ProcessId) -and
            [DesktopColdProbe.Native]::PackageFor([uint32]$_.ProcessId) -eq $app.PackageFullName
        })
    }
    $report.processes = @($observed.Values | Sort-Object processId)
    $report.windows = @($windows | Select-Object ProcessId, Left, Top, Width, Height)
    $starts = @(Get-Event -SourceIdentifier $eventSource -ErrorAction SilentlyContinue | ForEach-Object { $_.SourceEventArgs.NewEvent })
    $report.processStarts = @($starts | Where-Object {
        $observed.ContainsKey([string]$_.ProcessID) -or $observed.ContainsKey([string]$_.ParentProcessID)
    } | ForEach-Object { [ordered]@{
        processId = [int]$_.ProcessID; parentProcessId = [int]$_.ParentProcessID; name = [string]$_.ProcessName
        startedFileTime = [string]$_.TIME_CREATED
    } })
    if (-not $observed.ContainsKey([string]$activationPid)) { Block 'Activation PID package identity was not observed.' }
    if ($windows.Count -eq 0) { Block 'No visible window belonging to the verified package appeared.' }
    $report.stage = 'window-capture'
    $window = $windows | Sort-Object { $_.Width * $_.Height } -Descending | Select-Object -First 1
    # Read pixels only. Do not focus, click, inject, change resolution, or dismiss security prompts.
    if ([DesktopColdProbe.Native]::PackageFor([uint32]$window.ProcessId) -ne $app.PackageFullName) {
        Block 'Window owner changed before capture.'
    }
    $left = [Math]::Max(0, $window.Left); $top = [Math]::Max(0, $window.Top)
    $right = [Math]::Min($report.session.screenWidth, $window.Left + $window.Width)
    $bottom = [Math]::Min($report.session.screenHeight, $window.Top + $window.Height)
    $width = $right - $left; $height = $bottom - $top
    if ($width -le 0 -or $height -le 0) { Block 'App window is not in the capturable primary screen.' }
    $bitmap = New-Object Drawing.Bitmap($width, $height)
    $graphics = [Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.CopyFromScreen($left, $top, 0, 0, $bitmap.Size)
        $bitmap.Save((Join-Path $out 'official-window.png'), [Drawing.Imaging.ImageFormat]::Png)
    } finally { $graphics.Dispose(); $bitmap.Dispose() }
    $capturePath = Join-Path $out 'official-window.png'
    if ((Get-Item -LiteralPath $capturePath).Length -le 1048576) {
        Write-Output ('DESKTOP_SCREENSHOT_PNG=' + [Convert]::ToBase64String([IO.File]::ReadAllBytes($capturePath)))
    }
    $report.capture = 'official-window.png; screen pixels may contain occlusion and require visual review'
    if ($Mode -ne 'baseline') {
        $bridgeStarts = @($report.processStarts | Where-Object {
            $_.name -eq $launchName -and $observed.ContainsKey([string]$_.parentProcessId)
        })
        if ($Mode -eq 'observe') {
            $report.observer.startEvents = $bridgeStarts
            $report.bridge.startEvents = @()
        } else { $report.bridge.startEvents = $bridgeStarts }
        $report.bridge.sha256After = (Get-FileHash -Algorithm SHA256 -LiteralPath $BridgeExecutable).Hash.ToLowerInvariant()
        if ($report.bridge.sha256After -ne $report.bridge.sha256) { Block 'Bridge bytes changed during the probe.' }
        $report.bridge.diagnosticMarkers = @()
        $markers = @('magpie startup probe unavailable', 'unsupported arguments; use --help',
            'unsupported startup sign-out', 'invalid or unreadable bridge configuration')
        # Inspect only fresh test-app .log files, emit exact static marker names, never log lines or auth files.
        $logRoots = @((Join-Path $env:LOCALAPPDATA ('Packages\' + $app.PackageFamilyName + '\LocalCache')),
            (Join-Path $env:APPDATA 'Codex'), (Join-Path $env:APPDATA 'ChatGPT'))
        foreach ($root in $logRoots) {
            if (-not (Test-Path -LiteralPath $root)) { continue }
            foreach ($file in @(Get-ChildItem -LiteralPath $root -Filter '*.log' -File -Recurse -ErrorAction SilentlyContinue |
                Where-Object { $_.Length -le 1048576 } | Select-Object -First 20)) {
                foreach ($marker in $markers) {
                    if (Select-String -LiteralPath $file.FullName -SimpleMatch -Pattern $marker -Quiet) {
                        $report.bridge.diagnosticMarkers += $marker
                    }
                }
            }
        }
        $report.bridge.diagnosticMarkers = @($report.bridge.diagnosticMarkers | Select-Object -Unique)
        if ($bridgeStarts.Count -eq 0) { Block 'No uniquely named configured child start event was observed under the verified Desktop package.' }
        if ($Mode -eq 'observe') {
            $report.observer.sha256After = (Get-FileHash -Algorithm SHA256 -LiteralPath $ObserverExecutable).Hash.ToLowerInvariant()
            if ($report.observer.sha256After -ne $report.observer.sha256) { Block 'Observer bytes changed during the diagnostic.' }
            # Only the observer's fixed metadata schema is eligible for publication.
            # Unknown fields or values fail closed without copying or printing the record.
            $keys = @('seq', 'pid', 'kind', 'direction', 'method', 'correlation', 'code', 'class',
                'requestAttestation', 'explicitGatewayOauth', 'requiredHumanApproval', 'experimentalApi',
                'flag', 'configKey', 'configValue', 'configShape', 'configFamily',
                'configLexicalRoot', 'configKeyClasses', 'configLiteral', 'child_pid', 'exit',
                'publicPluginCatalogSHA256', 'publicPluginRef', 'configPluginShape',
                'authParamsValid', 'includeTokenRequested', 'refreshTokenRequested', 'tracePresent', 'traceValid',
                'configParamsValid', 'configIncludeLayers', 'configCwdProvided', 'featureListParamsValid',
                'featureListCursorProvided', 'featureListLimitProvided', 'featureListThreadIdProvided',
                'projectListParamsValid', 'projectListCursorProvided', 'projectListLimitProvided',
                'projectListSortKey', 'projectListSortDirection', 'fsReadParamsShapeValid',
                'fsReadPathForm', 'fsReadBasename', 'fsReadLexicalRoot') + @(Get-PublicThreadListShapeFields)
            $kinds = @('observer_start', 'public_plugin_catalog', 'argv', 'argv_config', 'rpc', 'rpc_error', 'stream_end',
                'setup_error', 'child_start_failed', 'child_start', 'copy_error', 'child_exit')
            $classes = @('test_only', 'request', 'notification', 'duplicate_id', 'correlation_limit',
                'response', 'unmatched_response', 'unrecognized', 'oversized', 'unterminated',
                'unsupported_authentication_capability', 'initialization_required',
                'config_requirements_disabled', 'config_requirements_failed', 'gateway_oauth_disabled',
                'unsupported_method', 'invalid_capabilities', 'invalid_initialization', 'already_initialized',
                'startup_probe_unavailable', 'unknown', 'eof', 'read_error', 'copy_error') + @(Get-PublicThreadListErrorClasses)
            $methods = @(Get-PublicRPCMethods)
            $flags = @('app-server', '--check', '--help', '-h', '--version', '-V',
                '--analytics-default-enabled', '--stdio', '--print-startup-config', '--startup-sign-out',
                '--listen', '--startup-client-info', '-c', '--config')
            $configKeys = @(
                'agents', 'agents.default_subagent_model', 'agents.default_subagent_reasoning_effort',
                'agents.enabled', 'agents.interrupt_message', 'agents.max_concurrent_threads_per_session',
                'agents.max_depth', 'allow_login_shell', 'allow_symlinked_codex_home',
                'analytics', 'analytics.enabled', 'approval_policy',
                'approval_policy.granular', 'approval_policy.granular.mcp_elicitations', 'approval_policy.granular.request_permissions',
                'approval_policy.granular.rules', 'approval_policy.granular.sandbox_approval', 'approval_policy.granular.skill_approval',
                'approvals_reviewer', 'apps', 'apps._default',
                'apps._default.approvals_reviewer', 'apps._default.default_tools_approval_mode', 'apps._default.destructive_enabled',
                'apps._default.enabled', 'apps._default.open_world_enabled', 'apps_mcp_product_sku',
                'audio', 'audio.microphone', 'audio.microphone_channel',
                'audio.speaker', 'auto_review', 'auto_review.circuit_break_action',
                'auto_review.conversation_history_max_output_tokens', 'auto_review.experimental_conversation_history_prompt', 'auto_review.experimental_policy_template',
                'auto_review.extra_policy', 'auto_review.policy', 'auto_review.timeout_ms',
                'background_terminal_max_timeout', 'browser_use', 'browser_use.allow_history_access',
                'browser_use.default_origin_policy', 'browser_use.default_origin_policy.access', 'browser_use.default_origin_policy.downloads',
                'browser_use.default_origin_policy.full_cdp_access', 'browser_use.default_origin_policy.uploads', 'browser_use.origins',
                'chatgpt_base_url', 'check_for_update_on_startup', 'cli_auth_credentials_store',
                'cloud', 'cloud.skills', 'cloud.skills.enabled',
                'compact_prompt', 'computer_use', 'computer_use.default_app_access',
                'computer_use.macos', 'computer_use.macos.bundle_ids', 'computer_use.windows',
                'computer_use.windows.aumids', 'computer_use.windows.exes', 'daybreak',
                'default_permissions', 'desktop', 'developer_instructions',
                'disable_paste_burst', 'experimental_compact_prompt_file', 'experimental_realtime_start_instructions',
                'experimental_realtime_webrtc_call_base_url', 'experimental_realtime_ws_backend_prompt', 'experimental_realtime_ws_base_url',
                'experimental_realtime_ws_model', 'experimental_realtime_ws_startup_context', 'experimental_thread_store',
                'experimental_thread_store.type', 'experimental_use_unified_exec_tool', 'features',
                'features.agent_message_board', 'features.analytics_plan_history', 'features.api_key_cyber_access_programs',
                'features.api_key_model_discovery', 'features.app_server_websocket_compression', 'features.apply_patch_freeform',
                'features.apply_patch_preserve_line_endings', 'features.apply_patch_streaming_events', 'features.apps',
                'features.apps_early_resource_discovery', 'features.apps_mcp_path_override', 'features.apps_mcp_path_override.enabled',
                'features.apps_mcp_path_override.path', 'features.async_compaction', 'features.async_compaction.enabled',
                'features.async_compaction.model_async_compact_threshold_percent', 'features.auth_elicitation', 'features.background_paginated_rollout_migration',
                'features.bedrock_setup_wizard', 'features.browser_annotation_api', 'features.browser_use',
                'features.browser_use_external', 'features.browser_use_full_cdp_access', 'features.chronicle',
                'features.cli_daybreak', 'features.code_mode', 'features.code_mode.default_exec_yield_time_ms',
                'features.code_mode.direct_only_tool_namespaces', 'features.code_mode.enabled', 'features.code_mode.excluded_tool_namespaces',
                'features.code_mode.experimental_show_cell_overhead', 'features.code_mode.tool_input_schema_max_bytes', 'features.code_mode_buffered_exec',
                'features.code_mode_host', 'features.code_mode_host.disable_in_process_fallback', 'features.code_mode_host.enabled',
                'features.code_mode_host_grpc', 'features.code_mode_host_output_truncation', 'features.code_mode_interrupt',
                'features.code_mode_only', 'features.code_mode_only_strict_3p_tools', 'features.code_mode_prewarm',
                'features.code_mode_tool_description_first', 'features.code_mode_tool_search', 'features.codex_apps_mcp_2026_07_28',
                'features.codex_git_commit', 'features.codex_hooks', 'features.collab',
                'features.collaboration_modes', 'features.compaction_image_budget', 'features.computer_use',
                'features.concurrent_reasoning_summaries', 'features.connectors', 'features.content_item_kinds',
                'features.context_management', 'features.context_management.experimental_mode', 'features.credential_masking',
                'features.critical_path_timing', 'features.current_time_reminder', 'features.current_time_reminder.clock_source',
                'features.current_time_reminder.delivery_mode', 'features.current_time_reminder.enabled', 'features.current_time_reminder.reminder_interval_seconds',
                'features.current_time_reminder.sleep_tool', 'features.cwd_relative_turn_diffs', 'features.daemon_auto_start',
                'features.default_mode_request_user_input', 'features.defer_mailbox_preemption', 'features.deferred_executor',
                'features.deferred_tool_world_state', 'features.elevated_windows_sandbox', 'features.enable_experimental_windows_sandbox',
                'features.enable_fanout', 'features.enable_mcp_apps', 'features.enable_request_compression',
                'features.exec_permission_approvals', 'features.executed_tool_call_metadata', 'features.executor_capability_discovery',
                'features.executor_capability_discovery_v2', 'features.experimental_use_unified_exec_tool', 'features.experimental_windows_sandbox',
                'features.external_agent_memory_import', 'features.external_migration', 'features.fast_mode',
                'features.gcp_setup_wizard', 'features.goals', 'features.guardian_approval',
                'features.guardian_conversation_history_tools', 'features.guardian_enhanced_node_repl_transcripts', 'features.guardian_ext',
                'features.guardian_native_multi_agent_history', 'features.guardian_node_repl_transcript_images', 'features.guardian_recorded_items',
                'features.guardian_reuse_parent_compaction', 'features.guardian_root_handoff_context', 'features.guardian_trust_orchestrator_connectors',
                'features.guardianv2', 'features.guardianv2.async_classifier_conversation_token_limit', 'features.guardianv2.async_classifier_mode',
                'features.guardianv2.classifier_instructions', 'features.guardianv2.enabled', 'features.guardianv2.free_guardian',
                'features.guardianv2.max_action_tokens', 'features.guardianv2.max_classifier_instruction_tokens', 'features.guardianv2.max_parent_compaction_tokens',
                'features.guardianv2.max_tool_call_lag', 'features.guardianv2.persist_scores', 'features.guardianv2.reasoning_effort',
                'features.guardianv2.reuse_parent_compaction', 'features.guardianv2.review_scope', 'features.guardianv2.review_scope.computer_use_only',
                'features.guardianv2.review_scope.sandboxed_exec_commands', 'features.guardianv2.review_threshold', 'features.guardianv2.thread_context',
                'features.guardianv2.transcript', 'features.guardianv2.transcript.include_images', 'features.guardianv2.transcript.max_message_entry_tokens',
                'features.guardianv2.transcript.max_message_transcript_tokens', 'features.guardianv2.transcript.max_recent_non_user_entries', 'features.guardianv2.transcript.max_tool_entry_tokens',
                'features.guardianv2.transcript.max_tool_transcript_tokens', 'features.guardianv2.transcript.sources', 'features.guardianv2.transcript_mode',
                'features.guardianv2_decisions_comparison', 'features.guardianv3', 'features.guardianv3.enabled',
                'features.hooks', 'features.image_detail_original', 'features.image_generation',
                'features.image_resize_notice', 'features.imagegenext', 'features.in_app_browser',
                'features.in_app_chat', 'features.in_app_dictation', 'features.in_app_local_automation',
                'features.in_app_updates', 'features.in_app_voice', 'features.incremental_exec_tools',
                'features.incremental_tools', 'features.instant_interrupt', 'features.interrupt_on_cancel',
                'features.item_ids', 'features.js_repl', 'features.js_repl_tools_only',
                'features.local_thread_store_compression', 'features.local_thread_store_shared_compression', 'features.login_shell_package_path',
                'features.mcp_2026_07_28', 'features.mcp_connection_sharing', 'features.mcp_oauth_refresh_coordination',
                'features.mcp_tool_list_changed', 'features.memories', 'features.memory_tool',
                'features.mentions_v2', 'features.model_catalog_in_context', 'features.multi_agent',
                'features.multi_agent_mode', 'features.multi_agent_v2', 'features.multi_agent_v2.default_wait_timeout_ms',
                'features.multi_agent_v2.disable_direct_message', 'features.multi_agent_v2.disable_native_tools', 'features.multi_agent_v2.enabled',
                'features.multi_agent_v2.expose_spawn_agent_model_overrides', 'features.multi_agent_v2.hide_spawn_agent_metadata', 'features.multi_agent_v2.max_concurrent_threads_per_session',
                'features.multi_agent_v2.max_wait_timeout_ms', 'features.multi_agent_v2.message_board_in_memory', 'features.multi_agent_v2.message_board_remote',
                'features.multi_agent_v2.message_board_remote.bearer_token', 'features.multi_agent_v2.message_board_remote.bearer_token_env_var', 'features.multi_agent_v2.message_board_remote.url',
                'features.multi_agent_v2.min_wait_timeout_ms', 'features.multi_agent_v2.multi_agent_mode_hint_text', 'features.multi_agent_v2.non_code_mode_only',
                'features.multi_agent_v2.preserve_fork_prefix', 'features.multi_agent_v2.root_agent_usage_hint_text', 'features.multi_agent_v2.subagent_developer_instructions',
                'features.multi_agent_v2.subagent_usage_hint_text', 'features.multi_agent_v2.tool_namespace', 'features.multi_agent_v2.usage_hint_enabled',
                'features.multi_agent_v2.usage_hint_text', 'features.multi_agent_v2.wait_agent_enabled', 'features.multi_agent_v2_dynamic_tools',
                'features.multi_env_copy_file', 'features.network_proxy', 'features.network_proxy.allow_local_binding',
                'features.network_proxy.allow_upstream_proxy', 'features.network_proxy.credential_broker', 'features.network_proxy.credentials',
                'features.network_proxy.dangerously_allow_all_unix_sockets', 'features.network_proxy.dangerously_allow_non_loopback_proxy', 'features.network_proxy.domains',
                'features.network_proxy.enable_socks5', 'features.network_proxy.enable_socks5_udp', 'features.network_proxy.enabled',
                'features.network_proxy.mode', 'features.network_proxy.proxy_url', 'features.network_proxy.socks_url',
                'features.network_proxy.unix_sockets', 'features.non_prefixed_mcp_tool_names', 'features.non_prefixed_mcp_tool_names.enabled',
                'features.non_prefixed_mcp_tool_names.server_names', 'features.nonfatal_clock_read_errors', 'features.omit_app_server_notification_media',
                'features.omit_skill_descriptions', 'features.output_token_replay', 'features.personality',
                'features.plugin_discovery_prefetch', 'features.plugin_hooks', 'features.plugin_sharing',
                'features.plugins', 'features.powershell_shell_version', 'features.prediction_preserve_parent_tools',
                'features.prefer_mxc', 'features.prevent_idle_sleep', 'features.psp',
                'features.realtime_conversation', 'features.reasoning_effort_override', 'features.recommended_plugins',
                'features.remote_compaction_v2', 'features.remote_control', 'features.remote_models',
                'features.remote_plugin', 'features.request_permissions', 'features.request_permissions_tool',
                'features.request_rule', 'features.resize_all_images', 'features.respect_system_proxy',
                'features.responses_websockets', 'features.responses_websockets_v2', 'features.retain_client_developer_messages',
                'features.retain_client_tool_outputs', 'features.rollout_budget', 'features.rollout_budget.enabled',
                'features.rollout_budget.enforce_limit', 'features.rollout_budget.limit_tokens', 'features.rollout_budget.prefill_token_weight',
                'features.rollout_budget.reminder_at_remaining_tokens', 'features.rollout_budget.sampling_token_weight', 'features.runtime_agent_instances',
                'features.runtime_agent_instances.enabled', 'features.runtime_agent_instances.mode', 'features.runtime_metrics',
                'features.search_tool', 'features.secret_auth_storage', 'features.send_async_message',
                'features.send_message_to_user_async', 'features.shell_snapshot', 'features.shell_snapshot_v2',
                'features.shell_tool', 'features.shell_zsh_fork', 'features.simplified_selection_hints',
                'features.skill_catalog_deltas', 'features.skill_env_var_dependency_prompt', 'features.skill_mcp_dependency_install',
                'features.skill_search', 'features.skip_host_skill_discovery', 'features.sleep_tool',
                'features.sleep_tool.enabled', 'features.sleep_tool.mode', 'features.sqlite',
                'features.stable_environment_tools', 'features.standalone_web_search', 'features.steer',
                'features.step_model_switching', 'features.subagent_default_context_limits', 'features.subagent_thread_environments',
                'features.suppress_model_change_announcements', 'features.system_proxy_fallback', 'features.telepathy',
                'features.terminal_resize_reflow', 'features.terminal_visualization_instructions', 'features.token_budget',
                'features.token_budget.auto_compact_fallback_buffer_tokens', 'features.token_budget.auto_compact_fallback_prompt', 'features.token_budget.enabled',
                'features.token_budget.guidance_message', 'features.token_budget.reminder_message_template', 'features.token_budget.reminder_threshold_tokens',
                'features.token_budget.use_history_notes_extension', 'features.tool_call_mcp_elicitation', 'features.tool_registry',
                'features.tool_registry.error_on_tool_collisions', 'features.tool_registry.turn_metadata_includes_tool_info', 'features.tool_search',
                'features.tool_search_always_defer_mcp_tools', 'features.tool_suggest', 'features.transcript_v2',
                'features.tui_app_server', 'features.tui_extensions_statusline', 'features.tui_multi_backend',
                'features.ultrafast_mode', 'features.unavailable_dummy_tools', 'features.unbounded_connection_retries',
                'features.undo', 'features.unified_exec', 'features.unified_exec_tty',
                'features.unified_exec_zsh_fork', 'features.unified_image_budget', 'features.untrusted_as_tool_output',
                'features.use_agent_identity', 'features.use_legacy_landlock', 'features.use_linux_sandbox_bwrap',
                'features.use_xaa', 'features.view_image', 'features.wake_on_agent_messages',
                'features.web_search', 'features.web_search_cached', 'features.web_search_request',
                'features.windows_sandbox_service', 'features.workspace_dependencies', 'features.workspace_owner_usage_nudge',
                'features.worktrees', 'features.write_stdin_approval', 'feedback',
                'feedback.enabled', 'file_opener', 'forced_chatgpt_workspace_id',
                'forced_login_method', 'ghost_snapshot', 'ghost_snapshot.disable_warnings',
                'ghost_snapshot.ignore_large_untracked_dirs', 'ghost_snapshot.ignore_large_untracked_files', 'goals',
                'goals.max_goal_token_budget', 'hide_agent_reasoning', 'history',
                'history.max_bytes', 'history.persistence', 'hooks',
                'hooks.Interrupt', 'hooks.PermissionRequest', 'hooks.PostCompact',
                'hooks.PostModelRequest', 'hooks.PostToolUse', 'hooks.PreCompact',
                'hooks.PreModelRequest', 'hooks.PreToolUse', 'hooks.SessionEnd',
                'hooks.SessionStart', 'hooks.Stop', 'hooks.SubagentStart',
                'hooks.SubagentStop', 'hooks.UserPromptSubmit', 'hooks.state',
                'include_apps_instructions', 'include_collaboration_mode_instructions', 'include_environment_context',
                'include_environment_context_time', 'include_permissions_instructions', 'instructions',
                'log_dir', 'marketplaces', 'mcp_enterprise_managed_auth',
                'mcp_enterprise_managed_auth.idp', 'mcp_enterprise_managed_auth.idp.client_id', 'mcp_enterprise_managed_auth.idp.client_secret',
                'mcp_enterprise_managed_auth.idp.issuer', 'mcp_enterprise_managed_auth.idp.token_endpoint_auth_method', 'mcp_oauth_callback_port',
                'mcp_oauth_callback_url', 'mcp_oauth_credentials_store', 'mcp_optional_startup_grace_ms',
                'mcp_servers', 'memories', 'memories.consolidation_model',
                'memories.consolidation_service_tier', 'memories.dedicated_tools', 'memories.disable_on_external_context',
                'memories.dual_write', 'memories.extract_model', 'memories.generate_memories',
                'memories.max_raw_memories_for_consolidation', 'memories.max_rollout_age_days', 'memories.max_rollouts_per_startup',
                'memories.max_unused_days', 'memories.min_rate_limit_remaining_percent', 'memories.min_rollout_idle_hours',
                'memories.use_memories', 'memories.version', 'model',
                'model_auto_compact_token_limit', 'model_auto_compact_token_limit_scope', 'model_catalog_json',
                'model_context_window', 'model_instructions_file', 'model_post_turn_compact_threshold_percent',
                'model_provider', 'model_providers', 'model_reasoning_effort',
                'model_reasoning_summary', 'model_verbosity', 'notice',
                'notice."hide_gpt-5.1-codex-max_migration_prompt"', 'notice.external_config_migration_prompts', 'notice.external_config_migration_prompts.home',
                'notice.external_config_migration_prompts.home_last_prompted_at', 'notice.external_config_migration_prompts.project_last_prompted_at', 'notice.external_config_migration_prompts.projects',
                'notice.fast_default_opt_out', 'notice.hide_full_access_warning', 'notice.hide_gov_cloud_guidance',
                'notice.hide_gpt5_1_migration_prompt', 'notice.hide_rate_limit_model_nudge', 'notice.hide_world_writable_warning',
                'notice.model_migrations', 'notify', 'openai_base_url',
                'orchestrator', 'orchestrator.mcp', 'orchestrator.mcp.enabled',
                'orchestrator.skills', 'orchestrator.skills.enabled', 'oss_provider',
                'otel', 'otel.environment', 'otel.exporter',
                'otel.exporter.otlp-grpc', 'otel.exporter.otlp-grpc.endpoint', 'otel.exporter.otlp-grpc.headers',
                'otel.exporter.otlp-grpc.tls', 'otel.exporter.otlp-grpc.tls.ca-certificate', 'otel.exporter.otlp-grpc.tls.client-certificate',
                'otel.exporter.otlp-grpc.tls.client-private-key', 'otel.exporter.otlp-http', 'otel.exporter.otlp-http.endpoint',
                'otel.exporter.otlp-http.headers', 'otel.exporter.otlp-http.protocol', 'otel.exporter.otlp-http.tls',
                'otel.exporter.otlp-http.tls.ca-certificate', 'otel.exporter.otlp-http.tls.client-certificate', 'otel.exporter.otlp-http.tls.client-private-key',
                'otel.log_agent_responses', 'otel.log_guardian_assessments', 'otel.log_sse_deltas',
                'otel.log_user_prompt', 'otel.metrics_exporter', 'otel.metrics_exporter.otlp-grpc',
                'otel.metrics_exporter.otlp-grpc.endpoint', 'otel.metrics_exporter.otlp-grpc.headers', 'otel.metrics_exporter.otlp-grpc.tls',
                'otel.metrics_exporter.otlp-grpc.tls.ca-certificate', 'otel.metrics_exporter.otlp-grpc.tls.client-certificate', 'otel.metrics_exporter.otlp-grpc.tls.client-private-key',
                'otel.metrics_exporter.otlp-http', 'otel.metrics_exporter.otlp-http.endpoint', 'otel.metrics_exporter.otlp-http.headers',
                'otel.metrics_exporter.otlp-http.protocol', 'otel.metrics_exporter.otlp-http.tls', 'otel.metrics_exporter.otlp-http.tls.ca-certificate',
                'otel.metrics_exporter.otlp-http.tls.client-certificate', 'otel.metrics_exporter.otlp-http.tls.client-private-key', 'otel.span_attributes',
                'otel.tool_result', 'otel.tool_result.max_bytes', 'otel.trace_exporter',
                'otel.trace_exporter.otlp-grpc', 'otel.trace_exporter.otlp-grpc.endpoint', 'otel.trace_exporter.otlp-grpc.headers',
                'otel.trace_exporter.otlp-grpc.tls', 'otel.trace_exporter.otlp-grpc.tls.ca-certificate', 'otel.trace_exporter.otlp-grpc.tls.client-certificate',
                'otel.trace_exporter.otlp-grpc.tls.client-private-key', 'otel.trace_exporter.otlp-http', 'otel.trace_exporter.otlp-http.endpoint',
                'otel.trace_exporter.otlp-http.headers', 'otel.trace_exporter.otlp-http.protocol', 'otel.trace_exporter.otlp-http.tls',
                'otel.trace_exporter.otlp-http.tls.ca-certificate', 'otel.trace_exporter.otlp-http.tls.client-certificate', 'otel.trace_exporter.otlp-http.tls.client-private-key',
                'otel.tracestate', 'permissions', 'personality',
                'plan_mode_reasoning_effort', 'plugins', 'plugins._default',
                'plugins._default.enabled', 'profile', 'profiles',
                'project_doc_fallback_filenames', 'project_doc_max_bytes', 'project_root_markers',
                'projects', 'realtime', 'realtime.transport',
                'realtime.type', 'realtime.version', 'realtime.voice',
                'responses_api_metadata', 'review_model', 'sandbox_mode',
                'sandbox_workspace_write', 'sandbox_workspace_write.exclude_slash_tmp', 'sandbox_workspace_write.exclude_tmpdir_env_var',
                'sandbox_workspace_write.network_access', 'sandbox_workspace_write.writable_roots', 'service_tier',
                'shell_environment_policy', 'shell_environment_policy.exclude', 'shell_environment_policy.experimental_use_profile',
                'shell_environment_policy.filters', 'shell_environment_policy.ignore_default_excludes', 'shell_environment_policy.include_only',
                'shell_environment_policy.inherit', 'shell_environment_policy.set', 'show_raw_agent_reasoning',
                'skills', 'skills.bundled', 'skills.bundled.enabled',
                'skills.config', 'skills.include_instructions', 'skills.max_context_tokens',
                'sqlite_home', 'suppress_unstable_features_warning', 'thread_unload_delay_secs',
                'tool_output_token_limit', 'tool_suggest', 'tool_suggest.disabled_tools',
                'tool_suggest.discoverables', 'tools', 'tools.experimental_request_user_input',
                'tools.experimental_request_user_input.enabled', 'tools.update_plan', 'tools.update_plan.enabled',
                'tools.web_search', 'tools.web_search.allowed_domains', 'tools.web_search.context_size',
                'tools.web_search.location', 'tools.web_search.location.city', 'tools.web_search.location.country',
                'tools.web_search.location.region', 'tools.web_search.location.timezone', 'tui',
                'tui.agents_overview_grouping', 'tui.alternate_screen', 'tui.animations',
                'tui.auto_recap', 'tui.copy_on_select', 'tui.copy_on_select_clear_selection',
                'tui.disable_paste_burst', 'tui.effects', 'tui.effects.effort',
                'tui.effects.progress', 'tui.effects.shimmer', 'tui.effects.starfield',
                'tui.effects.title', 'tui.effects.welcome', 'tui.fullscreen_transcript',
                'tui.keymap', 'tui.keymap.agents', 'tui.keymap.agents.archive',
                'tui.keymap.agents.delete', 'tui.keymap.agents.fork', 'tui.keymap.agents.hide',
                'tui.keymap.agents.new_task', 'tui.keymap.agents.new_worktree', 'tui.keymap.agents.rename',
                'tui.keymap.agents.resume', 'tui.keymap.agents.search', 'tui.keymap.agents.stop',
                'tui.keymap.agents.toggle_grouping', 'tui.keymap.agents.toggle_pin', 'tui.keymap.approval',
                'tui.keymap.approval.approve', 'tui.keymap.approval.approve_for_prefix', 'tui.keymap.approval.approve_for_session',
                'tui.keymap.approval.cancel', 'tui.keymap.approval.decline', 'tui.keymap.approval.deny',
                'tui.keymap.approval.open_fullscreen', 'tui.keymap.approval.open_thread', 'tui.keymap.chat',
                'tui.keymap.chat.decrease_reasoning_effort', 'tui.keymap.chat.edit_queued_message', 'tui.keymap.chat.increase_reasoning_effort',
                'tui.keymap.chat.interrupt_turn', 'tui.keymap.chat.next_permission_mode', 'tui.keymap.chat.open_presets',
                'tui.keymap.chat.previous_permission_mode', 'tui.keymap.chat.prompt_stack_back', 'tui.keymap.chat.skip_question',
                'tui.keymap.chat.toggle_voice', 'tui.keymap.chat.toggle_voice_mute', 'tui.keymap.composer',
                'tui.keymap.composer.history_search_next', 'tui.keymap.composer.history_search_previous', 'tui.keymap.composer.queue',
                'tui.keymap.composer.submit', 'tui.keymap.composer.toggle_shortcuts', 'tui.keymap.editor',
                'tui.keymap.editor.delete_backward', 'tui.keymap.editor.delete_backward_word', 'tui.keymap.editor.delete_forward',
                'tui.keymap.editor.delete_forward_word', 'tui.keymap.editor.insert_newline', 'tui.keymap.editor.kill_line_end',
                'tui.keymap.editor.kill_line_start', 'tui.keymap.editor.kill_whole_line', 'tui.keymap.editor.move_down',
                'tui.keymap.editor.move_left', 'tui.keymap.editor.move_line_end', 'tui.keymap.editor.move_line_start',
                'tui.keymap.editor.move_right', 'tui.keymap.editor.move_up', 'tui.keymap.editor.move_word_left',
                'tui.keymap.editor.move_word_right', 'tui.keymap.editor.yank', 'tui.keymap.global',
                'tui.keymap.global.clear_terminal', 'tui.keymap.global.copy', 'tui.keymap.global.find_transcript',
                'tui.keymap.global.focus_activity', 'tui.keymap.global.leader', 'tui.keymap.global.open_agents',
                'tui.keymap.global.open_external_editor', 'tui.keymap.global.open_transcript', 'tui.keymap.global.queue',
                'tui.keymap.global.select_transcript', 'tui.keymap.global.submit', 'tui.keymap.global.toggle_fast_mode',
                'tui.keymap.global.toggle_raw_output', 'tui.keymap.global.toggle_shortcuts', 'tui.keymap.global.toggle_side_conversation',
                'tui.keymap.global.toggle_vim_mode', 'tui.keymap.list', 'tui.keymap.list.accept',
                'tui.keymap.list.cancel', 'tui.keymap.list.jump_bottom', 'tui.keymap.list.jump_top',
                'tui.keymap.list.move_down', 'tui.keymap.list.move_left', 'tui.keymap.list.move_right',
                'tui.keymap.list.move_up', 'tui.keymap.list.page_down', 'tui.keymap.list.page_up',
                'tui.keymap.pager', 'tui.keymap.pager.close', 'tui.keymap.pager.close_transcript',
                'tui.keymap.pager.find', 'tui.keymap.pager.half_page_down', 'tui.keymap.pager.half_page_up',
                'tui.keymap.pager.jump_bottom', 'tui.keymap.pager.jump_top', 'tui.keymap.pager.page_down',
                'tui.keymap.pager.page_up', 'tui.keymap.pager.scroll_down', 'tui.keymap.pager.scroll_up',
                'tui.keymap.vim_normal', 'tui.keymap.vim_normal.append_after_cursor', 'tui.keymap.vim_normal.append_line_end',
                'tui.keymap.vim_normal.cancel_operator', 'tui.keymap.vim_normal.change_to_line_end', 'tui.keymap.vim_normal.delete_char',
                'tui.keymap.vim_normal.delete_to_line_end', 'tui.keymap.vim_normal.enter_insert', 'tui.keymap.vim_normal.enter_replace_mode',
                'tui.keymap.vim_normal.find_backward', 'tui.keymap.vim_normal.find_forward', 'tui.keymap.vim_normal.insert_line_start',
                'tui.keymap.vim_normal.jump_bottom', 'tui.keymap.vim_normal.jump_top', 'tui.keymap.vim_normal.move_down',
                'tui.keymap.vim_normal.move_left', 'tui.keymap.vim_normal.move_line_end', 'tui.keymap.vim_normal.move_line_start',
                'tui.keymap.vim_normal.move_right', 'tui.keymap.vim_normal.move_up', 'tui.keymap.vim_normal.move_word_backward',
                'tui.keymap.vim_normal.move_word_end', 'tui.keymap.vim_normal.move_word_forward', 'tui.keymap.vim_normal.open_line_above',
                'tui.keymap.vim_normal.open_line_below', 'tui.keymap.vim_normal.paste_after', 'tui.keymap.vim_normal.redo',
                'tui.keymap.vim_normal.repeat_last_change', 'tui.keymap.vim_normal.replace_char', 'tui.keymap.vim_normal.start_change_operator',
                'tui.keymap.vim_normal.start_delete_operator', 'tui.keymap.vim_normal.start_yank_operator', 'tui.keymap.vim_normal.substitute_char',
                'tui.keymap.vim_normal.till_backward', 'tui.keymap.vim_normal.till_forward', 'tui.keymap.vim_normal.undo',
                'tui.keymap.vim_normal.yank_line', 'tui.keymap.vim_operator', 'tui.keymap.vim_operator.cancel',
                'tui.keymap.vim_operator.delete_line', 'tui.keymap.vim_operator.motion_down', 'tui.keymap.vim_operator.motion_find_backward',
                'tui.keymap.vim_operator.motion_find_forward', 'tui.keymap.vim_operator.motion_jump_bottom', 'tui.keymap.vim_operator.motion_jump_top',
                'tui.keymap.vim_operator.motion_left', 'tui.keymap.vim_operator.motion_line_end', 'tui.keymap.vim_operator.motion_line_start',
                'tui.keymap.vim_operator.motion_right', 'tui.keymap.vim_operator.motion_till_backward', 'tui.keymap.vim_operator.motion_till_forward',
                'tui.keymap.vim_operator.motion_up', 'tui.keymap.vim_operator.motion_word_backward', 'tui.keymap.vim_operator.motion_word_end',
                'tui.keymap.vim_operator.motion_word_forward', 'tui.keymap.vim_operator.select_around_text_object', 'tui.keymap.vim_operator.select_inner_text_object',
                'tui.keymap.vim_operator.yank_line', 'tui.keymap.vim_search', 'tui.keymap.vim_search.backward',
                'tui.keymap.vim_search.forward', 'tui.keymap.vim_search.next', 'tui.keymap.vim_search.previous',
                'tui.keymap.vim_text_object', 'tui.keymap.vim_text_object.backtick', 'tui.keymap.vim_text_object.big_word',
                'tui.keymap.vim_text_object.braces', 'tui.keymap.vim_text_object.brackets', 'tui.keymap.vim_text_object.cancel',
                'tui.keymap.vim_text_object.double_quote', 'tui.keymap.vim_text_object.parentheses', 'tui.keymap.vim_text_object.single_quote',
                'tui.keymap.vim_text_object.word', 'tui.model_availability_nux', 'tui.mouse_scroll_speed',
                'tui.notification_condition', 'tui.notification_method', 'tui.notifications',
                'tui.pet', 'tui.pet_anchor', 'tui.presets',
                'tui.question_esc_back', 'tui.raw_output_mode', 'tui.rendering',
                'tui.rendering.lists', 'tui.rendering.math', 'tui.rendering.mermaid',
                'tui.rendering.tables', 'tui.resume_cwd', 'tui.right_click_paste',
                'tui.screen_reader_detection_done', 'tui.session_picker_view', 'tui.show_server_version_notice',
                'tui.show_tooltips', 'tui.status_line', 'tui.status_line_use_colors',
                'tui.terminal_resize_reflow_max_rows', 'tui.terminal_title', 'tui.theme',
                'tui.vim_mode_default', 'web_search', 'windows',
                'windows.allow_mxc', 'windows.sandbox',
                'plugins.browser@openai-bundled.auto_provision',
                'plugins.browser@openai-bundled.enabled',
                'plugins.chrome-dev@openai-bundled.auto_provision',
                'plugins.chrome-dev@openai-bundled.enabled',
                'plugins.chrome-internal@openai-bundled.auto_provision',
                'plugins.chrome-internal@openai-bundled.enabled',
                'plugins.chrome@openai-bundled.auto_provision',
                'plugins.chrome@openai-bundled.enabled',
                'plugins.computer-use@openai-bundled.auto_provision',
                'plugins.computer-use@openai-bundled.enabled',
                'plugins.sites@openai-bundled.auto_provision',
                'plugins.sites@openai-bundled.enabled',
                'plugins.unified-computer-use@openai-bundled.auto_provision',
                'plugins.unified-computer-use@openai-bundled.enabled',
                'plugins.visualize@openai-bundled.auto_provision',
                'plugins.visualize@openai-bundled.enabled'
            )
            $configFamilies = @(
                'agents', 'allow_login_shell', 'allow_symlinked_codex_home',
                'analytics', 'approval_policy', 'approvals_reviewer',
                'apps', 'apps_mcp_product_sku', 'audio',
                'auto_review', 'background_terminal_max_timeout', 'browser_use',
                'chatgpt_base_url', 'check_for_update_on_startup', 'cli_auth_credentials_store',
                'cloud', 'compact_prompt', 'computer_use',
                'daybreak', 'default_permissions', 'desktop',
                'developer_instructions', 'disable_paste_burst', 'experimental_compact_prompt_file',
                'experimental_realtime_start_instructions', 'experimental_realtime_webrtc_call_base_url', 'experimental_realtime_ws_backend_prompt',
                'experimental_realtime_ws_base_url', 'experimental_realtime_ws_model', 'experimental_realtime_ws_startup_context',
                'experimental_thread_store', 'experimental_use_unified_exec_tool', 'features',
                'feedback', 'file_opener', 'forced_chatgpt_workspace_id',
                'forced_login_method', 'ghost_snapshot', 'goals',
                'hide_agent_reasoning', 'history', 'hooks',
                'include_apps_instructions', 'include_collaboration_mode_instructions', 'include_environment_context',
                'include_environment_context_time', 'include_permissions_instructions', 'instructions',
                'log_dir', 'marketplaces', 'mcp_enterprise_managed_auth',
                'mcp_oauth_callback_port', 'mcp_oauth_callback_url', 'mcp_oauth_credentials_store',
                'mcp_optional_startup_grace_ms', 'mcp_servers', 'memories',
                'model', 'model_auto_compact_token_limit', 'model_auto_compact_token_limit_scope',
                'model_catalog_json', 'model_context_window', 'model_instructions_file',
                'model_post_turn_compact_threshold_percent', 'model_provider', 'model_providers',
                'model_reasoning_effort', 'model_reasoning_summary', 'model_verbosity',
                'notice', 'notify', 'openai_base_url',
                'orchestrator', 'oss_provider', 'otel',
                'permissions', 'personality', 'plan_mode_reasoning_effort',
                'plugins', 'profile', 'profiles',
                'project_doc_fallback_filenames', 'project_doc_max_bytes', 'project_root_markers',
                'projects', 'realtime', 'responses_api_metadata',
                'review_model', 'sandbox_mode', 'sandbox_workspace_write',
                'service_tier', 'shell_environment_policy', 'show_raw_agent_reasoning',
                'skills', 'sqlite_home', 'suppress_unstable_features_warning',
                'thread_unload_delay_secs', 'tool_output_token_limit', 'tool_suggest',
                'tools', 'tui', 'web_search',
                'windows', 'other'
            )
            $configValues = @('bool_true', 'bool_false', 'enum_file', 'enum_keyring', 'enum_auto',
                'enum_ephemeral', 'enum_elevated', 'enum_unelevated', 'enum_mxc', 'enum_chatgpt', 'enum_api', 'other')
            $configShapes = @('no_equals', 'empty_key', 'quoted_key', 'plain_key', 'other_key')
            $configKeyClasses = @('at_sign', 'slash', 'backslash', 'colon', 'ascii_space',
                'ascii_control', 'non_ascii', 'other_ascii_punctuation')
            $configLiterals = @('bool_true', 'bool_false', 'other')
            $booleanKeys = @('requestAttestation', 'explicitGatewayOauth', 'requiredHumanApproval', 'experimentalApi',
                'authParamsValid', 'includeTokenRequested', 'refreshTokenRequested', 'tracePresent', 'traceValid',
                'configParamsValid', 'configIncludeLayers', 'configCwdProvided', 'featureListParamsValid',
                'featureListCursorProvided', 'featureListLimitProvided', 'featureListThreadIdProvided',
                'projectListParamsValid', 'projectListCursorProvided', 'projectListLimitProvided', 'fsReadParamsShapeValid')
            $numericKeys = @('seq', 'pid', 'correlation', 'code', 'child_pid', 'exit', 'publicPluginRef')
            $seenPublicCatalogs = @{}
            $allEvents = @()
            $traceFiles = @()
            $complete = $true
            for ($slot = 1; $slot -le 16; $slot++) {
                $tracePath = $tracePrefix + ('.{0:D2}.jsonl' -f $slot)
                if (-not (Test-Path -LiteralPath $tracePath)) { continue }
                $traceInfo = Get-Item -LiteralPath $tracePath
                if ($traceInfo.Length -gt 65536 -or ($traceInfo.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
                    Block 'Unexpected observer trace file size or type.'
                }
                $lines = @(Get-Content -LiteralPath $tracePath -Encoding UTF8)
                $fileEvents = @()
                $previousSequence = 0
                foreach ($line in $lines) {
                    if ($line.Length -eq 0) { continue }
                    if ($line.Length -gt 4096) { Block 'Observer metadata record is too large.' }
                    try {
                        $record = $line | ConvertFrom-Json
                        Assert-PublicCanonicalJSON $line $record
                    } catch { Block 'Observer metadata is not canonical valid JSON.' }
                    Assert-PublicAuthRequestEvent $record
                    Assert-PublicReadRequestEvent $record
                    Assert-PublicPluginShapeEvent $record
                    Assert-PublicPluginEvent $record $publicPluginCatalog $seenPublicCatalogs
                    foreach ($property in $record.PSObject.Properties) {
                        if ($keys -cnotcontains $property.Name) { Block 'Observer emitted an unexpected metadata field.' }
                        $value = $property.Value
                        if ($numericKeys -ccontains $property.Name) {
                            $isNumber = $value -is [int] -or $value -is [long] -or $value -is [uint32] -or
                                $value -is [uint64] -or $value -is [double] -or $value -is [decimal]
                            if (-not $isNumber -or [double]$value -ne [Math]::Truncate([double]$value) -or
                                [Math]::Abs([double]$value) -gt 4294967296) {
                                Block 'Observer emitted invalid numeric metadata.'
                            }
                        } elseif ($property.Name -ceq 'publicPluginCatalogSHA256') {
                            Assert-PublicHash $value
                        } elseif ($booleanKeys -ccontains $property.Name) {
                            if ($value -isnot [bool]) { Block 'Observer emitted invalid boolean metadata.' }
                        } elseif ($property.Name -ceq 'configKeyClasses') {
                            if ($value -isnot [System.Array] -or $value.Count -lt 1 -or $value.Count -gt 8) {
                                Block 'Observer emitted invalid config key classes.'
                            }
                            if ($value.Count -eq 1 -and $value[0] -is [string] -and $value[0] -ceq 'none') {
                                # The sole none label represents no listed class.
                            } else {
                                $previousClassIndex = -1
                                foreach ($label in $value) {
                                    if ($label -isnot [string]) { Block 'Observer emitted invalid config key class type.' }
                                    $classIndex = [Array]::IndexOf($configKeyClasses, $label)
                                    if ($classIndex -lt 0 -or $classIndex -le $previousClassIndex) {
                                        Block 'Observer emitted an unknown, repeated or unordered config key class.'
                                    }
                                    $previousClassIndex = $classIndex
                                }
                            }
                        } else {
                            if ($value -isnot [string]) { Block 'Observer emitted invalid label metadata.' }
                            $allowed = switch ($property.Name) {
                                'kind' { $kinds }
                                'class' { $classes }
                                'method' { $methods }
                                'direction' { @('to_bridge', 'from_bridge', 'stderr') }
                                'flag' { $flags }
                                'configKey' { $configKeys }
                                'configValue' { $configValues }
                                'configShape' { $configShapes }
                                'configFamily' { $configFamilies }
                                'configLexicalRoot' { $configFamilies }
                                'configLiteral' { $configLiterals }
                                'configPluginShape' { Get-PublicPluginShapes }
                                'projectListSortKey' { Get-PublicReadTargetLabels 'projectListSortKey' }
                                'projectListSortDirection' { Get-PublicReadTargetLabels 'projectListSortDirection' }
                                'fsReadPathForm' { Get-PublicReadTargetLabels 'fsReadPathForm' }
                                'fsReadBasename' { Get-PublicReadTargetLabels 'fsReadBasename' }
                                'fsReadLexicalRoot' { Get-PublicReadTargetLabels 'fsReadLexicalRoot' }
                                'threadListProjectIdShape' { @('absent', 'null', 'string', 'invalid') }
                                'threadListParentIdShape' { @('absent', 'null', 'string', 'invalid') }
                                'threadListAncestorIdShape' { @('absent', 'null', 'string', 'invalid') }
                                'threadListAttachedIdShape' { @('absent', 'null', 'string', 'invalid') }
                            }
                            if ($allowed -cnotcontains $value) { Block 'Observer emitted an unreviewed metadata label.' }
                        }
                    }
                    $hasShape = [bool]$record.PSObject.Properties['configShape']
                    $hasFamily = [bool]$record.PSObject.Properties['configFamily']
                    $hasConfigKey = [bool]$record.PSObject.Properties['configKey']
                    $hasConfigValue = [bool]$record.PSObject.Properties['configValue']
                    $hasLexicalRoot = [bool]$record.PSObject.Properties['configLexicalRoot']
                    $hasKeyClasses = [bool]$record.PSObject.Properties['configKeyClasses']
                    $hasLiteral = [bool]$record.PSObject.Properties['configLiteral']
                    $hasPublicReference = [bool]$record.PSObject.Properties['publicPluginRef']
                    if ($record.kind -eq 'argv_config') {
                        $unknownConfig = $record.PSObject.Properties['class'] -and $record.class -ceq 'unknown'
                        if ($hasPublicReference) {
                            # The exact five-field shape and per-PID digest were checked above.
                        } elseif ($unknownConfig) {
                            if (-not $hasShape -or -not $hasFamily -or -not $hasLexicalRoot -or
                                -not $hasKeyClasses -or -not $hasLiteral -or $hasConfigKey -or $hasConfigValue) {
                                Block 'Unknown config metadata contains raw or incomplete fields.'
                            }
                            if ($record.configShape -ceq 'no_equals' -and
                                ($record.configLexicalRoot -cne 'other' -or $record.configLiteral -cne 'other' -or
                                 $record.configKeyClasses.Count -ne 1 -or $record.configKeyClasses[0] -cne 'none')) {
                                Block 'An unassigned argument exposed config metadata.'
                            }
                        } elseif (-not $hasConfigKey -or -not $hasConfigValue -or $hasShape -or $hasFamily -or
                            $hasLexicalRoot -or $hasKeyClasses -or $hasLiteral -or $record.PSObject.Properties['class']) {
                            Block 'Known config metadata has an invalid shape.'
                        }
                    } elseif ($hasShape -or $hasFamily -or $hasConfigKey -or $hasConfigValue -or
                        $hasLexicalRoot -or $hasKeyClasses -or $hasLiteral) {
                        Block 'Config metadata occurred outside an argument event.'
                    }
                    if (-not $record.PSObject.Properties['seq'] -or -not $record.PSObject.Properties['pid'] -or
                        -not $record.PSObject.Properties['kind'] -or $record.seq -le $previousSequence -or $record.pid -le 0) {
                        Block 'Observer metadata ordering or identity is invalid.'
                    }
                    if (@($bridgeStarts | ForEach-Object { $_.processId }) -notcontains $record.pid) {
                        Block 'Observer metadata is not tied to an observed test child.'
                    }
                    $previousSequence = $record.seq
                    $fileEvents += $record
                }
                $filePids = @($fileEvents | ForEach-Object { $_.pid } | Select-Object -Unique)
                if ($filePids.Count -ne 1 -or ($null -ne $publicPluginCatalog -and
                    -not $seenPublicCatalogs.ContainsKey([string]$filePids[0]))) {
                    Block 'Observer trace has no unique verified per-process public catalog.'
                }
                if ($traceInfo.Length -ge 64512 -or @($fileEvents | Where-Object { $_.kind -eq 'child_exit' }).Count -eq 0 -or
                    @($fileEvents | Where-Object { $_.kind -eq 'stream_end' -and
                        $_.PSObject.Properties['direction'] -and $_.direction -eq 'from_bridge' -and
                        $_.PSObject.Properties['class'] -and $_.class -eq 'eof' }).Count -eq 0 -or
                    @($fileEvents | Where-Object { $_.kind -eq 'stream_end' -and
                        $_.PSObject.Properties['direction'] -and $_.direction -eq 'stderr' -and
                        $_.PSObject.Properties['class'] -and $_.class -eq 'eof' }).Count -eq 0) {
                    $complete = $false
                }
                $allEvents += $fileEvents
                $traceName = [IO.Path]::GetFileName($tracePath)
                # Publish only the validated snapshot, never recopy a file that can still grow.
                $validatedLines = @($fileEvents | ForEach-Object { $_ | ConvertTo-Json -Depth 4 -Compress })
                [IO.File]::WriteAllLines((Join-Path $out $traceName), [string[]]$validatedLines, (New-Object Text.UTF8Encoding($false)))
                $traceFiles += [ordered]@{ fileName = $traceName; bytes = $traceInfo.Length; records = $fileEvents.Count }
            }
            if ($traceFiles.Count -eq 0 -or $allEvents.Count -eq 0) { Block 'Instrumented launch produced no readable metadata trace.' }
            if ($traceFiles.Count -eq 16) { $complete = $false }
            $report.observer.traceFiles = $traceFiles
            $report.observer.completeLifecycleEvidence = $complete
            $report.observer.correlationLimitsObserved = @($allEvents | Where-Object {
                $_.PSObject.Properties['class'] -and $_.class -in @('duplicate_id', 'correlation_limit')
            }).Count -gt 0
            $report.observer.publicPluginMatches = @($allEvents | Where-Object {
                $_.PSObject.Properties['publicPluginRef']
            } | ForEach-Object { [ordered]@{
                key = $publicPluginCatalog.candidates[$_.publicPluginRef - 1].key
                configValue = $_.configValue
            } })
            if ($null -ne $publicPluginCatalog) {
                $report.publicPluginCatalog.runtimeMatchesObserved = $report.observer.publicPluginMatches.Count -gt 0
            }
            $report.observer.recordCount = $allEvents.Count
            $report.observer.records = @($allEvents | Select-Object -First 256)
            $report.observer.recordsTruncatedInSummary = $allEvents.Count -gt 256
            $report.observer.limit = 'Missing child exit or output EOF, saturated slots or truncated summary preclude claims that no failure occurred. Read validated artifact records for remaining evidence.'
            if ($EnableReviewedLaunchCompatibility) {
                # Every collected trace snapshot passed the full schema, ordering,
                # process/catalog identity and artifact checks above. This is not
                # the 256-record preview or the completeLifecycleEvidence flag.
                $captureValidated = $true
                $report.captureValidated = $true
                $report.observer.rpcErrorEvidence = Get-DesktopRPCErrorEvidence $allEvents $methods
                $report.observer.rpcErrorEvidenceLimit = 'Counts cover observed RPC errors in the full sanitized snapshot, including unsupported methods and fixed thread/list rejection buckets. Filter shapes in the full trace are JSON types only; null projectId means unassigned threads. They are not complete-lifecycle evidence; zero errors does not prove all Desktop APIs are supported.'
                $report.observer.selectedCompatibilityScope = 'auth-status-feature-list-and-global-owned-config-only'
                $report.observer.fullDesktopCompatibility = 'not-established'
                # Full validated snapshots, not the truncated summary. No result payload is inspected or logged.
                $report.observer.noAuthStatusEvidence = Get-DesktopNoAuthStatusEvidence $allEvents
                $report.observer.noAuthStatusEvidenceLimit = 'Correlated success relies on the exact bridge no-auth result validator. It proves only this credential-free fixture, not login, token support or full Desktop compatibility.'
                $report.observer.featureListEvidence = Get-DesktopFeatureListEvidence $allEvents
                $report.observer.featureListEvidenceLimit = 'Correlated success relies on the production whole-page comparison validator, not observer inspection of results. Counts cover only observed unscoped requests, including limit-present requests and any cursor-present pages; no result, cursor or limit values are logged. It does not establish GUI usability, real-account support, inference or zero egress.'
                $report.observer.configReadEvidence = Get-DesktopConfigReadEvidence $allEvents -RequireLayers
                $report.observer.configReadEvidenceLimit = 'Counts cover actual global Desktop requests with and without layers and their correlated successes in the full observed snapshot, with at least one observed layer request required. They rely on the bridge-owned sessionFlags routing layer and fresh paired native requirements:null validator, not native config inspection or observer result bodies. Unsupported consumers fail honestly; this is not GUI usability, shared native configuration, account or service evidence.'
            }
        }
    }
    $report.status = 'observed'
    $report.stage = $(if ($Mode -eq 'baseline') { 'baseline-window-observed' } elseif ($Mode -eq 'observe') { 'instrumented-policy-trace-captured' } else { 'bridge-child-start-observed' })
    $report.reason = 'Verified package process and window observed. Direct mode requires the bridge child; observe mode requires the separately identified observer and validated metadata. Review screenshot and first failing exchange; no policy, login, model or production acceptance is claimed.'
    $exitCode = 0
} catch {
    if (-not $report.Contains('reason')) { $report.reason = $_.Exception.Message }
    $report.errorType = $_.Exception.GetType().FullName
    $report.errorHResult = ('0x{0:X8}' -f $_.Exception.HResult)
} finally {
    try {
        if ($subscription) { Unregister-Event -SourceIdentifier $eventSource -ErrorAction SilentlyContinue }
        Get-Event -SourceIdentifier $eventSource -ErrorAction SilentlyContinue | Remove-Event -ErrorAction SilentlyContinue
        if ($EnableReviewedLaunchCompatibility) {
            try {
                if ($null -eq $ownedActivationProcess -or [string]::IsNullOrWhiteSpace($ownedActivationCreatedUtc) -or
                    $activationPid -le 0 -or -not $report.Contains('registration') -or
                    -not $observed.ContainsKey([string]$activationPid) -or
                    $observed[[string]$activationPid].processId -ne $activationPid -or
                    -not $observed[[string]$activationPid].packageIdentityVerified) {
                    throw 'The Desktop activation identity is unavailable for cleanup.'
                }
                $activationExitConfirmed = Complete-DesktopActivationProcess -Process $ownedActivationProcess `
                    -ExpectedPid $activationPid -ObservedCreatedUtc $ownedActivationCreatedUtc `
                    -ExpectedPackage $report.registration.packageFullName `
                    -ReadPackageIdentity { param($ProcessId) [DesktopColdProbe.Native]::PackageFor([uint32]$ProcessId) }
            } finally {
                if ($null -ne $ownedActivationProcess) { $ownedActivationProcess.Dispose() }
            }
        } else {
            # Stop only the activation process, after rechecking package identity and creation time.
            if ($activationPid -gt 0 -and $report.Contains('registration') -and
                [DesktopColdProbe.Native]::PackageFor([uint32]$activationPid) -eq $report.registration.packageFullName) {
                $created = Get-Process -Id $activationPid -ErrorAction SilentlyContinue
                if ($created -and $observed.ContainsKey([string]$activationPid) -and
                    $created.StartTime.ToUniversalTime().ToString('o') -eq $observed[[string]$activationPid].createdUtc) {
                    [void]$created.CloseMainWindow()
                    if (-not $created.WaitForExit(5000)) { Stop-Process -Id $activationPid -ErrorAction SilentlyContinue }
                }
            }
        }
    } catch { $report.cleanupErrorType = $_.Exception.GetType().FullName }
    if ($EnableReviewedLaunchCompatibility) {
        $report.desktopActivationExited = [bool]$activationExitConfirmed
    }
}

# Same Windows job, after safe Desktop capture and exact activation-process exit.
# Native evidence stays separate: a later Desktop compatibility failure remains
# the original failure even when the isolated native slice can still be exercised.
if ($EnableReviewedLaunchCompatibility) {
    $desktopOutcome = [pscustomobject][ordered]@{
        status = $report.status; stage = $report.stage; reason = $report.reason; exitCode = $exitCode
    }
    $report.desktopOutcome = $desktopOutcome
    $nativeRunDecision = Get-DesktopNativeRunDecision -Reviewed $EnableReviewedLaunchCompatibility `
        -CaptureValidated $captureValidated -ActivationExited $activationExitConfirmed `
        -CleanupError $report.Contains('cleanupErrorType')
    $nativeStatus = 'not-run'
    $nativeNotRunReason = $nativeRunDecision.reason
    if ($nativeRunDecision.run) {
        $nativeFailureStage = 'wrapper'
        $nativeSummaryValidated = $false
        try {
            $nativeScratch = Join-Path $env:RUNNER_TEMP ('native-control-scratch-' + [guid]::NewGuid().ToString('N'))
            $nativeHelperHome = Join-Path $env:RUNNER_TEMP ('native-control-helper-' + [guid]::NewGuid().ToString('N'))
            New-Item -ItemType Directory -Path $nativeScratch, $nativeHelperHome | Out-Null
            . (Join-Path $env:GITHUB_WORKSPACE 'build/control-plane-native/invoke.ps1')
            $nativeEvidence = Invoke-ControlPlaneNative -HelperExecutable $NativeControlExecutable `
                -BridgeExecutable $BridgeExecutable -CodexExecutable $officialBackend.FullName `
                -CatalogPath $NativeControlCatalog -ScratchParent $nativeScratch -HelperHome $nativeHelperHome
            Assert-ControlPlaneNativeSummary $nativeEvidence
            $nativeSummaryValidated = $true
            if ($null -ne $nativeEvidence -and $nativeEvidence.PSObject.Properties['stage'] -and
                $nativeEvidence.stage -is [string] -and
                @('inputs', 'version', 'initialize', 'initial_config', 'catalog', 'resolved_config',
                    'threads', 'turns', 'ownership', 'final_config', 'shutdown', 'complete', 'supervisor') -ccontains $nativeEvidence.stage) {
                $nativeFailureStage = $nativeEvidence.stage
            }
            $report.nativeControlPlane = ConvertTo-DesktopNativeControlEvidence $nativeEvidence
            $nativeStatus = 'passed'
            $report.nativeControlPlaneLimit = 'The same compiled adapter and verified official native executable completed two projectless native threads with two text turns each against isolated loopback synthetic Responses fixtures. This is separate from actual Desktop evidence and cannot clear a Desktop failure. It is not a real-service, account, tool, corporate-policy, GUI-usability or zero-egress claim.'
        } catch {
            $nativeStatus = 'failed'
            if ($nativeSummaryValidated -and $nativeEvidence.status -ceq 'failed') {
                # Exact schema: bounded counts, fixed enums and the public pin.
                $report.nativeControlPlane = $nativeEvidence
            } else {
                $report.nativeControlPlane = [ordered]@{
                    schema = 1; scope = 'synthetic-loopback-only'; status = 'failed'
                    stage = $nativeFailureStage; reason = 'native_control_validation_failed'
                }
            }
        }
    } else {
        $report.nativeControlPlane = [ordered]@{
            schema = 1; scope = 'synthetic-loopback-only'; status = 'not-run'; reason = $nativeNotRunReason
        }
    }
    $combinedOutcome = Get-DesktopCombinedOutcome -DesktopOutcome $desktopOutcome `
        -NativeStatus $nativeStatus -NotRunReason $nativeNotRunReason
    $report.status = $combinedOutcome.status
    $report.stage = $combinedOutcome.stage
    $report.reason = $combinedOutcome.reason
    $exitCode = $combinedOutcome.exitCode
}
Save-Report
exit $exitCode

