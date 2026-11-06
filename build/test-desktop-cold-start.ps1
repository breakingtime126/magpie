# Official Store-package baseline and registered-alias bridge launch. No sign-in or package modification.
# Run solely in the disposable, branch-guarded GitHub hosted job.
[CmdletBinding()]
param(
    [ValidateSet('baseline', 'bridge', 'observe')][string]$Mode = 'baseline',
    [string]$BridgeExecutable = '',
    [string]$ObserverExecutable = '',
    [switch]$EnableReviewedStartupProbe
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted' -or
    $env:GITHUB_REPOSITORY -ne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -ne 'refs/heads/experiment/app-server-bridge-v0.1.3035') {
    throw 'This probe is restricted to the authorized disposable GitHub hosted branch job.'
}
if ($EnableReviewedStartupProbe -and $Mode -ne 'observe') {
    throw 'Reviewed startup inspection is limited to the explicitly instrumented CI mode.'
}
$out = Join-Path $env:RUNNER_TEMP 'desktop-cold-start-report'
$work = Join-Path $env:RUNNER_TEMP ('desktop-cold-start-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $out, $work | Out-Null
$report = [ordered]@{
    schema = 1; sourceCommit = $env:GITHUB_SHA; mode = $Mode
    status = 'blocked'; stage = 'preflight'; startedUtc = [DateTime]::UtcNow.ToString('o')
    limits = @('No account login or user credentials.', 'Latest official package, not pinned historical Desktop.',
        'Bridge phase tests launch only; any startup opt-in is recorded separately, auth modes remain disabled.', 'Window visibility is not model or account acceptance.')
}
$activationPid = 0
$eventSource = 'MagpieDesktopProcessStart'
$subscription = $null
$exitCode = 1
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
    # Presence checks only. Never read, copy, print, or delete any authentication value.
    foreach ($name in @('OPENAI_API_KEY', 'OPENAI_ACCESS_TOKEN', 'CODEX_API_KEY', 'CODEX_AUTH_JSON',
        'CODEX_HOME', 'CODEX_CLI_PATH', 'MAGPIE_APP_SERVER_BRIDGE_CONFIG',
        'MAGPIE_CI_REAL_BRIDGE', 'MAGPIE_CI_TRACE_FILE', 'MAGPIE_CI_OBSERVER_ACTIVE')) {
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
        $bridgeSettings = [ordered]@{ backends = $backends }
        if ($EnableReviewedStartupProbe) {
            $expectedVersion = 'codex-cli 0.167.0-alpha.3.2'
            $expectedBackendHash = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
            $actualBackendHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $officialBackend.FullName).Hash.ToLowerInvariant()
            if ($versionText -cne $expectedVersion -or $actualBackendHash -cne $expectedBackendHash) {
                Block 'Reviewed startup inspection requires the exact verified official backend.'
            }
            foreach ($backendEntry in $backends) {
                $backendEntry.startup_probe_sha256 = $expectedBackendHash
            }
            # Only the reviewed exact-version startup profile is enabled. No auth or JSONL policy modes.
            $bridgeSettings.startup_probe = 'compare-unrestricted-v0.167.0-alpha.3.2'
        }
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
                scope = 'Instrumented launch only; argv and wire bytes are forwarded to the unchanged production bridge.'
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
        }
        $start.EnvironmentVariables['MAGPIE_APP_SERVER_BRIDGE_CONFIG'] = $bridgeConfig
        $started = [Diagnostics.Process]::Start($start)
        if (-not $started) { Block 'Registered alias did not return a process.' }
        $activationPid = $started.Id
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
                'flag', 'configKey', 'configValue', 'child_pid', 'exit')
            $kinds = @('observer_start', 'argv', 'argv_config', 'rpc', 'rpc_error', 'stream_end',
                'setup_error', 'child_start_failed', 'child_start', 'copy_error', 'child_exit')
            $classes = @('test_only', 'request', 'notification', 'duplicate_id', 'correlation_limit',
                'response', 'unmatched_response', 'unrecognized', 'oversized', 'unterminated',
                'unsupported_authentication_capability', 'initialization_required',
                'config_requirements_disabled', 'config_requirements_failed', 'gateway_oauth_disabled',
                'unsupported_method', 'invalid_capabilities', 'invalid_initialization', 'already_initialized',
                'startup_probe_unavailable', 'unknown', 'eof', 'read_error', 'copy_error')
            $methods = @('initialize', 'initialized', 'configRequirements/read', 'account/read',
                'account/rateLimits/read', 'account/gatewayOAuth/read', 'attestation/generate', 'model/list',
                'config/read', 'environment/info', 'environment/status', 'thread/list', 'thread/loaded/list',
                'thread/start', 'thread/read', 'turn/start', 'unknown')
            $flags = @('app-server', '--check', '--help', '-h', '--version', '-V',
                '--analytics-default-enabled', '--stdio', '--print-startup-config', '--startup-sign-out',
                '--listen', '--startup-client-info', '-c', '--config')
            $configKeys = @(
                'allow_login_shell', 'allow_symlinked_codex_home', 'analytics.enabled',
                'check_for_update_on_startup', 'cli_auth_credentials_store', 'daybreak',
                'disable_paste_burst', 'experimental_use_unified_exec_tool', 'features.agent_message_board',
                'features.analytics_plan_history', 'features.api_key_cyber_access_programs', 'features.api_key_model_discovery',
                'features.app_server_websocket_compression', 'features.apply_patch_freeform', 'features.apply_patch_preserve_line_endings',
                'features.apply_patch_streaming_events', 'features.apps', 'features.apps_early_resource_discovery',
                'features.apps_mcp_path_override', 'features.async_compaction', 'features.auth_elicitation',
                'features.background_paginated_rollout_migration', 'features.bedrock_setup_wizard', 'features.browser_annotation_api',
                'features.browser_use', 'features.browser_use_external', 'features.browser_use_full_cdp_access',
                'features.chronicle', 'features.cli_daybreak', 'features.code_mode',
                'features.code_mode_buffered_exec', 'features.code_mode_host', 'features.code_mode_host.disable_in_process_fallback',
                'features.code_mode_host.enabled', 'features.code_mode_host_grpc', 'features.code_mode_host_output_truncation',
                'features.code_mode_interrupt', 'features.code_mode_only', 'features.code_mode_only_strict_3p_tools',
                'features.code_mode_prewarm', 'features.code_mode_tool_description_first', 'features.code_mode_tool_search',
                'features.codex_apps_mcp_2026_07_28', 'features.codex_git_commit', 'features.codex_hooks',
                'features.collab', 'features.collaboration_modes', 'features.compaction_image_budget',
                'features.computer_use', 'features.concurrent_reasoning_summaries', 'features.connectors',
                'features.content_item_kinds', 'features.context_management', 'features.credential_masking',
                'features.critical_path_timing', 'features.current_time_reminder', 'features.cwd_relative_turn_diffs',
                'features.daemon_auto_start', 'features.default_mode_request_user_input', 'features.defer_mailbox_preemption',
                'features.deferred_executor', 'features.deferred_tool_world_state', 'features.elevated_windows_sandbox',
                'features.enable_experimental_windows_sandbox', 'features.enable_fanout', 'features.enable_mcp_apps',
                'features.enable_request_compression', 'features.exec_permission_approvals', 'features.executed_tool_call_metadata',
                'features.executor_capability_discovery', 'features.executor_capability_discovery_v2', 'features.experimental_use_unified_exec_tool',
                'features.experimental_windows_sandbox', 'features.external_agent_memory_import', 'features.external_migration',
                'features.fast_mode', 'features.gcp_setup_wizard', 'features.goals',
                'features.guardian_approval', 'features.guardian_conversation_history_tools', 'features.guardian_enhanced_node_repl_transcripts',
                'features.guardian_ext', 'features.guardian_native_multi_agent_history', 'features.guardian_node_repl_transcript_images',
                'features.guardian_recorded_items', 'features.guardian_reuse_parent_compaction', 'features.guardian_root_handoff_context',
                'features.guardian_trust_orchestrator_connectors', 'features.guardianv2', 'features.guardianv2_decisions_comparison',
                'features.guardianv3', 'features.hooks', 'features.image_detail_original',
                'features.image_generation', 'features.image_resize_notice', 'features.imagegenext',
                'features.in_app_browser', 'features.in_app_chat', 'features.in_app_dictation',
                'features.in_app_local_automation', 'features.in_app_updates', 'features.in_app_voice',
                'features.incremental_exec_tools', 'features.incremental_tools', 'features.instant_interrupt',
                'features.interrupt_on_cancel', 'features.item_ids', 'features.js_repl',
                'features.js_repl_tools_only', 'features.local_thread_store_compression', 'features.local_thread_store_shared_compression',
                'features.login_shell_package_path', 'features.mcp_2026_07_28', 'features.mcp_connection_sharing',
                'features.mcp_oauth_refresh_coordination', 'features.mcp_tool_list_changed', 'features.memories',
                'features.memory_tool', 'features.mentions_v2', 'features.model_catalog_in_context',
                'features.multi_agent', 'features.multi_agent_mode', 'features.multi_agent_v2',
                'features.multi_agent_v2_dynamic_tools', 'features.multi_env_copy_file', 'features.network_proxy',
                'features.non_prefixed_mcp_tool_names', 'features.nonfatal_clock_read_errors', 'features.omit_app_server_notification_media',
                'features.omit_skill_descriptions', 'features.output_token_replay', 'features.personality',
                'features.plugin_discovery_prefetch', 'features.plugin_hooks', 'features.plugin_sharing',
                'features.plugins', 'features.powershell_shell_version', 'features.prediction_preserve_parent_tools',
                'features.prefer_mxc', 'features.prevent_idle_sleep', 'features.psp',
                'features.realtime_conversation', 'features.reasoning_effort_override', 'features.recommended_plugins',
                'features.remote_compaction_v2', 'features.remote_control', 'features.remote_models',
                'features.remote_plugin', 'features.request_permissions', 'features.request_permissions_tool',
                'features.request_rule', 'features.resize_all_images', 'features.respect_system_proxy',
                'features.responses_websockets', 'features.responses_websockets_v2', 'features.retain_client_developer_messages',
                'features.retain_client_tool_outputs', 'features.rollout_budget', 'features.runtime_agent_instances',
                'features.runtime_metrics', 'features.search_tool', 'features.secret_auth_storage',
                'features.send_async_message', 'features.send_message_to_user_async', 'features.shell_snapshot',
                'features.shell_snapshot_v2', 'features.shell_tool', 'features.shell_zsh_fork',
                'features.simplified_selection_hints', 'features.skill_catalog_deltas', 'features.skill_env_var_dependency_prompt',
                'features.skill_mcp_dependency_install', 'features.skill_search', 'features.skip_host_skill_discovery',
                'features.sleep_tool', 'features.sqlite', 'features.stable_environment_tools',
                'features.standalone_web_search', 'features.steer', 'features.step_model_switching',
                'features.subagent_default_context_limits', 'features.subagent_thread_environments', 'features.suppress_model_change_announcements',
                'features.system_proxy_fallback', 'features.telepathy', 'features.terminal_resize_reflow',
                'features.terminal_visualization_instructions', 'features.token_budget', 'features.tool_call_mcp_elicitation',
                'features.tool_registry', 'features.tool_search', 'features.tool_search_always_defer_mcp_tools',
                'features.tool_suggest', 'features.transcript_v2', 'features.tui_app_server',
                'features.tui_extensions_statusline', 'features.tui_multi_backend', 'features.ultrafast_mode',
                'features.unavailable_dummy_tools', 'features.unbounded_connection_retries', 'features.undo',
                'features.unified_exec', 'features.unified_exec_tty', 'features.unified_exec_zsh_fork',
                'features.unified_image_budget', 'features.untrusted_as_tool_output', 'features.use_agent_identity',
                'features.use_legacy_landlock', 'features.use_linux_sandbox_bwrap', 'features.use_xaa',
                'features.view_image', 'features.wake_on_agent_messages', 'features.web_search',
                'features.web_search_cached', 'features.web_search_request', 'features.windows_sandbox_service',
                'features.workspace_dependencies', 'features.workspace_owner_usage_nudge', 'features.worktrees',
                'features.write_stdin_approval', 'feedback.enabled', 'hide_agent_reasoning',
                'include_apps_instructions', 'include_collaboration_mode_instructions', 'include_environment_context',
                'include_environment_context_time', 'include_permissions_instructions', 'mcp_oauth_credentials_store',
                'plugins._default.enabled', 'show_raw_agent_reasoning', 'suppress_unstable_features_warning',
                'windows.allow_mxc', 'windows.sandbox'
            )
            $configValues = @('bool_true', 'bool_false', 'enum_file', 'enum_keyring', 'enum_auto',
                'enum_ephemeral', 'enum_elevated', 'enum_unelevated', 'enum_mxc', 'other')
            $booleanKeys = @('requestAttestation', 'explicitGatewayOauth', 'requiredHumanApproval', 'experimentalApi')
            $numericKeys = @('seq', 'pid', 'correlation', 'code', 'child_pid', 'exit')
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
                    try { $record = $line | ConvertFrom-Json } catch { Block 'Observer metadata is not valid JSON.' }
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
                        } elseif ($booleanKeys -ccontains $property.Name) {
                            if ($value -isnot [bool]) { Block 'Observer emitted invalid boolean metadata.' }
                        } else {
                            $allowed = switch ($property.Name) {
                                'kind' { $kinds }
                                'class' { $classes }
                                'method' { $methods }
                                'direction' { @('to_bridge', 'from_bridge', 'stderr') }
                                'flag' { $flags }
                                'configKey' { $configKeys }
                                'configValue' { $configValues }
                            }
                            if ($allowed -cnotcontains $value) { Block 'Observer emitted an unreviewed metadata label.' }
                        }
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
            $report.observer.recordCount = $allEvents.Count
            $report.observer.records = @($allEvents | Select-Object -First 256)
            $report.observer.recordsTruncatedInSummary = $allEvents.Count -gt 256
            $report.observer.limit = 'Missing child exit or output EOF, saturated slots or truncated summary preclude claims that no failure occurred. Read validated artifact records for remaining evidence.'
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
    } catch { $report.cleanupErrorType = $_.Exception.GetType().FullName }
    finally { Save-Report }
}
exit $exitCode
