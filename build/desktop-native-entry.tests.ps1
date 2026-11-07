# Offline only: mock registry snapshots and callbacks, never start Desktop or mutate environment.
[CmdletBinding()]
param([switch]$BreakRestoration, [string]$DiagnosticPath='', [switch]$TreatmentPrerequisites)
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'desktop-native-entry.ps1')
. (Join-Path $PSScriptRoot 'desktop-entry-observe.ps1')
$script:entryLastHelperEvidence=$null
$fixtureScope='all'; $observationStatus='not-started'
if ($TreatmentPrerequisites) { $fixtureScope='treatment'; $observationStatus='not-run' }
$script:entrySafetyDiagnostic=[ordered]@{schema=1;scope='native-entry-safety-only';host=[string]$PSVersionTable.PSEdition;
    fixtureScope=$fixtureScope;status='running';zeroRecords=$observationStatus;ownedProcess=$observationStatus;lastHelper=$null}
function Save-EntrySafetyDiagnostic([string]$Status) {
    if ([string]::IsNullOrEmpty($DiagnosticPath)) { return }
    $script:entrySafetyDiagnostic.status=$Status
    if ($null -ne $script:entryLastHelperEvidence) {
        # Fixed supervisor labels, booleans and phase only. Never exception/input/stream text.
        $safe=[ordered]@{}
        foreach ($name in @('start','waitExpired','timerExpired','overflowOrReadError','streamsCompleted','exit','phase','cleanup')) {
            $safe[$name]=$script:entryLastHelperEvidence[$name]
        }
        $script:entrySafetyDiagnostic.lastHelper=$safe
    }
    [IO.File]::WriteAllText($DiagnosticPath,($script:entrySafetyDiagnostic | ConvertTo-Json -Depth 4 -Compress),
        (New-Object Text.UTF8Encoding($false)))
}
trap {
    $entrySafetyFailure=$_
    try { Save-EntrySafetyDiagnostic 'failed' } catch { Write-Output 'ENTRY_SAFETY_DIAGNOSTIC_WRITE_FAILED' }
    throw $entrySafetyFailure
}
Save-EntrySafetyDiagnostic 'running'
$count=0
function Pass([bool]$Condition, [string]$Name) { if (-not $Condition) { throw $Name }; $script:count++ }
function Reject([scriptblock]$Action, [string]$Name) { $failed=$false; try { $null=& $Action } catch { $failed=$true }; Pass $failed $Name }
$files=@('desktop-native-entry.ps1','desktop-entry-activate.ps1','desktop-native-entry.tests.ps1','desktop-entry-observe.ps1','test-desktop-cold-start.ps1')
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
# Closing the harness-owned --version/--check processes releases their Windows
# process objects. This helper never touches a PID discovered by the guard.
$probeFunction=@($ast.FindAll({param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Complete-DesktopProbeProcess'
},$true))
Pass ($probeFunction.Count -eq 1) 'owned probe cleanup seam remains unique'
. ([scriptblock]::Create($probeFunction[0].Extent.Text))
function New-ProbeMock([bool]$Exited, [bool]$WaitResult=$true, [bool]$FailKill=$false) {
    $mock=[pscustomobject]@{Handle=[IntPtr]42;HasExited=$Exited;WaitResult=$WaitResult;FailKill=$FailKill;
        KillCalls=0;WaitCalls=0;Disposed=$false}
    $mock | Add-Member ScriptMethod Kill {
        $this.KillCalls++; if ($this.FailKill) { throw 'synthetic kill failure' }
        if ($this.WaitResult) { $this.HasExited=$true }
    }
    $mock | Add-Member ScriptMethod WaitForExit { param($Milliseconds)
        $this.WaitCalls++; if ($Milliseconds -ne 5000) { throw 'unexpected wait bound' }; return $this.WaitResult
    }
    $mock | Add-Member ScriptMethod Dispose { $this.Disposed=$true }
    return $mock
}
$probe=New-ProbeMock $true
Pass ((Complete-DesktopProbeProcess $probe) -and $probe.Disposed -and $probe.KillCalls -eq 0) 'exited owned probe closes without termination'
$probe=New-ProbeMock $false
Pass ((Complete-DesktopProbeProcess $probe) -and $probe.Disposed -and $probe.KillCalls -eq 1) 'active owned probe is reaped only through its held object'
foreach ($failKill in @($false,$true)) {
    $probe=New-ProbeMock $false $false $failKill
    Reject { Complete-DesktopProbeProcess $probe } 'unknown probe exit cannot pass'
    Pass $probe.Disposed 'owned probe object releases even on early cleanup failure'
}
$rows=@([pscustomobject]@{ProcessId=11;Name='Codex.exe'},
    [pscustomobject]@{ProcessId=12;Name='fixture-bridge.exe'},
    [pscustomobject]@{ProcessId=13;Name='ChatGPT.exe'},
    [pscustomobject]@{ProcessId=14;Name='private-unreported-name'},
    [pscustomobject]@{ProcessId=15;Name='unrelated-unreported-name'})
$facts=Get-EntryFreshnessEvidence $rows 'fixture.package' 'fixture-bridge.exe' 11 12 { param($ProcessId)
    if ($ProcessId -eq 14) { 'fixture.package' } else { $null }
}
Pass ($facts.matches -eq 4 -and $facts.versionProbePid -eq 1 -and $facts.configurationProbePid -eq 1 -and
    $facts.other -eq 2 -and $facts.package -eq 1) 'guard retains every matched process, including known probe PID candidates'
Pass (($facts | ConvertTo-Json -Depth 4 -Compress) -notmatch 'private-unreported|unrelated-unreported|fixture.package') 'freshness diagnostics reveal only restricted executable basenames and bounded metadata'
Pass ($facts.records.Count -eq 4 -and -not $facts.recordsTruncated -and
    $facts.records[3].basename -ceq 'unavailable' -and $facts.records[3].packageMatch -and
    $null -eq $facts.records[3].parentProcessId -and $null -eq $facts.records[3].createdUtc -and
    $facts.recordScope -ceq 'cim-snapshot-unbound-to-package-query') 'invalid basename and missing metadata remain unavailable without dropping the guard match'
$freshnessCreated=[DateTime]::new(2026,11,7,8,0,0,[DateTimeKind]::Utc)
$publicRows=@([pscustomobject]@{ProcessId=21;Name='rUnTiMeBrOkEr.ExE';ParentProcessId=[uint32]22;CreationDate=$freshnessCreated;
    CommandLine='private-command-canary';ExecutablePath='C:\private-path-canary\RuntimeBroker.exe'},
    [pscustomobject]@{ProcessId=22;Name='RuntimeBroker.exe.private-name-canary';ParentProcessId=-1;CreationDate='private-date-canary'},
    [pscustomobject]@{ProcessId=23;Name='private-name-canary';ParentProcessId=[long]4294967296;CreationDate=$null},
    [pscustomobject]@{ProcessId=24;Name='private-name-canary';ParentProcessId='private-parent-canary';CreationDate=$null},
    [pscustomobject]@{ProcessId=25;Name='private-name-canary';ParentProcessId=0;CreationDate=$null})
$script:freshnessPackageReads=0
$publicFacts=Get-EntryFreshnessEvidence $publicRows 'fixture.package' 'fixture-bridge.exe' 0 0 {
    param($ProcessId) $script:freshnessPackageReads++; 'fixture.package'
}
Pass ($publicFacts.matches -eq 5 -and $publicFacts.package -eq 5 -and $publicFacts.other -eq 5 -and
    $script:freshnessPackageReads -eq 5 -and $publicFacts.records.Count -eq 4 -and $publicFacts.recordsTruncated) 'bounded records never truncate guard counts or add package queries'
