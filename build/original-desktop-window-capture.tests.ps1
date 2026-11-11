$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-desktop-window-capture.ps1')
function Assert($Condition,[string]$Message) { if (-not $Condition) { throw "FAIL: $Message" } }
function Reject($Action,[string]$Message) { $failed=$false;try { & $Action } catch { $failed=$true };Assert $failed $Message }
$root=Join-Path ([IO.Path]::GetTempPath()) ('capture-tests-'+[guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
try {
    Reject { Assert-OriginalCaptureContext } 'production gate rejects ordinary local use'
    Reject { Assert-OriginalCaptureRegularPath '../relative' } 'relative path refused'
    $link=Join-Path $root 'linked'
    New-Item -ItemType SymbolicLink -Path $link -Target $root | Out-Null
    Reject { Assert-OriginalCaptureRegularPath (Join-Path $link 'new.png') } 'linked ancestor refused'
    Remove-Item -LiteralPath $link
    # Real owned child supervisor, offline. No desktop or target process exists.
    function New-TestStart([string]$Code) {
        $s=[Diagnostics.ProcessStartInfo]::new();$s.FileName=(Get-Process -Id $PID).Path
        $s.UseShellExecute=$false;$s.RedirectStandardInput=$true;$s.RedirectStandardOutput=$true;$s.RedirectStandardError=$true
        $s.Arguments='-NoProfile -NonInteractive -EncodedCommand '+[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($Code));return $s
    }
    . (Join-Path $PSScriptRoot 'original-desktop-window-capture-supervisor.ps1')
    Initialize-OriginalCaptureSupervisor
    $associated=[OriginalCaptureSupervisor.BoundedProcess]::TestFailureDuringStart((New-TestStart 'Start-Sleep -Seconds 60'),$true)
    Assert ($associated[0] -and $associated[1]) 'throwing start with retained association is stopped and exit confirmed'
    $unassociated=[OriginalCaptureSupervisor.BoundedProcess]::TestFailureDuringStart((New-TestStart 'Start-Sleep -Seconds 60'),$false)
    Assert (-not $unassociated[0] -and -not $unassociated[1]) 'throwing start without association remains cleanup unknown'
    $ok=Invoke-OriginalCaptureChild (New-TestStart '[Console]::Out.Write(''{"schema":1,"status":"captured"}'')')
    Assert ($ok.Success -and $ok.ExitConfirmed) 'exact schema and exit accepted'
    foreach ($code in @('[Console]::Out.Write(''{"schema":1,"status":"captured","extra":true}'')',
        '[Console]::Out.Write(''{"schema":1,"status":"captured"}'');[Console]::Error.Write(''private'')',
        '[Console]::Out.Write((''x''*10000))','exit 0')) {
        $bad=Invoke-OriginalCaptureChild (New-TestStart $code)
        Assert (-not $bad.Success -and $bad.ExitConfirmed) 'invalid/missing/overflow/stderr output never accepted'
    }
    $timeout=Invoke-OriginalCaptureChild (New-TestStart 'Start-Sleep -Seconds 60') 200
    Assert (-not $timeout.Success -and $timeout.ExitConfirmed -and $timeout.Reason -eq 'deadline') 'deadline kills and reaps only capture child'
    if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) {
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
        return @{Success=$script:success;ExitConfirmed=$script:exitConfirmed;Reason='child-failed'}
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
    Assert (($r.PSObject.Properties.Name -join ',') -ceq 'schema,scope,status,reason,childExitConfirmed,published') 'fixed public fields only'
    $before=[IO.File]::ReadAllBytes((Join-Path $script:evidence 'original-magpie-uncertain.png'))
    $again=Invoke-OriginalDesktopWindowCapture -OwnedProcess (Get-Process -Id $PID) -ExpectedExecutable fixture -PrivateDirectory $script:fixture -EvidenceDirectory $script:evidence -SequenceReportSaved
    Assert (-not $again.published -and $script:starts -eq 1) 'marker prevents retry'
    Assert ([Convert]::ToBase64String($before) -ceq [Convert]::ToBase64String([IO.File]::ReadAllBytes((Join-Path $script:evidence 'original-magpie-uncertain.png')))) 'never overwrite existing evidence'
    $script:fixture=Join-Path $root 'no-report';[void][IO.Directory]::CreateDirectory($script:fixture)
    $r=Invoke-OriginalDesktopWindowCapture -OwnedProcess (Get-Process -Id $PID) -PrivateDirectory $script:fixture
    Assert ($r.status -eq 'blocked' -and -not (Test-Path (Join-Path $script:fixture 'window-capture.attempt'))) 'saved-report attestation required before attempt'
    'PASS: capture context, ancestor links, real bounded children, identity drift, publication, no retry and no overwrite'
} finally { Remove-Item -LiteralPath $root -Recurse -Force }
