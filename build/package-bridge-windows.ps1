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

$formatDiff = & gofmt -d internal/appserverbridge internal/proc cmd/magpie-codex-bridge 2>&1
$formatExit = $LASTEXITCODE
if ($formatDiff) { $formatDiff | Write-Output }
if ($formatExit -ne 0 -or $formatDiff) { throw 'Go source formatting check failed; see the diagnostics above.' }

# Build the dedicated native entry point before its integration test. The test
# must exercise the packaged executable, not just an injected Go function.
$version = 'v0.1.3035-bridge.' + $head.Substring(0, 12)
$adapter = Join-Path $package 'magpie-codex-bridge-windows-amd64.exe'
& go build -tags nogui -trimpath -buildvcs=true -ldflags "-s -w -X main.buildVersion=$version" -o $adapter ./cmd/magpie-codex-bridge
Assert-NativeExit 'Build native stdio adapter'
$adapterBuildInfoLines = & go version -m $adapter
Assert-NativeExit 'Read native adapter build information'
$adapterBuildInfo = $adapterBuildInfoLines -join "`n"
if ($adapterBuildInfo -notmatch ('vcs\.revision=' + [regex]::Escape($head) + '(\s|$)') -or
    $adapterBuildInfo -notmatch 'vcs\.modified=false(\s|$)' -or
    $adapterBuildInfo -notmatch 'GOOS=windows(\s|$)' -or
    $adapterBuildInfo -notmatch 'GOARCH=amd64(\s|$)') {
    throw 'Native adapter provenance does not match the clean Windows source commit.'
}
$adapterVersion = & $adapter --version
Assert-NativeExit 'Native adapter version smoke test'
if (($adapterVersion -join "`n") -ne "magpie-codex-bridge (experimental stdio adapter) $version") {
    throw 'Native adapter did not identify itself truthfully.'
}
$adapterHelp = & $adapter --help
Assert-NativeExit 'Native adapter help smoke test'
if (($adapterHelp -join "`n") -notmatch 'Desktop launcher compatibility is unverified') {
    throw 'Native adapter help omitted its compatibility boundary.'
}
$env:MAGPIE_BRIDGE_ADAPTER_TEST_EXE = $adapter

