# Offline only: no Windows package download, provisioning, activation, or user data.
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-desktop-package.ps1')
$script:passed=0
function Pass([bool]$Condition,[string]$Name) {
    if (-not $Condition) { throw ('FAIL: '+$Name) };$script:passed++
}
function Reject([scriptblock]$Action,[string]$Name) {
    $rejected=$false;try { & $Action } catch { $rejected=$true };Pass $rejected $Name
}
function Parse([string]$Path) {
    $tokens=$null;$errors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile($Path,[ref]$tokens,[ref]$errors)
    Pass ($errors.Count -eq 0) ('syntax '+[IO.Path]::GetFileName($Path))
    return $ast
}
$source=Parse (Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1')
$copy=Parse (Join-Path $PSScriptRoot 'original-desktop-package-verification.ps1')
$runtime=Parse (Join-Path $PSScriptRoot 'original-desktop-package.ps1')
foreach ($fn in $copy.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst]},$false)) {
    $original=@($source.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq $fn.Name},$false))
    Pass ($original.Count -eq 1 -and $original[0].Extent.Text -ceq $fn.Extent.Text) ('unchanged reviewed verifier '+$fn.Name)
}
# The downloaded source is the exact reviewed release, not a moving latest alias.
$downloadCommands=@($runtime.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst] -and
    $n.GetCommandName() -ceq 'Invoke-WebRequest'},$true))
$downloadUrls=@($downloadCommands | ForEach-Object {
    $_.CommandElements | Where-Object {$_ -is [Management.Automation.Language.StringConstantExpressionAst] -and
        $_.Value.StartsWith('https://')} | ForEach-Object {$_.Value}
})
Pass ($downloadUrls.Count -eq 2 -and
    $downloadUrls[0] -ceq 'https://persistent.oaistatic.com/codex-app-prod/releases/26.1103.4239.0/ChatGPT-x64.msix' -and
    $downloadUrls[1] -ceq 'https://persistent.oaistatic.com/codex-app-prod/ChatGPT-License.xml') 'official download selects reviewed immutable package and unchanged official license'
# Exercise the exact shared bounded supervisor dependency without a Windows API.
$start=[Diagnostics.ProcessStartInfo]::new()
$start.FileName=(Get-Process -Id $PID).Path
$start.Arguments='-NoLogo -NoProfile -NonInteractive -Command "[Console]::Write(''entry-host-ready'')"'
$start.UseShellExecute=$false;$start.CreateNoWindow=$true
$start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
$bounded=Invoke-EntryBoundedHelper $start 5000
Pass ($bounded -ceq 'entry-host-ready') 'unchanged bounded helper initializes its own shared supervisor'
# Failure diagnostics survive before a context is returned, without raw errors.
foreach ($gate in @('runner','input-path')) {
    & {
        param($Gate)
        function Assert-OriginalDesktopPackageRunner { if ($Gate -ceq 'runner') { throw 'private-exception-canary' } }
        function Assert-PublicRegularPath { throw 'private-path-canary' }
        $diagnostic=[ordered]@{untrusted='private-seed-canary'}
        Reject { New-OriginalDesktopPackage -Work 'unused' -BridgeExecutable 'unused' -PreparationEvidence $diagnostic } ('preparation fault '+$Gate)
        Pass ($diagnostic.stage -ceq 'preflight' -and $diagnostic.status -ceq 'blocked' -and
            $diagnostic.cleanup -ceq 'not-needed' -and $diagnostic.Count -eq 5 -and
            $null -ne $diagnostic.failure -and $diagnostic.failure.category -ceq 'powershell') ('pre-context diagnostic retained '+$Gate)
        Pass (($diagnostic|ConvertTo-Json -Compress) -cnotmatch 'private|unused') ('pre-context diagnostic has no raw data '+$Gate)
    } $gate
}
$stageFunction=@($runtime.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and
    $n.Name -ceq 'Set-OriginalDesktopPreparationStage'},$false))[0]
$stages=@($stageFunction.FindAll({param($n) $n -is [Management.Automation.Language.StringConstantExpressionAst]},$true) |
    ForEach-Object {$_.Value} | Where-Object {$_ -cnotin @('Invalid preparation stage.','stage')})
