# Disposable CI review only. Does not install or activate Desktop, run inference,
# initialize app-server, read real profiles, or change accepted runtime pins.
[CmdletBinding()]
param([switch]$Run, [switch]$RendererMenuOnly)
if ($Run -and $RendererMenuOnly) { throw 'Contract review modes are mutually exclusive.' }
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script:contractChildExitUnconfirmed=$false
$script:contractCommandResults=[Collections.Generic.List[object]]::new()
Add-Type -AssemblyName System.IO.Compression
. (Join-Path $PSScriptRoot 'desktop-live-welcome.ps1')

# Reuse pure trust/ZIP validators, never dot-source the executable harness.
$tokens=$null; $parseErrors=$null
$harnessAst=[Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'),[ref]$tokens,[ref]$parseErrors)
if ($parseErrors.Count) { throw 'Contract review validator syntax is unavailable.' }
foreach ($name in @('Assert-PublicHash','Assert-PublicRegularPath','Get-BoundedStreamDigest',
    'Assert-PublicZipIndex','Get-PublicZipEntryDigest','Get-DesktopPackageIdentityEvidence')) {
    $definitions=@($harnessAst.FindAll({param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    },$true))
    if ($definitions.Count -ne 1) { throw 'Contract review validator is unavailable.' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
function Get-ContractReviewPins {
    return [ordered]@{
        version='26.1103.4239.0'; packageBytes=1061531069
        packageSha256='9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c'
        backendSha256='4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'
        archiveSha256='fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa'
    }
}
function Assert-ContractReviewCI([string]$Actions,[string]$Runner,[string]$OS,
    [string]$Repository,[string]$Ref,[string]$Marker) {
    if ($Actions -cne 'true' -or $Runner -cne 'github-hosted' -or $OS -cne 'Windows' -or
        $Repository -cne 'breakingtime126/magpie' -or
        $Ref -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $Marker -cne 'native-contract-review-v1') { throw 'Contract review requires its disposable CI opt-in.' }
}
function Assert-ContractReviewIdentity($Identity) {
    $pins=Get-ContractReviewPins
    if ($Identity.status -cne 'observed' -or $Identity.manifest.version -cne $pins.version -or
        $Identity.packageSha256 -cne $pins.packageSha256 -or $Identity.packageBytes -ne $pins.packageBytes -or
        $Identity.backend.sha256 -cne $pins.backendSha256 -or $Identity.archive.sha256 -cne $pins.archiveSha256) {
        throw 'Reviewed contract package identity does not match.'
    }
}
function Assert-ContractGeneratedBound([string]$Directory) {
    if (-not (Test-Path -LiteralPath $Directory)) { return }
    $pending=[Collections.Generic.Queue[string]]::new(); $pending.Enqueue($Directory)
    $files=0; $bytes=0L; $directories=0
    while ($pending.Count) {
        $current=$pending.Dequeue(); $directories++
        if ($directories -gt 64) { throw 'Generated protocol directory bound exceeded.' }
        foreach ($item in @(Get-ChildItem -LiteralPath $current -Force)) {
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Generated protocol link rejected.' }
            if ($item.PSIsContainer) { $pending.Enqueue($item.FullName); continue }
            $files++; $bytes+=$item.Length
            if ($files -gt 2048 -or $item.Length -gt 8388608 -or $bytes -gt 67108864) {
                throw 'Generated protocol size bound exceeded.'
            }
        }
    }
}
function Invoke-ContractCommand([string]$Executable,[string[]]$Arguments,[string]$Scratch,
    [int]$OutputLimit=262144,[string]$GeneratedDirectory='',
    [ValidateSet('signature','native-version','native-app-server-help','native-schema-help','native-schema-generate','schema-validate','frontend-excerpts','renderer-menu-excerpts')]
    [string]$Label='signature') {
    # A new empty HOME/CODEX_HOME for every invocation. New-LiveStart clears all
    # inherited variables and copies only OS support paths, not secrets or tokens.
    $profileRoot=Join-Path $Scratch ('home-'+[guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($profileRoot)
    $temp=Join-Path $profileRoot 'tmp'; [void][IO.Directory]::CreateDirectory($temp)
    $start=New-LiveStart -Executable $Executable -Arguments $Arguments -Directory $profileRoot
    $start.Environment['TEMP']=$temp; $start.Environment['TMP']=$temp
    Initialize-LiveSupervisor
    $child=$null; $exitConfirmed=$false
    $result=[ordered]@{command=$Label;status='failed';exitCode=$null;timedOut=$null;overflow=$null
        stdoutRetainedBytes=$null;stderr='discarded_with_stream_cap';streamLimitBytes=$OutputLimit;exitConfirmed=$null}
    try {
        $child=[DesktopLive.BoundedProcess]::new($start,$OutputLimit,30000)
        $child.CloseInput()
        while (-not $child.Wait(100)) {
            if ($GeneratedDirectory) { Assert-ContractGeneratedBound $GeneratedDirectory }
            if ($child.TimedOut -or $child.Overflow) { throw 'Contract command exceeded its bound.' }
        }
        $exitConfirmed=$child.Exited
        $drain=[Diagnostics.Stopwatch]::StartNew()
        while (-not $child.Drained -and $drain.ElapsedMilliseconds -lt 2000) { Start-Sleep -Milliseconds 10 }
        if (-not $exitConfirmed -or -not $child.Drained -or $child.TimedOut -or $child.Overflow -or $child.ExitCode -ne 0) {
            throw 'Contract command did not finish cleanly.'
        }
        if ($GeneratedDirectory) { Assert-ContractGeneratedBound $GeneratedDirectory }
        $result.status='observed'
        return $child.Output
    } catch {
        if ($_.Exception.Message -match 'owned process exit (is )?unconfirmed') { $script:contractChildExitUnconfirmed=$true; $result.exitConfirmed=$false }
        elseif ($_.Exception.Message -match 'construction failed; owned process exit confirmed') { $result.exitConfirmed=$true }
        throw
    } finally {
        if ($child) {
            try { if (-not $exitConfirmed) { $child.Stop(); $exitConfirmed=$child.Wait(5000) -and $child.Exited } }
            catch { $exitConfirmed=$false }
            $result.exitConfirmed=$exitConfirmed; $result.timedOut=$child.TimedOut; $result.overflow=$child.Overflow
            $result.stdoutRetainedBytes=[Text.Encoding]::UTF8.GetByteCount($child.Output)
            if ($exitConfirmed) { $result.exitCode=$child.ExitCode }
            [void]$script:contractCommandResults.Add($result)
            if (-not $exitConfirmed) { $script:contractChildExitUnconfirmed=$true }
            $child.Dispose()
            if (-not $exitConfirmed) { $script:contractChildExitUnconfirmed=$true; throw 'Contract command owned process exit is unconfirmed.' }
        } else { [void]$script:contractCommandResults.Add($result) }
    }
}
function Export-ContractReviewInputs([string]$Package,[string]$Directory,[switch]$ArchiveOnly) {
    $pins=Get-ContractReviewPins
    Assert-PublicRegularPath $Package
    $stream=[IO.File]::Open($Package,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
    $zip=$null
    try {
        $watch=[Diagnostics.Stopwatch]::StartNew()
        if ((Get-BoundedStreamDigest $stream $stream.Length 4294967296 $watch) -cne $pins.packageSha256) {
            throw 'Review package changed before extraction.'
        }
        $stream.Position=0
        $zip=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Read,$true)
        Assert-PublicZipIndex $zip
        foreach ($item in @(
            @{path='app/resources/codex.exe';leaf='codex.exe';maximum=536870912;sha256=$pins.backendSha256},
            @{path='app/resources/app.asar';leaf='app.asar';maximum=2147483648;sha256=$pins.archiveSha256})) {
            if ($ArchiveOnly -and $item.leaf -cne 'app.asar') { continue }
            if ((Get-PublicZipEntryDigest $zip $item.path $item.maximum $watch) -cne $item.sha256) {
                throw 'Review package entry changed.'
            }
            $entry=$zip.GetEntry($item.path); $input=$entry.Open(); $output=$null
            $destination=Join-Path $Directory $item.leaf
            try {
                $output=[IO.File]::Open($destination,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
                $buffer=New-Object byte[] 65536; $total=0L
                while (($count=$input.Read($buffer,0,$buffer.Length)) -gt 0) {
                    $total+=$count
                    if ($total -gt $entry.Length -or $watch.ElapsedMilliseconds -gt 120000) { throw 'Review extraction bound exceeded.' }
                    $output.Write($buffer,0,$count)
                }
                if ($total -ne $entry.Length) { throw 'Review extraction length mismatch.' }
            } finally { if ($output) { $output.Dispose() }; $input.Dispose() }
            if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant() -cne $item.sha256) {
                throw 'Review extracted entry digest mismatch.'
            }
        }
    } finally { if ($zip) { $zip.Dispose() }; $stream.Dispose() }
}
function Invoke-DesktopContractReview {
    Assert-ContractReviewCI $env:GITHUB_ACTIONS $env:RUNNER_ENVIRONMENT $env:RUNNER_OS `
        $env:GITHUB_REPOSITORY $env:GITHUB_REF $env:MAGPIE_NATIVE_CONTRACT_REVIEW
    $out=Join-Path $env:RUNNER_TEMP 'desktop-contract-review'
    $work=Join-Path $env:RUNNER_TEMP ('desktop-contract-private-'+[guid]::NewGuid().ToString('N'))
    if (Test-Path -LiteralPath $out) { throw 'Contract review output already exists.' }
    [void][IO.Directory]::CreateDirectory($out); [void][IO.Directory]::CreateDirectory($work)
    $report=[ordered]@{schema=1;scope='official-native-contract-review';sourceCommit=$env:GITHUB_SHA
        status='blocked';stage='package';nativeStatus='not-run';frontendStatus='not-run';nativeArtifacts=@();desktopActivated=$false;bridgeAccepted=$false;inferenceAttempted=$false
        limits=@('Help and schema generation only, not app-server initialization or Desktop acceptance.',
            'Each child has a fresh empty profile, cleared inherited environment, 30 second runtime and bounded streams.',
            'Generated file sizes are cooperatively sampled, not an OS disk quota. Process exit covers the held child only.',
            'Source excerpts are bounded public implementation evidence, not a supported configuration contract.')}
    $succeeded=$false
    try {
        $pins=Get-ContractReviewPins; $package=Join-Path $work 'ChatGPT-x64.msix'
        Invoke-WebRequest -Uri 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-x64.msix' `
            -OutFile $package -TimeoutSec 180 -MaximumRetryCount 0
        if ((Get-Item -LiteralPath $package).Length -ne $pins.packageBytes -or
            (Get-FileHash -LiteralPath $package -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pins.packageSha256) {
            throw 'Contract review package pin mismatch.'
        }
        $report.stage='signature'
        $sdk=Join-Path ([Environment]::GetEnvironmentVariable('ProgramFiles(x86)')) 'Windows Kits\10\bin'
        $verifier=Get-ChildItem -LiteralPath $sdk -Filter signtool.exe -Recurse |
            Where-Object { $_.FullName -match '\\x64\\signtool.exe$' } | Sort-Object FullName -Descending | Select-Object -First 1
        if (-not $verifier) { throw 'Official package verifier unavailable.' }
        $null=Invoke-ContractCommand $verifier.FullName @('verify','/pa','/v',$package) $work -Label signature
        $signature=Get-AuthenticodeSignature -LiteralPath $package
        $identity=Get-DesktopPackageIdentityEvidence $package $pins.packageSha256 0 ([string]$signature.Status) $signature.SignerCertificate.Subject
        Assert-ContractReviewIdentity $identity
        $report.packageIdentity=$identity
        $report.signature=[ordered]@{status=[string]$signature.Status;signer=$signature.SignerCertificate.Subject;thumbprint=$signature.SignerCertificate.Thumbprint}
        $report.stage='extract'
        Export-ContractReviewInputs $package $work
        $native=Join-Path $work 'codex.exe'; $archive=Join-Path $work 'app.asar'
        $inspector=Join-Path $PSScriptRoot 'review-desktop-contract.mjs'
        try {
            $report.stage='native-version-help'
            $commands=@(
                @{file='version.txt';label='native-version';args=@('--version')},
                @{file='app-server-help.txt';label='native-app-server-help';args=@('app-server','--help')},
                @{file='schema-help.txt';label='native-schema-help';args=@('app-server','generate-json-schema','--help')})
            foreach ($command in $commands) {
                if ((Get-FileHash -LiteralPath $native -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pins.backendSha256) { throw 'Review native digest changed.' }
                $text=Invoke-ContractCommand $native $command.args $work -Label $command.label
                if ($command.file -ceq 'version.txt' -and $text.Trim() -cnotmatch '^codex-cli [0-9][A-Za-z0-9.+-]{0,80}$') { throw 'Native version format unavailable.' }
                if ($command.file -ceq 'schema-help.txt' -and (-not $text.Contains('--experimental') -or -not $text.Contains('--out'))) {
                    throw 'Native schema generation flags are unavailable.'
                }
                [IO.File]::WriteAllText((Join-Path $out $command.file),$text,[Text.UTF8Encoding]::new($false))
                $report.nativeArtifacts += $command.file
            }
            $report.stage='native-protocol'
            $schemas=Join-Path $work 'schema'; [void][IO.Directory]::CreateDirectory($schemas)
            if ((Get-FileHash -LiteralPath $native -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pins.backendSha256) { throw 'Review native digest changed.' }
            $null=Invoke-ContractCommand $native @('app-server','generate-json-schema','--experimental','--out',$schemas) $work 262144 $schemas -Label native-schema-generate
            $node=(Get-Command node -CommandType Application -ErrorAction Stop).Source
            $manifest=Invoke-ContractCommand $node @('--max-old-space-size=512',$inspector,'--schemas',$schemas) $work 1048576 -Label schema-validate
            # Only validated generated JSON is promoted; never package binaries, ASAR,
            # HOME, stderr, arbitrary files, or an incomplete generation directory.
            $schemaManifest=$manifest | ConvertFrom-Json
            $publication=Join-Path $work 'validated-schema'; [void][IO.Directory]::CreateDirectory($publication)
            foreach ($file in $schemaManifest.files) {
                $source=Join-Path $schemas $file.path; $destination=Join-Path $publication $file.path
                [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($destination))
                if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant() -cne $file.sha256) { throw 'Generated protocol changed before publication.' }
                [IO.File]::Copy($source,$destination,$false)
            }
            [IO.Directory]::Move($publication,(Join-Path $out 'schema'))
            [IO.File]::WriteAllText((Join-Path $out 'schema-manifest.json'),$manifest,[Text.UTF8Encoding]::new($false))
            $report.nativeStatus='observed'; $report.protocolFileCount=@($schemaManifest.files).Count
        } catch {
            $report.nativeStatus='blocked'; $report.nativeFailedStage=$report.stage
        }
        if (-not $script:contractChildExitUnconfirmed) {
            try {
                $report.stage='frontend-contract'
                $node=(Get-Command node -CommandType Application -ErrorAction Stop).Source
                $excerpts=Invoke-ContractCommand $node @('--max-old-space-size=512',$inspector,'--archive',$archive) $work 1048576 -Label frontend-excerpts
                [IO.File]::WriteAllText((Join-Path $out 'frontend-contract.json'),$excerpts,[Text.UTF8Encoding]::new($false))
                $frontend=$excerpts | ConvertFrom-Json
                $report.frontendStatus=$(if ($frontend.requiredAnchorsPresent) { 'observed' } else { 'partial-missing-anchors' })
            } catch { $report.frontendStatus='blocked' }
        } else { $report.frontendStatus='not-run-cleanup-unconfirmed' }
        $report.ownedChildExitUnconfirmed=$script:contractChildExitUnconfirmed
        $report.stage='contract-artifacts'
        if ($report.nativeStatus -ceq 'observed' -and $report.frontendStatus -ceq 'observed') {
            $report.status='observed'; $succeeded=$true
        } elseif ($report.nativeArtifacts.Count -gt 0 -or $report.frontendStatus -cin @('observed','partial-missing-anchors')) { $report.status='partial' }
    } catch {
        $report.failure='review_unavailable_or_bound_exceeded'
        # No raw exception, process output or profile data is printed.
    } finally {
        $report.ownedChildExitUnconfirmed=$script:contractChildExitUnconfirmed
        $report.commands=@($script:contractCommandResults.ToArray())
        [IO.File]::WriteAllText((Join-Path $out 'report.json'),($report | ConvertTo-Json -Depth 10),[Text.UTF8Encoding]::new($false))
        Write-Output ('CONTRACT_REVIEW_STATUS='+$report.status+';STAGE='+$report.stage)
    }
    if (-not $succeeded) { throw 'Official native contract review stopped; see fixed-stage report.' }
}
# A separate static-only entry point: never enters Invoke-DesktopContractReview
# or executes any binary extracted from the signed package.
function Assert-RendererMenuReviewCI([string]$Actions,[string]$Runner,[string]$OS,
    [string]$Repository,[string]$Ref,[string]$Marker) {
    if ($Actions -cne 'true' -or $Runner -cne 'github-hosted' -or $OS -cne 'Windows' -or
        $Repository -cne 'breakingtime126/magpie' -or
        $Ref -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $Marker -cne 'renderer-menu-review-v1') { throw 'Renderer review requires its static-only CI opt-in.' }
}
function Invoke-RendererMenuReview {
    Assert-RendererMenuReviewCI $env:GITHUB_ACTIONS $env:RUNNER_ENVIRONMENT $env:RUNNER_OS `
        $env:GITHUB_REPOSITORY $env:GITHUB_REF $env:MAGPIE_RENDERER_MENU_REVIEW
    $out=Join-Path $env:RUNNER_TEMP 'renderer-menu-review'
    $work=Join-Path $env:RUNNER_TEMP ('renderer-menu-private-'+[guid]::NewGuid().ToString('N'))
    if (Test-Path -LiteralPath $out) { throw 'Renderer review output already exists.' }
    [void][IO.Directory]::CreateDirectory($out); [void][IO.Directory]::CreateDirectory($work)
    $report=[ordered]@{schema=1;scope='official-renderer-menu-static-only';sourceCommit=$env:GITHUB_SHA
        status='blocked';stage='package';desktopActivated=$false;nativeExecuted=$false;sourceEvaluated=$false
        bridgeAccepted=$false;inferenceAttempted=$false;menuContractProven=$false
        sourceLimits=[ordered]@{files=4096;fileBytes=16777216;totalBytes=134217728;excerptsPerAnchor=2;contextCharactersEachSide=900;excerptTextBytes=16384;outputBytes=1048576}
        limits=@('Literal bounded public-source excerpts, not a supported API or production fix.',
            'No native execution, Desktop activation, account, UI, setup, or security action.',
            'Only packed .js outside node_modules is scanned; missing anchors do not prove absence.',
            'Each signtool/Node child has cleared environment, 30 second runtime and bounded streams.')}
    $succeeded=$false
    try {
        $pins=Get-ContractReviewPins; $package=Join-Path $work 'ChatGPT-x64.msix'
        Invoke-WebRequest -Uri 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-x64.msix' `
            -OutFile $package -TimeoutSec 180 -MaximumRetryCount 0
        if ((Get-Item -LiteralPath $package).Length -ne $pins.packageBytes -or
            (Get-FileHash -LiteralPath $package -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pins.packageSha256) {
            throw 'Renderer review package pin mismatch.'
        }
        $report.stage='signature'
        $sdk=Join-Path ([Environment]::GetEnvironmentVariable('ProgramFiles(x86)')) 'Windows Kits\10\bin'
        $verifier=Get-ChildItem -LiteralPath $sdk -Filter signtool.exe -Recurse |
            Where-Object { $_.FullName -match '\\x64\\signtool.exe$' } | Sort-Object FullName -Descending | Select-Object -First 1
        if (-not $verifier) { throw 'Official package verifier unavailable.' }
        $null=Invoke-ContractCommand $verifier.FullName @('verify','/pa','/v',$package) $work -Label signature
        $signature=Get-AuthenticodeSignature -LiteralPath $package
        $identity=Get-DesktopPackageIdentityEvidence $package $pins.packageSha256 0 ([string]$signature.Status) $signature.SignerCertificate.Subject
        Assert-ContractReviewIdentity $identity
        $report.packageIdentity=$identity
        $report.signature=[ordered]@{status=[string]$signature.Status;signer=$signature.SignerCertificate.Subject;thumbprint=$signature.SignerCertificate.Thumbprint}
        $report.stage='archive-extraction'
        Export-ContractReviewInputs $package $work -ArchiveOnly
        if (Test-Path -LiteralPath (Join-Path $work 'codex.exe')) { throw 'Static-only review extracted a native binary.' }
        $report.stage='renderer-menu-excerpts'
        $node=(Get-Command node -CommandType Application -ErrorAction Stop).Source
        $inspector=Join-Path $PSScriptRoot 'review-desktop-contract.mjs'
        $text=Invoke-ContractCommand $node @('--max-old-space-size=512',$inspector,'--renderer-menu',(Join-Path $work 'app.asar')) $work 1048576 -Label renderer-menu-excerpts
        $evidence=$text | ConvertFrom-Json
        if ($evidence.schema -ne 1 -or $evidence.scope -cne 'public-static-renderer-menu-excerpts' -or
            $evidence.archiveSha256 -cne $pins.archiveSha256) { throw 'Renderer review output identity mismatch.' }
        [IO.File]::WriteAllText((Join-Path $out 'renderer-menu-excerpts.json'),$text,[Text.UTF8Encoding]::new($false))
        $report.status=$(if ($evidence.requiredAnchorsPresent) { 'observed' } else { 'partial-missing-anchors' })
        $report.anchors=$evidence.anchors; $report.coverage=$evidence.coverage
        $succeeded=$true
    } catch { $report.failure='static_review_unavailable_or_bound_exceeded' }
    finally {
        $report.ownedChildExitUnconfirmed=$script:contractChildExitUnconfirmed
        $report.commands=@($script:contractCommandResults.ToArray())
        [IO.File]::WriteAllText((Join-Path $out 'report.json'),($report | ConvertTo-Json -Depth 10),[Text.UTF8Encoding]::new($false))
        Write-Output ('RENDERER_MENU_REVIEW_STATUS='+$report.status+';STAGE='+$report.stage)
    }
    if (-not $succeeded) { throw 'Static renderer review stopped; see fixed-stage report.' }
}
if ($RendererMenuOnly) { Invoke-RendererMenuReview }
elseif ($Run) { Invoke-DesktopContractReview }
