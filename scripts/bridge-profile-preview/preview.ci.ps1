param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[0-9a-f]{40}$')]
    [string]$ExpectedSha
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($PSVersionTable.PSEdition -ne 'Desktop' -or $PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) {
    throw 'This job requires Windows PowerShell 5.1.'
}
$actual = git rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $actual.Trim() -cne $ExpectedSha) {
    throw 'Checkout does not match the triggering commit.'
}
& "$PSScriptRoot/preview.test.ps1"
if (-not $?) { throw 'Draft tests failed.' }
