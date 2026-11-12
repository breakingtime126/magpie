$ErrorActionPreference='Stop'
$root=Join-Path ([IO.Path]::GetTempPath()) ('capture-mutations-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
try {
    foreach ($mutation in @('identity','output','exit','report','start-return','input-preamble','worker-progress','compile-progress','worker-prefix','phase-prefix','worker-load-path','helper-load-path','fixture-path-cmdlet','sleep-exit','phase-deadline')) {
        $work=Join-Path $root $mutation;[void][IO.Directory]::CreateDirectory($work)
        Get-ChildItem -LiteralPath $PSScriptRoot -Filter 'original-desktop-window-capture*.ps1' | Copy-Item -Destination $work
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'original-desktop-window-capture-native.cs') -Destination $work
        if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) {
            # Same CI-built input, never recompilation or a missing-DLL false red.
            Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'original-desktop-window-capture-native.dll') -Destination $work -ErrorAction Stop
        }
        $filename=if ($mutation -eq 'start-return') { 'original-desktop-window-capture-supervisor.ps1' } elseif ($mutation -in @('worker-progress','worker-prefix','worker-load-path')) { 'original-desktop-window-capture-worker.ps1' } elseif ($mutation -in @('input-preamble','compile-progress','sleep-exit','phase-deadline')) { 'original-desktop-window-capture.tests.ps1' } else { 'original-desktop-window-capture.ps1' }
        $path=Join-Path $work $filename
        $text=[IO.File]::ReadAllText($path)
        switch ($mutation) {
            'sleep-exit' { $text=$text.Replace('[Threading.Thread]::Sleep(60000)','[Threading.Thread]::Sleep(1)') }
            'phase-deadline' { $text=$text.Replace('$phaseChild.Deadline(100)','$phaseChild.Deadline(60000)') }
            'worker-load-path' { $text=$text.Replace("[IO.Path]::Combine(`$PSScriptRoot,'original-desktop-window-capture.ps1')","Join-Path `$PSScriptRoot 'original-desktop-window-capture.ps1'") }
            'helper-load-path' { $text=$text.Replace("[IO.Path]::Combine(`$PSScriptRoot,'original-desktop-window-capture-native.ps1')","Join-Path `$PSScriptRoot 'original-desktop-window-capture-native.ps1'") }
            'fixture-path-cmdlet' { $text=$text.Replace("[IO.Path]::Combine(`$PrivateDirectory,'app','Magpie-Native-Codex-Experimental.exe')","(Join-Path `$PrivateDirectory 'app\Magpie-Native-Codex-Experimental.exe')") }
            'worker-prefix' { $text=$text.Replace('[Console]::Out.Write("context-checked`n");[Console]::Out.Flush()','') }
            'phase-prefix' { $text=$text.Replace("return 'unverified'","return 'helper-loaded'") }
            { $_ -in @('worker-progress','compile-progress') } { $text=$text.Replace("`$ProgressPreference='SilentlyContinue'","`$ProgressPreference='Continue'") }
            'input-preamble' { $text=$text.Replace('return [Text.UTF8Encoding]::new($false)','return $Encoding') }
            'start-return' { $text=$text.Replace('attempted=true;','').Replace('var held=process.Handle;','attempted=true; var held=process.Handle;') }
            'identity' { $text=$text.Replace('if (-not (Test-OriginalCaptureIdentity $OwnedProcess $identity))','if ($false)') }
            'output' { $text=$text.Replace(' -and $child.Output -ceq ((Get-OriginalCaptureProtocol)+''{"schema":1,"status":"captured"}'')','') }
            'exit' { $text=$text.Replace('if (-not $child.Success -or -not $child.ExitConfirmed)','if (-not $child.Success)') }
            'report' { $text=$text.Replace('if (-not $SequenceReportSaved)','if ($false)') }
        }
        [IO.File]::WriteAllText($path,$text)
        $savedPreference=$ErrorActionPreference
        try {
            $ErrorActionPreference='Continue'
            $output=& (Get-Process -Id $PID).Path -NoProfile -File (Join-Path $work 'original-desktop-window-capture.tests.ps1') 2>&1 | Out-String
        } finally { $ErrorActionPreference=$savedPreference }
        if ($LASTEXITCODE -eq 0 -or $output -notmatch 'FAIL:') { throw "Mutation did not fail behavior assertion: $mutation" }
        "PASS: red mutation $mutation"
    }
} finally { Remove-Item -LiteralPath $root -Recurse -Force }
