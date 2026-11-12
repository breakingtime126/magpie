# Only a newly created, retained PowerShell sleeper is terminated. No UI,
# application, child-process discovery, registry or network is used by the fixture.
[CmdletBinding()]
param([string]$CallerPath='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if (-not $CallerPath) { $CallerPath=Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1' }
. (Join-Path $PSScriptRoot 'original-desktop-cleanup.ps1')
function Need([bool]$Condition,[string]$Message) { if (-not $Condition) { throw ('FAIL: '+$Message) } }
$tokens=$null;$errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($CallerPath,[ref]$tokens,[ref]$errors)
Need ($errors.Count -eq 0) 'real retained-root fixture source parses'
foreach ($name in @('Assert-AcceptanceOwner','Stop-AcceptanceOwner')) {
    $functions=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq $name},$false))
    Need ($functions.Count -eq 1) 'one production owner adapter'
    Invoke-Expression $functions[0].Extent.Text
}
if ($PSVersionTable.PSEdition -ceq 'Desktop') { $hostExe=Join-Path $PSHOME 'powershell.exe' }
elseif ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { $hostExe=Join-Path $PSHOME 'pwsh.exe' }
else { $hostExe=Join-Path $PSHOME 'pwsh' }
$homeRoot=Join-Path ([IO.Path]::GetTempPath()) ('cleanup-owned-process-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($homeRoot)
$process=[Diagnostics.Process]::new();$started=$false
try {
    $start=[Diagnostics.ProcessStartInfo]::new();$start.FileName=$hostExe
    $start.Arguments='-NoLogo -NoProfile -NonInteractive -Command "[Console]::Out.WriteLine(''ready''); [Threading.Thread]::Sleep(60000)"'
    $start.UseShellExecute=$false;$start.CreateNoWindow=$true;$start.RedirectStandardOutput=$true
    $start.EnvironmentVariables.Clear()
    foreach ($name in @('SystemRoot','WINDIR')) {
        $value=[Environment]::GetEnvironmentVariable($name)
        if ($null -ne $value) { $start.EnvironmentVariables[$name]=$value }
    }
    foreach ($name in @('HOME','USERPROFILE','APPDATA','LOCALAPPDATA','TEMP','TMP','XDG_CACHE_HOME','XDG_CONFIG_HOME')) {
        $start.EnvironmentVariables[$name]=$homeRoot
    }
    $start.EnvironmentVariables['POWERSHELL_TELEMETRY_OPTOUT']='1'
    $start.EnvironmentVariables['POWERSHELL_UPDATECHECK']='Off'
    $process.StartInfo=$start;$started=$process.Start()
    Need $started 'owned sleeper starts'
    $null=$process.Handle
    $ticks=$process.StartTime.ToUniversalTime().Ticks;$image=$process.MainModule.FileName
    $ready=$process.StandardOutput.ReadLineAsync()
    Need ($ready.Wait(30000) -and $ready.GetAwaiter().GetResult() -ceq 'ready') 'owned sleeper is running before cleanup'
    $result=$null;$threw=$false
    try { $result=Stop-AcceptanceOwner $process $image $ticks } catch { $threw=$true }
    Need (-not $threw -and $null -ne $result -and $result.rootExitConfirmed -and $result.exitKind -ceq 'forced' -and $process.HasExited) 'real forced root exit is recorded rather than discarded as an exception'
    $again=Stop-AcceptanceOwner $process $image $ticks
    Need ($again.rootExitConfirmed -and $again.exitKind -ceq 'already-exited' -and -not $again.forceAttempted) 'same retained exited process requires no further action'
    Write-Output 'ORIGINAL_DESKTOP_CLEANUP_OWNED_PROCESS_TESTS_PASSED'
} finally {
    if ($started -and -not $process.HasExited) {
        $process.Kill()
        Need ($process.WaitForExit(5000)) 'fixture reaps its retained process even when a regression fails'
    }
    $process.Dispose()
    [IO.Directory]::Delete($homeRoot,$true)
}
