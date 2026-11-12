[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
# The Desktop host promotes redirected native stderr to ErrorRecord objects.
# Expected child failures must not terminate the driver before classification.
function Invoke-MutationChild([string]$HostExe,[string]$Script,[string]$Implementation) {
    $savedPreference=$ErrorActionPreference
    try {
        $ErrorActionPreference='Continue'
        $scriptLiteral="'"+$Script.Replace("'","''")+"'"
        $implementationLiteral="'"+$Implementation.Replace("'","''")+"'"
        $command="try { & $scriptLiteral -Implementation $implementationLiteral; exit 0 } catch { if (`$_.Exception.Message.StartsWith('FAIL: ')) { [Console]::Out.WriteLine('MUTATION_ASSERTION:' + `$_.Exception.Message); exit 17 }; exit 18 }"
        $encoded=[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
        $output=@(& $HostExe -NoProfile -NonInteractive -EncodedCommand $encoded 2>&1)
        $code=$LASTEXITCODE
        return @{exitCode=$code;output=$output}
    } finally { $ErrorActionPreference=$savedPreference }
}
function Test-MutationRejection($Result,[string]$Expected) {
    if ($Result.exitCode -ne 17) { return $false }
    $matches=@($Result.output | ForEach-Object { [string]$_ } | Where-Object { $_.StartsWith('MUTATION_ASSERTION:') })
    return ($matches.Count -eq 1 -and $matches[0] -cmatch ('\AMUTATION_ASSERTION:FAIL: '+$Expected+'\z'))
}
$source=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-connect-stages.ps1'))
$cases=@(
    @{name='attempt-binding';expected='unknown attempt-binding at fixture line [0-9]+ observed none';old='if ($attempt -ne 1)';new='if ($false)'},
    @{name='sequence-binding';expected='unknown malformed at fixture line [0-9]+ observed none';old='$sequence -ne $records+1';new='$false'},
    @{name='nested-binding';expected='unknown malformed at fixture line [0-9]+ observed incomplete';old='$stack[$stack.Count-1] -cne $stage';new='$false'},
    @{name='partial-outcome';expected='unknown incomplete at fixture line [0-9]+ observed incomplete';old="`$report.reason='incomplete';`$report.lastEnteredStage=`$lastEntered;return `$report";new="`$report.reason='incomplete';`$report.attemptOutcome='failed';return `$report"},
    @{name='whole-line-grammar';expected='unknown malformed at fixture line [0-9]+ observed none';old='outcome=(entered|completed|failed)\z';new='outcome=(entered|completed|failed)'},
    @{name='sequence-evidence';expected='requires completed recovery and saved sequence';old='-not $RecoveryChecksCompleted -or -not $SequenceReportSaved -or';new=''},
    @{name='no-authority';expected='complete report has no authority';old='changesRecoveryDecision=$false;authorizesAction=$false';new='changesRecoveryDecision=$true;authorizesAction=$true'},
    @{name='query-allowlist';expected='actual original-query observer replay';old="'query-wait-failure'";new="'query-unrecognized'"},
    @{name='root-allowlist';expected='root snapshot preserves deadline failure and no authority';old="'query-root-exited'";new="'query-root-unrecognized'"},
    @{name='progress-allowlist';expected='progress snapshot preserves original failure and no authority';old="'query-progress-json'";new="'query-progress-unrecognized'"},
    @{name='failure-order';expected='actual Go observer replay binding';old="`$outcome -ceq 'failed' -and `$firstFailed -ceq 'unknown'";new="`$outcome -ceq 'failed'"}
)
$temp=Join-Path ([IO.Path]::GetTempPath()) ('connect-stage-mutants-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
try {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'original-connect-preconditions.ps1') -Destination $temp
    if ($PSVersionTable.PSEdition -ceq 'Desktop') { $hostExe=Join-Path $PSHOME 'powershell.exe' }
    elseif ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { $hostExe=Join-Path $PSHOME 'pwsh.exe' }
    else { $hostExe=Join-Path $PSHOME 'pwsh' }
    # Negative driver fixtures cannot be mistaken for a behaviorally killed mutant.
    $fixture=Join-Path $temp 'driver-fixture.ps1'
    foreach ($check in @(
        @{body="param(`$Implementation) [Console]::Error.WriteLine('expected child stderr'); throw 'FAIL: driver sentinel'";accepted=$true},
        @{body="param(`$Implementation) throw 'FAIL: wrong assertion'";accepted=$false},
        @{body="param(`$Implementation) throw 'child crash'";accepted=$false},
        @{body="param(`$Implementation) exit 19";accepted=$false},
        @{body="param(`$Implementation) Write-Output 'MUTATION_ASSERTION:FAIL: driver sentinel'";accepted=$false},
        @{body="param(`$Implementation) if (";accepted=$false}
    )) {
        [IO.File]::WriteAllText($fixture,$check.body)
        $result=Invoke-MutationChild $hostExe $fixture ''
        if ($ErrorActionPreference -cne 'Stop' -or (Test-MutationRejection $result 'driver sentinel') -ne $check.accepted) { throw 'Mutation driver classification or preference regression.' }
    }
    Write-Output 'MUTATION_DRIVER_REGRESSIONS_PASSED'
    foreach ($case in $cases) {
        if ($source.Split([string[]]@($case.old),[StringSplitOptions]::None).Count -ne 2) { throw ('Mutation anchor not unique: '+$case.name) }
        $path=Join-Path $temp ($case.name+'.ps1');[IO.File]::WriteAllText($path,$source.Replace($case.old,$case.new))
        $result=Invoke-MutationChild $hostExe (Join-Path $PSScriptRoot 'original-connect-stages.tests.ps1') $path
        if ($ErrorActionPreference -cne 'Stop') { throw 'Child invocation changed driver error preference.' }
        if (-not (Test-MutationRejection $result $case.expected)) { throw ('Mutation not behaviorally rejected: '+$case.name) }
        Write-Output ('RED_MUTATION_CAUGHT '+$case.name)
    }
} finally { [IO.Directory]::Delete($temp,$true) }
Write-Output 'ORIGINAL_CONNECT_STAGES_RED_MUTATIONS_PASSED'
