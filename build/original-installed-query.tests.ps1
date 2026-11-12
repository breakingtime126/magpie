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
# Fixed markers are the only query-body delta. Removing their exact complete
# source lines must recover the previously reviewed query byte-for-byte.
$progressNames=@('entry','appx-import','appx-return','setup','appx','management','utility','packages','path','json')
$progressLines=@($query -split "`n" | Where-Object { $_.Contains('MAGPIE_QUERY_V1') })
Assert ($progressLines.Count -eq $progressNames.Count) 'only ten fixed progress markers'
for ($i=0;$i -lt $progressNames.Count;$i++) {
    $expected="[Console]::Error.WriteLine('MAGPIE_QUERY_V1:"+$progressNames[$i]+"')"
    if ($progressNames[$i] -cin @('appx-import','appx-return')) { $expected="    if (`$moduleName -ceq 'Appx') { "+$expected+" }" }
    Assert ($progressLines[$i] -ceq $expected) 'ordered fixed stderr marker literals'
}
Assert ($query.Contains("}`n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:setup')`n`$appxModule=Import-QueryModule 'Appx'")) 'setup completes before Appx helper invocation'
$importBoundary="    if (`$moduleName -ceq 'Appx') { [Console]::Error.WriteLine('MAGPIE_QUERY_V1:appx-import') }`n"+
    '    $loaded=@(Microsoft.PowerShell.Core\Import-Module -Name $manifest -PassThru -ErrorAction Stop)'+"`n"+
    "    if (`$moduleName -ceq 'Appx') { [Console]::Error.WriteLine('MAGPIE_QUERY_V1:appx-return') }`n"+
    '    if ($loaded.Count -ne 1'
