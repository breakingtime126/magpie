# Compile only this repository's diagnostic helper, before any cleared child.
# Run in a disposable GitHub Windows job, not a user's desktop.
$ErrorActionPreference='Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT -or
    $PSVersionTable.PSEdition -ne 'Desktop' -or
    $env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
    $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035') {
    throw 'Capture helper build requires the disposable Windows fixture.'
}
. ([IO.Path]::Combine($PSScriptRoot,'original-desktop-window-capture.ps1'))
$source=[IO.Path]::Combine($PSScriptRoot,'original-desktop-window-capture-native.cs')
$destination=[IO.Path]::Combine($PSScriptRoot,'original-desktop-window-capture-native.dll')
foreach ($path in @($source,$destination,$env:RUNNER_TEMP)) { Assert-OriginalCaptureRegularPath $path }
if (-not [IO.File]::Exists($source) -or [IO.File]::Exists($destination)) { throw 'Capture helper build input or output unavailable.' }
$runtimeDirectory=[Runtime.InteropServices.RuntimeEnvironment]::GetRuntimeDirectory()
$compiler=[IO.Path]::Combine($runtimeDirectory,'csc.exe')
Assert-OriginalCaptureRegularPath $compiler
if (-not [IO.File]::Exists($compiler)) { throw 'Framework compiler unavailable.' }
$staging=[IO.Path]::Combine($env:RUNNER_TEMP,('capture-native-build-'+[guid]::NewGuid().ToString('N')))
Assert-OriginalCaptureRegularPath $staging
[void][IO.Directory]::CreateDirectory($staging)
try {
    $assembly=[IO.Path]::Combine($staging,'original-desktop-window-capture-native.dll')
    $arguments=@('/nologo','/target:library','/optimize+','/platform:anycpu','/noconfig','/nostdlib+',
        ('/reference:'+([IO.Path]::Combine($runtimeDirectory,'mscorlib.dll'))),
        ('/reference:'+([IO.Path]::Combine($runtimeDirectory,'System.dll'))),
        ('/reference:'+([IO.Path]::Combine($runtimeDirectory,'System.Drawing.dll'))),
        ('/out:'+$assembly),$source)
    & $compiler @arguments
    if ($LASTEXITCODE -ne 0 -or -not [IO.File]::Exists($assembly)) { throw 'Capture helper compilation failed.' }
    Assert-OriginalCaptureRegularPath $assembly
    $length=([IO.FileInfo]$assembly).Length
    if ($length -le 0 -or $length -gt 1048576 -or
        [Reflection.AssemblyName]::GetAssemblyName($assembly).Name -cne 'original-desktop-window-capture-native') {
        throw 'Capture helper output invalid.'
    }
    # Move rejects an existing destination; no stale output is reused or replaced.
    [IO.File]::Move($assembly,$destination)
    'CAPTURE_NATIVE_HELPER_BUILT'
} finally {
    if ([IO.Directory]::Exists($staging)) { [IO.Directory]::Delete($staging,$true) }
}
