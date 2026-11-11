# Execute deliberately broken copies against the same offline behavioral tests.
# Mutants never replace repository source, open a window or touch the registry.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if ($PSVersionTable.PSEdition -cne 'Core') { throw 'Core test host required.' }
$source=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-connect-action-core.ps1'))
$cases=@(
    @{name='duplicate-control';old='$all.Count -ne 1';new='$all.Count -lt 1'},
    @{name='deadline-boundary';old='$Elapsed -ge $Limit';new='$Elapsed -gt $Limit'},
    @{name='retry-uncertain';old="if (`$Ledger.ContainsKey(`$Action)) { throw 'Action was already attempted.' }";new='# mutation: permit retry'},
    @{name='identity-drift';old='if ($Expected[$field] -cne $Actual[$field])';new='if ($false)'},
    @{name='journal-removal';old='$JournalPresent -or -not (Test-OriginalSelectionEqual $Current $Before)';new='-not (Test-OriginalSelectionEqual $Current $Before)'},
    @{name='external-recovery';old="{ throw 'External selection change.' }";new='{ }'},
    @{name='readiness-duplicate';old="if (`$all.Count -gt 1) { `$o.reason='duplicate' }";new="if (`$all.Count -gt 2) { `$o.reason='duplicate' }"},
    @{name='readiness-owner';old="`$Readiness.lastCompleteObservation=Get-OriginalConfigureObservation `$targets`n        `$Readiness.status='owner-check'; & `$CheckOwner";new='$Readiness.lastCompleteObservation=Get-OriginalConfigureObservation $targets'},
    @{name='readiness-deadline';old="`$Readiness.status='deadline';Assert-OriginalDeadline (& `$ReadElapsed) 15000`n        `$Readiness.status='reading'";new="`$Readiness.status='deadline';Assert-OriginalDeadline (& `$ReadElapsed) 16000`n        `$Readiness.status='reading'"},
    @{name='readiness-report-fields';old="Assert-OriginalFields `$o @('total','enabledVisible','button','toggle','invoke','off','on','otherState','reason')";new='# mutation: accept unknown report fields'},
    @{name='post-action-unknown';old='if ($c.name -cnotin $labels)';new='if ($false)'},
    @{name='post-action-first';old='if ($null -eq $Report.firstComplete)';new='if ($true)'},
    @{name='post-action-read-failure';old="catch { `$Report.status='read-failed';throw }";new="catch { `$Report.status='observed';throw }"},
    @{name='post-action-hidden-error';old='} elseif (-not $c.offscreen) {';new='} elseif ($true) {'}
)
$temp=Join-Path ([IO.Path]::GetTempPath()) ('original-action-mutants-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
try {
    foreach ($case in $cases) {
        if (($source.Split([string[]]@($case.old),[StringSplitOptions]::None)).Count -ne 2) { throw ('Mutation anchor not unique: '+$case.name) }
        $path=Join-Path $temp ($case.name+'.ps1')
        [IO.File]::WriteAllText($path,$source.Replace($case.old,$case.new))
        $hostPath=Join-Path $PSHOME $(if ($IsWindows) { 'pwsh.exe' } else { 'pwsh' })
        $output=@(& $hostPath -NoLogo -NoProfile -NonInteractive -File (Join-Path $PSScriptRoot 'original-connect-action.tests.ps1') -CorePath $path 2>&1)
        if ($LASTEXITCODE -eq 0 -or ($output -join "`n") -notmatch 'FAIL:') { throw ('Mutation did not fail behaviorally: '+$case.name) }
        Write-Output ('RED_MUTATION_CAUGHT '+$case.name)
    }
} finally { [IO.Directory]::Delete($temp,$true) }
Write-Output 'ORIGINAL_CONNECT_ACTION_RED_MUTATIONS_PASSED'
