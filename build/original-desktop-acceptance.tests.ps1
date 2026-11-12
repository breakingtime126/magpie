# Offline caller contracts. Does not run the coordinator, Windows APIs or UIA.
[CmdletBinding()]
param([string]$CallerPath='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-connect-action-core.ps1')
function Assert([bool]$Condition,[string]$Message) { if (-not $Condition) { throw ('FAIL: '+$Message) } }
function Parse([string]$Name) {
    $path=$Name;if (-not [IO.Path]::IsPathRooted($path)) { $path=Join-Path $PSScriptRoot $Name }
    $tokens=$null;$errors=$null;$ast=[Management.Automation.Language.Parser]::ParseFile($path,[ref]$tokens,[ref]$errors)
    Assert ($errors.Count -eq 0) ('parse '+$Name);return $ast
}
if (-not $CallerPath) { $CallerPath=Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1' }
$caller=Parse $CallerPath
# Automatic PowerShell variables are case-insensitive and cannot be reused for fixture paths.
$assignments=@($caller.FindAll({param($node) $node -is [Management.Automation.Language.AssignmentStatementAst]},$true))
foreach ($assignment in $assignments) {
    if ($assignment.Left -is [Management.Automation.Language.VariableExpressionAst]) {
        Assert ($assignment.Left.VariablePath.UserPath -inotmatch '^(HOME|PID|PSHOME|Host|Error|ExecutionContext|PSVersionTable|PSScriptRoot|PSCommandPath)$') 'fixture variables must not overwrite PowerShell automatic variables'
    }
}
$package=Parse 'package-native-magpie-windows.ps1'
$sequence=Parse 'original-desktop-sequence.ps1'
foreach ($name in @('Get-AcceptanceSupplierFixture','Test-AcceptanceJournal','Get-AcceptanceDiagnostics','Get-AcceptanceBuildTags','Assert-AcceptanceBuildTags')) {
    $function=@($caller.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name},$false))
    Assert ($function.Count -eq 1) ('unique function '+$name)
    Invoke-Expression $function[0].Extent.Text
}
foreach ($diagnostic in @($false,$true)) {
    $message='[original-connect-desktop-v1]'
    if ($diagnostic) { $message+=' [original-connect-stages-only-v1] [original-backend-handshake-v1]' }
    $expected=Get-AcceptanceBuildTags $diagnostic $message
    foreach ($badField in @('none','buildTags','adapterBuildTags','handshakeDiagnostic','handshakeType')) {
        $build=@{buildTags=$expected.gui;adapterBuildTags=$expected.adapter;handshakeDiagnostic=$diagnostic}
        switch -CaseSensitive ($badField) {
            'buildTags' { $build.buildTags+=',unreviewed' }
            'adapterBuildTags' { $build.adapterBuildTags+=',unreviewed' }
            'handshakeDiagnostic' { $build.handshakeDiagnostic=-not $diagnostic }
            'handshakeType' { $build.handshakeDiagnostic=[string]$diagnostic }
        }
        $allowed=$true
        try { Assert-AcceptanceBuildTags $build $expected } catch { $allowed=$false }
        Assert ($allowed -eq ($badField -ceq 'none')) 'coordinator requires exact GUI and adapter tags plus boolean diagnostic provenance'
    }
}
foreach ($captureFailure in @($false,$true)) {
    foreach ($captureThrows in @($false,$true)) {
        foreach ($trayThrows in @($false,$true)) {
            foreach ($preconditionThrows in @($false,$true)) {
                $script:calls=@()
                $result=Get-AcceptanceDiagnostics $captureFailure {
                    $script:calls+='capture';if ($captureThrows) {throw 'private-capture-error'};return @{status='captured';phase='capture-complete'}
                } {
                    $script:calls+='tray';if ($trayThrows) {throw 'private-tray-error'};return @{status='observed'}
                } {
                    $script:calls+='preconditions';if ($preconditionThrows) {throw 'private-precondition-error'};return @{status='snapshot'}
                }
                $expected='preconditions,tray';if ($captureFailure) {$expected='preconditions,capture,tray'}
                Assert (($script:calls -join ',') -ceq $expected) 'diagnostic failure cannot skip other read-only evidence'
                Assert ($result.Count -eq 3 -and ($result|ConvertTo-Json -Compress) -cnotmatch 'private|cleanup|Recovery') 'diagnostics do not expose errors or carry recovery decisions'
                if (-not $captureFailure) {Assert ($null -eq $result.capture) 'no failure capture on verified sequence'}
            }
        }
    }
}
$fixture=Get-AcceptanceSupplierFixture
Assert ($fixture.noAutoGroups -eq $true -and $fixture.providers.Count -eq 1) 'ordinary single provider fixture'
$p=$fixture.providers[0]
Assert (($p.Keys | Sort-Object) -join ',' -ceq 'id,key,models,name,responses') 'no account, refresh, plugin, decision or capability metadata'
Assert ($p.responses -ceq 'https://aihub.friendtimes.net/v1' -and $p.key -ceq 'PUBLIC-INERT-NOT-A-CREDENTIAL') 'ordinary endpoint and public inert key only'
Assert ($p.models.Count -eq 1 -and $p.models[0] -ceq 'original-ui-fixture-model') 'explicit inert model pick without Fetch'
$root=Join-Path ([IO.Path]::GetTempPath()) ('original-caller-test-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
try {
    $path=Join-Path $root 'journal.json'
    Assert (-not (Test-AcceptanceJournal $path)) 'truly absent journal'
    [IO.File]::WriteAllText($path,'{}')
    Assert (Test-AcceptanceJournal $path) 'existing journal'
    $rejected=$false;try { Test-AcceptanceJournal $root } catch { $rejected=$true }
    Assert $rejected 'directory is not an absent journal'
} finally { [IO.Directory]::Delete($root,$true) }
$commands=@($caller.FindAll({param($node) $node -is [Management.Automation.Language.CommandAst]},$true) | ForEach-Object {$_.GetCommandName()})
foreach ($name in @('Set-EntryOverrides','Restore-EntryOverrides','Write-EntryUserEnvironment','Invoke-WebRequest','Invoke-RestMethod','Set-ExecutionPolicy','Remove-Item')) {
    Assert ($commands -cnotcontains $name) ('no fallback action '+$name)
}
$text=$caller.Extent.Text
Assert ($text.Contains("[original-connect-desktop-v1]") -and $text.Contains("'original-ui-v1'")) 'exact caller opt-in'
Assert ($text.Contains('if (-not $sequenceEntered -and $null -eq $report.sequence') -and
    $text.IndexOf('$sequenceEntered=$true') -lt $text.IndexOf('$report.sequence=Invoke-OriginalDesktopSequence')) 'unexpected post-dispatch orchestration failure cannot use pre-sequence cleanup'
Assert ($text.Contains('Invoke-OriginalDesktopSequence -Connect') -and $text.Contains('Invoke-OriginalConnectAction -Mode Restore')) 'caller routes original Connect and Restore through bounded actions'
Assert ($text.IndexOf('$report.stage=''owned-window-evidence''') -gt $text.IndexOf('$safeCleanup=$report.sequence.cleanupAllowed')) 'captures only after recovery'
Assert ($text.Contains('if ($safeCleanup)') -and $text.Contains('if ($null -ne $context -and $context.Evidence.activated)')) 'cleanup requires recovery and held activation authority'
Assert ($text.Contains("Save-AcceptanceReport`n    # Diagnostics follow") -and $text.IndexOf('$diagnostics=Get-AcceptanceDiagnostics') -gt $text.IndexOf('$safeCleanup=$report.sequence.cleanupAllowed')) 'diagnostics follow saved final sequence without changing recovery'
Assert ($text.Contains('-OriginalStartInfo $start') -and $text.Contains('-RecoveryChecksCompleted -SequenceReportSaved') -and $text.Contains('$report.connectPreconditions=$diagnostics.preconditions')) 'precondition observer receives retained original environment after saved sequence evidence'
Assert ($text.Contains('restoredDesktopStartVerified=$false') -and $text.Contains('backendHandshakeVerified=$false') -and $text.Contains('zeroEgressProven=$false')) 'bounded claims retained'
Assert (-not $text.Contains("RemoveAll") -and -not $text.Contains(".Delete(")) 'coordinator preserves recovery files'
Assert ($package.Extent.Text.Contains('$DiscoverOriginalConnect -or $env:MAGPIE_ORIGINAL_CONNECT_DISCOVERY')) 'discovery and action gates exclusive'
Assert ($package.Extent.Text.Contains('-AcceptOriginalConnect') -or $package.Extent.Text.Contains('[switch]$AcceptOriginalConnect')) 'package action option exists'
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '..\.github\workflows\experimental-codex-connectivity-preview.yml'))
Assert ($workflow.Contains('[original-connect-desktop-v1]') -and $workflow.Contains('MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE')) 'workflow action opt-in'
Assert (-not $workflow.Contains('secrets.')) 'acceptance workflow has no provider secrets'
Write-Output 'ORIGINAL_DESKTOP_ACCEPTANCE_CALLER_TESTS_PASSED'
# Diagnostic-only mode has no capture calls and cannot become full acceptance.
$callerText=$caller.Extent.Text
Assert ($callerText.Contains("'[original-connect-stages-only-v1]'")) 'diagnostic mode requires exact source marker'
Assert ($callerText.Contains("'stage-diagnostics-v1'")) 'diagnostic mode requires exact environment value'
$script:diagnosticCaptureCalls=0
function Save-AcceptanceWindow { $script:diagnosticCaptureCalls++;throw 'capture must not run' }
$stageDiagnosticsOnly=$true
$report=@{sequence=@{status='verified'};status='blocked'}
$successGuard=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and $n.Extent.Text.StartsWith('if (-not $stageDiagnosticsOnly)') -and $n.Extent.Text.Contains('Save-AcceptanceWindow')},$true))
Assert ($successGuard.Count -eq 1) 'all successful captures have diagnostic-mode guard'
Invoke-Expression $successGuard[0].Extent.Text
Assert ($script:diagnosticCaptureCalls -eq 0 -and $report.status -cne 'verified') 'diagnostic mode never captures successful windows or verifies acceptance'
foreach ($sequenceStatus in @('verified','uncertain','blocked')) {
    $report.sequence.status=$sequenceStatus
    $result=Get-AcceptanceDiagnostics (-not $stageDiagnosticsOnly -and $report.sequence.status -cne 'verified') {throw 'failure capture must not run'} {return @{status='observed'}} {return @{status='snapshot'}}
    Assert ($null -eq $result.capture) 'diagnostic mode never invokes failure capture'
}
$finalGuard=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and $n.Extent.Text -ceq "if (`$stageDiagnosticsOnly) { `$report.status='diagnostic-only' }"},$true))
Assert ($finalGuard.Count -eq 1) 'final report unconditionally marks diagnostic-only'
$report.status='verified';Invoke-Expression $finalGuard[0].Extent.Text
Assert ($report.status -ceq 'diagnostic-only') 'even verified transaction cannot enable full acceptance'
Assert ($callerText.Contains("if (-not `$stageDiagnosticsOnly) { . (Join-Path `$PSScriptRoot 'original-desktop-window-capture.ps1') }")) 'diagnostic mode does not import capture code'
Assert ($package.Extent.Text.Contains("if (`$env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS) { throw 'Stage diagnostics cannot publish an acceptance package.' }")) 'packager cannot succeed in diagnostic mode'
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-codex-connectivity-preview.yml'))
Assert ($workflow.Contains("if: success() && !contains(github.event.head_commit.message, '[original-connect-stages-only-v1]')")) 'diagnostic package upload explicitly prohibited'
$independent=$workflow.Substring($workflow.IndexOf('  diagnostic-capture-fixtures:'))
Assert ($independent.Contains('original-desktop-window-capture.tests.ps1') -and $independent.Contains('original-desktop-window-capture.mutations.ps1')) 'independent capture job reports genuine fixtures'
Assert ($workflow -notmatch '(?m)^\s+needs:') 'capture failure cannot gate or enable transaction job'
$modeGuards=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and ($n.Extent.Text.Contains("throw 'Stage diagnostics marker required.'") -or $n.Extent.Text.Contains("throw 'Stage diagnostics mode mismatch.'"))},$true))
Assert ($modeGuards.Count -eq 2) 'exact two coordinator mode guards'
$savedMode=$env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS
try {
    foreach ($case in @(
        @{env='';marker=$false;allow=$true},
        @{env='stage-diagnostics-v1';marker=$true;allow=$true},
        @{env='stage-diagnostics-v1';marker=$false;allow=$false},
        @{env='';marker=$true;allow=$false},
        @{env='wrong';marker=$true;allow=$false}
    )) {
        $env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS=$case.env
        $stageDiagnosticsOnly=$case.env -ceq 'stage-diagnostics-v1'
        $message='[original-connect-desktop-v1]';if ($case.marker) {$message+=' [original-connect-stages-only-v1]'}
        $allowed=$true
        try { foreach ($guard in $modeGuards) {Invoke-Expression $guard.Extent.Text} } catch {$allowed=$false}
        Assert ($allowed -eq $case.allow) 'diagnostic marker and environment must match'
    }
} finally {$env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS=$savedMode}
Write-Output 'STAGE_DIAGNOSTICS_ONLY_CALLER_TESTS_PASSED'

