# Dot-source this file only. No execution occurs until the explicit invocation.
function Assert-ControlPlaneNativeSummary($Report, [string]$AcceptanceProfile = '') {
    $expectedNativeSHA = '40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39'
    $expectedScope = 'synthetic-loopback-only'
    if ($AcceptanceProfile -ceq 'synthetic-models-4239') {
        $expectedNativeSHA = '4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'
        $expectedScope = 'self-authored-models-loopback-only'
    } elseif ($AcceptanceProfile -ceq 'app-server-4239') {
        $expectedNativeSHA = '4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382'
    } elseif ($AcceptanceProfile -cne '') { throw 'Invalid native control-plane summary.' }
    $names = @('schema', 'scope', 'status', 'stage', 'failure', 'native_sha256',
        'binary_verified', 'version_verified', 'config_reads', 'layer_reads',
        'stable_layer_rereads', 'layer_version_changes', 'catalog_models',
        'threads', 'turns', 'assistant_messages', 'thread_reads', 'thread_list_queries', 'thread_list_pages', 'thread_list_items', 'ownership_rejections',
        'alpha_requests', 'beta_requests', 'fixture_errors', 'unexpected_server_requests',
        'adapter_clean_exit', 'cleanup_complete')
    if ($null -eq $Report -or $Report -isnot [pscustomobject]) { throw 'Invalid native control-plane summary.' }
    $actual = @($Report.PSObject.Properties | ForEach-Object { $_.Name })
    if ($AcceptanceProfile -ceq 'synthetic-models-4239') {
        foreach ($name in @('restart_cycles', 'resumed_threads')) {
            if ($actual -ccontains $name) { $names += $name }
        }
    }
    if ($actual.Count -ne $names.Count) { throw 'Invalid native control-plane summary.' }
    foreach ($name in $actual) {
        if ($names -cnotcontains $name) { throw 'Invalid native control-plane summary.' }
    }
    $stages = @('inputs', 'version', 'initialize', 'initial_config', 'catalog', 'resolved_config',
        'threads', 'turns', 'thread_list', 'ownership', 'final_config', 'shutdown', 'restart', 'resume', 'complete', 'supervisor')
    $failures = @('', 'internal_failure', 'input_invalid', 'environment_rejected',
        'scratch_invalid', 'scratch_not_empty', 'native_hash_mismatch', 'catalog_hash_mismatch',
        'catalog_invalid', 'platform_rejected', 'process_start_failed', 'version_invalid',
        'fixture_invalid', 'fixture_failed', 'protocol_invalid', 'protocol_limit',
        'unexpected_server_request', 'native_error', 'protocol_write_failed', 'deadline',
        'process_ended', 'unexpected_item', 'cleanup_failed', 'shutdown_failed',
        'logical_provider_mismatch', 'project_not_empty', 'ownership_changed',
        'config_shape_mismatch', 'turn_failed', 'answer_crossed_route', 'unexpected_auth_data',
        'control_home_mismatch', 'thread_list_invalid', 'supervision_failed')
    if (($Report.schema -isnot [int] -and $Report.schema -isnot [long]) -or $Report.schema -ne 1 -or
        $Report.scope -isnot [string] -or $Report.scope -cne $expectedScope -or
        $Report.status -isnot [string] -or @('passed', 'failed') -cnotcontains $Report.status -or
        $Report.stage -isnot [string] -or $stages -cnotcontains $Report.stage -or
        $Report.failure -isnot [string] -or $failures -cnotcontains $Report.failure -or
        $Report.native_sha256 -isnot [string] -or
        @('', $expectedNativeSHA) -cnotcontains $Report.native_sha256) {
        throw 'Invalid native control-plane summary.'
    }
    foreach ($name in @('binary_verified', 'version_verified', 'adapter_clean_exit', 'cleanup_complete')) {
        if ($Report.$name -isnot [bool]) { throw 'Invalid native control-plane summary.' }
    }
    $counts = @('config_reads', 'layer_reads', 'stable_layer_rereads', 'layer_version_changes',
        'catalog_models', 'threads', 'turns', 'assistant_messages',
        'thread_reads', 'thread_list_queries', 'thread_list_pages', 'thread_list_items',
        'ownership_rejections', 'alpha_requests', 'beta_requests',
        'fixture_errors', 'unexpected_server_requests')
    foreach ($name in $counts) {
        if (($Report.$name -isnot [int] -and $Report.$name -isnot [long]) -or
            $Report.$name -lt 0 -or $Report.$name -gt 2048) { throw 'Invalid native control-plane summary.' }
    }
    foreach ($entry in @(@('restart_cycles', 1), @('resumed_threads', 2))) {
        $name = $entry[0]
        if ($actual -ccontains $name) {
            if (($Report.$name -isnot [int] -and $Report.$name -isnot [long]) -or
                $Report.$name -lt 1 -or $Report.$name -gt $entry[1]) { throw 'Invalid native control-plane summary.' }
        }
        if ($Report.status -ceq 'passed' -and $AcceptanceProfile -ceq 'synthetic-models-4239') {
            if ($actual -cnotcontains $name -or $Report.$name -ne $entry[1]) { throw 'Invalid native control-plane summary.' }
        }
    }
    if ($actual -ccontains 'resumed_threads' -and $actual -cnotcontains 'restart_cycles') {
        throw 'Invalid native control-plane summary.'
    }
    if ($Report.status -ceq 'passed') {
        $expected = @(9, 6, 3, 0, 2, 2, 4, 4, 2, 2, 4, 4, 2, 2, 2, 0, 0)
        if ($counts.Count -ne $expected.Count) { throw 'Invalid native control-plane summary.' }
        for ($i = 0; $i -lt $counts.Count; $i++) {
            if ($Report.($counts[$i]) -ne $expected[$i]) { throw 'Invalid native control-plane summary.' }
        }
        if ($Report.stage -cne 'complete' -or $Report.failure -cne '' -or
            $Report.native_sha256 -ceq '' -or -not $Report.binary_verified -or
            -not $Report.version_verified -or -not $Report.adapter_clean_exit -or
            -not $Report.cleanup_complete) { throw 'Invalid native control-plane summary.' }
    } elseif ($Report.failure -ceq '') { throw 'Invalid native control-plane summary.' }
}

