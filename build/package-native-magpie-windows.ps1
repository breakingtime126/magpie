# Build the upstream main/GUI, using its existing portable-data contract.
[CmdletBinding()]
param([switch]$DiscoverOriginalConnect,[switch]$AcceptOriginalConnect)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
function Assert-Exit([string]$Name) { if ($LASTEXITCODE -ne 0) { throw "$Name failed." } }
function Get-OriginalPackageBuildTags([bool]$Acceptance,[string]$StageMode,[string]$CommitMessage) {
    $diagnostic=$CommitMessage.Contains('[original-backend-handshake-v1]')
    if ($diagnostic -and (-not $Acceptance -or $StageMode -cne 'stage-diagnostics-v1' -or
        -not $CommitMessage.Contains('[original-connect-desktop-v1]') -or
        -not $CommitMessage.Contains('[original-connect-stages-only-v1]'))) {
        throw 'Handshake diagnostics require exact original stage-only acceptance.'
    }
    $gui='production,nativecodex';$adapter='nogui,nativecodex'
    if ($diagnostic) { $gui+=',nativehandshakediag';$adapter+=',nativehandshakediag' }
    return [ordered]@{gui=$gui;adapter=$adapter;handshakeDiagnostic=$diagnostic}
}
if ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_OS -cne 'Windows' -or
    $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035') { throw 'Scoped temporary CI required.' }
