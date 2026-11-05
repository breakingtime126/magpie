# Build only on the explicitly authorized GitHub-hosted experimental branch.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$PSNativeCommandUseErrorActionPreference = $false

function Assert-NativeExit([string] $Operation) {
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed with exit code $LASTEXITCODE"
    }
}

if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true' -or
    $env:GITHUB_REPOSITORY -ne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -ne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
    $env:GITHUB_SHA -notmatch '^[0-9a-f]{40}$') {
    throw 'This packaging script is restricted to the experimental Windows CI job.'
}
Set-Location -LiteralPath $env:GITHUB_WORKSPACE
$head = (& git rev-parse HEAD).Trim()
Assert-NativeExit 'Read source commit'
if ($head -ne $env:GITHUB_SHA) { throw 'Checkout does not match the triggering commit.' }
$tree = (& git rev-parse 'HEAD^{tree}').Trim()
Assert-NativeExit 'Read source tree'
$dirty = & git status --porcelain
Assert-NativeExit 'Check source status'
if ($dirty) { throw 'The source checkout is not clean.' }

# Pin caches before replacing HOME; tests must not use the runner account's
# agent configuration. Dependencies still come from the committed go.sum.
$goPathsJSON = & go env -json GOPATH GOMODCACHE GOCACHE
Assert-NativeExit 'Read Go cache paths'
$goPaths = ($goPathsJSON -join "`n") | ConvertFrom-Json
$env:GOPATH = $goPaths.GOPATH
$env:GOMODCACHE = $goPaths.GOMODCACHE
$env:GOCACHE = $goPaths.GOCACHE
$isolated = Join-Path $env:RUNNER_TEMP "mb-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
$paths = @{
    HOME = 'home'; USERPROFILE = 'home'; CODEX_HOME = 'codex'
    APPDATA = 'appdata'; LOCALAPPDATA = 'localappdata'
    XDG_CONFIG_HOME = 'config'; XDG_CACHE_HOME = 'cache'
    XDG_DATA_HOME = 'data'; XDG_STATE_HOME = 'state'
    TEMP = 'tmp'; TMP = 'tmp'; TMPDIR = 'tmp'
}
foreach ($name in $paths.Keys) {
    $value = Join-Path $isolated $paths[$name]
    New-Item -ItemType Directory -Path $value -Force | Out-Null
    [Environment]::SetEnvironmentVariable($name, $value, 'Process')
}
$package = Join-Path $env:RUNNER_TEMP 'bridge-package'
if (Test-Path -LiteralPath $package) { throw 'Artifact directory already exists.' }
New-Item -ItemType Directory -Path $package | Out-Null

$formatDiff = & gofmt -d internal/appserverbridge internal/proc
Assert-NativeExit 'Check Go formatting'
if ($formatDiff) {
    $formatDiff | Write-Output
    throw 'Go source needs gofmt.'
}

# These synthetic tests do not start official Codex or contact model services.
& go test -v -count=1 -timeout=10m -tags nogui ./internal/appserverbridge
Assert-NativeExit 'Native bridge tests'
& go test -v -count=20 -timeout=10m -tags nogui ./internal/appserverbridge -run '^TestRun(WindowsReapsStartedBackendFamilies|InterruptPreservesOtherBackendAndAllowsFollowup)$'
Assert-NativeExit 'Repeated Windows cleanup and interruption tests'
& go test -v -count=20 -timeout=10m -tags nogui ./internal/proc -run '^TestStartTreeStrict'
Assert-NativeExit 'Repeated strict Windows process startup tests'
& go test -v -count=1 -timeout=10m -tags nogui ./internal/cli -run '^TestAppServerBridgeHelpAndInvalidConfigDoNotStartMagpie$'
Assert-NativeExit 'Native bridge CLI test'
& go vet -tags nogui ./...
Assert-NativeExit 'Windows Go vet'
try {
    $env:GOOS = 'linux'
    & go vet -tags nogui ./...
    Assert-NativeExit 'Linux cross-platform Go vet'
} finally {
    $env:GOOS = 'windows'
}


# Prove the startup regression detects the old runnable-before-assignment
# behavior. Mutate a separate source archive, never the artifact checkout.
$mutationRoot = Join-Path $env:RUNNER_TEMP 'bridge-mutation'
$mutationArchive = Join-Path $env:RUNNER_TEMP 'bridge-mutation.zip'
if ((Test-Path -LiteralPath $mutationRoot) -or (Test-Path -LiteralPath $mutationArchive)) {
    throw 'Mutation scratch path already exists.'
}
& git archive --format=zip --output=$mutationArchive HEAD
Assert-NativeExit 'Archive exact source for regression mutation'
Expand-Archive -LiteralPath $mutationArchive -DestinationPath $mutationRoot
$mutationFile = Join-Path $mutationRoot 'internal/proc/tree_strict_windows.go'
$mutationSource = [IO.File]::ReadAllText($mutationFile)
$flagLine = 'attr.CreationFlags |= windows.CREATE_SUSPENDED'
if ([regex]::Matches($mutationSource, [regex]::Escape($flagLine)).Count -ne 1) {
    throw 'Strict startup mutation target is not unique.'
}
[IO.File]::WriteAllText($mutationFile, $mutationSource.Replace($flagLine, '// Mutation: initial thread is allowed to execute before assignment.'))
try {
    Set-Location -LiteralPath $mutationRoot
    $mutationLog = & go test -v -count=1 -timeout=2m -tags nogui ./internal/proc -run '^TestStartTreeStrictAssignedBeforeExecution$' 2>&1
    $mutationExit = $LASTEXITCODE
    $mutationLog | Write-Output
    if ($mutationExit -eq 0 -or ($mutationLog -join "`n") -notmatch '--- FAIL: TestStartTreeStrictAssignedBeforeExecution') {
        throw 'Startup mutation did not produce the expected behavioral regression failure.'
    }
} finally {
    Set-Location -LiteralPath $env:GITHUB_WORKSPACE
    Remove-Item -LiteralPath $mutationRoot -Recurse -Force
    Remove-Item -LiteralPath $mutationArchive -Force
}
Write-Output 'Strict startup regression failed with CREATE_SUSPENDED removed, as required.'