Pass ($publicFacts.records[0].processId -eq 21 -and $publicFacts.records[0].basename -ceq 'rUnTiMeBrOkEr.ExE' -and
    $publicFacts.records[0].parentProcessId -eq 22 -and
    $publicFacts.records[0].createdUtc -ceq '2026-11-07T08:00:00.0000000Z') 'restricted basename and typed CIM metadata are preserved'
foreach ($record in @($publicFacts.records | Select-Object -Skip 1)) {
    Pass ($record.basename -ceq 'unavailable' -and $null -eq $record.parentProcessId -and
        $null -eq $record.createdUtc) 'invalid optional metadata stays null and invalid basenames remain unavailable'
}
$freshnessJson=$publicFacts | ConvertTo-Json -Depth 4 -Compress
Pass ($freshnessJson -notmatch 'private-|CommandLine|ExecutablePath|fixture.package') 'freshness evidence never includes raw command, path, package name or unreviewed strings'
foreach ($record in $publicFacts.records) {
    Pass (($record.Keys -join ',') -ceq 'processId,basename,parentProcessId,createdUtc,packageMatch') 'freshness record has a closed public schema'
}
foreach ($nameCase in @('unlisted-public-tool.exe', ('a'*92+'.exe'))) {
    $nameFacts=Get-EntryFreshnessEvidence @([pscustomobject]@{ProcessId=31;Name=$nameCase}) 'fixture.package' 'fixture-bridge.exe' 0 0 { 'fixture.package' }
    Pass ($nameFacts.records[0].basename -ceq $nameCase) 'an unlisted executable and the 96-character boundary remain identifiable'
}
foreach ($nameCase in @(('a'*93+'.exe'), 'C:\private-path-canary.exe','private/path-canary.exe',
    "private`ncontrol-canary.exe", "private.exe`n", "private.exe`r", "private.exe`0", 'private-canary.txt',
    ('private'+[char]0x00e9+'.exe'), '.exe', 'private:canary.exe', 'private canary.exe')) {
    $nameFacts=Get-EntryFreshnessEvidence @([pscustomobject]@{ProcessId=31;Name=$nameCase}) 'fixture.package' 'fixture-bridge.exe' 0 0 { 'fixture.package' }
    Pass ($nameFacts.matches -eq 1 -and $nameFacts.records[0].basename -ceq 'unavailable') 'invalid basename never leaks or removes a guard match'
}
$emptyFacts=Get-EntryFreshnessEvidence @() 'fixture.package' 'fixture-bridge.exe' 0 0 { $null }
Pass ($emptyFacts.matches -eq 0 -and $emptyFacts.records.Count -eq 0 -and -not $emptyFacts.recordsTruncated) 'empty input produces no invented process evidence'
Reject { Get-EntryFreshnessEvidence $rows '' 'fixture-bridge.exe' 11 12 { $null } } 'empty expected package never matches unknown identities'
Reject { Get-EntryFreshnessEvidence $rows 'fixture.package' 'fixture-bridge.exe' 11 12 { throw 'synthetic read failure' } } 'freshness read errors fail closed'
# Synthetic manifest only. This is not a signed package, SCM query or service exemption.
$serviceExtension='<d:Extension Category="windows.service" Executable="app/resources/codex-windows-sandbox-service.exe"><d:Service Name="CodexSandboxService.OpenAI.Codex" StartupType="auto" StartAccount="localSystem" /></d:Extension>'
$serviceManifest='<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10" xmlns:d="http://schemas.microsoft.com/appx/manifest/desktop/windows10/6"><Applications><Application Id="App"><Extensions>'+$serviceExtension+'</Extensions></Application></Applications></Package>'
$serviceDeclaration=Get-EntryServiceDeclaration ([xml]$serviceManifest)
Pass ($serviceDeclaration.name -ceq 'CodexSandboxService.OpenAI.Codex' -and
    $serviceDeclaration.path -ceq 'app/resources/codex-windows-sandbox-service.exe' -and
    $serviceDeclaration.startupType -ceq 'auto' -and $serviceDeclaration.startAccount -ceq 'localSystem') 'exact explicit service declaration is required'
$packageService=$serviceManifest.Replace('<Applications><Application Id="App">','').Replace('</Application></Applications>','')
Reject { Get-EntryServiceDeclaration ([xml]$packageService) } 'windows.service belongs to application Extensions, never package Extensions'
Pass ($serviceDeclaration.startupTypeSource -ceq 'declared' -and $serviceDeclaration.startAccountSource -ceq 'declared' -and
    $serviceDeclaration.argumentsSource -ceq 'not-declared') 'schema metadata source does not invent runtime account or arguments'
