# Offline only: mock registry snapshots and callbacks, never start Desktop or mutate environment.
[CmdletBinding()]
param([switch]$BreakRestoration)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'desktop-native-entry.ps1')
$count=0
function Pass([bool]$Condition, [string]$Name) { if (-not $Condition) { throw $Name }; $script:count++ }
function Reject([scriptblock]$Action, [string]$Name) { $failed=$false; try { $null=& $Action } catch { $failed=$true }; Pass $failed $Name }
$files=@('desktop-native-entry.ps1','desktop-entry-activate.ps1','desktop-native-entry.tests.ps1','test-desktop-cold-start.ps1')
foreach ($file in $files) {
    $tokens=$null; $errors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file),[ref]$tokens,[ref]$errors)
    Pass ($errors.Count -eq 0) ('PowerShell syntax: '+$file)
    if ($file -ceq 'test-desktop-cold-start.ps1') { continue }
    $binders=@($ast.FindAll({param($node)
        ($node -is [Management.Automation.Language.AssignmentStatementAst] -and $node.Left -is [Management.Automation.Language.VariableExpressionAst]) -or
        $node -is [Management.Automation.Language.ParameterAst] -or $node -is [Management.Automation.Language.ForEachStatementAst]
    },$true))
    foreach ($binder in $binders) {
        if ($binder -is [Management.Automation.Language.ParameterAst]) { $variable=$binder.Name }
        elseif ($binder -is [Management.Automation.Language.ForEachStatementAst]) { $variable=$binder.Variable }
        else { $variable=$binder.Left }
        Pass (@('HOME','Host','PID','PSHOME','PSVersionTable','PSEdition','PSCulture','PSUICulture','IsWindows','IsLinux','IsMacOS','true','false') -notcontains
            ($variable.VariablePath.UserPath -split ':')[-1]) 'no read-only automatic variable assignments'
    }
}
if ($BreakRestoration) {
    # Behavioral mutation: omit the second rollback write, but leave reads, activation and tests intact.
    $functionText=(Get-Command Restore-EntryOverrides).ScriptBlock.ToString()
    $needle='foreach ($name in Get-EntryVariableNames) {'
    Pass ($functionText.Contains($needle)) 'rollback mutation seam exists'
    $functionText=$functionText.Replace($needle,"foreach (`$name in @('CODEX_CLI_PATH')) {")
    Set-Item Function:Restore-EntryOverrides ([scriptblock]::Create($functionText))
}
function New-Snapshot {
    $snapshot=@{}
    foreach ($name in Get-EntryVariableNames) { $snapshot[$name]=[pscustomobject]@{Present=$false;Kind=$null;Value=$null} }
    return $snapshot
}
function Copy-Snapshot($Snapshot) {
    $copy=@{}
    foreach ($name in Get-EntryVariableNames) {
        $item=$Snapshot[$name]
        $copy[$name]=[pscustomobject]@{Present=$item.Present;Kind=$item.Kind;Value=$item.Value}
    }
    return $copy
}
# The entry opt-in uses only the pre-existing pinned genuine startup comparison.
$settingsFunction=@($ast.FindAll({param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'New-DesktopBridgeSettings'
},$true))
Pass ($settingsFunction.Count -eq 1) 'existing startup settings seam remains unique'
. ([scriptblock]::Create($settingsFunction[0].Extent.Text))
$backends=@([ordered]@{id='alpha';home='C:\alpha-home';codex_home='C:\alpha-codex'},
    [ordered]@{id='beta';home='C:\beta-home';codex_home='C:\beta-codex'})
$settings=New-DesktopBridgeSettings $backends $true $false 'codex-cli 0.167.0-alpha.3.2' '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
Pass ($settings.Count -eq 2 -and $settings.startup_probe -ceq 'compare-unrestricted-v0.167.0-alpha.3.2' -and
    $settings.backends.Count -eq 2 -and -not $settings.Contains('control_home')) 'entry enables only genuine startup comparison'
