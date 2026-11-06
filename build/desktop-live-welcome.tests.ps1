# Offline fixtures: no Desktop activation, account, network, or UI action.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'desktop-live-welcome.ps1')
$count=0
function Pass([bool]$Condition,[string]$Name) { if (-not $Condition) { throw $Name }; $script:count++ }
function Reject([scriptblock]$Action,[string]$Name) { $failed=$false; try { & $Action } catch { $failed=$true }; Pass $failed $Name }
# PowerShell variables are case-insensitive. Fail early on read-only automatic
# names in locals, parameters and foreach binders, before any process fixtures.
# Assigning to $null is the intentional output sink and remains allowed.
$readOnlyNames=@('HOME','Host','PID','PSHOME','PSVersionTable','PSEdition','PSCulture','PSUICulture',
    'IsWindows','IsLinux','IsMacOS','IsCoreCLR','ShellId','ExecutionContext','Error','true','false')
foreach ($file in @('desktop-live-welcome.ps1','desktop-live-welcome.tests.ps1',
    'desktop-welcome-step.ps1','desktop-welcome-step.tests.ps1')) {
    $parseTokens=$null; $parseErrors=$null
    $scriptAst=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file),[ref]$parseTokens,[ref]$parseErrors)
    Pass ($parseErrors.Count -eq 0) 'new script parses before fixture execution'
    $binders=@($scriptAst.FindAll({
        param($node)
        ($node -is [Management.Automation.Language.AssignmentStatementAst] -and
            $node.Left -is [Management.Automation.Language.VariableExpressionAst]) -or
        $node -is [Management.Automation.Language.ParameterAst] -or
        $node -is [Management.Automation.Language.ForEachStatementAst]
    },$true))
    foreach ($binder in $binders) {
        if ($binder -is [Management.Automation.Language.ParameterAst]) { $variable=$binder.Name }
        elseif ($binder -is [Management.Automation.Language.ForEachStatementAst]) { $variable=$binder.Variable }
        else { $variable=$binder.Left }
        $variableName=($variable.VariablePath.UserPath -split ':')[-1]
        Pass ($readOnlyNames -notcontains $variableName) 'new script must not bind read-only automatic variable'
    }
}
Assert-LiveWelcomeOptions $false 'baseline' $false $false
Assert-LiveWelcomeOptions $true 'observe' $true $true
foreach ($mode in @('baseline','bridge')) { Reject { Assert-LiveWelcomeOptions $true $mode $true $true } 'live mode guard' }
Reject { Assert-LiveWelcomeOptions $true 'observe' $false $true } 'startup guard'
Reject { Assert-LiveWelcomeOptions $true 'observe' $true $false } 'compatibility guard'
$ready=ConvertFrom-LiveJSON '{"schema":1,"scope":"desktop-loopback-fixtures-only","status":"ready","lifetime_seconds":180,"native_sha256":"40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39","catalog_blob":"489ebe3f2483348641a16160ce8491dd077eabca"}'
Assert-LiveReady $ready
$ready.lifetime_seconds=181; Reject { Assert-LiveReady $ready } 'readiness bound'; $ready.lifetime_seconds=180
$ready.native_sha256='bad'; Reject { Assert-LiveReady $ready } 'native pin'
Reject { ConvertFrom-LiveJSON '{"schema":1,"schema":1}' } 'duplicate metadata'
Reject { ConvertFrom-LiveJSON '{"outer":{"home":"a","HOME":"b"}}' } 'nested ambiguous metadata'
Reject { ConvertFrom-LiveJSON ('x'*8193) } 'bounded output'
$audit=ConvertFrom-LiveJSON '{"schema":1,"scope":"desktop-loopback-fixtures-only","status":"stopped","failure":"","alpha_requests":0,"beta_requests":0,"fixture_errors":0}'
Assert-LiveAudit $audit 0
Reject { Assert-LiveAudit $audit 1 } 'audit exit status'
$audit.alpha_requests=17; Reject { Assert-LiveAudit $audit 0 } 'audit request bound'; $audit.alpha_requests=0
$audit.fixture_errors=1; Reject { Assert-LiveAudit $audit 0 } 'audit error cannot stop'; $audit.fixture_errors=0
$audit.status='passed'; Reject { Assert-LiveAudit $audit 0 } 'fixture never conversation pass'
$scratch=Join-Path ([IO.Path]::GetTempPath()) 'synthetic-private-root'; $exe=Join-Path $scratch 'codex.exe'
$backends=@(); foreach ($id in @('alpha','beta')) { $backends += [pscustomobject]@{id=$id;provider=($id+'_provider');command=$exe;home=(Join-Path $scratch ($id+'-home'));codex_home=(Join-Path $scratch ($id+'-codex'));startup_probe_sha256='40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'} }
$config=[pscustomobject]@{backends=$backends;working_directory=(Join-Path $scratch 'workspace');control_plane='routing-v0.167.0-alpha.3.2';control_home=(Join-Path $scratch 'control-home')}
$owned=ConvertTo-LiveConfiguration $config $scratch $exe
Pass ($owned.Backends[0].command -ceq $exe) 'reconstructed executable'
$config.backends[0].command='other.exe'; Reject { ConvertTo-LiveConfiguration $config $scratch $exe } 'reject arbitrary executable'; $config.backends[0].command=$exe
$config.backends[1].home=$scratch; Reject { ConvertTo-LiveConfiguration $config $scratch $exe } 'reject path authority'; $config.backends[1].home=Join-Path $scratch 'beta-home'
$config.backends[0].provider='foreign'; Reject { ConvertTo-LiveConfiguration $config $scratch $exe } 'reject provider authority'; $config.backends[0].provider='alpha_provider'
$config | Add-Member NoteProperty extra 'unknown'; Reject { ConvertTo-LiveConfiguration $config $scratch $exe } 'reject extra config'
$welcome=ConvertFrom-LiveJSON '{"schema":1,"scope":"synthetic-welcome-only","status":"observed","reason":"single_transition_attempted","stage":"complete","actions":3,"suggestionsDisabled":true,"engineeringSelected":true,"continueInvoked":true,"nextScreen":"unreviewed_screen","targetObservations":null}'
Assert-WelcomeResult $welcome
$welcome.targetObservations=[pscustomobject]@{
    engineering=[pscustomobject]@{interactiveCount=1;controlType='RadioButton'}
    suggestions=[pscustomobject]@{interactiveCount=1;controlType='CheckBox'}
    continue=[pscustomobject]@{interactiveCount=1;controlType='Button'}
}
Assert-WelcomeResult $welcome
$welcome.targetObservations.engineering.controlType='raw UI text'
Reject { Assert-WelcomeResult $welcome } 'reject raw target text'
$welcome.targetObservations.engineering.controlType='RadioButton'
$welcome.targetObservations.engineering.interactiveCount=2
Reject { Assert-WelcomeResult $welcome } 'target ambiguity must be explicit'
$welcome.targetObservations.engineering.interactiveCount=1
$welcome.stage='raw exception'
Reject { Assert-WelcomeResult $welcome } 'reject raw stage'
$welcome.stage='complete'
$prior=[pscustomobject]@{status='observed';stage='native-complete';reason='original';exitCode=0}
Pass ((Get-LiveWelcomeOutcome $prior $true $welcome $true).stage -ceq 'single-welcome-transition-observed') 'narrow outcome'
Pass ((Get-LiveWelcomeOutcome $prior $false $welcome $true).exitCode -eq 1) 'fixture failure'
Pass ((Get-LiveWelcomeOutcome $prior $true $welcome $false).exitCode -eq 1) 'capture failure'
Pass ((Get-LiveWelcomeOutcome $prior $true $null $true).exitCode -eq 1) 'timeout or partial action never success'
$welcome.nextScreen='welcome_still_visible'; Pass ((Get-LiveWelcomeOutcome $prior $true $welcome $true).exitCode -eq 1) 'unchanged screen'
$welcome.actions=4; Reject { Assert-WelcomeResult $welcome } 'action bound'; $welcome.actions=3
$welcome.status='stopped'; $welcome.reason='identity_or_foreground_changed'; $welcome.stage='continue_invoke'; $welcome.nextScreen='not_observed'; $welcome.continueInvoked=$false
Assert-WelcomeResult $welcome
Pass ((Get-LiveWelcomeOutcome $prior $true $welcome $true).exitCode -eq 1) 'stopped partial action'
$prior.exitCode=1; $prior.reason='original Desktop or native failure'
Pass ((Get-LiveWelcomeOutcome $prior $false $null $false).reason -ceq $prior.reason) 'failure precedence'
# Exhausted initial shell-only snapshots are a distinct zero-action stop.
$notReady=ConvertFrom-LiveJSON '{"schema":1,"scope":"synthetic-welcome-only","status":"stopped","reason":"accessibility_not_ready","stage":"welcome_read","actions":0,"suggestionsDisabled":false,"engineeringSelected":false,"continueInvoked":false,"nextScreen":"not_observed","targetObservations":{"engineering":{"interactiveCount":0,"controlType":"absent"},"suggestions":{"interactiveCount":0,"controlType":"absent"},"continue":{"interactiveCount":0,"controlType":"absent"}}}'
Assert-WelcomeResult $notReady
$successfulPrior=[pscustomobject]@{status='observed';stage='native-complete';reason='original';exitCode=0}
Pass ((Get-LiveWelcomeOutcome $successfulPrior $true $notReady $true).exitCode -eq 1) 'accessibility readiness stop is not success'
foreach ($field in @('suggestionsDisabled','engineeringSelected','continueInvoked')) {
    $notReady.$field=$true
    Reject { Assert-WelcomeResult $notReady } 'readiness stop cannot claim completed action state'
    $notReady.$field=$false
}
$notReady.actions=1; Reject { Assert-WelcomeResult $notReady } 'readiness stop cannot follow an attempted action'; $notReady.actions=0
$notReady.stage='suggestions_off'; Reject { Assert-WelcomeResult $notReady } 'readiness stop is initial only'; $notReady.stage='welcome_read'
$notReady.targetObservations.engineering.interactiveCount=1; $notReady.targetObservations.engineering.controlType='RadioButton'
Reject { Assert-WelcomeResult $notReady } 'readiness stop must have absent targets'
$notReady.targetObservations.engineering.interactiveCount=0; $notReady.targetObservations.engineering.controlType='absent'
$notReady.reason='unknown_readiness_reason'; Reject { Assert-WelcomeResult $notReady } 'readiness reason vocabulary remains fixed'
# Exact package-relative executable spelling supports manifest separators only.
$packageRoot=Join-Path ([IO.Path]::GetTempPath()) 'synthetic-package'
$expectedExecutable=[IO.Path]::GetFullPath((Join-Path (Join-Path $packageRoot 'app') 'ChatGPT.exe'))
foreach ($relative in @('app/ChatGPT.exe','app\ChatGPT.exe')) {
    Pass ((Get-LiveDesktopExecutable $packageRoot $relative) -ceq $expectedExecutable) 'manifest executable separator normalization'
}
foreach ($relative in @('app/Other.exe','../ChatGPT.exe','app/../ChatGPT.exe','app/ChatGPT.exe/other','C:\foreign\ChatGPT.exe')) {
    Reject { Get-LiveDesktopExecutable $packageRoot $relative } 'unreviewed executable remains rejected'
}
# Test actual bounded process supervision using only a fresh local script. No UI.
Initialize-LiveSupervisor
$temp=Join-Path ([IO.Path]::GetTempPath()) ('live-supervisor-fixture-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $temp | Out-Null
try {
    $hostPath=(Get-Process -Id $PID).MainModule.FileName
    $scriptPath=Join-Path $temp 'child.ps1'
    [IO.File]::WriteAllText($scriptPath,"Start-Sleep -Seconds 60")
    $start=[Diagnostics.ProcessStartInfo]::new(); $start.FileName=$hostPath; $start.UseShellExecute=$false
    $start.RedirectStandardInput=$true; $start.RedirectStandardOutput=$true; $start.RedirectStandardError=$true
    foreach ($arg in @('-NoProfile','-File',$scriptPath)) { $start.ArgumentList.Add($arg) }
    $start.Environment.Clear()
    foreach ($key in @('SystemRoot','WINDIR','TEMP','TMP')) { $v=[Environment]::GetEnvironmentVariable($key); if($v){$start.Environment[$key]=$v} }
    $child=[DesktopLive.BoundedProcess]::new($start,8192,200)
    try { Pass ($child.Wait(10000) -and $child.TimedOut) 'external supervisor deadline' } finally { $child.Dispose() }
    [IO.File]::WriteAllText($scriptPath,"[Console]::Out.Write(('x'*20000))")
    $child=[DesktopLive.BoundedProcess]::new($start,8192,10000)
    try { $null=$child.Wait(15000); Start-Sleep -Milliseconds 100; Pass ($child.Overflow -and [Text.Encoding]::UTF8.GetByteCount($child.Output) -le 8192) 'stdout memory bound' } finally { $child.Dispose() }
    # Fixed supervisor diagnostics do not echo malformed output or stderr.
    [IO.File]::WriteAllText($scriptPath,"[Console]::Out.Write('{""PRIVATE_CANARY"":true}')")
    $diagnostic=[ordered]@{}
    Reject { Invoke-LiveWelcome $start -Diagnostic $diagnostic } 'invalid child schema is rejected'
    Pass ($diagnostic.stage -ceq 'helper_schema' -and $diagnostic.reason -ceq 'helper_schema_invalid' -and
        ($diagnostic | ConvertTo-Json -Compress) -notmatch 'PRIVATE_CANARY') 'fixed schema failure diagnostic'
    [IO.File]::WriteAllText($scriptPath,'exit 2')
    $diagnostic=[ordered]@{}
    Reject { Invoke-LiveWelcome $start -Diagnostic $diagnostic } 'nonzero helper exit is rejected'
    Pass ($diagnostic.reason -ceq 'helper_exit_nonzero' -and $diagnostic.exitCode -eq 2 -and
        $diagnostic.stdoutBytes -eq 0 -and -not $diagnostic.outputLimitExceeded) 'fixed exit diagnostic'
    $start.FileName=Join-Path $temp 'missing-helper.exe'
    $diagnostic=[ordered]@{}
    Reject { Invoke-LiveWelcome $start -Diagnostic $diagnostic } 'missing helper executable is rejected'
    Pass ($diagnostic.stage -ceq 'helper_start' -and $diagnostic.reason -ceq 'helper_start_failed') 'fixed launch failure diagnostic'
    # Windows-only, real PS5.1 argv/environment/schema smoke. Zero context stops
    # before Add-Type, Get-Process, UIA, foreground reads or any input action.
    if ($IsWindows) {
        $windowsHost=Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
        Pass ([IO.File]::Exists($windowsHost)) 'installed Windows PowerShell host exists'
        $launch=New-LiveStart -Executable $windowsHost -Directory $temp -Welcome -Arguments @(
            '-NoProfile','-NonInteractive','-File',(Join-Path $PSScriptRoot 'desktop-welcome-step.ps1'),
            '-WindowHandle','0','-OwnerProcessId','0','-OwnerStartUtcTicks','0',
            '-OwnerExecutable',$windowsHost,'-PackageFullName','Synthetic.InvalidContext','-SessionId','0')
        Pass ($launch.Environment['GITHUB_ACTIONS'] -ceq 'true' -and $launch.Environment['RUNNER_OS'] -ceq 'Windows' -and
            -not $launch.Environment.ContainsKey('GITHUB_TOKEN')) 'minimal helper launch environment'
        $diagnostic=[ordered]@{}
        try { $smoke=Invoke-LiveWelcome $launch -Diagnostic $diagnostic } catch {
            Write-Output ('WINDOWS_HELPER_LAUNCH_DIAGNOSTIC=' + ($diagnostic | ConvertTo-Json -Compress))
            throw 'Windows helper launch smoke failed before a validated result.'
        }
        Write-Output ('WINDOWS_HELPER_LAUNCH_RESULT=' + ($smoke | ConvertTo-Json -Depth 5 -Compress))
        Pass ($smoke.status -ceq 'stopped' -and $smoke.reason -ceq 'invalid_context' -and
            $smoke.stage -ceq 'initialization' -and $smoke.actions -eq 0 -and -not $smoke.continueInvoked -and
            $null -eq $smoke.targetObservations -and $diagnostic.reason -ceq 'helper_result_validated' -and
            -not $diagnostic.partialActionUncertain) 'real PS5.1 helper rejects zero context before UI'
        Write-Output 'WINDOWS_HELPER_LAUNCH_CHECK=invalid_context_verified'
    } else { Write-Output 'WINDOWS_HELPER_LAUNCH_CHECK=not_run_non_windows' }
} finally { Remove-Item -LiteralPath $temp -Recurse -Force }
Write-Output "LIVE_WELCOME_OFFLINE_CHECKS=$count"
exit 0
