# Offline safety checks only; fixtures are synthetic, not actual package evidence.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'review-desktop-contract.ps1')
$script:checks=0
function Check([bool]$Condition,[string]$Name) { if (-not $Condition) { throw ('FAIL: '+$Name) }; $script:checks++ }
function Reject([scriptblock]$Action,[string]$Name) {
    $failed=$false; try { & $Action } catch { $failed=$true }; Check $failed $Name
}
Assert-ContractReviewCI 'true' 'github-hosted' 'Windows' 'breakingtime126/magpie' 'refs/heads/experiment/app-server-bridge-v0.1.3035' 'native-contract-review-v1'
$valid=@('true','github-hosted','Windows','breakingtime126/magpie','refs/heads/experiment/app-server-bridge-v0.1.3035','native-contract-review-v1')
for ($i=0; $i -lt $valid.Count; $i++) {
    $values=$valid.Clone(); $values[$i]='unapproved'; Reject { Assert-ContractReviewCI @values } ('CI gate '+$i)
}
$pins=Get-ContractReviewPins
$identity=[pscustomobject]@{status='observed';manifest=[pscustomobject]@{version=$pins.version};packageSha256=$pins.packageSha256
    packageBytes=$pins.packageBytes;backend=[pscustomobject]@{sha256=$pins.backendSha256};archive=[pscustomobject]@{sha256=$pins.archiveSha256}}
Assert-ContractReviewIdentity $identity
foreach ($field in @('status','packageSha256','packageBytes')) {
    $copy=$identity | ConvertTo-Json -Depth 5 | ConvertFrom-Json; $copy.$field='unapproved'
    Reject { Assert-ContractReviewIdentity $copy } ('review identity '+$field)
}
foreach ($field in @('manifest','backend','archive')) {
    $copy=$identity | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    if ($field -ceq 'manifest') { $copy.manifest.version='0.0.0.0' } else { $copy.$field.sha256='0'*64 }
    Reject { Assert-ContractReviewIdentity $copy } ('review entry '+$field)
}
$temp=Join-Path ([IO.Path]::GetTempPath()) ('contract-test-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($temp)
try {
    Assert-ContractGeneratedBound $temp
    $file=Join-Path $temp 'one.json'; $stream=[IO.File]::Create($file)
    try { $stream.SetLength(8388609) } finally { $stream.Dispose() }
    Reject { Assert-ContractGeneratedBound $temp } 'generated file size cap'
} finally { Remove-Item -LiteralPath $temp -Force -Recurse }
$source=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'review-desktop-contract.ps1') -Raw
Check (-not ($source -match 'Add-AppxPackage|Add-AppxProvisionedPackage|ActivateApplication|Start-Process|--print-startup-config|--startup-client-info')) 'review never installs activates or probes startup'
Check ($source.Contains("@('app-server','generate-json-schema','--experimental','--out',`$schemas)")) 'exact experimental generator argv'
Check ($source.Contains('[DesktopLive.BoundedProcess]::new($start,$OutputLimit,30000)')) 'shared externally timed supervisor'
Check ($source.Contains('$start=New-LiveStart -Executable $Executable -Arguments $Arguments -Directory $profileRoot')) 'cleared environment fresh profile helper'
Check ($source.Contains('$child.Stop(); $exitConfirmed=$child.Wait(5000) -and $child.Exited')) 'explicit held-child cleanup confirmation'
Check ($source.IndexOf('Assert-ContractReviewIdentity $identity') -lt $source.IndexOf('Export-ContractReviewInputs $package $work')) 'identity before extraction and execution'
Check ($source.IndexOf("'--schemas',`$schemas") -lt $source.IndexOf('[IO.Directory]::Move($publication')) 'validate before publishing generated protocol'
# The new static mode has its own exact gate; it never invokes the native lane.
$rendererGate=@('true','github-hosted','Windows','breakingtime126/magpie','refs/heads/experiment/app-server-bridge-v0.1.3035','renderer-menu-review-v1')
Assert-RendererMenuReviewCI @rendererGate
for ($i=0; $i -lt $rendererGate.Count; $i++) {
    $values=$rendererGate.Clone(); $values[$i]='unapproved'
    Reject { Assert-RendererMenuReviewCI @values } ('renderer gate '+$i)
}
$tokens=$null; $errors=$null
$reviewAst=[Management.Automation.Language.Parser]::ParseInput($source,[ref]$tokens,[ref]$errors)
Check ($errors.Count -eq 0) 'renderer/static source parses'
$staticFunctions=@($reviewAst.FindAll({param($n)
    $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Invoke-RendererMenuReview'
},$true))
Check ($staticFunctions.Count -eq 1) 'one static entry point'
$staticSource=$staticFunctions[0].Extent.Text
$commands=@($staticFunctions[0].FindAll({param($n)
    $n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -ceq 'Invoke-ContractCommand'
},$true))
Check ($commands.Count -eq 2 -and $commands[0].CommandElements[1].Extent.Text -ceq '$verifier.FullName' -and
    $commands[1].CommandElements[1].Extent.Text -ceq '$node') 'static children only verifier and Node'
Check (-not ($staticSource -match 'Invoke-DesktopContractReview|generate-json-schema|Add-AppxPackage|ActivateApplication|Start-Process')) 'static mode cannot enter native or Desktop commands'
Check ($staticSource.Contains('Export-ContractReviewInputs $package $work -ArchiveOnly')) 'static extracts only archive'
Check ($staticSource.Contains("'--renderer-menu'")) 'fixed renderer scanner mode'
Check ($source.Contains("if (`$ArchiveOnly -and `$item.leaf -cne 'app.asar') { continue }")) 'archive-only omits native extraction'
Check ($source.Contains("if (`$Run -and `$RendererMenuOnly) { throw 'Contract review modes are mutually exclusive.' }")) 'mutually exclusive entry flags'
Check ($source.Contains('if ($RendererMenuOnly) { Invoke-RendererMenuReview }') -and
    $source.Contains('elseif ($Run) { Invoke-DesktopContractReview }')) 'static selector does not fall through to native'
Check ($staticSource.IndexOf('Assert-ContractReviewIdentity $identity') -lt $staticSource.IndexOf('Export-ContractReviewInputs $package $work -ArchiveOnly')) 'static trust before extraction'
Write-Output ('CONTRACT_REVIEW_OFFLINE_CHECKS='+$script:checks)
