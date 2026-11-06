# Official Store-package baseline only. No sign-in, package modification, or CLI override.
# Run solely in the disposable, branch-guarded GitHub hosted job.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted' -or
    $env:GITHUB_REPOSITORY -ne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -ne 'refs/heads/experiment/app-server-bridge-v0.1.3035') {
    throw 'This probe is restricted to the authorized disposable GitHub hosted branch job.'
}
$out = Join-Path $env:RUNNER_TEMP 'desktop-cold-start-report'
$work = Join-Path $env:RUNNER_TEMP ('desktop-cold-start-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $out, $work | Out-Null
$report = [ordered]@{
    schema = 1; sourceCommit = $env:GITHUB_SHA; mode = 'official-baseline'
    status = 'blocked'; stage = 'preflight'; startedUtc = [DateTime]::UtcNow.ToString('o')
    limits = @('No account login or user credentials.', 'Latest official package, not pinned historical Desktop.',
        'No bridge override in this first baseline phase.', 'Window visibility is not model or account acceptance.')
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
## Official Desktop cold-start baseline
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
        'CODEX_HOME', 'CODEX_CLI_PATH', 'MAGPIE_APP_SERVER_BRIDGE_CONFIG')) {
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
        } })
        aliases = $aliases
        dependencies = @($manifest.SelectNodes("//*[local-name()='Dependencies']/*") | ForEach-Object { $_.OuterXml })
    }
    if ($report.manifest.name -ne 'OpenAI.Codex' -or $report.manifest.architecture -ne 'x64') {
        Block 'Unexpected official package identity or architecture; inspect evidence before proceeding.'
    }
    if ($apps.Count -ne 1) { Block 'Application selection is ambiguous; no AUMID will be guessed.' }
    if (-not $hasInput -or -not [Environment]::UserInteractive -or
        $report.session.activeConsoleSessionId -ne $processSession -or
        $report.session.screenWidth -le 0 -or $report.session.screenHeight -le 0) {
        Block 'This runner does not expose the required interactive input desktop; GUI cold start not attempted.'
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
    $aumid = $app.PackageFamilyName + '!' + $apps[0].GetAttribute('Id')
    $report.aumid = $aumid
    $report.stage = 'normal-package-activation'
    $subscription = Register-CimIndicationEvent -Query 'SELECT * FROM Win32_ProcessStartTrace' -SourceIdentifier $eventSource
    [uint32]$launched = 0
    $hr = [DesktopColdProbe.Native]::Activate($aumid, [ref]$launched)
    $activationPid = [int]$launched
    $report.activation = [ordered]@{ hresult = ('0x{0:X8}' -f $hr); processId = $activationPid }
    if ($hr -ne 0 -or $activationPid -le 0) { Block 'Normal package activation failed.' }
    $windows = @()
    $observed = @{}
    for ($i = 0; $i -lt 30; $i++) {
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
        if ($windows.Count -gt 0 -and $i -ge 9) { break }
    }
    $report.processes = @($observed.Values | Sort-Object processId)
    $report.windows = @($windows | Select-Object ProcessId, Left, Top, Width, Height)
    $starts = @(Get-Event -SourceIdentifier $eventSource -ErrorAction SilentlyContinue | ForEach-Object { $_.SourceEventArgs.NewEvent })
    $report.processStarts = @($starts | Where-Object {
        $observed.ContainsKey([string]$_.ProcessID) -or $observed.ContainsKey([string]$_.ParentProcessID)
    } | ForEach-Object { [ordered]@{
        processId = [int]$_.ProcessID; parentProcessId = [int]$_.ParentProcessID; name = [string]$_.ProcessName
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
    $report.capture = 'official-window.png; screen pixels may contain occlusion and require visual review'
    $report.status = 'observed'
    $report.stage = 'baseline-window-observed'
    $report.reason = 'Normal activation produced a package-identity-verified process and visible window. Screenshot review remains required; no bridge or logged-in behavior tested.'
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
