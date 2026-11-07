# Synthetic safety fixtures only. No Desktop activation, download, account or UIA.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'desktop-live-welcome.ps1')
. (Join-Path $PSScriptRoot 'desktop-winapp-inspect.ps1')
$count=0
function Pass([bool]$Condition,[string]$Name) { if (-not $Condition) { throw ('FAIL: '+$Name) }; $script:count++ }
function Reject([scriptblock]$Action,[string]$Name) { $failed=$false;try { & $Action | Out-Null } catch { $failed=$true };Pass $failed $Name }
function Read-TestAst([string]$Name) {
    $tokens=$null;$errors=$null
    $result=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $Name),[ref]$tokens,[ref]$errors)
    Pass ($errors.Count -eq 0) ('valid syntax: '+$Name)
    return $result
}
function Import-TestFunction($Source,[string]$Name) {
    $definitions=@($Source.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq $Name},$true))
    Pass ($definitions.Count -eq 1) ('unique pure helper: '+$Name)
    return [scriptblock]::Create($definitions[0].Extent.Text)
}
$ast=Read-TestAst 'test-desktop-cold-start.ps1'
$null=Read-TestAst 'desktop-winapp-inspect.native.tests.ps1'
foreach ($name in @('Assert-DesktopCIOptions','Assert-PublicHash','Assert-PublicRegularPath','Assert-PublicZipIndex',
    'Get-BoundedStreamDigest','Get-PublicZipEntryDigest','Get-DesktopPackageIdentityEvidence','Wait-DesktopRegistration',
    'Complete-DesktopActivationProcess')) { . (Import-TestFunction $ast $name) }
$profile=Read-TestAst 'test-desktop-ci-profile.ps1'
foreach ($name in @('New-ActivationProcessFixture','Invoke-ActivationFixture','New-RegistrationFixture','Invoke-RegistrationFixture')) {
    . (Import-TestFunction $profile $name)
}
$catalog=Read-TestAst 'test-desktop-public-catalog.ps1'
. (Import-TestFunction $catalog 'New-PackageIdentityFixture')
Assert-NativeUIOptions $true 'baseline' $false $false $false $false $false $false '' 'native-ui-inspection-v1'
Assert-NativeUIOptions $false 'observe' $true $true $true $true $true $true 'anything' ''
for ($mask=1;$mask -lt 64;$mask++) {
    $flags=@();for($bit=0;$bit -lt 6;$bit++){ $flags += [bool]($mask -band (1 -shl $bit)) }
    Reject { Assert-NativeUIOptions $true 'baseline' $flags[0] $flags[1] $flags[2] $flags[3] $flags[4] $flags[5] '' 'native-ui-inspection-v1' } 'all conflicting mode combinations stop'
}
Reject { Assert-NativeUIOptions $true 'observe' $false $false $false $false $false $false '' 'native-ui-inspection-v1' } 'no bridge mode'
Reject { Assert-NativeUIOptions $true 'baseline' $false $false $false $false $false $false 'baseline' 'native-ui-inspection-v1' } 'no native-entry persistence case'
Reject { Assert-NativeUIOptions $true 'baseline' $false $false $false $false $false $false '' '' } 'explicit marker required'
Assert-DesktopCIOptions 'baseline' $false $false $false 'true' 'github-hosted' 'breakingtime126/magpie' 'refs/heads/experiment/app-server-bridge-v0.1.3035'
Reject { Assert-DesktopCIOptions 'baseline' $false $false $false 'true' 'self-hosted' 'breakingtime126/magpie' 'refs/heads/experiment/app-server-bridge-v0.1.3035' } 'no user computer'

# Existing strict registration helper: identity/status unknown never becomes ready.
$r=Invoke-RegistrationFixture @((New-RegistrationFixture))
Pass ($r.result.status -ceq 'ready') 'registered expected package ready'
$r=Invoke-RegistrationFixture @((New-RegistrationFixture 'Servicing'))
Pass ($r.result.status -ceq 'blocked' -and $r.result.reason -ceq 'registration_readiness_timeout') 'bounded registration deadline'
$r=Invoke-RegistrationFixture @((New-RegistrationFixture)) -ReadFails
Pass ($r.result.status -ceq 'blocked') 'registration read failure stops'
$p=New-ActivationProcessFixture
Pass (Invoke-ActivationFixture $p) 'exact activation cleanup confirmed'
Pass ($p.CloseCalls -eq 1 -and $p.KillCalls -eq 0) 'cleanup targets only owned process'
$p=New-ActivationProcessFixture @($false,$false)
Reject { Invoke-ActivationFixture $p } 'uncertain cleanup is not success'
$p=New-ActivationProcessFixture
Reject { Invoke-ActivationFixture $p { 'other.package' } } 'changed package never signaled'
Pass ($p.CloseCalls -eq 0 -and $p.KillCalls -eq 0) 'foreign package untouched'
$p=New-ActivationProcessFixture;$p.Id=43
Reject { Invoke-ActivationFixture $p } 'changed PID never signaled'
Pass ($p.CloseCalls -eq 0 -and $p.KillCalls -eq 0) 'foreign PID untouched'

