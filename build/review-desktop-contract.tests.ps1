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
Write-Output ('CONTRACT_REVIEW_OFFLINE_CHECKS='+$script:checks)
