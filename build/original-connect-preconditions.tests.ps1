[CmdletBinding()]
param([string]$Implementation='')
if ($Implementation -eq '') { $Implementation=Join-Path $PSScriptRoot 'original-connect-preconditions.ps1' }
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. $Implementation
function Assert($Value,[string]$Message) { if (-not $Value) { throw ('FAIL: '+$Message) } }
function Context {
    $install=Join-Path ([IO.Path]::GetTempPath()) 'authored-package-canary'
    return @{Prepared=$true;Closed=$false;App=@{Name='OpenAI.Codex';PackageFamilyName='OpenAI.Codex_2p2nqsd0c76g0';SignatureKind='Store';Status='Ok';Version='26.1103.4239.0';InstallLocation=$install};NativeExecutable=[IO.Path]::Combine($install,'app\resources\codex.exe')}
}
$f=Get-OriginalPreparedRegistrationFacts (Context)
$r=New-OriginalPreconditionReport $f
Assert ($r.inspected.exactPackageFamily -ceq 'passed' -and $r.inspected.storeSignatureKind -ceq 'passed') 'prepared exact metadata accepted'
foreach ($case in @(@('Name','Other','exactPackageName'),@('PackageFamilyName','OpenAI.Codex_wrong','exactPackageFamily'),@('SignatureKind','Developer','storeSignatureKind'),@('Status','Modified','packageStatusOk'),@('Version','99.0','expectedPackageVersion'))) {
    $c=Context;$c.App[$case[0]]=$case[1]
    $r=New-OriginalPreconditionReport (Get-OriginalPreparedRegistrationFacts $c)
    Assert ($r.inspected[$case[2]] -ceq 'failed') ('reject metadata '+$case[0])
}
foreach ($change in @('closed','unprepared','missing')) {
    $c=Context
    if ($change -ceq 'closed') {$c.Closed=$true};if ($change -ceq 'unprepared') {$c.Prepared=$false};if ($change -ceq 'missing') {$c=$null}
    $threw=$false;try {$null=Get-OriginalPreparedRegistrationFacts $c}catch{$threw=$true}
    Assert $threw 'invalid preparation context rejected'
}
foreach ($name in @('App','Name','PackageFamilyName','SignatureKind','Status','Version','InstallLocation')) {
    foreach ($kind in @('missing','null','wrong-type')) {
        $c=Context
        if ($name -ceq 'App') { $c.App=$null } elseif ($kind -ceq 'missing') { $c.App.Remove($name) } elseif ($kind -ceq 'null') { $c.App[$name]=$null } else { $c.App[$name]=@{secret='MISSING_METADATA_CANARY'} }
        $threw=$false;try {$null=Get-OriginalPreparedRegistrationFacts $c}catch{$threw=$true}
        Assert $threw ('missing or malformed metadata unavailable '+$name+' '+$kind)
    }
}
$r=New-OriginalPreconditionReport @{exactPackageName='SECRET-CANARY';selectionJournal='C:\PRIVATE-CANARY';extra='TOKEN-CANARY';storeSignatureKind=$false}
$j=$r|ConvertTo-Json -Depth 6 -Compress
Assert ($j -notmatch 'SECRET-CANARY|PRIVATE-CANARY|TOKEN-CANARY|extra') 'projection redacts arbitrary values and fields'
Assert ($r.inspected.exactPackageName -ceq 'unknown' -and $r.artifacts.selectionJournal -ceq 'unknown') 'unknown never becomes acceptance or absence'
Assert ($r.notProven -contains 'actual-cli-version-probe' -and $r.notProven -contains 'same-environment-package-query' -and $r.notProven -contains 'host-preparation') 'metadata never proves original execution'
Assert (-not $r.changesRecoveryDecision -and -not $r.authorizesAction) 'no authority in report'
$sequence=@{cleanupAllowed=$false;preserveRecovery=$true;status='uncertain'}
$before=$sequence|ConvertTo-Json -Compress
foreach ($flags in @(@{},@{RecoveryChecksCompleted=$true},@{SequenceReportSaved=$true})) {
    $r=Get-OriginalConnectPreconditions -Sequence $sequence @flags
    Assert ($r.status -ceq 'sequence-evidence-required') 'no inspection before saved recovery evidence'
}
Assert (($sequence|ConvertTo-Json -Compress) -ceq $before) 'sequence remains unchanged'
$r=Get-OriginalConnectPreconditions -Sequence @{} -RecoveryChecksCompleted -SequenceReportSaved
Assert ($r.status -ceq 'sequence-evidence-required') 'missing sequence fields produce fixed refusal'
# Parse the production implementation independently. Its command surface cannot
# create a process, invoke an app, query a service or read file contents.
$tokens=$null;$errors=$null;$ast=[Management.Automation.Language.Parser]::ParseFile($Implementation,[ref]$tokens,[ref]$errors)
Assert ($errors.Count -eq 0) 'PowerShell syntax'
# Windows PowerShell can bind script parameters before PSScriptRoot is set.
# Resolve script-relative paths in the body, never inside parameter defaults.
function Test-SafeScriptParameterDefaults($Tree) {
    foreach ($parameter in $Tree.FindAll({param($node) $node -is [Management.Automation.Language.ParameterAst]},$true)) {
        if ($null -eq $parameter.DefaultValue) { continue }
        $earlyPaths=@($parameter.DefaultValue.FindAll({param($node)
            $node -is [Management.Automation.Language.VariableExpressionAst] -and
            (($node.VariablePath.UserPath -split ':')[-1] -iin @('PSScriptRoot','PSCommandPath'))
        },$true))
        if ($earlyPaths.Count -ne 0) { return $false }
    }
    return $true
}
$defaultTokens=$null;$defaultErrors=$null
$unsafeDefault=[Management.Automation.Language.Parser]::ParseInput('param([string]$Path=(Join-Path $pSsCrIpTrOoT "fixture"))',[ref]$defaultTokens,[ref]$defaultErrors)
Assert (-not (Test-SafeScriptParameterDefaults $unsafeDefault)) 'default-expression guard detects old Desktop binding bug'
$safeDefault=[Management.Automation.Language.Parser]::ParseInput('param([string]$Path=""); if ($Path -eq "") {$Path=Join-Path $PSScriptRoot "fixture"}',[ref]$defaultTokens,[ref]$defaultErrors)
Assert (Test-SafeScriptParameterDefaults $safeDefault) 'body-time script-relative resolution is allowed'
Assert (Test-SafeScriptParameterDefaults $ast) 'implementation has no script-relative parameter defaults'
$defaultFiles=@(Get-ChildItem -LiteralPath $PSScriptRoot -File | Where-Object { $_.Name -like 'original-connect-preconditions*.ps1' -or $_.Name -like 'original-connect-action*.ps1' -or $_.Name -like 'original-desktop-window-capture*.ps1' })
foreach ($defaultFile in $defaultFiles) {
    $defaultAst=[Management.Automation.Language.Parser]::ParseFile($defaultFile.FullName,[ref]$defaultTokens,[ref]$defaultErrors)
    Assert ($defaultErrors.Count -eq 0) 'sibling diagnostic syntax'
    Assert (Test-SafeScriptParameterDefaults $defaultAst) ('script-relative parameter default forbidden: '+$defaultFile.Name)
}