# AST boundaries complement behavior tests. Never execute the harness body.
$reads=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Get-DesktopPackageIdentityEvidence'},$true))
Pass ($reads.Count -eq 3) 'separate package-only/native/reviewed-build trust reads'
$nativeRead=@($reads|Where-Object {
    $branch=$_.Parent;while($null -ne $branch -and $branch -isnot [Management.Automation.Language.IfStatementAst]){$branch=$branch.Parent}
    $null -ne $branch -and $branch.Clauses[0].Item1.Extent.Text -ceq '$EnableNativeReadOnlyInspection'
})
Pass ($nativeRead.Count -eq 1) 'one native signed identity read'
$installs=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Add-AppxProvisionedPackage'},$true))
Pass ($installs.Count -eq 1 -and $nativeRead[0].Extent.EndOffset -lt $installs[0].Extent.StartOffset) 'signature/identity before installation'
$calls=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-WinAppInspection'},$true))
Pass ($calls.Count -eq 1 -and $calls[0].Extent.Text.EndsWith('-NativeOriginal:$EnableNativeReadOnlyInspection')) 'one shared bounded inspect call'
$source=$ast.Extent.Text
Pass ($source.Contains('$useLiveFixtures = $EnableReviewedLiveWelcome -or $EnableReadOnlyWinAppInspection')) 'native original never starts synthetic fixtures'
Pass ($source.Contains('if ($NativeEntryCase -or $EnableNativeReadOnlyInspection) {')) 'AUMID process identity is retained'
Pass ($source.Contains('$ownedActivationProcess.StartTime.ToUniversalTime() -lt $entryActivationFloor')) 'fresh activation boundary retained'
Pass ($source.Contains('$ownedActivationProcess.SessionId -ne $processSession')) 'owned session boundary retained'
Pass ($source.Contains('-or $NativeEntryCase -or $EnableNativeReadOnlyInspection) -and')) 'native original uses held process cleanup'
$footer=$source.IndexOf("if (`$EnableNativeReadOnlyInspection) {`n    `$report.nativeControlPlane")
if ($footer -lt 0) { $footer=$source.IndexOf("if (`$EnableNativeReadOnlyInspection) {`r`n    `$report.nativeControlPlane") }
$turns=$source.IndexOf('$nativeEvidence = Invoke-ControlPlaneNative')
Pass ($footer -gt 0 -and $footer -lt $turns) 'native-only footer precedes native turns'
$footerText=$source.Substring($footer,$turns-$footer)
Pass ($footerText.Contains('exit $exitCode') -and $footerText.Contains("`$report.bridgeAcceptance = 'not-tested'")) 'native-only outcome exits without bridge claim'

