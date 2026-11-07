# CI-only compiler. Dot-sourcing does not activate, notify, or change environment.
function New-EntryActivationHost([string]$OutputPath) {
    if ($PSVersionTable.PSEdition -cne 'Desktop' -or -not [IO.Path]::IsPathRooted($OutputPath) -or
        [IO.Path]::GetFullPath($OutputPath) -cne $OutputPath -or $OutputPath.Contains('"') -or
        [IO.Path]::GetFileName($OutputPath) -cne 'entry-activation-control.exe' -or
        [IO.File]::Exists($OutputPath) -or [IO.Directory]::Exists($OutputPath)) {
        throw 'Invalid private activation host output.'
    }
    $parentAttributes=[IO.File]::GetAttributes([IO.Path]::GetDirectoryName($OutputPath))
    if (($parentAttributes -band [IO.FileAttributes]::Directory) -eq 0 -or
        ($parentAttributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Invalid private activation host directory.' }
    # Compile before USER mutation in the working parent, never in a cold child PowerShell.
    Add-Type -OutputAssembly $OutputPath -OutputType ConsoleApplication -TypeDefinition @'
using System;
using System.Collections;
using System.Globalization;
using System.Runtime.InteropServices;
using System.Text.RegularExpressions;
namespace DesktopEntryActivation {
 [ComImport, Guid("2e941141-7f97-4756-ba1d-9decde894a3d"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
 interface IApplicationActivationManager {
  [PreserveSig] int ActivateApplication([MarshalAs(UnmanagedType.LPWStr)] string app,
   [MarshalAs(UnmanagedType.LPWStr)] string args, uint options, out uint processId);
  [PreserveSig] int ActivateForFile(IntPtr app, IntPtr items, IntPtr verb, out uint processId);
  [PreserveSig] int ActivateForProtocol(IntPtr app, IntPtr items, out uint processId);
 }
 static class Native {
  [DllImport("ole32.dll",ExactSpelling=true)] static extern int CoInitializeEx(IntPtr reserved,uint mode);
  [DllImport("ole32.dll",ExactSpelling=true)] static extern void CoUninitialize();
  [DllImport("ole32.dll",ExactSpelling=true)]
  static extern int CoCreateInstance(ref Guid clsid,IntPtr outer,uint context,ref Guid iid,
   [MarshalAs(UnmanagedType.Interface)] out IApplicationActivationManager manager);
  [DllImport("kernel32.dll",ExactSpelling=true)] internal static extern void SetLastError(uint error);
  [DllImport("user32.dll",CharSet=CharSet.Unicode,ExactSpelling=true,SetLastError=true)]
  internal static extern IntPtr SendMessageTimeoutW(IntPtr window,uint message,UIntPtr wparam,
   string lparam,uint flags,uint milliseconds,out UIntPtr result);
  internal static int Activate(string aumid,out uint processId) {
   processId=0;
   int initialized=CoInitializeEx(IntPtr.Zero,2); // STA, balanced even when already initialized.
   if(initialized<0)return initialized;
   IApplicationActivationManager manager=null;
   try {
    Guid clsid=new Guid("45BA127D-10A8-46EA-8AB7-56EA9078943C"),iid=new Guid("2e941141-7f97-4756-ba1d-9decde894a3d");
    // A short-lived caller uses the documented out-of-process activation manager lifetime.
    int hr=CoCreateInstance(ref clsid,IntPtr.Zero,4,ref iid,out manager); // CLSCTX_LOCAL_SERVER
    if(hr<0)return hr;
    return manager.ActivateApplication(aumid,"",0,out processId); // AO_NONE, one call only.
   } finally {
    try {if(manager!=null)Marshal.ReleaseComObject(manager);}
    finally {CoUninitialize();}
   }
  }
 }
 public static class Program {
  [STAThread] public static int Main(string[] args) {
   try {
    bool activate=args.Length==2 && args[0]=="activate" &&
     Regex.IsMatch(args[1],@"\AOpenAI\.Codex_[a-z0-9]+![A-Za-z0-9._-]+\z",RegexOptions.CultureInvariant);
    bool notify=args.Length==1 && args[0]=="notify";
    bool selfTest=args.Length==1 && args[0]=="self-test";
    if(!activate && !notify && !selfTest)throw new InvalidOperationException();
    foreach(DictionaryEntry item in Environment.GetEnvironmentVariables(EnvironmentVariableTarget.Process)) {
     string name=(string)item.Key;
     if(String.Equals(name,"CODEX_CLI_PATH",StringComparison.OrdinalIgnoreCase) ||
        String.Equals(name,"MAGPIE_APP_SERVER_BRIDGE_CONFIG",StringComparison.OrdinalIgnoreCase))throw new InvalidOperationException();
    }
    // The same supervisor closes redirected stdin. Even a BOM byte is not accepted.
    if(Console.OpenStandardInput().ReadByte()!=-1)throw new InvalidOperationException();
    if(selfTest){Console.WriteLine("entry-host-ready");return 0;} // No activation or notification APIs.
    if(notify) {
     UIntPtr result;
     Native.SetLastError(0);
     IntPtr sent=Native.SendMessageTimeoutW(new IntPtr(0xffff),0x001a,UIntPtr.Zero,"Environment",0x0002,1000,out result);
     uint error=sent==IntPtr.Zero?unchecked((uint)Marshal.GetLastWin32Error()):0;
     Console.WriteLine("{\"sent\":"+(sent==IntPtr.Zero?"false":"true")+",\"win32Error\":"+
      error.ToString(CultureInfo.InvariantCulture)+"}");return 0;
    }
    uint processId;
    int hr=Native.Activate(args[1],out processId);
    // Deliver a bounded numeric result even for a returned HRESULT failure; caller still rejects it.
    Console.WriteLine("{\"hresult\":"+hr.ToString(CultureInfo.InvariantCulture)+",\"processId\":"+
     processId.ToString(CultureInfo.InvariantCulture)+",\"callerOverridesAbsent\":true}");
    return 0;
   } catch {Console.WriteLine("activation-helper-failed");return 1;}
  }
 }
}
'@
    $stream=$null
    try {
        $stream=[IO.File]::Open($OutputPath,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)
        if ($stream.Length -lt 1 -or $stream.Length -gt 1048576) { throw 'Private activation host exceeds its bound.' }
        $algorithm=[Security.Cryptography.SHA256]::Create()
        try { $hash=([BitConverter]::ToString($algorithm.ComputeHash($stream))).Replace('-','').ToLowerInvariant() }
        finally { $algorithm.Dispose() }
        return [ordered]@{Path=$OutputPath;Stream=$stream;Sha256=$hash}
    } catch {
        if ($null -ne $stream) { $stream.Dispose() }
        throw 'Private activation host compilation or locking failed.'
    }
}
