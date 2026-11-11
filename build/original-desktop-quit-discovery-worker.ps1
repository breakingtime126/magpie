# Read-only Win32 probe. Supervisor supplies the external wall-clock deadline.
param([int]$TargetProcessId,[long]$TargetStartTicks,[string]$TargetExecutable)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
. (Join-Path $PSScriptRoot 'original-desktop-quit-discovery-core.ps1')
$r=Get-OriginalTrayObservation -TargetProcessId 0 -NextWindow { throw 'Not initialized.' } -Owner {} -Rectangle {} -Verify {}
$r.reason='context';$ownerProcess=$null
try {
    if ($PSVersionTable.PSEdition -cne 'Core' -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:GITHUB_EVENT_NAME -cne 'push' -or $env:RUNNER_OS -cne 'Windows' -or
        $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or $env:GITHUB_REPOSITORY -cne 'breakingtime126/magpie' -or
        $env:GITHUB_REF -cne 'refs/heads/experiment/app-server-bridge-v0.1.3035' -or
        $env:MAGPIE_ORIGINAL_QUIT_DISCOVERY -cne 'read-only-v1') { throw 'Context unavailable.' }
    $r.reason='identity'
    if ($TargetProcessId -le 0 -or $TargetStartTicks -le 0 -or -not [IO.Path]::IsPathFullyQualified($TargetExecutable)) { throw 'Owner required.' }
    $ownerProcess=Get-Process -Id $TargetProcessId -ErrorAction Stop;$null=$ownerProcess.Handle
    $watch=[Diagnostics.Stopwatch]::StartNew()
    function Assert-TrayOwner {
        if ($watch.ElapsedMilliseconds -ge 10000) { throw 'Probe deadline.' }
        $ownerProcess.Refresh()
        if ($ownerProcess.HasExited -or $ownerProcess.Id -ne $TargetProcessId -or
            $ownerProcess.StartTime.ToUniversalTime().Ticks -ne $TargetStartTicks -or
            $ownerProcess.MainModule.FileName -cne $TargetExecutable) { throw 'Owner changed.' }
    }
    Assert-TrayOwner
    $r.reason='read'
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace OriginalTrayDiscovery {
    [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct Identifier {
        public uint Size; public IntPtr Window; public uint UID; public Guid Guid;
    }
    public static class Native {
        [DllImport("user32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
        public static extern IntPtr FindWindowExW(IntPtr parent, IntPtr after, string cls, string title);
        [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr window, out uint pid);
        [DllImport("shell32.dll")] public static extern int Shell_NotifyIconGetRect(ref Identifier id, out Rect rect);
        [DllImport("user32.dll")] public static extern int GetSystemMetrics(int index);
        [DllImport("kernel32.dll")] public static extern void SetLastError(uint error);
        public static long Next(long after) {
            SetLastError(0);
            IntPtr found = FindWindowExW(new IntPtr(-3), new IntPtr(after), "WailsWebviewWindow", null);
            if (found == IntPtr.Zero && Marshal.GetLastWin32Error() != 0) throw new InvalidOperationException();
            return found.ToInt64();
        }
        public static int Owner(long window) {
            uint pid; if (GetWindowThreadProcessId(new IntPtr(window), out pid)==0) throw new InvalidOperationException();
            return checked((int)pid);
        }
    }
}
'@
    $uiaAvailable=$false
    try { Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes,WindowsBase;$uiaAvailable=$true } catch { }
    $r=Get-OriginalTrayObservation -TargetProcessId $TargetProcessId -Verify { Assert-TrayOwner } `
        -NextWindow { param($last) [OriginalTrayDiscovery.Native]::Next($last) } `
        -Owner { param($window) [OriginalTrayDiscovery.Native]::Owner($window) } `
        -Rectangle { param($window)
            $id=[OriginalTrayDiscovery.Identifier]::new()
            $id.Size=[Runtime.InteropServices.Marshal]::SizeOf($id);$id.Window=[IntPtr]$window;$id.UID=1;$id.Guid=[guid]::Empty
            $rect=[OriginalTrayDiscovery.Rect]::new()
            $hr=[OriginalTrayDiscovery.Native]::Shell_NotifyIconGetRect([ref]$id,[ref]$rect)
            $data=@{hitTest='unavailable';success=($hr -eq 0);left=$rect.Left;top=$rect.Top;right=$rect.Right;bottom=$rect.Bottom;
                screenX=[OriginalTrayDiscovery.Native]::GetSystemMetrics(76);screenY=[OriginalTrayDiscovery.Native]::GetSystemMetrics(77);
                screenWidth=[OriginalTrayDiscovery.Native]::GetSystemMetrics(78);screenHeight=[OriginalTrayDiscovery.Native]::GetSystemMetrics(79)}
            if ($uiaAvailable -and $hr -eq 0 -and $rect.Right -gt $rect.Left -and $rect.Bottom -gt $rect.Top) {
                $point=[Windows.Point]::new(([double]$rect.Left+$rect.Right)/2,([double]$rect.Top+$rect.Bottom)/2)
                $data.hitTest=Get-OriginalTrayHit -Rectangle $data -Verify { Assert-TrayOwner } -ReadHit {
                    $element=[Windows.Automation.AutomationElement]::FromPoint($point)
                    if ($null -eq $element) { return $null }
                    $current=$element.Current;$box=$current.BoundingRectangle;$pattern=$null
                    $supported=$element.TryGetCurrentPattern([Windows.Automation.InvokePattern]::Pattern,[ref]$pattern)
                    @{name=$current.Name;role=$current.ControlType.ProgrammaticName;enabled=$current.IsEnabled;
                        offscreen=$current.IsOffscreen;invoke=$supported;runtime=($element.GetRuntimeId() -join ',');
                        left=$box.Left;top=$box.Top;right=$box.Right;bottom=$box.Bottom}
                }
            }
            # The reducer rechecks retained generation and tray HWND ownership
            # after UIA, even if the optional hit reader returned unavailable.
            return $data
        }
} catch { } finally { if ($null -ne $ownerProcess) { $ownerProcess.Dispose() } }
Assert-OriginalTrayReport $r
$r | ConvertTo-Json -Compress
