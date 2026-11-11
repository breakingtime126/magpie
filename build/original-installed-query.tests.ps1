[CmdletBinding()]
param([string]$Source='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if ($Source -ceq '') { $Source=Join-Path $PSScriptRoot '../internal/nativepreview/discovery_windows.go' }
function Assert([bool]$Value,[string]$Message) { if (-not $Value) { throw ('FAIL: '+$Message) } }
function Refuses([scriptblock]$Action,[string]$Message) {
    $refused=$false
    try { $null=& $Action } catch { $refused=$true }
    Assert $refused $Message
}
$sourceText=[IO.File]::ReadAllText($Source)
$marker='const installedQuery = `'
$begin=$sourceText.IndexOf($marker,[StringComparison]::Ordinal)+$marker.Length
$end=$sourceText.IndexOf('`',$begin,[StringComparison]::Ordinal)
Assert ($begin -ge $marker.Length -and $end -gt $begin) 'embedded query found'
$query=$sourceText.Substring($begin,$end-$begin)
$tokens=$null;$parseErrors=$null
$ast=[Management.Automation.Language.Parser]::ParseInput($query,[ref]$tokens,[ref]$parseErrors)
Assert ($parseErrors.Count -eq 0) 'query parses'
$definitions=@($ast.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst]},$true))
Assert ($definitions.Count -eq 2) 'only fixed query helpers'
foreach ($definition in $definitions) { . ([scriptblock]::Create($definition.Extent.Text)) }
# Real official module metadata only; neither helper executes an exported query.
$management=Import-QueryModule 'Microsoft.PowerShell.Management'
$utility=Import-QueryModule 'Microsoft.PowerShell.Utility'
$joinPath=Get-QueryCommand $management 'Join-Path'
$testPath=Get-QueryCommand $management 'Test-Path'
$toJSON=Get-QueryCommand $utility 'ConvertTo-Json'
Assert ($joinPath -is [Management.Automation.CmdletInfo] -and $testPath -is [Management.Automation.CmdletInfo] -and $toJSON -is [Management.Automation.CmdletInfo]) 'actual exported cmdlets bound'
Refuses { Import-QueryModule 'UnlistedFixture' } 'unlisted module rejected'
Refuses { Get-QueryCommand $management 'Get-AppxPackage' } 'missing export rejected'
Refuses { Get-QueryCommand ([pscustomobject]@{ExportedCommands=@{'Join-Path'=$testPath}}) 'Join-Path' } 'wrong command name rejected'
function Fixture-Function {}
$functionInfo=Microsoft.PowerShell.Core\Get-Command Fixture-Function -CommandType Function
Refuses { Get-QueryCommand ([pscustomobject]@{ExportedCommands=@{'Fixture-Function'=$functionInfo}}) 'Fixture-Function' } 'function export rejected'
# Exercise module admission with authored replacements for just the import call.
$importDefinition=($definitions | Microsoft.PowerShell.Core\Where-Object Name -CEQ 'Import-QueryModule').Extent.Text
$importCall='Microsoft.PowerShell.Core\Import-Module -Name $manifest -PassThru -ErrorAction Stop'
Assert ($importDefinition.Contains($importCall)) 'fixed exact manifest import'
function Get-FixtureModule { return $script:moduleFixture }
$fakeImport=$importDefinition.Replace($importCall,'Get-FixtureModule').Replace('function Import-QueryModule','function Import-FixtureModule')
. ([scriptblock]::Create($fakeImport))
$script:moduleFixture=$management
Assert ((Import-FixtureModule 'Microsoft.PowerShell.Management').Name -ceq 'Microsoft.PowerShell.Management') 'matching imported root accepted'
$script:moduleFixture=$utility
Refuses { Import-FixtureModule 'Microsoft.PowerShell.Management' } 'wrong imported name rejected'
$script:moduleFixture=@($management,$management)
Refuses { Import-FixtureModule 'Microsoft.PowerShell.Management' } 'multiple import results rejected'
$script:moduleFixture=[pscustomobject]@{Name='Microsoft.PowerShell.Management';ModuleBase=$management.ModuleBase}
Refuses { Import-FixtureModule 'Microsoft.PowerShell.Management' } 'untyped module rejected'
$script:moduleFixture=$management
$wrongRoot=$fakeImport.Replace('[IO.Path]::GetFullPath($loaded[0].ModuleBase)','[IO.Path]::GetFullPath([IO.Path]::Combine($PSHOME,''wrong-root''))')
. ([scriptblock]::Create($wrongRoot))
Refuses { Import-FixtureModule 'Microsoft.PowerShell.Management' } 'wrong imported base rejected'
$wrongManifest=$fakeImport.Replace('[IO.Path]::GetFullPath($loaded[0].Path)','[IO.Path]::GetFullPath([IO.Path]::Combine($PSHOME,''wrong.psd1''))')
. ([scriptblock]::Create($wrongManifest))
Refuses { Import-FixtureModule 'Microsoft.PowerShell.Management' } 'wrong imported manifest rejected with matching name and base'

