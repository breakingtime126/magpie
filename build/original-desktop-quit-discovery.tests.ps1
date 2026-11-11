# Offline reducer, report and source-bound tests. No worker body is executed.
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-desktop-quit-discovery-core.ps1')
function Need([bool]$Value) { if (-not $Value) { throw 'Tray fixture assertion failed.' } }
function Reject([scriptblock]$Action) { $failed=$false;try { & $Action } catch { $failed=$true };Need $failed }
$script:mode='ok';$script:reads=0
$next={ param($last)
    switch ($script:mode) {
        'none' { return 0L }
        'cycle' { return 1L }
        'limit' { return $last+1 }
        'duplicate' { if ($last -lt 2) { return $last+1 }; return 0L }
        'enumeration-error' { if ($last -eq 1) { throw 'Read failed.' } }
    }
    if ($last -eq 0) { return 1L };return 0L
}
$owner={param($window) if ($script:mode -ceq 'foreign') {return 8};return 7}
$rect={param($window)
    if ($script:mode -ceq 'read-error') {throw 'Shell failed.'}
    @{hitFacts=$(if($script:mode -cin @('hitmatch','duplicate')){ $facts=New-OriginalTrayHitFacts;foreach($key in @($facts.Keys)){if($key -ceq 'hitStableIdentity'){$facts[$key]='same'}else{$facts[$key]=2}};$facts }else{New-OriginalTrayHitFacts});hitTest=$(if($script:mode -cin @('hitmatch','duplicate')){'matched'}else{'unavailable'});success=($script:mode -cne 'hr-failure');left=10;top=10;right=$(if($script:mode -ceq 'empty'){10}else{30});bottom=30;
        screenX=0;screenY=0;screenWidth=$(if($script:mode -ceq 'offscreen'){20}else{1920});screenHeight=1080}
}
$verify={ $script:reads++;if ($script:mode -ceq 'identity' -and $script:reads -gt 2) {throw 'Changed.'} }
function Run-Fixture([string]$Mode) {
    $script:mode=$Mode;$script:reads=0
    $r=Get-OriginalTrayObservation 7 $next $owner $rect $verify
    Assert-OriginalTrayReport $r
    return $r
}
$r=Run-Fixture 'ok';Need ($r.status -ceq 'observed' -and $r.geometryValid -and $r.rectSuccesses -eq 1 -and $script:reads -eq 5)
Need (-not $r.visibilityVerified -and -not $r.quitVerified -and $r.actions -eq 0)
foreach ($mode in @('none','foreign','duplicate','cycle','limit','read-error','enumeration-error','hr-failure','empty','offscreen','identity')) {
    $r=Run-Fixture $mode
    Need ($r.status -ceq 'blocked' -and -not $r.geometryValid -and $r.classWindows -le 256)
}
$r=Run-Fixture 'ok'
foreach ($field in @('quitVerified','visibilityVerified')) {
    $bad=[ordered]@{};foreach ($key in $r.Keys) {$bad[$key]=$r[$key]};$bad[$field]=$true
    Reject { Assert-OriginalTrayReport $bad }
}
foreach ($pair in @(@('actions',1),@('uid',2),@('ownedWindows',2),@('rectFailures',1),@('classWindows',257),@('scope','private'))) {
    $bad=[ordered]@{};foreach ($key in $r.Keys) {$bad[$key]=$r[$key]};$bad[$pair[0]]=$pair[1]
    Reject { Assert-OriginalTrayReport $bad }
}
$bad=[ordered]@{};foreach ($key in $r.Keys) {$bad[$key]=$r[$key]};$bad['rawWindow']=123
Reject { Assert-OriginalTrayReport $bad }
foreach ($name in @('original-desktop-quit-discovery.ps1','original-desktop-quit-discovery-worker.ps1','original-desktop-quit-discovery-core.ps1')) {
    $tokens=$null;$errors=$null
    $ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $name),[ref]$tokens,[ref]$errors)
    Need ($errors.Count -eq 0)
    foreach ($call in @($ast.FindAll({param($n) $n -is [Management.Automation.Language.InvokeMemberExpressionAst]},$true))) {
        Need ($call.Member.Extent.Text -cnotin @('Invoke','Toggle','SetFocus','CloseMainWindow','SendMessage','PostMessage','SendInput','SetCursorPos'))
    }
}
$worker=Get-Content (Join-Path $PSScriptRoot 'original-desktop-quit-discovery-worker.ps1') -Raw
foreach ($required in @('new IntPtr(-3)','"WailsWebviewWindow"','GetWindowThreadProcessId','Shell_NotifyIconGetRect',
    '$id.UID=1','$id.Guid=[guid]::Empty','$watch.ElapsedMilliseconds -ge 10000',"'github-hosted'",'Assert-TrayOwner')) { Need ($worker.Contains($required)) }
foreach ($bad in @('SendMessage','PostMessage','SendInput','SetCursorPos','ReadProcessMemory','CloseMainWindow','.Kill(')) { Need (-not $worker.Contains($bad)) }
# Compile declarations only; do not call any Windows DLL on this host.
$native=[regex]::Match($worker, "(?s)Add-Type -TypeDefinition @'\r?\n(.*?)\r?\n'@")
Need $native.Success
Add-Type -TypeDefinition $native.Groups[1].Value
Need ([Runtime.InteropServices.Marshal]::SizeOf([OriginalTrayDiscovery.Identifier]::new()) -eq $(if ([IntPtr]::Size -eq 8) {40}else{28}))
Need ([Runtime.InteropServices.Marshal]::SizeOf([OriginalTrayDiscovery.Rect]::new()) -eq 16)
$goMod=Get-Content (Join-Path $PSScriptRoot '../go.mod') -Raw
Need ($goMod.Contains('github.com/wailsapp/wails/v3 v3.0.0-beta.24'))
$supervisor=Get-Content (Join-Path $PSScriptRoot 'original-desktop-quit-discovery.ps1') -Raw
foreach ($required in @('$watch.ElapsedMilliseconds -gt 15000','$texts[$i].Length+$count -gt 8192',
    '$process.Kill()','$process.WaitForExit(3000)','$start.EnvironmentVariables.Clear()','Assert-OriginalTrayReport $result')) { Need ($supervisor.Contains($required)) }