# Inspect all helper and fixture bodies even when a Windows branch is not run.
# $null is the intentional output-discard target; preferences are not automatic.
$automaticNames=@('HOME','HOST','PID','PROFILE','PSHOME','PSVersionTable','PSEdition','PSCommandPath','PSScriptRoot','args','input','Error','Matches','MyInvocation','PSBoundParameters','PSCmdlet','StackTrace','LASTEXITCODE','ExecutionContext','ShellId','NestedPromptLevel','foreach','switch','true','false','_','PSItem','IsWindows','IsLinux','IsMacOS','PSStyle','PSSenderInfo','EnabledExperimentalFeatures','PSCulture','PSUICulture','ConsoleFileName','Event','EventArgs','EventSubscriber','Sender','PSDebugContext')
foreach ($checkedFile in @($Implementation,$PSCommandPath,(Join-Path $PSScriptRoot 'original-connect-preconditions.mutations.ps1'))) {
    $parseTokens=$null;$parseErrors=$null
    $checkedAst=[Management.Automation.Language.Parser]::ParseFile($checkedFile,[ref]$parseTokens,[ref]$parseErrors)
    Assert ($parseErrors.Count -eq 0) 'checked helper syntax'
    $writes=$checkedAst.FindAll({param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -or
        $node -is [Management.Automation.Language.ForEachStatementAst] -or
        $node -is [Management.Automation.Language.ParameterAst] -or
        ($node -is [Management.Automation.Language.UnaryExpressionAst] -and $node.TokenKind -in @('PlusPlus','MinusMinus','PostfixPlusPlus','PostfixMinusMinus'))
    },$true)
    foreach ($write in $writes) {
        $target=$null
        if ($write -is [Management.Automation.Language.AssignmentStatementAst]) {$target=$write.Left}
        elseif ($write -is [Management.Automation.Language.ForEachStatementAst]) {$target=$write.Variable}
        elseif ($write -is [Management.Automation.Language.ParameterAst]) {$target=$write.Name}
        else {$target=$write.Child}
        while ($target -is [Management.Automation.Language.ConvertExpressionAst]) {$target=$target.Child}
        if ($target -is [Management.Automation.Language.VariableExpressionAst]) {
            $variableName=($target.VariablePath.UserPath -split ':')[-1]
            Assert ($automaticNames -inotcontains $variableName) ('automatic variable assignment forbidden: '+$variableName)
        }
    }
}