$message=''
$head = (& git rev-parse HEAD).Trim(); Assert-Exit 'Read source'
if ($head -cne $env:GITHUB_SHA) { throw 'Source mismatch.' }
if ($AcceptOriginalConnect) {
 if ($DiscoverOriginalConnect -or $env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY -or
     $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or $env:GITHUB_EVENT_NAME -cne 'push' -or
     $env:MAGPIE_ORIGINAL_CONNECT_ACTION -cne 'original-ui-v1' -or
     $env:MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE -cne 'original-ui-v1') { throw 'Exclusive hosted original acceptance required.' }
 $message = (& git log -1 --format=%B) -join "`n"; Assert-Exit 'Read acceptance opt-in'
 if (-not $message.Contains('[original-connect-desktop-v1]')) { throw 'Original acceptance commit marker required.' }
} elseif ($env:MAGPIE_ORIGINAL_CONNECT_ACTION -or $env:MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE) { throw 'Acceptance marker without explicit option.' }
if ($env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS -and (-not $AcceptOriginalConnect -or $env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS -cne 'stage-diagnostics-v1' -or -not $message.Contains('[original-connect-stages-only-v1]'))) { throw 'Exact stage diagnostics opt-in required.' }
if ($AcceptOriginalConnect -and ($message.Contains('[original-connect-stages-only-v1]') -ne ($env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS -ceq 'stage-diagnostics-v1'))) { throw 'Stage diagnostics mode mismatch.' }
if ($DiscoverOriginalConnect) {
 if ($env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or $env:GITHUB_EVENT_NAME -cne 'push' -or
     $env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY -cne 'read-only-v1') { throw 'Explicit hosted discovery context required.' }
 $message = (& git log -1 --format=%B) -join "`n"; Assert-Exit 'Read discovery opt-in'
 if (-not $message.Contains('[original-connect-discovery-v1]')) { throw 'Discovery commit marker required.' }
 & (Join-Path $PSScriptRoot 'original-connect-discovery.tests.ps1')
 . (Join-Path $PSScriptRoot 'original-connect-discovery.ps1')
} elseif ($env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY) { throw 'Discovery marker without explicit option.' }
$buildMode=Get-OriginalPackageBuildTags ([bool]$AcceptOriginalConnect) ([string]$env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS) $message
$package = Join-Path $env:RUNNER_TEMP 'native-magpie-package'
$evidence = Join-Path $env:RUNNER_TEMP 'native-magpie-evidence'
[void](New-Item -ItemType Directory -Path $package -ErrorAction Stop)
[void](New-Item -ItemType Directory -Path $evidence -ErrorAction Stop)
& go vet -tags nogui,nativecodex ./internal/nativeconnect ./internal/nativeprepdiag ./internal/nativepreview ./internal/nativeconfigguard ./internal/provider ./internal/gateway ./internal/gui ./internal/agent ./internal/library ./internal/codexsetup
Assert-Exit 'Windows native-mode vet'
& go vet -tags production,nativecodex ./internal/gui
Assert-Exit 'Windows original GUI vet'
& go test -tags nogui,nativecodex -count=1 -timeout=5m -run '^TestNative(Codex|GPT)' ./internal/provider ./internal/gateway ./internal/agent ./internal/library ./internal/gui
Assert-Exit 'Synthetic native-mode write protection'
& go test -tags nogui,nativecodex -count=1 -timeout=2m ./internal/nativeconnect
Assert-Exit 'Synthetic launch selection transaction'
& go test -tags nogui,nativecodex -count=1 -timeout=2m ./internal/nativeprepdiag
Assert-Exit 'Synthetic Connect stage observer'
& go test -tags nogui,nativecodex -count=1 -timeout=2m -run '^(TestRuntimePreparationDiagnosticsObserveOriginalCalls|TestInstalledQueryBranches|TestInstalledQuerySourceContract)$' ./internal/nativepreview
Assert-Exit 'Synthetic runtime preparation diagnostics'
& go test -tags nogui,nativecodex -count=1 -timeout=5m -run '^TestInstalledQueryRoot' ./internal/nativepreview
Assert-Exit 'Synthetic retained query-root observation'
& go test -tags nogui,nativecodex -count=1 -timeout=5m -run '^TestInstalledQueryProgress' ./internal/nativepreview
Assert-Exit 'Synthetic query progress observation and pipe drain'
& go test -tags nogui,nativecodex -count=1 -timeout=2m -run '^TestOriginalConnectDiagnosticsBoundToOriginalInvocation$' ./internal/gui
Assert-Exit 'Synthetic original Connect diagnostics binding'
& go vet -tags nogui,nativecodex ./internal/nativesupplier ./internal/nativecatalog ./internal/nativehost ./internal/nativebootstrap ./internal/nativechannel ./internal/nativeendpoint ./internal/nativelaunch ./internal/nativeprofile
Assert-Exit 'Windows supplier and ownership stack vet'
& go test -tags nogui,nativecodex -count=1 -timeout=5m ./internal/nativesupplier ./internal/nativecatalog ./internal/nativehost ./internal/nativebootstrap ./internal/nativechannel ./internal/nativeendpoint ./internal/nativelaunch ./internal/nativeprofile
Assert-Exit 'Synthetic supplier and ownership stack'
& go test -tags nogui,nativecodex -count=1 -timeout=5m -run '^TestConfiguredSupplier' ./internal/provider
Assert-Exit 'Synthetic configured supplier source'
if ($buildMode.handshakeDiagnostic) {
    & go vet -tags nogui,nativecodex,nativehandshakediag ./internal/nativehandshake ./internal/nativehost ./internal/nativelaunch ./internal/appserverbridge ./cmd/magpie-codex-bridge
    Assert-Exit 'Windows diagnostic handshake instrumentation vet'
    & go test -tags nogui,nativecodex -count=1 -timeout=2m ./internal/nativehandshake
    Assert-Exit 'Default handshake observer is inert'
    & go test -v -tags nogui,nativecodex,nativehandshakediag -count=1 -timeout=5m -run '^Test(Handshake|OwnedStartupProbe)' ./internal/nativehandshake ./internal/nativehost ./internal/nativelaunch ./internal/appserverbridge ./cmd/magpie-codex-bridge
    Assert-Exit 'Bounded diagnostic handshake observer'
}
& go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json --arch amd64 --out rsrc --file-version '0.0.0' --product-version '0.0.0'
Assert-Exit 'Original Windows resources'
$adapter = Join-Path $package 'magpie-codex-bridge.exe'
# Console stdio component, not a new GUI or a bundled official Codex binary.
& go build -tags $buildMode.adapter -trimpath -ldflags "-s -w -X main.buildVersion=$head" -o $adapter ./cmd/magpie-codex-bridge
Assert-Exit 'Same-source stdio adapter build'
$adapterVersion = (& $adapter --version | Out-String).Trim()
Assert-Exit 'Adapter identity only'
if (-not $adapterVersion.Contains($head)) { throw 'Adapter source identity mismatch.' }
$adapterDigest = (Get-FileHash $adapter -Algorithm SHA256).Hash.ToLowerInvariant()
$exe = Join-Path $package 'Magpie-Native-Codex-Experimental.exe'
# Pin the actual same-source adapter inside the GUI, never from a mutable
# sibling manifest or environment override. Keep the original GUI/main entry.
$nativePins = "-X github.com/yetone/magpie/internal/nativeconnect.packagedLauncherSHA256=$adapterDigest -X github.com/yetone/magpie/internal/nativeconnect.packagedLauncherSourceRevision=$head"
& go test -tags nogui,nativecodex -trimpath -ldflags $nativePins -count=1 -timeout=2m -run '^TestPackagedLauncherLinkerPins$' ./internal/nativeconnect -args "-native-package-sha256=$adapterDigest" "-native-package-revision=$head"
Assert-Exit 'Compiled same-source launcher pin injection'
& go build -tags $buildMode.gui -trimpath -ldflags "-s -w -H windowsgui -X main.version=native-codex-$($head.Substring(0,8)) $nativePins" -o $exe .
Assert-Exit 'Original Magpie GUI with pinned adapter build'
$guiBuildInfo = & go version -m $exe
Assert-Exit 'Read GUI build provenance'
# Go deliberately omits -ldflags under -trimpath. Check source/tag provenance
# here; the executed package test above checks the shared linker pin arguments.
$provenance = $guiBuildInfo -join "`n"
if ($provenance -notmatch ('(?m)^\s*build\s+-tags=' + [regex]::Escape($buildMode.gui) + '\s*$') -or
    $provenance -notmatch ('(?m)^\s*build\s+vcs\.revision=' + [regex]::Escape($head) + '\s*$')) { throw 'GUI source or native build tags do not match.' }
