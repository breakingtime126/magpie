# Dedicated GUI artifact. No ordinary Magpie binary or old profile helper.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
function Assert-Exit([string]$Operation) { if ($LASTEXITCODE -ne 0) { throw "$Operation failed." } }
$head = (& git rev-parse HEAD).Trim(); Assert-Exit 'Read source'
if ($head -cne $env:GITHUB_SHA) { throw 'Source commit mismatch.' }
$package = Join-Path $env:RUNNER_TEMP 'codex-connectivity-package'
$evidence = Join-Path $env:RUNNER_TEMP 'codex-connectivity-evidence'
[void](New-Item -ItemType Directory -Path $package -ErrorAction Stop)
[void](New-Item -ItemType Directory -Path $evidence -ErrorAction Stop)
& go vet -tags nogui ./internal/nativepreview ./internal/codexsetup
Assert-Exit 'Windows pure-package vet'
& go vet -tags production ./cmd/magpie-codex-setup ./internal/codexsetup
Assert-Exit 'Windows GUI vet'
& node internal/codexsetup/app.test.cjs
Assert-Exit 'Page-state synthetic regressions'
# Only synthetic controller/handler tests. Neither suite uses a real account.
& go test -tags nogui -count=1 -timeout=3m ./internal/nativepreview ./internal/codexsetup
Assert-Exit 'Synthetic preview tests'
$exe = Join-Path $package 'Magpie-Codex-Connectivity-Preview.exe'
& go build -tags production -trimpath -ldflags '-s -w -H windowsgui' -o $exe ./cmd/magpie-codex-setup
Assert-Exit 'Independent Wails GUI build'
Copy-Item -LiteralPath 'LICENSE','THIRD_PARTY_NOTICES' -Destination $package
Copy-Item -LiteralPath 'docs/subsystems/codex-connectivity-preview.md' -Destination (Join-Path $package 'README.md')
@{ sourceCommit=$head; guiSHA256=(Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant(); unsigned=$true } |
 ConvertTo-Json | Set-Content -LiteralPath (Join-Path $package 'BUILD.json') -Encoding UTF8
# Capture only the window owned by this newly launched, no-action preview.
# No UIA, injected input, ordinary Desktop, login, native model service or setup.
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class PreviewWindowCapture {
 [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
 [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
 [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
}
'@
$expectedTitle = '"Magpie \u00b7 Codex \u4f9b\u5e94\u5546\u9884\u89c8"' | ConvertFrom-Json
$process = $null
try {
 $process = Start-Process -FilePath $exe -PassThru
 $until = [DateTime]::UtcNow.AddSeconds(30)
 do {
  Start-Sleep -Milliseconds 250
  $process.Refresh()
  if ($process.HasExited) { throw 'Preview exited before showing its own window.' }
  if ($process.MainWindowHandle -ne [IntPtr]::Zero -and $process.MainWindowTitle -ceq $expectedTitle) { break }
 } while ([DateTime]::UtcNow -lt $until)
 if ($process.MainWindowHandle -eq [IntPtr]::Zero -or $process.MainWindowTitle -cne $expectedTitle) {
  throw 'Expected preview window unavailable; WebView2/interactive session not verified.'
 }
 Start-Sleep -Seconds 2
 $process.Refresh()
 $rect = New-Object PreviewWindowCapture+RECT
 if (-not [PreviewWindowCapture]::GetWindowRect($process.MainWindowHandle,[ref]$rect)) { throw 'Window bounds unavailable.' }
 $width=$rect.Right-$rect.Left; $height=$rect.Bottom-$rect.Top
 if ($width -le 0 -or $height -le 0 -or $width -gt 2000 -or $height -gt 2000) { throw 'Unexpected own-window bounds.' }
 $bitmap=[Drawing.Bitmap]::new($width,$height);$graphics=[Drawing.Graphics]::FromImage($bitmap);$dc=$graphics.GetHdc()
 try {
  if (-not [PreviewWindowCapture]::PrintWindow($process.MainWindowHandle,$dc,2)) { throw 'Own-window capture failed.' }
 } finally { $graphics.ReleaseHdc($dc);$graphics.Dispose() }
 try { $bitmap.Save((Join-Path $evidence 'preview-window.png'),[Drawing.Imaging.ImageFormat]::Png) } finally { $bitmap.Dispose() }
 @{ sourceCommit=$head; stage='own-window-captured-not-controls-accepted'; nativeActions=0; realAccountsUsed=$false } |
  ConvertTo-Json | Set-Content -LiteralPath (Join-Path $evidence 'report.json') -Encoding UTF8
} finally {
 if ($null -ne $process) {
  if (-not $process.HasExited) { [void]$process.CloseMainWindow(); if (-not $process.WaitForExit(5000)) { $process.Kill(); if (-not $process.WaitForExit(3000)) { throw 'Preview shutdown unconfirmed.' } } }
  $process.Dispose()
 }
}
Get-ChildItem -LiteralPath $package -File | Sort-Object Name | ForEach-Object {
 ((Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $_.Name)
} | Set-Content -LiteralPath (Join-Path $package 'SHA256SUMS') -Encoding ascii
