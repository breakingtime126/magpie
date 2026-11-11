Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$source=Join-Path $PSScriptRoot 'original-connect-preconditions.ps1'
$tests=Join-Path $PSScriptRoot 'original-connect-preconditions.tests.ps1'
$original=[IO.File]::ReadAllText($source)
$dir=Join-Path ([IO.Path]::GetTempPath()) ('precondition-mutations-'+[guid]::NewGuid().ToString('N'))
$null=[IO.Directory]::CreateDirectory($dir)
try {
    $mutations=@(
        @('$facts.storeSignatureKind=([string]$app.SignatureKind -ceq ''Store'')','$facts.storeSignatureKind=$true'),
        @('$facts.exactPackageFamily=([string]$app.PackageFamilyName -ceq ''OpenAI.Codex_2p2nqsd0c76g0'')','$facts.exactPackageFamily=$true'),
        @("return 'unknown'","return 'passed'"),
        @('changesRecoveryDecision=$false','changesRecoveryDecision=$true'),
        @('$entries.Count -ge 128','$entries.Count -ge 999999')
    )
    $i=0
    foreach ($m in $mutations) {
        if (-not $original.Contains($m[0])) {throw 'Mutation target absent.'}
        $path=Join-Path $dir ('mutation-'+$i+'.ps1');[IO.File]::WriteAllText($path,$original.Replace($m[0],$m[1]))
        $failed=$false
        try { & $tests -Implementation $path | Out-Null } catch { if ($_.Exception.Message -notlike '*FAIL:*') {throw 'Mutation failed outside assertion.'};$failed=$true }
        if (-not $failed) {throw 'Mutation survived.'}
        Write-Output ('PASS mutation '+$i+' rejected');$i++
    }
} finally { [IO.Directory]::Delete($dir,$true) }
