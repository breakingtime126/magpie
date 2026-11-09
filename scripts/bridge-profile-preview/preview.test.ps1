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

# Only temporary synthetic filesystem fixtures; the real adapter is never run.
$root = Join-Path ([IO.Path]::GetTempPath()) ('bridge-profile-save-test-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $root -ErrorAction Stop)
$originalChecker = (Get-Item Function:Invoke-BridgeProfileCheck).ScriptBlock
$beforeEnv = [Environment]::GetEnvironmentVariable('MAGPIE_APP_SERVER_BRIDGE_CONFIG', 'Process')
try {
    $exe = Join-Path $root 'synthetic.exe'
    [IO.File]::WriteAllText($exe, 'public non-executable fixture')
    $parent = Join-Path $root 'profiles'
    [void](New-Item -ItemType Directory -Path $parent)
    $backendRows = @()
    foreach ($name in @('alpha', 'beta')) {
        $homePath = Join-Path $root ($name + '-home')
        $codexPath = Join-Path $root ($name + '-codex')
        [void](New-Item -ItemType Directory -Path $homePath)
        [void](New-Item -ItemType Directory -Path $codexPath)
        $backendRows += @{ id=$name;provider=('fixture_' + $name);command=$exe;home=$homePath;codex_home=$codexPath }
    }
    function Invoke-BridgeProfileCheck([string]$ProfilePath) {
        $readStream = [IO.File]::Open($ProfilePath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
        $reader = $null
        try {
            $reader = [IO.StreamReader]::new($readStream)
            $read = $reader.ReadToEnd() | ConvertFrom-Json
        } finally {
            if ($null -ne $reader) { $reader.Dispose() } else { $readStream.Dispose() }
        }
        if ($read.backends.Count -ne 2 -or @($read.PSObject.Properties).Count -ne 1) { throw 'Bad saved shape.' }
        $locked = $false
        try { [IO.File]::WriteAllText($ProfilePath, 'must not replace') } catch [IO.IOException] { $locked = $true }
        if (-not $locked) { throw 'Saved input was mutable during check.' }
        return 'passed'
    }
    $first = Save-BridgeProfileDraft $backendRows $parent
    if ($first.Status -cne 'passed' -or [IO.Path]::GetFileName($first.Path) -cne 'bridge.json') { throw 'Save/check failed.' }
    $firstBytes = [IO.File]::ReadAllText($first.Path)
    function Invoke-BridgeProfileCheck([string]$ProfilePath) { return 'timeout' }
    $second = Save-BridgeProfileDraft $backendRows $parent
    if ($second.Status -cne 'timeout' -or $second.Path -ceq $first.Path -or
        [IO.File]::ReadAllText($first.Path) -cne $firstBytes) { throw 'Timeout or no-overwrite contract failed.' }
    function Invoke-BridgeProfileCheck([string]$ProfilePath) { throw 'synthetic checker refusal' }
    $third = Save-BridgeProfileDraft $backendRows $parent
    if ($third.Status -cne 'failed' -or -not [IO.File]::Exists($third.Path)) { throw 'Failed draft was reported valid or lost.' }
    Assert-Rejected { Save-BridgeProfileDraft $backendRows $backendRows[0].home }
    Assert-Rejected { Save-BridgeProfileDraft $backendRows $root }
    $backendRows[0].token = 'PUBLIC-NOT-A-CREDENTIAL'
    Assert-Rejected { Save-BridgeProfileDraft $backendRows $parent }
    if ([Environment]::GetEnvironmentVariable('MAGPIE_APP_SERVER_BRIDGE_CONFIG', 'Process') -cne $beforeEnv) {
        throw 'Process-global environment changed.'
    }
} finally {
    Set-Item Function:Invoke-BridgeProfileCheck $originalChecker
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction Stop
}

# Guard the deliberately narrow production capabilities, not a sandbox proof.
$source = Get-Content -LiteralPath "$PSScriptRoot/preview.ps1" -Raw
foreach ($forbidden in @('Start-Process', 'Invoke-WebRequest', 'Invoke-RestMethod', 'SetEnvironmentVariable',
    'Set-ItemProperty', 'Set-Acl', 'ExecutionPolicy', 'auth.json', 'config.toml', 'account/login', 'app-server --')) {
    if ($source.Contains($forbidden)) { throw "Unexpected capability: $forbidden" }
}
foreach ($required in @('$psi.Arguments = ''--check''', '[IO.FileMode]::CreateNew',
    'magpie-codex-bridge-windows-amd64.exe', 'nativeAdapterSHA256',
    'Fields changed: previous check no longer applies.')) {
    if (-not $source.Contains($required)) { throw "Missing contract: $required" }
}
'PASS: synthetic profile generation, new-file persistence, check outcomes and unchanged global environment'
