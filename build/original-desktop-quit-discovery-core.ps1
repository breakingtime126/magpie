# Fixed comparison facts only: each count is the number of matching snapshots.
function New-OriginalTrayHitFacts {
    return [ordered]@{hitSamples=0;hitNameMatches=0;hitButtonMatches=0;hitEnabledMatches=0;
        hitOnscreenMatches=0;hitInvokeMatches=0;hitExactBoundsMatches=0;hitContainsIconMatches=0;
        hitStableIdentity='unavailable'}
}
# Classify two read-only hit snapshots. Raw properties never leave this function.
function Get-OriginalTrayHit([scriptblock]$ReadHit,$Rectangle,[scriptblock]$Verify,[Collections.IDictionary]$Facts=(New-OriginalTrayHitFacts)) {
    $initial=New-OriginalTrayHitFacts;foreach ($key in $initial.Keys) { $Facts[$key]=$initial[$key] }
    try {
        & $Verify
        $first=& $ReadHit
        & $Verify
        $second=& $ReadHit
        & $Verify
        if ($null -eq $first -or $null -eq $second) { return 'unavailable' }
        $Facts.hitSamples=2
        if (-not [string]::IsNullOrEmpty($first.runtime) -and -not [string]::IsNullOrEmpty($second.runtime)) {
            $Facts.hitStableIdentity=if ($first.runtime -ceq $second.runtime) { 'same' } else { 'different' }
        }
        foreach ($sample in @($first,$second)) {
            if ($sample.name -ceq 'magpie') { $Facts.hitNameMatches++ }
            if ($sample.role -ceq 'ControlType.Button') { $Facts.hitButtonMatches++ }
            if ($sample.enabled -eq $true) { $Facts.hitEnabledMatches++ }
            if ($sample.offscreen -eq $false) { $Facts.hitOnscreenMatches++ }
            if ($sample.invoke -eq $true) { $Facts.hitInvokeMatches++ }
            if ($sample.left -eq $Rectangle.left -and $sample.top -eq $Rectangle.top -and
                $sample.right -eq $Rectangle.right -and $sample.bottom -eq $Rectangle.bottom) { $Facts.hitExactBoundsMatches++ }
            if ($sample.left -le $Rectangle.left -and $sample.top -le $Rectangle.top -and
                $sample.right -ge $Rectangle.right -and $sample.bottom -ge $Rectangle.bottom) { $Facts.hitContainsIconMatches++ }
        }
        if ([string]::IsNullOrEmpty($first.runtime) -or $first.runtime -cne $second.runtime) { return 'unavailable' }
        foreach ($hit in @($first,$second)) {
            if ($hit.name -cne 'magpie' -or $hit.role -cne 'ControlType.Button' -or
                $hit.enabled -ne $true -or $hit.offscreen -ne $false -or $hit.invoke -ne $true -or
                $hit.left -ne $Rectangle.left -or $hit.top -ne $Rectangle.top -or
                $hit.right -ne $Rectangle.right -or $hit.bottom -ne $Rectangle.bottom) { return 'mismatch' }
        }
        return 'matched'
    } catch {
        $empty=New-OriginalTrayHitFacts;foreach ($key in $empty.Keys) { $Facts[$key]=$empty[$key] }
        return 'unavailable'
    }
}
# Pure bounded discovery reducer. The caller supplies read-only OS operations.
function Get-OriginalTrayObservation {
    param([int]$TargetProcessId,[scriptblock]$NextWindow,[scriptblock]$Owner,
        [scriptblock]$Rectangle,[scriptblock]$Verify)
    $r=[ordered]@{schema=1;scope='original-magpie-tray-read-only';status='blocked';reason='none';
        actions=0;quitVerified=$false;visibilityVerified=$false;geometryValid=$false;
        classWindows=0;ownedWindows=0;rectSuccesses=0;rectFailures=0;validRectangles=0;uid=1;hitTest='unavailable'}
    $emptyFacts=New-OriginalTrayHitFacts;foreach ($key in $emptyFacts.Keys) { $r[$key]=$emptyFacts[$key] }
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
            foreach ($key in $emptyFacts.Keys) { $r[$key]=$rect.hitFacts[$key] }
        } else { $r.reason='unavailable' }
    } catch {
        $r.status='blocked';$r.geometryValid=$false;$r.hitTest='unavailable'
        foreach ($key in $emptyFacts.Keys) { $r[$key]=$emptyFacts[$key] }
        if ($r.reason -ceq 'none') { $r.reason='read' }
    }
    return $r
}
function Assert-OriginalTrayReport($r) {
    $fields=@('schema','scope','status','reason','actions','quitVerified','visibilityVerified','geometryValid',
        'classWindows','ownedWindows','rectSuccesses','rectFailures','validRectangles','uid','hitTest')
    $factNames=New-OriginalTrayHitFacts;$fields+=@($factNames.Keys)
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
    if ($r.hitSamples -isnot [int] -and $r.hitSamples -isnot [long]) { throw 'Hit sample type.' }
    if ($r.hitSamples -notin @(0,2) -or $r.hitStableIdentity -cnotin @('unavailable','same','different')) { throw 'Hit facts invalid.' }
    foreach ($name in $factNames.Keys) {
        if ($name -cin @('hitSamples','hitStableIdentity')) { continue }
        if (($r[$name] -isnot [int] -and $r[$name] -isnot [long]) -or $r[$name] -lt 0 -or $r[$name] -gt $r.hitSamples) { throw 'Hit count invalid.' }
    }
    if ($r.hitExactBoundsMatches -gt $r.hitContainsIconMatches -or
        ($r.hitSamples -eq 0 -and $r.hitStableIdentity -cne 'unavailable')) { throw 'Hit comparisons inconsistent.' }
    if ($r.hitTest -ceq 'matched') {
        if ($r.hitSamples -ne 2 -or $r.hitStableIdentity -cne 'same') { throw 'Matched identity absent.' }
        foreach ($name in $factNames.Keys) {
            if ($name -cin @('hitSamples','hitStableIdentity')) { continue }
            if ($r[$name] -ne 2) { throw 'Matched predicate absent.' }
        }
    }
    if ($r.status -cne 'observed' -and $r.hitSamples -ne 0) { throw 'Ambiguous hit facts.' }
    if ($r.status -ceq 'observed') {
        if ($r.reason -cne 'none' -or -not $r.geometryValid -or $r.ownedWindows -ne 1 -or
            $r.rectSuccesses -ne 1 -or $r.rectFailures -ne 0 -or $r.validRectangles -ne 1) { throw 'Observation incomplete.' }
    } elseif ($r.geometryValid -or $r.hitTest -cne 'unavailable') { throw 'Blocked geometry.' }
}
