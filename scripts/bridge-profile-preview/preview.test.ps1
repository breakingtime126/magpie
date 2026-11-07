# No Pester dependency, UI, native backend or network. Run on Windows PowerShell 5.1.
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/preview.ps1"

function Assert-Rejected([scriptblock]$Action) {
    $rejected = $false
    try { & $Action | Out-Null } catch { $rejected = $true }
    if (-not $rejected) { throw 'Expected draft rejection.' }
}
function New-Fixture {
    return @(
        @{ id = 'alpha'; provider = 'fixture_a'; command = 'C:\Synthetic\codex.exe'; home = 'C:\Synthetic\home-a'; codex_home = 'C:\Synthetic\codex-a' },
        @{ id = 'beta'; provider = 'fixture_b'; command = 'C:\Synthetic\codex.exe'; home = 'C:\Synthetic\home-b'; codex_home = 'C:\Synthetic\codex-b' }
    )
}

# These paths deliberately do not exist: preview must not inspect or modify them.
$fixture = New-Fixture
$json = New-BridgeProfileDraft -Backends $fixture
$parsed = $json | ConvertFrom-Json
if ($parsed.backends.Count -ne 2 -or $parsed.backends[1].provider -ne 'fixture_b') { throw 'Wrong output.' }
if (@($parsed.PSObject.Properties).Count -ne 1) { throw 'Unexpected top-level capability.' }
foreach ($backend in $parsed.backends) {
    if (@($backend.PSObject.Properties).Count -ne 5) { throw 'Unexpected backend capability.' }
}
$fixture[0].token = 'synthetic-do-not-export'
$fixture[0].inherit_env = @('SYNTHETIC_SECRET')
$fixture[0].models = @('invented')
$json = New-BridgeProfileDraft -Backends $fixture
if ($json.Contains('synthetic-do-not-export') -or $json.Contains('inherit_env') -or $json.Contains('models')) {
    throw 'Unapproved fields leaked into profile.'
}
Assert-Rejected { New-BridgeProfileDraft -Backends @() }
foreach ($invalid in @('C:\Synthetic\codex.ps1', '\\server\share\codex.exe', 'codex.exe', 'C:\..\codex.exe', 'C:\%USERPROFILE%\codex.exe')) {
    $fixture = New-Fixture
    $fixture[0].command = $invalid
    Assert-Rejected { New-BridgeProfileDraft -Backends $fixture }
}
foreach ($invalid in @('C:\Synthetic\home-a', 'c:\synthetic\HOME-A\child', 'C:\Synthetic')) {
    $fixture = New-Fixture
    $fixture[1].home = $invalid
    Assert-Rejected { New-BridgeProfileDraft -Backends $fixture }
}
$fixture = New-Fixture
$fixture[1].id = 'ALPHA'
Assert-Rejected { New-BridgeProfileDraft -Backends $fixture }
$fixture = New-Fixture
$fixture[0].provider = 'https://example.invalid'
Assert-Rejected { New-BridgeProfileDraft -Backends $fixture }

# Structural guard against accidentally connecting this isolated utility to normal
# Magpie startup or filesystem/process/network mutation. Not a sandbox proof.
$source = Get-Content -LiteralPath "$PSScriptRoot/preview.ps1" -Raw
foreach ($forbidden in @('Start-Process', 'Invoke-WebRequest', 'Invoke-RestMethod', 'Set-Content', 'Out-File', 'WriteAllText', 'ReadAllText', 'Get-Content', 'SetEnvironmentVariable', 'Set-ItemProperty', 'auth.json', 'config.toml')) {
    if ($source.Contains($forbidden)) { throw "Unexpected capability: $forbidden" }
}
'PASS: pure draft generation, input rejection, field isolation and capability guard'