foreach ($backend in $settings.backends) { Pass ($backend.startup_probe_sha256 -ceq '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39') 'entry native executable pin' }
$defaults=New-DesktopBridgeSettings $backends $false $false '' ''
Pass ($defaults.Count -eq 1 -and -not $defaults.backends[0].Contains('startup_probe_sha256')) 'existing default configuration unchanged'
Reject { New-DesktopBridgeSettings $backends $true $false 'other-version' '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39' } 'entry rejects mismatching native version'
$workflow='breakingtime126/magpie/.github/workflows/experimental-desktop-native-entry.yml@refs/heads/experiment/app-server-bridge-v0.1.3035'
foreach ($case in @('baseline','treatment','restore-before-activation','alias-control')) {
    $mode='bridge'; if ($case -ceq 'baseline') { $mode='baseline' }
    Assert-EntryOptions $case $mode $false $workflow
    Reject { Assert-EntryOptions $case $mode $true $workflow } 'no accidental compatibility opt-in'
    Reject { Assert-EntryOptions $case $mode $false 'wrong' } 'exact workflow guard'
}
Reject { Assert-EntryOptions 'treatment' 'observe' $false $workflow } 'observer not normal entry'
Reject { Assert-EntryOptions 'unknown' 'bridge' $false $workflow } 'closed case set'
$before=New-Snapshot
Assert-EntryFresh $before $false $false $false $false
foreach ($position in 0..3) {
    $flags=@($false,$false,$false,$false); $flags[$position]=$true
    Reject { Assert-EntryFresh $before $flags[0] $flags[1] $flags[2] $flags[3] } 'dirty preflight rejected'
}
foreach ($kind in @([Microsoft.Win32.RegistryValueKind]::String,[Microsoft.Win32.RegistryValueKind]::ExpandString)) {
    $existing=Copy-Snapshot $before
    $existing.CODEX_CLI_PATH=[pscustomobject]@{Present=$true;Kind=$kind;Value=''}
    Reject { Assert-EntryFresh $existing $false $false $false $false } 'empty existing value rejected'
    Pass (-not (Test-EntrySnapshotsEqual $before $existing)) 'absence differs from empty'
    $copy=Copy-Snapshot $existing; $copy.CODEX_CLI_PATH.Value='%PRIVATE_TEST_ROOT%\bridge.exe'
    Pass (-not (Test-EntrySnapshotsEqual $copy $existing)) 'raw expandable data stays distinct'
    $different=Copy-Snapshot $copy
    $different.CODEX_CLI_PATH.Kind=[Microsoft.Win32.RegistryValueKind]::DWord
    Pass (-not (Test-EntrySnapshotsEqual $copy $different)) 'type matters to exact readback'
}
$values=@{CODEX_CLI_PATH='C:\owned\bridge.exe';MAGPIE_APP_SERVER_BRIDGE_CONFIG='C:\owned\bridge.json'}
$read={ Copy-Snapshot $script:store }
$write={ param($Name,$Item) $script:writes += $Name; $script:store[$Name]=$Item }
$broadcast={ $script:broadcasts++; return 'sent' }
$script:store=Copy-Snapshot $before; $script:writes=@(); $script:broadcasts=0
$state=New-EntryTransaction (Copy-Snapshot $before)
try { Set-EntryOverrides $state $values $read $write $broadcast }
finally { try { Restore-EntryOverrides $state $read $write $broadcast } catch { } }
Pass (Test-EntrySnapshotsEqual $before $script:store) 'treatment rollback removes both introduced values'
Pass ($state.SetVerified -and $state.RestoreVerified -and $script:broadcasts -eq 2) 'write and restoration readbacks plus notifications'
# First activation in rollback case can only run after exact readback, never against a previous instance.
$script:store=Copy-Snapshot $before; $state=New-EntryTransaction (Copy-Snapshot $before)
Set-EntryOverrides $state $values $read $write $broadcast
Restore-EntryOverrides $state $read $write $broadcast
Pass ($state.RestoreVerified -and (Test-EntrySnapshotsEqual $before $script:store)) 'restore-before-first-activation gate'
foreach ($failAfterChange in @($false,$true)) {
    $script:store=Copy-Snapshot $before; $state=New-EntryTransaction (Copy-Snapshot $before)
    $partialWrite={ param($Name,$Item)
        if ($Name -ceq 'MAGPIE_APP_SERVER_BRIDGE_CONFIG') {
            if ($failAfterChange) { $script:store[$Name]=$Item }
            throw 'synthetic write failure'
        }
        $script:store[$Name]=$Item
    }
    try { Reject { Set-EntryOverrides $state $values $read $partialWrite $broadcast } 'partial write throws' }
    finally { Restore-EntryOverrides $state $read $write $broadcast }
    Pass ($state.RestoreVerified -and (Test-EntrySnapshotsEqual $before $script:store)) 'finally restores after partial uncertain write'
}
$script:store=Copy-Snapshot $before; $state=New-EntryTransaction (Copy-Snapshot $before)
try { Reject { Set-EntryOverrides $state $values $read $write { throw 'synthetic broadcast failure' } } 'broadcast failure surfaced' }
finally { Restore-EntryOverrides $state $read $write $broadcast }
Pass $state.RestoreVerified 'broadcast error cannot prevent exact persistent rollback'
$script:store=Copy-Snapshot $before; $state=New-EntryTransaction (Copy-Snapshot $before)
Set-EntryOverrides $state $values $read $write $broadcast
$script:writes=@()
Reject { Restore-EntryOverrides $state $read {
    param($Name,$Item)
    $script:writes += $Name
    if ($Name -ceq 'CODEX_CLI_PATH') { throw 'synthetic restore failure' }
    $script:store[$Name]=$Item
} $broadcast } 'restoration failure not hidden'
Pass ($script:writes.Count -eq 2 -and -not $script:store.MAGPIE_APP_SERVER_BRIDGE_CONFIG.Present -and -not $state.RestoreVerified) 'restore attempts both keys independently'
# Always finish mock state, even though no OS state was changed.
Restore-EntryOverrides $state $read $write { 'unconfirmed' }
Pass ($state.RestoreVerified -and $state.RestoreBroadcast -ceq 'unconfirmed') 'registry restore and notification evidence stay separate'
$state=New-EntryTransaction (Copy-Snapshot $before); $script:store=Copy-Snapshot $before
$script:store.CODEX_CLI_PATH=[pscustomobject]@{Present=$true;Kind=[Microsoft.Win32.RegistryValueKind]::String;Value='other'}
$script:writes=@()
Reject { Set-EntryOverrides $state $values $read $write $broadcast } 'concurrent state change rejected'
Pass ($script:writes.Count -eq 0 -and -not $state.MayHaveChanged) 'preflight mismatch writes nothing'
# No values inherited from the GitHub runner or persistent registry enter the caller's explicit environment.
$start=New-EntryActivationStart 'C:\owned\desktop-entry-activate.ps1' 'OpenAI.Codex_test!App'
foreach ($name in @('CODEX_CLI_PATH','MAGPIE_APP_SERVER_BRIDGE_CONFIG','CODEX_HOME','GITHUB_TOKEN','ACTIONS_RUNTIME_TOKEN','GITHUB_SHA')) {
    Pass (-not $start.EnvironmentVariables.ContainsKey($name)) ('caller excludes '+$name)
}
foreach ($case in @('treatment','alias-control')) {
    Pass ((Get-EntryOutcome $case 1 1 0 $true $true $false $true) -ceq 'bridge-image-observed') 'verified exact bridge selection'
    Pass ((Get-EntryOutcome $case 1 0 0 $true $true $false $true) -ceq 'bridge-start-event-only') 'brief child not upgraded to image proof'
    Pass ((Get-EntryOutcome $case 0 0 1 $true $true $false $true) -ceq 'original-native-observed-no-bridge') 'native observation not a global noninheritance claim'
    Pass ((Get-EntryOutcome $case 0 0 0 $true $true $false $true) -ceq 'inheritance-unknown') 'no observations remains unknown'
    Pass ((Get-EntryOutcome $case 1 1 1 $true $true $false $true) -ceq 'bridge-image-observed') 'native fallback does not erase bridge selection'
}
foreach ($case in @('baseline','restore-before-activation')) {
    Pass ((Get-EntryOutcome $case 0 0 1 $true $true $false $true) -ceq 'original-native-observed') 'exact original native control'
    Pass ((Get-EntryOutcome $case 1 0 1 $true $true $false $true) -ceq 'unexpected-bridge') 'unexpected override in original control'
}
Pass ((Get-EntryOutcome 'treatment' 1 1 0 $false $true $false $true) -ceq 'cleanup-unconfirmed') 'unknown activation exit fails'
Pass ((Get-EntryOutcome 'treatment' 1 1 0 $true $false $false $true) -ceq 'cleanup-unconfirmed') 'unknown persistent restoration fails'
Pass ((Get-EntryOutcome 'treatment' 1 1 0 $true $true $true $true) -ceq 'cleanup-unconfirmed') 'cleanup error cannot pass'
Pass ((Get-EntryOutcome 'treatment' 1 1 0 $true $true $false $false) -ceq 'capture-failed') 'earlier observation failure cannot pass'
# Exercise real bounded helper lifecycle without registry, package, network or UI operations.
$hostPath=(Get-Process -Id $PID).Path
foreach ($fixture in @('normal','overflow','timeout')) {
    $start=New-Object Diagnostics.ProcessStartInfo
    $start.FileName=$hostPath; $start.UseShellExecute=$false; $start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true; $start.RedirectStandardError=$true
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($value) { $start.EnvironmentVariables[$name]=$value }
    }
    $command="[Console]::WriteLine('fixture-ok')"; $deadline=10000
    if ($fixture -ceq 'overflow') { $command="[Console]::Write(('x' * 4096))" }
    if ($fixture -ceq 'timeout') { $command='Start-Sleep -Seconds 30'; $deadline=1000 }
    $start.Arguments='-NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
    $script:entryHelperExitUnconfirmed=$false
    if ($fixture -ceq 'normal') { Pass ((Invoke-EntryBoundedHelper $start $deadline) -ceq 'fixture-ok') 'bounded helper normal output' }
    else { Reject { Invoke-EntryBoundedHelper $start $deadline } ('bounded helper rejects '+$fixture) }
    Pass (-not $script:entryHelperExitUnconfirmed) 'owned helper exit confirmed after success or failure'
}
# Compile and exercise the actual Windows observer in the matrix's host. The fixture
# waits for stdin EOF instead of racing a short sleep; all handles/processes are owned.
if ($PSVersionTable.PSEdition -ceq 'Desktop') {
    Initialize-EntryImageObserver
    $observer=$null; $fixtureProcess=$null
    try {
        $observer=[DesktopEntryImages.Observer]::new($hostPath,$hostPath)
        $start.RedirectStandardInput=$true
        $command='[Console]::In.ReadToEnd() | Out-Null'
        $start.Arguments='-NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
        $fixtureProcess=[Diagnostics.Process]::Start($start)
        $null=$fixtureProcess.Handle
        $watch=[Diagnostics.Stopwatch]::StartNew(); $matches=@()
        while ($watch.Elapsed.TotalSeconds -lt 15) {
            $matches=@($observer.Snapshot() | Where-Object { $_.ProcessId -eq $fixtureProcess.Id })
            if ($matches.Count -gt 0) { break }
            Start-Sleep -Milliseconds 50
        }
        Pass ($matches.Count -eq 1 -and $matches[0].Path -ieq $hostPath -and
            $matches[0].ParentId -eq $PID -and $matches[0].CreatedTicks -eq $fixtureProcess.StartTime.ToUniversalTime().Ticks -and
            $matches[0].ParentCreatedTicks -eq (Get-Process -Id $PID).StartTime.ToUniversalTime().Ticks) 'event image holds exact child and parent creation identity'
        $observer.Dispose(); $observer.Dispose()
        $snapshotCount=$observer.Snapshot().Count
        $fixtureProcess.StandardInput.Close()
        Pass ($fixtureProcess.WaitForExit(5000)) 'observer fixture exits after stdin EOF'
        Pass ($observer.Snapshot().Count -eq $snapshotCount) 'stopped observer snapshot is stable'
    } finally {
        try { if ($null -ne $observer) { $observer.Dispose() } }
        finally {
            if ($null -ne $fixtureProcess) {
                try {
                    if (-not $fixtureProcess.HasExited) { $fixtureProcess.Kill() }
                    Pass ($fixtureProcess.WaitForExit(5000)) 'owned observer fixture cleanup confirmed'
                } finally { $fixtureProcess.Dispose() }
            }
        }
    }
}
Write-Output ('Native entry offline safety assertions passed: '+$count)
