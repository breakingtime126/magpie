# Classify two read-only hit snapshots. Raw properties never leave this function.
function Get-OriginalTrayHit([scriptblock]$ReadHit,$Rectangle,[scriptblock]$Verify) {
    try {
        & $Verify
        $first=& $ReadHit
        & $Verify
        $second=& $ReadHit
        & $Verify
        if ($null -eq $first -or $null -eq $second) { return 'unavailable' }
        if ([string]::IsNullOrEmpty($first.runtime) -or $first.runtime -cne $second.runtime) { return 'unavailable' }
        foreach ($hit in @($first,$second)) {
            if ($hit.name -cne 'magpie' -or $hit.role -cne 'ControlType.Button' -or
                $hit.enabled -ne $true -or $hit.offscreen -ne $false -or $hit.invoke -ne $true -or
                $hit.left -ne $Rectangle.left -or $hit.top -ne $Rectangle.top -or
                $hit.right -ne $Rectangle.right -or $hit.bottom -ne $Rectangle.bottom) { return 'mismatch' }
        }
        return 'matched'
    } catch { return 'unavailable' }
}
# Pure bounded discovery reducer. The caller supplies read-only OS operations.
function Get-OriginalTrayObservation {
    param([int]$TargetProcessId,[scriptblock]$NextWindow,[scriptblock]$Owner,
        [scriptblock]$Rectangle,[scriptblock]$Verify)
    $r=[ordered]@{schema=1;scope='original-magpie-tray-read-only';status='blocked';reason='none';
        actions=0;quitVerified=$false;visibilityVerified=$false;geometryValid=$false;
        classWindows=0;ownedWindows=0;rectSuccesses=0;rectFailures=0;validRectangles=0;uid=1;hitTest='unavailable'}
    $seen=[Collections.Generic.HashSet[long]]::new();$last=0L
    try {
        while ($true) {
            & $Verify
            $next=[long](& $NextWindow $last)
            if ($next -eq 0) { break }
            if ($r.classWindows -ge 256 -or -not $seen.Add($next)) { $r.reason='limit';throw 'Enumeration bound.' }
            $r.classWindows++;$last=$next
            if ([int](& $Owner $next) -ne $TargetProcessId) { continue }
            $r.ownedWindows++
            & $Verify
            $rect=& $Rectangle $next
            # Re-check HWND ownership after the shell call; generation too.
            if ([int](& $Owner $next) -ne $TargetProcessId) { $r.reason='identity';throw 'Owner changed.' }
            & $Verify
            if ($rect.success -ne $true) { $r.rectFailures++;continue }
            $r.rectSuccesses++
            if ($rect.right -gt $rect.left -and $rect.bottom -gt $rect.top -and
                $rect.screenWidth -gt 0 -and $rect.screenHeight -gt 0 -and
                $rect.left -ge $rect.screenX -and $rect.top -ge $rect.screenY -and
                $rect.right -le ([long]$rect.screenX+$rect.screenWidth) -and
                $rect.bottom -le ([long]$rect.screenY+$rect.screenHeight)) { $r.validRectangles++ }
        }
        & $Verify
        if ($r.ownedWindows -eq 1 -and $r.rectSuccesses -eq 1 -and $r.rectFailures -eq 0 -and $r.validRectangles -eq 1) {
            $r.status='observed';$r.geometryValid=$true
            if ($rect.hitTest -cin @('matched','mismatch','unavailable')) { $r.hitTest=$rect.hitTest }
        } else { $r.reason='unavailable' }
    } catch {
        $r.status='blocked';$r.geometryValid=$false
        if ($r.reason -ceq 'none') { $r.reason='read' }
    }
    return $r
}
function Assert-OriginalTrayReport($r) {
    $fields=@('schema','scope','status','reason','actions','quitVerified','visibilityVerified','geometryValid',
        'classWindows','ownedWindows','rectSuccesses','rectFailures','validRectangles','uid','hitTest')
    if ($r -isnot [Collections.IDictionary] -or $r.Count -ne $fields.Count) { throw 'Report fields.' }
    foreach ($name in $fields) { if ($name -cnotin @($r.Keys)) { throw 'Report field missing.' } }
    if ($r.schema -cne 1 -or $r.scope -cne 'original-magpie-tray-read-only' -or $r.actions -cne 0 -or $r.uid -cne 1 -or
        $r.hitTest -cnotin @('matched','mismatch','unavailable') -or $r.status -cnotin @('blocked','observed') -or $r.reason -cnotin @('none','context','identity','limit','read','unavailable') -or
        $r.quitVerified -isnot [bool] -or $r.quitVerified -ne $false -or
        $r.visibilityVerified -isnot [bool] -or $r.visibilityVerified -ne $false -or $r.geometryValid -isnot [bool]) { throw 'Report contract.' }
    foreach ($name in @('classWindows','ownedWindows','rectSuccesses','rectFailures','validRectangles')) {
        if (($r[$name] -isnot [int] -and $r[$name] -isnot [long]) -or $r[$name] -lt 0 -or $r[$name] -gt 256) { throw 'Report count.' }
    }
    if ($r.ownedWindows -gt $r.classWindows -or $r.rectSuccesses+$r.rectFailures -gt $r.ownedWindows -or
        $r.validRectangles -gt $r.rectSuccesses) { throw 'Inconsistent report.' }
    if ($r.status -ceq 'observed') {
        if ($r.reason -cne 'none' -or -not $r.geometryValid -or $r.ownedWindows -ne 1 -or
            $r.rectSuccesses -ne 1 -or $r.rectFailures -ne 0 -or $r.validRectangles -ne 1) { throw 'Observation incomplete.' }
    } elseif ($r.geometryValid -or $r.hitTest -cne 'unavailable') { throw 'Blocked geometry.' }
}
