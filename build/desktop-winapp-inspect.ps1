# Test-only official-tool observation. Dot-source in the disposable opt-in harness.
# The HWND is requested, not an exact root/PID exclusion proof. See the subsystem reference.
function Assert-WinAppOptions([bool]$Enabled, [string]$Mode, [bool]$Startup, [bool]$Compatibility,
    [bool]$Welcome, [string]$EntryCase, [string]$Marker) {
    if ($Enabled -and ($Mode -cne 'observe' -or -not $Startup -or -not $Compatibility -or
        $Welcome -or $EntryCase -or $Marker -cne 'winapp-inspection-v0.7.1')) {
        throw 'Read-only inspection requires its exclusive reviewed CI mode.'
    }
}
function Assert-WinAppSignature($Signature) {
    if ($null -eq $Signature -or [string]$Signature.Status -cne 'Valid' -or
        [string]$Signature.SignatureType -cne 'Authenticode' -or $null -eq $Signature.SignerCertificate -or
        $Signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
        throw 'Official tool embedded Microsoft signature unavailable.'
    }
}
function Assert-NativeUIOptions([bool]$Enabled,[string]$Mode,[bool]$Startup,[bool]$Compatibility,
    [bool]$Welcome,[bool]$BridgeInspection,[bool]$PackageOnly,[bool]$StaticLauncher,[string]$EntryCase,[string]$Marker) {
    if ($Enabled -and ($Mode -cne 'baseline' -or $Startup -or $Compatibility -or $Welcome -or
        $BridgeInspection -or $PackageOnly -or $StaticLauncher -or $EntryCase -or $Marker -cne 'native-ui-inspection-v1')) {
        throw 'Native UI observation requires its exclusive original-Desktop mode.'
    }
}
function Get-WinAppPins {
    return [ordered]@{
        version='0.7.1'; sourceCommit='e3cd0b94f3e7b776ac51ba0564f4908094e979b5'
        archiveSha256='d925d1e32cdc320b6d271f653fd2cd3c05b5d7558deb20d1b548bead77900fcf'
        archiveBytes=99013508
        archiveUrl='https://github.com/microsoft/winappCli/releases/download/v0.7.1/winappcli-x64.zip'
        licenseUrl='https://github.com/microsoft/winappCli/blob/e3cd0b94f3e7b776ac51ba0564f4908094e979b5/LICENSE'
    }
}
function Initialize-WinAppTool([string]$Directory) {
    # New private scratch only. No installer, PATH update, init, account or security setting.
    if (Test-Path -LiteralPath $Directory) { throw 'Tool scratch already exists.' }
    [void][IO.Directory]::CreateDirectory($Directory)
    $pins=Get-WinAppPins
    $archive=Join-Path $Directory 'winappcli-x64.zip'
    Invoke-WebRequest -Uri $pins.archiveUrl -OutFile $archive -TimeoutSec 90 -MaximumRetryCount 0
    if ((Get-Item -LiteralPath $archive).Length -ne $pins.archiveBytes -or
        (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pins.archiveSha256) {
        throw 'Official tool archive identity mismatch.'
    }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $bundle=Join-Path $Directory 'bundle'
    $zip=[IO.Compression.ZipFile]::OpenRead($archive)
    try {
        $total=0L; $seen=[Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
        if ($zip.Entries.Count -lt 1 -or $zip.Entries.Count -gt 2048) { throw 'Invalid tool archive count.' }
        foreach ($entry in $zip.Entries) {
            $name=$entry.FullName.Replace('\','/')
            $parts=$name.TrimEnd('/') -split '/'
            if (-not $name -or $name.StartsWith('/') -or $name.Contains(':') -or
                $parts -contains '..' -or $parts -contains '.' -or $parts -contains '' -or
                -not $seen.Add($name) -or $entry.Length -gt 536870912) { throw 'Invalid tool archive path or size.' }
            $total+=$entry.Length
            if ($total -gt 1073741824) { throw 'Tool archive expansion limit exceeded.' }
        }
        if (-not $seen.Contains('winapp.exe')) { throw 'Official tool root executable missing.' }
    } finally { $zip.Dispose() }
    [IO.Compression.ZipFile]::ExtractToDirectory($archive,$bundle)
    $executable=Join-Path $bundle 'winapp.exe'
    $signature=Get-AuthenticodeSignature -LiteralPath $executable
    Assert-WinAppSignature $signature
    $pinFiles=@()
    foreach ($file in @(Get-ChildItem -LiteralPath $bundle -File -Recurse)) {
        if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Tool reparse file rejected.' }
        $pinFiles += [pscustomobject]@{path=$file.FullName;hash=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash}
    }
    return [pscustomobject]@{
        Executable=$executable; Directory=$Directory; Files=$pinFiles
        Provenance=[ordered]@{
            version=$pins.version;sourceCommit=$pins.sourceCommit;archiveSha256=$pins.archiveSha256
            archiveBytes=$pins.archiveBytes;archiveUrl=$pins.archiveUrl;licenseUrl=$pins.licenseUrl
            executableSha256=(Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash.ToLowerInvariant()
            signatureStatus='Valid';signatureType='Authenticode';publisher='Microsoft Corporation'
            signerThumbprint=$signature.SignerCertificate.Thumbprint
        }
    }
}
function Assert-WinAppFiles($Tool) {
    foreach ($file in $Tool.Files) {
        $info=Get-Item -LiteralPath $file.path
        if ($info.PSIsContainer -or ($info.Attributes -band [IO.FileAttributes]::ReparsePoint) -or
            (Get-FileHash -LiteralPath $file.path -Algorithm SHA256).Hash -cne $file.hash) {
            throw 'Official tool files changed.'
        }
    }
    Assert-WinAppSignature (Get-AuthenticodeSignature -LiteralPath $Tool.Executable)
}
function New-WinAppStart($Tool,[long]$WindowHandle) {
    if ($WindowHandle -le 0) { throw 'Invalid requested window.' }
    $state=Join-Path $Tool.Directory 'coordination'
    $cache=Join-Path $Tool.Directory 'cache'
    if ((Test-Path -LiteralPath $state) -or (Test-Path -LiteralPath $cache)) { throw 'Tool state is not fresh.' }
    $start=New-LiveStart -Executable $Tool.Executable -Directory $Tool.Directory -Welcome -Arguments @(
        'ui','inspect','-w',$WindowHandle.ToString([Globalization.CultureInfo]::InvariantCulture),'--depth','8','--json')
    $start.Environment['WINAPP_UI_LOCK_DIRECTORY']=$state
    $start.Environment['WINAPP_CLI_CACHE_DIRECTORY']=$cache
    $start.Environment['WINAPP_CLI_TELEMETRY_OPTOUT']='1'
    # New-LiveStart cleared all inherited variables, including WINAPP_UI_WORKFLOW_ID.
    return $start
}
function Assert-WinAppFields($Value,[string[]]$Required,[string[]]$Optional=@()) {
    if ($Value -isnot [pscustomobject]) { throw 'Invalid inspection object.' }
    foreach ($key in $Required) { if (-not $Value.PSObject.Properties[$key]) { throw 'Missing inspection field.' } }
    foreach ($property in $Value.PSObject.Properties) {
        if (($Required+$Optional) -cnotcontains $property.Name) { throw 'Unknown inspection field.' }
    }
}
function ConvertTo-WinAppSummary([string]$Text,[long]$RequestedWindow) {
    # Duplicate detection, strict UTF-8 byte bound and parsing use the existing helper.
    $value=ConvertFrom-LiveJSON $Text 262144
    Assert-WinAppFields $value @('depth','interactive','hideDisabled','hideOffscreen','windows')
    Assert-LiveInteger $value.depth 8 8
    foreach ($key in @('interactive','hideDisabled','hideOffscreen')) {
        if ($value.$key -isnot [bool] -or $value.$key) { throw 'Unexpected inspection filter.' }
    }
    if ($value.windows -isnot [array] -or $value.windows.Count -ne 1) { throw 'Inspection window count unavailable.' }
    $win=$value.windows[0]
    Assert-WinAppFields $win @('hwnd','elementCount','elements','windowDpi','scale','dpiAwareness','coordinateSpace') @('title','className')
    Assert-LiveInteger $win.hwnd 1 ([long]::MaxValue)
    if ($win.hwnd -ne $RequestedWindow -or $win.coordinateSpace -cne 'physical-screen-pixels' -or
        @('unaware','system-aware','per-monitor-aware') -cnotcontains $win.dpiAwareness) { throw 'Inspection target metadata mismatch.' }
    Assert-LiveInteger $win.windowDpi 1 960
    if ($win.scale -isnot [double] -and $win.scale -isnot [long] -and $win.scale -isnot [int] -and $win.scale -isnot [decimal]) { throw 'Invalid inspection scale.' }
    if ([double]$win.scale -ne [double]$win.windowDpi/96.0) { throw 'Invalid inspection scale.' }
    Assert-LiveInteger $win.elementCount 1 2048
    if ($win.elements -isnot [array] -or $win.elements.Count -lt 1) { throw 'Inspection tree unavailable.' }
    $summary=[ordered]@{windowCount=1;nodeCount=0;truncatedNodeCount=0;maxRenderedDepth=0;
        headingExactCount=0;headingWhitespaceNormalizedCount=0;engineeringCount=0;suggestionsCount=0;continueCount=0;
        headingVisibleCount=0;engineeringVisibleCount=0;suggestionsVisibleCount=0;continueVisibleCount=0;
        engineeringInvokableCount=0;suggestionsInvokableCount=0;continueInvokableCount=0;loginMarkerCount=0;protectedMarkerCount=0;errorMarkerCount=0;
        requiredTargetStatus='missing';requiredTargetVisibility='missing-or-offscreen';
        loginAssessment='unknown-requires-visual-review'}
    $pending=[Collections.Generic.Stack[object]]::new()
    foreach ($node in $win.elements) { $pending.Push(@($node,0)) }
    $optional=@('name','automationId','className','selector','value','toggleState','expandState','scrollDir','children','hasMoreChildren')
    while ($pending.Count -gt 0) {
        $item=$pending.Pop();$node=$item[0];$depth=[int]$item[1]
        if ($depth -gt 8 -or $summary.nodeCount -ge 2048) { throw 'Inspection output tree bound exceeded.' }
        Assert-WinAppFields $node @('type','isEnabled','isOffscreen','isInvokable','x','y','width','height') $optional
        if ($node.type -isnot [string]) { throw 'Invalid inspection type.' }
        foreach ($key in @('isEnabled','isOffscreen','isInvokable')) {
            if ($node.$key -isnot [bool]) { throw 'Invalid inspection state.' }
        }
        foreach ($key in @('x','y','width','height')) {
            $number=$node.$key
            if (($number -isnot [double] -and $number -isnot [int] -and $number -isnot [long] -and $number -isnot [decimal]) -or
                [double]::IsNaN([double]$number) -or [double]::IsInfinity([double]$number)) { throw 'Invalid inspection geometry.' }
        }
        foreach ($key in @('automationId','className','selector','value','toggleState','expandState','scrollDir')) {
            if ($node.PSObject.Properties[$key] -and $node.$key -isnot [string]) { throw 'Invalid inspection string.' }
        }
        $summary.nodeCount++;$summary.maxRenderedDepth=[Math]::Max($summary.maxRenderedDepth,$depth)
        if ($node.PSObject.Properties['hasMoreChildren']) {
            if ($node.hasMoreChildren -isnot [bool]) { throw 'Invalid truncation state.' }
            if ($node.hasMoreChildren) { $summary.truncatedNodeCount++ }
        }
        $name=''
        if ($node.PSObject.Properties['name']) {
            if ($node.name -isnot [string]) { throw 'Invalid inspection name.' }
            $name=$node.name
        }
        if ($name -ceq 'Which best describes your work?') {
            $summary.headingExactCount++
            if (-not $node.isOffscreen) { $summary.headingVisibleCount++ }
        }
        if (($name -replace '\s+',' ').Trim() -ceq 'Which best describes your work?') { $summary.headingWhitespaceNormalizedCount++ }
        foreach ($spec in @(@('Engineering','engineering'),@('Suggest personalized tasks','suggestions'),@('Continue','continue'))) {
            if ($name -ceq $spec[0]) {
                $summary[$spec[1]+'Count']++
                if (-not $node.isOffscreen) { $summary[$spec[1]+'VisibleCount']++ }
                if ($node.isInvokable) { $summary[$spec[1]+'InvokableCount']++ }
            }
        }
        if ($name -match '(?i)\b(sign\s*in|log\s*in|password|verification code)\b') { $summary.loginMarkerCount++ }
        if ($name -match '(?i)\b(error|something went wrong|failed to|could not start|unable to start|unexpected failure)\b') { $summary.errorMarkerCount++ }
        if ($name -match '(?i)\b(sign\s*in|log\s*in|password|verification code|sign up|payment|subscribe|terms of|privacy policy|authorize|permission|connect your|allow access|access denied|administrator|security warning|certificate|captcha)\b') {
            $summary.protectedMarkerCount++
        }
        if ($node.PSObject.Properties['children']) {
            if ($node.children -isnot [array]) { throw 'Invalid inspection children.' }
            foreach ($child in $node.children) { $pending.Push(@($child,($depth+1))) }
        }
    }
    if ($summary.nodeCount -ne $win.elementCount) { throw 'Inspection node count mismatch.' }
    # Names may be omitted, incomplete, or from the CLI's fallback root. Zero
    # matched names cannot establish that the visible window has no login UI.
    if ($summary.loginMarkerCount -gt 0) { $summary.loginAssessment='login-marker-observed' }
    if ($summary.headingExactCount -gt 0 -and $summary.engineeringCount -gt 0 -and
        $summary.suggestionsCount -gt 0 -and $summary.continueCount -gt 0) { $summary.requiredTargetStatus='present' }
    if ($summary.headingVisibleCount -gt 0 -and $summary.engineeringVisibleCount -gt 0 -and
        $summary.suggestionsVisibleCount -gt 0 -and $summary.continueVisibleCount -gt 0) { $summary.requiredTargetVisibility='visible' }
    return $summary
}
function Complete-WinAppProcess($Child,[System.Collections.IDictionary]$Result) {
    try {
        if (-not $Child.Exited) { $Child.Stop();[void]$Child.Wait(5000) }
        $Result.processExitConfirmed=$Child.Exited
        if (-not $Result.processExitConfirmed) { $Result.status='stopped';$Result.reason='tool_exit_unconfirmed' }
    } catch { $Result.status='stopped';$Result.reason='tool_exit_unconfirmed' }
    try { $Child.Dispose() } catch { $Result.status='stopped';$Result.reason='tool_cleanup_unconfirmed' }
}
function Invoke-WinAppInspection($Start,[long]$WindowHandle,[scriptblock]$CheckIdentity,[switch]$NativeOriginal) {
    $result=[ordered]@{status='stopped';reason='identity_unavailable';actions=0;attempts=0;
        beforeIdentity=$false;afterIdentity=$false;exitCode=$null;stdoutBytes=0;summary=$null;processExitConfirmed=$null}
    $child=$null
    try {
        & $CheckIdentity; $result.beforeIdentity=$true
        # Constructor failure may follow Start. Report uncertainty and never retry.
        $result.reason='tool_start_uncertain';$result.attempts=1;$result.processExitConfirmed=$false
        $child=[DesktopLive.BoundedProcess]::new($Start,262144,30000)
        $result.reason='tool_timeout'
        if (-not $child.Wait(30000)) { $child.Stop();[void]$child.Wait(5000); return $result }
        $result.reason='tool_output_unclosed'
        $watch=[Diagnostics.Stopwatch]::StartNew()
        while (-not $child.Drained -and $watch.Elapsed.TotalSeconds -lt 2) { Start-Sleep -Milliseconds 20 }
        if ($child.TimedOut) { $result.reason='tool_timeout';return $result }
        if ($child.Overflow) { $result.reason='tool_output_limit';return $result }
        if (-not $child.Drained) { return $result }
        $result.exitCode=$child.ExitCode;$result.stdoutBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
        $result.reason='identity_changed'; & $CheckIdentity; $result.afterIdentity=$true
        if ($child.ExitCode -ne 0) { $result.reason='tool_exit_nonzero';return $result }
        $result.reason='tool_schema_invalid'
        $result.summary=ConvertTo-WinAppSummary $child.Output $WindowHandle
        if ($result.summary.loginMarkerCount -gt 0) { $result.reason='login_marker_observed';return $result }
        if ($result.summary.protectedMarkerCount -gt 0) { $result.reason='protected_marker_observed';return $result }
        if ($NativeOriginal -and $result.summary.errorMarkerCount -gt 0) { $result.reason='error_marker_observed';return $result }
        if (-not $NativeOriginal -and $result.summary.requiredTargetStatus -cne 'present') { $result.reason='required_targets_missing';return $result }
        if (-not $NativeOriginal -and $result.summary.requiredTargetVisibility -cne 'visible') { $result.reason='required_targets_offscreen';return $result }
        $result.status='observed'
        $result.reason=$(if ($NativeOriginal) { 'native_original_tree_observed' } else { 'read_only_target_labels_observed' })
    } catch {
        # Do not publish exceptions, raw output, values, paths or stderr.
    } finally {
        if ($null -ne $child) { Complete-WinAppProcess $child $result }
    }
    return $result
}
