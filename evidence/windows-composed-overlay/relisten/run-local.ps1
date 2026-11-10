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
$stage=Join-Path $ModuleRoot 'build/native-relisten-fixture'
if (Test-Path -LiteralPath $stage) { throw 'Fixture package already exists; select a fresh frozen workspace.' }
[void](New-Item -ItemType Directory -Path $stage)
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'fixture/main_windows.go'),(Join-Path $PSScriptRoot 'fixture/composed_windows_test.go'),(Join-Path $PSScriptRoot 'fixture/backend_ledger.go'),(Join-Path $PSScriptRoot 'fixture/backend_ledger_test.go') -Destination $stage
Push-Location $ModuleRoot
try {
 $bridge=Join-Path $package 'magpie-codex-bridge.exe'
 & go build -buildvcs=false -mod=readonly -tags nogui,nativecodex -trimpath -o $bridge ./cmd/magpie-codex-bridge; Check 'Actual adapter build'
 $digest=(Get-FileHash -LiteralPath $bridge -Algorithm SHA256).Hash.ToLowerInvariant()
 & go build -buildvcs=false -mod=readonly -tags nogui,nativecodex -trimpath -o (Join-Path $package 'synthetic-backend.exe') ./build/native-relisten-fixture; Check 'Authored backend helper build'
 $hostExe=Join-Path $package 'Magpie-Native-Codex-Experimental.exe'
 & go build -buildvcs=false -mod=readonly -tags nogui,nativecodex -trimpath -ldflags "-X github.com/yetone/magpie/internal/nativeconnect.packagedLauncherSHA256=$digest -X github.com/yetone/magpie/internal/nativeconnect.packagedLauncherSourceRevision=$SourceRevision" -o $hostExe ./build/native-relisten-fixture
 Check 'Authored pinned host fixture build'
 & go test -buildvcs=false -mod=readonly -c -tags nogui,nativecodex -o (Join-Path $OutputRoot 'relisten.test.exe') ./build/native-relisten-fixture; Check 'Synthetic harness build'
 & go vet -buildvcs=false -mod=readonly -tags nogui,nativecodex ./build/native-relisten-fixture; Check 'Fixture vet'
 $hashes=@{}; Get-ChildItem -LiteralPath $package -Filter '*.exe' | ForEach-Object {$hashes[$_.Name]=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
 @{source_revision=$SourceRevision;bridge_mode='actual-adapter';sha256=$hashes}|ConvertTo-Json -Depth 4|Set-Content -LiteralPath (Join-Path $package 'manifest.json') -Encoding utf8NoBOM
 if ($RunAfterAudit) {
  # The harness uses testenv.Main and passes a cleared allowlist to each actor.
  # Caller must first review gateway-startup-audit/README.md and supply a fixed
  # disposable Windows runner without WSL/network changes. No firewall edits.
  # Clear environment BEFORE importing/initializing gateway in the harness.
  $harnessHome=Join-Path $OutputRoot 'harness-home'; [void](New-Item -ItemType Directory -Path $harnessHome)
  $psi=[Diagnostics.ProcessStartInfo]::new()
  $psi.FileName=Join-Path $OutputRoot 'relisten.test.exe'
  $psi.Arguments='-test.run=^TestComposedRelistenTakeoverAndRecovery$ -test.v -test.timeout=120s'
  $psi.WorkingDirectory=$harnessHome; $psi.UseShellExecute=$false
  $psi.RedirectStandardOutput=$true; $psi.RedirectStandardError=$true
  $psi.Environment.Clear()
  foreach($key in @('SystemRoot','WINDIR','ComSpec')) { $value=[Environment]::GetEnvironmentVariable($key); if($value){$psi.Environment[$key]=$value} }
  $psi.Environment['PATH']=''; $psi.Environment['PATHEXT']='.COM;.EXE;.BAT;.CMD'
  $dirs=@{HOME='.';USERPROFILE='.';APPDATA='AppData/Roaming';LOCALAPPDATA='AppData/Local';XDG_CONFIG_HOME='.config';XDG_CONFIG_DIRS='etc/xdg';XDG_CACHE_HOME='.cache';XDG_DATA_HOME='.local/share';XDG_STATE_HOME='.local/state';XDG_RUNTIME_DIR='runtime';CODEX_HOME='native';TEMP='tmp';TMP='tmp';TMPDIR='tmp'}
  foreach($key in $dirs.Keys){$path=Join-Path $harnessHome $dirs[$key];[void](New-Item -ItemType Directory -Force -Path $path);$psi.Environment[$key]=$path}
  $psi.Environment['MAGPIE_NO_ACCOUNTS']='1';$psi.Environment['MAGPIE_PARTNERS']='off';$psi.Environment['MAGPIE_ADDR']='127.0.0.1:0'
  $psi.Environment['MAGPIE_RELISTEN_FIXTURE_PACKAGE']=$package
  $psi.Environment['MAGPIE_RELISTEN_REPORT']=Join-Path $OutputRoot 'result.json'
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
   if($process.ExitCode -ne 0){throw 'Synthetic Relisten/takeover/recovery test failed.'}
   if($text -notmatch '(?m)^--- PASS: TestComposedRelistenTakeoverAndRecovery \('){throw 'Named Relisten test PASS missing.'}
   $resultPath=Join-Path $OutputRoot 'result.json'
   if(-not (Test-Path -LiteralPath $resultPath)){throw 'No Relisten result: a skip or missing report is not acceptance.'}
   $result=Get-Content -LiteralPath $resultPath -Raw|ConvertFrom-Json
   if($result.first_http_status -ne 400 -or $result.recovered_http_status -ne 400 -or -not $result.genuine_relisten_changed_port -or -not $result.old_capability_invalid_immediately -or -not $result.old_request_rejected_without_body_read -or $result.host_cancel_observed_ms -gt 1500 -or -not $result.selection_restore_kept_bridge_alive -or -not $result.pending_frontend_request_terminated -or $result.takeover_protected_requests -ne 0 -or $result.takeover_protected_credential_markers -ne 0 -or $result.takeover_protected_body_markers -ne 0 -or -not $result.fixed_url_negative_control_detected -or $result.stale_profile_backend_starts -ne 0 -or -not $result.duplicate_phase_backend_negative_control_detected -or $result.backend_start_count -ne 5 -or -not $result.backend_control_outer_job_drained -or -not $result.old_profile_relaunch_rejected -or -not $result.fresh_generation_verified_via_real_named_pipe -or ($result.actual_adapter_exit_codes -join ',') -cne '1,1,1' -or -not $result.retained_outer_jobs_drained -or -not $result.endpoint_addresses_reusable_after_drain -or $result.runner_cleanup_error_distinguished -or $result.vendor_native_protocol_verified -or $result.real_accounts_used -or $result.zero_egress_proven){throw 'Relisten result did not satisfy the scoped proof.'}
  } finally {$process.Dispose()}
 }
} finally {Pop-Location}
