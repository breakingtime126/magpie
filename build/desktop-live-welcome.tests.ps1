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
} finally { Remove-Item -LiteralPath $temp -Recurse -Force }
Write-Output "LIVE_WELCOME_OFFLINE_CHECKS=$count"
exit 0
