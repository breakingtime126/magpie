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

# Only fixed, complete-read metadata leaves the worker. Unknown UI strings,
# runtime IDs, class names, and paths never appear in this observation.
function Get-OriginalConfigureObservation($Candidates) {
    $all=@($Candidates)
    $o=[ordered]@{total=$all.Count;enabledVisible=0;button=0;toggle=0;invoke=0;off=0;on=0;otherState=0;reason='missing'}
    foreach ($c in $all) {
        if ($c.enabled -and -not $c.offscreen) { $o.enabledVisible++ }
        if ($c.role -ceq 'ControlType.Button') { $o.button++ }
        if ($c.pattern -ceq 'Toggle') { $o.toggle++ }
        if ($c.pattern -ceq 'Invoke') { $o.invoke++ }
        if ($c.state -ceq 'Off') { $o.off++ }
        elseif ($c.state -ceq 'On') { $o.on++ }
        else { $o.otherState++ }
    }
    if ($all.Count -gt 1) { $o.reason='duplicate' }
    elseif ($all.Count -eq 1) {
        $c=$all[0]
        if ($c.name -cne 'Configure Codex launch selection' -or $c.role -cne 'ControlType.Button') { $o.reason='role' }
        elseif ($c.pattern -cne 'Toggle') { $o.reason='pattern' }
        elseif ($c.state -cne 'Off') { $o.reason='state' }
        elseif ($c.offscreen) { $o.reason='hidden' }
        elseif (-not $c.enabled) { $o.reason='disabled' }
        else { $o.reason='ready' }
    }
    return $o
}
function New-OriginalConfigureReadiness {
    return @{status='not-started';readAttempts=0;completedReads=0;lastCompleteObservation=$null}
}
# HWND creation does not establish that the original page has finished loading.
# Poll reads only, never an action. A wrong role/pattern/state or duplicate is not
# a loading assumption. Identity/read errors propagate without a retry.
function Wait-OriginalConfigureTarget($Readiness,[scriptblock]$ReadTargets,[scriptblock]$CheckOwner,
    [scriptblock]$ReadElapsed,[scriptblock]$Delay) {
    while ($true) {
        $Readiness.status='owner-check'; & $CheckOwner
        $Readiness.status='deadline';Assert-OriginalDeadline (& $ReadElapsed) 15000
        $Readiness.status='reading';$Readiness.readAttempts++
        $targets=@(& $ReadTargets)
        $Readiness.completedReads++
        $Readiness.lastCompleteObservation=Get-OriginalConfigureObservation $targets
        $Readiness.status='owner-check'; & $CheckOwner
        $Readiness.status='deadline';Assert-OriginalDeadline (& $ReadElapsed) 15000
        switch -CaseSensitive ($Readiness.lastCompleteObservation.reason) {
            'ready' {
                # Return the actual retained element, not a reconstructed selector.
                $target=Assert-OriginalTarget $targets 'Configure Codex launch selection' 'Toggle' 'Off'
                $Readiness.status='ready';return $target
            }
            { $_ -cin @('missing','disabled','hidden') } {
                $Readiness.status='waiting'; & $Delay 100
            }
            default { $Readiness.status='rejected';throw 'Configure target rejected.' }
        }
    }
}
function Assert-OriginalConfigureReadiness($Readiness) {
    Assert-OriginalFields $Readiness @('status','readAttempts','completedReads','lastCompleteObservation')
    if ($Readiness.status -cnotin @('unavailable','not-started','owner-check','deadline','reading','waiting','ready','rejected')) { throw 'Unknown readiness status.' }
    foreach ($field in @('readAttempts','completedReads')) {
        $n=$Readiness[$field]
        if (($n -isnot [int] -and $n -isnot [long]) -or $n -lt 0 -or $n -gt 151) { throw 'Invalid read count.' }
    }
    if ($Readiness.completedReads -gt $Readiness.readAttempts) { throw 'Invalid completed reads.' }
    $o=$Readiness.lastCompleteObservation
    if ($null -eq $o) {
        if ($Readiness.completedReads -ne 0) { throw 'Missing complete observation.' }
        return
    }
    Assert-OriginalFields $o @('total','enabledVisible','button','toggle','invoke','off','on','otherState','reason')
    if ($Readiness.completedReads -eq 0 -or $o.reason -cnotin @('missing','duplicate','role','pattern','state','hidden','disabled','ready')) { throw 'Invalid complete observation.' }
    foreach ($field in @('total','enabledVisible','button','toggle','invoke','off','on','otherState')) {
        $n=$o[$field]
        if (($n -isnot [int] -and $n -isnot [long]) -or $n -lt 0 -or $n -gt 2048 -or
            ($field -cne 'total' -and $n -gt $o.total)) { throw 'Invalid observation count.' }
    }
}

