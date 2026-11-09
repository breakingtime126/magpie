# An isolated configuration helper. No normal Magpie initialization is loaded.
# Dot-source to test the pure draft builder without opening a window.
Set-StrictMode -Version Latest

function New-BridgeProfileDraft {
    param([Parameter(Mandatory)][object[]]$Backends)
    if ($Backends.Count -ne 2) { throw 'Select exactly two backends.' }
    $ids = @{}
    $roots = @()
    $output = @()
    foreach ($backend in $Backends) {
        foreach ($name in @('id', 'provider')) {
            $value = [string]$backend[$name]
            if ($value -cnotmatch '^[A-Za-z0-9_-]{1,64}$') {
                throw "$name must contain 1-64 ASCII letters, digits, underscores or hyphens."
            }
        }
        if ($ids.ContainsKey([string]$backend.id)) { throw 'Backend IDs must differ (including case).' }
        $ids[[string]$backend.id] = $true
        foreach ($name in @('command', 'home', 'codex_home')) {
            $value = [string]$backend[$name]
            # This preview intentionally supports only local, rooted Windows paths.
            # No UNC, device namespace, variables, relative path or URI interpretation.
            if ($value -notmatch '^[A-Za-z]:\\' -or $value -match '[\x00-\x1f%]' -or $value.Contains('..')) {
                throw "$name must be a selected local absolute Windows path."
            }
            if ($name -eq 'command') {
                if ([IO.Path]::GetExtension($value) -ine '.exe') { throw 'Select a native .exe, not a script.' }
            } else {
                $path = $value.TrimEnd('\')
                foreach ($root in $roots) {
                    if ($path.Equals($root, [StringComparison]::OrdinalIgnoreCase) -or
                        $path.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase) -or
                        $root.StartsWith($path + '\', [StringComparison]::OrdinalIgnoreCase)) {
                        throw 'All four state directories must be distinct and not nested.'
                    }
                }
                $roots += $path
            }
        }
        # Explicit allowlist: never pass extra UI data, environment or credentials through.
        $output += [ordered]@{
            id = [string]$backend.id
            provider = [string]$backend.provider
            command = [string]$backend.command
            home = [string]$backend.home
            codex_home = [string]$backend.codex_home
        }
    }
    return ([ordered]@{ backends = $output } | ConvertTo-Json -Depth 5)
}

# Only local selected paths, with no junction/symlink traversal in this UI slice.
function Get-BridgeProfileLocalPath([string]$Path, [switch]$Directory) {
    if ($Path -notmatch '^[A-Za-z]:\\' -or $Path -match '[\x00-\x1f%]' -or $Path.Contains('..')) {
        throw 'Select an existing local absolute path.'
    }
    $full = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    if ($full.Length -lt 3) { throw 'Select a directory below the drive root.' }
    if ([IO.DriveInfo]::new([IO.Path]::GetPathRoot($full)).DriveType -eq [IO.DriveType]::Network) {
        throw 'Mapped network drives are not supported by this configuration helper.'
    }
    $item = Get-Item -LiteralPath $full -Force -ErrorAction Stop
    if ($Directory -and $item -isnot [IO.DirectoryInfo]) { throw 'Select an existing directory.' }
    while ($null -ne $item) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'Junctions and symbolic links are not supported by this configuration helper.'
        }
        if ($item -is [IO.DirectoryInfo]) { $item = $item.Parent } else { $item = $item.Directory }
    }
    return $full
}

