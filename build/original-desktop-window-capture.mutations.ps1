$ErrorActionPreference='Stop'
$root=Join-Path ([IO.Path]::GetTempPath()) ('capture-mutations-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
try {
    foreach ($mutation in @('identity','output','exit','report','start-return','input-preamble')) {
        $work=Join-Path $root $mutation;[void][IO.Directory]::CreateDirectory($work)
        Get-ChildItem -LiteralPath $PSScriptRoot -Filter 'original-desktop-window-capture*.ps1' | Copy-Item -Destination $work
        $filename=if ($mutation -eq 'start-return') { 'original-desktop-window-capture-supervisor.ps1' } elseif ($mutation -eq 'input-preamble') { 'original-desktop-window-capture.tests.ps1' } else { 'original-desktop-window-capture.ps1' }
        $path=Join-Path $work $filename
        $text=[IO.File]::ReadAllText($path)
        switch ($mutation) {
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