# Validate presence, not only value: even -CatalogPath '' is forbidden for self-authored models.
function Get-ControlPlaneNativeCatalogArguments([string]$Profile, [bool]$CatalogSupplied, [string]$Catalog) {
    if ($Profile -ceq 'synthetic-models-4239') {
        if ($CatalogSupplied) { throw 'Catalog input is forbidden for self-authored models.' }
        return
    }
    if (($Profile -cne '' -and $Profile -cne 'app-server-4239') -or
        -not $CatalogSupplied -or [string]::IsNullOrWhiteSpace($Catalog)) { throw 'Invalid catalog profile input.' }
    return @('-catalog', $Catalog)
}

function Invoke-ControlPlaneNative {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$HelperExecutable,
        [Parameter(Mandatory)][string]$BridgeExecutable,
        [Parameter(Mandatory)][string]$CodexExecutable,
        [AllowNull()][AllowEmptyString()][string]$CatalogPath,
        [Parameter(Mandatory)][string]$ScratchParent,
        [Parameter(Mandatory)][string]$HelperHome,
        [string]$AcceptanceProfile = ''
    )
    $ErrorActionPreference = 'Stop'
    $process = $null
    $started = $false
    try {
        $catalogArguments = @(Get-ControlPlaneNativeCatalogArguments $AcceptanceProfile ($PSBoundParameters.ContainsKey('CatalogPath')) $CatalogPath)
        $inputFiles = @($HelperExecutable, $BridgeExecutable, $CodexExecutable)
        if ($catalogArguments.Count -ne 0) { $inputFiles += $CatalogPath }
        foreach ($path in $inputFiles) {
            $file = Get-Item -LiteralPath $path
            if (-not [IO.Path]::IsPathFullyQualified($path) -or $file.PSIsContainer -or
                ($file.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Invalid input.' }
        }
        foreach ($path in @($ScratchParent, $HelperHome)) {
            $dir = Get-Item -LiteralPath $path
            if (-not [IO.Path]::IsPathFullyQualified($path) -or -not $dir.PSIsContainer -or
                ($dir.Attributes -band [IO.FileAttributes]::ReparsePoint) -or
                @(Get-ChildItem -LiteralPath $path -Force).Count -ne 0) { throw 'Invalid input.' }
        }
        $scratchFull = [IO.Path]::GetFullPath($ScratchParent).TrimEnd('\') + '\'
        $homeFull = [IO.Path]::GetFullPath($HelperHome).TrimEnd('\') + '\'
        if ($scratchFull.StartsWith($homeFull, [StringComparison]::OrdinalIgnoreCase) -or
            $homeFull.StartsWith($scratchFull, [StringComparison]::OrdinalIgnoreCase)) { throw 'Overlapping input.' }

        if (-not ('MagpieControlPlaneNativeV167.Capture' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Text;
using System.Threading.Tasks;
namespace MagpieControlPlaneNativeV167 {
    public sealed class Result {
        public string Text;
        public bool Invalid;
    }
    public static class Capture {
        public static async Task<Result> Read(Stream stream) {
            var saved = new MemoryStream(16384);
            var buffer = new byte[2048];
            bool invalid = false;
            int n;
            while ((n = await stream.ReadAsync(buffer, 0, buffer.Length)) != 0) {
                for (int i = 0; i < n; i++) if (buffer[i] > 127) invalid = true;
                if (saved.Length + n <= 16384) saved.Write(buffer, 0, n);
                else invalid = true;
            }
            return new Result { Text = Encoding.ASCII.GetString(saved.ToArray()), Invalid = invalid };
        }
        public static async Task Drain(Stream stream) {
            var buffer = new byte[2048];
            while (await stream.ReadAsync(buffer, 0, buffer.Length) != 0) {}
        }
    }
}
'@
        }
        $psi = [Diagnostics.ProcessStartInfo]::new()
        $psi.FileName = $HelperExecutable
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $psi.RedirectStandardInput = $true
        $psi.WorkingDirectory = $HelperHome
        foreach ($arg in @('-supervise', '-bridge', $BridgeExecutable, '-codex', $CodexExecutable)) { $psi.ArgumentList.Add($arg) }
        foreach ($arg in $catalogArguments) { $psi.ArgumentList.Add($arg) }
        foreach ($arg in @('-scratch-parent', $ScratchParent)) { $psi.ArgumentList.Add($arg) }
        if ($AcceptanceProfile -cne '') {
            $psi.ArgumentList.Add('-acceptance-profile')
            $psi.ArgumentList.Add($AcceptanceProfile)
        }
        $psi.Environment.Clear()
        $systemRoot = [Environment]::GetEnvironmentVariable('SystemRoot')
        if (-not [IO.Path]::IsPathFullyQualified($systemRoot)) { throw 'Invalid system root.' }
        $psi.Environment['SystemRoot'] = $systemRoot
        $psi.Environment['WINDIR'] = $systemRoot
        $psi.Environment['PATH'] = Join-Path $systemRoot 'System32'
        $psi.Environment['LANG'] = 'C.UTF-8'
        foreach ($name in @('HOME', 'USERPROFILE', 'APPDATA', 'LOCALAPPDATA', 'TMP', 'TEMP', 'TMPDIR')) {
            $psi.Environment[$name] = $HelperHome
        }
        $helperCodex = Join-Path $HelperHome 'codex-home'
        [void][IO.Directory]::CreateDirectory($helperCodex)
        $psi.Environment['CODEX_HOME'] = $helperCodex
        $process = [Diagnostics.Process]::new()
        $process.StartInfo = $psi
        $started = $process.Start()
        if (-not $started) { throw 'Failed to start.' }
        $process.StandardInput.Close()
        $capture = [MagpieControlPlaneNativeV167.Capture]::Read($process.StandardOutput.BaseStream)
        $drain = [MagpieControlPlaneNativeV167.Capture]::Drain($process.StandardError.BaseStream)
        if (-not $process.WaitForExit(270000)) {
            # Only the owned watchdog is killed. Its OS-owned Job Object closes,
            # which kills its acceptance process, adapter and native descendants.
            $process.Kill()
            [void]$process.WaitForExit(5000)
            throw 'Timeout.'
        }
        if (-not $capture.Wait(5000) -or -not $drain.Wait(5000)) { throw 'Capture failed.' }
        $result = $capture.Result
        if ($result.Invalid) { throw 'Capture failed.' }
        $text = $result.Text
        if ($text.EndsWith("`r`n", [StringComparison]::Ordinal)) {
            $text = $text.Substring(0, $text.Length - 2)
        } elseif ($text.EndsWith("`n", [StringComparison]::Ordinal)) {
            $text = $text.Substring(0, $text.Length - 1)
        } else { throw 'Invalid result.' }
        if ($text.Contains("`n") -or $text.Contains("`r")) { throw 'Invalid result.' }
        $report = $text | ConvertFrom-Json
        Assert-ControlPlaneNativeSummary $report -AcceptanceProfile $AcceptanceProfile
        if (($report | ConvertTo-Json -Depth 16 -Compress) -cne $text -or
            (($process.ExitCode -eq 0) -ne ($report.status -ceq 'passed'))) { throw 'Invalid result.' }
        return $report
    } catch {
        throw 'Native control-plane check failed.'
    } finally {
        if ($null -ne $process) {
            if ($started -and -not $process.HasExited) {
                try { $process.Kill(); [void]$process.WaitForExit(5000) } catch {}
            }
            $process.Dispose()
        }
    }
}