Assert ($query.Contains($importBoundary)) 'Appx markers immediately surround actual import before validation'
$withoutProgress=($query -split "`n" | Where-Object { -not $_.Contains('MAGPIE_QUERY_V1') }) -join "`n"
$sha=[Security.Cryptography.SHA256]::Create()
try { $originalHash=([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($withoutProgress)))).Replace('-','').ToLowerInvariant() }
finally { $sha.Dispose() }
Assert ($originalHash -ceq '22d3baf0d80dd8d5b7446e88414afa7d929c1b70dcfb89f98d943a9cc6c3140b') 'query semantics unchanged except fixed marker lines'
Assert ($query.StartsWith($progressLines[0]+"`n",[StringComparison]::Ordinal)) 'entry marker precedes script work'
foreach ($boundary in @(
    @('$appxModule=Import-QueryModule ''Appx''','appx'),
    @('$managementModule=Import-QueryModule ''Microsoft.PowerShell.Management''','management'),
    @('$utilityModule=Import-QueryModule ''Microsoft.PowerShell.Utility''','utility'),
    @('if(-not (& $testPath -LiteralPath $f -PathType Leaf)){exit 3}','path'),
    @('@{path=$f;family=[string]$p[0].PackageFamilyName;signatureKind=[string]$p[0].SignatureKind}|& $toJSON -Compress','json')
)) {
    Assert ($query.Contains($boundary[0]+"`n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:"+$boundary[1]+"')")) 'progress follows completed operation'
}
Assert ($query.Contains("`n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:packages')`nif(`$p.Count -ne 1){exit 2}")) 'package marker follows enumeration before count admission'
$tokens=$null;$parseErrors=$null
$ast=[Management.Automation.Language.Parser]::ParseInput($query,[ref]$tokens,[ref]$parseErrors)
Assert ($parseErrors.Count -eq 0) 'query parses'
$definitions=@($ast.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst]},$true))
Assert ($definitions.Count -eq 2) 'only fixed query helpers'
foreach ($definition in $definitions) { . ([scriptblock]::Create($definition.Extent.Text)) }
# Test-only observations, never admission criteria. No metadata values are printed.
function Get-FixedModuleMetadata([string]$Label,[object[]]$Modules) {
    $moduleName='';$assemblyName='';$commands=@()
    switch -CaseSensitive ($Label) {
        management { $moduleName='Microsoft.PowerShell.Management';$assemblyName='Microsoft.PowerShell.Commands.Management';$commands=@('Join-Path','Test-Path') }
        utility { $moduleName='Microsoft.PowerShell.Utility';$assemblyName='Microsoft.PowerShell.Commands.Utility';$commands=@('ConvertTo-Json') }
        appx { $moduleName='Appx';$assemblyName='Microsoft.Windows.Appx.PackageManager.Commands';$commands=@('Get-AppxPackage') }
        default { throw 'invalid fixed metadata label' }
    }
    $count='zero';if ($Modules.Count -eq 1) {$count='one'} elseif ($Modules.Count -gt 1) {$count='multiple'}
    $type='unknown';$name='unknown';$path='unknown';$base='unknown';$exports='unknown';$assembly='unknown';$assemblyPath='unknown'
    if ($Modules.Count -eq 1) {
        $entry=$Modules[0]
        $type='other'
        if ($entry -is [Management.Automation.PSModuleInfo]) {
            $type='module';$name=if ($entry.Name -ceq $moduleName) {'expected'} else {'other'}
            try {
                $moduleRoot=[IO.Path]::Combine($PSHOME,'Modules',$moduleName)
                $manifest=[IO.Path]::Combine($moduleRoot,$moduleName+'.psd1')
                $binary=[IO.Path]::Combine($PSHOME,$assemblyName+'.dll')
                $moduleBinary=[IO.Path]::Combine($moduleRoot,$assemblyName+'.dll')
                $pathValue=$entry.Path
                $path='other'
                if ([string]::IsNullOrEmpty($pathValue)) {$path='empty'}
                elseif ([string]::Equals($pathValue,$assemblyName,[StringComparison]::OrdinalIgnoreCase) -or [string]::Equals($pathValue,$assemblyName+'.dll',[StringComparison]::OrdinalIgnoreCase)) {$path='bare-known-assembly'}
                elseif ([IO.Path]::IsPathRooted($pathValue)) {
                    if ([string]::Equals([IO.Path]::GetFullPath($pathValue),$manifest,[StringComparison]::OrdinalIgnoreCase)) {$path='manifest'}
                    elseif ([string]::Equals([IO.Path]::GetFullPath($pathValue),$binary,[StringComparison]::OrdinalIgnoreCase)) {$path='pshome-binary'}
                    elseif ([string]::Equals([IO.Path]::GetFullPath($pathValue),$moduleBinary,[StringComparison]::OrdinalIgnoreCase)) {$path='module-binary'}
                }
                $base='other'
                if ([string]::IsNullOrEmpty($entry.ModuleBase)) {$base='empty'}
                elseif ([string]::Equals([IO.Path]::GetFullPath($entry.ModuleBase),$moduleRoot,[StringComparison]::OrdinalIgnoreCase)) {$base='module-root'}
                elseif ([string]::Equals([IO.Path]::GetFullPath($entry.ModuleBase),$PSHOME,[StringComparison]::OrdinalIgnoreCase)) {$base='pshome'}
                $exports='expected';$assembly='expected';$assemblyPath='match'
                foreach ($commandName in $commands) {
                    $command=$entry.ExportedCommands[$commandName]
                    if ($command -isnot [Management.Automation.CmdletInfo] -or $command.Name -cne $commandName -or $command.CommandType -ne [Management.Automation.CommandTypes]::Cmdlet) {
                        $exports='other';$assembly='unknown';$assemblyPath='unknown';break
                    }
                    $commandAssembly=$command.ImplementingType.Assembly
                    if ($commandAssembly.GetName().Name -cne $assemblyName) {$assembly='other'}
                    if ([string]::IsNullOrEmpty($pathValue) -or -not [IO.Path]::IsPathRooted($pathValue) -or -not [string]::Equals([IO.Path]::GetFullPath($pathValue),$commandAssembly.Location,[StringComparison]::OrdinalIgnoreCase)) {$assemblyPath='different'}
                }
            } catch { $path='unknown';$base='unknown';$exports='unknown';$assembly='unknown';$assemblyPath='unknown' }
        }
    }
    return "fixed-module-metadata-v1 module=$Label count=$count type=$type name=$name path=$path base=$base exports=$exports assembly-name=$assembly export-assembly-path=$assemblyPath"
}
# Bound schema/redaction fixtures run on both hosts before actual metadata imports.
$emptyMetadata=Get-FixedModuleMetadata 'management' @()
Assert ($emptyMetadata -ceq 'fixed-module-metadata-v1 module=management count=zero type=unknown name=unknown path=unknown base=unknown exports=unknown assembly-name=unknown export-assembly-path=unknown') 'empty metadata stays unknown'
$untypedMetadata=Get-FixedModuleMetadata 'utility' @([pscustomobject]@{Path='PRIVATE-SENTINEL';Name='PRIVATE-SENTINEL'})
Assert ($untypedMetadata -notmatch 'PRIVATE-SENTINEL' -and $untypedMetadata.Contains('type=other name=unknown')) 'untyped metadata never exposes arbitrary fields'
# All fixed imports are observed before the unchanged strict helper can fail.
$metadataSpecs=@(@('management','Microsoft.PowerShell.Management'),@('utility','Microsoft.PowerShell.Utility'))
if ($PSVersionTable.PSEdition -ceq 'Desktop') {$metadataSpecs+=,@('appx','Appx')}
foreach ($spec in $metadataSpecs) {
    try {
        $metadataManifest=[IO.Path]::Combine($PSHOME,'Modules',$spec[1],$spec[1]+'.psd1')
        $metadataModules=@(Microsoft.PowerShell.Core\Import-Module -Name $metadataManifest -PassThru -ErrorAction Stop 3>$null 4>$null 5>$null 6>$null)
        Write-Output (Get-FixedModuleMetadata $spec[0] $metadataModules)
    } catch { Write-Output ('fixed-module-metadata-v1 module='+$spec[0]+' import=unavailable') }
}
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
# Authored import failure distinguishes call entry from return. Validation failure
# after a returned untyped value must still emit return, never appx completion.
$savedImportError=[Console]::Error
$importCapture=[IO.StringWriter]::new()
try {
    [Console]::SetError($importCapture)
    foreach ($case in @('throw','invalid','management')) {
        [void]$importCapture.GetStringBuilder().Clear()
        $script:moduleFixture=[pscustomobject]@{Name='Appx'}
        $fixtureDefinition=$fakeImport
        if ($case -ceq 'throw') { $fixtureDefinition=$fakeImport.Replace('Get-FixtureModule','Throw-FixtureImport') }
        function Throw-FixtureImport { throw 'authored import failure' }
        . ([scriptblock]::Create($fixtureDefinition))
        $module=if ($case -ceq 'management') {'Microsoft.PowerShell.Management'} else {'Appx'}
        Refuses { Import-FixtureModule $module } 'authored import or validation failure remains refused'
        $expected=''
        if ($case -cne 'management') { $expected='MAGPIE_QUERY_V1:appx-import'+[Environment]::NewLine }
        if ($case -ceq 'invalid') { $expected+='MAGPIE_QUERY_V1:appx-return'+[Environment]::NewLine }
        Assert ($importCapture.ToString() -ceq $expected) 'Appx import markers distinguish throw from return without other module markers'
    }
} finally { [Console]::SetError($savedImportError);$importCapture.Dispose() }
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

