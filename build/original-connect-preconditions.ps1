# Read-only post-sequence observations. Importing this file performs no action.
# No package query, native process, file-content read, write or recovery operation.
function ConvertTo-OriginalPredicate($Value) {
    if ($Value -isnot [bool]) { return 'unknown' }
    if ($Value) { return 'passed' }; return 'failed'
}
function New-OriginalPreconditionReport($Facts) {
    # Copy only fixed names and typed values, never paths, errors or caller text.
    $report=[ordered]@{schema=1;scope='read-only-original-connect-preconditions';status='snapshot';
        inspected=[ordered]@{};artifacts=[ordered]@{};
        notProven=@('same-environment-package-query','actual-cli-version-probe','runtime-identity-across-probe','embedded-launcher-pins','gateway-ownership','host-preparation','original-action-outcome');
        changesRecoveryDecision=$false;authorizesAction=$false}
    foreach ($name in @('preparedContextOpen','exactPackageName','exactPackageFamily','storeSignatureKind','packageStatusOk','expectedPackageVersion','retainedNativePathMatches','systemRootAbsolute','homeUserProfileAgree','homeMatchesLaunchIdentity','codexHomeMatchesFixture','unsupportedOverridesAbsent','nativeHomeSafe','magpieDataSafe','powershellFilePresent','originalExecutableName','siblingAdapterPresent')) {
        $report.inspected[$name]=ConvertTo-OriginalPredicate $Facts[$name]
    }
    foreach ($name in @('connectionDirectory','selectionJournal','runtimeProbeDirectory','generationDirectory','supplierCatalog','channelDescriptor','profileDirectory','bridgeProfile')) {
        $value=$Facts[$name]
        if ($value -isnot [string] -or $value -cnotin @('present','absent','unknown')) { $value='unknown' }
        $report.artifacts[$name]=$value
    }
    return $report
}
function Test-OriginalDiagnosticPath([string]$Path,[bool]$Directory) {
    # Local fixed drives only; bound ancestors and reject redirection before IO
    # reaches a descendant. Never inspect contents or traverse a reparse point.
    if ($Path -notmatch '^[A-Za-z]:\\' -or $Path.Length -gt 2048 -or [IO.Path]::GetFullPath($Path) -ine $Path) { return $false }
    $root=[IO.Path]::GetPathRoot($Path)
    if ([IO.DriveInfo]::new($root).DriveType -ne [IO.DriveType]::Fixed) { return $false }
    $parts=$Path.Substring($root.Length).Split([char]'\',[StringSplitOptions]::RemoveEmptyEntries)
    if ($parts.Count -gt 64) { return $false }
    $current=$root
    $attrs=[IO.File]::GetAttributes($root)
    if (($attrs -band [IO.FileAttributes]::ReparsePoint) -ne 0) { return $false }
    for ($i=0;$i -lt $parts.Count;$i++) {
        $current=[IO.Path]::Combine($current,$parts[$i]);$attrs=[IO.File]::GetAttributes($current)
        if (($attrs -band [IO.FileAttributes]::ReparsePoint) -ne 0) { return $false }
        if ($i -lt $parts.Count-1 -and ($attrs -band [IO.FileAttributes]::Directory) -eq 0) { return $false }
    }
    return ((($attrs -band [IO.FileAttributes]::Directory) -ne 0) -eq $Directory)
}
function Get-OriginalDiagnosticPresence([string]$Path,[bool]$Directory) {
    try {
        if (Test-OriginalDiagnosticPath $Path $Directory) { return 'present' }
        return 'unknown'
    } catch [IO.FileNotFoundException] { return 'absent' }
      catch [IO.DirectoryNotFoundException] { return 'absent' }
      catch { return 'unknown' }
}
function Get-OriginalDiagnosticEntries([string]$Directory) {
    if (-not (Test-OriginalDiagnosticPath $Directory $true)) { throw 'Diagnostic directory unavailable.' }
    $entries=[Collections.Generic.List[string]]::new()
    $iterator=[IO.Directory]::EnumerateFileSystemEntries($Directory).GetEnumerator()
    try {
        while ($iterator.MoveNext()) {
            if ($entries.Count -ge 128) { throw 'Diagnostic enumeration bound.' }
            $entries.Add([string]$iterator.Current)
        }
    } finally { if ($iterator -is [IDisposable]) { $iterator.Dispose() } }
    return ,$entries
}
function Get-OriginalPreparedRegistrationFacts($PackageContext) {
    $facts=@{}
        if ($null -eq $PackageContext -or $PackageContext.Prepared -ne $true -or $PackageContext.Closed -ne $false) { throw 'Prepared context unavailable.' }
        $facts.preparedContextOpen=$true
        $app=$PackageContext.App
        if ($null -eq $app) { throw 'Registration metadata unavailable.' }
        foreach ($name in @('Name','PackageFamilyName','InstallLocation')) {
            $value=$app.$name
            if ($value -isnot [string] -or [string]::IsNullOrWhiteSpace($value)) { throw 'Registration metadata unavailable.' }
        }
        foreach ($name in @('SignatureKind','Status')) {
            $value=$app.$name
            if (($value -isnot [string] -and $value -isnot [Enum]) -or [string]::IsNullOrWhiteSpace([string]$value)) { throw 'Registration metadata unavailable.' }
        }
        if (($app.Version -isnot [string] -and $app.Version -isnot [Version]) -or [string]::IsNullOrWhiteSpace([string]$app.Version) -or
            [IO.Path]::IsPathRooted($app.InstallLocation) -ne $true) { throw 'Registration metadata unavailable.' }
        if ($PackageContext.NativeExecutable -isnot [string] -or [string]::IsNullOrWhiteSpace($PackageContext.NativeExecutable)) { throw 'Retained native metadata unavailable.' }
        # These are historical prepared-context metadata, not another query or
        # proof that the original process's PowerShell discovery succeeded.
        $facts.exactPackageName=([string]$app.Name -ceq 'OpenAI.Codex')
        $facts.exactPackageFamily=([string]$app.PackageFamilyName -ceq 'OpenAI.Codex_2p2nqsd0c76g0')
        $facts.storeSignatureKind=([string]$app.SignatureKind -ceq 'Store')
        $facts.packageStatusOk=([string]$app.Status -ceq 'Ok')
        $facts.expectedPackageVersion=([string]$app.Version -ceq '26.1103.4239.0')
        $facts.retainedNativePathMatches=([string]$PackageContext.NativeExecutable -ieq [IO.Path]::Combine([string]$app.InstallLocation,'app\resources\codex.exe'))
    return $facts
}
function Get-OriginalConnectPreconditions {
    [CmdletBinding()]
    param($PackageContext,[Diagnostics.ProcessStartInfo]$OriginalStartInfo,[string]$OwnedRun,[string]$OwnedHome,[string]$LaunchIdentityHome,
        $Sequence,[switch]$RecoveryChecksCompleted,[switch]$SequenceReportSaved)
    # Both attestations belong to the caller's already-completed sequence. This
    # observer never runs recovery checks itself or updates their result.
    if (-not $RecoveryChecksCompleted -or -not $SequenceReportSaved -or $Sequence -isnot [Collections.IDictionary] -or
        $Sequence['cleanupAllowed'] -isnot [bool] -or $Sequence['preserveRecovery'] -isnot [bool]) {
        $r=New-OriginalPreconditionReport @{};$r.status='sequence-evidence-required';return $r
    }
    $facts=@{}
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        $r=New-OriginalPreconditionReport $facts;$r.status='unsupported-platform';return $r
    }
    try {
        $facts=Get-OriginalPreparedRegistrationFacts $PackageContext
        $envs=$OriginalStartInfo.EnvironmentVariables
        $system=[string]$envs['SystemRoot'];$launchHome=[string]$envs['HOME'];$userProfilePath=[string]$envs['USERPROFILE'];$codex=[string]$envs['CODEX_HOME']
        $facts.systemRootAbsolute=($system -match '^[A-Za-z]:\\' -and [IO.Path]::GetFullPath($system) -ieq $system)
        $facts.homeUserProfileAgree=((-not $envs.ContainsKey('HOME') -or $launchHome -ieq $userProfilePath) -and $userProfilePath -ine '')
        $facts.homeMatchesLaunchIdentity=($facts.homeUserProfileAgree -and $userProfilePath -ieq $LaunchIdentityHome -and $LaunchIdentityHome -ine $OwnedHome -and $LaunchIdentityHome -match '^[A-Za-z]:\\' -and [IO.Path]::GetFullPath($LaunchIdentityHome) -ieq $LaunchIdentityHome)
        $facts.codexHomeMatchesFixture=($codex -ieq [IO.Path]::Combine($OwnedHome,'.codex'))
        $facts.unsupportedOverridesAbsent=$true
        foreach ($name in @('CODEX_WINDOWS_REGISTERED_CORE','CODEX_SQLITE_HOME','CODEX_CLI_PATH','MAGPIE_APP_SERVER_BRIDGE_CONFIG')) {
            if ($envs.ContainsKey($name)) { $facts.unsupportedOverridesAbsent=$false }
        }
        # Reject unexpected arguments before using supplied paths for metadata.
        if (-not $facts.homeMatchesLaunchIdentity -or -not $facts.codexHomeMatchesFixture -or
            -not (Test-OriginalDiagnosticPath $OwnedRun $true) -or -not (Test-OriginalDiagnosticPath $OwnedHome $true)) { throw 'Owned fixture paths unavailable.' }
        $data=[IO.Path]::Combine($OwnedRun,'data');$connection=[IO.Path]::Combine($data,'native-codex-connection')
        $facts.nativeHomeSafe=Test-OriginalDiagnosticPath $codex $true
        $facts.magpieDataSafe=Test-OriginalDiagnosticPath $data $true
        $facts.originalExecutableName=($OriginalStartInfo.FileName -ceq [IO.Path]::Combine($OwnedRun,'Magpie-Native-Codex-Experimental.exe'))
        $presence=Get-OriginalDiagnosticPresence ([IO.Path]::Combine($OwnedRun,'magpie-codex-bridge.exe')) $false
        if ($presence -ceq 'unknown') { throw 'Adapter metadata unavailable.' };$facts.siblingAdapterPresent=($presence -ceq 'present')
        if ($facts.systemRootAbsolute) {
            $presence=Get-OriginalDiagnosticPresence ([IO.Path]::Combine($system,'System32\WindowsPowerShell\v1.0\powershell.exe')) $false
            if ($presence -ceq 'unknown') { throw 'Discovery executable metadata unavailable.' };$facts.powershellFilePresent=($presence -ceq 'present')
        }
        $facts.connectionDirectory=Get-OriginalDiagnosticPresence $connection $true
        $facts.selectionJournal=Get-OriginalDiagnosticPresence ([IO.Path]::Combine($connection,'selection.json')) $false
        if ($facts.selectionJournal -ceq 'unknown') { throw 'Journal metadata unavailable.' }
        foreach ($name in @('runtimeProbeDirectory','generationDirectory','supplierCatalog','channelDescriptor','profileDirectory','bridgeProfile')) { $facts[$name]='absent' }
        foreach ($entry in (Get-OriginalDiagnosticEntries $data)) {
            if ([IO.Path]::GetFileName($entry) -cmatch '^runtime-version-[0-9]+$') {
                $presence=Get-OriginalDiagnosticPresence $entry $true
                if ($presence -ceq 'unknown') { throw 'Probe metadata unavailable.' };if ($presence -ceq 'present') { $facts.runtimeProbeDirectory='present' }
            }
        }
        if ($facts.connectionDirectory -ceq 'present') {
            $generationCount=0
            foreach ($entry in (Get-OriginalDiagnosticEntries $connection)) {
                $name=[IO.Path]::GetFileName($entry)
                if ($name -cmatch '^generation-[0-9]+$') {
                    $generationCount++;if ($generationCount -gt 8) { throw 'Generation enumeration bound.' }
                    if ((Get-OriginalDiagnosticPresence $entry $true) -cne 'present') { throw 'Generation metadata unavailable.' }
                    $facts.generationDirectory='present'
                    $presence=Get-OriginalDiagnosticPresence ([IO.Path]::Combine($entry,'supplier-catalog.json')) $false
                    if ($presence -ceq 'unknown') { throw 'Catalog metadata unavailable.' };if ($presence -ceq 'present') { $facts.supplierCatalog='present' }
                    foreach ($child in (Get-OriginalDiagnosticEntries $entry)) {
                        if ([IO.Path]::GetFileName($child) -cmatch '^channel-[0-9a-f]{32}\.json$') {
                            $presence=Get-OriginalDiagnosticPresence $child $false
                            if ($presence -ceq 'unknown') { throw 'Channel metadata unavailable.' };if ($presence -ceq 'present') { $facts.channelDescriptor='present' }
                        }
                    }
                } elseif ($name -cmatch '^profile-[0-9]+$') {
                    if ((Get-OriginalDiagnosticPresence $entry $true) -cne 'present') { throw 'Profile metadata unavailable.' }
                    $facts.profileDirectory='present'
                    $presence=Get-OriginalDiagnosticPresence ([IO.Path]::Combine($entry,'bridge.json')) $false
                    if ($presence -ceq 'unknown') { throw 'Profile metadata unavailable.' };if ($presence -ceq 'present') { $facts.bridgeProfile='present' }
                }
            }
        } elseif ($facts.connectionDirectory -cne 'absent') { throw 'Connection metadata unavailable.' }
        return New-OriginalPreconditionReport $facts
    } catch {
        # Enumeration/read failures invalidate aggregate absence claims. Preserve
        # already established predicate facts, never serialize exception text.
        foreach ($name in @('runtimeProbeDirectory','generationDirectory','supplierCatalog','channelDescriptor','profileDirectory','bridgeProfile')) { $facts[$name]='unknown' }
        $r=New-OriginalPreconditionReport $facts;$r.status='unavailable';return $r
    }
}