foreach ($startup in @('manual','disabled')) {
    $declared=Get-EntryServiceDeclaration ([xml]$serviceManifest.Replace('StartupType="auto"',('StartupType="'+$startup+'"')))
    Pass ($declared.startupType -ceq $startup -and $declared.startupTypeSource -ceq 'declared') 'other documented startup values are metadata, not a presumed auto identity'
}
foreach ($account in @('localService','networkService')) {
    $declared=Get-EntryServiceDeclaration ([xml]$serviceManifest.Replace('StartAccount="localSystem"',('StartAccount="'+$account+'"')))
    Pass ($declared.startAccount -ceq $account -and $declared.startAccountSource -ceq 'declared') 'other documented accounts are metadata, not a presumed LocalSystem identity'
}
$serviceArguments=$serviceManifest.Replace('StartAccount="localSystem"','StartAccount="localSystem" Arguments="--service"')
$declared=Get-EntryServiceDeclaration ([xml]$serviceArguments)
Pass ($declared.arguments -ceq '--service' -and $declared.argumentsSource -ceq 'declared') 'only the source-reviewed optional service argument is accepted'
foreach ($change in @(
    @($serviceExtension,'','service_missing'), @($serviceExtension,($serviceExtension+$serviceExtension),'service_ambiguous'),
    @('desktop/windows10/6','desktop/windows10/unknown','service_namespace'), @('Id="App"','Id="Other"','service_location'),
    @('Executable="app/resources/codex-windows-sandbox-service.exe"','','service_path'),
    @('app/resources/codex-windows-sandbox-service.exe','app/resources/../codex-windows-sandbox-service.exe','service_path'),
    @('Name="CodexSandboxService.OpenAI.Codex"','','service_name'), @('StartupType="auto"','','service_startup'),
    @('StartAccount="localSystem"','','service_account'), @('StartupType="auto"','StartupType="private-startup"','service_startup'),
    @('StartAccount="localSystem"','StartAccount="private-account"','service_account'),
    @('StartAccount="localSystem"','StartAccount="localSystem" Arguments="private-unreviewed-argument"','service_arguments'),
    @('StartAccount="localSystem"','StartAccount="localSystem" Arguments=""','service_arguments'),
    @('StartAccount="localSystem"','StartAccount="localSystem" d:Arguments="--service"','service_arguments'),
    @('Category="windows.service"','Category="windows.service" d:Parameters="private-extra-argument"','service_arguments'),
    @('Id="App"','Id="App" d:Parameters="private-extra-argument"','service_arguments')
)) {
    $changedManifest=$serviceManifest.Replace($change[0],$change[1]); $declarationFailure=''
    try { $null=Get-EntryServiceDeclaration ([xml]$changedManifest) } catch { $declarationFailure=$_.Exception.Message }
    Pass ($declarationFailure -ceq $change[2] -and $declarationFailure -notmatch 'private-') 'invalid declaration fails with its fixed reason and never leaks unknown data'
}
$singleServiceFacts=[ordered]@{matches=1;package=1;desktopName=0;nativeName=0;bridgeName=0;versionProbePid=0;configurationProbePid=0;other=1}
Pass (Test-EntrySingleServiceCandidate $singleServiceFacts) 'one package-only match is only a candidate for independent verification'
foreach ($field in @('matches','package','desktopName','nativeName','bridgeName','versionProbePid','configurationProbePid','other')) {
    $changedFacts=[ordered]@{};foreach ($key in $singleServiceFacts.Keys) { $changedFacts[$key]=$singleServiceFacts[$key] }
    $changedFacts[$field]=[int]$changedFacts[$field]+1
    Reject { if (-not (Test-EntrySingleServiceCandidate $changedFacts)) { throw 'blocked' } } 'additional, native, Desktop or bridge matches cannot be exempted'
}
function New-ServiceHeldMock {
    $mock=[pscustomobject]@{Alive=$true;CreatedUtcTicks=[long]200;ImagePath='C:\package\service.exe';Relation='expected'}
    $mock | Add-Member ScriptMethod PackageRelation { param($Expected) return $this.Relation }
    return $mock
}
$heldService=New-ServiceHeldMock
Pass (@(Assert-EntryServiceBinding $heldService 31 200 100 'fixture.package' 'C:\package\service.exe' 31).Count -eq 0) 'held service binding accepts only complete matching evidence'
foreach ($changedField in @('Alive','CreatedUtcTicks','ImagePath','Relation')) {
    $heldService=New-ServiceHeldMock
    switch ($changedField) {
        'Alive' { $heldService.Alive=$false }
        'CreatedUtcTicks' { $heldService.CreatedUtcTicks=201 }
        'ImagePath' { $heldService.ImagePath='C:\other\service.exe' }
        'Relation' { $heldService.Relation='other' }
    }
    Reject { Assert-EntryServiceBinding $heldService 31 200 100 'fixture.package' 'C:\package\service.exe' 31 } 'exit, generation, image or package mismatch remains blocking'
}
$heldService=New-ServiceHeldMock
Reject { Assert-EntryServiceBinding $heldService 31 200 100 'fixture.package' 'C:\package\service.exe' 32 } 'SCM PID mismatch remains blocking'
Reject { Assert-EntryServiceBinding $heldService 31 200 201 'fixture.package' 'C:\package\service.exe' 31 } 'a service predating this run remains blocking'
$workflow='breakingtime126/magpie/.github/workflows/experimental-desktop-native-entry.yml@refs/heads/experiment/app-server-bridge-v0.1.3035'
foreach ($case in @('baseline','treatment','restore-before-activation','alias-control','freshness-observation')) {
    $mode='bridge'; if ($case -cin @('baseline','freshness-observation')) { $mode='baseline' }
    Assert-EntryOptions $case $mode $false $workflow
    Reject { Assert-EntryOptions $case $mode $true $workflow } 'no accidental compatibility opt-in'
    Reject { Assert-EntryOptions $case $mode $false 'wrong' } 'exact workflow guard'
}
Reject { Assert-EntryOptions 'treatment' 'observe' $false $workflow } 'observer not normal entry'
Reject { Assert-EntryOptions 'unknown' 'bridge' $false $workflow } 'closed case set'
$entry4239Workflow='breakingtime126/magpie/.github/workflows/experimental-desktop-native-entry-4239.yml@refs/heads/experiment/app-server-bridge-v0.1.3035'
$entry4239Marker='user-aumid-4239-v1'
Assert-EntryOptions 'treatment' 'bridge' $false $entry4239Workflow $true $entry4239Marker
foreach ($case in @('', 'baseline','restore-before-activation','alias-control','freshness-observation','unknown')) {
    Reject { Assert-EntryOptions $case 'bridge' $false $entry4239Workflow $true $entry4239Marker } '4239 entry permits treatment only'
}
foreach ($mode in @('baseline','observe')) {
    Reject { Assert-EntryOptions 'treatment' $mode $false $entry4239Workflow $true $entry4239Marker } '4239 entry forbids alternate launch modes'
}
foreach ($marker in @('', 'wrong')) {
    Reject { Assert-EntryOptions 'treatment' 'bridge' $false $entry4239Workflow $true $marker } '4239 entry requires its exact opt-in marker'
}
Reject { Assert-EntryOptions 'treatment' 'bridge' $true $entry4239Workflow $true $entry4239Marker } '4239 entry rejects every additional opt-in'
Reject { Assert-EntryOptions 'treatment' 'bridge' $false $workflow $true $entry4239Marker } 'old workflow cannot enable 4239 entry'
Reject { Assert-EntryOptions 'treatment' 'bridge' $false $entry4239Workflow $false $entry4239Marker } 'new workflow cannot implicitly select 4239 entry'
Reject { Assert-EntryOptions 'treatment' 'bridge' $false $workflow $false $entry4239Marker } 'entry marker cannot change old default behavior'
$entry4239Hash=Get-EntryNativeHash $true
Pass ($entry4239Hash -ceq '4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382' -and
    (Get-EntryNativeHash) -ceq '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39') 'entry native image oracles select an exact independent digest'
$entry4239Settings=New-DesktopBridgeSettings $backends $true $false 'codex-cli 0.167.0-alpha.3.2' $entry4239Hash '' $false $true
Pass ($entry4239Settings.Count -eq 2 -and $entry4239Settings.startup_probe -ceq 'compare-unrestricted-v0.167.0-alpha.3.2') '4239 entry selects only startup comparison'
foreach ($backend in $entry4239Settings.backends) { Pass ($backend.startup_probe_sha256 -ceq $entry4239Hash) '4239 entry pins both original native children' }
Reject { New-DesktopBridgeSettings $backends $true $false 'codex-cli 0.167.0-alpha.3.2' (Get-EntryNativeHash) '' $false $true } '4239 entry rejects old same-version backend'
Reject { New-DesktopBridgeSettings $backends $false $false 'codex-cli 0.167.0-alpha.3.2' $entry4239Hash '' $false $true } '4239 entry cannot skip genuine startup comparison'
Reject { New-DesktopBridgeSettings $backends $true $true 'codex-cli 0.167.0-alpha.3.2' $entry4239Hash 'C:\control' $false $true } '4239 entry cannot select launch compatibility'
Reject { New-DesktopBridgeSettings $backends $true $true 'codex-cli 0.167.0-alpha.3.2' $entry4239Hash 'C:\control' $true $true } '4239 entry cannot combine with the UI build profile'
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
# Native control preserves the old allowlist but does not invoke a PowerShell child.
$controlPath='C:\owned\entry-activation-control.exe'
$controlStart=New-EntryControlStart $controlPath 'activate' 'OpenAI.Codex_test!App'
Pass ($controlStart.FileName -ceq $controlPath -and $controlStart.Arguments -ceq 'activate "OpenAI.Codex_test!App"' -and
    -not $controlStart.UseShellExecute -and $controlStart.RedirectStandardOutput -and $controlStart.RedirectStandardError) 'native host retains explicit non-shell process construction'