$commands=@($ast.FindAll({param($n)$n -is [Management.Automation.Language.CommandAst]},$true)|ForEach-Object {$_.GetCommandName()})
$allowed=@('ConvertTo-OriginalPredicate','New-OriginalPreconditionReport','Get-OriginalPreparedRegistrationFacts','Test-OriginalDiagnosticPath','Get-OriginalDiagnosticPresence','Get-OriginalDiagnosticEntries')
foreach ($command in $commands) { Assert ($command -cin $allowed) ('unexpected command surface '+$command) }
$text=[IO.File]::ReadAllText($Implementation)
Assert ($text -notmatch '::(Start|ReadAllText|ReadAllBytes|OpenRead|WriteAllText|WriteAllBytes|CreateDirectory|Delete)\s*\(') 'no process/content/mutation APIs'
Assert ($text.Contains('$entries.Count -ge 128') -and $text.Contains('$generationCount -gt 8') -and $text.Contains('$parts.Count -gt 64')) 'finite metadata bounds'
Write-Output 'PASS exact metadata, invalid context, redaction, provenance, saved-sequence gate, immutable recovery and bounded read-only surface'
if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) {
    $fixture=Join-Path ([IO.Path]::GetTempPath()) ('precondition-fixture-'+[guid]::NewGuid().ToString('N'))
    try {
        $run=Join-Path $fixture 'app';$fixtureHome=Join-Path $fixture 'home';$system=Join-Path $fixture 'system'
        foreach ($relative in @('app\data\native-codex-connection\generation-123','app\data\native-codex-connection\profile-456','home\.codex','system\System32\WindowsPowerShell\v1.0')) {
            $null=[IO.Directory]::CreateDirectory((Join-Path $fixture $relative))
        }
        foreach ($relative in @('app\magpie-codex-bridge.exe','app\data\native-codex-connection\generation-123\supplier-catalog.json','app\data\native-codex-connection\generation-123\channel-0123456789abcdef0123456789abcdef.json','app\data\native-codex-connection\profile-456\bridge.json','system\System32\WindowsPowerShell\v1.0\powershell.exe')) {
            [IO.File]::WriteAllText((Join-Path $fixture $relative),'INERT_CONTENT_CANARY_NOT_EXECUTABLE')
        }
        $start=[Diagnostics.ProcessStartInfo]::new();$start.FileName=Join-Path $run 'Magpie-Native-Codex-Experimental.exe'
        $start.EnvironmentVariables.Clear();$start.EnvironmentVariables['SystemRoot']=$system
        $start.EnvironmentVariables['HOME']=$fixtureHome;$start.EnvironmentVariables['USERPROFILE']=$fixtureHome;$start.EnvironmentVariables['CODEX_HOME']=Join-Path $fixtureHome '.codex'
        $r=Get-OriginalConnectPreconditions -PackageContext (Context) -OriginalStartInfo $start -OwnedRun $run -OwnedHome $fixtureHome -Sequence $sequence -RecoveryChecksCompleted -SequenceReportSaved
        Assert ($r.status -ceq 'snapshot' -and $r.inspected.homeMatchesFixture -ceq 'passed' -and $r.inspected.nativeHomeSafe -ceq 'passed') 'Windows owned environment metadata'
        Assert ($r.artifacts.selectionJournal -ceq 'absent' -and $r.artifacts.generationDirectory -ceq 'present' -and $r.artifacts.supplierCatalog -ceq 'present' -and $r.artifacts.channelDescriptor -ceq 'present' -and $r.artifacts.bridgeProfile -ceq 'present') 'Windows fixed artifacts without content reads'
        Assert (($r|ConvertTo-Json -Depth 6 -Compress) -notmatch 'INERT_CONTENT_CANARY|precondition-fixture') 'Windows report leaks no content or fixture path'
        for ($i=0;$i -lt 129;$i++) { [IO.File]::WriteAllText((Join-Path $run ('data\unused-'+$i)), '') }
        $r=Get-OriginalConnectPreconditions -PackageContext (Context) -OriginalStartInfo $start -OwnedRun $run -OwnedHome $fixtureHome -Sequence $sequence -RecoveryChecksCompleted -SequenceReportSaved
        Assert ($r.status -ceq 'unavailable' -and $r.artifacts.generationDirectory -ceq 'unknown') 'bounded enumeration fails unknown, not absent'
        Assert (($sequence|ConvertTo-Json -Compress) -ceq $before) 'Windows observation preserves uncertain recovery'
        Write-Output 'PASS Windows inert filesystem collector fixtures'
    } finally { if ([IO.Directory]::Exists($fixture)) { [IO.Directory]::Delete($fixture,$true) } }
} else { Write-Output 'NOT RUN Windows inert filesystem collector fixtures (non-Windows host)' }
