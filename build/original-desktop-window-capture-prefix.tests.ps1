# Invoked by original-desktop-window-capture.tests.ps1 in Windows Desktop only.
# Use the real environment builder and guards, but never invoke target identity,
# Capture, or another UI operation. This is not a mode in the production worker.
$ErrorActionPreference='Stop'
Assert ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT -and $PSVersionTable.PSEdition -eq 'Desktop') 'prefix fixture runs in production Desktop host'
Assert-OriginalCaptureRegularPath $env:RUNNER_TEMP
$prefixRoot=Join-Path $env:RUNNER_TEMP ('capture-prefix-tests-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $prefixRoot -ErrorAction Stop | Out-Null
try {
    $private=Join-Path $prefixRoot ('original-desktop-acceptance-'+[guid]::NewGuid().ToString('N'))
    $evidence=Join-Path $prefixRoot 'native-magpie-evidence'
    foreach ($directory in @($private,$evidence,(Join-Path $private 'app'),(Join-Path $private 'window-capture'))) {
        [void][IO.Directory]::CreateDirectory($directory)
    }
    $executable=Join-Path $private 'app\Magpie-Native-Codex-Experimental.exe'
    [IO.File]::WriteAllBytes($executable,[byte[]]::new(0)) # Path fixture only; never executed.
    $identity=@{Id=0;Ticks=0;Window=0;Executable=$executable}
    $prefixCode=@'
$ProgressPreference='SilentlyContinue'
$ErrorActionPreference='Stop'
try {
    [Console]::Out.Write("worker-entered`n");[Console]::Out.Flush()
    . (Join-Path '__HELPER_DIRECTORY__' 'original-desktop-window-capture.ps1')
    [Console]::Out.Write("helper-loaded`n");[Console]::Out.Flush()
    Assert-OriginalCaptureContext
    [Console]::Out.Write("context-checked`n");[Console]::Out.Flush()
    Assert-OriginalCapturePaths '__EXECUTABLE__' '__PRIVATE__' '__EVIDENCE__'
    [Console]::Out.Write("fixture-checked`n");[Console]::Out.Flush()
    $output=Join-Path '__PRIVATE__' 'window-capture\original-magpie-uncertain.png'
    Assert-OriginalCaptureRegularPath $output
    [Console]::Out.Write("native-compile`n");[Console]::Out.Flush()
    Initialize-OriginalCaptureNative
    if (-not ('OriginalCapture.Native' -as [type])) { exit 1 }
    [Console]::Out.Write('prefix-ready');exit 0
} catch { exit 1 }
'@
    foreach ($pair in @(@('__HELPER_DIRECTORY__',$PSScriptRoot),@('__EXECUTABLE__',$executable),@('__PRIVATE__',$private),@('__EVIDENCE__',$evidence))) {
        $prefixCode=$prefixCode.Replace($pair[0],$pair[1].Replace("'","''"))
    }
    Assert ($prefixCode -notmatch '::(Capture|Matches)|Get-Process|PrintWindow') 'prefix fixture contains no target/window invocation'
    $expected="worker-entered`nhelper-loaded`ncontext-checked`nfixture-checked`nnative-compile`nprefix-ready"
    Assert ([Text.Encoding]::UTF8.GetByteCount($expected) -le 256) 'prefix fixture fits unchanged stream bound'
    foreach ($contextAllowed in @($true,$false)) {
        # New-OriginalCaptureStart chooses the actual fixed Desktop host, clears
        # environment and applies the production allowlist. Only test paths/gate
        # values change in this owned child; the parent environment is untouched.
        $start=New-OriginalCaptureStart $identity $private $evidence
        $start.Arguments='-NoLogo -NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($prefixCode))
        $fixtureEnvironment=@{RUNNER_TEMP=$prefixRoot;GITHUB_ACTIONS='true';GITHUB_EVENT_NAME='push';RUNNER_OS='Windows';
            RUNNER_ENVIRONMENT='github-hosted';GITHUB_REPOSITORY='breakingtime126/magpie';
            GITHUB_REF='refs/heads/experiment/app-server-bridge-v0.1.3035';
            MAGPIE_ORIGINAL_CONNECT_ACTION='original-ui-v1';MAGPIE_ORIGINAL_WINDOW_CAPTURE='diagnostic-v1'}
        foreach ($name in $fixtureEnvironment.Keys) { $start.EnvironmentVariables[$name]=$fixtureEnvironment[$name] }
        if (-not $contextAllowed) { $start.EnvironmentVariables['GITHUB_REF']='refs/heads/main' }
        $allowed=@('SystemRoot','WINDIR','TEMP','TMP','RUNNER_TEMP','GITHUB_ACTIONS','GITHUB_EVENT_NAME','RUNNER_OS',
            'RUNNER_ENVIRONMENT','GITHUB_REPOSITORY','GITHUB_REF','MAGPIE_ORIGINAL_CONNECT_ACTION','MAGPIE_ORIGINAL_WINDOW_CAPTURE')
        foreach ($name in $start.EnvironmentVariables.Keys) { Assert ($name -in $allowed) 'production child environment has no inherited extra key' }
        $child=New-TestNoInputChild $start
        try {
            $child.CloseInput();$waited=$child.Wait(8000)
            $watch=[Diagnostics.Stopwatch]::StartNew()
            while (-not $child.Drained -and $watch.ElapsedMilliseconds -lt 1000) { Start-Sleep -Milliseconds 10 }
            $exact=$child.Output -ceq $expected
            $phase=if ($exact) { 'prefix-ready' } else { Get-OriginalCapturePhase $child.Output }
            $facts=[ordered]@{contextAllowed=$contextAllowed;phase=$phase;waitCompleted=[bool]$waited;deadline=[bool]$child.TimedOut;
                overflowOrReadError=[bool]$child.Overflow;stderrPresent=[bool]$child.ErrorOutput;streamsComplete=[bool]$child.Drained;
                exitConfirmed=[bool]$child.Exited;exitZero=($child.Exited -and $child.ExitCode -eq 0);outputExact=[bool]$exact}
            'PREFIX_PROBE '+($facts | ConvertTo-Json -Compress)
            Assert ($waited -and $child.Exited -and -not $child.TimedOut -and -not $child.Overflow -and
                -not $child.ErrorOutput -and $child.Drained) 'prefix fixture remains within unchanged transport bounds'
            if ($contextAllowed) { Assert ($child.ExitCode -eq 0 -and $exact) 'cleared-environment prefix and compilation succeed without window access' }
            else { Assert ($child.ExitCode -ne 0 -and $phase -ceq 'helper-loaded' -and -not $exact) 'real context gate rejects before fixture/compile stages' }
        } finally { $child.Dispose() }
    }
    Assert (([IO.FileInfo]$executable).Length -eq 0 -and -not [IO.File]::Exists((Join-Path $private 'window-capture\original-magpie-uncertain.png'))) 'dummy target stays unchanged and no capture file is produced'
} finally { Remove-Item -LiteralPath $prefixRoot -Recurse -Force }
