# Opt-in disposable CI only. Import definitions, never the old acceptance body.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$TestExecutable,
    [ValidateSet('TestOfficialNativePreviewText','TestOfficialNativeSupplierCatalogText')]
    [string]$TestName = 'TestOfficialNativePreviewText'
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_OS -cne 'Windows' -or
    $env:GITHUB_EVENT_NAME -cne 'push' -or $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
    $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
    $env:GITHUB_SHA -cnotmatch '^[0-9a-f]{40}\z' -or
    $env:PREVIEW_NATIVE_TEXT_OPT_IN -cne 'native-preview-text-v1' -or
    -not [IO.Path]::IsPathFullyQualified($env:RUNNER_TEMP)) { throw 'Explicit isolated CI context required.' }
# Presence, including an empty value, is a safety selection, not a value to erase.
$parentEnvironment = [Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
foreach ($name in $parentEnvironment.Keys) {
    if ([string]::Equals([string]$name, 'CODEX_WINDOWS_REGISTERED_CORE', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Registered Core environment is outside this test scope.'
    }
}
$head = (& git rev-parse HEAD).Trim()
$message = (& git log -1 --format=%B) -join "`n"
$requiredMarker = if ($TestName -ceq 'TestOfficialNativeSupplierCatalogText') { '[native-supplier-catalog-v1]' } else { '[native-preview-text-v1]' }
if ($LASTEXITCODE -ne 0 -or $head -cne $env:GITHUB_SHA -or
    -not $message.Contains($requiredMarker)) { throw 'Exact opt-in commit required.' }
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $PSScriptRoot 'control-plane-native/ci-4239.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw 'Preparation definitions did not parse.' }
$definitions = foreach ($name in @('Get-StandaloneIdentityHelpers', 'Save-StandalonePublicInput',
    'Get-StandaloneSignatureExit', 'Export-Standalone4239Native')) {
    $matches = @($ast.FindAll({ param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($matches.Count -ne 1) { throw 'Preparation definition missing or ambiguous.' }
    $matches[0].Extent.Text
}
. ([scriptblock]::Create(($definitions -join "`n")))
. (Get-StandaloneIdentityHelpers (Join-Path $PSScriptRoot 'test-desktop-cold-start.ps1'))
Assert-PublicRegularPath $TestExecutable
$root = Join-Path $env:RUNNER_TEMP ('native-preview-text-' + [guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($root)
$reportName = if ($TestName -ceq 'TestOfficialNativeSupplierCatalogText') { 'native-supplier-catalog-text-report.json' } else { 'native-preview-text-report.json' }
$scope = if ($TestName -ceq 'TestOfficialNativeSupplierCatalogText') { 'official-native-generated-supplier-catalog-text-only' } else { 'official-native-loopback-text-only' }
$reportPath = Join-Path $env:RUNNER_TEMP $reportName
if (Test-Path -LiteralPath $reportPath) { throw 'Report already exists; no retry.' }
$report = [ordered]@{schema=1;scope=$scope;source_commit=$head;
    status='failed';stage='download';package_verified=$false;test_passed=$false;cleanup_complete=$false;
    desktop_verified=$false;real_account_used=$false;zero_egress_proven=$false;
    diagnostic_status='unavailable';native_stage=$null;native_class=$null;
    test_process_started=$false;host_error_line=0;host_error_category='none';
    cleanup_error_line=0;cleanup_error_category='none';cleanup_error_hresult=0;cleanup_delete_attempts=0;cleanup_sharing_retries=0}
$process = $null; $started = $false
try {
    $package = Join-Path $root 'official.msix'
    # Microsoft's winget 26.1103.4239.0 manifest names this fixed official URL
    # with the same package SHA pinned below. The rolling URL can move ahead.
    Save-StandalonePublicInput 'https://persistent.oaistatic.com/codex-app-prod/releases/26.1103.4239.0/ChatGPT-x64.msix' $package 4294967296
    $report.stage = 'signature'
    $signatureExit = Get-StandaloneSignatureExit $package
    $signature = Get-AuthenticodeSignature -LiteralPath $package
    $subject = ''
    if ($signature.SignerCertificate) { $subject = $signature.SignerCertificate.Subject }
    $report.stage = 'identity'
    $identity = Get-DesktopPackageIdentityEvidence $package `
        '9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c' `
        $signatureExit ([string]$signature.Status) $subject
    Assert-Desktop4239Identity $identity
    $codex = Join-Path $root 'codex.exe'
    Export-Standalone4239Native $package $codex $identity
    $report.package_verified = $true
    $report.stage = 'native-text-test'
    $testHome = Join-Path $root 'test-home'
    [void][IO.Directory]::CreateDirectory($testHome)
    $psi = [Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = [IO.Path]::GetFullPath($TestExecutable)
    $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    $psi.WorkingDirectory = $testHome
    $psi.Environment.Clear()
    $systemRoot = [Environment]::GetEnvironmentVariable('SystemRoot')
    if (-not [IO.Path]::IsPathFullyQualified($systemRoot)) { throw 'System root unavailable.' }
    $psi.Environment['SystemRoot'] = $systemRoot; $psi.Environment['WINDIR'] = $systemRoot
    $psi.Environment['PATH'] = Join-Path $systemRoot 'System32'
    foreach ($name in @('HOME','USERPROFILE','TEMP','TMP','APPDATA','LOCALAPPDATA')) { $psi.Environment[$name] = $testHome }
    # Keep existing transport/trust choices, including empty values. No bypass.
    $transportNames = @('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY',
        'http_proxy','https_proxy','all_proxy','no_proxy','CODEX_CA_CERTIFICATE',
        'SSL_CERT_FILE','SSL_CERT_DIR','CURL_CA_BUNDLE','REQUESTS_CA_BUNDLE','NODE_EXTRA_CA_CERTS')
    foreach ($name in $parentEnvironment.Keys) {
        if ($transportNames -contains [string]$name) {
            $psi.Environment[[string]$name] = [string]$parentEnvironment[$name]
        }
    }
    $psi.Environment['MAGPIE_PREVIEW_OFFICIAL_TEXT_TEST'] = '1'
    $psi.Environment['MAGPIE_PREVIEW_OFFICIAL_NATIVE'] = $codex
    foreach ($argument in @('-test.run',('^' + [regex]::Escape($TestName) + '$'),'-test.v','-test.timeout','120s')) {
        $psi.ArgumentList.Add($argument)
    }
    $process = [Diagnostics.Process]::new(); $process.StartInfo = $psi
    $started = $process.Start()
    if (-not $started) { throw 'Test did not start.' }
    # Read both streams with a byte-independent UTF-16 character ceiling.
    $buffers = @([char[]]::new(4096), [char[]]::new(4096))
    $readers = @($process.StandardOutput, $process.StandardError)
    $texts = @([Text.StringBuilder]::new(), [Text.StringBuilder]::new())
    $reads = @($readers[0].ReadAsync($buffers[0], 0, 4096), $readers[1].ReadAsync($buffers[1], 0, 4096))
    $closed = @($false, $false)
    $watch = [Diagnostics.Stopwatch]::StartNew()
    while (-not ($closed[0] -and $closed[1] -and $process.HasExited)) {
        if ($watch.ElapsedMilliseconds -gt 135000) { throw 'Test timed out.' }
        for ($i = 0; $i -lt 2; $i++) {
            if (-not $closed[$i] -and $reads[$i].IsCompleted) {
                $count = $reads[$i].GetAwaiter().GetResult()
                if ($count -eq 0) { $closed[$i] = $true; continue }
                if ($texts[$i].Length + $count -gt 65536) { throw 'Test output exceeded limit.' }
                [void]$texts[$i].Append($buffers[$i], 0, $count)
                $reads[$i] = $readers[$i].ReadAsync($buffers[$i], 0, 4096)
            }
        }
        Start-Sleep -Milliseconds 10
    }
    $output = $texts[0].ToString()
    # Only a single exact allowlisted line is eligible for the public report.
    # No native response, source error or other stdout/stderr is published.
    $diagnostics = @($output -split '\r?\n' | Where-Object { $_.StartsWith('native-preview-diagnostic') })
    $allowedStages = @(
        'admission', 'cleanup', 'close',
        'config-agents-enabled', 'config-cloud-skills-enabled', 'config-features-apps',
        'config-features-code_mode', 'config-features-code_mode_host', 'config-features-code_mode_only',
        'config-features-code_mode_prewarm', 'config-features-deferred_executor', 'config-features-executor_capability_discovery_v2',
        'config-features-goals', 'config-features-hooks', 'config-features-image_generation',
        'config-features-mcp_connection_sharing', 'config-features-memories', 'config-features-multi_agent_v2',
        'config-features-multi_env_copy_file', 'config-features-plugin_hooks', 'config-features-plugins',
        'config-features-request_permissions_tool', 'config-features-shell_tool', 'config-features-token_budget',
        'config-features-tool_suggest', 'config-hooks-notify', 'config-mcp',
        'config-orchestrator-mcp-enabled', 'config-provider', 'config-request',
        'config-web_search', 'done', 'initialize',
        'initialize-receipt', 'model-selection', 'models',
        'notification-item-completed', 'notification-item-started', 'notification-thread-settings-updated',
        'notification-thread-started', 'notification-thread-status-changed', 'notification-thread-tokenUsage-updated',
        'notification-turn-completed', 'notification-turn-started', 'notification-unsupported',
        'reply', 'start', 'test-isolation',
        'test-setup', 'test-transport', 'thread-receipt',
        'thread-request', 'turn-receipt', 'turn-request',
        'turn-stream', 'unclassified', 'version',
        'notification-configWarning', 'notification-warning', 'notification-account-updated',
        'config-rpc-other', 'config-rpc-invalid-params', 'config-rpc-invalid-request',
        'config-rpc-method-not-found', 'config-rpc-internal', 'config-rpc-server',
        'notification-envelope', 'config-rpc-write', 'config-rpc-transport',
        'config-rpc-canceled', 'config-rpc-eof', 'config-rpc-envelope',
        'config-rpc-id', 'notification-error', 'notification-thread-readState-changed',
        'notification-thread-archived', 'notification-thread-deleted', 'notification-thread-unarchived',
        'notification-thread-closed', 'notification-thread-reverted', 'notification-skills-changed',
        'notification-thread-name-updated', 'notification-thread-section-changed', 'notification-thread-attachment-updated',
        'notification-thread-goal-updated', 'notification-thread-prediction-updated', 'notification-thread-goal-cleared',
        'notification-thread-queue-changed', 'notification-project-changed', 'notification-thread-project-updated',
        'notification-thread-environment-connected', 'notification-thread-environment-disconnected', 'notification-hook-started',
        'notification-hook-completed', 'notification-turn-diff-updated', 'notification-turn-plan-updated',
        'notification-item-autoApprovalReview-started', 'notification-item-autoApprovalReview-completed', 'notification-autoApprovalReview-strictReviewRequired',
        'notification-rawResponseItem-completed', 'notification-rawResponse-completed', 'notification-item-agentMessage-delta',
        'notification-item-plan-delta', 'notification-command-exec-outputDelta', 'notification-process-outputDelta',
        'notification-process-exited', 'notification-item-commandExecution-outputDelta', 'notification-item-commandExecution-terminalInteraction',
        'notification-item-fileChange-outputDelta', 'notification-item-fileChange-patchUpdated', 'notification-serverRequest-resolved',
        'notification-item-mcpToolCall-progress', 'notification-mcpServer-oauthLogin-completed', 'notification-mcpServer-startupStatus-updated',
        'notification-mcpServer-event-stream-notification', 'notification-account-gatewayOAuth-changed', 'notification-account-rateLimits-updated',
        'notification-app-list-updated', 'notification-remoteControl-status-changed', 'notification-externalAgentConfig-import-progress',
        'notification-externalAgentConfig-import-completed', 'notification-fs-changed', 'notification-item-reasoning-summaryTextDelta',
        'notification-item-reasoning-summaryPartAdded', 'notification-item-reasoning-textDelta', 'notification-thread-compacted',
        'notification-model-rerouted', 'notification-model-verification', 'notification-modelProvider-authRecoveryStarted',
        'notification-modelProvider-authRecoveryCompleted', 'notification-turn-moderationMetadata', 'notification-model-safetyBuffering-updated',
        'notification-guardianWarning', 'notification-deprecationNotice', 'notification-fuzzyFileSearch-sessionUpdated',
        'notification-fuzzyFileSearch-sessionCompleted', 'notification-thread-realtime-started', 'notification-thread-realtime-itemAdded',
        'notification-thread-realtime-item-started', 'notification-thread-realtime-item-transcript-delta', 'notification-thread-realtime-item-completed',
        'notification-thread-realtime-transcript-delta', 'notification-thread-realtime-transcript-done', 'notification-thread-realtime-outputAudio-delta',
        'notification-thread-realtime-sdp', 'notification-thread-realtime-error', 'notification-thread-realtime-closed',
        'notification-windows-worldWritableWarning', 'notification-windowsSandbox-setupCompleted', 'notification-account-login-completed'
    )
    $allowedClasses = @('passed','failed-no-request','failed-request-seen','failed-fixture-rejected')
    $report.diagnostic_status = 'invalid'
    if ($diagnostics.Count -ne 1 -or
        $diagnostics[0] -cnotmatch '\Anative-preview-diagnostic stage=([^ ]+) class=([^ ]+)\z') {
        throw 'Required fixed diagnostic missing or ambiguous.'
    }
    $nativeStage = $Matches[1]; $nativeClass = $Matches[2]
    if ($allowedStages -cnotcontains $nativeStage -or $allowedClasses -cnotcontains $nativeClass) {
        throw 'Required fixed diagnostic is not allowlisted.'
    }
    $report.native_stage = $nativeStage; $report.native_class = $nativeClass
    $report.diagnostic_status = 'valid'
    if ($output.Length -gt 65536 -or  $texts[1].Length -gt 65536 -or $process.ExitCode -ne 0 -or
        $output -cnotmatch ('(?m)^--- PASS: ' + [regex]::Escape($TestName) + ' \(') -or
        $output -cmatch ('(?m)^--- SKIP: ' + [regex]::Escape($TestName)) -or
        $output -cnotmatch '(?m)^PASS\r?$') { throw 'Required native test did not pass.' }
    $report.test_passed = $true; $report.status = 'passed'; $report.stage = 'complete'
} catch {
    $report.status = 'failed'
    # Fixed host-only metadata; never publish the exception message or source text.
    if ($_.InvocationInfo.ScriptName -ceq $PSCommandPath) {
        $report.host_error_line = [int]$_.InvocationInfo.ScriptLineNumber
    }
    $report.host_error_category = switch ($_.Exception.GetBaseException().GetType().FullName) {
        'System.Management.Automation.RuntimeException' { 'powershell-runtime'; break }
        'System.Management.Automation.MethodException' { 'method-binding'; break }
        'System.Management.Automation.MethodInvocationException' { 'method-invocation'; break }
        'System.Management.Automation.PropertyNotFoundException' { 'property-missing'; break }
        'System.Management.Automation.PSInvalidOperationException' { 'powershell-operation'; break }
        'System.InvalidOperationException' { 'invalid-operation'; break }
        'System.ArgumentException' { 'argument'; break }
        'System.ArgumentNullException' { 'argument-null'; break }
        'System.IndexOutOfRangeException' { 'index-range'; break }
        'System.NullReferenceException' { 'null-reference'; break }
        'System.IO.IOException' { 'io'; break }
        'System.ObjectDisposedException' { 'disposed'; break }
        'System.UnauthorizedAccessException' { 'access'; break }
        'System.ComponentModel.Win32Exception' { 'win32'; break }
        'System.Threading.Tasks.TaskCanceledException' { 'task-cancelled'; break }
        default { 'other' }
    }
} finally {
    $report.test_process_started = [bool]$started
    try {
        if ($started) {
            if (-not $process.HasExited) { $process.Kill($true) }
            if (-not $process.WaitForExit(5000)) { throw 'Test cleanup incomplete.' }
        }
        if ($process) { $process.Dispose() }
        # Only this invocation's newly created root; no ACL changes or foreign kills.
        # Seven attempts at most, with a three-second retry window. The filesystem
        # call itself is synchronous; this is not a hard I/O completion deadline.
        $deleteWatch = [Diagnostics.Stopwatch]::StartNew()
        while ($true) {
            $report.cleanup_delete_attempts++
            try {
                Remove-Item -LiteralPath $root -Recurse -Force
                break
            } catch {
                if ($_.Exception.GetBaseException().HResult -ne -2147024864 -or
                    $report.cleanup_delete_attempts -ge 7 -or $deleteWatch.ElapsedMilliseconds -ge 3000) {
                    throw
                }
                $remaining = 3000 - $deleteWatch.ElapsedMilliseconds
                if ($remaining -le 0) { throw }
                Start-Sleep -Milliseconds ([int][Math]::Min(500, $remaining))
                if ($deleteWatch.ElapsedMilliseconds -ge 3000) { throw }
                $report.cleanup_sharing_retries++
            }
        }
        if (Test-Path -LiteralPath $root) { throw 'Owned directory remains.' }
        $report.cleanup_complete = $true
    } catch {
        $report.status = 'failed'; $report.stage = 'cleanup'
        # Preserve the original host/native failure; record cleanup separately.
        if ($_.InvocationInfo.ScriptName -ceq $PSCommandPath) {
            $report.cleanup_error_line = [int]$_.InvocationInfo.ScriptLineNumber
        }
        $report.cleanup_error_hresult = [int]$_.Exception.GetBaseException().HResult
        $report.cleanup_error_category = switch ($_.Exception.GetBaseException().GetType().FullName) {
            'System.Management.Automation.RuntimeException' { 'powershell-runtime'; break }
            'System.Management.Automation.MethodException' { 'method-binding'; break }
            'System.Management.Automation.MethodInvocationException' { 'method-invocation'; break }
            'System.Management.Automation.PropertyNotFoundException' { 'property-missing'; break }
            'System.Management.Automation.PSInvalidOperationException' { 'powershell-operation'; break }
            'System.InvalidOperationException' { 'invalid-operation'; break }
            'System.ArgumentException' { 'argument'; break }
            'System.ArgumentNullException' { 'argument-null'; break }
            'System.IndexOutOfRangeException' { 'index-range'; break }
            'System.NullReferenceException' { 'null-reference'; break }
            'System.IO.IOException' { 'io'; break }
            'System.ObjectDisposedException' { 'disposed'; break }
            'System.UnauthorizedAccessException' { 'access'; break }
            'System.ComponentModel.Win32Exception' { 'win32'; break }
            'System.Threading.Tasks.TaskCanceledException' { 'task-cancelled'; break }
            default { 'other' }
        }
    }
    [IO.File]::WriteAllText($reportPath, ($report | ConvertTo-Json -Compress), [Text.UTF8Encoding]::new($false))
}
if ($report.status -cne 'passed') { throw 'Official native text test failed; inspect fixed-stage report.' }
Write-Output 'PASS: official native loopback text test; Desktop and real accounts not tested.'
