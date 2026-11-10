$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$baseline = Join-Path $env:GITHUB_WORKSPACE 'baseline'
$overlay = $PSScriptRoot
$manifest = Get-Content -Raw (Join-Path $overlay 'manifest.json') | ConvertFrom-Json
if ((git -C $baseline rev-parse HEAD).Trim() -ne $manifest.baseline_commit) { throw 'Wrong baseline commit' }
function Assert-Hash($path, $expected) {
  if ((Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant() -ne $expected) { throw "Byte hash mismatch: $path" }
}
foreach ($item in $manifest.baseline) {
  $path = Join-Path $baseline $item.path
  Assert-Hash $path $item.sha256
  if ((git -C $baseline hash-object --no-filters -- $item.path).Trim() -ne $item.git_blob_sha1) { throw "Baseline blob mismatch: $($item.path)" }
}
foreach ($item in $manifest.overlay) {
  $source = Join-Path $overlay $item.path
  $target = Join-Path $baseline $item.path
  Assert-Hash $source $item.sha256
  New-Item -ItemType Directory -Force (Split-Path $target) | Out-Null
  Copy-Item -LiteralPath $source -Destination $target
  Assert-Hash $target $item.sha256
}
Write-Host "Verified exact baseline $($manifest.baseline.Count) files and candidate overlay $($manifest.overlay.Count) files."
$root = Join-Path $env:RUNNER_TEMP 'magpie-synthetic-components'
New-Item -ItemType Directory -Force $root | Out-Null
$filename = 'go1.26.8.windows-amd64.zip'
$releases = Invoke-RestMethod 'https://go.dev/dl/?mode=json&include=all'
$files = @($releases | Where-Object version -EQ 'go1.26.8' | ForEach-Object files | Where-Object { $_.filename -eq $filename -and $_.os -eq 'windows' -and $_.arch -eq 'amd64' -and $_.kind -eq 'archive' })
if ($files.Count -ne 1 -or $files[0].sha256 -notmatch '^[0-9a-f]{64}$') { throw 'Official Go archive metadata is missing or ambiguous' }
if ($files[0].sha256 -ne 'b92c3b2adae85a11ba71fe7216daf0d84e82af4c8ab6c5625807f28622043a59' -or $files[0].size -ne 74957586) { throw 'Official metadata differs from reviewed pinned release' }
$archive = Join-Path $root $filename
Invoke-WebRequest "https://go.dev/dl/$filename" -OutFile $archive
if ((Get-Item -LiteralPath $archive).Length -ne 74957586) { throw 'Archive size mismatch' }
Assert-Hash $archive $files[0].sha256
Write-Host "Official archive verified: $filename SHA256=$($files[0].sha256)"
Expand-Archive -LiteralPath $archive -DestinationPath $root
$go = Join-Path $root 'go/bin/go.exe'
$isolated = @{}
foreach ($key in @('SystemRoot','WINDIR','ComSpec','PATHEXT')) { $isolated[$key] = [Environment]::GetEnvironmentVariable($key) }
$isolated['PATH'] = "$(Join-Path $root 'go/bin');$(Join-Path $env:SystemRoot 'System32')"
foreach ($key in @('HOME','USERPROFILE','APPDATA','LOCALAPPDATA','CODEX_HOME','XDG_CONFIG_HOME','XDG_CONFIG_DIRS','XDG_DATA_HOME','XDG_CACHE_HOME','XDG_STATE_HOME','XDG_RUNTIME_DIR','MAGPIE_TEST_SANDBOX','TMP','TEMP','GOCACHE','GOMODCACHE','GOPATH')) {
  $isolated[$key] = Join-Path $root $key.ToLowerInvariant()
  New-Item -ItemType Directory -Force $isolated[$key] | Out-Null
}
$isolated['GOTOOLCHAIN']='local'; $isolated['GOTELEMETRY']='off'; $isolated['CGO_ENABLED']='0'
$isolated['GOOS']='windows'; $isolated['GOARCH']='amd64'; $isolated['GOPROXY']='off'
function Run-Go([string[]]$arguments) {
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName=$go; $info.WorkingDirectory=$baseline; $info.UseShellExecute=$false
  $info.Environment.Clear()
  foreach ($entry in $isolated.GetEnumerator()) { $info.Environment[$entry.Key]=$entry.Value }
  foreach ($arg in $arguments) { $info.ArgumentList.Add($arg) }
  $process = [Diagnostics.Process]::Start($info)
  try {
    if (-not $process.WaitForExit(240000)) { $process.Kill($true); $process.WaitForExit(); throw 'Synthetic Go command exceeded deadline' }
    if ($process.ExitCode -ne 0) { throw "Go command failed: $($process.ExitCode)" }
  } finally { $process.Dispose() }
}
Run-Go @('version')
[Environment]::OSVersion.VersionString
$isolated['GOPROXY']='https://proxy.golang.org'
Run-Go @('mod','download','golang.org/x/sys')
$isolated['GOPROXY']='off'
Run-Go @('test','-mod=readonly','-buildvcs=false','-count=1','-json','-timeout=180s','-run','^(TestEndpoint|TestInheritance|TestDrained|TestRejectsConflictingWindowsAttributes|TestAllowsProc|TestWindowsEndpoint|TestStartTreeStrict)','./internal/nativeendpoint','./internal/proc')
