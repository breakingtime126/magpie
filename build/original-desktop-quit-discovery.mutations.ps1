# Offline mutations run in temporary copies, never the acceptance worker.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$base=Join-Path ([IO.Path]::GetTempPath()) ('tray-mutations-'+[guid]::NewGuid().ToString('N'))
$root=Join-Path $base 'build'
[void][IO.Directory]::CreateDirectory($root)
try {
    [IO.File]::Copy((Join-Path $PSScriptRoot '../go.mod'),(Join-Path $base 'go.mod'))
    foreach ($name in @('original-desktop-quit-discovery.ps1','original-desktop-quit-discovery-worker.ps1',
        'original-desktop-quit-discovery-core.ps1','original-desktop-quit-discovery.tests.ps1')) {
        [IO.File]::Copy((Join-Path $PSScriptRoot $name),(Join-Path $root $name))
    }
    $core=Join-Path $root 'original-desktop-quit-discovery-core.ps1'
    $source=[IO.File]::ReadAllText($core)
    $mutations=@(
        @('$r.validRectangles -eq 1','$r.validRectangles -ge 0'),
        @('visibilityVerified=$false','visibilityVerified=$true'),
        @('$rect.right -le ([long]$rect.screenX+$rect.screenWidth)', '$true'),
        @('actions=0','actions=1'),
        @('$first.runtime -cne $second.runtime','$false'),
        @('$hit.name -cne ''magpie''','$false')
    )
    $caught=0
    foreach ($mutation in $mutations) {
        $changed=$source.Replace($mutation[0],$mutation[1])
        if ($changed -ceq $source) { throw 'Mutation anchor absent.' }
        [IO.File]::WriteAllText($core,$changed)
        $si=[Diagnostics.ProcessStartInfo]::new();$si.FileName=(Get-Process -Id $PID).Path
        $si.UseShellExecute=$false;$si.RedirectStandardOutput=$true;$si.RedirectStandardError=$true
        foreach ($arg in @('-NoLogo','-NoProfile','-File',(Join-Path $root 'original-desktop-quit-discovery.tests.ps1'))) { $si.ArgumentList.Add($arg) }
        $child=[Diagnostics.Process]::new();$child.StartInfo=$si
        try {
            if (-not $child.Start()) { throw 'Mutation process failed to start.' }
            if (-not $child.WaitForExit(15000)) { $child.Kill();[void]$child.WaitForExit(3000);throw 'Mutation timed out.' }
            if ($child.ExitCode -eq 0) { throw 'Mutation survived.' }
            $caught++
        } finally { $child.Dispose() }
    }
    "Tray discovery mutations caught: $caught."
} finally { [IO.Directory]::Delete($base,$true) }
