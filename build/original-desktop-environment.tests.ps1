# Inert directories only; never official native state or processes.
[CmdletBinding()]
param([string]$Implementation='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if (-not $Implementation) { $Implementation=Join-Path $PSScriptRoot 'original-desktop-environment.ps1' }
. (Join-Path $PSScriptRoot 'original-connect-action-core.ps1')
. $Implementation
function Assert($Value,[string]$Message) { if (-not $Value) { throw ('FAIL: '+$Message) } }
# Only the host gate is stubbed for portable tests. Filesystem checks and the
# production ProcessStartInfo construction run unchanged on authored directories.
$script:hostAllowed=$true
function Assert-OriginalDesktopPackageRunner { if (-not $script:hostAllowed) { throw 'Untrusted fixture host.' } }
$root=Join-Path ([IO.Path]::GetTempPath()) ('original-environment-'+[guid]::NewGuid().ToString('N'))
$session=Join-Path $root 'session';$owned=Join-Path $root 'owned';$run=Join-Path $root 'app';$native=Join-Path $owned '.codex'
$before=[Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
try {
    foreach ($path in @($session,$owned,$run,$native)) { $null=[IO.Directory]::CreateDirectory($path) }
    $parent=@{USERPROFILE=$session;HOME=$session;SystemRoot=$root;OPENAI_API_KEY='INERT_CANARY';HTTP_PROXY='http://inert.invalid'}
    foreach ($hasHome in @($true,$false)) {
        if ($hasHome) { $parent.HOME=$session } else { $parent.Remove('HOME') }
        $saved=$parent|ConvertTo-Json -Compress
        $start=New-OriginalDesktopEnvironment $parent $session 'inert.exe' $run $owned
        Assert ($start.EnvironmentVariables['USERPROFILE'] -ceq $session) 'preserve session USERPROFILE rather than synthetic home'
        Assert ($start.EnvironmentVariables.ContainsKey('HOME') -eq $hasHome) 'preserve absent HOME'
        if ($hasHome) { Assert ($start.EnvironmentVariables['HOME'] -ceq $session) 'preserve ambient HOME identity' }
        Assert ($start.EnvironmentVariables['CODEX_HOME'] -ceq $native) 'native state stays isolated'
        foreach ($name in @('APPDATA','LOCALAPPDATA','TEMP','TMP','XDG_CONFIG_HOME','XDG_CACHE_HOME')) {
            Assert ($start.EnvironmentVariables[$name] -ceq $owned) 'owned data and temporary directories'
        }
        Assert (-not $start.EnvironmentVariables.ContainsKey('OPENAI_API_KEY')) 'no unrelated credentials inherited'
        Assert ($start.EnvironmentVariables['HTTP_PROXY'] -ceq $parent.HTTP_PROXY) 'transport configuration retained'
        Assert (($parent|ConvertTo-Json -Compress) -ceq $saved) 'parent dictionary unchanged'
        Assert (-not $start.UseShellExecute -and $start.Arguments -ceq 'gui agents') 'original GUI launch only'
    }
    foreach ($case in @('mismatched-home','relative-home','empty-home','missing-profile','wrong-session','existing-native','nonempty-isolated','not-hosted','native-override')) {
        $inputEnv=@{USERPROFILE=$session;HOME=$session;SystemRoot=$root};$identity=$session
        $script:hostAllowed=$true
        switch ($case) {
            'mismatched-home' { $inputEnv.HOME=$owned }
            'relative-home' { $inputEnv.HOME='relative' }
            'empty-home' { $inputEnv.HOME='' }
            'missing-profile' { $inputEnv.Remove('USERPROFILE') }
            'wrong-session' { $identity=$owned }
            'existing-native' { $null=[IO.Directory]::CreateDirectory((Join-Path $session '.codex')) }
            'nonempty-isolated' { [IO.File]::WriteAllText((Join-Path $native 'inert'), 'INERT') }
            'not-hosted' { $script:hostAllowed=$false }
            'native-override' { $inputEnv.CODEX_HOME=$native }
        }
        $rejected=$false
        try { $null=New-OriginalDesktopEnvironment $inputEnv $identity 'inert.exe' $run $owned } catch { $rejected=$true }
        Assert $rejected ('fail closed '+$case)
        if ($case -ceq 'existing-native') { [IO.Directory]::Delete((Join-Path $session '.codex')) }
        if ($case -ceq 'nonempty-isolated') { [IO.File]::Delete((Join-Path $native 'inert')) }
    }
    $after=[Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
    Assert ($before.Count -eq $after.Count) 'process environment count unchanged'
    foreach ($name in $before.Keys) { Assert ($before[$name] -ceq $after[$name]) 'process environment unchanged' }
    $text=[IO.File]::ReadAllText($Implementation)
    Assert ($text -notmatch 'SetEnvironmentVariable|Set-Item|Set-Entry|WriteAll|ReadAll|::Start\(') 'no global mutation, contents or process execution'
    Write-Output 'PASS actual construction seam: session identity, absent HOME, isolated empty native state, refusals and immutable environment'
} finally { if ([IO.Directory]::Exists($root)) { [IO.Directory]::Delete($root,$true) } }