# These synthetic tests do not start official Codex or contact model services.
& go test -v -count=1 -timeout=10m -tags nogui ./cmd/magpie-codex-bridge
Assert-NativeExit 'Native stdio adapter unit and lifecycle tests'
& go test -v -count=20 -timeout=10m -tags nogui ./cmd/magpie-codex-bridge -run '^TestBuiltAdapter(Stdio|DesktopArguments|StartupProbe)$'
Assert-NativeExit 'Repeated compiled adapter stdio, Desktop arguments, startup probe and EOF tests'
& go test -v -count=1 -timeout=10m -tags nogui ./internal/appserverbridge
Assert-NativeExit 'Native bridge tests'
& go test -v -count=20 -timeout=10m -tags nogui ./internal/appserverbridge -run '^TestStartupProbeNative'
Assert-NativeExit 'Repeated native startup probe and owned cleanup tests'
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

    # Restore strict process startup before testing the independent argument
    # regression, so a process-start failure cannot explain that result.
    [IO.File]::WriteAllText($mutationFile, $mutationSource)
    $launchFile = Join-Path $mutationRoot 'internal/appserverbridge/launch.go'
    $launchSource = [IO.File]::ReadAllText($launchFile)
    $granularToggle = '"features.code_mode_host.enabled=true"'
    if ([regex]::Matches($launchSource, [regex]::Escape($granularToggle)).Count -ne 1) {
        throw 'Granular code-mode mutation target is not unique.'
    }
    [IO.File]::WriteAllText($launchFile, $launchSource.Replace($granularToggle, '"features.code_mode_host=true"'))
    $argumentMutationLog = & go test -v -count=1 -timeout=2m -tags nogui ./cmd/magpie-codex-bridge -run '^TestNativeAdapterDesktopArguments$' 2>&1
    $argumentMutationExit = $LASTEXITCODE
    $argumentMutationLog | Write-Output
    if ($argumentMutationExit -eq 0 -or ($argumentMutationLog -join "`n") -notmatch '--- FAIL: TestNativeAdapterDesktopArguments') {
        throw 'Scalar code-mode mutation did not produce the expected argument-policy regression failure.'
    }


    # Restore launch controls before checking a distinct requirements guard.
    [IO.File]::WriteAllText($launchFile, $launchSource)
    $probeFile = Join-Path $mutationRoot 'internal/appserverbridge/probe.go'
    $probeSource = [IO.File]::ReadAllText($probeFile)
    $nullGuard = '!bytes.Equal(bytes.TrimSpace(requirements["requirements"]), []byte("null"))'
    if ([regex]::Matches($probeSource, [regex]::Escape($nullGuard)).Count -ne 1) {
        throw 'Startup requirements mutation target is not unique.'
    }
    [IO.File]::WriteAllText($probeFile, $probeSource.Replace($nullGuard, 'false'))
    $probeMutationLog = & go test -v -count=1 -timeout=2m -tags nogui ./internal/appserverbridge -run '^TestStartupProbeSuccessSchemaIsCompleteAndExact$' 2>&1
    $probeMutationExit = $LASTEXITCODE
    $probeMutationLog | Write-Output
    if ($probeMutationExit -eq 0 -or ($probeMutationLog -join "`n") -notmatch '--- FAIL: TestStartupProbeSuccessSchemaIsCompleteAndExact') {
        throw 'Non-null requirements mutation did not produce the expected behavioral failure.'
    }
} finally {
    Set-Location -LiteralPath $env:GITHUB_WORKSPACE
    Remove-Item -LiteralPath $mutationRoot -Recurse -Force
    Remove-Item -LiteralPath $mutationArchive -Force
}
Write-Output 'Strict startup regression failed with CREATE_SUSPENDED removed, as required.'
Write-Output 'Desktop argument-policy regression failed with granular code-mode toggle replaced, as required.'
Write-Output 'Startup probe regression rejected allowing non-null requirements, as required.'

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
[IO.File]::WriteAllText((Join-Path $package 'adapter-build-info.txt'), $adapterBuildInfo + "`n", $utf8)
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
    nativeAdapterSHA256 = (Get-FileHash -LiteralPath $adapter -Algorithm SHA256).Hash.ToLowerInvariant()
    nativeAdapterBuildFlags = '-tags nogui -trimpath -buildvcs=true -ldflags=-s -w -X main.buildVersion=<version>'
    signing = 'unsigned'
    checks = @('Linux focused bridge, proc and adapter race tests', 'Native stdio adapter unit and lifecycle tests', 'Repeated compiled adapter initialize and EOF tests', 'Repeated compiled adapter observed Desktop argument tests', 'Granular code-mode policy mutation rejected by regression', 'Repeated compiled startup probes with pinned synthetic backends', 'Repeated native startup probe and owned cleanup tests', 'Non-null startup requirements mutation rejected by regression', 'Desktop bootstrap auth/config local rejection tests', 'Native synthetic bridge tests', 'Native bridge CLI isolation test', 'Repeated Windows process-family and interruption tests', 'Repeated strict process startup tests', 'CREATE_SUSPENDED mutation rejected by regression', 'Windows and Linux-target go vet -tags nogui ./...', 'Built executable bridge help', 'Built native adapter truthful help and version')
    notValidated = @('Full repository test suite', 'Managed Windows policy and external injection scenarios', 'Official Codex runtime and tools', 'Codex Desktop integration and exact launcher arguments', 'Desktop-required auth/config bootstrap support', 'Real official startup inspection and credential/policy side effects', 'Restricted or different-version startup configuration (unsupported)', 'Startup sign-out (explicitly unsupported)', 'Real official backend granular feature merge', 'Native adapter OS-signal handling', 'Real OpenAI or AIHub inference', 'Corporate deployment approval')
}
[IO.File]::WriteAllText((Join-Path $package 'BUILD.json'), ($manifest | ConvertTo-Json -Depth 5) + "`n", $utf8)
$readme = @'
Magpie experimental app-server bridge, Windows amd64 executables

These unsigned executables were built from the exact source commit in BUILD.json.
LICENSE and THIRD_PARTY_NOTICES are copied unchanged from that commit.
SHA256SUMS checks every packaged file except SHA256SUMS itself. The GitHub run
also records the uploaded ZIP digest. Checksums are not a code signature.

magpie-app-server-bridge-windows-amd64.exe is the nogui Magpie executable
with an experimental app-server-bridge subcommand. Do not point a Desktop
executable override at this ordinary Magpie binary.
magpie-codex-bridge-windows-amd64.exe is the separate bridge-only native stdio
adapter. It accepts only reviewed app-server stdio forms, needs the explicit
MAGPIE_APP_SERVER_BRIDGE_CONFIG process environment, and truthfully identifies
itself as an experimental adapter. The reviewed Desktop base argv is accepted;
its code-mode toggle is normalized to the granular enabled field to preserve
existing policy. Startup inspection is disabled by default and supports only
explicitly enabled, SHA256-pinned 0.166.0-alpha.6 backends returning the same
complete unrestricted result. It can refresh credentials, contact policy
services and write caches, even on failure. Sign-out, restricted startup results
and unreviewed overrides remain unsupported. It does not establish working Desktop integration.
Neither executable is a standalone official Codex installation. The package
bundles no official Codex backend, credentials, AIHub account or integration.
The example configuration contains placeholders, not usable authentication.
See DESIGN.md for arguments, unsupported auth/config RPCs and remaining limits.

CI runs synthetic bridge/CLI/process tests, static checks, a startup mutation,
compiled adapter initialize/EOF and observed Desktop-argument tests, pinned
synthetic startup probes and cleanup checks, a granular code-mode policy mutation,
a non-null startup requirements mutation, Linux focused race tests, and truthful
help/version smoke tests. No real official startup command is executed by tests.
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