$expectedStages=@('preflight','adapter-lock','package-download','license-download','package-pin',
    'package-path','license-path','package-open','package-digest','package-hash-match','license-open',
    'signature-verification','signed-package-identity','interactive-session','licensed-provisioning',
    'registration-readiness','installed-image-verification','activation-host-build','activation-host-self-test',
    'current-context-verification','freshness-scan','service-binding','prepared')
Pass (($stages -join ',') -ceq ($expectedStages -join ',')) 'diagnostic stages match exact reviewed allowlist'
foreach ($stage in $stages) {
    foreach ($cleanupFails in @($false,$true)) {
        & {
            param($Stage,$CleanupFails)
            function Close-OriginalDesktopPackage { if ($CleanupFails) { throw 'private-cleanup-canary' } }
            $diagnostic=[ordered]@{schema=1;status='incomplete';stage='preflight';cleanup='not-needed'}
            try { Set-OriginalDesktopPreparationStage $diagnostic $Stage;throw 'private-operation-canary' }
            catch { try { Complete-OriginalDesktopPreparationFailure $diagnostic @{} } catch { } }
            $expected='completed';if ($CleanupFails) {$expected='unconfirmed'}
            Pass ($diagnostic.stage -ceq $Stage -and $diagnostic.status -ceq 'blocked' -and $diagnostic.cleanup -ceq $expected) ('failed stage preserved '+$Stage+' cleanup '+$expected)
            Pass (($diagnostic|ConvertTo-Json -Compress) -cnotmatch 'private') ('fixed-only diagnostic '+$Stage)
        } $stage $cleanupFails
    }
}
Reject { Set-OriginalDesktopPreparationStage ([ordered]@{}) 'private-unknown-stage' } 'unknown diagnostic stage cannot leak'
# The retained report remains optional; neither stage reporting nor failure cleanup admits an operation.
Set-OriginalDesktopPreparationStage $null 'freshness-scan'
Pass $true 'optional diagnostics preserve existing freshness callers'
# Exception diagnostics expose only fixed classes and bounded numbers.
foreach ($exception in @([InvalidOperationException]::new('private-operation-canary'),
    [UnauthorizedAccessException]::new('private-path-canary'),[TimeoutException]::new('private-url-canary'),
    [Exception]::new('private-unknown-canary'))) {
    try { throw $exception } catch { $failure=Get-OriginalDesktopFailureEvidence $_ }
    Pass ($failure.Count -eq 5 -and $failure.webStatus -eq -1 -and $failure.hresult -is [int] -and $failure.httpStatus -eq 0 -and
        $failure.line -gt 0 -and ($failure|ConvertTo-Json -Compress) -cnotmatch 'private|Exception') 'exception report omits raw names and text'
}
Add-Type -TypeDefinition @'
using System.Net;
public sealed class OriginalDesktopFixtureResponse : WebResponse {
 public int StatusCode {get;set;}
}
'@
foreach ($http in @(200,403,429,500,999)) {
    $response=[OriginalDesktopFixtureResponse]::new();$response.StatusCode=$http
    $exception=[Net.WebException]::new('private-http-canary',$null,[Net.WebExceptionStatus]::ProtocolError,$response)
    try { throw [InvalidOperationException]::new('private-wrapper-canary',$exception) }
    catch { $failure=Get-OriginalDesktopFailureEvidence $_ }
    $expected=$http;if ($http -gt 599) {$expected=0}
    Pass ($failure.category -ceq 'web' -and $failure.webStatus -eq [int][Net.WebExceptionStatus]::ProtocolError -and $failure.httpStatus -eq $expected -and
        $failure.hresult -eq $exception.HResult -and ($failure|ConvertTo-Json -Compress) -cnotmatch 'private') ('bounded HTTP diagnostics '+$http)
}
foreach ($webStatus in @([Net.WebExceptionStatus]::TrustFailure,[Net.WebExceptionStatus]::Timeout,[Net.WebExceptionStatus]::ProtocolError)) {
    $exception=[Net.WebException]::new('private-transport-canary',$null,$webStatus,$null)
    try { throw $exception } catch { $failure=Get-OriginalDesktopFailureEvidence $_ }
    Pass ($failure.webStatus -eq [int]$webStatus -and $failure.httpStatus -eq 0 -and
        ($failure|ConvertTo-Json -Compress) -cnotmatch 'private|TrustFailure|Timeout|ProtocolError') 'defined WebException status is numeric only'
}
$undefined=[Enum]::ToObject([Net.WebExceptionStatus],999)
try { throw [Net.WebException]::new('private-invalid-canary',$null,$undefined,$null) }
catch { $failure=Get-OriginalDesktopFailureEvidence $_ }
Pass ($failure.webStatus -eq -1) 'undefined WebException status remains unknown'
$inner=[Net.WebException]::new('private-inner-canary',$null,[Net.WebExceptionStatus]::Timeout,$null)
try { throw [Net.WebException]::new('private-outer-canary',$inner,[Net.WebExceptionStatus]::TrustFailure,$null) }
catch { $failure=Get-OriginalDesktopFailureEvidence $_ }
Pass ($failure.webStatus -eq [int][Net.WebExceptionStatus]::TrustFailure) 'first bounded-chain WebException status wins'
# GetBaseException performs its standard traversal for category/HResult. Only
# optional HTTP/Web status extraction has the explicit eight-link bound.
$deep=[Net.WebException]::new('private-deep-canary',$null,[Net.WebExceptionStatus]::Timeout,$null)
for ($i=0;$i -lt 8;$i++) { $deep=[InvalidOperationException]::new('private-wrapper-canary',$deep) }
$record=[Management.Automation.ErrorRecord]::new($deep,'private-id-canary',[Management.Automation.ErrorCategory]::NotSpecified,$null)
$failure=Get-OriginalDesktopFailureEvidence $record
Pass ($failure.webStatus -eq -1 -and $failure.httpStatus -eq 0 -and $failure.category -ceq 'web') 'status extraction stops after eight exception links'
# Pure admission rejects every independently changed pin.
$identity=@{status='observed';packageSha256='9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c';
    manifest=@{name='OpenAI.Codex';publisher='CN=50BDFD77-8903-4850-9FFE-6E8522F64D5B';version='26.1103.4239.0'};
    backend=@{sha256='4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'};
    archive=@{sha256='fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa'}}
