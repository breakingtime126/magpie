# Read-only assertions and a one-shot UI-action controller. No registry writer,
# backend call, Desktop launch or cleanup is provided by this file.
function Assert-OriginalActionContext {
    if ($PSVersionTable.PSEdition -cne 'Core' -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:GITHUB_EVENT_NAME -cne 'push' -or $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_CONNECT_ACTION -cne 'original-ui-v1') { throw 'Action context unavailable.' }
}
function Assert-OriginalRegularPath([string]$Path) {
    if (-not [IO.Path]::IsPathRooted($Path) -or [IO.Path]::GetFullPath($Path) -cne $Path) { throw 'Absolute owned path required.' }
    $cursor=[IO.Path]::GetFullPath($Path)
    while ($cursor) {
        try { $attributes=[IO.File]::GetAttributes($cursor) }
        catch [IO.FileNotFoundException] { $cursor=[IO.Path]::GetDirectoryName($cursor);continue }
        catch [IO.DirectoryNotFoundException] { $cursor=[IO.Path]::GetDirectoryName($cursor);continue }
        if (($attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Linked path refused.' }
        $cursor=[IO.Path]::GetDirectoryName($cursor)
    }
}
function Read-OriginalJSON([string]$Path,[int]$Maximum=131072) {
    Assert-OriginalRegularPath $Path
    $stream=[IO.File]::Open($Path,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
    try {
        if ($stream.Length -le 0 -or $stream.Length -gt $Maximum) { throw 'JSON size invalid.' }
        $bytes=[byte[]]::new([int]$stream.Length);$stream.ReadExactly($bytes)
    } finally { $stream.Dispose() }
    $text=[Text.UTF8Encoding]::new($false,$true).GetString($bytes)
    $doc=[Text.Json.JsonDocument]::Parse($text)
    try {
        function Assert-UniqueJSON($node) {
            if ($node.ValueKind -eq [Text.Json.JsonValueKind]::Object) {
                $seen=[Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
                foreach ($property in $node.EnumerateObject()) {
                    if (-not $seen.Add($property.Name)) { throw 'Duplicate JSON field.' }
                    Assert-UniqueJSON $property.Value
                }
            } elseif ($node.ValueKind -eq [Text.Json.JsonValueKind]::Array) {
                foreach ($item in $node.EnumerateArray()) { Assert-UniqueJSON $item }
            }
        }
        Assert-UniqueJSON $doc.RootElement
    } finally { $doc.Dispose() }
    return ConvertFrom-Json -InputObject $text -AsHashtable -Depth 20
}
function Assert-OriginalFields($Object,[string[]]$Names) {
    if ($Object -isnot [Collections.IDictionary] -or $Object.Count -ne $Names.Count) { throw 'Unexpected object.' }
    foreach ($name in $Names) { if ($name -cnotin @($Object.Keys)) { throw 'Missing field.' } }
}
function Assert-OriginalSelection($Selection) {
    Assert-OriginalFields $Selection @('cli','profile')
    foreach ($name in @('cli','profile')) {
        $v=$Selection[$name];Assert-OriginalFields $v @('present','kind','text')
        if ($v.present -isnot [bool] -or $v.text -isnot [string] -or
            ($v.kind -isnot [int] -and $v.kind -isnot [long]) -or
            $v.text.Length -gt 32767 -or $v.text.Contains([char]0)) { throw 'Invalid registry value.' }
        if ($v.present) { if ($v.kind -notin @(1,2)) { throw 'Unsupported registry type.' } }
        elseif ($v.kind -ne 0 -or $v.text -cne '') { throw 'Invalid absent value.' }
    }
}
function Test-OriginalSelectionEqual($Left,$Right) {
    Assert-OriginalSelection $Left;Assert-OriginalSelection $Right
    foreach ($name in @('cli','profile')) {
        foreach ($field in @('present','kind','text')) {
            if ($Left[$name][$field] -cne $Right[$name][$field]) { return $false }
        }
    }
    return $true
}
function Read-OriginalSelection {
    $key=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment',$false)
    try {
        $result=@{};$names=if ($null -eq $key) { @() } else { @($key.GetValueNames()) }
        foreach ($pair in @(@('cli','CODEX_CLI_PATH'),@('profile','MAGPIE_APP_SERVER_BRIDGE_CONFIG'))) {
            $v=@{present=$false;kind=0;text=''}
            if ($pair[1] -iin $names) {
                $v=@{present=$true;kind=[int]$key.GetValueKind($pair[1]);
                    text=$key.GetValue($pair[1],$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)}
            }
            $result[$pair[0]]=$v
        }
        Assert-OriginalSelection $result;return $result
    } finally { if ($null -ne $key) { $key.Dispose() } }
}
function Assert-OriginalJournal($Journal,$Before,[string]$Launcher,[string]$JournalPath) {
    Assert-OriginalFields $Journal @('version','before','installed','profileSHA256')
    if ($Journal.version -ne 1 -or ($Journal.version -isnot [int] -and $Journal.version -isnot [long]) -or
        $Journal.profileSHA256 -isnot [string] -or $Journal.profileSHA256 -cnotmatch '^[0-9a-f]{64}$') { throw 'Invalid journal.' }
    if (-not (Test-OriginalSelectionEqual $Journal.before $Before)) { throw 'Original values changed.' }
    Assert-OriginalSelection $Journal.installed
    if (-not $Journal.installed.cli.present -or $Journal.installed.cli.kind -ne 1 -or
        $Journal.installed.cli.text -cne $Launcher -or -not $Journal.installed.profile.present -or
        $Journal.installed.profile.kind -ne 1) { throw 'Installed paths invalid.' }
    $profile=$Journal.installed.profile.text
    if (-not [IO.Path]::IsPathFullyQualified($profile) -or
        [IO.Path]::GetFileName($profile) -cne 'bridge.json' -or
        [IO.Path]::GetFileName([IO.Path]::GetDirectoryName($profile)) -cnotmatch '^profile-[^\\/]+$' -or
        [IO.Path]::GetDirectoryName([IO.Path]::GetDirectoryName($profile)) -cne [IO.Path]::GetDirectoryName($JournalPath)) {
        throw 'Profile outside owned generation.'
    }
}
function Assert-OriginalRecoverySelection($Current,$Journal) {
    Assert-OriginalSelection $Current
    foreach ($name in @('cli','profile')) {
        $a=@{cli=$Current[$name];profile=$Current[$name]}
        $b=@{cli=$Journal.before[$name];profile=$Journal.before[$name]}
        $c=@{cli=$Journal.installed[$name];profile=$Journal.installed[$name]}
        if (-not (Test-OriginalSelectionEqual $a $b) -and -not (Test-OriginalSelectionEqual $a $c)) { throw 'External selection change.' }
    }
}
function Assert-OriginalTarget($Candidates,[string]$Label,[string]$Pattern,[string]$State) {
    $all=@($Candidates)
    if ($all.Count -ne 1) { throw 'Target is not unique.' }
    $c=$all[0]
    if ($c.name -cne $Label -or -not $c.enabled -or $c.offscreen -or $c.role -cne 'ControlType.Button' -or
        $c.pattern -cne $Pattern -or $c.state -cne $State) { throw 'Target is not actionable.' }
    return $c
}
# Mark attempted before entering UIA, including a call which throws or hangs.
# No catch/retry exists here. Each process mode is additionally claimed on disk.
function Invoke-OriginalOnce($Ledger,[string]$Action,[scriptblock]$Check,[scriptblock]$Mark,[scriptblock]$Act) {
    if ($Ledger.ContainsKey($Action)) { throw 'Action was already attempted.' }
    & $Check
    $Ledger[$Action]='uncertain'
    & $Mark $Action
    & $Act
    & $Check
    $Ledger[$Action]='returned'
}
function Assert-OriginalDeadline([long]$Elapsed,[long]$Limit=45000) {
    if ($Elapsed -lt 0 -or $Elapsed -ge $Limit) { throw 'Action deadline.' }
}
function Assert-OriginalOwnerIdentity($Expected,$Actual) {
    foreach ($field in @('pid','ticks','window','executable','title')) {
        if ($Expected[$field] -cne $Actual[$field]) { throw 'Owned identity changed.' }
    }
    if ($Actual.exited -ne $false) { throw 'Owned process exited.' }
}
function Write-OriginalExclusive([string]$Path,[string]$Text) {
    Assert-OriginalRegularPath $Path
    $stream=[IO.File]::Open($Path,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::Read)
    try { $bytes=[Text.UTF8Encoding]::new($false).GetBytes($Text);$stream.Write($bytes,0,$bytes.Length);$stream.Flush($true) }
    finally { $stream.Dispose() }
}
function Assert-OriginalRestored([bool]$JournalPresent,$Current,$Before) {
    if ($JournalPresent -or -not (Test-OriginalSelectionEqual $Current $Before)) { throw 'Restoration not verified.' }
}

# Post-action reads may observe the original page's disabled in-flight button.
# Waiting for completion is read-only; action admission remains strict above.
function Test-OriginalTargetReady($Candidates,[string]$Label,[string]$Pattern,[string]$State) {
    $all=@($Candidates)
    if ($all.Count -gt 1) { throw 'Completion target is ambiguous.' }
    if ($all.Count -eq 0) { return $false }
    $c=$all[0]
    return $c.name -ceq $Label -and $c.enabled -and -not $c.offscreen -and
        $c.role -ceq 'ControlType.Button' -and $c.pattern -ceq $Pattern -and $c.state -ceq $State
}