# Query body uses authored package objects only; no Appx cmdlet is invoked.
$tail=$query.Substring($query.IndexOf('$p=@(',[StringComparison]::Ordinal))
Assert ($tail.Contains('if($p.Count -ne 1){exit 2}') -and $tail.Contains('if(-not (& $testPath -LiteralPath $f -PathType Leaf)){exit 3}')) 'count and literal-leaf exits unchanged'
$body=[scriptblock]::Create($tail.Replace('exit 2','throw ''fixture-exit-two''').Replace('exit 3','throw ''fixture-exit-three'''))
$fixtureRoot=Join-Path ([IO.Path]::GetTempPath()) ('bound-query-fixture-'+[guid]::NewGuid().ToString('N'))
try {
    $resources=[IO.Path]::Combine($fixtureRoot,'app','resources')
    [void][IO.Directory]::CreateDirectory($resources)
    [IO.File]::WriteAllText([IO.Path]::Combine($resources,'codex.exe'),'inert, never executed')
    $good=[pscustomobject]@{PackageFamilyName='OpenAI.Codex_2p2nqsd0c76g0';SignatureKind='Store';Status='Ok';InstallLocation=$fixtureRoot}
    $script:packageFixture=@($good)
    $getPackage={param($Name,$ErrorAction) if ($Name -cne 'OpenAI.Codex' -or $ErrorAction -cne 'Stop') { throw 'wrong query arguments' };return $script:packageFixture}
    $result=& {
        function Get-AppxPackage { throw 'unexpected package name lookup' }
        function Join-Path { throw 'unexpected join name lookup' }
        function Test-Path { throw 'unexpected test name lookup' }
        function ConvertTo-Json { throw 'unexpected JSON name lookup' }
        & $body
    }
    $parsed=$result|Microsoft.PowerShell.Utility\ConvertFrom-Json
    Assert ($parsed.path -ceq (& $joinPath $fixtureRoot 'app\resources\codex.exe') -and $parsed.family -ceq $good.PackageFamilyName -and $parsed.signatureKind -ceq 'Store') 'same success JSON'
    Assert (@($parsed.PSObject.Properties).Count -eq 3) 'only original JSON fields'
    foreach ($kind in @('empty','duplicate','family','signature','status','missing-leaf')) {
        $entry=[pscustomobject]@{PackageFamilyName=$good.PackageFamilyName;SignatureKind='Store';Status='Ok';InstallLocation=$fixtureRoot}
        switch ($kind) {
            empty { $script:packageFixture=@() }
            duplicate { $script:packageFixture=@($good,$good) }
            family { $entry.PackageFamilyName='openai.codex_2p2nqsd0c76g0';$script:packageFixture=@($entry) }
            signature { $entry.SignatureKind='store';$script:packageFixture=@($entry) }
            status { $entry.Status='ok';$script:packageFixture=@($entry) }
            missing-leaf { $entry.InstallLocation=[IO.Path]::Combine($fixtureRoot,'missing');$script:packageFixture=@($entry) }
        }
        $expected=if ($kind -ceq 'missing-leaf') {'fixture-exit-three'} else {'fixture-exit-two'}
        $caught='';try { $null=& $body } catch { $caught=$_.Exception.Message }
        Assert ($caught -ceq $expected) ('same refusal '+$kind)
    }
} finally { if ([IO.Directory]::Exists($fixtureRoot)) { [IO.Directory]::Delete($fixtureRoot,$true) } }
# No ordinary command name lookup remains for non-Core commands.
foreach ($binding in @('& $getPackage','& $joinPath','& $testPath','& $toJSON')) { Assert ($query.Contains($binding)) 'bound object invocation' }
Assert ($query.Contains('Microsoft.PowerShell.Core\Where-Object') -and -not $query.Contains('Get-Command')) 'no command discovery'
Assert ($query -notmatch 'PSModulePath|ExecutionPolicy|LanguageMode|WaitDelay|Set-ExecutionPolicy|Invoke-Expression') 'no environment policy or drain workaround'
Assert ($query -notmatch 'commandInfo\.ModuleBase') 'nested binary command base is not manifest root'
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-codex-connectivity-preview.yml'))
Assert ($workflow.Contains('& $hostName -NoProfile -NonInteractive -File build/original-installed-query.tests.ps1') -and
    $workflow.IndexOf('build/original-installed-query.tests.ps1') -lt $workflow.IndexOf('Build original portable GUI')) 'both-host binding fixtures precede live work'

if ($PSVersionTable.PSEdition -ceq 'Desktop') {
    $appx=Import-QueryModule 'Appx'
    $appxCommand=Get-QueryCommand $appx 'Get-AppxPackage'
    Assert ($appxCommand -is [Management.Automation.CmdletInfo]) 'official Windows Appx manifest export'
    Write-Output 'PASS official Windows PowerShell fixed manifest/export metadata (no package query)'
} else { Write-Output 'NOT RUN Windows PowerShell Appx manifest metadata gate (non-Desktop host)' }
Write-Output 'PASS fixed module/command binding and authored package query semantics'