# These labels come from nativeconnect's fixed public errors, not provider text.
# A missing match proves nothing: a toast can be transient or a joined error.
function Get-OriginalPostActionLabels {
    return @('Configure Codex launch selection','Disconnect and restore Codex launch selection',
        'native Codex launch selection unavailable',
        'native Codex launch selection changed externally; nothing restored',
        'native Codex launch selection partially changed; use Disconnect to recover',
        'native Codex connection requires configured AIHub models',
        'native Codex connection requires configured AIHub models: current provider settings or saved models could not be read; check Providers',
        'native Codex connection requires configured AIHub models: supplier capability evidence is stale or changed',
        'native Codex connection requires configured AIHub models: the current supplier route no longer matches its prepared profile',
        'native Codex connection requires configured AIHub models: enable a direct AIHub provider, then select or fetch a model in Providers',
        'native Codex connection requires configured AIHub models: use a direct AIHub model route without cross-provider fallback or model redirection')
}
function Get-OriginalPostActionObservation($Candidates) {
    $o=[ordered]@{configureTotal=0;configureEnabledVisible=0;configureDisabledVisible=0;configureOff=0;
        restoreTotal=0;restoreEnabledVisible=0;restoreDisabledVisible=0;restoreOn=0;restoreInvoke=0;
        unavailableErrors=0;conflictErrors=0;uncertainErrors=0;catalogErrors=0}
    $labels=Get-OriginalPostActionLabels
    foreach ($c in @($Candidates)) {
        if ($c.name -cnotin $labels) { continue }
        if ($c.name -ceq $labels[0]) {
            $o.configureTotal++
            if (-not $c.offscreen) {
                if ($c.enabled) { $o.configureEnabledVisible++ } else { $o.configureDisabledVisible++ }
            }
            if ($c.role -ceq 'ControlType.Button' -and $c.pattern -ceq 'Toggle' -and $c.state -ceq 'Off') { $o.configureOff++ }
        } elseif ($c.name -ceq $labels[1]) {
            $o.restoreTotal++
            if (-not $c.offscreen) {
                if ($c.enabled) { $o.restoreEnabledVisible++ } else { $o.restoreDisabledVisible++ }
            }
            if ($c.role -ceq 'ControlType.Button' -and $c.pattern -ceq 'Toggle' -and $c.state -ceq 'On') { $o.restoreOn++ }
            if ($c.role -ceq 'ControlType.Button' -and $c.pattern -ceq 'Invoke') { $o.restoreInvoke++ }
        } elseif (-not $c.offscreen) {
            if ($c.name -ceq $labels[2]) { $o.unavailableErrors++ }
            elseif ($c.name -ceq $labels[3]) { $o.conflictErrors++ }
            elseif ($c.name -ceq $labels[4]) { $o.uncertainErrors++ }
            else { $o.catalogErrors++ }
        }
    }
    return $o
}
function New-OriginalPostActionReadback {
    return @{status='not-started';readAttempts=0;completedReads=0;firstComplete=$null;lastComplete=$null;firstKnownError=$null;
        sawDisabledRestore=$false;sawKnownError=$false}
}
function Update-OriginalPostActionReadback($Report,[scriptblock]$ReadTargets) {
    $Report.status='reading';$Report.readAttempts++
    try { $targets=@(& $ReadTargets) } catch { $Report.status='read-failed';throw }
    $o=Get-OriginalPostActionObservation $targets
    $Report.completedReads++
    if ($null -eq $Report.firstComplete) { $Report.firstComplete=$o }
    $Report.lastComplete=$o
    if ($o.restoreDisabledVisible -gt 0) { $Report.sawDisabledRestore=$true }
    if ($o.unavailableErrors+$o.conflictErrors+$o.uncertainErrors+$o.catalogErrors -gt 0) {
        $Report.sawKnownError=$true
        if ($null -eq $Report.firstKnownError) { $Report.firstKnownError=$o }
    }
    $Report.status='observed'
}
function Assert-OriginalPostActionReadback($Report) {
    Assert-OriginalFields $Report @('status','readAttempts','completedReads','firstComplete','lastComplete','firstKnownError','sawDisabledRestore','sawKnownError')
    if ($Report.status -cnotin @('unavailable','not-started','reading','read-failed','observed') -or
        $Report.sawDisabledRestore -isnot [bool] -or $Report.sawKnownError -isnot [bool]) { throw 'Invalid post-action metadata.' }
    foreach ($field in @('readAttempts','completedReads')) {
        $n=$Report[$field]
        if (($n -isnot [int] -and $n -isnot [long]) -or $n -lt 0 -or $n -gt 46) { throw 'Invalid post-action read count.' }
    }
    if ($Report.completedReads -gt $Report.readAttempts) { throw 'Invalid post-action completed count.' }
    foreach ($field in @('firstComplete','lastComplete','firstKnownError')) {
        $o=$Report[$field]
        if ($null -eq $o) {
            if ($field -cne 'firstKnownError' -and $Report.completedReads -ne 0) { throw 'Missing post-action observation.' }
            continue
        }
        if ($Report.completedReads -eq 0) { throw 'Unexpected post-action observation.' }
        Assert-OriginalFields $o @('configureTotal','configureEnabledVisible','configureDisabledVisible','configureOff',
            'restoreTotal','restoreEnabledVisible','restoreDisabledVisible','restoreOn','restoreInvoke',
            'unavailableErrors','conflictErrors','uncertainErrors','catalogErrors')
        foreach ($n in $o.Values) {
            if (($n -isnot [int] -and $n -isnot [long]) -or $n -lt 0 -or $n -gt 2048) { throw 'Invalid post-action observation count.' }
        }
        $hasError=($o.unavailableErrors+$o.conflictErrors+$o.uncertainErrors+$o.catalogErrors -gt 0)
        if ($field -ceq 'firstKnownError' -and -not $hasError) { throw 'First known error has no error count.' }
        if ($hasError -and -not $Report.sawKnownError) { throw 'Observed error contradicts history flag.' }
    }
    if ($Report.sawKnownError -ne ($null -ne $Report.firstKnownError)) { throw 'Known error retention mismatch.' }
}