foreach ($name in @('CODEX_CLI_PATH','MAGPIE_APP_SERVER_BRIDGE_CONFIG','CODEX_HOME','GITHUB_TOKEN','ACTIONS_RUNTIME_TOKEN','GITHUB_SHA')) {
    Pass (-not $controlStart.EnvironmentVariables.ContainsKey($name)) ('native control excludes '+$name)
}
Pass ((New-EntryControlStart $controlPath 'notify').Arguments -ceq 'notify') 'notify has no AUMID or arbitrary arguments'
Reject { New-EntryControlStart $controlPath 'notify' 'OpenAI.Codex_test!App' } 'notify cannot hide an activation target'
Reject { New-EntryControlStart $controlPath 'unknown' } 'control action set is closed'
Reject { New-EntryControlStart 'C:\owned\other.exe' 'self-test' } 'only the private control basename is accepted'
Reject { New-EntryControlStart $controlPath 'activate' 'private-invalid-target' } 'invalid activation target cannot reach a child'
. (Join-Path $PSScriptRoot 'desktop-entry-activate.ps1')
if ($PSVersionTable.PSEdition -ceq 'Desktop') {
    Write-Output 'ENTRY_SAFETY_FIXTURE=compiled-control-no-action'
    $controlScratch=Join-Path ([IO.Path]::GetTempPath()) ('entry-control-test-'+[guid]::NewGuid().ToString('N'))
    $null=[IO.Directory]::CreateDirectory($controlScratch); $controlHost=$null
    try {
        $controlHost=New-EntryActivationHost (Join-Path $controlScratch 'entry-activation-control.exe')
        $nativeCheck=New-EntryControlStart $controlHost.Path 'self-test'
        Pass ((Invoke-EntryBoundedHelper $nativeCheck 10000) -ceq 'entry-host-ready') 'real compiled host consumes raw EOF without broadcast or activation'
        Pass ($controlHost.Sha256 -cmatch '^[a-f0-9]{64}$') 'private compiled artifact has a recorded digest'
        Reject {
            $writer=$null
            try { $writer=[IO.File]::Open($controlHost.Path,[IO.FileMode]::Open,[IO.FileAccess]::Write,[IO.FileShare]::ReadWrite) }
            finally { if ($null -ne $writer) { $writer.Dispose() } }
        } 'compiled host remains read-locked until caller cleanup'
        foreach ($invalidArguments in @('', 'unknown', 'activate "private-invalid-target"', 'activate "OpenAI.Codex_test!App" extra')) {
            $nativeCheck=New-EntryControlStart $controlHost.Path 'self-test'; $nativeCheck.Arguments=$invalidArguments
            Reject { Invoke-EntryBoundedHelper $nativeCheck 10000 } 'invalid native request fails before any Windows activation or notification API'
        }
        foreach ($overrideName in @('CODEX_CLI_PATH','magpie_app_server_bridge_config')) {
            $nativeCheck=New-EntryControlStart $controlHost.Path 'self-test'
            $nativeCheck.EnvironmentVariables[$overrideName]='private-override-canary'
            Reject { Invoke-EntryBoundedHelper $nativeCheck 10000 } 'native host rejects case-insensitive inherited override even in self-test'
        }
        Reject { New-EntryActivationHost $controlHost.Path } 'existing compiled artifact is never overwritten'
        # Synthetic returned-result cases, never an actual COM call or broadcast.
        $originalBounded=(Get-Command Invoke-EntryBoundedHelper).ScriptBlock
        try {
            Set-Item Function:Invoke-EntryBoundedHelper { param($Start,$Milliseconds) return $script:controlReply }
            $script:controlReply='{"hresult":-2147467259,"processId":0,"callerOverridesAbsent":true}'
            Reject { Invoke-EntryActivation $controlHost.Path 'OpenAI.Codex_test!App' } 'failed HRESULT never grants an activation root'
            Pass ($script:entryLastActivationResult.hresult -ceq '0x80004005' -and
                $script:entryLastActivationResult.processId -eq 0) 'failed numeric COM result is retained separately from ownership'
            $script:controlReply='{"hresult":0,"processId":0,"callerOverridesAbsent":true}'
            Reject { Invoke-EntryActivation $controlHost.Path 'OpenAI.Codex_test!App' } 'success HRESULT without a PID still fails'
            $script:controlReply='{"hresult":0,"processId":31,"callerOverridesAbsent":true}'
            Pass ((Invoke-EntryActivation $controlHost.Path 'OpenAI.Codex_test!App').processId -eq 31) 'well-formed synthetic success still requires downstream runtime identity checks'
            foreach ($reply in @('{"sent":false,"win32Error":1460}','{"sent":false,"win32Error":0}')) {
                $script:controlReply=$reply
                Pass ((Send-EntryEnvironmentChange $controlHost.Path) -ceq 'unconfirmed' -and
                    -not $script:entryLastBroadcastResult.sent) 'failure with a Win32 code or zero last-error remains unconfirmed'
            }
            $script:controlReply='{"sent":false,"win32Error":1460}'
            $null=Send-EntryEnvironmentChange $controlHost.Path
            Pass ($script:entryLastBroadcastResult.win32Error -ceq '0x000005B4') 'notification Win32 error remains numeric public evidence'
        } finally { Set-Item Function:Invoke-EntryBoundedHelper $originalBounded }

    } finally {
        if ($null -ne $controlHost) { $controlHost.Stream.Dispose() }
        Remove-Item -LiteralPath $controlScratch -Recurse -Force
    }
} else { Write-Output 'ENTRY_CONTROL_COMPILATION=not-run-Core-parent-requires-Framework' }
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
Pass (Test-EntryHelperSucceeded $false $true $false $false 0) 'completed zero exit within every bound is accepted'
Pass (-not (Test-EntryHelperSucceeded $true $true $false $false 0)) 'expired bounded wait rejects later zero exit even if timer callback is delayed'
Pass (-not (Test-EntryHelperSucceeded $false $false $false $false 0)) 'unfinished streams reject zero exit'
Pass (-not (Test-EntryHelperSucceeded $false $true $true $false 0)) 'timer deadline rejects zero exit'
Pass (-not (Test-EntryHelperSucceeded $false $true $false $true 0)) 'overflow or read error rejects zero exit'
Pass (-not (Test-EntryHelperSucceeded $false $true $false $false 7)) 'nonzero exit rejects otherwise complete helper'
# Model the Framework writer boundary with real streams, without changing console settings.
$preambleStream=[IO.MemoryStream]::new()
$preambleWriter=[IO.StreamWriter]::new($preambleStream,[Text.UTF8Encoding]::new($true),128,$true)
try {
    $preambleWriter.AutoFlush=$true
    Pass ($preambleStream.Length -eq 3) 'an empty auto-flushed UTF-8 writer emits its preamble before close'
} finally { $preambleWriter.Dispose(); $preambleStream.Dispose() }
foreach ($encoding in @([Text.Encoding]::ASCII,[Text.UTF8Encoding]::new($false))) {
    Pass ([object]::ReferenceEquals((Get-EntryNoInputEncoding $encoding),$encoding)) 'preamble-free encoding is unchanged'
}
$bomEncoding=[Text.UTF8Encoding]::new($true)
$noBomEncoding=Get-EntryNoInputEncoding $bomEncoding
Pass ($noBomEncoding.CodePage -eq $bomEncoding.CodePage -and $noBomEncoding.GetPreamble().Length -eq 0 -and
    $bomEncoding.GetPreamble().Length -eq 3) 'UTF-8 normalization preserves its code page and original encoding'
