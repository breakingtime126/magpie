# Pure bounded verification copied unchanged from test-desktop-cold-start.ps1.
# Parity tests prevent drift; importing this file performs no runtime action.
function Assert-PublicHash($value) {
    if ($value -isnot [string] -or $value -cnotmatch '^[0-9a-f]{64}\z') { throw 'Invalid public catalog digest.' }
}

function Get-BoundedStreamDigest([IO.Stream]$stream, [long]$expected, [long]$maximum, [Diagnostics.Stopwatch]$watch) {
    if ($expected -lt 1 -or $expected -gt $maximum) { throw 'Public source size is outside its bound.' }
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try {
        $buffer = New-Object byte[] 65536
        [long]$total = 0
        while (($count = $stream.Read($buffer, 0, $buffer.Length)) -ne 0) {
            $total += $count
            if ($total -gt $expected -or $watch.ElapsedMilliseconds -gt 120000) {
                throw 'Public source streaming bound exceeded.'
            }
            [void]$algorithm.TransformBlock($buffer, 0, $count, $buffer, 0)
        }
        if ($total -ne $expected -or $watch.ElapsedMilliseconds -gt 120000) { throw 'Public source length or time mismatch.' }
        [void]$algorithm.TransformFinalBlock((New-Object byte[] 0), 0, 0)
        return [BitConverter]::ToString($algorithm.Hash).Replace('-', '').ToLowerInvariant()
    } finally { $algorithm.Dispose() }
}

