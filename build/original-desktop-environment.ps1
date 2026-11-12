# Hosted-runner launch identity is separate from isolated native and Magpie data.
# Imports do nothing; construction never activates Desktop or changes parent state.
function Get-OriginalSessionProfile {
    return [Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)
}
function New-OriginalDesktopStartInfo([string]$Executable,[string]$OwnedRun,[string]$OwnedHome) {
    Assert-OriginalDesktopPackageRunner
    $parent=[Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
    return New-OriginalDesktopEnvironment $parent (Get-OriginalSessionProfile) $Executable $OwnedRun $OwnedHome
}
function New-OriginalDesktopEnvironment([Collections.IDictionary]$Parent,[string]$SessionProfile,
    [string]$Executable,[string]$OwnedRun,[string]$OwnedHome) {
    Assert-OriginalDesktopPackageRunner
    foreach ($name in @('CODEX_HOME','CODEX_WINDOWS_REGISTERED_CORE','CODEX_SQLITE_HOME','CODEX_CLI_PATH','MAGPIE_APP_SERVER_BRIDGE_CONFIG')) {
        if ($Parent.Contains($name)) { throw 'Unexpected native override.' }
    }
    $profilePath=[string]$Parent['USERPROFILE']
    if ([string]::IsNullOrEmpty($profilePath) -or $profilePath -ine $SessionProfile) { throw 'Session identity mismatch.' }
    foreach ($path in @($SessionProfile,$OwnedRun,$OwnedHome)) {
        Assert-OriginalRegularPath $path
        if (-not [IO.Directory]::Exists($path)) { throw 'Fixture directory unavailable.' }
    }
    if ($Parent.Contains('HOME')) {
        $launchHome=[string]$Parent['HOME'];Assert-OriginalRegularPath $launchHome
        if ($launchHome -ine $profilePath) { throw 'Launch identity mismatch.' }
    }
    if ($OwnedHome -ieq $profilePath -or $OwnedHome.StartsWith($profilePath+[IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) {
        throw 'Fixture must be separate from session profile.'
    }
    # Only metadata: existing, linked or unreadable native state blocks the run.
    $ambientNative=[IO.Path]::Combine($profilePath,'.codex');Assert-OriginalRegularPath $ambientNative
    try { $null=[IO.File]::GetAttributes($ambientNative);throw 'Existing native state.' }
    catch [IO.FileNotFoundException] { }
    catch [IO.DirectoryNotFoundException] { }
    $nativeHome=[IO.Path]::Combine($OwnedHome,'.codex');Assert-OriginalRegularPath $nativeHome
    if (-not [IO.Directory]::Exists($nativeHome)) { throw 'Isolated native directory unavailable.' }
    $entries=[IO.Directory]::EnumerateFileSystemEntries($nativeHome).GetEnumerator()
    try { if ($entries.MoveNext()) { throw 'Isolated native state is not empty.' } }
    finally { if ($entries -is [IDisposable]) { $entries.Dispose() } }
    $start=[Diagnostics.ProcessStartInfo]::new();$start.FileName=$Executable;$start.Arguments='gui agents';$start.WorkingDirectory=$OwnedRun;$start.UseShellExecute=$false
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','ProgramFiles','ProgramFiles(x86)','CommonProgramFiles','ProgramData','HOME','USERPROFILE')) {
        if ($Parent.Contains($name)) { $start.EnvironmentVariables[$name]=[string]$Parent[$name] }
    }
    $start.EnvironmentVariables['PATH']=[IO.Path]::Combine([string]$Parent['SystemRoot'],'System32')
    foreach ($name in @('APPDATA','LOCALAPPDATA','TEMP','TMP','XDG_CONFIG_HOME','XDG_CACHE_HOME')) { $start.EnvironmentVariables[$name]=$OwnedHome }
    $start.EnvironmentVariables['CODEX_HOME']=$nativeHome
    foreach ($name in @('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY','http_proxy','https_proxy','all_proxy','no_proxy','CODEX_CA_CERTIFICATE','SSL_CERT_FILE','SSL_CERT_DIR','CURL_CA_BUNDLE','REQUESTS_CA_BUNDLE','NODE_EXTRA_CA_CERTS')) {
        if ($Parent.Contains($name)) { $start.EnvironmentVariables[$name]=[string]$Parent[$name] }
    }
    return $start
}
