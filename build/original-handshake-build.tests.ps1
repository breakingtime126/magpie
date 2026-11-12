# Offline build-scope contracts. Never invokes the packager, Go, native UI or a process.
[CmdletBinding()]
param([string]$PackagerPath='')
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
function Assert([bool]$Condition,[string]$Message) { if (-not $Condition) { throw ('FAIL: '+$Message) } }
if (-not $PackagerPath) { $PackagerPath=Join-Path $PSScriptRoot 'package-native-magpie-windows.ps1' }
$tokens=$null;$errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($PackagerPath,[ref]$tokens,[ref]$errors)
Assert ($errors.Count -eq 0) 'packager parses'
$functions=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Get-OriginalPackageBuildTags'},$true))
Assert ($functions.Count -eq 1) 'one build-mode resolver'
Invoke-Expression $functions[0].Extent.Text
$callerTokens=$null;$callerErrors=$null
$caller=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'original-desktop-acceptance.ps1'),[ref]$callerTokens,[ref]$callerErrors)
Assert ($callerErrors.Count -eq 0) 'acceptance coordinator parses'
$callerFunctions=@($caller.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -ceq 'Get-AcceptanceBuildTags'},$true))
Assert ($callerFunctions.Count -eq 1) 'one coordinator build-mode resolver'
Invoke-Expression $callerFunctions[0].Extent.Text
foreach ($acceptance in @($false,$true)) {
    foreach ($stage in @('','stage-diagnostics-v1','wrong')) {
        foreach ($desktop in @($false,$true)) {
            foreach ($stageMarker in @($false,$true)) {
                foreach ($handshake in @($false,$true)) {
                    $message='ordinary commit'
                    if ($desktop) { $message+=' [original-connect-desktop-v1]' }
                    if ($stageMarker) { $message+=' [original-connect-stages-only-v1]' }
                    if ($handshake) { $message+=' [original-backend-handshake-v1]' }
                    $expected=-not $handshake -or ($acceptance -and $stage -ceq 'stage-diagnostics-v1' -and $desktop -and $stageMarker)
                    $allowed=$true;$result=$null
                    try { $result=Get-OriginalPackageBuildTags $acceptance $stage $message } catch { $allowed=$false }
                    Assert ($allowed -eq $expected) 'diagnostic tag needs every scope condition'
                    if ($allowed) {
                        $gui='production,nativecodex';$adapter='nogui,nativecodex'
                        if ($handshake) { $gui+=',nativehandshakediag';$adapter+=',nativehandshakediag' }
                        Assert ($result.gui -ceq $gui -and $result.adapter -ceq $adapter) 'both binaries use exact matching diagnostic choice'
                        Assert ($result.handshakeDiagnostic -eq $handshake) 'build metadata reflects exact opt-in'
                        if ($acceptance) {
                            $coordinator=Get-AcceptanceBuildTags ($stage -ceq 'stage-diagnostics-v1') $message
                            Assert ($coordinator.gui -ceq $result.gui -and $coordinator.adapter -ceq $result.adapter -and
                                $coordinator.handshakeDiagnostic -eq $result.handshakeDiagnostic) 'coordinator and packager bind the same mode'
                        }
                    }
                }
            }
        }
    }
}
$text=$ast.Extent.Text
Assert ($text.Contains('& go build -tags $buildMode.adapter') -and $text.Contains('& go build -tags $buildMode.gui')) 'actual builds use resolved tags'
Assert ($text.Contains("& go test -v -tags nogui,nativecodex,nativehandshakediag -count=1 -timeout=5m -run '^Test(Handshake|OwnedStartupProbe)' ./internal/nativehandshake ./internal/nativehost ./internal/nativelaunch ./internal/appserverbridge ./cmd/magpie-codex-bridge")) 'Windows gate exercises actual probe environment admission and handshake observations'
Assert ($text.Contains('[regex]::Escape($buildMode.gui)') -and $text.Contains('buildTags=$buildMode.gui;adapterBuildTags=$buildMode.adapter;handshakeDiagnostic=[bool]$buildMode.handshakeDiagnostic')) 'provenance checks and manifest use actual tags'
Assert ($text.Contains("if (`$env:MAGPIE_ORIGINAL_STAGE_DIAGNOSTICS) { throw 'Stage diagnostics cannot publish an acceptance package.' }")) 'diagnostic build cannot publish acceptance package'
$workflow=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../.github/workflows/experimental-codex-connectivity-preview.yml'))
Assert ($workflow.Contains("if: success() && !contains(github.event.head_commit.message, '[original-connect-stages-only-v1]')")) 'diagnostic package remains excluded from upload'
Write-Output 'ORIGINAL_HANDSHAKE_BUILD_SCOPE_TESTS_PASSED'