function Test-BridgeProfileOverlap([string]$Left, [string]$Right) {
    return $Left.Equals($Right, [StringComparison]::OrdinalIgnoreCase) -or
        $Left.StartsWith($Right + '\', [StringComparison]::OrdinalIgnoreCase) -or
        $Right.StartsWith($Left + '\', [StringComparison]::OrdinalIgnoreCase)
}

function New-BridgeAdapterProcessInfo([string]$ProfilePath) {
    # The checker is fixed to this package, never selected by the UI or profile.
    # BUILD.json verifies package consistency, not signatures or trusted origin.
    $adapter = Get-BridgeProfileLocalPath (Join-Path $PSScriptRoot 'magpie-codex-bridge-windows-amd64.exe')
    $manifestPath = Get-BridgeProfileLocalPath (Join-Path $PSScriptRoot 'BUILD.json')
    $manifestInfo = Get-Item -LiteralPath $manifestPath -ErrorAction Stop
    if ($manifestInfo.Length -gt 65536) { throw 'Package manifest is invalid.' }
    $manifest = [IO.File]::ReadAllText($manifestPath) | ConvertFrom-Json
    if ([string]$manifest.nativeAdapterSHA256 -cnotmatch '^[0-9a-f]{64}$' -or
        (Get-FileHash -LiteralPath $adapter -Algorithm SHA256).Hash.ToLowerInvariant() -cne $manifest.nativeAdapterSHA256) {
        throw 'The bundled adapter does not match BUILD.json.'
    }
    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = $adapter
    $psi.WorkingDirectory = [IO.Path]::GetDirectoryName($ProfilePath)
    $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    # Only this child receives the profile. No user/process-global setting changes.
    $psi.EnvironmentVariables.Clear()
    $systemRoot = [Environment]::GetEnvironmentVariable('SystemRoot')
    if (-not [IO.Path]::IsPathRooted($systemRoot)) { throw 'System root unavailable.' }
    foreach ($name in @('SystemRoot', 'WINDIR')) { $psi.EnvironmentVariables[$name] = $systemRoot }
    $psi.EnvironmentVariables['PATH'] = Join-Path $systemRoot 'System32'
    foreach ($name in @('HOME', 'USERPROFILE', 'TEMP', 'TMP')) { $psi.EnvironmentVariables[$name] = $psi.WorkingDirectory }
    $psi.EnvironmentVariables['MAGPIE_APP_SERVER_BRIDGE_CONFIG'] = $ProfilePath
    return $psi
}

function Invoke-BridgeProfileCheck([string]$ProfilePath) {
    $psi = New-BridgeAdapterProcessInfo $ProfilePath
    $psi.Arguments = '--check'
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $psi; $started = $false
    try {
        $started = $process.Start()
        if (-not $started) { return 'failed' }
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(15000)) { return 'timeout' }
        if (-not $stdout.Wait(1000) -or -not $stderr.Wait(1000)) { return 'failed' }
        $outText = $stdout.Result; $errText = $stderr.Result
        if ($outText.Length -ne 0 -or $errText.Length -gt 4096) { return 'failed' }
        if ($process.ExitCode -eq 0 -and $errText.Trim() -ceq 'magpie-codex-bridge: configuration valid; no children started') {
            return 'passed'
        }
        return 'failed'
    } finally {
        if ($started -and -not $process.HasExited) {
            # --check never starts children. Do not terminate unrelated processes.
            $process.Kill()
            if (-not $process.WaitForExit(3000)) { throw 'Configuration checker did not stop.' }
        }
        $process.Dispose()
    }
}

function Save-BridgeProfileDraft([object[]]$Backends, [string]$ParentDirectory) {
    foreach ($backend in $Backends) {
        if ($backend -isnot [Collections.IDictionary] -or $backend.Count -ne 5) {
            throw 'Only the five displayed profile fields may be saved; no secrets or advanced options.'
        }
        foreach ($name in $backend.Keys) {
            if (@('id', 'provider', 'command', 'home', 'codex_home') -cnotcontains $name) {
                throw 'Unsupported profile field.'
            }
        }
    }
    $json = New-BridgeProfileDraft -Backends $Backends
    $parent = Get-BridgeProfileLocalPath $ParentDirectory -Directory
    foreach ($backend in $Backends) {
        [void](Get-BridgeProfileLocalPath ([string]$backend.command))
        foreach ($name in @('home', 'codex_home')) {
            $state = Get-BridgeProfileLocalPath ([string]$backend[$name]) -Directory
            if (Test-BridgeProfileOverlap $parent $state) {
                throw 'Choose a profile parent separate from all four backend state directories.'
            }
        }
    }
    $directory = Join-Path $parent ('magpie-bridge-profile-' + [guid]::NewGuid().ToString('N'))
    # New-Item without Force rejects an existing directory; CreateNew also refuses
    # to replace any existing file. Do not delete or overwrite a previous profile.
    [void](New-Item -ItemType Directory -Path $directory -ErrorAction Stop)
    $path = Join-Path $directory 'bridge.json'
    $stream = $null
    try {
        $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $data = [Text.UTF8Encoding]::new($false).GetBytes($json)
        $stream.Write($data, 0, $data.Length); $stream.Flush($true)
        $status = 'failed'
        try { $status = Invoke-BridgeProfileCheck $path } catch { $status = 'failed' }
        if (@('passed', 'failed', 'timeout') -cnotcontains $status) { $status = 'failed' }
        return [pscustomobject]@{ Path=$path; Status=$status }
    } finally { if ($null -ne $stream) { $stream.Dispose() } }
}

# A distinct consented native operation, never part of Save and check.
function Confirm-BridgePrimaryIdentity([object]$Primary, [string]$Digest) {
    $message = "Inspect ONLY the selected primary using the reviewed 0.167.0-alpha.3.2 contract?`r`n`r`nProgram: $($Primary.command)`r`nHOME: $($Primary.home)`r`nCODEX_HOME: $($Primary.codex_home)`r`nCurrent SHA-256: $Digest`r`n`r`nYES confirms you trust this reviewed program/version and native configuration, and these are dedicated contexts, not your ordinary account homes. The hash does not establish official origin or network destinations.`r`n`r`nThis starts the primary backend. It can read/refresh credentials, contact services and change native state even with refreshToken=false. Failure or cancellation does not undo those effects. No login, logout, chat, token request or secondary launch is initiated. No automatic retry."
    return [Windows.Forms.MessageBox]::Show($message, 'Consent to one primary identity inspection',
        [Windows.Forms.MessageBoxButtons]::YesNo, [Windows.Forms.MessageBoxIcon]::Warning,
        [Windows.Forms.MessageBoxDefaultButton]::Button2) -eq [Windows.Forms.DialogResult]::Yes
}

function Read-BridgeIdentityProjection([string]$Text) {
    if ($Text.Length -gt 4096) { throw 'Invalid identity projection.' }
    $p = $Text | ConvertFrom-Json
    if ($null -eq $p -or @($p.PSObject.Properties).Count -ne 4) { throw 'Invalid identity projection.' }
    foreach ($name in @('schema','scope','authMethod','requiresOpenaiAuth')) {
        if ($null -eq $p.PSObject.Properties[$name]) { throw 'Invalid identity projection.' }
    }
    if (($p.schema -isnot [int] -and $p.schema -isnot [long]) -or $p.schema -ne 1 -or $p.scope -isnot [string] -or $p.scope -cne 'native-reported-identity-only' -or
        $p.requiresOpenaiAuth -isnot [bool]) { throw 'Invalid identity projection.' }
    if ($null -ne $p.authMethod -and ($p.authMethod -isnot [string] -or
        @('apikey','chatgpt','chatgptAuthTokens','headers','agentIdentity','personalAccessToken','bedrockApiKey','bedrockAccessKeys') -cnotcontains $p.authMethod)) { throw 'Invalid identity projection.' }
    if (-not $p.requiresOpenaiAuth -and $null -ne $p.authMethod) { throw 'Invalid identity projection.' }
    return $p
}

function Invoke-BridgePrimaryIdentity([object[]]$Backends, [string]$ParentDirectory) {
    # Rebuild from exactly the displayed fields. Never edit/import a saved profile.
    foreach ($b in $Backends) {
        if ($b -isnot [Collections.IDictionary] -or $b.Count -ne 5) { throw 'Only displayed fields allowed.' }
        foreach ($key in $b.Keys) { if (@('id','provider','command','home','codex_home') -cnotcontains $key) { throw 'Unsupported field.' } }
    }
    $profile = New-BridgeProfileDraft $Backends | ConvertFrom-Json
    if ($profile.backends[0].provider -cne 'openai') { throw 'Primary provider must be openai.' }
    $parent = Get-BridgeProfileLocalPath $ParentDirectory -Directory
    foreach ($b in $Backends) {
        [void](Get-BridgeProfileLocalPath $b.command)
        foreach ($name in @('home','codex_home')) {
            $statePath = Get-BridgeProfileLocalPath $b[$name] -Directory
            if (Test-BridgeProfileOverlap $parent $statePath) { throw 'Choose a separate snapshot parent.' }
        }
    }
    $digest = (Get-FileHash -LiteralPath $Backends[0].command -Algorithm SHA256).Hash.ToLowerInvariant()
    if (-not (Confirm-BridgePrimaryIdentity $Backends[0] $digest)) { return 'Cancelled before launch; no native operation performed.' }
    $profile.backends[0] | Add-Member -NotePropertyName startup_probe_sha256 -NotePropertyValue $digest
    $dir = Join-Path $parent ('magpie-identity-once-' + [guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Directory -Path $dir -ErrorAction Stop)
    $path = Join-Path $dir 'bridge.json'
    $stream = $null; $process = $null; $started = $false
    try {
        $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $data = [Text.UTF8Encoding]::new($false).GetBytes(($profile | ConvertTo-Json -Depth 5))
        $stream.Write($data,0,$data.Length); $stream.Flush($true)
        $psi = New-BridgeAdapterProcessInfo $path
        $psi.Arguments = '--inspect-primary-identity-v0.167.0-alpha.3.2'
        $process = New-Object Diagnostics.Process
        $process.StartInfo = $psi
        $started = $process.Start()
        if (-not $started) { throw 'Inspection unavailable.' }
        $stdout = $process.StandardOutput.ReadToEndAsync(); $stderr = $process.StandardError.ReadToEndAsync()
        # UI timeout is distinct from the core exchange deadline and native cleanup.
        if (-not $process.WaitForExit(30000)) { throw 'Inspection unavailable.' }
        if (-not $stdout.Wait(1000) -or -not $stderr.Wait(1000) -or $process.ExitCode -ne 0 -or $stderr.Result.Length -ne 0) { throw 'Inspection unavailable.' }
        $p = Read-BridgeIdentityProjection $stdout.Result
        $method = 'Native did not report an identity (not proof no account exists)'
        if ($null -ne $p.authMethod) { $method = 'Native identity type: ' + $p.authMethod }
        return ($method + '; requiresOpenaiAuth=' + $p.requiresOpenaiAuth + '. Not a chat-readiness result.')
    } finally {
        try {
            if ($started -and -not $process.HasExited) {
                $process.Kill()
                if (-not $process.WaitForExit(3000)) { throw 'Inspection cleanup unconfirmed.' }
            }
        } finally {
            if ($null -ne $process) { $process.Dispose() }
            if ($null -ne $stream) { $stream.Dispose() }
            Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction Stop
        }
    }
}

function Select-BridgeProfileParent {
    $dialog = New-Object Windows.Forms.FolderBrowserDialog
    $dialog.Description = 'Choose a separate profile parent; a new folder and bridge.json will be created. Existing files are never overwritten.'
    $dialog.ShowNewFolderButton = $false
    try {
        if ($dialog.ShowDialog() -eq [Windows.Forms.DialogResult]::OK) { return $dialog.SelectedPath }
        return $null
    } finally { $dialog.Dispose() }
}

# Construction is separate from the modal lifetime so the owned controls can be
# exercised directly. This does not add a production automation/launch mode.
function New-BridgeProfileWindow {
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Windows only.' }
    if ([Threading.Thread]::CurrentThread.ApartmentState -ne 'STA') { throw 'Run PowerShell in STA mode.' }
    Add-Type -AssemblyName System.Windows.Forms
    Add-Type -AssemblyName System.Drawing
    $form = New-Object Windows.Forms.Form
    $form.Text = 'Magpie Bridge - Isolated profile helper'
    $form.ClientSize = [Drawing.Size]::new(850, 820)
    $form.MinimumSize = $form.Size
    $form.StartPosition = 'CenterScreen'
    $form.AutoScroll = $true
    $form.Font = New-Object Drawing.Font('Segoe UI', 10)
    $notice = New-Object Windows.Forms.Label
    $notice.Location = New-Object Drawing.Point(16, 12)
    $notice.Size = New-Object Drawing.Size(800, 80)
    $notice.Text = "EXPERIMENTAL: Save/check creates a profile without contacting a backend.`r`nIdentity inspection is separate and requires consent to possible native side effects.`r`nUse dedicated homes, not ordinary account homes. This helper does NOT connect Codex."
    $form.Controls.Add($notice)
    $rows = @()
    $y = 100
    for ($i = 0; $i -lt 2; $i++) {
        $heading = New-Object Windows.Forms.Label
        $heading.Text = 'Backend ' + ($i + 1)
        $heading.Location = New-Object Drawing.Point(16, $y)
        $heading.Size = New-Object Drawing.Size(780, 24)
        $form.Controls.Add($heading)
        $y += 28
        $fields = @{}
        foreach ($name in @('id', 'provider', 'command', 'home', 'codex_home')) {
            $label = New-Object Windows.Forms.Label
            $label.Text = $name
            $label.Location = New-Object Drawing.Point(16, $y)
            $label.Size = New-Object Drawing.Size(110, 24)
            $form.Controls.Add($label)
            $box = New-Object Windows.Forms.TextBox
            $box.Location = New-Object Drawing.Point(132, $y)
            $box.Size = New-Object Drawing.Size(560, 26)
            $box.MaxLength = 4096
            $form.Controls.Add($box)
            $fields[$name] = $box
            if ($name -eq 'id') { $box.Text = @('alpha', 'beta')[$i] }
            if ($name -in @('command', 'home', 'codex_home')) {
                $box.ReadOnly = $true
                $browse = New-Object Windows.Forms.Button
                $browse.Text = 'Choose...'
                $browse.Location = [Drawing.Point]::new(704, ($y - 1))
                $browse.Size = New-Object Drawing.Size(106, 28)
                $browse.Tag = @{ Box = $box; IsFile = ($name -eq 'command') }
                $browse.Add_Click({
                    $dialog = $null
                    try {
                        if ($this.Tag.IsFile) {
                            $dialog = New-Object Windows.Forms.OpenFileDialog
                            $dialog.Title = 'Select an existing official app-server executable (not the bridge)'
                            $dialog.Filter = 'Native executable (*.exe)|*.exe'
                            $dialog.CheckFileExists = $true
                            $dialog.Multiselect = $false
                        } else {
                            $dialog = New-Object Windows.Forms.FolderBrowserDialog
                            $dialog.Description = 'Select an existing isolated test directory; no files will be imported'
                            $dialog.ShowNewFolderButton = $false
                        }
                        if ($dialog.ShowDialog() -eq [Windows.Forms.DialogResult]::OK) {
                            if ($this.Tag.IsFile) { $this.Tag.Box.Text = $dialog.FileName }
                            else { $this.Tag.Box.Text = $dialog.SelectedPath }
                        }
                    } finally { if ($null -ne $dialog) { $dialog.Dispose() } }
                })
                $form.Controls.Add($browse)
            }
            $y += 32
        }
        $rows += $fields
        $y += 8
    }
    $generate = New-Object Windows.Forms.Button
    $generate.Text = 'Preview draft'
    $generate.Location = New-Object Drawing.Point(16, $y)
    $generate.Size = New-Object Drawing.Size(150, 30)
    $form.Controls.Add($generate)
    $status = New-Object Windows.Forms.Label
    $status.Location = New-Object Drawing.Point(345, $y)
    $status.Size = New-Object Drawing.Size(465, 48)
    $status.Text = 'Provider means the existing backend config identifier, not a URL or API key.'
    $form.Controls.Add($status)
    $preview = New-Object Windows.Forms.TextBox
    $preview.Location = New-Object Drawing.Point(16, ($y + 54))
    $preview.Size = New-Object Drawing.Size(794, 170)
    $preview.Multiline = $true
    $preview.ReadOnly = $true
    $preview.ScrollBars = 'Both'
    $preview.WordWrap = $false
    $form.Controls.Add($preview)
    $generate.Tag = @{ Rows = $rows; Preview = $preview; Status = $status }
    $generate.Add_Click({
        $this.Tag.Preview.Clear()
        try {
            $inputs = @()
            foreach ($row in $this.Tag.Rows) {
                $inputRow = @{}
                foreach ($key in $row.Keys) { $inputRow[$key] = $row[$key].Text }
                $inputs += $inputRow
            }
            $this.Tag.Preview.Text = New-BridgeProfileDraft -Backends $inputs
            $this.Tag.Status.Text = 'Draft only. Binary identity, symlink overlap, policy and runtime compatibility NOT verified.'
        } catch {
            $this.Tag.Status.Text = $_.Exception.Message
        }
    })
    $save = New-Object Windows.Forms.Button
    $save.Text = 'Save and check'
    $save.Location = New-Object Drawing.Point(178, $y)
    $save.Size = New-Object Drawing.Size(155, 30)
    $save.Tag = @{ Rows = $rows; Preview = $preview; Status = $status }
    $save.Add_Click({
        try {
            $parent = Select-BridgeProfileParent
            if ([string]::IsNullOrEmpty($parent)) { return }
            $this.Enabled = $false
            $this.Tag.Preview.Clear()
            $inputs = @()
            foreach ($row in $this.Tag.Rows) {
                $inputRow = @{}
                foreach ($key in $row.Keys) { $inputRow[$key] = $row[$key].Text }
                $inputs += $inputRow
            }
            $this.Tag.Status.Text = 'Saving a new profile and checking paths; no backend starts.'
            $result = Save-BridgeProfileDraft $inputs $parent
            $this.Tag.Preview.Text = $result.Path
            if ($result.Status -ceq 'passed') {
                $this.Tag.Status.Text = 'Saved; structure/path check passed. Authentication, models and Desktop remain unverified.'
            } elseif ($result.Status -ceq 'timeout') {
                $this.Tag.Status.Text = 'Saved but UNCHECKED: checker timed out. No retry or connection was attempted.'
            } else {
                $this.Tag.Status.Text = 'Saved but UNCHECKED: bundled adapter missing/invalid or check failed. No connection attempted.'
            }
        } catch {
            $this.Tag.Status.Text = 'Not validated. Check local paths and permissions; no existing profile was overwritten.'
        } finally { $this.Enabled = $true }
    })
    $form.Controls.Add($save)
    $identity = New-Object Windows.Forms.Button
    $identity.Text = 'Inspect primary identity...'
    $identity.Location = [Drawing.Point]::new(16, ($y + 232))
    $identity.Size = [Drawing.Size]::new(215, 30)
    $identityStatus = New-Object Windows.Forms.Label
    $identityStatus.Location = [Drawing.Point]::new(245, ($y + 232))
    $identityStatus.Size = [Drawing.Size]::new(565, 60)
    $identityStatus.Text = 'Optional native operation, separate consent required. Not a login or chat test.'
    $identity.Tag = @{ Rows=$rows; Status=$identityStatus }
    $identity.Add_Click({
        $this.Tag.Status.Text = 'No identity result.'
        try {
            $parent = Select-BridgeProfileParent
            if ([string]::IsNullOrEmpty($parent)) { $this.Tag.Status.Text = 'Cancelled before launch.'; return }
            $inputs = @()
            foreach ($row in $this.Tag.Rows) {
                $inputRow = @{}
                foreach ($key in $row.Keys) { $inputRow[$key] = $row[$key].Text }
                $inputs += $inputRow
            }
            $this.Enabled = $false
            $this.Tag.Status.Text = Invoke-BridgePrimaryIdentity $inputs $parent
        } catch {
            $this.Tag.Status.Text = 'Identity unavailable or cleanup unconfirmed. Native side effects are not rolled back; no retry performed.'
        } finally { $this.Enabled = $true }
    })
    $form.Controls.Add($identity)
    $form.Controls.Add($identityStatus)
    # Invalidate the old draft immediately when any selected value changes.
    foreach ($row in $rows) {
        foreach ($box in $row.Values) {
            $box.Tag = @{ Preview = $preview; Status = $status; IdentityStatus=$identityStatus }
            $box.Add_TextChanged({
                $this.Tag.Preview.Clear()
                $this.Tag.IdentityStatus.Text = 'Fields changed: previous identity result no longer applies.'
                $this.Tag.Status.Text = 'Fields changed: previous check no longer applies. Previously saved files remain unchanged.'
            })
        }
    }
    return [pscustomobject]@{ Form=$form; Rows=$rows; Save=$save; Preview=$preview; Status=$status; Identity=$identity; IdentityStatus=$identityStatus }
}

function Show-BridgeProfilePreview {
    $window = New-BridgeProfileWindow
    try { [void]$window.Form.ShowDialog() } finally { $window.Form.Dispose() }
}

if ($MyInvocation.InvocationName -ne '.') { Show-BridgeProfilePreview }