Assert-Desktop4239Identity $identity;Pass $true 'exact reviewed package accepted as static identity only'
foreach ($slot in @(@($identity,'status'),@($identity,'packageSha256'),@($identity.manifest,'version'),@($identity.backend,'sha256'),@($identity.archive,'sha256'))) {
    $before=$slot[0][$slot[1]];$slot[0][$slot[1]]='changed'
    Reject { Assert-Desktop4239Identity $identity } ('reject changed '+$slot[1]);$slot[0][$slot[1]]=$before
}
# Explicitly invalidate the opt-in even when this test runs in the admitted CI job.
$savedMarker=$env:MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE
try {
    $env:MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE='offline-test-invalid'
    Reject { Assert-OriginalDesktopPackageRunner } 'offline invocation cannot enter Windows preparation'
} finally { $env:MAGPIE_ORIGINAL_DESKTOP_ACCEPTANCE=$savedMarker }
# Even a valid 4239 option cannot expand the preexisting single-treatment guard.
Reject { Assert-EntryOptions 'restore-before-activation' 'bridge' $false `
 'breakingtime126/magpie/.github/workflows/experimental-desktop-native-entry-4239.yml@refs/heads/experiment/app-server-bridge-v0.1.3035' $true 'user-aumid-4239-v1' } '4239 treatment-only guard unchanged'
$app=[pscustomobject]@{Name='OpenAI.Codex';Publisher=$identity.manifest.publisher;Version='26.1103.4239.0';Status='Ok';Architecture='X64';
    PackageFullName='OpenAI.Codex_26.1103.4239.0_x64__fixture';PackageFamilyName='OpenAI.Codex_fixture';InstallLocation=[IO.Path]::GetFullPath($PSScriptRoot)}
Assert-OriginalDesktopRegistration $app $identity;Pass $true 'registration matches exact installed identity'
foreach ($field in @('Name','Publisher','Version','Status','Architecture','PackageFullName','PackageFamilyName','InstallLocation')) {
    $before=$app.$field;$app.$field='changed'
    Reject { Assert-OriginalDesktopRegistration $app $identity } ('registration rejects '+$field);$app.$field=$before
}
$prior=$app.PSObject.Copy();$prior.PackageFamilyName='OpenAI.Codex_other'
Reject { Assert-OriginalDesktopRegistration $app $identity $prior } 'registration cannot change after preparation'
$held=[pscustomobject]@{Alive=$true;CreatedUtcTicks=[long]200;ImagePath='/fixture/ChatGPT.exe';Relation='expected'}
$held|Add-Member ScriptMethod PackageRelation {param($Package) return $this.Relation}
Assert-OriginalDesktopActivationBinding $held 200 100 'package' '/fixture/ChatGPT.exe';Pass $true 'held activation binding admits exact generation'
Reject { Assert-OriginalDesktopActivationBinding $held 199 100 'package' '/fixture/ChatGPT.exe' } 'PID reuse generation mismatch'
Reject { Assert-OriginalDesktopActivationBinding $held 200 201 'package' '/fixture/ChatGPT.exe' } 'old process cannot become owned activation'
Reject { Assert-OriginalDesktopActivationBinding $held 200 100 'package' '/fixture/other.exe' } 'different image cannot become owned activation'
$held.Relation='other';Reject { Assert-OriginalDesktopActivationBinding $held 200 100 'package' '/fixture/ChatGPT.exe' } 'different package cannot become owned activation'
$held.Relation='expected';$held.Alive=$false
Reject { Assert-OriginalDesktopActivationBinding $held 200 100 'package' '/fixture/ChatGPT.exe' } 'exited activation is not accepted'
# Image evidence distinguishes a verified direct child from a merely matching name/path.
$bridgePath=Join-Path $PSScriptRoot 'original-desktop-package.tests.ps1'
$nativePath=Join-Path $PSScriptRoot 'original-desktop-package-verification.ps1'
$observer=[pscustomobject]@{Images=@(
    [pscustomobject]@{ParentId=11;CreatedTicks=300;ParentCreatedTicks=200;Path=$bridgePath},
    [pscustomobject]@{ParentId=11;CreatedTicks=301;ParentCreatedTicks=200;Path=$nativePath},
    [pscustomobject]@{ParentId=12;CreatedTicks=302;ParentCreatedTicks=200;Path=$bridgePath},
    [pscustomobject]@{ParentId=11;CreatedTicks=303;ParentCreatedTicks=199;Path=$bridgePath});Misses=1;Full=$false}
$observer|Add-Member ScriptMethod Snapshot { return $this.Images }
$imageContext=@{ImageObserver=$observer;Evidence=@{activated=$true};ActivationProcess=@{Id=11};ActivationFloor=250;ActivationCreatedTicks=200;
    BridgeExecutable=$bridgePath;NativeExecutable=$nativePath;
    BridgeHash=(Get-FileHash -Algorithm SHA256 -LiteralPath $bridgePath).Hash.ToLowerInvariant();
    Identity=@{backend=@{sha256=(Get-FileHash -Algorithm SHA256 -LiteralPath $nativePath).Hash.ToLowerInvariant()}}}
$images=Get-OriginalDesktopImageEvidence $imageContext
Pass ($images.adapterDirectChildren -eq 1 -and $images.nativeDirectChildren -eq 1 -and $images.misses -eq 1 -and
    -not $images.backendHandshakeVerified) 'image snapshot verifies parent generation but never claims handshake'
$imageContext.BridgeHash='0'*64
Pass ((Get-OriginalDesktopImageEvidence $imageContext).adapterDirectChildren -eq 0) 'adapter image hash mismatch is not observed startup'
$imageContext.Evidence.activated=$false
Pass ((Get-OriginalDesktopImageEvidence $imageContext).status -ceq 'unavailable') 'unverified Desktop activation cannot bind image evidence'
# Current package revalidation hashes retained bytes and rejects registration drift.
function Assert-OriginalDesktopPackageRunner { }
function Assert-OriginalDesktopInteractive { }
function Get-AppxPackage { return $script:registeredFixture }
function Test-EntryProcessOverride { return $false }
function Test-EntryMachineOverride { return $false }
$script:registeredFixture=$app
$bytes=[Text.Encoding]::UTF8.GetBytes('retained fixture bytes')
$stream=[IO.MemoryStream]::new($bytes)
$hash=Get-BoundedStreamDigest $stream $bytes.Length 100 ([Diagnostics.Stopwatch]::StartNew())
$current=@{Closed=$false;Prepared=$true;Identity=$identity;App=$app;Service=$null;
    Files=@([pscustomobject]@{Stream=$stream;Hash=$hash;Bytes=$bytes.Length});
    ActivationHost=@{Stream=$stream;Sha256=$hash}}
Assert-OriginalDesktopPackageCurrent $current;Pass $true 'current check rehashes retained package and host'
$current.Files[0].Hash='0'*64
Reject { Assert-OriginalDesktopPackageCurrent $current } 'changed retained package bytes reject activation'
$current.Files[0].Hash=$hash;$current.ActivationHost.Sha256='0'*64
Reject { Assert-OriginalDesktopPackageCurrent $current } 'changed retained activation host rejects activation'
$current.ActivationHost.Sha256=$hash;$current.Closed=$true
Reject { Assert-OriginalDesktopPackageCurrent $current } 'closed context cannot activate'
$current.Closed=$false;$current.Prepared=$false
Reject { Assert-OriginalDesktopPackageCurrent $current } 'incomplete preparation cannot activate'
$current.Prepared=$true;$script:registeredFixture=$prior
Reject { Assert-OriginalDesktopPackageCurrent $current } 'registration drift cannot activate'
$stream.Dispose()
# A failed observer close must not strand the remaining retained file locks.
$bad=[pscustomobject]@{};$bad|Add-Member ScriptMethod Dispose { throw 'synthetic observer close failure' }
$closeStream=[IO.MemoryStream]::new([byte[]]@(1,2,3))
$closeContext=@{Closed=$false;ImageObserver=$bad;ActivationHeld=$null;ActivationProcess=$null;Service=$null;ActivationHost=$null;
    Streams=[Collections.Generic.List[IO.Stream]]::new()}
$closeContext.Streams.Add($closeStream)
Reject { Close-OriginalDesktopPackage $closeContext } 'closure failure stays a blocker'
Pass ($closeContext.Closed -and -not $closeStream.CanRead) 'remaining retained resources close despite observer failure'
# Verify activation ordering and failed API attempt consumption without calling any API.
function Assert-OriginalDesktopPackageCurrent($Context) { $script:sequence+='pins';if ($script:blockPins) {throw 'pins'} }
function Assert-OriginalDesktopPackageFresh($Context) { $script:sequence+='fresh';if ($script:blockFresh) {throw 'fresh'} }
function Assert-EntryVerifiedServiceCurrent($Service) { $script:sequence+='service' }
function New-OriginalDesktopImageObserver($Context) { $script:sequence+='observer';return $null }
function Invoke-EntryActivation($Executable,$Aumid) { $script:sequence+='activate';throw 'synthetic uncertain API outcome' }
foreach ($case in @('pins','fresh','api')) {
    $context=@{ActivationAttempted=$false;Service=@{};ActivationHost=@{Path='unused'};Aumid='unused'}
    $script:sequence=@();$script:blockPins=$case -ceq 'pins';$script:blockFresh=$case -ceq 'fresh'
    Reject { Invoke-OriginalDesktopPackageActivation $context } ('activation blocks at '+$case)
    if ($case -ceq 'pins') { Pass (($script:sequence -join ',') -ceq 'pins' -and -not $context.ActivationAttempted) 'pin rejection never activates' }
    if ($case -ceq 'fresh') { Pass (($script:sequence -join ',') -ceq 'pins,fresh' -and -not $context.ActivationAttempted) 'freshness rejection never activates' }
    if ($case -ceq 'api') {
        Pass (($script:sequence -join ',') -ceq 'pins,fresh,service,observer,activate' -and $context.ActivationAttempted) 'verified guards precede one API attempt'
        $script:sequence=@();Reject { Invoke-OriginalDesktopPackageActivation $context } 'uncertain activation is never retried'
        Pass ($script:sequence.Count -eq 0) 'retry stops before any other action'
    }
}
# No existing harness or override writer is executable from the new helper.
$commands=@($runtime.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst]},$true) | ForEach-Object { $_.GetCommandName() })
foreach ($forbidden in @('Set-EntryOverrides','Write-EntryUserEnvironment','Restore-EntryOverrides','Set-ExecutionPolicy','Add-AppxPackage','Start-Process')) {
    Pass ($commands -cnotcontains $forbidden) ('no forbidden fallback '+$forbidden)
}
Pass (-not $runtime.Extent.Text.Contains('test-desktop-cold-start.ps1')) 'monolithic cold start is never executed or imported'
Write-Output ('PASS: '+$script:passed+' official Desktop package offline checks; Windows runtime not exercised.')
