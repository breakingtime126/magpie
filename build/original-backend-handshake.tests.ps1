$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-backend-handshake.ps1')
function Assert($Value,[string]$Name) { if (-not $Value) { throw "Handshake fixture failed: $Name" } }
function Bytes([string[]]$Stages) {
    $lines=@();for($i=0;$i -lt $Stages.Count;$i++) { $lines+=('native-backend-handshake-v1 sequence={0} stage={1}' -f ($i+1),$Stages[$i]) }
    return [Text.Encoding]::UTF8.GetBytes(($lines -join "`n")+"`n")
}
$all=@('adapter-entered','jsonl-requested','profile-authenticated','endpoint-allocated','primary-started','secondary-started','initialize-received','primary-initialize-written','secondary-initialize-written','primary-initialize-reply','secondary-initialize-reply','initialization-ready','initialize-output-written')
$ok=ConvertFrom-OriginalHandshakeLog (Bytes $all) 'bridge'
Assert ($ok.status -ceq 'observed' -and $ok.initializeOutputWrittenObserved) 'full single-invocation sequence'
foreach ($stage in $all) {
    $bad=ConvertFrom-OriginalHandshakeLog (Bytes @($all|Where-Object {$_ -cne $stage})) 'bridge'
    Assert (-not $bad.initializeOutputWrittenObserved) ('missing prerequisite '+$stage)
}
$partial=ConvertFrom-OriginalHandshakeLog (Bytes $all[0..11]) 'bridge'
Assert ($partial.status -ceq 'observed' -and -not $partial.initializeOutputWrittenObserved) 'ready does not prove write'
$hostTrace=ConvertFrom-OriginalHandshakeLog (Bytes @('host-listening','host-probe-admitted','host-probe-acknowledged','host-jsonl-admitted')) 'host'
Assert ($hostTrace.status -ceq 'observed' -and -not $hostTrace.initializeOutputWrittenObserved) 'host bootstrap separate'
$probe=ConvertFrom-OriginalHandshakeLog (Bytes @('adapter-entered','startup-probe-requested','profile-authenticated','endpoint-allocated','probe-result-validated','probe-output-written')) 'bridge'
Assert ($probe.status -ceq 'observed' -and -not $probe.initializeOutputWrittenObserved) 'one-shot inspection separate'
foreach ($stage in @('probe-environment-admitted','probe-environment-rejected')) {
    $environment=ConvertFrom-OriginalHandshakeLog (Bytes @('adapter-entered','startup-probe-requested','profile-authenticated','endpoint-allocated',$stage)) 'bridge'
    Assert ($environment.status -ceq 'observed' -and -not $environment.initializeOutputWrittenObserved) 'probe environment admission is not a native result'
    $mixed=ConvertFrom-OriginalHandshakeLog (Bytes @($all+$stage)) 'bridge'
    Assert ($mixed.status -ceq 'unknown' -and -not $mixed.initializeOutputWrittenObserved) 'probe environment cannot combine with JSONL acknowledgement'
}
foreach ($stage in @('primary-exited','bridge-context-cancelled','client-stream-ended','primary-stream-ended','secondary-stream-ended')) {
    $failure=ConvertFrom-OriginalHandshakeLog (Bytes @('adapter-entered','jsonl-requested',$stage,'jsonl-failed')) 'bridge'
    Assert ($failure.status -ceq 'observed' -and -not $failure.initializeOutputWrittenObserved) 'fixed stream end is not successful initialization'
}
foreach ($bytes in @([Text.Encoding]::UTF8.GetBytes("native-backend-handshake-v1 sequence=1 stage=adapter-entered"),[Text.Encoding]::UTF8.GetBytes("native-backend-handshake-v1 sequence=2 stage=adapter-entered`n"),[Text.Encoding]::UTF8.GetBytes("native-backend-handshake-v1 sequence=1 stage=PRIVATE_DATA`n"),(Bytes @('adapter-entered','adapter-entered')))) {
    $bad=ConvertFrom-OriginalHandshakeLog $bytes 'bridge'
    Assert ($bad.status -ceq 'unknown' -and -not $bad.initializeOutputWrittenObserved -and ($bad|ConvertTo-Json -Depth 5) -notmatch 'PRIVATE_DATA') 'malformed or repeated records stay unknown'
}
Assert ((ConvertFrom-OriginalHandshakeLog (Bytes $all) 'host').status -ceq 'unknown') 'role mixing'
Assert ((ConvertFrom-OriginalHandshakeLog ([byte[]]::new(16385)) 'bridge').reason -ceq 'limit') 'byte bound'
$report=New-OriginalBackendHandshakeReport
Assert (-not $report.backendHandshakeVerified -and -not $report.desktopProvenanceVerified -and -not $report.changesRecoveryDecision -and -not $report.authorizesAction) 'no acceptance authority'
Assert ((Get-OriginalBackendHandshake).reason -ceq 'sequence-evidence-required') 'recovery gates precede IO'
# Source parity checks the exact writer's fixed dictionary, not copied runtime records.
$source=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../internal/nativehandshake/observe.go'))
$goStages=@([regex]::Matches($source,':\s*"([a-z-]+)"')|ForEach-Object{$_.Groups[1].Value})
$psStages=Get-OriginalHandshakeStages
Assert ($goStages.Count -eq $psStages.Count) 'cross-language stage count'
foreach ($stage in $goStages) { Assert ($psStages -ccontains $stage) 'cross-language stage identity' }
$writer=[IO.File]::ReadAllText((Join-Path $PSScriptRoot '../internal/nativehandshake/observe_enabled.go'))
Assert ($writer.Contains('native-backend-handshake-v1 sequence=%d stage=%s\n')) 'actual writer grammar'
# Portable reader fixtures replace only the Windows fixed-drive metadata
# validator. Every byte read remains in this test's newly created directory.
$savedPathValidator=(Get-Item Function:Test-OriginalDiagnosticPath).ScriptBlock
$script:handshakeRoot=Join-Path ([IO.Path]::GetTempPath()) ('handshake-reader-'+[guid]::NewGuid().ToString('N'))
$null=[IO.Directory]::CreateDirectory($script:handshakeRoot)
$script:handshakeDeny=$false;$script:handshakeChange=$false;$script:handshakePathCalls=0
function Test-OriginalDiagnosticPath([string]$Path,[bool]$Directory) {
    if (-not $Path.StartsWith($script:handshakeRoot+[IO.Path]::DirectorySeparatorChar)) { throw 'Fixture path escaped.' }
    $script:handshakePathCalls++
    if ($script:handshakeDeny) { return $false }
    $attrs=[IO.File]::GetAttributes($Path)
    if ($script:handshakeChange -and $script:handshakePathCalls -eq 2) { [IO.File]::WriteAllText($Path,'changed synthetic file') }
    return ($attrs -band ([IO.FileAttributes]::Directory -bor [IO.FileAttributes]::ReparsePoint)) -eq 0
}
try {
    $floor=[DateTime]::UtcNow.Ticks
    $one=Join-Path $script:handshakeRoot 'native-handshake-bridge.01.log'
    Assert ((Read-OriginalHandshakeFile $one 'bridge' $floor).reason -ceq 'missing') 'missing reader input stays unknown'
    [IO.File]::WriteAllBytes($one,(Bytes $all))
    Assert ((Read-OriginalHandshakeFile $one 'bridge' $floor).initializeOutputWrittenObserved) 'reader preserves complete writer-shaped input'
    $future=[IO.FileInfo]::new($one).CreationTimeUtc.Ticks+1
    Assert ((Read-OriginalHandshakeFile $one 'bridge' $future).reason -ceq 'attempt-binding') 'stale file rejected'
    $script:handshakeDeny=$true
    Assert ((Read-OriginalHandshakeFile $one 'bridge' $floor).reason -ceq 'path') 'unsafe path rejected before metadata/content'
    $script:handshakeDeny=$false;$script:handshakeChange=$true;$script:handshakePathCalls=0
    Assert ((Read-OriginalHandshakeFile $one 'bridge' $floor).reason -ceq 'changed') 'changed file rejected'
    $script:handshakeChange=$false
    [IO.File]::WriteAllBytes($one,[byte[]]::new(0))
    Assert ((Read-OriginalHandshakeFile $one 'bridge' $floor).reason -ceq 'empty') 'empty existing file not absence'
    [IO.File]::WriteAllBytes($one,(Bytes $all[0..11]))
    $two=Join-Path $script:handshakeRoot 'native-handshake-bridge.02.log'
    [IO.File]::WriteAllBytes($two,(Bytes @('adapter-entered','initialize-output-written')))
    $snapshot=Read-OriginalHandshakeDirectory $script:handshakeRoot $floor
    Assert ($snapshot.status -ceq 'unknown' -and $snapshot.fileCount -eq 2 -and -not $snapshot.initializeOutputWrittenObserved) 'separate files cannot combine handshake'
    [IO.File]::WriteAllBytes($one,(Bytes $all))
    $snapshot=Read-OriginalHandshakeDirectory $script:handshakeRoot $floor
    Assert ($snapshot.status -ceq 'unknown' -and $snapshot.initializeOutputWrittenObserved -and -not $snapshot.backendHandshakeVerified) 'valid milestone survives another unknown file without overall success'
    Assert ($snapshot.files[0].slot -eq 1 -and $snapshot.files[1].slot -eq 2) 'fixed slot identity retained'
    [IO.File]::WriteAllBytes((Join-Path $script:handshakeRoot 'native-handshake-bridge.17.log'),(Bytes $all))
    Assert ((Read-OriginalHandshakeDirectory $script:handshakeRoot $floor).fileCount -eq 2) 'fixed slot bound ignores unapproved filename'
} finally {
    Set-Item Function:Test-OriginalDiagnosticPath $savedPathValidator
    [IO.Directory]::Delete($script:handshakeRoot,$true)
}
$runtime=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'original-backend-handshake.ps1'))
Assert (-not $runtime.Contains('[IO.File]::GetAttributes($Path)')) 'reader never touches target before ancestor validator'
$tokens=$null;$errors=$null
$null=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'original-backend-handshake.ps1'),[ref]$tokens,[ref]$errors)
Assert ($errors.Count -eq 0) 'runtime parses'
Write-Output 'Original backend handshake fixed-observation fixtures passed.'

