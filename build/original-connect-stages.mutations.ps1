[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$source=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-connect-stages.ps1'))
$cases=@(
    @{name='attempt-binding';old='if ($attempt -ne 1)';new='if ($false)'},
    @{name='sequence-binding';old='$sequence -ne $records+1';new='$false'},
    @{name='nested-binding';old='$stack[$stack.Count-1] -cne $stage';new='$false'},
    @{name='partial-outcome';old="`$report.reason='incomplete';`$report.lastEnteredStage=`$lastEntered;return `$report";new="`$report.reason='incomplete';`$report.attemptOutcome='failed';return `$report"},
    @{name='whole-line-grammar';old='outcome=(entered|completed|failed)\z';new='outcome=(entered|completed|failed)'},
    @{name='sequence-evidence';old='-not $RecoveryChecksCompleted -or -not $SequenceReportSaved -or';new=''},
    @{name='no-authority';old='changesRecoveryDecision=$false;authorizesAction=$false';new='changesRecoveryDecision=$true;authorizesAction=$true'},
    @{name='failure-order';old="`$outcome -ceq 'failed' -and `$firstFailed -ceq 'unknown'";new="`$outcome -ceq 'failed'"}
)
$temp=Join-Path ([IO.Path]::GetTempPath()) ('connect-stage-mutants-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
try {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'original-connect-preconditions.ps1') -Destination $temp
    if ($PSVersionTable.PSEdition -ceq 'Desktop') { $hostExe=Join-Path $PSHOME 'powershell.exe' }
    elseif ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { $hostExe=Join-Path $PSHOME 'pwsh.exe' }
    else { $hostExe=Join-Path $PSHOME 'pwsh' }
    foreach ($case in $cases) {
        if ($source.Split([string[]]@($case.old),[StringSplitOptions]::None).Count -ne 2) { throw ('Mutation anchor not unique: '+$case.name) }
        $path=Join-Path $temp ($case.name+'.ps1');[IO.File]::WriteAllText($path,$source.Replace($case.old,$case.new))
        $output=@(& $hostExe -NoProfile -NonInteractive -File (Join-Path $PSScriptRoot 'original-connect-stages.tests.ps1') -Implementation $path 2>&1)
        if ($LASTEXITCODE -eq 0 -or ($output -join "`n") -notmatch 'FAIL:') { throw ('Mutation not behaviorally rejected: '+$case.name) }
        Write-Output ('RED_MUTATION_CAUGHT '+$case.name)
    }
} finally { [IO.Directory]::Delete($temp,$true) }
Write-Output 'ORIGINAL_CONNECT_STAGES_RED_MUTATIONS_PASSED'