$version = 'v0.1.3035-bridge.' + $head.Substring(0, 12)
$binary = Join-Path $package 'magpie-app-server-bridge-windows-amd64.exe'
& go build -tags nogui -trimpath -buildvcs=true -ldflags "-s -w -X main.version=$version" -o $binary .
Assert-NativeExit 'Build Windows CLI'
$buildInfoLines = & go version -m $binary
Assert-NativeExit 'Read executable build information'
$buildInfo = $buildInfoLines -join "`n"
if ($buildInfo -notmatch ('vcs\.revision=' + [regex]::Escape($head) + '(\s|$)') -or
    $buildInfo -notmatch 'vcs\.modified=false(\s|$)' -or
    $buildInfo -notmatch 'GOOS=windows(\s|$)' -or
    $buildInfo -notmatch 'GOARCH=amd64(\s|$)') {
    throw 'Executable provenance does not match the clean Windows source commit.'
}
$helpLines = & $binary app-server-bridge --help
Assert-NativeExit 'Experimental command help smoke test'
if (($helpLines -join "`n") -notmatch 'app-server-bridge --config') {
    throw 'The built executable did not expose the bridge command.'
}
$dirty = & git status --porcelain
Assert-NativeExit 'Recheck source status'
if ($dirty) { throw 'Tests or build changed the source checkout.' }

Copy-Item -LiteralPath 'LICENSE' -Destination $package
Copy-Item -LiteralPath 'THIRD_PARTY_NOTICES' -Destination $package
Copy-Item -LiteralPath 'docs/examples/app-server-bridge.example.json' -Destination $package
Copy-Item -LiteralPath 'docs/subsystems/app-server-bridge.md' -Destination (Join-Path $package 'DESIGN.md')
$utf8 = [System.Text.UTF8Encoding]::new($false)
[IO.File]::WriteAllText((Join-Path $package 'build-info.txt'), $buildInfo + "`n", $utf8)
$goVersion = & go version
Assert-NativeExit 'Read Go version'
$manifest = [ordered]@{
    repository = $env:GITHUB_REPOSITORY
    sourceCommit = $head
    sourceTree = $tree
    sourceRef = $env:GITHUB_REF
    workflowRun = "$env:GITHUB_SERVER_URL/$env:GITHUB_REPOSITORY/actions/runs/$env:GITHUB_RUN_ID"
    workflowAttempt = $env:GITHUB_RUN_ATTEMPT
    goVersion = $goVersion
    target = 'windows/amd64'
    version = $version
    buildFlags = '-tags nogui -trimpath -buildvcs=true -ldflags=-s -w -X main.version=<version>'
    executableSHA256 = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()
    signing = 'unsigned'
    checks = @('Native synthetic bridge tests', 'Native bridge CLI isolation test', 'Repeated Windows process-family and interruption tests', 'Repeated strict process startup tests', 'CREATE_SUSPENDED mutation rejected by regression', 'Windows and Linux-target go vet -tags nogui ./...', 'Built executable bridge help')
    notValidated = @('Full repository test suite', 'Managed Windows policy and external injection scenarios', 'Official Codex runtime and tools', 'Codex Desktop integration', 'Real OpenAI or AIHub inference', 'Corporate deployment approval')
}
[IO.File]::WriteAllText((Join-Path $package 'BUILD.json'), ($manifest | ConvertTo-Json -Depth 5) + "`n", $utf8)
$readme = @'
Magpie experimental app-server bridge, Windows amd64 CLI

This unsigned executable was built from the exact source commit in BUILD.json.
LICENSE and THIRD_PARTY_NOTICES are copied unchanged from that commit.
SHA256SUMS checks every packaged file except SHA256SUMS itself. The GitHub run
also records the uploaded ZIP digest. Checksums are not a code signature.

This is the nogui Magpie executable with an experimental app-server-bridge
subcommand. It is not a standalone official Codex installation. It bundles no
official Codex backend, credentials, AIHub account or Desktop integration.
The example configuration contains placeholders, not usable authentication.
See DESIGN.md and the source documentation for behavior and remaining limits.

CI runs synthetic bridge/CLI/process tests, static checks, a startup mutation
check and a help smoke test.
It does not establish working Desktop integration, real tools/model inference,
full-suite correctness or complete Windows process-tree containment.

No installer, system configuration change, release publication or deployment is
performed. Artifact availability does not authorize installation or operation.
Do not use this artifact to bypass an organization's download, software or
network restrictions. Any deployment requires its own applicable approval.
'@
[IO.File]::WriteAllText((Join-Path $package 'README.txt'), $readme + "`n", $utf8)
$sums = Get-ChildItem -LiteralPath $package -File | Sort-Object Name | ForEach-Object {
    (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $_.Name
}
[IO.File]::WriteAllText((Join-Path $package 'SHA256SUMS'), ($sums -join "`n") + "`n", $utf8)
Write-Output "Prepared unsigned Windows artifact for $head"
