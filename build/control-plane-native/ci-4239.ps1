# Explicit disposable-CI entry only. No Desktop registration, launch or UI proof.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$HelperExecutable,
    [Parameter(Mandatory)][string]$BridgeExecutable,
    [Parameter(Mandatory)][string]$ReportDirectory,
    [string]$AcceptanceProfile = ''
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-StandaloneIdentityHelpers([string]$ProbePath) {
    # Import definitions only. Never dot-source/execute the Desktop probe.
    $tokens = $null; $errors = $null
    $ast = [Management.Automation.Language.Parser]::ParseFile($ProbePath, [ref]$tokens, [ref]$errors)
    if ($errors.Count -ne 0) { throw 'Package identity helper syntax failed.' }
    $definitions = foreach ($name in @('Assert-PublicHash', 'Assert-PublicRegularPath',
        'Get-BoundedStreamDigest', 'Assert-PublicZipIndex', 'Get-PublicZipEntryDigest',
        'Get-DesktopPackageIdentityEvidence', 'Assert-Desktop4239Identity')) {
        $matches = @($ast.FindAll({ param($node)
            $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
        }, $true))
        if ($matches.Count -ne 1) { throw 'Package identity helper is missing or ambiguous.' }
        $matches[0].Extent.Text
    }
    return [scriptblock]::Create(($definitions -join "`n"))
}

