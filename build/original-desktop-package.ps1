# Disposable original-UI acceptance preparation. No Connect, override writer,
# package patch, sign-in, setup, or Desktop activation runs when dot-sourced.
. (Join-Path $PSScriptRoot 'original-desktop-package-verification.ps1')
. (Join-Path $PSScriptRoot 'desktop-native-entry.ps1')
. (Join-Path $PSScriptRoot 'desktop-entry-observe.ps1')
. (Join-Path $PSScriptRoot 'desktop-entry-activate.ps1')

function Assert-OriginalDesktopPackageRunner {
    if ($PSVersionTable.PSEdition -cne 'Desktop' -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE -cne 'original-ui-v1') {
        throw 'Explicit disposable original-UI Desktop acceptance required.'
    }
}
function Assert-OriginalDesktopInteractive {
    if (-not ('OriginalDesktopSession.Native' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace OriginalDesktopSession {
 public static class Native {
  [DllImport("user32.dll",SetLastError=true)] public static extern IntPtr OpenInputDesktop(uint flags,bool inherit,uint access);
  [DllImport("user32.dll")] public static extern bool CloseDesktop(IntPtr desktop);
  [DllImport("user32.dll")] public static extern int GetSystemMetrics(int index);
  [DllImport("kernel32.dll")] public static extern uint WTSGetActiveConsoleSessionId();
 }
}
'@
    }
    $desktop=[OriginalDesktopSession.Native]::OpenInputDesktop(0,$false,1)
    try {
        if ($desktop -eq [IntPtr]::Zero -or -not [Environment]::UserInteractive -or
            [OriginalDesktopSession.Native]::WTSGetActiveConsoleSessionId() -ne (Get-Process -Id $PID).SessionId -or
            [OriginalDesktopSession.Native]::GetSystemMetrics(0) -le 0 -or
            [OriginalDesktopSession.Native]::GetSystemMetrics(1) -le 0) { throw 'Interactive input desktop is unavailable.' }
    } finally { if ($desktop -ne [IntPtr]::Zero) { [void][OriginalDesktopSession.Native]::CloseDesktop($desktop) } }
}
function Assert-OriginalDesktopRegistration($App,$Identity,$Previous=$null) {
    if ($null -eq $App -or $App.Name -cne $Identity.manifest.name -or
        [string]$App.Version -cne $Identity.manifest.version -or $App.Publisher -cne $Identity.manifest.publisher -or
        [string]$App.Status -cne 'Ok' -or $App.Architecture.ToString() -ine 'X64' -or
        $App.PackageFullName -cnotmatch '^OpenAI\.Codex_26\.1103\.4239\.0_x64__[a-z0-9]+\z' -or
        $App.PackageFamilyName -cnotmatch '^OpenAI\.Codex_[a-z0-9]+\z' -or
        -not [IO.Path]::IsPathRooted($App.InstallLocation) -or
        [IO.Path]::GetFullPath($App.InstallLocation) -cne $App.InstallLocation) { throw 'Official registration identity mismatch.' }
    if ($null -ne $Previous -and ($App.PackageFullName -cne $Previous.PackageFullName -or
        $App.PackageFamilyName -cne $Previous.PackageFamilyName -or $App.InstallLocation -cne $Previous.InstallLocation)) {
        throw 'Official registration changed.'
    }
}
function Open-OriginalDesktopInstalledFile($Context,[string]$Relative,[string]$ExpectedHash,[long]$Bytes) {
    $path=Join-Path $Context.App.InstallLocation $Relative
    Assert-PublicRegularPath $path
    $stream=[IO.File]::Open($path,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
    # Keep every opened stream even if verification fails, so finally can release it.
    $Context.Streams.Add($stream)
    $actual=Get-BoundedStreamDigest $stream $Bytes 2147483648 ([Diagnostics.Stopwatch]::StartNew())
    if ($stream.Length -ne $Bytes -or $actual -cne $ExpectedHash) { throw 'Installed official bytes mismatch.' }
    $Context.Files.Add([pscustomobject]@{Stream=$stream;Hash=$ExpectedHash;Bytes=$Bytes})
    return $path
}
function Assert-OriginalDesktopPackageCurrent($Context) {
    Assert-OriginalDesktopPackageRunner
    Assert-OriginalDesktopInteractive
    if ($Context.Closed -or $Context.Prepared -ne $true) { throw 'Verified package context is unavailable.' }
    Assert-Desktop4239Identity $Context.Identity
    $apps=@(Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction Stop)
    if ($apps.Count -ne 1) { throw 'Official registration is not unique.' }
    Assert-OriginalDesktopRegistration $apps[0] $Context.Identity $Context.App
    foreach ($file in $Context.Files) {
        $file.Stream.Position=0
        if ((Get-BoundedStreamDigest $file.Stream $file.Bytes 4294967296 ([Diagnostics.Stopwatch]::StartNew())) -cne $file.Hash) {
            throw 'Retained official bytes changed.'
        }
    }
    $Context.ActivationHost.Stream.Position=0
    if ((Get-BoundedStreamDigest $Context.ActivationHost.Stream $Context.ActivationHost.Stream.Length 1048576 ([Diagnostics.Stopwatch]::StartNew())) -cne $Context.ActivationHost.Sha256) {
        throw 'Retained activation host changed.'
    }
    if (Test-EntryProcessOverride) { throw 'Caller has a process override.' }
    if (Test-EntryMachineOverride) { throw 'Machine has an override.' }
    if ($null -ne $Context.Service) { Assert-EntryVerifiedServiceCurrent $Context.Service }
}
function Assert-OriginalDesktopPackageFresh($Context,[Collections.IDictionary]$PreparationEvidence=$null) {
    Set-OriginalDesktopPreparationStage $PreparationEvidence 'freshness-scan'
    Initialize-EntryHeldObservation
    $processes=@(Get-CimInstance Win32_Process -ErrorAction Stop)
    $candidates=@($processes | Where-Object { $_.Name -ieq 'codex-windows-sandbox-service.exe' })
    $state=[ordered]@{Held=$null;ImageStream=$null;ProcessId=0;CreatedTicks=[long]0}
    $ids=[Collections.Generic.List[int]]::new()
    try {
        $facts=Get-EntryFreshnessEvidence $processes $Context.App.PackageFullName $Context.BridgeName 0 0 {
            param($ProcessId)
            if ($candidates.Count -eq 1 -and $ProcessId -eq $candidates[0].ProcessId) {
                if ($null -ne $state.Held) { throw 'Duplicate service candidate.' }
                $state.Held=[DesktopEntryReadOnly.Held]::new($ProcessId)
                $state.ProcessId=$ProcessId; $state.CreatedTicks=$state.Held.CreatedUtcTicks
                if (-not $state.Held.Alive) { throw 'Service exited.' }
                $relation=$state.Held.PackageRelation($Context.App.PackageFullName)
                if ($relation -ceq 'unavailable') { throw 'Service identity unavailable.' }
                if ($relation -ceq 'expected') { return $Context.App.PackageFullName }
                return $null
            }
            # As in the original freshness scan, protected unrelated system PIDs
            # may be unreadable. Named Desktop/native/bridge rows still block.
            $held=$null
            try {
                $held=[DesktopEntryReadOnly.Held]::new($ProcessId)
                if ($held.PackageRelation($Context.App.PackageFullName) -ceq 'expected') { return $Context.App.PackageFullName }
            } catch { } finally { if ($null -ne $held) { $held.Dispose() } }
            return $null
        } $ids
        if ($facts.matches -eq 0 -and $candidates.Count -eq 0 -and $null -eq $Context.Service) { return }
        if (-not (Test-EntrySingleServiceCandidate $facts) -or $ids.Count -ne 1 -or
            $null -eq $state.Held -or $ids[0] -ne $state.ProcessId) { throw 'Prior Desktop or bridge process blocks activation.' }
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'service-binding'
        $service=Open-EntryVerifiedService $state $Context.Identity.sandboxService $Context.App.PackageFullName `
            $Context.App.InstallLocation $Context.StartedTicks
        if ($null -ne $Context.Service) {
            Assert-EntryVerifiedServiceCurrent $Context.Service
            if ($service.ProcessId -ne $Context.Service.ProcessId -or $service.CreatedTicks -ne $Context.Service.CreatedTicks) {
                throw 'Packaged service generation changed.'
            }
        } else {
            $Context.Service=$service
            $state.Held=$null; $state.ImageStream=$null
        }
    } finally {
        if ($null -ne $state.ImageStream) { $state.ImageStream.Dispose() }
        if ($null -ne $state.Held) { $state.Held.Dispose() }
    }
}
# Fixed diagnostic labels only. This never records exceptions, paths, or downloaded data.
function Set-OriginalDesktopPreparationStage([Collections.IDictionary]$Evidence,[string]$Stage) {
    if ($Stage -cnotin @('preflight','adapter-lock','package-download','license-download','package-pin',
        'package-path','license-path','package-open','package-digest','package-hash-match','license-open',
        'signature-verification','signed-package-identity','interactive-session','licensed-provisioning',
        'registration-readiness','installed-image-verification','activation-host-build','activation-host-self-test',
        'current-context-verification','freshness-scan','service-binding','prepared')) { throw 'Invalid preparation stage.' }
    if ($null -ne $Evidence) { $Evidence.stage=$Stage }
}
function Get-OriginalDesktopFailureEvidence([Management.Automation.ErrorRecord]$Record) {
    $safe=[ordered]@{category='other';hresult=0;httpStatus=0;webStatus=-1;line=0}
    if ($null -eq $Record -or $null -eq $Record.Exception) { return $safe }
    $base=$Record.Exception.GetBaseException()
    $safe.hresult=[int]$base.HResult
    $safe.category=switch ($base.GetType().FullName) {
        'System.Net.WebException' {'web';break}
        'System.Net.Http.HttpRequestException' {'http';break}
        'System.Security.Authentication.AuthenticationException' {'authentication';break}
        'System.Net.Sockets.SocketException' {'socket';break}
        'System.TimeoutException' {'timeout';break}
        'System.Threading.Tasks.TaskCanceledException' {'cancelled';break}
        'System.OperationCanceledException' {'cancelled';break}
        'System.UnauthorizedAccessException' {'access';break}
        'System.IO.IOException' {'io';break}
        'System.IO.FileNotFoundException' {'file-missing';break}
        'System.IO.DirectoryNotFoundException' {'directory-missing';break}
        'System.InvalidOperationException' {'operation';break}
        'System.ArgumentException' {'argument';break}
        'System.Management.Automation.CommandNotFoundException' {'command-missing';break}
        'System.Management.Automation.RuntimeException' {'powershell';break}
        default {'other'}
    }
    if ($null -ne $Record.InvocationInfo) {
        $line=$Record.InvocationInfo.ScriptLineNumber
        if ($line -gt 0 -and $line -le 100000) { $safe.line=[int]$line }
    }
    # A wrapper may contain an HTTP response while its base is a socket/TLS error.
    # Inspect at most eight exceptions, never messages, response bodies or URLs.
    $exception=$Record.Exception;$webSeen=$false
    for ($depth=0;$depth -lt 8 -and $null -ne $exception;$depth++) {
        $status=$null
        try {
            if ($exception -is [Net.WebException] -and -not $webSeen) {
                $webSeen=$true
                if ([Enum]::IsDefined([Net.WebExceptionStatus],$exception.Status)) { $safe.webStatus=[int]$exception.Status }
            }
            if ($exception -is [Net.WebException] -and $null -ne $exception.Response) {
                $property=$exception.Response.PSObject.Properties['StatusCode']
                if ($null -ne $property) { $status=$property.Value }
            } elseif ($exception.GetType().FullName -ceq 'System.Net.Http.HttpRequestException') {
                $property=$exception.PSObject.Properties['StatusCode']
                if ($null -ne $property) { $status=$property.Value }
            }
            if ($safe.httpStatus -eq 0 -and ($status -is [Net.HttpStatusCode] -or $status -is [int]) -and [int]$status -ge 100 -and [int]$status -le 599) {
                $safe.httpStatus=[int]$status
            }
        } catch { } # Missing status is unknown, not permission to retry.
        $exception=$exception.InnerException
    }
    return $safe
}
function Assert-OriginalDesktopDownloadDestination([string]$Kind,[string]$Destination) {
    if ($Kind -cnotin @('package','license') -or -not [IO.Path]::IsPathRooted($Destination) -or
        [IO.Path]::GetFullPath($Destination) -cne $Destination -or $Destination.Contains('"') -or
        -not $Destination.StartsWith(([IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\')+'\'),[StringComparison]::OrdinalIgnoreCase)) {
        throw 'Invalid private official download destination.'
    }
    $name='ChatGPT-x64.msix';if ($Kind -ceq 'license') { $name='ChatGPT-License.xml' }
    if ([IO.Path]::GetFileName($Destination) -cne $name -or (Test-Path -LiteralPath $Destination)) {
        throw 'Fresh official download destination required.'
    }
    $directory=[IO.Path]::GetDirectoryName($Destination)
    $root=[IO.Path]::GetPathRoot($directory);$current=$root
    foreach ($part in $directory.Substring($root.Length).Split([IO.Path]::DirectorySeparatorChar)) {
        if ($part.Length -eq 0) { continue };$current=Join-Path $current $part
        $parent=Get-Item -LiteralPath $current -Force -ErrorAction Stop
        if (-not $parent.PSIsContainer -or ($parent.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw 'Private download directory is unavailable.'
        }
    }
}
function ConvertFrom-OriginalDesktopDownloadResult([string]$Text) {
    if ($Text.Length -gt 4096) { throw 'Official download result exceeds its bound.' }
    $value=$Text | ConvertFrom-Json -ErrorAction Stop
    if ($null -eq $value -or @($value.PSObject.Properties).Count -ne 4 -or
        @($value.PSObject.Properties.Name | Where-Object {$_ -cnotin @('schema','status','bytes','failure')}).Count -ne 0 -or
        ($value.schema -isnot [int] -and $value.schema -isnot [long]) -or $value.schema -ne 1 -or $value.status -cnotin @('completed','blocked') -or
        ($value.bytes -isnot [int] -and $value.bytes -isnot [long]) -or $value.bytes -lt 0 -or $value.bytes -gt 4294967296) {
        throw 'Official download result is invalid.'
    }
    if ($value.status -ceq 'completed') {
        if ($value.bytes -lt 1 -or $null -ne $value.failure) { throw 'Official download completion is invalid.' }
    } else {
        $f=$value.failure
        if ($value.bytes -ne 0 -or $null -eq $f -or @($f.PSObject.Properties).Count -ne 5 -or
            @($f.PSObject.Properties.Name | Where-Object {$_ -cnotin @('category','hresult','httpStatus','webStatus','line')}).Count -ne 0 -or
            $f.category -cnotin @('other','web','http','authentication','socket','timeout','cancelled','access','io','file-missing',
                'directory-missing','operation','argument','command-missing','powershell') -or
            ($f.hresult -isnot [int] -and $f.hresult -isnot [long]) -or $f.hresult -lt [int]::MinValue -or $f.hresult -gt [int]::MaxValue -or
            ($f.httpStatus -isnot [int] -and $f.httpStatus -isnot [long]) -or
            ($f.httpStatus -ne 0 -and ($f.httpStatus -lt 100 -or $f.httpStatus -gt 599)) -or
            ($f.webStatus -isnot [int] -and $f.webStatus -isnot [long]) -or $f.webStatus -lt -1 -or $f.webStatus -gt [int]::MaxValue -or
            ($f.webStatus -ne -1 -and -not [Enum]::IsDefined([Net.WebExceptionStatus],[int]$f.webStatus)) -or
            ($f.line -isnot [int] -and $f.line -isnot [long]) -or $f.line -lt 0 -or $f.line -gt 100000) { throw 'Official download failure facts are invalid.' }
    }
    $failure=$null
    if ($null -ne $value.failure) {
        $failure=[ordered]@{category=[string]$value.failure.category;hresult=[int]$value.failure.hresult;
            httpStatus=[int]$value.failure.httpStatus;webStatus=[int]$value.failure.webStatus;line=[int]$value.failure.line}
    }
    return [pscustomobject]@{schema=1;status=[string]$value.status;bytes=[long]$value.bytes;failure=$failure}
}
function Invoke-OriginalDesktopDownload([string]$Kind,[string]$Destination,[Collections.IDictionary]$PreparationEvidence) {
    Assert-OriginalDesktopPackageRunner
    Assert-OriginalDesktopDownloadDestination $Kind $Destination
    $worker=Join-Path $PSScriptRoot 'original-desktop-package-download.ps1'
    if ($worker.Contains('"')) { throw 'Invalid download helper path.' }
    $start=[Diagnostics.ProcessStartInfo]::new()
    $start.FileName=Join-Path $PSHOME 'powershell.exe'
    $start.Arguments='-NoLogo -NoProfile -NonInteractive -File "'+$worker+'" -Kind '+$Kind+' -Destination "'+$Destination+'"'
    $start.UseShellExecute=$false;$start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
    # Inherit the caller's environment unchanged, including transport/proxy/trust
    # choices. Never clear it, add credentials, or change TLS/security settings.
    $diagnostic=[ordered]@{status='blocked';deadlineMilliseconds=180000;timedOut=$false;exitConfirmed=$false}
    $PreparationEvidence.download=$diagnostic
    $script:entryLastHelperEvidence=$null
    try { $text=Invoke-EntryBoundedHelper $start 180000 4096 }
    catch {
        if ($null -ne $script:entryLastHelperEvidence) {
            $diagnostic.timedOut=[bool]($script:entryLastHelperEvidence.waitExpired -or $script:entryLastHelperEvidence.timerExpired)
            $diagnostic.exitConfirmed=$script:entryLastHelperEvidence.cleanup -ceq 'exit-confirmed'
        }
        throw
    }
    $diagnostic.exitConfirmed=$true
    $result=ConvertFrom-OriginalDesktopDownloadResult $text
    if ($result.status -cne 'completed') {
        $PreparationEvidence.failure=$result.failure
        throw 'Official download worker reported a failure.'
    }
    $diagnostic.status='completed'
}
function Complete-OriginalDesktopPreparationFailure([Collections.IDictionary]$Evidence,$Context) {
    $Evidence.status='blocked'
    # Keep the failed operation's stage even if resource closure itself fails.
    if ($null -eq $Context) { return }
    try { Close-OriginalDesktopPackage $Context; $Evidence.cleanup='completed' }
    catch { $Evidence.cleanup='unconfirmed'; throw }
}
function New-OriginalDesktopPackage([string]$Work,[string]$BridgeExecutable,[Collections.IDictionary]$PreparationEvidence=$null) {
    $ErrorActionPreference='Stop'
    if ($null -eq $PreparationEvidence) { $PreparationEvidence=[ordered]@{} }
    $PreparationEvidence.Clear()
    $PreparationEvidence.schema=1;$PreparationEvidence.status='incomplete';$PreparationEvidence.stage='preflight';$PreparationEvidence.cleanup='not-needed';$PreparationEvidence.failure=$null;$PreparationEvidence.download=$null
    $context=$null
    try {
    Assert-OriginalDesktopPackageRunner
    Assert-PublicRegularPath $BridgeExecutable
    $BridgeName=[IO.Path]::GetFileName($BridgeExecutable)
    if (-not [IO.Path]::IsPathRooted($Work) -or [IO.Path]::GetFullPath($Work) -cne $Work -or
        $BridgeName -cnotmatch '^[A-Za-z0-9._-]+\.exe\z' -or
        -not $Work.StartsWith(([IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\')+'\'),[StringComparison]::OrdinalIgnoreCase) -or
        (Test-Path -LiteralPath $Work)) { throw 'Fresh private runner directory required.' }
    if (@(Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction Stop).Count -ne 0 -or
        (Test-Path -LiteralPath (Join-Path $env:USERPROFILE '.codex'))) { throw 'Preexisting Desktop/profile blocks preparation.' }
    $aliases=Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps'
    if ((Test-Path -LiteralPath $aliases) -and
        @(Get-ChildItem -LiteralPath $aliases -Filter 'chatgpt-desktop.exe' -Force -Recurse -Depth 1).Count -ne 0) {
        throw 'Preexisting alias blocks preparation.'
    }
    Assert-EntryFresh (Read-EntryUserEnvironment) (Test-EntryProcessOverride) (Test-EntryMachineOverride) $false $false
    $context=[ordered]@{Prepared=$false;Closed=$false;ActivationAttempted=$false;StartedTicks=[DateTime]::UtcNow.Ticks;
        BridgeName=$BridgeName;BridgeExecutable=$BridgeExecutable;BridgeHash='';ImageObserver=$null;NativeExecutable='';ActivationFloor=[long]0;App=$null;Identity=$null;Aumid='';DesktopExecutable='';Service=$null;ActivationHost=$null;
        ActivationProcess=$null;ActivationHeld=$null;ActivationCreatedTicks=[long]0;
        Streams=[Collections.Generic.List[IO.Stream]]::new();Files=[Collections.Generic.List[object]]::new();
        Evidence=[ordered]@{schema=1;scope='official-package-preparation-only';prepared=$false;activated=$false;desktopAccepted=$false;backendHandshakeVerified=$false}}
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'adapter-lock'
        $bridgeStream=[IO.File]::Open($BridgeExecutable,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
        $context.Streams.Add($bridgeStream)
        $context.BridgeHash=Get-BoundedStreamDigest $bridgeStream $bridgeStream.Length 536870912 ([Diagnostics.Stopwatch]::StartNew())
        $context.Files.Add([pscustomobject]@{Stream=$bridgeStream;Hash=$context.BridgeHash;Bytes=$bridgeStream.Length})
        New-Item -ItemType Directory -Path $Work -ErrorAction Stop | Out-Null
        $package=Join-Path $Work 'ChatGPT-x64.msix'; $license=Join-Path $Work 'ChatGPT-License.xml'
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'package-download'
        Invoke-OriginalDesktopDownload 'package' $package $PreparationEvidence
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'license-download'
        Invoke-OriginalDesktopDownload 'license' $license $PreparationEvidence
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'package-path'
        Assert-PublicRegularPath $package
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'license-path'
        Assert-PublicRegularPath $license
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'package-open'
        $packageStream=[IO.File]::Open($package,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
        $context.Streams.Add($packageStream)
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'package-digest'
        $hash=Get-BoundedStreamDigest $packageStream $packageStream.Length 4294967296 ([Diagnostics.Stopwatch]::StartNew())
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'package-hash-match'
        if ($hash -cne '9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c') {
            throw 'Official pinned package digest mismatch; review required before installation.'
        }
        $context.Files.Add([pscustomobject]@{Stream=$packageStream;Hash=$hash;Bytes=$packageStream.Length})
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'license-open'
        $licenseStream=[IO.File]::Open($license,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
        $context.Streams.Add($licenseStream)
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'signature-verification'
        $sdk=Join-Path ([Environment]::GetEnvironmentVariable('ProgramFiles(x86)')) 'Windows Kits\10\bin'
        $signtool=Get-ChildItem -LiteralPath $sdk -Filter signtool.exe -Recurse |
            Where-Object { $_.FullName -match '\\x64\\signtool.exe$' } | Sort-Object FullName -Descending | Select-Object -First 1
        if ($null -eq $signtool) { throw 'Official signature verifier unavailable.' }
        $null=& $signtool.FullName verify /pa /v $package 2>&1; $signatureExit=$LASTEXITCODE
        $signature=Get-AuthenticodeSignature -LiteralPath $package
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'signed-package-identity'
        $context.Identity=Get-DesktopPackageIdentityEvidence $package $hash $signatureExit ([string]$signature.Status) `
            $signature.SignerCertificate.Subject -RequireEntryService
        Assert-Desktop4239Identity $context.Identity
        if ($context.Identity.sandboxService.status -cne 'observed') { throw 'Signed service identity unavailable.' }
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'interactive-session'
        Assert-OriginalDesktopInteractive
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'licensed-provisioning'
        $provisioned=Add-AppxProvisionedPackage -Online -PackagePath $package -LicensePath $license -Regions all
        if ($provisioned.RestartNeeded) { throw 'Official provisioning requires restart.' }
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'registration-readiness'
        $watch=[Diagnostics.Stopwatch]::StartNew()
        $ready=Wait-DesktopRegistration 'OpenAI.Codex' $context.Identity.manifest.version $context.Identity.manifest.publisher `
            { Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction Stop } { $watch.ElapsedMilliseconds } `
            { param($Milliseconds) Start-Sleep -Milliseconds $Milliseconds }
        if ($ready.status -cne 'ready') { throw ('Official registration blocked: '+$ready.reason) }
        $context.App=$ready.package
        Assert-OriginalDesktopRegistration $context.App $context.Identity
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'installed-image-verification'
        Add-Type -AssemblyName System.IO.Compression
        $packageStream.Position=0
        $zip=[IO.Compression.ZipArchive]::new($packageStream,[IO.Compression.ZipArchiveMode]::Read,$true)
        try {
            $manifestPath=Open-OriginalDesktopInstalledFile $context 'AppxManifest.xml' $context.Identity.manifest.sha256 $zip.GetEntry('AppxManifest.xml').Length
            $context.NativeExecutable=Open-OriginalDesktopInstalledFile $context 'app/resources/codex.exe' $context.Identity.backend.sha256 $context.Identity.backend.bytes
            $null=Open-OriginalDesktopInstalledFile $context 'app/resources/app.asar' $context.Identity.archive.sha256 $context.Identity.archive.bytes
            $exeHash=Get-PublicZipEntryDigest $zip 'app/ChatGPT.exe' 536870912 ([Diagnostics.Stopwatch]::StartNew())
            $context.DesktopExecutable=Open-OriginalDesktopInstalledFile $context 'app/ChatGPT.exe' $exeHash $zip.GetEntry('app/ChatGPT.exe').Length
            $settings=[Xml.XmlReaderSettings]::new();$settings.DtdProcessing=[Xml.DtdProcessing]::Prohibit;$settings.XmlResolver=$null;$settings.MaxCharactersInDocument=262144
            $reader=[Xml.XmlReader]::Create($manifestPath,$settings)
            try { $manifest=[Xml.XmlDocument]::new();$manifest.XmlResolver=$null;$manifest.Load($reader) } finally { $reader.Dispose() }
            $apps=@($manifest.SelectNodes("//*[local-name()='Applications']/*[local-name()='Application']") | Where-Object {
                $_.GetAttribute('Executable').Replace('\','/') -ceq 'app/ChatGPT.exe' -and $_.GetAttribute('EntryPoint') -ceq 'Windows.FullTrustApplication' })
            if ($apps.Count -ne 1 -or $apps[0].GetAttribute('Id') -cnotmatch '^[A-Za-z0-9._-]{1,64}\z') { throw 'Official Desktop application is ambiguous.' }
            $context.Aumid=$context.App.PackageFamilyName+'!'+$apps[0].GetAttribute('Id')
        } finally { $zip.Dispose() }
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'activation-host-build'
        $context.ActivationHost=New-EntryActivationHost (Join-Path $Work 'entry-activation-control.exe')
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'activation-host-self-test'
        $selfTest=Invoke-EntryBoundedHelper (New-EntryControlStart $context.ActivationHost.Path 'self-test') 5000
        if ($selfTest -cne 'entry-host-ready') { throw 'Activation host self-test failed before Connect.' }
        $context.Prepared=$true
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'current-context-verification'
        Assert-OriginalDesktopPackageCurrent $context
        Assert-OriginalDesktopPackageFresh $context $PreparationEvidence
        $context.Evidence.prepared=$true
        Set-OriginalDesktopPreparationStage $PreparationEvidence 'prepared'
        $PreparationEvidence.status='prepared'
        return $context
    } catch {
        if ($null -eq $PreparationEvidence.failure) { $PreparationEvidence.failure=Get-OriginalDesktopFailureEvidence $_ }
        Complete-OriginalDesktopPreparationFailure $PreparationEvidence $context
        throw
    }
}
function Invoke-OriginalDesktopPackageActivation($Context) {
    if ($Context.ActivationAttempted) { throw 'Desktop activation is single-attempt only.' }
    Assert-OriginalDesktopPackageCurrent $Context
    Assert-OriginalDesktopPackageFresh $Context
    if ($null -ne $Context.Service) { Assert-EntryVerifiedServiceCurrent $Context.Service }
    $Context.ImageObserver=New-OriginalDesktopImageObserver $Context
    $Context.ActivationAttempted=$true
    $floor=[DateTime]::UtcNow.Ticks
    $Context.ActivationFloor=$floor
    $result=Invoke-EntryActivation $Context.ActivationHost.Path $Context.Aumid
    # Retain handles immediately; never later reopen a PID for cleanup authority.
    $Context.ActivationProcess=[Diagnostics.Process]::GetProcessById($result.processId)
    $null=$Context.ActivationProcess.Handle
    $Context.ActivationHeld=[DesktopEntryReadOnly.Held]::new($result.processId)
    $held=$Context.ActivationHeld
    $Context.ActivationCreatedTicks=$held.CreatedUtcTicks
    Assert-OriginalDesktopActivationBinding $held $Context.ActivationProcess.StartTime.ToUniversalTime().Ticks `
        $floor $Context.App.PackageFullName $Context.DesktopExecutable
    $Context.Evidence.activated=$true
    return $Context.ActivationProcess
}
function New-OriginalDesktopImageObserver($Context) {
    Initialize-EntryImageObserver
    return [DesktopEntryImages.Observer]::new($Context.BridgeExecutable,$Context.NativeExecutable)
}
function Get-OriginalDesktopImageEvidence($Context) {
    if ($null -eq $Context.ImageObserver -or -not $Context.Evidence.activated) {
        return [ordered]@{status='unavailable';adapterDirectChildren=0;nativeDirectChildren=0;backendHandshakeVerified=$false}
    }
    $bridge=0;$native=0
    foreach ($image in $Context.ImageObserver.Snapshot()) {
        if ($image.ParentId -ne $Context.ActivationProcess.Id -or $image.CreatedTicks -lt $Context.ActivationFloor -or
            $image.ParentCreatedTicks -ne $Context.ActivationCreatedTicks) { continue }
        if ($image.Path -ieq $Context.BridgeExecutable -and
            (Get-FileHash -Algorithm SHA256 -LiteralPath $Context.BridgeExecutable).Hash.ToLowerInvariant() -ceq $Context.BridgeHash) { $bridge++ }
        if ($image.Path -ieq $Context.NativeExecutable -and
            (Get-FileHash -Algorithm SHA256 -LiteralPath $Context.NativeExecutable).Hash.ToLowerInvariant() -ceq $Context.Identity.backend.sha256) { $native++ }
    }
    return [ordered]@{status='snapshot';adapterDirectChildren=$bridge;nativeDirectChildren=$native;
        misses=$Context.ImageObserver.Misses;saturated=$Context.ImageObserver.Full;backendHandshakeVerified=$false;
        limit='Exact direct-child image observations only. Missing events do not prove absence; neither startup nor fallback proves a handshake.'}
}
function Assert-OriginalDesktopActivationBinding($Held,[long]$ProcessTicks,[long]$Floor,[string]$Package,[string]$Image) {
    if (-not $Held.Alive -or $Held.CreatedUtcTicks -lt $Floor -or $Held.CreatedUtcTicks -ne $ProcessTicks -or
        $Held.PackageRelation($Package) -cne 'expected' -or $Held.ImagePath -ine $Image -or -not $Held.Alive) {
        throw 'Activated Desktop identity unavailable.'
    }
}
function Close-OriginalDesktopPackage($Context) {
    # Release query handles/locks only. Never stop the package service, remove an
    # installed package, erase a journal, restore USER values, or kill a found PID.
    if ($null -eq $Context -or $Context.Closed) { return }
    $failed=$false
    $resources=@($Context.ImageObserver,$Context.ActivationHeld,$Context.ActivationProcess)
    if ($null -ne $Context.Service) { $resources+=@($Context.Service.ImageStream,$Context.Service.Held) }
    if ($null -ne $Context.ActivationHost) { $resources+=@($Context.ActivationHost.Stream) }
    $resources+=@($Context.Streams)
    foreach ($resource in $resources) {
        if ($null -ne $resource) { try { $resource.Dispose() } catch { $failed=$true } }
    }
    $Context.Closed=$true
    if ($failed) { throw 'Package observation resource closure unconfirmed.' }
}