# Shared-state startup may defer secondary until the genuine primary reply.
$serial=@('adapter-entered','jsonl-requested','profile-authenticated','endpoint-allocated','primary-started','initialize-received','primary-initialize-written','primary-initialize-reply','secondary-started','secondary-initialize-written','secondary-initialize-reply','initialization-ready','initialize-output-written')
$serialOK=ConvertFrom-OriginalHandshakeLog (Bytes $serial) 'bridge'
Assert ($serialOK.status -ceq 'observed' -and $serialOK.initializeOutputWrittenObserved) 'serialized genuine initialize sequence'
foreach ($missing in @('primary-initialize-reply','secondary-started','secondary-initialize-reply')) {
    $bad=ConvertFrom-OriginalHandshakeLog (Bytes @($serial|Where-Object {$_ -cne $missing})) 'bridge'
    Assert (-not $bad.initializeOutputWrittenObserved) 'serialized prerequisite cannot be omitted'
}
$premature=@($serial.Clone())
$premature[7]='secondary-started';$premature[8]='primary-initialize-reply'
Assert (-not (ConvertFrom-OriginalHandshakeLog (Bytes $premature) 'bridge').initializeOutputWrittenObserved) 'late secondary start requires earlier successful primary reply'

# Fixed source/shape/pending/class facts never establish readiness.
foreach ($stage in @('protocol-source-client','protocol-source-primary','protocol-source-secondary','protocol-class-envelope','protocol-class-correlation','protocol-class-unscoped-request','protocol-class-thread-ownership','protocol-class-initialization','protocol-class-client-notification','protocol-class-other','protocol-shape-notification','protocol-shape-request','protocol-shape-response','protocol-shape-invalid','protocol-class-admission','protocol-class-catalog','protocol-pending-initialize','protocol-pending-policy','protocol-pending-config','protocol-pending-catalog','protocol-pending-none','protocol-pending-other')) {
    $partial=ConvertFrom-OriginalHandshakeLog (Bytes @('adapter-entered','jsonl-requested',$stage,'protocol-failed')) 'bridge'
    Assert ($partial.status -ceq 'observed' -and -not $partial.initializeOutputWrittenObserved) 'fixed failure category is metadata only'
}

foreach ($stage in @('catalog-reason-pin','catalog-reason-backend-count','catalog-reason-empty-list','catalog-reason-json','catalog-reason-json-limit','catalog-reason-model-schema','catalog-reason-effort-null','catalog-reason-effort-unknown','catalog-reason-duplicate-model','catalog-reason-duplicate-effort','catalog-reason-default-unsupported','catalog-reason-no-visible-default','catalog-reason-snapshot','catalog-reason-settings-store','catalog-reason-router','catalog-reason-default-mismatch','catalog-reason-policy-schema','catalog-reason-policy-size','catalog-reason-policy-mismatch','catalog-reason-policy-nonnull')) {
    $partial=ConvertFrom-OriginalHandshakeLog (Bytes @('adapter-entered','jsonl-requested',$stage,'protocol-failed')) 'bridge'
    Assert ($partial.status -ceq 'observed' -and -not $partial.initializeOutputWrittenObserved) 'catalog failure reason has no readiness authority'
}