function Save-StandalonePublicInput([string]$Url, [string]$Destination, [long]$Maximum) {
    # Only public package/catalog URLs are supplied below. No credentials/cookies.
    $client = [Net.Http.HttpClient]::new()
    $cancel = [Threading.CancellationTokenSource]::new(180000)
    $response = $null; $inputStream = $null; $outputStream = $null
    try {
        $response = $client.GetAsync($Url, [Net.Http.HttpCompletionOption]::ResponseHeadersRead,
            $cancel.Token).GetAwaiter().GetResult()
        [void]$response.EnsureSuccessStatusCode()
        if ($response.Content.Headers.ContentLength -gt $Maximum) { throw 'Public download exceeds its bound.' }
        $inputStream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
        $outputStream = [IO.File]::Open($Destination, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $buffer = [byte[]]::new(65536); [long]$total = 0
        while (($count = $inputStream.ReadAsync($buffer, 0, $buffer.Length, $cancel.Token).GetAwaiter().GetResult()) -ne 0) {
            $total += $count
            if ($total -gt $Maximum) { throw 'Public download exceeds its bound.' }
            $outputStream.Write($buffer, 0, $count)
        }
        if ($total -eq 0 -or $cancel.IsCancellationRequested) { throw 'Public download is empty or expired.' }
    } finally {
        if ($outputStream) { $outputStream.Dispose() }
        if ($inputStream) { $inputStream.Dispose() }
        if ($response) { $response.Dispose() }
        $client.Dispose(); $cancel.Dispose()
    }
}

function Get-StandaloneSignatureExit([string]$PackagePath) {
    $sdk = Join-Path ([Environment]::GetEnvironmentVariable('ProgramFiles(x86)')) 'Windows Kits/10/bin'
    $verifier = Get-ChildItem -LiteralPath $sdk -Filter signtool.exe -Recurse |
        Where-Object { $_.FullName -match '\\x64\\signtool.exe$' } |
        Sort-Object FullName -Descending | Select-Object -First 1
    if (-not $verifier) { throw 'Signature verifier unavailable.' }
    Assert-PublicRegularPath $verifier.FullName
    $psi = [Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $verifier.FullName; $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $psi.WorkingDirectory = [IO.Path]::GetDirectoryName($PackagePath)
    $psi.Environment.Clear()
    $systemRoot = [Environment]::GetEnvironmentVariable('SystemRoot')
    if (-not [IO.Path]::IsPathFullyQualified($systemRoot)) { throw 'System root unavailable.' }
    $psi.Environment['SystemRoot'] = $systemRoot; $psi.Environment['WINDIR'] = $systemRoot
    $psi.Environment['PATH'] = Join-Path $systemRoot 'System32'
    $psi.Environment['TEMP'] = $psi.WorkingDirectory; $psi.Environment['TMP'] = $psi.WorkingDirectory
    foreach ($argument in @('verify', '/pa', $PackagePath)) { $psi.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new(); $process.StartInfo = $psi; $started = $false
    try {
        $started = $process.Start()
        if (-not $started) { throw 'Signature verifier did not start.' }
        $stdout = $process.StandardOutput.BaseStream.CopyToAsync([IO.Stream]::Null)
        $stderr = $process.StandardError.BaseStream.CopyToAsync([IO.Stream]::Null)
        if (-not $process.WaitForExit(120000)) { throw 'Signature verifier timed out.' }
        if (-not $stdout.Wait(5000) -or -not $stderr.Wait(5000)) { throw 'Signature verifier streams did not close.' }
        return $process.ExitCode
    } finally {
        if ($started -and -not $process.HasExited) {
            $process.Kill($true)
            if (-not $process.WaitForExit(5000)) { throw 'Signature verifier cleanup failed.' }
        }
        $process.Dispose()
    }
}

function Export-Standalone4239Native([string]$PackagePath, [string]$Destination, $Evidence) {
    Assert-Desktop4239Identity $Evidence
    Assert-PublicRegularPath $PackagePath
    $packageStream = $null; $zip = $null; $entryStream = $null; $outputStream = $null
    try {
        # Rehash and read the same locked package, not an installed/guessed file.
        $packageStream = [IO.File]::Open($PackagePath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $digest = Get-BoundedStreamDigest $packageStream $packageStream.Length 4294967296 $watch
        if ($digest -cne $Evidence.packageSha256) { throw 'Verified package changed before extraction.' }
        $packageStream.Position = 0
        $zip = [IO.Compression.ZipArchive]::new($packageStream, [IO.Compression.ZipArchiveMode]::Read, $true)
        Assert-PublicZipIndex $zip
        $digest = Get-PublicZipEntryDigest $zip 'app/resources/codex.exe' 536870912 $watch
        if ($digest -cne $Evidence.backend.sha256) { throw 'Verified native entry changed.' }
        $entry = $zip.GetEntry('app/resources/codex.exe')
        $entryStream = $entry.Open()
        $outputStream = [IO.File]::Open($Destination, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        $buffer = [byte[]]::new(65536); [long]$total = 0
        while (($count = $entryStream.Read($buffer, 0, $buffer.Length)) -ne 0) {
            $total += $count
            if ($total -gt $entry.Length -or $watch.ElapsedMilliseconds -gt 120000) { throw 'Native extraction exceeded its bound.' }
            $outputStream.Write($buffer, 0, $count)
        }
        if ($total -ne $entry.Length) { throw 'Native extraction length mismatch.' }
        $outputStream.Dispose(); $outputStream = $null
        Assert-PublicRegularPath $Destination
        if ((Get-FileHash -Algorithm SHA256 -LiteralPath $Destination).Hash.ToLowerInvariant() -cne $Evidence.backend.sha256) {
            throw 'Extracted native bytes changed.'
        }
    } finally {
        if ($outputStream) { $outputStream.Dispose() }
        if ($entryStream) { $entryStream.Dispose() }
        if ($zip) { $zip.Dispose() }
        if ($packageStream) { $packageStream.Dispose() }
    }
}

function Assert-Standalone4239CI([string]$Profile) {
    if ($Profile -cne 'app-server-4239' -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:RUNNER_OS -cne 'Windows' -or $env:GITHUB_EVENT_NAME -cne 'push' -or
        $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:GITHUB_SHA -cnotmatch '^[0-9a-f]{40}\z' -or
        -not [IO.Path]::IsPathFullyQualified($env:RUNNER_TEMP)) {
        throw 'Explicit disposable CI profile required.'
    }
}

# No setup or network occurs until the exact profile and CI context pass.
Assert-Standalone4239CI $AcceptanceProfile
. (Get-StandaloneIdentityHelpers (Join-Path $PSScriptRoot '../test-desktop-cold-start.ps1'))
. (Join-Path $PSScriptRoot 'invoke.ps1')
$runnerRoot = [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd([IO.Path]::DirectorySeparatorChar)
$outputRoot = [IO.Path]::GetFullPath($ReportDirectory)
if (-not $outputRoot.StartsWith($runnerRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -or
    (Test-Path -LiteralPath $outputRoot)) { throw 'A fresh CI report directory is required.' }
[void][IO.Directory]::CreateDirectory($outputRoot)
$root = Join-Path $runnerRoot ('app-server-4239-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $root)
$report = [ordered]@{schema=1;scope='app-server-4239-synthetic-only';profile='app-server-4239';
    source_commit=$env:GITHUB_SHA;status='failed';stage='download';failure='preparation_failed';
    gui_verified=$false;package_verified=$false;preparation_cleanup_complete=$false;native=$null}
try {
    $package = Join-Path $root 'official.msix'
    Save-StandalonePublicInput 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-x64.msix' $package 4294967296
    $report.stage = 'signature'
    $signatureExit = Get-StandaloneSignatureExit $package
    $signature = Get-AuthenticodeSignature -LiteralPath $package
    $subject = ''
    if ($signature.SignerCertificate) { $subject = $signature.SignerCertificate.Subject }
    $report.stage = 'identity'
    $identity = Get-DesktopPackageIdentityEvidence $package `
        '9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c' `
        $signatureExit ([string]$signature.Status) $subject
    Assert-Desktop4239Identity $identity
    $report.package_verified = $true
    $report.stage = 'extraction'
    $codex = Join-Path $root 'codex.exe'
    Export-Standalone4239Native $package $codex $identity
    $report.stage = 'catalog'
    $catalog = Join-Path $root 'models.json'
    Save-StandalonePublicInput 'https://raw.githubusercontent.com/openai/codex/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/models-manager/models.json' $catalog 10485760
    $scratch = Join-Path $root 'scratch'; $helperHome = Join-Path $root 'helper-home'
    [void][IO.Directory]::CreateDirectory($scratch); [void][IO.Directory]::CreateDirectory($helperHome)
    $report.stage = 'acceptance'; $report.failure = 'native_acceptance_failed'
    $report.native = Invoke-ControlPlaneNative -HelperExecutable $HelperExecutable -BridgeExecutable $BridgeExecutable `
        -CodexExecutable $codex -CatalogPath $catalog -ScratchParent $scratch -HelperHome $helperHome `
        -AcceptanceProfile app-server-4239
    Assert-ControlPlaneNativeSummary $report.native -AcceptanceProfile app-server-4239
    if ($report.native.status -cne 'passed') { throw 'Native acceptance failed.' }
    $report.status = 'passed'; $report.stage = 'complete'; $report.failure = ''
} catch {
    # Only fixed labels and the wrapper-validated native summary may be published.
    $report.status = 'failed'
} finally {
    try {
        Remove-Item -LiteralPath $root -Recurse -Force
        if (Test-Path -LiteralPath $root) { throw 'Owned input cleanup incomplete.' }
        $report.preparation_cleanup_complete = $true
    } catch {
        $report.status = 'failed'; $report.stage = 'cleanup'; $report.failure = 'cleanup_failed'
    }
    [IO.File]::WriteAllText((Join-Path $outputRoot 'report.json'), ($report | ConvertTo-Json -Depth 16 -Compress), [Text.UTF8Encoding]::new($false))
}
if ($report.status -cne 'passed') { throw 'Standalone 4239 app-server acceptance failed; inspect the sanitized report.' }
Write-Output 'PASS: standalone 4239 synthetic app-server acceptance; GUI remains unverified.'