$temporary=Join-Path ([IO.Path]::GetTempPath()) ('native-ui-tests-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temporary)
Add-Type -AssemblyName System.IO.Compression.FileSystem
try {
    $fixture=New-PackageIdentityFixture
    $e=Get-DesktopPackageIdentityEvidence $fixture.path $fixture.sha256 0 'Valid' 'CN=50BDFD77-8903-4850-9FFE-6E8522F64D5B'
    Pass ($e.status -ceq 'observed' -and -not $e.runtimeAccepted -and $e.packageSha256 -ceq $fixture.sha256) 'new signed identity is data, not protocol acceptance'
    foreach($variant in @('version','publisher','architecture','name','duplicate-identity','dtd','malformed','manifest-size','missing-backend','missing-archive','case-alias','traversal','symlink','package-hash')) {
        Reject { $bad=New-PackageIdentityFixture $variant;Get-DesktopPackageIdentityEvidence $bad.path $bad.sha256 0 'Valid' 'CN=50BDFD77-8903-4850-9FFE-6E8522F64D5B' } 'untrusted or unbounded package metadata stops'
    }
    Reject { Get-DesktopPackageIdentityEvidence $fixture.path $fixture.sha256 0 'NotSigned' 'CN=50BDFD77-8903-4850-9FFE-6E8522F64D5B' } 'unsigned package cannot reach installation'
    Initialize-LiveSupervisor
    $hostExecutable=(Get-Process -Id $PID).Path
    function New-NativeTreeText([string]$Label,[switch]$OmitName,[switch]$ValueOnly) {
        $node=[ordered]@{type='Text';name=$Label;isEnabled=$true;isOffscreen=$false;isInvokable=$false;x=0;y=0;width=10;height=10}
        if ($OmitName -or $ValueOnly) { $node.Remove('name') }
        if ($ValueOnly) { $node.value=$Label }
        $tree=[ordered]@{depth=8;interactive=$false;hideDisabled=$false;hideOffscreen=$false;windows=@(
            [ordered]@{hwnd=123;elementCount=1;elements=@($node);windowDpi=96;scale=1;dpiAwareness='per-monitor-aware';coordinateSpace='physical-screen-pixels'})}
        return ($tree|ConvertTo-Json -Depth 12 -Compress)
    }
    function New-NativeTreeChild([string]$Label,[switch]$OmitName) {
        $text=New-NativeTreeText $Label -OmitName:$OmitName
        $file=Join-Path $temporary ([guid]::NewGuid().ToString('N')+'.ps1')
        [IO.File]::WriteAllText($file,"[Console]::Out.WriteLine('"+$text.Replace("'","''")+"')")
        return (New-LiveStart -Executable $hostExecutable -Directory $temporary -Welcome -Arguments @('-NoProfile','-NonInteractive','-File',$file))
    }
    $r=Invoke-WinAppInspection (New-NativeTreeChild 'PRIVATE_NATIVE_SENTINEL') 123 {} -NativeOriginal
    Pass ($r.status -ceq 'observed' -and $r.reason -ceq 'native_original_tree_observed' -and $r.summary.requiredTargetStatus -ceq 'missing' -and $r.actions -eq 0 -and $r.attempts -eq 1 -and $r.processExitConfirmed) 'native tree observation is not a required-controls claim'
    Pass ($r.summary.Contains('loginAssessment') -and $r.summary.loginMarkerCount -eq 0 -and $r.summary.loginAssessment -ceq 'unknown-requires-visual-review') 'unmatched names never establish absence of login'
    Pass (($r|ConvertTo-Json -Depth 12 -Compress) -notmatch 'PRIVATE_NATIVE') 'raw UI names remain private'
    # Labels transcribed from the public 341298e1 screenshot, not captured UIA
    # output. The original three sign-in variants already match the regex.
    foreach ($label in @('Sign in to ChatGPT','Continue to sign in','Sign in another way')) {
        $s=ConvertTo-WinAppSummary (New-NativeTreeText $label) 123
        Pass ($s.loginMarkerCount -eq 1 -and $s.loginAssessment -ceq 'login-marker-observed') 'visible sign-in wording matches when exposed as a name'
        Pass (($s|ConvertTo-Json -Depth 12 -Compress) -notmatch [regex]::Escape($label)) 'observed label remains private'
    }
    $s=ConvertTo-WinAppSummary (New-NativeTreeText 'Sign up') 123
    Pass ($s.protectedMarkerCount -eq 1 -and $s.loginAssessment -ceq 'unknown-requires-visual-review') 'sign-up is a protected marker, not an authenticated-state inference'
    foreach ($text in @((New-NativeTreeText ''),(New-NativeTreeText 'Sign in to ChatGPT' -OmitName),
        (New-NativeTreeText 'Sign in to ChatGPT' -ValueOnly))) {
        $s=ConvertTo-WinAppSummary $text 123
        Pass ($s.loginMarkerCount -eq 0 -and $s.loginAssessment -ceq 'unknown-requires-visual-review') 'empty, omitted or value-only name leaves login unknown'
        Pass (($s|ConvertTo-Json -Depth 12 -Compress) -notmatch 'Sign in to ChatGPT') 'no raw value fallback is published'
    }
    $r=Invoke-WinAppInspection (New-NativeTreeChild 'Sign in to ChatGPT' -OmitName) 123 {} -NativeOriginal
    Pass ($r.status -ceq 'observed' -and $r.summary.loginAssessment -ceq 'unknown-requires-visual-review' -and $r.actions -eq 0 -and $r.attempts -eq 1 -and $r.processExitConfirmed) 'nameless tree only proves a read, with visual review still required'
    foreach($case in @(@('Sign in','login_marker_observed'),@('Sign in to ChatGPT','login_marker_observed'),
        @('Continue to sign in','login_marker_observed'),@('Sign in another way','login_marker_observed'),
        @('Sign up','protected_marker_observed'),@('Allow access','protected_marker_observed'),@('Something went wrong','error_marker_observed'))) {
        $r=Invoke-WinAppInspection (New-NativeTreeChild $case[0]) 123 {} -NativeOriginal
        Pass ($r.status -ceq 'stopped' -and $r.reason -ceq $case[1] -and $r.actions -eq 0 -and $r.attempts -eq 1 -and $r.processExitConfirmed) 'native login/permission/error stops without action'
    }
} finally { Remove-Item -LiteralPath $temporary -Recurse -Force }
Write-Output ('PASS: '+$count+' native-original safety assertions.')
