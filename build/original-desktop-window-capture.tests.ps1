$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-desktop-window-capture.ps1')
function Assert($Condition,[string]$Message) { if (-not $Condition) { throw "FAIL: $Message" } }
function Reject($Action,[string]$Message) { $failed=$false;try { & $Action } catch { $failed=$true };Assert $failed $Message }
# Fixed fixture-only evidence. Never publish child output, error text or paths.
function Get-TestCompileFacts($Child) {
    $phase='unverified'
    $prefix="probe-entered`nsource-loaded`n"
    if ($Child.Output -ceq '') { $phase='not-observed' }
    elseif ($Child.Output -ceq "probe-entered`n") { $phase='probe-entered' }
    elseif ($Child.Output -ceq $prefix) { $phase='source-loaded' }
    elseif ($Child.Output -ceq "probe-entered`nsource-error") { $phase='source-error' }
    else {
        foreach ($label in @('native-ready','initialization-error','type-missing','compiler-error',
            'compiler-error-cs0012','compiler-error-cs0234','compiler-error-cs0246','compiler-error-cs0103')) {
            if ($Child.Output -ceq ($prefix+$label)) { $phase=$label;break }
        }
    }
    $exitZero=$Child.Exited -and $Child.ExitCode -eq 0
    $outputExact=$Child.Output -ceq ($prefix+'native-ready')
    $category=if ($Child.TimedOut) { 'deadline' } elseif ($Child.Overflow) { 'overflow-or-read-error' }
        elseif ($Child.ErrorOutput) { 'stderr-present' } elseif (-not $Child.Drained) { 'streams-incomplete' }
        elseif (-not $exitZero) { 'exit-not-zero' } elseif (-not $outputExact) { 'output-invalid' } else { 'none' }
    return [pscustomobject][ordered]@{category=$category;phase=$phase;deadline=[bool]$Child.TimedOut;
        overflowOrReadError=[bool]$Child.Overflow;stderrPresent=[bool]$Child.ErrorOutput;
        streamsComplete=[bool]$Child.Drained;exitConfirmed=[bool]$Child.Exited;exitZero=[bool]$exitZero;outputExact=[bool]$outputExact}
}
function New-TestCompileCode([string]$NativeScript) {
    $code=@'
$ErrorActionPreference='Stop'
$stage='source'
try {
    [Console]::Out.Write("probe-entered`n");[Console]::Out.Flush()
    . '__NATIVE_SCRIPT__'
    [Console]::Out.Write("source-loaded`n");[Console]::Out.Flush()
    $stage='compile'
    Initialize-OriginalCaptureNative
    if (-not ('OriginalCapture.Native' -as [type])) { [Console]::Out.Write('type-missing');exit 1 }
    [Console]::Out.Write('native-ready');exit 0
} catch {
    $label=if ($stage -eq 'source') { 'source-error' } else { 'initialization-error' }
    $id=($_.FullyQualifiedErrorId -split ',')[0]
    if ($id -cin @('SOURCE_CODE_ERROR','COMPILER_ERRORS')) {
        $label='compiler-error'
        # Read privately, emit only one of these fixed known compiler labels.
        $details=[string]$_.Exception.Message+' '+[string]$_.ErrorDetails.Message
        foreach ($code in @('CS0012','CS0234','CS0246','CS0103')) {
            if ($details -cmatch ('\b'+$code+'\b')) { $label='compiler-error-'+$code.ToLowerInvariant();break }
        }
    }
    [Console]::Out.Write($label);exit 1
}
'@
    return $code.Replace('__NATIVE_SCRIPT__',$NativeScript.Replace("'","''"))
}
# Desktop PowerShell Remove-Item can prompt for a directory symlink even when
# only the link is intended. Never recurse through this fixture's link target.
function Remove-TestDirectoryLink([string]$Path) {
    try { $attributes=[IO.File]::GetAttributes($Path) }
    catch [IO.FileNotFoundException] { return }
    catch [IO.DirectoryNotFoundException] { return }
    Assert (($attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) 'cleanup refuses a non-link directory'
    [IO.Directory]::Delete($Path,$false)
    Assert (-not [IO.Directory]::Exists($Path)) 'directory link removed without recursion'
}
$root=Join-Path ([IO.Path]::GetTempPath()) ('capture-tests-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
$link=Join-Path $root 'linked'
try {
    $compileFactsInput=@{Output="probe-entered`nsource-loaded`nnative-ready";TimedOut=$false;
        Overflow=$false;ErrorOutput=$false;Drained=$true;Exited=$true;ExitCode=0}
    Assert ((Get-TestCompileFacts $compileFactsInput).category -eq 'none') 'compile facts accept only exact healthy completion'
    foreach ($case in @(@('TimedOut',$true,'deadline'),@('Overflow',$true,'overflow-or-read-error'),
        @('ErrorOutput',$true,'stderr-present'),@('Drained',$false,'streams-incomplete'),
        @('ExitCode',1,'exit-not-zero'),@('Output','private arbitrary text','output-invalid'))) {
        $changed=$compileFactsInput.Clone();$changed[$case[0]]=$case[1]
        $facts=Get-TestCompileFacts $changed
        Assert ($facts.category -eq $case[2]) 'compile evidence classifies exact failure flag'
        Assert (($facts | ConvertTo-Json -Compress) -notmatch 'private arbitrary text') 'compile facts never echo child text'
    }
    $changed=$compileFactsInput.Clone();$changed.Output="probe-entered`nsource-loaded`ncompiler-error-cs0246";$changed.ExitCode=1
    Assert ((Get-TestCompileFacts $changed).phase -eq 'compiler-error-cs0246') 'compiler evidence preserves only fixed category'
    $changed.Output="probe-entered`nsource-loaded`nnative-ready`nextra"
    Assert ((Get-TestCompileFacts $changed).phase -eq 'unverified') 'extra compile output stays unverified'
    $probeTokens=$null;$probeErrors=$null
    [void][Management.Automation.Language.Parser]::ParseInput((New-TestCompileCode "fixture's native.ps1"),[ref]$probeTokens,[ref]$probeErrors)
    Assert ($probeErrors.Count -eq 0) 'compile probe code parses with quoted source path'
    Reject { Assert-OriginalCaptureContext } 'production gate rejects ordinary local use'
    Reject { Assert-OriginalCaptureRegularPath '../relative' } 'relative path refused'
    $linkTarget=Join-Path $root 'link-target'
    [void][IO.Directory]::CreateDirectory($linkTarget)
    $sentinel=Join-Path $linkTarget 'preserve.txt'
    [IO.File]::WriteAllText($sentinel,'original target bytes')
    Reject { Remove-TestDirectoryLink $linkTarget } 'link cleanup refuses ordinary target directory'
    New-Item -ItemType SymbolicLink -Path $link -Target $linkTarget | Out-Null
    try {
        Reject { Assert-OriginalCaptureRegularPath (Join-Path $link 'new.png') } 'linked ancestor refused'
    } finally { Remove-TestDirectoryLink $link }
    Assert ([IO.Directory]::Exists($linkTarget) -and [IO.File]::ReadAllText($sentinel) -ceq 'original target bytes') 'link cleanup preserves target and original bytes'
    # Real owned child supervisor, offline. No desktop or target process exists.
    function New-TestStart([string]$Code) {
        $s=[Diagnostics.ProcessStartInfo]::new();$s.FileName=(Get-Process -Id $PID).Path
        $s.UseShellExecute=$false;$s.RedirectStandardInput=$true;$s.RedirectStandardOutput=$true;$s.RedirectStandardError=$true
        $s.Arguments='-NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($Code));return $s
    }
    . (Join-Path $PSScriptRoot 'original-desktop-window-capture-supervisor.ps1')
    Initialize-OriginalCaptureSupervisor
    # Exercise caught compiler/source failures with a real bounded child and no
    # compiler/native window invocation. Raw fixture error text must not escape.
    $fakeNative=Join-Path $root 'compile-failure.ps1'
    [IO.File]::WriteAllText($fakeNative,"function Initialize-OriginalCaptureNative { throw [Management.Automation.ErrorRecord]::new([Exception]::new('CS0246 private fixture path'),'SOURCE_CODE_ERROR',[Management.Automation.ErrorCategory]::InvalidData,`$null) }")
    foreach ($probeCase in @(@($fakeNative,'compiler-error-cs0246'),@((Join-Path $root 'missing-native.ps1'),'source-error'))) {
        $probeChild=[OriginalCaptureSupervisor.BoundedProcess]::new((New-TestStart (New-TestCompileCode $probeCase[0])),256,8000)
        try {
            $probeChild.CloseInput()
            Assert ($probeChild.Wait(8000) -and $probeChild.Exited) 'fixed error probe exits within budget'
            $probeWatch=[Diagnostics.Stopwatch]::StartNew()
            while (-not $probeChild.Drained -and $probeWatch.ElapsedMilliseconds -lt 1000) { Start-Sleep -Milliseconds 10 }
            $probeFacts=Get-TestCompileFacts $probeChild
            Assert ($probeFacts.phase -ceq $probeCase[1] -and $probeFacts.category -eq 'exit-not-zero' -and
                -not $probeFacts.stderrPresent -and $probeFacts.streamsComplete) 'caught probe failure emits fixed category and nonzero exit'
            Assert ($probeChild.Output -notmatch 'private fixture path') 'caught compile error text never leaves child'
        } finally { $probeChild.Dispose() }
    }
    $associated=[OriginalCaptureSupervisor.BoundedProcess]::TestFailureDuringStart((New-TestStart 'Start-Sleep -Seconds 60'),$true)
    Assert ($associated[0] -and $associated[1]) 'throwing start with retained association is stopped and exit confirmed'
    $unassociated=[OriginalCaptureSupervisor.BoundedProcess]::TestFailureDuringStart((New-TestStart 'Start-Sleep -Seconds 60'),$false)
    Assert (-not $unassociated[0] -and -not $unassociated[1]) 'throwing start without association remains cleanup unknown'
    $protocol=Get-OriginalCaptureProtocol
    $successText=$protocol+'{"schema":1,"status":"captured"}'
    $successCode="[Console]::Out.Write('$successText')"
    $ok=Invoke-OriginalCaptureChild (New-TestStart $successCode)
    Assert ($ok.Success -and $ok.ExitConfirmed -and $ok.Phase -eq 'capture-complete') 'exact schema and exit accepted'
    Assert ([Text.Encoding]::UTF8.GetByteCount($successText) -le 256) 'complete phase protocol stays within original output cap'
    $prefixFailure=Invoke-OriginalCaptureChild (New-TestStart '[Console]::Out.Write("worker-entered`nnative-compile`n");exit 1')
    Assert (-not $prefixFailure.Success -and $prefixFailure.ExitConfirmed -and $prefixFailure.Phase -eq 'native-compile') 'failed helper reports only validated last phase'
    $missingReport=Invoke-OriginalCaptureChild (New-TestStart "[Console]::Out.Write('$protocol')")
    Assert (-not $missingReport.Success -and $missingReport.Phase -eq 'capture-complete') 'phase completion alone never authorizes capture success'
    foreach ($code in @('[Console]::Out.Write(''{"schema":1,"status":"captured","extra":true}'')',
        '[Console]::Out.Write(''{"schema":1,"status":"captured"}'');[Console]::Error.Write(''private'')',
        '[Console]::Out.Write((''x''*10000))','exit 0')) {
        $bad=Invoke-OriginalCaptureChild (New-TestStart $code)
        Assert (-not $bad.Success -and $bad.ExitConfirmed) 'invalid/missing/overflow/stderr output never accepted'
    }
    $timeout=Invoke-OriginalCaptureChild (New-TestStart 'Start-Sleep -Seconds 60') 200
    Assert (-not $timeout.Success -and $timeout.ExitConfirmed -and $timeout.Reason -eq 'deadline') 'deadline kills and reaps only capture child'
    Assert ((Get-OriginalCapturePhase '') -eq 'not-observed') 'empty output remains unknown'
    Assert ((Get-OriginalCapturePhase "worker-entered`nnative-compile`n") -eq 'native-compile') 'complete ordered phase prefix accepted'
    foreach ($badPhase in @("worker-entered`nnative-comp", "native-compile`n", "worker-entered`nprivate path")) {
        Assert ((Get-OriginalCapturePhase $badPhase) -eq 'unverified') 'partial arbitrary and reordered phase data rejected'
    }
    $phaseStart=New-TestStart '[Console]::Out.Write("worker-entered`nnative-compile`n");[Console]::Out.Flush();Start-Sleep -Seconds 60'
    $phaseChild=[OriginalCaptureSupervisor.BoundedProcess]::new($phaseStart,256,8000)
    try {
        $phaseChild.CloseInput();$phaseWatch=[Diagnostics.Stopwatch]::StartNew()
        while ($phaseChild.Output -cne "worker-entered`nnative-compile`n" -and $phaseWatch.ElapsedMilliseconds -lt 6000) { Start-Sleep -Milliseconds 10 }
        Assert ($phaseChild.Output -ceq "worker-entered`nnative-compile`n") 'phase fixture reached compile before timeout injection'
        $phaseChild.Deadline(100)
        Assert ($phaseChild.Wait(3000) -and $phaseChild.Exited -and $phaseChild.TimedOut) 'phase fixture exits on unchanged supervisor deadline mechanism'
        Assert ((Get-OriginalCapturePhase $phaseChild.Output) -eq 'native-compile') 'last flushed phase survives killed child'
    } finally { $phaseChild.Dispose() }
    if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) {
        # Same Desktop executable and 8s child budget as production, but no target
        # process/window and no Capture call. This isolates startup plus Add-Type.
        # It inherits fixture environment, not the live child environment allowlist.
        Assert ($PSVersionTable.PSEdition -eq 'Desktop') 'Windows fixture uses production Desktop host'
        $nativeScript=Join-Path $PSScriptRoot 'original-desktop-window-capture-native.ps1'
        $compileCode=New-TestCompileCode $nativeScript
        $compileWatch=[Diagnostics.Stopwatch]::StartNew()
        $compileChild=[OriginalCaptureSupervisor.BoundedProcess]::new((New-TestStart $compileCode),256,8000)
        try {
            $compileChild.CloseInput()
            $compileWaited=$compileChild.Wait(8000)
            if (-not $compileWaited -or -not $compileChild.Exited) {
                'COMPILE_PROBE '+((Get-TestCompileFacts $compileChild) | ConvertTo-Json -Compress)
                Assert $false 'native startup/compile child exits within existing budget'
            }
            $drainWatch=[Diagnostics.Stopwatch]::StartNew()
            while (-not $compileChild.Drained -and $drainWatch.ElapsedMilliseconds -lt 1000) { Start-Sleep -Milliseconds 10 }
            $compileFacts=Get-TestCompileFacts $compileChild
            'COMPILE_PROBE '+($compileFacts | ConvertTo-Json -Compress)
            Assert ($compileFacts.category -ceq 'none') 'native startup/compile succeeds without window access'
            "PASS: Windows Desktop startup/native compile elapsedMs=$($compileWatch.ElapsedMilliseconds)"
        } finally { $compileChild.Dispose() }
        Initialize-OriginalCaptureNative
        Add-Type -AssemblyName System.Drawing
        $bitmap=[Drawing.Bitmap]::new(2,2);$memory=[IO.MemoryStream]::new()
        try {
            $bitmap.Save($memory,[Drawing.Imaging.ImageFormat]::Png)
            $png=$memory.ToArray()
            Assert ([OriginalCapture.Native]::ValidPng($png)) 'real bounded PNG accepted'
            $png[0]=0
            Assert (-not [OriginalCapture.Native]::ValidPng($png)) 'invalid PNG signature rejected'
            Assert (-not [OriginalCapture.Native]::ValidPng([byte[]]::new(10485761))) 'oversized PNG rejected'
            Assert (-not [OriginalCapture.Native]::ValidPng([byte[]](137,80,78,71))) 'truncated PNG rejected'
        } finally { $bitmap.Dispose();$memory.Dispose() }
    } else { 'NOT RUN: Windows native PNG decoder and PrintWindow' }
    # Orchestrator seams isolate Windows native code, not publication/attempt logic.
    function Assert-OriginalCaptureContext {}
    function Assert-OriginalCapturePaths {}
    function Initialize-OriginalCaptureNative {}
    function Test-OriginalCaptureIdentity { $script:checks++;return ($script:checks -ne $script:drift) }
    function New-OriginalCaptureStart { return @{} }
    function Invoke-OriginalCaptureChild {
        $script:starts++
        [IO.File]::WriteAllBytes((Join-Path $script:fixture 'window-capture/original-magpie-uncertain.png'),[byte[]](1,2,3))
        return @{Success=$script:success;ExitConfirmed=$script:exitConfirmed;Reason='child-failed';Phase='capture-complete'}
    }
    function Read-OriginalCaptureImage {
        if ($script:invalidImage) { throw 'invalid private image' }
        return ,[byte[]](137,80,78,71)
    }
    function Run-Case([string]$Name,[int]$Drift=0,[bool]$Success=$true,[bool]$ExitConfirmed=$true,[bool]$InvalidImage=$false) {
        $script:fixture=Join-Path $root $Name;$script:evidence=Join-Path $root ($Name+'-evidence')
        [void][IO.Directory]::CreateDirectory($script:fixture);[void][IO.Directory]::CreateDirectory($script:evidence)
        $script:checks=0;$script:drift=$Drift;$script:starts=0;$script:success=$Success;$script:exitConfirmed=$ExitConfirmed;$script:invalidImage=$InvalidImage
        return Invoke-OriginalDesktopWindowCapture -OwnedProcess (Get-Process -Id $PID) -ExpectedExecutable 'fixture' -PrivateDirectory $script:fixture -EvidenceDirectory $script:evidence -SequenceReportSaved
    }
    foreach ($n in @(1,2,3,4)) {
        $r=Run-Case ('drift'+$n) $n
        Assert (-not $r.published -and $r.reason -eq 'identity') "identity drift $n refused"
        Assert (-not (Test-Path (Join-Path $script:evidence 'original-magpie-uncertain.png'))) 'identity drift publishes nothing'
    }
    foreach ($case in @(@('child-failure',$false,$true,$false),@('exit-unknown',$true,$false,$false),@('invalid-image',$true,$true,$true))) {
        $r=Run-Case $case[0] 0 $case[1] $case[2] $case[3]
        Assert (-not $r.published) 'failed child, unknown exit or invalid PNG refuses publication'
        Assert (-not (Test-Path (Join-Path $script:evidence 'original-magpie-uncertain.png'))) 'partial image stays private'
        Assert (Test-Path (Join-Path $script:fixture 'window-capture/original-magpie-uncertain.png')) 'private partial retained'
    }
    $r=Run-Case 'success'
    Assert ($r.published -and $r.status -eq 'captured' -and $script:starts -eq 1) 'one successful attempt publishes'
    Assert (($r.PSObject.Properties.Name -join ',') -ceq 'schema,scope,status,reason,phase,childExitConfirmed,published') 'fixed public fields only'
    $before=[IO.File]::ReadAllBytes((Join-Path $script:evidence 'original-magpie-uncertain.png'))
    $again=Invoke-OriginalDesktopWindowCapture -OwnedProcess (Get-Process -Id $PID) -ExpectedExecutable fixture -PrivateDirectory $script:fixture -EvidenceDirectory $script:evidence -SequenceReportSaved
    Assert (-not $again.published -and $script:starts -eq 1) 'marker prevents retry'
    Assert ([Convert]::ToBase64String($before) -ceq [Convert]::ToBase64String([IO.File]::ReadAllBytes((Join-Path $script:evidence 'original-magpie-uncertain.png')))) 'never overwrite existing evidence'
    $script:fixture=Join-Path $root 'no-report';[void][IO.Directory]::CreateDirectory($script:fixture)
    $r=Invoke-OriginalDesktopWindowCapture -OwnedProcess (Get-Process -Id $PID) -PrivateDirectory $script:fixture
    Assert ($r.status -eq 'blocked' -and -not (Test-Path (Join-Path $script:fixture 'window-capture.attempt'))) 'saved-report attestation required before attempt'
    'PASS: capture context, ancestor links, real bounded children, identity drift, publication, no retry and no overwrite'
} finally {
    # If unlinking fails, stop here rather than letting Desktop's recursive cleanup
    # encounter a surviving directory link. The private fixture may be retained.
    Remove-TestDirectoryLink $link
    Remove-Item -LiteralPath $root -Recurse -Force
}
