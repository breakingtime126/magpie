# This helper runs with an explicit allowlisted process environment, never USER override copies.
[CmdletBinding()]
param([string]$Aumid = '', [switch]$NotifyEnvironment)
$ErrorActionPreference='Stop'
try {
    if (-not $NotifyEnvironment -and $Aumid -cnotmatch '^OpenAI\.Codex_[a-z0-9]+![A-Za-z0-9._-]+$') { throw 'target' }
    $variables=[Environment]::GetEnvironmentVariables([EnvironmentVariableTarget]::Process)
    foreach ($name in @('CODEX_CLI_PATH','MAGPIE_APP_SERVER_BRIDGE_CONFIG')) {
        if ($variables.Contains($name)) { throw 'override' }
    }
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace DesktopEntryActivation {
 [ComImport, Guid("2e941141-7f97-4756-ba1d-9decde894a3d"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
 interface IApplicationActivationManager {
  [PreserveSig] int ActivateApplication([MarshalAs(UnmanagedType.LPWStr)] string app,
   [MarshalAs(UnmanagedType.LPWStr)] string args, uint options, out uint processId);
  [PreserveSig] int ActivateForFile(IntPtr app, IntPtr items, IntPtr verb, out uint processId);
  [PreserveSig] int ActivateForProtocol(IntPtr app, IntPtr items, out uint processId);
 }
 [ComImport, Guid("45BA127D-10A8-46EA-8AB7-56EA9078943C")] class ApplicationActivationManager {}
 public static class Native {
  [DllImport("user32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
  public static extern IntPtr SendMessageTimeout(IntPtr window, uint message, UIntPtr wparam,
   string lparam, uint flags, uint milliseconds, out UIntPtr result);
  public static int Activate(string aumid, out uint processId) {
   var manager=new ApplicationActivationManager();
   try { return ((IApplicationActivationManager)manager).ActivateApplication(aumid,"",0,out processId); }
   finally { Marshal.ReleaseComObject(manager); }
  }
 }
}
'@
    if ($NotifyEnvironment) {
        [UIntPtr]$result=[UIntPtr]::Zero
        $sent=[DesktopEntryActivation.Native]::SendMessageTimeout([IntPtr]0xffff,0x001a,
            [UIntPtr]::Zero,'Environment',0x0002,1000,[ref]$result)
        if ($sent -eq [IntPtr]::Zero) { Write-Output 'unconfirmed' } else { Write-Output 'sent' }
        exit 0
    }
    [uint32]$launched=0
    $hr=[DesktopEntryActivation.Native]::Activate($Aumid,[ref]$launched)
    if ($hr -ne 0 -or $launched -le 0) { throw 'activation' }
    [ordered]@{hresult=[int]$hr;processId=[int]$launched;callerOverridesAbsent=$true} | ConvertTo-Json -Compress
    exit 0
} catch { Write-Output 'activation-helper-failed'; exit 1 }