Reject { Get-EntryNoInputEncoding ([Text.UnicodeEncoding]::new($false,$true)) } 'other preamble-bearing code pages fail before process start'
Reject { Get-EntryNoInputEncoding $null } 'missing input encoding is not accepted'
$hostPath=(Get-Process -Id $PID).Path
foreach ($fixture in @('normal','stdin-eof','stdin-byte-eof','nonzero','overflow','timeout')) {
    Write-Output ('ENTRY_SAFETY_FIXTURE=bounded-'+$fixture)
    $start=New-Object Diagnostics.ProcessStartInfo
    $start.FileName=$hostPath; $start.UseShellExecute=$false; $start.CreateNoWindow=$true
    $start.RedirectStandardOutput=$true; $start.RedirectStandardError=$true
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($value) { $start.EnvironmentVariables[$name]=$value }
    }
    $command="[Console]::WriteLine('fixture-ok')"; $deadline=10000
    if ($fixture -ceq 'stdin-eof') { $command="if ([Console]::In.ReadToEnd().Length -ne 0) { exit 2 }; [Console]::WriteLine('fixture-eof')" }
    if ($fixture -ceq 'stdin-byte-eof') { $command="if ([Console]::OpenStandardInput().ReadByte() -ne -1) { exit 2 }; [Console]::WriteLine('fixture-byte-eof')" }
    if ($fixture -ceq 'nonzero') { $command="[Console]::Out.WriteLine('private-output-fixture'); [Console]::Error.WriteLine('private-error-fixture'); exit 7" }
    if ($fixture -ceq 'overflow') { $command="[Console]::Write(('x' * 4096))" }
    if ($fixture -ceq 'timeout') { $command='Start-Sleep -Seconds 30'; $deadline=1000 }
    $start.Arguments='-NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
    $script:entryHelperExitUnconfirmed=$false
    $encodingBeforeHelper=[Console]::InputEncoding
    if ($fixture -ceq 'normal') { Pass ((Invoke-EntryBoundedHelper $start $deadline) -ceq 'fixture-ok') 'bounded helper normal output' }
    elseif ($fixture -ceq 'stdin-eof') { Pass ((Invoke-EntryBoundedHelper $start $deadline) -ceq 'fixture-eof') 'no-input helper receives EOF rather than runner stdin' }
    elseif ($fixture -ceq 'stdin-byte-eof') { Pass ((Invoke-EntryBoundedHelper $start $deadline) -ceq 'fixture-byte-eof') 'no-input helper receives zero bytes, including no encoding preamble' }
    else { Reject { Invoke-EntryBoundedHelper $start $deadline } ('bounded helper rejects '+$fixture) }
    $encodingAfterHelper=[Console]::InputEncoding
    Pass ($encodingAfterHelper.CodePage -eq $encodingBeforeHelper.CodePage -and
        [Convert]::ToBase64String($encodingAfterHelper.GetPreamble()) -ceq
        [Convert]::ToBase64String($encodingBeforeHelper.GetPreamble())) 'helper success and failure restore the original input encoding'
    Pass (-not $script:entryHelperExitUnconfirmed) 'owned helper exit confirmed after success or failure'
    $helperEvidence=$script:entryLastHelperEvidence
    Pass ($helperEvidence.Count -eq 8 -and $helperEvidence.start -ceq 'confirmed' -and $helperEvidence.cleanup -ceq 'exit-confirmed' -and
        $helperEvidence.phase -ceq 'not-reported' -and $helperEvidence.streamsCompleted) 'helper records fixed lifecycle evidence'
    Pass (($helperEvidence | ConvertTo-Json -Compress) -notmatch 'private-output-fixture|private-error-fixture|FileName|Arguments') 'helper diagnostics exclude raw output and process inputs'
    if ($fixture -cin @('normal','stdin-eof','stdin-byte-eof','nonzero')) {
        Pass (-not $helperEvidence.waitExpired -and -not $helperEvidence.timerExpired -and -not $helperEvidence.overflowOrReadError) 'normal and nonzero exits are not relabeled deadline or overflow'
        $expectedExit='zero'; if ($fixture -ceq 'nonzero') { $expectedExit='nonzero' }
        Pass ($helperEvidence.exit -ceq $expectedExit) 'helper zero and nonzero exits stay distinct'
    } elseif ($fixture -ceq 'overflow') {
        Pass $helperEvidence.overflowOrReadError 'real byte overflow retains its dedicated flag'
    } else {
        Pass ($helperEvidence.waitExpired -or $helperEvidence.timerExpired) 'timeout retains wait or timer deadline evidence'
        Pass (-not $helperEvidence.overflowOrReadError) 'timeout is not reported as byte overflow'
    }
}
Write-Output 'ENTRY_SAFETY_FIXTURE=helper-construction-boundary'
$script:entryHelperExitUnconfirmed=$false
Reject { Invoke-EntryBoundedHelper ([pscustomobject]@{}) 10000 } 'pre-construction setup failure is sanitized'
Pass ($script:entryLastHelperEvidence.start -ceq 'not-attempted' -and
    $script:entryLastHelperEvidence.cleanup -ceq 'not-attempted' -and -not $script:entryHelperExitUnconfirmed) 'failure before construction does not claim a child'
$missingStart=New-Object Diagnostics.ProcessStartInfo
$missingStart.FileName=Join-Path ([IO.Path]::GetTempPath()) ('entry-missing-'+[guid]::NewGuid().ToString('N')+'.exe')
$missingStart.UseShellExecute=$false; $missingStart.RedirectStandardOutput=$true; $missingStart.RedirectStandardError=$true
$missingStart.EnvironmentVariables.Clear()
Pass (-not (Test-Path -LiteralPath $missingStart.FileName)) 'constructor-failure fixture executable is absent'
$encodingBeforeConstruction=[Console]::InputEncoding
Reject { Invoke-EntryBoundedHelper $missingStart 10000 } 'constructor failure is sanitized'
$encodingAfterConstruction=[Console]::InputEncoding
Pass ($encodingAfterConstruction.CodePage -eq $encodingBeforeConstruction.CodePage -and
    [Convert]::ToBase64String($encodingAfterConstruction.GetPreamble()) -ceq
    [Convert]::ToBase64String($encodingBeforeConstruction.GetPreamble())) 'failed construction restores the original input encoding'
Pass ($script:entryLastHelperEvidence.start -ceq 'uncertain' -and
    $script:entryLastHelperEvidence.cleanup -ceq 'unconfirmed' -and $script:entryHelperExitUnconfirmed) 'constructor failure cannot prove no process started or cleaned up'
