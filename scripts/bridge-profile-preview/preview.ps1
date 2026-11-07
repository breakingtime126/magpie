# An isolated, draft-only Windows UI. No normal Magpie initialization is loaded.
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

function Show-BridgeProfilePreview {
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Windows only.' }
    if ([Threading.Thread]::CurrentThread.ApartmentState -ne 'STA') { throw 'Run PowerShell in STA mode.' }
    Add-Type -AssemblyName System.Windows.Forms
    Add-Type -AssemblyName System.Drawing
    $form = New-Object Windows.Forms.Form
    $form.Text = 'Magpie Bridge - Draft profile only'
    $form.Size = New-Object Drawing.Size(850, 820)
    $form.MinimumSize = $form.Size
    $form.StartPosition = 'CenterScreen'
    $form.AutoScroll = $true
    $form.Font = New-Object Drawing.Font('Segoe UI', 10)
    $notice = New-Object Windows.Forms.Label
    $notice.Location = New-Object Drawing.Point(16, 12)
    $notice.Size = New-Object Drawing.Size(800, 80)
    $notice.Text = "EXPERIMENTAL PREVIEW: no files are saved or imported; no account or backend is contacted.`r`nSelect existing, separately prepared test homes. Do not select your ordinary Codex home.`r`nThis draft does NOT connect Codex, configure authentication, or enable Desktop compatibility."
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
                $browse.Location = New-Object Drawing.Point(704, $y - 1)
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
    $generate.Size = New-Object Drawing.Size(160, 30)
    $form.Controls.Add($generate)
    $status = New-Object Windows.Forms.Label
    $status.Location = New-Object Drawing.Point(190, $y)
    $status.Size = New-Object Drawing.Size(620, 48)
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
    # Invalidate the old draft immediately when any selected value changes.
    foreach ($row in $rows) {
        foreach ($box in $row.Values) {
            $box.Tag = $preview
            $box.Add_TextChanged({ $this.Tag.Clear() })
        }
    }
    try { [void]$form.ShowDialog() } finally { $form.Dispose() }
}

if ($MyInvocation.InvocationName -ne '.') { Show-BridgeProfilePreview }