# Trace read failures remain independent of recovery and acceptance decisions.
$handshakeGuards=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.IfStatementAst] -and
    $n.Extent.Text.StartsWith('if ($buildMode.handshakeDiagnostic)')},$true))
Assert ($handshakeGuards.Count -eq 1) 'one compile-opted handshake observation block'
function New-OriginalBackendHandshakeReport { return @{reason='unavailable';backendHandshakeVerified=$false} }
function Get-OriginalBackendHandshake {
    param($OwnedRun,$OriginalStartInfo,$ProcessStartTicks,$Sequence,[switch]$RecoveryChecksCompleted,[switch]$SequenceReportSaved)
    Assert ($RecoveryChecksCompleted -and $SequenceReportSaved) 'handshake evidence requires saved recovery'
    $script:handshakeReads++
    if ($script:handshakeReadFails) { throw 'private-handshake-log-error' }
    return @{reason='observed';backendHandshakeVerified=$false}
}
foreach ($enabled in @($false,$true)) {
    foreach ($readFails in @($false,$true)) {
        $buildMode=@{handshakeDiagnostic=$enabled};$script:handshakeReads=0;$script:handshakeReadFails=$readFails
        $report=@{backendHandshakeObservation=(New-OriginalBackendHandshakeReport);sequence=@{status='verified'};
            status='blocked';backendHandshakeVerified=$false;selectionRestored=$true;journalRemoved=$true}
        $safeCleanup=$true;$run='owned-run';$start=[Diagnostics.ProcessStartInfo]::new();$ticks=1
        Invoke-Expression $handshakeGuards[0].Extent.Text
        Assert ($script:handshakeReads -eq [int]$enabled) 'no diagnostic file read without compile-mode opt-in'
        Assert ($safeCleanup -and $report.selectionRestored -and $report.journalRemoved -and $report.status -ceq 'blocked' -and
            -not $report.backendHandshakeVerified) 'handshake observation cannot alter recovery or success'
        if ($enabled -and $readFails) { Assert ($report.backendHandshakeObservation.reason -ceq 'read-error') 'read failure is unknown fixed evidence' }
        Assert (($report | ConvertTo-Json -Depth 5 -Compress) -cnotmatch 'private') 'no arbitrary handshake exception export'
    }
}
Assert ($text.IndexOf('$report.backendHandshakeObservation=Get-OriginalBackendHandshake') -gt $text.IndexOf('$safeCleanup=$report.sequence.cleanupAllowed')) 'handshake collector follows recovery'
Assert ($text.Contains("Save-AcceptanceReport`n    # Compile-only handshake traces")) 'handshake collector follows saved report'
Assert ($text.Contains("if (-not `$buildMode.handshakeDiagnostic -and `$report.images.status -ceq 'snapshot'") -and
    $text.Contains('if ($observation.ElapsedMilliseconds -ge 15000) { break }')) 'handshake mode uses full existing image budget without extending deadline'
Assert ($text.Contains("Save-AcceptanceReport`n    `$recoveryReportSaved=`$true")) 'final observer gate records successful recovery report save'
Write-Output 'ORIGINAL_BACKEND_HANDSHAKE_CALLER_TESTS_PASSED'

# The cleanup suite exercises the coordinator's actual finally block with inert
# retained-owner doubles. It never starts the acceptance coordinator.
& (Join-Path $PSScriptRoot 'original-desktop-cleanup.tests.ps1') -CallerPath $CallerPath