function Assert-PublicRegularPath([string]$path) {
    if (-not [IO.Path]::IsPathRooted($path) -or [IO.Path]::GetFullPath($path) -cne $path) {
        throw 'Public package path is not canonical.'
    }
    $root = [IO.Path]::GetPathRoot($path)
    $current = $root
    $parts = $path.Substring($root.Length).Split([char[]]@([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar),
        [StringSplitOptions]::RemoveEmptyEntries)
    foreach ($part in $parts) {
        $current = Join-Path $current $part
        $item = Get-Item -Force -LiteralPath $current -ErrorAction Stop
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Public package path is redirected.' }
    }
    if ($item.PSIsContainer) { throw 'Public package is not a regular file.' }
}

function Assert-PublicZipIndex($zip) {
    if ($zip.Entries.Count -gt 100000) { throw 'Public package ZIP entry count exceeds its bound.' }
    $seen = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    $directories = New-Object 'System.Collections.Generic.Dictionary[string,bool]' ([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $zip.Entries) {
        $name = $entry.FullName
        if ([string]::IsNullOrEmpty($name) -or $name.Length -gt 1024 -or
            $name.StartsWith('/') -or $name -match '[\\:\x00-\x1f\x7f]') {
            throw 'Public package ZIP path is not canonical.'
        }
        $directory = $name.EndsWith('/')
        $canonical = $name
        if ($directory) { $canonical = $name.Substring(0, $name.Length - 1) }
        foreach ($part in $canonical.Split('/')) {
            if ($part.Length -eq 0 -or $part -ceq '.' -or $part -ceq '..' -or $part -match '[. ]\z') {
                throw 'Public package ZIP path has an unsafe component.'
            }
        }
        if (-not $seen.Add($canonical)) { throw 'Public package ZIP paths are ambiguous.' }
        $directories.Add($canonical, $directory)
        $unixType = ($entry.ExternalAttributes -shr 16) -band 61440
        if (($entry.ExternalAttributes -band 1024) -ne 0 -or
            ($directory -and $unixType -ne 0 -and $unixType -ne 16384) -or
            (-not $directory -and $unixType -ne 0 -and $unixType -ne 32768) -or
            (-not $directory -and ($entry.ExternalAttributes -band 16) -ne 0)) {
            throw 'Public package ZIP contains a link or nonregular entry.'
        }
    }
    foreach ($name in $directories.Keys) {
        $parent = $name
        while (($separator = $parent.LastIndexOf('/')) -ge 0) {
            $parent = $parent.Substring(0, $separator)
            if ($directories.ContainsKey($parent) -and -not $directories[$parent]) {
                throw 'Public package ZIP file conflicts with a descendant path.'
            }
        }
    }
}

function Get-PublicZipEntryDigest($zip, [string]$path, [long]$maximum, [Diagnostics.Stopwatch]$watch) {
    # Exact and case-insensitive uniqueness both matter on the installed Windows filesystem.
    $matches = @($zip.Entries | Where-Object { [StringComparer]::OrdinalIgnoreCase.Equals($_.FullName, $path) })
    if ($matches.Count -ne 1 -or $matches[0].FullName -cne $path) { throw 'Public source ZIP entry is missing or ambiguous.' }
    $entry = $matches[0]
    $unixType = ($entry.ExternalAttributes -shr 16) -band 61440
    if (($unixType -ne 0 -and $unixType -ne 32768) -or
        ($entry.ExternalAttributes -band 1040) -ne 0 -or $entry.CompressedLength -gt $maximum) {
        throw 'Public source ZIP entry is not a regular bounded file.'
    }
    if ($entry.Length -lt 1 -or $entry.Length -gt $maximum) { throw 'Public source ZIP size exceeds its bound.' }
    $stream = $entry.Open()
    try { return Get-BoundedStreamDigest $stream $entry.Length $maximum $watch }
    finally { $stream.Dispose() }
}

function Get-DesktopPackageIdentityEvidence([string]$PackagePath, [string]$PackageSha256,
    [int]$SignatureExitCode, [string]$SignatureStatus, [string]$SignerSubject, [switch]$RequireEntryService) {
    # Data only. No extraction, installed-package discovery, executable invocation
    # or ASAR source parsing. Keep the same read-locked file through hash and ZIP reads.
    $packageStream = $null; $zip = $null
    try {
        if ($SignatureExitCode -ne 0 -or $SignatureStatus -cne 'Valid' -or
            $SignerSubject -cne 'CN=50BDFD77-8903-4850-9FFE-6E8522F64D5B') {
            throw 'Package identity signature gate failed.'
        }
        Assert-PublicHash $PackageSha256
        Assert-PublicRegularPath $PackagePath
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $packageStream = [IO.File]::Open($PackagePath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        $actualPackage = Get-BoundedStreamDigest $packageStream $packageStream.Length 4294967296 $watch
        if ($actualPackage -cne $PackageSha256) { throw 'Package identity input changed.' }
        $packageStream.Position = 0
        Add-Type -AssemblyName System.IO.Compression
        $zip = New-Object IO.Compression.ZipArchive($packageStream, [IO.Compression.ZipArchiveMode]::Read, $true)
        Assert-PublicZipIndex $zip
        $manifestHash = Get-PublicZipEntryDigest $zip 'AppxManifest.xml' 262144 $watch
        $manifestEntry = $zip.GetEntry('AppxManifest.xml')
        $settings = New-Object Xml.XmlReaderSettings
        $settings.DtdProcessing = [Xml.DtdProcessing]::Prohibit
        $settings.XmlResolver = $null
        $settings.MaxCharactersInDocument = 262144
        $manifestStream = $manifestEntry.Open()
        $reader = $null
        try {
            $reader = [Xml.XmlReader]::Create($manifestStream, $settings)
            $document = New-Object Xml.XmlDocument
            $document.XmlResolver = $null
            $document.Load($reader)
        } finally {
            if ($reader) { $reader.Dispose() }
            $manifestStream.Dispose()
        }
        $identities = @($document.SelectNodes("/*[local-name()='Package' and namespace-uri()='http://schemas.microsoft.com/appx/manifest/foundation/windows10']/*[local-name()='Identity' and namespace-uri()='http://schemas.microsoft.com/appx/manifest/foundation/windows10']"))
        if ($identities.Count -ne 1) { throw 'Package identity is missing or ambiguous.' }
        $identity = $identities[0]
        $name = $identity.GetAttribute('Name'); $publisher = $identity.GetAttribute('Publisher')
        $version = $identity.GetAttribute('Version'); $architecture = $identity.GetAttribute('ProcessorArchitecture')
        if ($name -cne 'OpenAI.Codex' -or $publisher -cne $SignerSubject -or $architecture -cne 'x64' -or
            $version -cnotmatch '^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}\z') {
            throw 'Package identity fields are outside the public contract.'
        }
        foreach ($component in $version.Split('.')) {
            if ([int]$component -gt 65535) { throw 'Package identity version is out of range.' }
        }
        $backendHash = Get-PublicZipEntryDigest $zip 'app/resources/codex.exe' 536870912 $watch
        $archiveHash = Get-PublicZipEntryDigest $zip 'app/resources/app.asar' 2147483648 $watch
        $serviceEvidence=$null
        if ($RequireEntryService) {
            # Reuse this same verified, read-locked MSIX. No installed file or guessed digest is a source.
            $serviceEvidence=[ordered]@{status='unavailable';reason='service_declaration_unavailable'}
            try {
                $declaration=Get-EntryServiceDeclaration $document
                $serviceEvidence.reason='service_digest'
                $serviceHash=Get-PublicZipEntryDigest $zip $declaration.path 536870912 $watch
                $serviceEvidence=[ordered]@{status='observed';name=$declaration.name;path=$declaration.path;
                    startupType=$declaration.startupType;startupTypeSource=$declaration.startupTypeSource;
                    startAccount=$declaration.startAccount;startAccountSource=$declaration.startAccountSource;
                    arguments=$declaration.arguments;argumentsSource=$declaration.argumentsSource;
                    bytes=$zip.GetEntry($declaration.path).Length;sha256=$serviceHash}
            } catch {
                # Publish only these fixed labels; never an unknown attribute, path or raw exception.
                if ($_.Exception.Message -cin @('service_missing','service_ambiguous','service_namespace','service_location',
                    'service_path','service_name','service_startup','service_account','service_arguments')) {
                    $serviceEvidence.reason=$_.Exception.Message
                }
            }
        }
        if ($watch.ElapsedMilliseconds -gt 120000) { throw 'Package identity time limit exceeded.' }
        $evidence=[ordered]@{
            schema=1; status='observed'; scope='signed-package-static-identity-only'; runtimeAccepted=$false
            packageSha256=$actualPackage; packageBytes=$packageStream.Length
            manifest=[ordered]@{name=$name;publisher=$publisher;version=$version;architecture=$architecture;sha256=$manifestHash}
            backend=[ordered]@{path='app/resources/codex.exe';bytes=$zip.GetEntry('app/resources/codex.exe').Length;sha256=$backendHash}
            archive=[ordered]@{path='app/resources/app.asar';bytes=$zip.GetEntry('app/resources/app.asar').Length;sha256=$archiveHash}
        }
        if ($RequireEntryService) { $evidence.sandboxService=$serviceEvidence }
        return $evidence
    } catch { throw 'Read-only signed package identity is unavailable.' }
    finally {
        if ($zip) { $zip.Dispose() }
        if ($packageStream) { $packageStream.Dispose() }
    }
}

function Assert-Desktop4239Identity($Evidence) {
    if ($Evidence.status -cne 'observed' -or $Evidence.manifest.version -cne '26.1103.4239.0' -or
        $Evidence.packageSha256 -cne '9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c' -or
        $Evidence.backend.sha256 -cne '4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382' -or
        $Evidence.archive.sha256 -cne 'fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa') {
        throw 'Reviewed 4239 package and native/source identities do not match.'
    }
}

function Wait-DesktopRegistration(
    [string]$ExpectedName, [string]$ExpectedVersion, [string]$ExpectedPublisher,
    [scriptblock]$ReadPackages, [scriptblock]$ReadElapsedMilliseconds, [scriptblock]$Delay
) {
    $attempts = 0; $previousElapsed = -1L
    $fullName = ''; $familyName = ''; $installLocation = ''
    $lastStatus = 'not_read'
    $reason = 'registration_read_failed'
    try {
        for ($slot = 0; $slot -lt 31; $slot++) {
            $reason = 'registration_clock_failed'
            $elapsed = & $ReadElapsedMilliseconds
            if (($elapsed -isnot [long] -and $elapsed -isnot [int]) -or
                $elapsed -lt 0 -or $elapsed -lt $previousElapsed) { throw 'clock' }
            $previousElapsed = $elapsed
            if ($elapsed -ge 60000) { $reason = 'registration_readiness_timeout'; break }
            $reason = 'registration_read_failed'
            $packages = @(& $ReadPackages); $attempts++
            if ($packages.Count -eq 0) { $reason = 'registration_missing'; break }
            if ($packages.Count -ne 1) { $reason = 'registration_ambiguous'; break }
            $candidate = $packages[0]
            $reason = 'registration_identity_mismatch'
            if ($null -eq $candidate -or $candidate.Name -isnot [string] -or
                ($candidate.Version -isnot [string] -and $candidate.Version -isnot [version]) -or
                $candidate.Publisher -isnot [string] -or $candidate.Name -cne $ExpectedName -or
                [string]$candidate.Version -cne $ExpectedVersion -or $candidate.Publisher -cne $ExpectedPublisher) { break }
            foreach ($field in @('PackageFullName','PackageFamilyName','InstallLocation')) {
                if ($candidate.$field -isnot [string] -or [string]::IsNullOrWhiteSpace($candidate.$field) -or
                    $candidate.$field.Length -gt 4096) { throw 'identity' }
            }
            if ($attempts -eq 1) {
                $fullName=$candidate.PackageFullName; $familyName=$candidate.PackageFamilyName
                $installLocation=$candidate.InstallLocation
            } elseif ($candidate.PackageFullName -cne $fullName -or $candidate.PackageFamilyName -cne $familyName -or
                $candidate.InstallLocation -cne $installLocation) { break }
            $reason = 'registration_status_unreviewed'
            if ($null -eq $candidate.Status -or ($candidate.Status -isnot [string] -and
                -not $candidate.Status.GetType().IsEnum)) { break }
            $status = [string]$candidate.Status
            if (@('Ok','DeploymentInProgress','Servicing','DeploymentInProgress, Servicing') -cnotcontains $status) { break }
            $lastStatus = $status
            $reason = 'registration_clock_failed'
            $elapsed = & $ReadElapsedMilliseconds
            if (($elapsed -isnot [long] -and $elapsed -isnot [int]) -or $elapsed -lt $previousElapsed) { throw 'clock' }
            $previousElapsed = $elapsed
            if ($elapsed -ge 60000) { $reason = 'registration_readiness_timeout'; break }
            if ($status -ceq 'Ok') {
                return [pscustomobject]@{ status='ready'; reason='registration_ready'; attempts=$attempts;
                    lastStatus=$lastStatus; package=$candidate }
            }
            $reason = 'registration_wait_failed'
            $null = & $Delay ([int][Math]::Min(2000,60000-$elapsed))
            $reason = 'registration_readiness_timeout'
        }
    } catch { }
    return [pscustomobject]@{ status='blocked'; reason=$reason; attempts=$attempts; lastStatus=$lastStatus; package=$null }
}
