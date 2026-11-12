# Broken disposable copies must fail a named behavioral assertion, not parsing.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$implementation=Join-Path $PSScriptRoot 'original-desktop-cleanup.ps1'
$caller=Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1'
$source=[IO.File]::ReadAllText($implementation)
$callerSource=[IO.File]::ReadAllText($caller)
# Use literal strings so mutation anchors cannot expand PowerShell variables.
$cases=@(
    @{name='discard-confirmed-force';old='$r.rootExitConfirmed=$true;$r.exitKind=''forced''';new='$r.rootExitConfirmed=$false;$r.exitKind=''forced''';expected='forced exit keeps confirmed root fact'},
    @{name='timeout-as-exit';old='if (-not (& $WaitForExit 5000)) { $r.failureStage=$stage;return $r }';new='if (-not (& $WaitForExit 5000)) { $r.rootExitConfirmed=$true;$r.failureStage=$stage;return $r }';expected='unknown root remains unknown: timeout'},
    @{name='skip-identity-recheck';old='$stage=''identity-before-force''; & $CheckIdentity';new='$stage=''identity-before-force''';expected='force rechecks owner then confirms exit'},
    @{name='original-close-is-not-quit';old='if ($RequestWindowClose) {';new='if ($true) {';expected='original tray root is not closed as if that were Quit'},
    @{name='root-is-not-tree';old='$Cleanup.descendantExitVerified=$false';new='$Cleanup.descendantExitVerified=$Cleanup.rootExitsConfirmed';expected='root and disposal never prove graceful or descendants'},
    @{name='root-is-not-graceful';old='$Cleanup.gracefulShutdownVerified=$false';new='$Cleanup.gracefulShutdownVerified=$Cleanup.rootExitsConfirmed';expected='root and disposal never prove graceful or descendants'},
    @{name='empty-root-success';old='$Cleanup.requiredRoots -gt 0 -and';new='$true -and';expected='no roots is not observed exit success'},
    @{name='skip-package-disposal';old="try { & `$entry[1];`$Cleanup[`$entry[0]]='disposed' } catch { }";new="try { & `$entry[1];`$Cleanup[`$entry[0]]='disposed' } catch { break }";expected='each disposal runs independently'},
    @{name='disposal-erases-root';old='$Cleanup.gracefulShutdownVerified=$false';new='$Cleanup.rootExitsConfirmed=$Cleanup.rootExitsConfirmed -and $Cleanup.resourcesDisposed; $Cleanup.gracefulShutdownVerified=$false';expected='disposal cannot erase known root exits: original'},
    @{name='ignore-recovery';caller=$true;old='    if ($safeCleanup) {';new='    if ($true) {';expected='unknown recovery performs no root close or kill'},
    @{name='discard-original-result';caller=$true;old='$report.cleanup.originalRoot=Stop-AcceptanceOwner $process $exe $ticks';new='$null=Stop-AcceptanceOwner $process $exe $ticks';expected='coordinator verifies retained-root cleanup without graceful or tree-drain claims'},
    @{name='overwrite-operation-failure';caller=$true;old="if (`$report.failureStage -ceq 'none') { `$report.failureStage='owned-cleanup'";new="if (`$true) { `$report.failureStage='owned-cleanup'";expected='cleanup preserves the prior operation failure'}
)
if ($PSVersionTable.PSEdition -ceq 'Desktop') { $hostExe=Join-Path $PSHOME 'powershell.exe' }
elseif ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { $hostExe=Join-Path $PSHOME 'pwsh.exe' }
else { $hostExe=Join-Path $PSHOME 'pwsh' }
$temp=Join-Path ([IO.Path]::GetTempPath()) ('original-cleanup-mutants-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
try {
    foreach ($case in $cases) {
        $isCaller=$case.ContainsKey('caller') -and $case.caller
        $data=if ($isCaller) { $callerSource } else { $source }
        if ($data.Split([string[]]@($case.old),[StringSplitOptions]::None).Count -ne 2) { throw ('Mutation anchor not unique: '+$case.name) }
        $path=Join-Path $temp ($case.name+'.ps1');[IO.File]::WriteAllText($path,$data.Replace($case.old,$case.new))
        $impl=if ($isCaller) { $implementation } else { $path }
        $call=if ($isCaller) { $path } else { $caller }
        $test=Join-Path $PSScriptRoot 'original-desktop-cleanup.tests.ps1'
        $command="try { & '"+$test.Replace("'","''")+"' -Implementation '"+$impl.Replace("'","''")+"' -CallerPath '"+$call.Replace("'","''")+"'; exit 0 } catch { if (`$_.Exception.Message.StartsWith('FAIL: ')) { [Console]::Out.WriteLine('MUTATION_ASSERTION:' + `$_.Exception.Message); exit 17 }; exit 18 }"
        $encoded=[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
        $savedPreference=$ErrorActionPreference
        try {
            $ErrorActionPreference='Continue'
            $output=@(& $hostExe -NoProfile -NonInteractive -EncodedCommand $encoded 2>&1)
            $code=$LASTEXITCODE
        } finally { $ErrorActionPreference=$savedPreference }
        $assertions=@($output | ForEach-Object { [string]$_ } | Where-Object { $_.StartsWith('MUTATION_ASSERTION:') })
        if ($code -ne 17 -or $assertions.Count -ne 1 -or $assertions[0] -cne ('MUTATION_ASSERTION:FAIL: '+$case.expected)) {
            throw ('Mutation not behaviorally rejected: '+$case.name)
        }
        Write-Output ('RED_MUTATION_CAUGHT '+$case.name)
    }
} finally { [IO.Directory]::Delete($temp,$true) }
Write-Output 'ORIGINAL_DESKTOP_CLEANUP_RED_MUTATIONS_PASSED'
