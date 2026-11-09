# Build the upstream main/GUI, using its existing portable-data contract.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
function Assert-Exit([string]$Name) { if ($LASTEXITCODE -ne 0) { throw "$Name failed." } }
if ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_OS -cne 'Windows' -or
    $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035') { throw 'Scoped temporary CI required.' }
$head = (& git rev-parse HEAD).Trim(); Assert-Exit 'Read source'
if ($head -cne $env:GITHUB_SHA) { throw 'Source mismatch.' }
$package = Join-Path $env:RUNNER_TEMP 'native-magpie-package'
$evidence = Join-Path $env:RUNNER_TEMP 'native-magpie-evidence'
[void](New-Item -ItemType Directory -Path $package -ErrorAction Stop)
[void](New-Item -ItemType Directory -Path $evidence -ErrorAction Stop)
& go vet -tags nogui,nativecodex ./internal/gui ./internal/agent ./internal/library ./internal/codexsetup
Assert-Exit 'Windows native-mode vet'
& go vet -tags production,nativecodex ./internal/gui
Assert-Exit 'Windows original GUI vet'
& go test -tags nogui,nativecodex -count=1 -timeout=5m -run '^TestNativeCodex' ./internal/agent ./internal/library ./internal/gui
Assert-Exit 'Synthetic native-mode write protection'
& go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json --arch amd64 --out rsrc --file-version '0.0.0' --product-version '0.0.0'
Assert-Exit 'Original Windows resources'
$exe = Join-Path $package 'Magpie-Native-Codex-Experimental.exe'
# No comlauncher installer; the existing GUI/main remain the product entry.
& go build -tags production,nativecodex -trimpath -ldflags "-s -w -H windowsgui -X main.version=native-codex-$($head.Substring(0,8))" -o $exe .
Assert-Exit 'Original Magpie GUI build'
Copy-Item -LiteralPath 'LICENSE','THIRD_PARTY_NOTICES' -Destination $package
Copy-Item -LiteralPath 'docs/subsystems/original-gui-native-codex.md' -Destination (Join-Path $package 'README.md')
[IO.File]::WriteAllText((Join-Path $package '.portable'), '', [Text.UTF8Encoding]::new($false))
[void](New-Item -ItemType Directory -Path (Join-Path $package 'data'))
# Native Magpie setting, not a fake account state; explicit logins remain actions.
[IO.File]::WriteAllText((Join-Path $package 'data/settings.json'), '{"noAccountDiscovery":true,"lang":"zh","theme":"light","guideHidden":true}', [Text.UTF8Encoding]::new($false))
@{sourceCommit=$head;guiSHA256=(Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant();unsigned=$true;originalGUI=$true;buildTags='production,nativecodex';portable=$true} |
 ConvertTo-Json | Set-Content (Join-Path $package 'BUILD.json') -Encoding UTF8
# Launch a copy, never the deliverable data directory. Only synthetic home files.
$root = Join-Path $env:RUNNER_TEMP ('native-magpie-window-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $root)
$homeRoot = Join-Path $root 'home'; [void](New-Item -ItemType Directory -Path $homeRoot)
$run = Join-Path $root 'app'; Copy-Item -LiteralPath $package -Destination $run -Recurse
$codexHome = Join-Path $homeRoot '.codex'; [void](New-Item -ItemType Directory -Path $codexHome)
$fixture = Join-Path $codexHome 'config.toml'
[IO.File]::WriteAllText($fixture, "model = `"native-gui-fixture`"`n", [Text.UTF8Encoding]::new($false))
$before = (Get-FileHash $fixture -Algorithm SHA256).Hash
$parentEnv = [Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
foreach ($key in $parentEnv.Keys) {
 if ([string]::Equals([string]$key,'CODEX_WINDOWS_REGISTERED_CORE',[StringComparison]::OrdinalIgnoreCase)) { throw 'Registered Core context is outside this test.' }
}
$psi = [Diagnostics.ProcessStartInfo]::new()
$psi.FileName = Join-Path $run 'Magpie-Native-Codex-Experimental.exe'
$psi.Arguments = 'gui agents'; $psi.WorkingDirectory = $run; $psi.UseShellExecute = $false
$psi.EnvironmentVariables.Clear()
foreach ($key in @('SystemRoot','WINDIR','ProgramFiles','ProgramFiles(x86)','CommonProgramFiles','ProgramData')) {
 if ($parentEnv.Contains($key)) { $psi.EnvironmentVariables[$key] = [string]$parentEnv[$key] }
}
$psi.EnvironmentVariables['PATH'] = Join-Path $env:SystemRoot 'System32'
foreach ($key in @('HOME','USERPROFILE','APPDATA','LOCALAPPDATA','TEMP','TMP','XDG_CONFIG_HOME','XDG_CACHE_HOME')) { $psi.EnvironmentVariables[$key] = $homeRoot }
$psi.EnvironmentVariables['CODEX_HOME'] = $codexHome
foreach ($key in @('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY','http_proxy','https_proxy','all_proxy','no_proxy','CODEX_CA_CERTIFICATE','SSL_CERT_FILE','SSL_CERT_DIR','CURL_CA_BUNDLE','REQUESTS_CA_BUNDLE','NODE_EXTRA_CA_CERTS')) {
 if ($parentEnv.Contains($key)) { $psi.EnvironmentVariables[$key] = [string]$parentEnv[$key] }
}
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class NativeMagpieCapture {
 [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
 [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
 [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
}
'@
$process = [Diagnostics.Process]::new(); $process.StartInfo = $psi; $started = $false
try {
 if (-not $process.Start()) { throw 'Original GUI did not start.' }; $started = $true
 $until = [DateTime]::UtcNow.AddSeconds(45)
 do {
  Start-Sleep -Milliseconds 250; $process.Refresh()
  if ($process.HasExited) { throw 'Original GUI exited before its window.' }
  if ($process.MainWindowHandle -ne [IntPtr]::Zero -and $process.MainWindowTitle -ceq 'magpie') { break }
 } while ([DateTime]::UtcNow -lt $until)
 if ($process.MainWindowHandle -eq [IntPtr]::Zero -or $process.MainWindowTitle -cne 'magpie') { throw 'Owned original GUI window unavailable.' }
 Start-Sleep -Seconds 2; $process.Refresh()
 $rect = New-Object NativeMagpieCapture+RECT
 if (-not [NativeMagpieCapture]::GetWindowRect($process.MainWindowHandle,[ref]$rect)) { throw 'Window bounds unavailable.' }
 $width=$rect.Right-$rect.Left; $height=$rect.Bottom-$rect.Top
 if ($width -le 0 -or $height -le 0 -or $width -gt 2400 -or $height -gt 2000) { throw 'Unexpected owned window size.' }
 $bitmap=[Drawing.Bitmap]::new($width,$height); $graphics=[Drawing.Graphics]::FromImage($bitmap); $dc=$graphics.GetHdc()
 try { if (-not [NativeMagpieCapture]::PrintWindow($process.MainWindowHandle,$dc,2)) { throw 'Window capture failed.' } }
 finally { $graphics.ReleaseHdc($dc); $graphics.Dispose() }
 try { $bitmap.Save((Join-Path $evidence 'original-magpie.png'),[Drawing.Imaging.ImageFormat]::Png) } finally { $bitmap.Dispose() }
} finally {
 if ($started -and -not $process.HasExited) {
  [void]$process.CloseMainWindow()
  if (-not $process.WaitForExit(5000)) { $process.Kill($true); if (-not $process.WaitForExit(5000)) { throw 'Owned GUI exit unconfirmed.' } }
 }
 $process.Dispose()
}
if ((Get-FileHash $fixture -Algorithm SHA256).Hash -cne $before) { throw 'Synthetic Codex configuration changed.' }
@{sourceCommit=$head;stage='original-window-captured';nativeActions=0;realAccountsUsed=$false;syntheticCodexConfigUnchanged=$true;controlsAccepted=$false;zeroEgressProven=$false} |
 ConvertTo-Json | Set-Content (Join-Path $evidence 'report.json') -Encoding UTF8
Get-ChildItem -LiteralPath $package -File -Recurse -Force | Sort-Object FullName | ForEach-Object {
 ((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $_.FullName.Substring($package.Length + 1).Replace('\','/'))
} | Set-Content (Join-Path $package 'SHA256SUMS') -Encoding ascii
