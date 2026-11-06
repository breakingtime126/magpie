# Offline, synthetic fixtures only. Import allowlisted helpers, never execute the probe body.
[CmdletBinding()]
param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'FAIL: cold-start syntax' }
foreach ($name in @('Assert-DesktopCIOptions', 'Assert-DesktopCompatibilityPackage', 'New-DesktopBridgeSettings')) {
    $definitions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($definitions.Count -ne 1) { throw 'FAIL: missing or duplicated CI configuration helper' }
    . ([scriptblock]::Create($definitions[0].Extent.Text))
}
$script:passed = 0
function Expect-Pass([string]$Name, [scriptblock]$Action) {
    try { $null = & $Action } catch { throw ('FAIL: ' + $Name) }
    $script:passed++
}
function Expect-Reject([string]$Name, [scriptblock]$Action) {
    $rejected = $false
    try { $null = & $Action } catch {
        if ($_.Exception.Message.Contains('PRIVATE_CANARY')) { throw ('FAIL: leaked input in ' + $Name) }
        $rejected = $true
    }
    if (-not $rejected) { throw ('FAIL: accepted ' + $Name) }
    $script:passed++
}
function Assert-Keys($Value, [string[]]$Expected) {
    $actual = @($Value.PSObject.Properties | ForEach-Object { $_.Name } | Sort-Object)
    if (($actual -join '|') -cne (($Expected | Sort-Object) -join '|')) { throw 'Unexpected profile fields.' }
}
function New-Options {
    return @{
        Mode = 'observe'; StartupProbe = $true; LaunchCompatibility = $true; StaticLauncher = $true
        Actions = 'true'; RunnerEnvironment = 'github-hosted'; Repository = 'breakingtime126/magpie'
        Ref = 'refs/heads/experiment/app-server-bridge-v0.1.3035'
    }
}
function New-Backends {
    return @(
        [ordered]@{ id = 'alpha'; provider = 'alpha_provider'; command = 'fixture-codex.exe'; home = 'fixture-alpha-home'; codex_home = 'fixture-alpha-codex' },
        [ordered]@{ id = 'beta'; provider = 'beta_provider'; command = 'fixture-codex.exe'; home = 'fixture-beta-home'; codex_home = 'fixture-beta-codex' }
    )
}
$version = 'codex-cli 0.167.0-alpha.3.2'
$hash = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
Expect-Pass 'explicit observed compatibility under existing CI guards' {
    $options = New-Options
    Assert-DesktopCIOptions @options
}
foreach ($mode in @('baseline', 'bridge')) {
    Expect-Reject ('compatibility outside observe: ' + $mode) {
        $options = New-Options; $options.Mode = $mode
        Assert-DesktopCIOptions @options
    }
    Expect-Pass ('unchanged no-opt-in mode: ' + $mode) {
        $options = New-Options; $options.Mode = $mode
        $options.StartupProbe = $false; $options.LaunchCompatibility = $false; $options.StaticLauncher = $false
        Assert-DesktopCIOptions @options
    }
}
Expect-Reject 'compatibility without separate startup opt-in' {
    $options = New-Options; $options.StartupProbe = $false
    Assert-DesktopCIOptions @options
}
Expect-Pass 'startup-only observe remains accepted' {
    $options = New-Options; $options.LaunchCompatibility = $false
    Assert-DesktopCIOptions @options
}
foreach ($field in @('Actions', 'RunnerEnvironment', 'Repository', 'Ref')) {
    foreach ($value in @('', 'PRIVATE_CANARY')) {
        Expect-Reject ('unreviewed CI context: ' + $field) {
            $options = New-Options; $options[$field] = $value
            Assert-DesktopCIOptions @options
        }
    }
}
Expect-Reject 'self-hosted runner' {
    $options = New-Options; $options.RunnerEnvironment = 'self-hosted'
    Assert-DesktopCIOptions @options
}
Expect-Reject 'main branch' {
    $options = New-Options; $options.Ref = 'refs/heads/main'
    Assert-DesktopCIOptions @options
}
Expect-Reject 'fork repository' {
    $options = New-Options; $options.Repository = 'another-owner/magpie'
    Assert-DesktopCIOptions @options
}
$packageVersion = '26.1103.3517.0'
$packageHash = 'ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643'
Expect-Pass 'compatibility requires the exact Desktop caller package' {
    Assert-DesktopCompatibilityPackage $true $packageVersion $packageHash
}
foreach ($badVersion in @('', '26.1103.3517.1', 'PRIVATE_CANARY')) {
    Expect-Reject 'mismatching Desktop package version' {
        Assert-DesktopCompatibilityPackage $true $badVersion $packageHash
    }
}
foreach ($badHash in @('', ('0' * 64), $packageHash.ToUpperInvariant(), 'PRIVATE_CANARY')) {
    Expect-Reject 'mismatching Desktop package hash' {
        Assert-DesktopCompatibilityPackage $true $packageVersion $badHash
    }
}
Expect-Pass 'new package gate does not broaden or change startup-only and baseline gates' {
    Assert-DesktopCompatibilityPackage $false '' ''
}
Expect-Pass 'default config has no opt-ins or pins' {
    $settings = New-DesktopBridgeSettings (New-Backends) $false $false '' ''
    $json = $settings | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    Assert-Keys $json @('backends')
    if ($json.backends.Count -ne 2) { throw 'Expected two backends.' }
    foreach ($backend in $json.backends) {
        Assert-Keys $backend @('id', 'provider', 'command', 'home', 'codex_home')
    }
}
Expect-Pass 'startup-only config is not silently broadened' {
    $settings = New-DesktopBridgeSettings (New-Backends) $true $false $version $hash
    $json = $settings | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    Assert-Keys $json @('backends', 'startup_probe')
    if ($json.startup_probe -cne 'compare-unrestricted-v0.167.0-alpha.3.2') { throw 'Wrong startup mode.' }
    foreach ($backend in $json.backends) {
        Assert-Keys $backend @('id', 'provider', 'command', 'home', 'codex_home', 'startup_probe_sha256')
        if ($backend.startup_probe_sha256 -cne $hash) { throw 'Missing exact pin.' }
    }
}
Expect-Pass 'compatibility config contains only reviewed exact modes and both pins' {
    $backends = New-Backends
    $settings = New-DesktopBridgeSettings $backends $true $true $version $hash
    $json = $settings | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $expected = [ordered]@{
        startup_probe = 'compare-unrestricted-v0.167.0-alpha.3.2'
        desktop_plugin_override = 'codex-app-tools-v0.167.0-alpha.3.2'
        attestation = 'unavailable-v0.167.0-alpha.3.2'
        gateway_oauth = 'primary-read-only-v0.167.0-alpha.3.2'
        gateway_oauth_provider_name = 'CI alpha launch-only'
        config_requirements_read = 'compare-null-v0.167.0-alpha.3.2'
    }
    Assert-Keys $json (@('backends') + @($expected.Keys))
    foreach ($key in $expected.Keys) {
        if ($json.$key -cne $expected[$key]) { throw 'Wrong exact compatibility mode.' }
    }
    if ($json.backends.Count -ne 2) { throw 'Expected both backends.' }
    for ($index = 0; $index -lt 2; $index++) {
        $backend = $json.backends[$index]
        Assert-Keys $backend @('id', 'provider', 'command', 'home', 'codex_home', 'startup_probe_sha256')
        if ($backend.startup_probe_sha256 -cne $hash) { throw 'Missing exact pin.' }
        foreach ($key in @('id', 'provider', 'command', 'home', 'codex_home')) {
            if ($backend.$key -cne $backends[$index][$key]) { throw 'Backend configuration was changed.' }
        }
        if ($backends[$index].Contains('startup_probe_sha256')) { throw 'Input configuration was mutated.' }
    }
    if ($json.backends[0].home -ceq $json.backends[1].home -or
        $json.backends[0].codex_home -ceq $json.backends[1].codex_home) { throw 'Backend homes must remain separate.' }
}
Expect-Reject 'builder refuses compatibility without startup pinning' {
    New-DesktopBridgeSettings (New-Backends) $false $true $version $hash
}
foreach ($compatibility in @($false, $true)) {
    foreach ($badVersion in @('', 'codex-cli 0.166.0', 'codex-cli 0.167.0-alpha.3.3', 'PRIVATE_CANARY')) {
        Expect-Reject 'nonmatching official version' {
            New-DesktopBridgeSettings (New-Backends) $true $compatibility $badVersion $hash
        }
    }
    foreach ($badHash in @('', ('0' * 64), $hash.ToUpperInvariant(), 'PRIVATE_CANARY')) {
        Expect-Reject 'nonmatching executable hash' {
            New-DesktopBridgeSettings (New-Backends) $true $compatibility $version $badHash
        }
    }
    foreach ($count in @(0, 1, 3)) {
        Expect-Reject 'profile requires both exact backends' {
            $all = @(New-Backends) + @([ordered]@{ id = 'extra' })
            $subset = @($all | Select-Object -First $count)
            New-DesktopBridgeSettings $subset $true $compatibility $version $hash
        }
    }
}
Write-Output ('DESKTOP_CI_PROFILE_TESTS_PASSED=' + $script:passed)