'Tray discovery offline tests passed.'

# Two stable hit snapshots are required; names and geometry remain internal.
$script:hitMode='match';$script:hits=0
$geometry=@{left=10;top=10;right=30;bottom=30}
$reader={
    $script:hits++
    if ($script:hitMode -ceq 'null') { return $null }
    if ($script:hitMode -ceq 'error') { throw 'Private error text.' }
    @{name=$(if($script:hitMode -ceq 'private'){'private account title'}elseif($script:hitMode -ceq 'case'){'MAGPIE'}else{'magpie'});
        role=$(if($script:hitMode -ceq 'role'){'ControlType.Text'}else{'ControlType.Button'});
        enabled=($script:hitMode -cne 'disabled');offscreen=($script:hitMode -ceq 'hidden');invoke=($script:hitMode -cne 'noInvoke');
        left=10;top=10;right=$(if($script:hitMode -ceq 'geometry'){31}else{30});bottom=30;
        runtime=$(if($script:hitMode -ceq 'ambiguous'){[string]$script:hits}else{'1,2'})}
}
foreach ($mode in @('match','null','error','private','case','role','disabled','hidden','noInvoke','geometry','ambiguous')) {
    $script:hitMode=$mode;$script:hits=0
    $answer=Get-OriginalTrayHit $reader $geometry {}
    $expected=if($mode -ceq 'match'){'matched'}elseif($mode -cin @('null','error','ambiguous')){'unavailable'}else{'mismatch'}
    Need ($answer -ceq $expected)
    Need ($answer -cin @('matched','mismatch','unavailable'))
}
$r=Run-Fixture 'hitmatch';Assert-OriginalTrayReport $r
$r.hitTest='private account title';Reject { Assert-OriginalTrayReport $r }
$r=Run-Fixture 'none';$r.hitTest='matched';Reject { Assert-OriginalTrayReport $r }
'Optional tray hit tests passed.'

$r=Run-Fixture 'hitmatch';Need ($r.hitTest -ceq 'matched' -and -not $r.visibilityVerified)
$r=Run-Fixture 'duplicate';Need ($r.hitTest -ceq 'unavailable')

# Predicate evidence diagnoses a containing button without changing admission.
foreach ($mode in @('match','geometry','private','role','disabled','hidden','noInvoke','ambiguous','null','error')) {
    $script:hitMode=$mode;$script:hits=0;$facts=New-OriginalTrayHitFacts
    $answer=Get-OriginalTrayHit $reader $geometry {} $facts
    if($mode -cin @('null','error')) {Need ($facts.hitSamples -eq 0);continue}
    Need ($facts.hitSamples -eq 2)
    Need ($facts.hitNameMatches -eq $(if($mode -ceq 'private'){0}else{2}))
    Need ($facts.hitButtonMatches -eq $(if($mode -ceq 'role'){0}else{2}))
    Need ($facts.hitEnabledMatches -eq $(if($mode -ceq 'disabled'){0}else{2}))
    Need ($facts.hitOnscreenMatches -eq $(if($mode -ceq 'hidden'){0}else{2}))
    Need ($facts.hitInvokeMatches -eq $(if($mode -ceq 'noInvoke'){0}else{2}))
    Need ($facts.hitExactBoundsMatches -eq $(if($mode -ceq 'geometry'){0}else{2}))
    Need ($facts.hitContainsIconMatches -eq 2)
    Need ($facts.hitStableIdentity -ceq $(if($mode -ceq 'ambiguous'){'different'}else{'same'}))
    if($mode -ceq 'geometry'){Need ($answer -ceq 'mismatch')}
    $serialized=$facts|ConvertTo-Json -Compress
    Need (-not $serialized.Contains('private account') -and -not $serialized.Contains('1,2'))
}
foreach($pair in @(@('hitNameMatches',3),@('hitSamples',1),@('hitStableIdentity','private'),@('hitEnabledMatches','2'))) {
    $r=Run-Fixture 'hitmatch';$r[$pair[0]]=$pair[1];Reject {Assert-OriginalTrayReport $r}
}
$r=Run-Fixture 'hitmatch';$r.hitExactBoundsMatches=0;Reject {Assert-OriginalTrayReport $r}
'Per-predicate tray comparison tests passed.'

$script:hits=0;$script:hitMode='match';$facts=New-OriginalTrayHitFacts
$mixed={ $h=& $reader;if($script:hits -eq 2){$h.name='unknown'};return $h }
Need ((Get-OriginalTrayHit $mixed $geometry {} $facts) -ceq 'mismatch')
Need ($facts.hitNameMatches -eq 1 -and $facts.hitExactBoundsMatches -eq 2)
$script:hitMode='null';Need ((Get-OriginalTrayHit $reader $geometry {} $facts) -ceq 'unavailable')
Need ($facts.hitSamples -eq 0 -and $facts.hitNameMatches -eq 0)
$outside={ $h=& $reader;$h.left=11;return $h };$script:hitMode='match'
Need ((Get-OriginalTrayHit $outside $geometry {} $facts) -ceq 'mismatch')
Need ($facts.hitExactBoundsMatches -eq 0 -and $facts.hitContainsIconMatches -eq 0)
