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
foreach ($key in @('HOME','USERPROFILE','APPDATA','LOCALAPPDATA','CODEX_HOME','XDG_CONFIG_HOME','XDG_CONFIG_DIRS','XDG_DATA_HOME','XDG_CACHE_HOME','XDG_STATE_HOME','XDG_RUNTIME_DIR','TMP','TEMP','GOCACHE','GOMODCACHE','GOPATH')) {
  $isolated[$key] = Join-Path $root $key.ToLowerInvariant()
  New-Item -ItemType Directory -Force $isolated[$key] | Out-Null
}
$isolated['GOTOOLCHAIN']='local'; $isolated['GOTELEMETRY']='off'; $isolated['CGO_ENABLED']='0'
$isolated['GOOS']='windows'; $isolated['GOARCH']='amd64'; $isolated['GOPROXY']='off'
$passed=@{}
function Run-Go([string[]]$arguments, [bool]$expectedRetentionFailure = $false) {
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName=$go; $info.WorkingDirectory=$baseline; $info.UseShellExecute=$false
  $info.Environment.Clear()
  foreach ($entry in $isolated.GetEnumerator()) { $info.Environment[$entry.Key]=$entry.Value }
  foreach ($arg in $arguments) { $info.ArgumentList.Add($arg) }
  $info.RedirectStandardOutput=$true; $info.RedirectStandardError=$true
  $process = [Diagnostics.Process]::Start($info)
  $stdout=$process.StandardOutput.ReadToEndAsync(); $stderr=$process.StandardError.ReadToEndAsync()
  try {
    if (-not $process.WaitForExit(600000)) { $process.Kill($true); if (-not $process.WaitForExit(10000)) { throw 'Synthetic process tree did not exit within kill grace' }; throw 'Synthetic Go command exceeded deadline' }
    if (-not $stdout.Wait(10000) -or -not $stderr.Wait(10000)) { throw 'Synthetic output pipes did not drain within deadline' }
    $out=$stdout.GetAwaiter().GetResult(); $err=$stderr.GetAwaiter().GetResult()
    Write-Host $out; Write-Host $err
    if (-not $expectedRetentionFailure) {
      foreach ($line in ($out -split "`n")) {
        if (-not $line.StartsWith('{')) { continue }
        $event=$line | ConvertFrom-Json
        if ($event.PSObject.Properties.Name -contains 'Test' -and $event.Action -eq 'pass') { $passed[$event.Package+'/'+$event.Test]=$true }
      }
    }
    if ($expectedRetentionFailure) {
      if ($process.ExitCode -eq 0 -or $out -notmatch 'original socket was not retained by owned child') { throw 'Mutation did not fail the required retention assertion' }
      $events=@($out -split "`n" | Where-Object { $_.StartsWith('{') } | ForEach-Object { $_ | ConvertFrom-Json })
      if (-not @($events | Where-Object { $_.PSObject.Properties.Name -contains 'Test' -and $_.Action -eq 'fail' -and $_.Test -eq 'TestWindowsEndpointInheritedOriginalSurvivesParentSocketClose' }).Count) { throw 'Missing explicit expected-red test event' }
      Write-Host 'EXPECTED MUTATION RED VERIFIED: production inherited-handle omission triggered retention assertion.'
    } elseif ($process.ExitCode -ne 0) { throw "Go command failed: $($process.ExitCode)" }
  } finally { $process.Dispose() }
}
Run-Go @('version')
[Environment]::OSVersion.VersionString

$isolated['GOPROXY']='https://proxy.golang.org'
$downloadOnly=@('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY','SSL_CERT_FILE','SSL_CERT_DIR','CURL_CA_BUNDLE','REQUESTS_CA_BUNDLE','GIT_SSL_CAINFO')
foreach ($key in $downloadOnly) { $value=[Environment]::GetEnvironmentVariable($key); if ($value) { $isolated[$key]=$value } }
try { Run-Go @('mod','download','github.com/MoeForge/moejs','github.com/clipperhouse/stringish','github.com/clipperhouse/uax29/v2','github.com/coder/websocket','github.com/dustin/go-humanize','github.com/fsnotify/fsnotify','github.com/gen2brain/h265','github.com/google/uuid','github.com/klauspost/compress','github.com/mattn/go-isatty','github.com/ncruces/go-strftime','github.com/pelletier/go-toml/v2','github.com/remyoudompheng/bigfft','github.com/santhosh-tekuri/jsonschema/v6','github.com/tidwall/gjson','github.com/tidwall/jsonc','github.com/tidwall/match','github.com/tidwall/pretty','golang.org/x/exp','golang.org/x/image','golang.org/x/net','golang.org/x/sys','golang.org/x/text','gopkg.in/yaml.v3','modernc.org/libc','modernc.org/mathutil','modernc.org/memory','modernc.org/sqlite') }
finally { foreach ($key in $downloadOnly) { $isolated.Remove($key) } }
$isolated['GOPROXY']='off'
Run-Go @('test','-tags','nogui,nativecodex','-mod=readonly','-buildvcs=false','-count=1','-json','-timeout=180s','./internal/nativehost','./internal/nativechannel')
Run-Go @('test','-tags','nogui,nativecodex','-mod=readonly','-buildvcs=false','-count=1','-json','-timeout=180s','-run','^TestOwned','./internal/gateway')
$requiredPasses=@{
 'nativehost'=@('TestWindowsOwnerAwaitFailureReturnsNilInterface','TestHostCapturesChannelProfileAndSingleUseLease','TestHostSessionRevocationCancelsLeaseBeforeChannelDrain','TestHostPlanRejectsRevocationBeforeConsumptionOrSupervisorCancel');
 'nativechannel'=@('TestSessionRevokedBeforeUncooperativeHandlerDrains');
 'gateway'=@('TestOwnedConnectionValidNeverReacquiresGeneration','TestOwnedConnectionRejectsExpiredGeneration')
}
foreach ($package in $requiredPasses.Keys) {
 foreach ($test in $requiredPasses[$package]) {
  $key='github.com/yetone/magpie/internal/'+$package+'/'+$test
  if (-not $passed.ContainsKey($key)) { throw "Required named test did not PASS (missing/skip is not acceptance): $key" }
 }
}
Write-Host 'Required host/channel/gateway named PASS events verified.'