# This fixture verified an absent executable, so no actual process was created.
# Only its conservative wrapper flag is reset; no unknown process is queried or killed.
$script:entryHelperExitUnconfirmed=$false
Pass ((Get-EntryHelperPhase "ENTRY_PHASE=started`nENTRY_PHASE=input_validated`nENTRY_PHASE=interop") -ceq 'input_validated') 'partial marker cannot imply completed phase'
Pass ((Get-EntryHelperPhase "ENTRY_PHASE=started`nprivate-output-fixture`nENTRY_PHASE=output_ready`n") -ceq 'started') 'phase scan stops before arbitrary output'
# Compile and exercise the actual Windows observer in the matrix's host. The fixture
# waits for stdin EOF instead of racing a short sleep; all handles/processes are owned.
if ($PSVersionTable.PSEdition -ceq 'Desktop') {
    Write-Output 'ENTRY_SAFETY_FIXTURE=image-observer'
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
# Exact image classification and observation schema never emit an unknown path/name.
$applications=@([pscustomobject]@{id='App';executable='app/ChatGPT.exe'},
    [pscustomobject]@{id='CodexChromeNativeHost';executable='app/resources/plugins/openai-bundled/plugins/chrome/extension-host/windows/x64/extension-host.exe'},
    [pscustomobject]@{id='CodexCoreCommandRunner';executable='app/resources/codex-command-runner.exe'})
$images=Get-EntryKnownImages 'C:\fixture-package' 'C:\Windows' $applications
Pass ((Get-EntryImageLabel 'C:\Windows\System32\RuntimeBroker.exe' $images) -ceq 'windows_runtime_broker') 'exact Windows broker image path'
Pass ((Get-EntryImageLabel 'C:\Windows\System32\nested\RuntimeBroker.exe' $images) -ceq 'other') 'broker basename is not identity'
Pass ((Get-EntryImageLabel 'C:\fixture-package\app\resources\codex-command-runner.exe' $images) -ceq 'package_command_runner') 'exact manifest image path'
Pass ((Get-EntryImageLabel 'C:\unreported-private-path\RuntimeBroker.exe' $images) -ceq 'other') 'unknown path stays an opaque label'
$applications[0].executable='app/unknown.exe'
Reject { Get-EntryKnownImages 'C:\fixture-package' 'C:\Windows' $applications } 'changed manifest path rejected'
$applications[0].executable='app/ChatGPT.exe'
$observation='{"schema":1,"scope":"freshness-observation-only","matches":1,"truncated":false,"originalGenerationVerified":false,"records":[{"status":"observed","image":"windows_runtime_broker","package":"expected","liveThroughout":true}]}' | ConvertFrom-Json
Assert-EntryObservation $observation 1
$canonical=$observation | ConvertTo-Json -Depth 6 -Compress
$inputPhases=@('input_file_validated','input_bytes_read','input_json_parsed','input_shape_validated')
$prefix="ENTRY_PHASE=started`n"
$previousPhase='started'
foreach ($phase in $inputPhases) {
    $partialPrefix=$prefix+'ENTRY_PHASE='+$phase
    Pass ((Get-EntryHelperPhase $partialPrefix) -ceq $previousPhase) 'partial input marker cannot advance completed phase'
    $prefix+="ENTRY_PHASE=$phase`n"
    Pass ((Get-EntryHelperPhase $prefix) -ceq $phase) 'completed input marker identifies the last finished operation'
    $previousPhase=$phase
}
$prefix+="ENTRY_PHASE=input_validated`nENTRY_PHASE=interop_ready`n"
$recordFrames="ENTRY_PHASE=record_started`nENTRY_PHASE=record_finished`n"
$framed=$prefix+$recordFrames+"ENTRY_PHASE=output_ready`n"+$canonical
Pass ((ConvertFrom-EntryObservationOutput $framed 1).records[0].image -ceq 'windows_runtime_broker') 'ordered phase frames preserve the canonical validated report'
foreach ($phase in $inputPhases) {
    $frame="ENTRY_PHASE=$phase`n"
    Reject { ConvertFrom-EntryObservationOutput ($framed.Replace($frame,'')) 1 } 'missing input phase rejects otherwise valid observation'
    Reject { ConvertFrom-EntryObservationOutput ($framed.Replace($frame,($frame+$frame))) 1 } 'repeated input phase rejects otherwise valid observation'
    Reject { ConvertFrom-EntryObservationOutput ($framed.Replace($frame,'').Replace("ENTRY_PHASE=started`n",($frame+"ENTRY_PHASE=started`n"))) 1 } 'reordered input phase rejects otherwise valid observation'
}
$zero=$canonical | ConvertFrom-Json
$zero.matches=0; $zero.records=@()
$zeroFrames=$prefix+"ENTRY_PHASE=output_ready`n"+($zero | ConvertTo-Json -Depth 6 -Compress)
Pass ((ConvertFrom-EntryObservationOutput $zeroFrames 0).records.Count -eq 0) 'zero-record observation requires complete input phases without record frames'
Reject { ConvertFrom-EntryObservationOutput ($zeroFrames.Replace("ENTRY_PHASE=output_ready`n",($recordFrames+"ENTRY_PHASE=output_ready`n"))) 0 } 'zero-record observation rejects an unexpected record frame'
$invalidJsonError=''
try { $null=ConvertFrom-EntryObservationOutput ($prefix+$recordFrames+"ENTRY_PHASE=output_ready`n"+'{"private-output-fixture":') 1 }
catch { $invalidJsonError=$_.Exception.Message }
Pass ($invalidJsonError -ceq 'Invalid observation JSON.') 'JSON parser failure cannot publish raw helper output'
foreach ($invalidFrames in @(
    $framed.Replace('ENTRY_PHASE=interop_ready','ENTRY_PHASE=private-unreported'),
    $framed.Replace("ENTRY_PHASE=interop_ready`n", "ENTRY_PHASE=interop_ready`nENTRY_PHASE=interop_ready`n"),
    $framed.Replace("ENTRY_PHASE=record_finished`n",''),
    ("private-output-fixture`n"+$framed), ($framed+"`nprivate-output-fixture"),
    $framed.Replace($canonical,(' '+$canonical)),
    ($prefix+"ENTRY_PHASE=output_ready`n"+$canonical))) {
    Reject { ConvertFrom-EntryObservationOutput $invalidFrames 1 } 'unknown reordered missing or raw output frames fail closed'
}
$four=$canonical | ConvertFrom-Json
$four.matches=4; $four.records=@($four.records[0],$four.records[0],$four.records[0],$four.records[0])
$fourFrames=$prefix+($recordFrames*4)+"ENTRY_PHASE=output_ready`n"+($four | ConvertTo-Json -Depth 6 -Compress)
Pass ([Text.Encoding]::UTF8.GetByteCount($fourFrames) -lt 4096 -and
    (ConvertFrom-EntryObservationOutput $fourFrames 4).records.Count -eq 4) 'four complete phase records fit the unchanged output bound'
foreach ($invalidInteger in @([double]1,'1',$true)) {
    $observation.schema=$invalidInteger; Reject { Assert-EntryObservation $observation 1 } 'JSON schema rejects floating string and boolean types'
    $observation.schema=[long]1; $observation.matches=$invalidInteger
    Reject { Assert-EntryObservation $observation 1 } 'JSON counts reject floating string and boolean types'
}
$observation.schema=[long]1; $observation.matches=[long]1
Assert-EntryObservation $observation 1
$observation.originalGenerationVerified=$true
Reject { Assert-EntryObservation $observation 1 } 'reopened PID cannot claim original guard generation'
$observation.originalGenerationVerified=$false
$observation.records[0].image=@('windows_runtime_broker')
Reject { Assert-EntryObservation $observation 1 } 'label array is not a fixed string'
$observation.records[0].image='C:\unreported-private-path'; Reject { Assert-EntryObservation $observation 1 } 'observation rejects raw image path'
$observation.records[0].image='windows_runtime_broker'; $observation.records[0].package='other'
Reject { Assert-EntryObservation $observation 1 } 'observed matched package must be revalidated through held handle'
$observation.records[0].package='expected'; $observation.records[0].liveThroughout=$false
Reject { Assert-EntryObservation $observation 1 } 'exit before metadata completion remains unavailable'
$observation.records[0].liveThroughout=$true
$observation.records[0] | Add-Member NoteProperty parentRelation 'unverified'
Reject { Assert-EntryObservation $observation 1 } 'parent metadata is outside the minimal observation contract'
$observation.records[0].PSObject.Properties.Remove('parentRelation')
$observation.records[0] | Add-Member NoteProperty commandLine 'unreported'
Reject { Assert-EntryObservation $observation 1 } 'observation rejects extra private fields'
$matchedIds=[Collections.Generic.List[int]]::new()
$many=@(1..6 | ForEach-Object { [pscustomobject]@{ProcessId=$_;Name='ChatGPT.exe'} })
$manyFacts=Get-EntryFreshnessEvidence $many 'fixture.package' 'fixture-bridge.exe' 0 0 { $null } $matchedIds
Pass ($manyFacts.matches -eq 6 -and $matchedIds.Count -eq 4) 'inspection cap never weakens all-match guard'
# Real filesystem boundaries for the helper's metadata gate. No ACL or privilege changes.
$metadataScratch=Join-Path ([IO.Path]::GetTempPath()) ('entry-metadata-test-'+[guid]::NewGuid().ToString('N'))
$null=[IO.Directory]::CreateDirectory($metadataScratch)
$metadataFile=Join-Path $metadataScratch 'input.json'
$metadataDirectory=Join-Path $metadataScratch 'directory'
$metadataJunction=Join-Path $metadataScratch 'junction'
try {
    Write-Output 'ENTRY_SAFETY_FIXTURE=observation-input-metadata'
    foreach ($size in @(0,1,16384,16385)) {
        [IO.File]::WriteAllBytes($metadataFile,[byte[]]::new($size))
        if ($size -eq 1 -or $size -eq 16384) {
            Pass (@(Assert-EntryInputFileMetadata $metadataFile).Count -eq 0) ('metadata accepts bounded file length '+$size)
        } else { Reject { Assert-EntryInputFileMetadata $metadataFile } ('metadata rejects file length '+$size) }
    }
    Reject { Assert-EntryInputFileMetadata (Join-Path $metadataScratch 'missing.json') } 'missing metadata fails closed'
    $null=[IO.Directory]::CreateDirectory($metadataDirectory)
    Reject { Assert-EntryInputFileMetadata $metadataDirectory } 'directory input is rejected before length'
    [IO.File]::WriteAllBytes($metadataFile,[byte[]]::new(1))
    $originalAttributes=[IO.File]::GetAttributes($metadataFile)
    try {
        [IO.File]::SetAttributes($metadataFile,($originalAttributes -bor [IO.FileAttributes]::ReadOnly))
        Pass (@(Assert-EntryInputFileMetadata $metadataFile).Count -eq 0) 'read-only regular input remains readable metadata'
        [IO.File]::SetAttributes($metadataFile,($originalAttributes -bor [IO.FileAttributes]::Hidden))
        Pass (([IO.File]::GetAttributes($metadataFile) -band [IO.FileAttributes]::Hidden) -ne 0) 'hidden fixture attribute is established'
        Reject { Assert-EntryInputFileMetadata $metadataFile } 'hidden input retains prior no-Force rejection'
        [IO.File]::SetAttributes($metadataFile,($originalAttributes -bor [IO.FileAttributes]::System))
        Pass (([IO.File]::GetAttributes($metadataFile) -band [IO.FileAttributes]::System) -ne 0) 'system fixture attribute is established'
        Reject { Assert-EntryInputFileMetadata $metadataFile } 'system input is conservatively rejected by the private-input contract'
    } finally { [IO.File]::SetAttributes($metadataFile,$originalAttributes) }
    Pass ([IO.File]::GetAttributes($metadataFile) -eq $originalAttributes) 'owned file attributes are restored after boundary cases'
    $null=New-Item -ItemType Junction -Path $metadataJunction -Target $metadataDirectory
    Pass (([IO.File]::GetAttributes($metadataJunction) -band [IO.FileAttributes]::ReparsePoint) -ne 0) 'owned junction is a real reparse point'
    Reject { Assert-EntryInputFileMetadata $metadataJunction } 'junction input is rejected'
    # A junction also has Directory; this does not independently isolate the ReparsePoint branch.
} finally {
    if ([IO.File]::Exists($metadataFile)) {
        [IO.File]::SetAttributes($metadataFile,[IO.FileAttributes]::Normal)
        [IO.File]::Delete($metadataFile)
    }
    # Delete only known leaves, never recursively traverse a junction or uncertain cleanup.
    if ([IO.Directory]::Exists($metadataJunction)) { [IO.Directory]::Delete($metadataJunction) }
    if ([IO.Directory]::Exists($metadataDirectory)) { [IO.Directory]::Delete($metadataDirectory) }
    [IO.Directory]::Delete($metadataScratch)
}
Initialize-EntryHeldObservation
Pass ([DesktopEntryReadOnly.Held]::RequireRunningServicePid(0x210,4,31) -eq 31) 'packaged own-process SCM running PID is eligible for binding only'
Pass ([DesktopEntryReadOnly.Held]::RequireRunningServicePid(0x10,4,31) -eq 31) 'native own-process status still requires the independent held package binding'
foreach ($serviceState in @(1,2,3,5,6,7)) {
    Reject { [DesktopEntryReadOnly.Held]::RequireRunningServicePid(0x210,$serviceState,31) } 'SCM stopped, pending or paused state is not accepted'
}
Reject { [DesktopEntryReadOnly.Held]::RequireRunningServicePid(0x20,4,31) } 'shared-process service is not the reviewed own-process service'
foreach ($serviceType in @(0x1,0x50,0x110,0x410)) {
    Reject { [DesktopEntryReadOnly.Held]::RequireRunningServicePid($serviceType,4,31) } 'driver, user, interactive or unknown SCM type flags cannot be accepted'
}
Reject { [DesktopEntryReadOnly.Held]::RequireRunningServicePid(0x210,4,0) } 'SCM zero PID is not an identity'
Reject { [DesktopEntryReadOnly.Held]::RequireRunningServicePid(0x210,4,[uint32]::MaxValue) } 'SCM PID outside the held-process interface is not accepted'
$heldSelf=$null
try {
    $heldSelf=[DesktopEntryReadOnly.Held]::new($PID)
    Pass ($heldSelf.Alive -and $heldSelf.ImagePath -ieq $hostPath) 'query-only held handle captures exact current image metadata'
    Pass ($heldSelf.PackageRelation('fixture.package') -cin @('none','other')) 'package query does not invent expected identity'
    Pass ($heldSelf.CreatedUtcTicks -eq (Get-Process -Id $PID).StartTime.ToUniversalTime().Ticks) 'creation time is taken from the held process, never rounded CIM metadata'
} finally { if ($null -ne $heldSelf) { $heldSelf.Dispose() } }
# Exercise zero records first, preserving the complete input and interop path
# without opening any process handle. This does not skip the owned-process case.
# Then exercise the complete helper/JSON path against only this owned test process.
# Its package is deliberately absent/different: this is unavailable observation,
# never fake package identity, provisioning, Desktop activation or a guard waiver.
# Only freshness-observation calls this helper at runtime, never treatment.
# Keep these cases in the default suite; treatment records them as not-run.
if (-not $TreatmentPrerequisites) {
$observationScratch=Join-Path ([IO.Path]::GetTempPath()) ('entry-observation-test-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $observationScratch | Out-Null
try {
    Write-Output 'ENTRY_SAFETY_FIXTURE=observation-zero-records'
    $script:entrySafetyDiagnostic.zeroRecords='started'; Save-EntrySafetyDiagnostic 'running'
    $zeroIds=[Collections.Generic.List[int]]::new()
    $zeroObservation=Invoke-EntryFreshnessObservation $zeroIds 0 'OpenAI.Codex_26.1103.3517.0_x64__2p2nqsd0c76g0' `
        'C:\fixture-package' $applications $observationScratch
    Pass ($zeroObservation.matches -eq 0 -and $zeroObservation.records.Count -eq 0 -and
        -not $zeroObservation.originalGenerationVerified) 'complete zero-record helper preserves absence of process observations'
    Pass ($script:entryLastHelperEvidence.phase -ceq 'output_ready' -and $script:entryLastHelperEvidence.exit -ceq 'zero' -and
        $script:entryLastHelperEvidence.cleanup -ceq 'exit-confirmed' -and -not $script:entryHelperExitUnconfirmed) 'zero-record helper completes and exits under unchanged supervision'
    $script:entrySafetyDiagnostic.zeroRecords='passed'; Save-EntrySafetyDiagnostic 'running'
    Write-Output 'ENTRY_SAFETY_FIXTURE=observation-owned-process'
    $script:entrySafetyDiagnostic.ownedProcess='started'; Save-EntrySafetyDiagnostic 'running'
    $selfIds=[Collections.Generic.List[int]]::new(); $selfIds.Add($PID)
    $selfObservation=Invoke-EntryFreshnessObservation $selfIds 1 'OpenAI.Codex_26.1103.3517.0_x64__2p2nqsd0c76g0' `
        'C:\fixture-package' $applications $observationScratch
    Pass ($selfObservation.records.Count -eq 1 -and $selfObservation.records[0].status -ceq 'unavailable' -and
        $selfObservation.records[0].package -cin @('none','other') -and -not $selfObservation.originalGenerationVerified) 'complete bounded observation helper preserves unavailable package identity'
    Pass ($script:entryLastHelperEvidence.phase -ceq 'output_ready' -and $script:entryLastHelperEvidence.exit -ceq 'zero') 'real helper completes the fixed observation protocol'
    $script:entrySafetyDiagnostic.ownedProcess='passed'; Save-EntrySafetyDiagnostic 'running'
} finally { Remove-Item -LiteralPath $observationScratch -Recurse -Force }
}
# Observation-only mode has two unconditional stop paths before environment writes or activation.
$source=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'))
$observationOffset=$source.IndexOf('$report.nativeEntry.matchedProcessObservation=Invoke-EntryFreshnessObservation')
$strictBlock=$source.IndexOf("if (`$freshness.blockingMatches -ne 0) { Block 'A prior Desktop or bridge process prevents a fresh native-entry observation.' }")
$zeroBlock=$source.IndexOf("if (`$NativeEntryCase -ceq 'freshness-observation') { Block 'Observation-only baseline finished; activation and USER writes are deliberately not attempted.' }")
$writeOffset=$source.IndexOf('            Set-EntryOverrides $entryState')
Pass ($observationOffset -gt 0 -and $strictBlock -gt $observationOffset -and $zeroBlock -gt $strictBlock -and
    $writeOffset -gt $zeroBlock) 'inspection cannot fall through into writes or activation even with zero matches'
# The sole runtime exemption must remain entry-only and downstream of full verification.
$serviceExemptions=@($ast.FindAll({param($node)
    $node -is [Management.Automation.Language.AssignmentStatementAst] -and
    $node.Left.Extent.Text -ceq '$freshness.blockingMatches' -and $node.Right.Extent.Text -ceq '0'
},$true))
Pass ($serviceExemptions.Count -eq 1) 'there is only one service exemption assignment'
$serviceBody=$serviceExemptions[0].Parent
$serviceGate=$serviceBody.Parent.Parent.Parent
Pass ($serviceGate -is [Management.Automation.Language.IfStatementAst] -and
    $serviceGate.Clauses[0].Item1.Extent.Text.Contains('$EnableReviewedEntry4239 -and (Test-EntrySingleServiceCandidate $freshness)') -and
    $serviceGate.Clauses[0].Item1.Extent.Text.Contains('$matchedProcessIds[0] -eq $entryServiceHandleState.ProcessId')) 'only one held package-only candidate in the exclusive entry profile can reach the exemption'
$serviceVerification=@($serviceBody.FindAll({param($node)
    $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq 'Open-EntryVerifiedService'
},$true))
Pass ($serviceVerification.Count -eq 1 -and $serviceVerification[0].Extent.StartOffset -lt $serviceExemptions[0].Extent.StartOffset -and
    $serviceVerification[0].CommandElements[1].Extent.Text -ceq '$entryServiceHandleState') 'successful verification of the retained package-query handle precedes exemption'
$serviceOpenText=(Get-Command Open-EntryVerifiedService).ScriptBlock.ToString()
Pass (-not $serviceOpenText.Contains('::new($ProcessId)') -and $serviceOpenText.Contains('$held=$HeldState.Held') -and
    $serviceOpenText.Contains('$HeldState.ImageStream=[IO.File]::Open(')) 'verification never reopens the PID and preserves its file handle on failure'
Pass ($source.Contains('$entryServiceHandleState.Held=[DesktopEntryReadOnly.Held]::new($ProcessId)') -and
    $source.Contains('$relation=$entryServiceHandleState.Held.PackageRelation($app.PackageFullName)')) 'original candidate package query uses the retained handle'
$serviceIdentityCalls=@($ast.FindAll({param($node)
    $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ceq 'Get-DesktopPackageIdentityEvidence' -and
    $node.Extent.Text.Contains('-RequireEntryService:')
},$true))
Pass ($serviceIdentityCalls.Count -eq 1 -and
    $serviceIdentityCalls[0].Extent.Text.Contains('-RequireEntryService:$EnableReviewedEntry4239')) 'only entry requests the additional signed service contract'
Pass ($source.Replace("`r`n","`n").Contains("if (`$null -ne `$entryService) { Assert-EntryVerifiedServiceCurrent `$entryService }`n            Set-EntryOverrides") -and
    $source.Replace("`r`n","`n").Contains("if (`$null -ne `$entryService) { Assert-EntryVerifiedServiceCurrent `$entryService }`n        `$entryActivationAttempted=`$true`n        `$result = Invoke-EntryActivation")) 'same held service and SCM PID are rechecked before USER writes and activation'
$serviceRestoreOffset=$source.LastIndexOf('Restore-EntryOverrides $entryState')
$serviceFileCloseOffset=$source.IndexOf('$entryServiceHandleState.ImageStream.Dispose()')
$serviceHandleCloseOffset=$source.IndexOf('$entryServiceHandleState.Held.Dispose()')
Pass ($serviceFileCloseOffset -gt $serviceRestoreOffset -and $serviceHandleCloseOffset -gt $serviceFileCloseOffset -and
    $source.Contains('try { $entryServiceHandleState.ImageStream.Dispose() } catch { $entryCleanupError=$true }') -and
    $source.Contains('try { $entryServiceHandleState.Held.Dispose() } catch { $entryCleanupError=$true }')) 'restoration precedes independent checked service-handle disposal on success and failure'
# Actual Windows lifetime oracle: keep an exited process handle, then close it.
# A separate live fixture remains a blocker, and is never passed to that cleanup.
$exitedFixture=$null; $liveFixture=$null
try {
    Write-Output 'ENTRY_SAFETY_FIXTURE=owned-probe-lifetime'
    $lifetimeStart=New-Object Diagnostics.ProcessStartInfo
    $lifetimeStart.FileName=$hostPath; $lifetimeStart.UseShellExecute=$false; $lifetimeStart.CreateNoWindow=$true
    $lifetimeStart.RedirectStandardInput=$true
    $lifetimeStart.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR','TEMP','TMP')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($value) { $lifetimeStart.EnvironmentVariables[$name]=$value }
    }
    $hold='[Console]::In.ReadToEnd() | Out-Null'
    $lifetimeStart.Arguments='-NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($hold))
    $liveFixture=[Diagnostics.Process]::Start($lifetimeStart); $null=$liveFixture.Handle
    $lifetimeStart.Arguments='-NoProfile -NonInteractive -Command "exit 0"'
    $exitedFixture=[Diagnostics.Process]::Start($lifetimeStart); $null=$exitedFixture.Handle
    $exitedFixtureId=$exitedFixture.Id
    Pass ($exitedFixture.WaitForExit(10000) -and $exitedFixture.HasExited) 'short fixture is exited before retaining-handle observation'
    $beforeClose=Read-EntryProbeVisibility $exitedFixtureId
    Pass ($beforeClose.status -ceq 'read') 'held-exit process visibility is readable'
    Pass (Complete-DesktopProbeProcess $exitedFixture) 'held exited fixture is closed'
    $exitedFixture=$null
    $watch=[Diagnostics.Stopwatch]::StartNew(); $afterClose=$null
    do {
        $afterClose=Read-EntryProbeVisibility $exitedFixtureId
        if ($afterClose.status -cne 'read' -or $afterClose.matches -eq 0) { break }
        Start-Sleep -Milliseconds 50
    } while ($watch.Elapsed.TotalSeconds -lt 10)
    Pass ($afterClose.status -ceq 'read' -and $afterClose.matches -eq 0) 'closed exited fixture leaves process enumeration'
    Pass (-not $liveFixture.HasExited) 'closing exited fixture never targets the live neighbor'
    $liveRows=@(Get-CimInstance Win32_Process -Filter ("ProcessId="+$liveFixture.Id) -ErrorAction Stop)
    $liveFacts=Get-EntryFreshnessEvidence $liveRows 'fixture.package' ([IO.Path]::GetFileName($hostPath)) $exitedFixtureId 0 { $null }
    Pass ($liveFacts.matches -eq 1 -and $liveFacts.other -eq 1) 'live unrelated fixture remains a strict freshness blocker'
    Write-Output ('ENTRY_OWNED_PROBE_LIFETIME='+([ordered]@{heldExitedVisibleBeforeClose=($beforeClose.matches -eq 1);
        exitedVisibleAfterClose=($afterClose.matches -eq 1);liveNeighborStillBlocks=($liveFacts.matches -eq 1)} | ConvertTo-Json -Compress))
} finally {
    try { if ($null -ne $exitedFixture) { $null=Complete-DesktopProbeProcess $exitedFixture } }
    finally {
        if ($null -ne $liveFixture) {
            try { $liveFixture.StandardInput.Close(); $null=$liveFixture.WaitForExit(5000) }
            finally { $null=Complete-DesktopProbeProcess $liveFixture }
        }
    }
}
Save-EntrySafetyDiagnostic 'passed'
Write-Output ('Native entry offline safety assertions passed: '+$count)