Copy-Item -LiteralPath 'docs/subsystems/native-codex-connect.md' -Destination (Join-Path $package 'CONNECT-STATUS.md')

Copy-Item -LiteralPath 'LICENSE','THIRD_PARTY_NOTICES' -Destination $package
Copy-Item -LiteralPath 'docs/subsystems/original-gui-native-codex.md' -Destination (Join-Path $package 'README.md')
[IO.File]::WriteAllText((Join-Path $package '.portable'), '', [Text.UTF8Encoding]::new($false))
[void](New-Item -ItemType Directory -Path (Join-Path $package 'data'))
# Native Magpie setting, not a fake account state; explicit logins remain actions.
[IO.File]::WriteAllText((Join-Path $package 'data/settings.json'), '{"noAccountDiscovery":true,"lang":"zh","theme":"light","guideHidden":true}', [Text.UTF8Encoding]::new($false))
@{sourceCommit=$head;adapterSHA256=(Get-FileHash $adapter -Algorithm SHA256).Hash.ToLowerInvariant();guiSHA256=(Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant();unsigned=$true;originalGUI=$true;buildTags=$buildMode.gui;adapterBuildTags=$buildMode.adapter;handshakeDiagnostic=[bool]$buildMode.handshakeDiagnostic;portable=$true} |
 ConvertTo-Json | Set-Content (Join-Path $package 'BUILD.json') -Encoding UTF8
if ($AcceptOriginalConnect) {
 # Desktop host owns the retained official package/service/activation handles.
 # No policy override or environment shortcut; a policy refusal remains blocked.
 $desktop = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
 & $desktop -NoLogo -NoProfile -NonInteractive -File (Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1') -PackageDirectory $package -EvidenceDirectory $evidence -SourceCommit $head
 if ($env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS) { throw 'Stage diagnostics cannot publish an acceptance package.' }
 Assert-Exit 'Original UI Connect, official activation and original recovery'
 Get-ChildItem -LiteralPath $package -File -Recurse -Force | Sort-Object FullName | ForEach-Object {
  ((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $_.FullName.Substring($package.Length + 1).Replace('\','/'))
 } | Set-Content (Join-Path $package 'SHA256SUMS') -Encoding ascii
 return
}
# Launch a copy, never the deliverable data directory. Only synthetic home files.
$root = Join-Path $env:RUNNER_TEMP ('native-magpie-window-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $root)
$homeRoot = Join-Path $root 'home'; [void](New-Item -ItemType Directory -Path $homeRoot)
$run = Join-Path $root 'app'; Copy-Item -LiteralPath $package -Destination $run -Recurse
if ($DiscoverOriginalConnect) {
 # Only the disposable run copy changes language; the deliverable remains zh.
 $runSettings = Join-Path $run 'data/settings.json'
 $settings = [IO.File]::ReadAllText($runSettings) | ConvertFrom-Json
 $settings.lang = 'en'
 [IO.File]::WriteAllText($runSettings, ($settings | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
}
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
 if ($DiscoverOriginalConnect) {
  $discovery = $null
  try {
   $discovery = Invoke-OriginalConnectDiscovery -OwnedProcess $process -ExpectedExecutable $psi.FileName
  } catch {
   # Fixed failure metadata only; no worker exception/UI text is published.
   $discovery = [ordered]@{schema=1;scope='original-magpie-controls-read-only';status='blocked';reason='supervisor';
    nodes=0;counts=$null;actions=0;connectVerified=$false;desktopVerified=$false}
  }
  $discovery | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $evidence 'original-connect-discovery.json') -Encoding UTF8
  if ($discovery.status -cne 'observed') { throw 'Read-only original control discovery blocked; see fixed report.' }
 }
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
