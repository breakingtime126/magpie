# Local authored fixture builder. Default is build-only: no gateway starts.
param(
 [Parameter(Mandatory=$true)][string]$ModuleRoot,
 [Parameter(Mandatory=$true)][string]$SourceRevision,
 [Parameter(Mandatory=$true)][string]$OutputRoot,
 [switch]$RunAfterAudit
)
$ErrorActionPreference='Stop'; Set-StrictMode -Version Latest
if (-not [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)) { throw 'Windows fixture required.' }
if ($SourceRevision -cnotmatch '^[0-9a-f]{40}$') { throw 'Exact source revision required.' }
function Check([string]$Name) { if ($LASTEXITCODE -ne 0) { throw "$Name failed." } }
if (Test-Path -LiteralPath $OutputRoot) { throw 'Use a fresh output directory; artifacts are never overwritten.' }
[void](New-Item -ItemType Directory -Path $OutputRoot)
$package=Join-Path $OutputRoot 'package'; [void](New-Item -ItemType Directory -Path $package)
$stage=Join-Path $ModuleRoot 'build/native-composed-fixture'
if (Test-Path -LiteralPath $stage) { throw 'Fixture package already exists; select a fresh frozen workspace.' }
[void](New-Item -ItemType Directory -Path $stage)
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'fixture/main_windows.go'),(Join-Path $PSScriptRoot 'fixture/composed_windows_test.go') -Destination $stage
Push-Location $ModuleRoot
try {
 $bridge=Join-Path $package 'magpie-codex-bridge.exe'
 & go build -buildvcs=false -tags nogui,nativecodex -trimpath -o $bridge ./cmd/magpie-codex-bridge; Check 'Actual adapter build'
 $digest=(Get-FileHash -LiteralPath $bridge -Algorithm SHA256).Hash.ToLowerInvariant()
 & go build -buildvcs=false -tags nogui,nativecodex -trimpath -o (Join-Path $package 'synthetic-backend.exe') ./build/native-composed-fixture; Check 'Authored backend helper build'
 $hostExe=Join-Path $package 'Magpie-Native-Codex-Experimental.exe'
 & go build -buildvcs=false -tags nogui,nativecodex -trimpath -ldflags "-X github.com/yetone/magpie/internal/nativeconnect.packagedLauncherSHA256=$digest -X github.com/yetone/magpie/internal/nativeconnect.packagedLauncherSourceRevision=$SourceRevision" -o $hostExe ./build/native-composed-fixture
 Check 'Authored pinned host fixture build'
 & go test -c -tags nogui,nativecodex -o (Join-Path $OutputRoot 'composed.test.exe') ./build/native-composed-fixture; Check 'Synthetic harness build'
 & go vet -tags nogui,nativecodex ./build/native-composed-fixture; Check 'Fixture vet'
 $hashes=@{}; Get-ChildItem -LiteralPath $package -Filter '*.exe' | ForEach-Object {$hashes[$_.Name]=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
 @{source_revision=$SourceRevision;bridge_mode='actual-adapter';sha256=$hashes}|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $package 'manifest.json') -Encoding utf8NoBOM
 if ($RunAfterAudit) {
  # The harness uses testenv.Main and passes a cleared allowlist to each actor.
  # Caller must first review gateway-startup-audit/README.md and supply a fixed
  # disposable Windows runner without WSL/network changes. No firewall edits.
  # Clear environment BEFORE importing/initializing gateway in the harness.
  $harnessHome=Join-Path $OutputRoot 'harness-home'; [void](New-Item -ItemType Directory -Path $harnessHome)
  $psi=[Diagnostics.ProcessStartInfo]::new()
  $psi.FileName=Join-Path $OutputRoot 'composed.test.exe'
  $psi.Arguments='-test.run=^TestComposedHTTPDeliveryAndLeaseDrain$ -test.v -test.timeout=120s'
  $psi.WorkingDirectory=$harnessHome; $psi.UseShellExecute=$false
  $psi.RedirectStandardOutput=$true; $psi.RedirectStandardError=$true
  $psi.Environment.Clear()
  foreach($key in @('SystemRoot','WINDIR','ComSpec')) { $value=[Environment]::GetEnvironmentVariable($key); if($value){$psi.Environment[$key]=$value} }
  $psi.Environment['PATH']=''; $psi.Environment['PATHEXT']='.COM;.EXE;.BAT;.CMD'
  $dirs=@{HOME='.';USERPROFILE='.';APPDATA='AppData/Roaming';LOCALAPPDATA='AppData/Local';XDG_CONFIG_HOME='.config';XDG_CONFIG_DIRS='etc/xdg';XDG_CACHE_HOME='.cache';XDG_DATA_HOME='.local/share';XDG_STATE_HOME='.local/state';XDG_RUNTIME_DIR='runtime';CODEX_HOME='native';TEMP='tmp';TMP='tmp';TMPDIR='tmp'}
  foreach($key in $dirs.Keys){$path=Join-Path $harnessHome $dirs[$key];[void](New-Item -ItemType Directory -Force -Path $path);$psi.Environment[$key]=$path}
  $psi.Environment['MAGPIE_NO_ACCOUNTS']='1';$psi.Environment['MAGPIE_PARTNERS']='off';$psi.Environment['MAGPIE_ADDR']='127.0.0.1:0'
  $psi.Environment['MAGPIE_COMPOSED_FIXTURE_PACKAGE']=$package
  $psi.Environment['MAGPIE_COMPOSED_REPORT']=Join-Path $OutputRoot 'result.json'
  $process=[Diagnostics.Process]::new();$process.StartInfo=$psi
  try {
   if(-not $process.Start()){throw 'Harness did not start.'}
   $stdout=$process.StandardOutput.ReadToEndAsync();$stderr=$process.StandardError.ReadToEndAsync()
   if(-not $process.WaitForExit(150000)){$process.Kill($true);if(-not $process.WaitForExit(10000)){throw 'Harness timeout and owned process exit is unconfirmed.'};throw 'Harness timed out; process exited after kill, test cleanup is not confirmed.'}
   $drain=[Threading.Tasks.Task]::WhenAll([Threading.Tasks.Task[]]@($stdout,$stderr))
   if(-not $drain.Wait(10000)){$process.StandardOutput.Close();$process.StandardError.Close();throw 'Harness output drain exceeded deadline; completion is unconfirmed.'}
   $text=$stdout.GetAwaiter().GetResult()+"`n"+$stderr.GetAwaiter().GetResult()
   [IO.File]::WriteAllText((Join-Path $OutputRoot 'run.log'),$text,[Text.UTF8Encoding]::new($false))
   Write-Output $text
   if($process.ExitCode -ne 0){
    # These two authored, isolated fixture actors never receive real accounts.
    # Expose only bounded actor diagnostics, not arbitrary files/environment.
    foreach($name in @('Magpie-Native-Codex-Experimental.exe.log','magpie-codex-bridge.exe.log')) {
     $diagnostic=Join-Path (Join-Path $OutputRoot 'actor-evidence') $name
     if(Test-Path -LiteralPath $diagnostic){
      if((Get-Item -LiteralPath $diagnostic).Length -gt 16384){throw 'Authored actor diagnostic exceeds bound.'}
      Write-Output ("Authored actor diagnostic: "+$name)
      Get-Content -LiteralPath $diagnostic -Raw | Write-Output
     }
    }
    throw 'Synthetic composed HTTP and drain test failed.'
   }
   if($text -notmatch '(?m)^--- PASS: TestComposedHTTPDeliveryAndLeaseDrain \('){throw 'Named composed test PASS missing.'}
   $resultPath=Join-Path $OutputRoot 'result.json'
   if(-not (Test-Path -LiteralPath $resultPath)){throw 'No composed result: a skip or missing report is not acceptance.'}
   $result=Get-Content -LiteralPath $resultPath -Raw|ConvertFrom-Json
   if($result.http_status -ne 400 -or $result.actual_adapter_exit_code -ne 1 -or -not $result.retained_outer_jobs_drained -or -not $result.endpoint_address_reusable_after_drain -or $result.runner_cleanup_error_distinguished){throw 'Composed result did not satisfy the scoped proof.'}
  } finally {$process.Dispose()}
 }
} finally {Pop-Location}
