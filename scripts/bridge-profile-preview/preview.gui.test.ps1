# Own WinForms controls only; the system selectors are not automated or accepted.
param([Parameter(Mandatory)][string]$PackageDirectory, [Parameter(Mandatory)][string]$EvidenceDirectory, [Parameter(Mandatory)][string]$IdentityFixture)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PackageDirectory 'preview.ps1')
if (-not [Environment]::UserInteractive) { throw 'No interactive runner session; GUI not verified.' }
$root = Join-Path ([IO.Path]::GetTempPath()) ('bridge-helper-ui-' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $root -ErrorAction Stop)
[void](New-Item -ItemType Directory -Path $EvidenceDirectory -ErrorAction Stop)
$before = [Environment]::GetEnvironmentVariables('Process')
$window = $null; $timer = $null
$report = [ordered]@{ shown=$false; realAdapterCheck=$false; repeatNoOverwrite=$false; invalidated=$false; homesUnchanged=$false; environmentUnchanged=$false; rollback=$false; selectors='mocked-not-verified'; capture='own-control-render-not-screen'; identityConsent='mocked-not-verified'; identityCancelled=$false; identitySynthetic=$false; identityInvalidated=$false; identitySnapshotRemoved=$false; outcome='failed' }
$state = @{ Stage=0; Error=$null; First=$null; FirstBytes=$null; Second=$null }
$consent = @{ Allow=$false }
$originalConsent = (Get-Item Function:Confirm-BridgePrimaryIdentity).ScriptBlock
$originalSelector = (Get-Item Function:Select-BridgeProfileParent).ScriptBlock
try {
    $parent = Join-Path $root 'profiles'
    [void](New-Item -ItemType Directory -Path $parent)
    # Deliberately invalid executable bytes. --check inspects but must never run it.
    $exe = Join-Path $root 'synthetic-never-run.exe'
    [IO.File]::WriteAllText($exe, 'PUBLIC-NONEXECUTABLE-FIXTURE')
    $rows = @(); $homes = @()
    foreach ($id in @('alpha', 'beta')) {
        $h = Join-Path $root ($id + '-home'); $c = Join-Path $root ($id + '-codex')
        [void](New-Item -ItemType Directory -Path $h)
        [void](New-Item -ItemType Directory -Path $c)
        $homes += @($h, $c)
        $rows += @{id=$id;provider=('fixture_' + $id);command=$exe;home=$h;codex_home=$c}
    }
    # Separate identity homes: these intentionally acquire only synthetic peer logs.
    # The original Save/check homes remain empty and keep their own assertion.
    $identityRows = @()
    foreach ($id in @('alpha','beta')) {
        $h = Join-Path $root ('identity-' + $id + '-home'); $c = Join-Path $root ('identity-' + $id + '-codex')
        [void](New-Item -ItemType Directory -Path $h)
        [void](New-Item -ItemType Directory -Path $c)
        $identityRows += @{id=$id;provider='openai';command=$IdentityFixture;home=$h;codex_home=$c}
    }
    $identityPrimary = $identityRows[0].home
    [IO.File]::WriteAllText((Join-Path $identityPrimary 'fixture-primary-identity'), 'normal')
    [IO.File]::WriteAllText((Join-Path $identityPrimary 'identity-response'), '{"id":"identity-status","result":{"authMethod":"chatgpt","authToken":null,"requiresOpenaiAuth":true}}')
    Set-Item Function:Confirm-BridgePrimaryIdentity ({ return $consent.Allow }.GetNewClosure())
    # Only selection is substituted, never Save or the real checker.
    Set-Item Function:Select-BridgeProfileParent ({ return $parent }.GetNewClosure())
    $window = New-BridgeProfileWindow
    if ($window.IdentityStatus.Bottom -gt $window.Form.ClientSize.Height -or $window.Identity.Right -gt $window.Form.ClientSize.Width) { throw 'Identity controls exceed the client area.' }
    for ($i=0; $i -lt 2; $i++) {
        foreach ($key in $rows[$i].Keys) { $window.Rows[$i][$key].Text = $rows[$i][$key] }
    }
    $timer = New-Object Windows.Forms.Timer
    $timer.Interval = 500
    $timer.Add_Tick({
        $timer.Stop()
        try {
            if (-not $window.Form.Visible -or -not $window.Form.IsHandleCreated) { throw 'Owned form not visible.' }
            $report.shown = $true
            switch ($state.Stage) {
                0 {
                    $window.Save.PerformClick()
                    if (-not $window.Status.Text.StartsWith('Saved; structure/path check passed.')) { throw 'Real check not passed.' }
                    $state.First = $window.Preview.Text
                    $state.FirstBytes = [IO.File]::ReadAllText($state.First)
                    $report.realAdapterCheck = $true
                }
                1 {
                    $window.Save.PerformClick()
                    $state.Second = $window.Preview.Text
                    if (-not $window.Status.Text.StartsWith('Saved; structure/path check passed.') -or
                        $state.Second -ceq $state.First -or
                        [IO.File]::ReadAllText($state.First) -cne $state.FirstBytes) { throw 'Repeat save changed original or failed.' }
                    $report.repeatNoOverwrite = $true
                }
                2 {
                    $window.Rows[0].provider.Text = 'fixture_changed'
                    if ($window.Preview.Text.Length -ne 0 -or -not $window.Status.Text.StartsWith('Fields changed: previous check no longer applies.')) { throw 'Old result remained valid.' }
                    $report.invalidated = $true
                }
                3 {
                    for ($i=0; $i -lt 2; $i++) {
                        foreach ($key in $identityRows[$i].Keys) { $window.Rows[$i][$key].Text = $identityRows[$i][$key] }
                    }
                    $window.Identity.PerformClick()
                    if (-not $window.IdentityStatus.Text.StartsWith('Cancelled before launch;') -or
                        (Test-Path -LiteralPath (Join-Path $identityPrimary 'fixture-report'))) { throw 'Cancelled inspection launched or misreported.' }
                    $report.identityCancelled = $true
                }
                4 {
                    $consent.Allow = $true
                    $window.Identity.PerformClick()
                    if ($window.IdentityStatus.Text -cne 'Native identity type: chatgpt; requiresOpenaiAuth=True. Not a chat-readiness result.') { throw 'Synthetic identity projection failed.' }
                    if (-not (Test-Path -LiteralPath (Join-Path $identityPrimary 'identity-clean-exit'))) { throw 'Synthetic peer did not exit cleanly.' }
                    $requests = @(Get-Content -LiteralPath (Join-Path $identityPrimary 'identity-requests') | ForEach-Object { $_ | ConvertFrom-Json })
                    if ($requests.Count -ne 3 -or $requests[0].method -cne 'initialize' -or $requests[1].method -cne 'initialized' -or
                        $requests[2].method -cne 'getAuthStatus' -or $requests[2].params.includeToken -ne $false -or $requests[2].params.refreshToken -ne $false) { throw 'Unexpected synthetic identity exchange.' }
                    foreach ($h in @($identityRows[1].home, $identityRows[1].codex_home)) {
                        if (@(Get-ChildItem -LiteralPath $h -Force).Count -ne 0) { throw 'Secondary identity context changed.' }
                    }
                    if (@(Get-ChildItem -LiteralPath $parent -Force).Count -ne 2 -or [IO.File]::ReadAllText($state.First) -cne $state.FirstBytes) { throw 'Snapshot remains or old profile changed.' }
                    $report.identitySnapshotRemoved = $true
                    $report.identitySynthetic = $true
                }
                5 {
                    $window.Rows[0].provider.Text = 'changed'
                    if ($window.IdentityStatus.Text -cne 'Fields changed: previous identity result no longer applies.') { throw 'Old identity result remained valid.' }
                    $report.identityInvalidated = $true
                }
                6 { $window.Form.Close(); return }
            }
            # Renders only our owned control tree, not unrelated desktop pixels.
            $window.Form.Refresh()
            $bitmap = New-Object Drawing.Bitmap($window.Form.Width, $window.Form.Height)
            try {
                $window.Form.DrawToBitmap($bitmap, $window.Form.ClientRectangle)
                $bitmap.Save((Join-Path $EvidenceDirectory ('stage-' + $state.Stage + '.png')), [Drawing.Imaging.ImageFormat]::Png)
            } finally { $bitmap.Dispose() }
            $state.Stage++
            $timer.Start()
        } catch {
            $state.Error = 'Owned-control save/check acceptance failed; inspect fixed stage flags.'
            $window.Form.Close()
        }
    })
    $window.Form.Add_Shown({ $timer.Start() })
    [void]$window.Form.ShowDialog()
    if ($null -ne $state.Error -or $state.Stage -ne 6) { throw 'GUI sequence did not complete.' }
    foreach ($h in $homes) {
        if (@(Get-ChildItem -LiteralPath $h -Force -ErrorAction Stop).Count -ne 0) { throw 'Backend home changed.' }
    }
    $report.homesUnchanged = $true
    $after = [Environment]::GetEnvironmentVariables('Process')
    if ($before.Count -ne $after.Count) { throw 'Process environment count changed.' }
    foreach ($key in $before.Keys) { if ($after[$key] -cne $before[$key]) { throw 'Process environment changed.' } }
    $report.environmentUnchanged = $true
    # Only the two paths produced by this attempt, constrained to our fresh parent.
    foreach ($p in @($state.First, $state.Second)) {
        $dir = [IO.Path]::GetDirectoryName($p)
        if ([IO.Path]::GetDirectoryName($dir) -cne $parent -or [IO.Path]::GetFileName($p) -cne 'bridge.json') { throw 'Unexpected rollback path.' }
        Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction Stop
        if (Test-Path -LiteralPath $dir) { throw 'Profile rollback incomplete.' }
    }
    if (@(Get-ChildItem -LiteralPath $parent -Force).Count -ne 0) { throw 'Unexpected profile remains.' }
    $report.rollback = $true
    $report.outcome = 'passed'
} finally {
    if ($null -ne $timer) { $timer.Stop(); $timer.Dispose() }
    if ($null -ne $window) { $window.Form.Close(); $window.Form.Dispose() }
    Set-Item Function:Select-BridgeProfileParent $originalSelector
    Set-Item Function:Confirm-BridgePrimaryIdentity $originalConsent
    try { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction Stop }
    catch { $report.outcome = 'cleanup-failed'; throw }
    finally { $report | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $EvidenceDirectory 'report.json') -Encoding UTF8 }
}
'PASS: own window save/check, repeat save, invalidation and rollback; no Desktop or authentication acceptance'