# Exercise the Appx-only binary identity using an actual typed module and
# CmdletInfo, substituting only unavailable Appx metadata with authored values.
$appxFixtureImport=$fakeImport.Replace('$loaded[0].Name','$script:appxFixtureName').Replace('$loaded[0].ModuleBase','$script:appxFixtureBase').Replace('$loaded[0].Path','$script:appxFixturePath').Replace('Get-QueryCommand $loaded[0] ''Get-AppxPackage''', 'Get-QueryCommand $script:appxFixtureExports ''Get-AppxPackage''').Replace('$appxCommand.ImplementingType.Assembly','$script:appxFixtureAssembly')
. ([scriptblock]::Create($appxFixtureImport))
$appxFixtureCommand=[Management.Automation.CmdletInfo]::new('Get-AppxPackage',$joinPath.ImplementingType)
$appxCases=@('valid','wrong-name','wrong-base','missing-path','relative-path','wrong-path','missing-assembly','wrong-assembly-name','missing-location','relative-location','wrong-location','missing-export','wrong-export','both-relative')
if ([IO.Path]::DirectorySeparatorChar -eq '\') {$appxCases+=@('drive-relative','root-relative')}
$savedAppxError=[Console]::Error
$appxCapture=[IO.StringWriter]::new()
try {
    [Console]::SetError($appxCapture)
foreach ($case in $appxCases) {
    [void]$appxCapture.GetStringBuilder().Clear()
    $script:appxFixtureName='Appx'
    $script:appxFixtureBase=[IO.Path]::Combine($PSHOME,'Modules','Appx')
    $script:appxFixturePath=[IO.Path]::Combine($PSHOME,'authored-assembly.dll')
    $script:appxFixtureAssembly=[pscustomobject]@{Location=$script:appxFixturePath;SimpleName='Microsoft.Windows.Appx.PackageManager.Commands'}
    $script:appxFixtureAssembly|Add-Member -MemberType ScriptMethod -Name GetName -Value { return [pscustomobject]@{Name=$this.SimpleName} }
    $script:appxFixtureExports=[pscustomobject]@{ExportedCommands=@{'Get-AppxPackage'=$appxFixtureCommand}}
    switch ($case) {
        both-relative {$script:appxFixturePath='authored-assembly.dll';$script:appxFixtureAssembly.Location=$script:appxFixturePath}
        drive-relative {$script:appxFixturePath='C:authored-assembly.dll';$script:appxFixtureAssembly.Location=$script:appxFixturePath}
        root-relative {$script:appxFixturePath='\authored-assembly.dll';$script:appxFixtureAssembly.Location=$script:appxFixturePath}
        wrong-name {$script:appxFixtureName='Other'}
        wrong-base {$script:appxFixtureBase=$PSHOME}
        missing-path {$script:appxFixturePath=''}
        relative-path {$script:appxFixturePath='authored-assembly.dll'}
        wrong-path {$script:appxFixturePath=[IO.Path]::Combine($PSHOME,'different.dll')}
        missing-assembly {$script:appxFixtureAssembly=$null}
        wrong-assembly-name {$script:appxFixtureAssembly.SimpleName='Other'}
        missing-location {$script:appxFixtureAssembly.Location=''}
        relative-location {$script:appxFixtureAssembly.Location='authored-assembly.dll'}
        wrong-location {$script:appxFixtureAssembly.Location=[IO.Path]::Combine($PSHOME,'different.dll')}
        missing-export {$script:appxFixtureExports.ExportedCommands=@{}}
        wrong-export {$script:appxFixtureExports.ExportedCommands=@{'Get-AppxPackage'=$joinPath}}
    }
    if ($case -ceq 'valid') { Assert ($null -ne (Import-FixtureModule 'Appx')) 'matching Appx binary identity accepted' }
    else { Refuses { Import-FixtureModule 'Appx' } ('Appx identity refuses '+$case) }
    Assert ($appxCapture.ToString() -ceq ('MAGPIE_QUERY_V1:appx-import'+[Environment]::NewLine+'MAGPIE_QUERY_V1:appx-return'+[Environment]::NewLine)) 'returned import marker precedes every identity verdict'
}
} finally { [Console]::SetError($savedAppxError);$appxCapture.Dispose() }

# Query body uses authored package objects only; no Appx cmdlet is invoked.
$tail=$query.Substring($query.IndexOf('$p=@(',[StringComparison]::Ordinal))
Assert ($tail.Contains('if($p.Count -ne 1){exit 2}') -and $tail.Contains('if(-not (& $testPath -LiteralPath $f -PathType Leaf)){exit 3}')) 'count and literal-leaf exits unchanged'
$body=[scriptblock]::Create($tail.Replace('exit 2','throw ''fixture-exit-two''').Replace('exit 3','throw ''fixture-exit-three'''))
$fixtureRoot=Join-Path ([IO.Path]::GetTempPath()) ('bound-query-fixture-'+[guid]::NewGuid().ToString('N'))
$savedConsoleError=[Console]::Error
$progressCapture=[IO.StringWriter]::new()
try {
    [Console]::SetError($progressCapture)
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
    Assert ($progressCapture.ToString() -ceq ('MAGPIE_QUERY_V1:packages'+[Environment]::NewLine+'MAGPIE_QUERY_V1:path'+[Environment]::NewLine+'MAGPIE_QUERY_V1:json'+[Environment]::NewLine)) 'success emits only fixed completed boundaries to stderr'
    Assert (@($result).Count -eq 1 -and -not ([string]$result).Contains('MAGPIE_QUERY_V1')) 'progress never pollutes JSON stdout'
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
        [void]$progressCapture.GetStringBuilder().Clear()
        $caught='';try { $null=& $body } catch { $caught=$_.Exception.Message }
        Assert ($caught -ceq $expected) ('same refusal '+$kind)
        Assert ($progressCapture.ToString() -ceq ('MAGPIE_QUERY_V1:packages'+[Environment]::NewLine)) 'refusal does not invent path or JSON completion'
    }
} finally { [Console]::SetError($savedConsoleError);$progressCapture.Dispose();if ([IO.Directory]::Exists($fixtureRoot)) { [IO.Directory]::Delete($fixtureRoot,$true) } }
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
