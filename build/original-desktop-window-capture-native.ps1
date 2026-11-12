# Shared native primitives loaded from this checkout's CI-built helper.
# No compilation, target lookup, window operation or fallback occurs here.
function Initialize-OriginalCaptureNative {
    $assemblyPath=[IO.Path]::Combine($PSScriptRoot,'original-desktop-window-capture-native.dll')
    Assert-OriginalCaptureRegularPath $assemblyPath
    if (-not [IO.File]::Exists($assemblyPath)) { throw 'Capture helper unavailable.' }
    $type='OriginalCapture.Native' -as [type]
    if ($null -ne $type) {
        if ($type.Assembly.Location -ine $assemblyPath) { throw 'Capture helper origin mismatch.' }
        return
    }
    $assembly=[Reflection.Assembly]::LoadFrom($assemblyPath)
    $type=$assembly.GetType('OriginalCapture.Native',$false)
    if ($null -eq $type -or $type.Assembly.Location -ine $assemblyPath) { throw 'Capture helper type unavailable.' }
}
