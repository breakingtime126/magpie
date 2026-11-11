# Shared native primitives. No desktop capture, window activation, or UI actions.
function Initialize-OriginalCaptureNative {
    if ('OriginalCapture.Native' -as [type]) { return }
    Add-Type -ReferencedAssemblies System.Drawing -TypeDefinition @'
using System;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
namespace OriginalCapture {
 public static class Native {
  [StructLayout(LayoutKind.Sequential)] struct RECT { public int Left,Top,Right,Bottom; }
  [DllImport("user32.dll")] static extern bool IsWindow(IntPtr h);
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
  [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr h,out RECT r);
  [DllImport("user32.dll")] static extern bool PrintWindow(IntPtr h,IntPtr dc,uint flags);
  public static bool Matches(Process p,int id,long ticks,string exe,long window) {
   try {
    p.Refresh(); uint owner; var h=new IntPtr(window); var title=new StringBuilder(16);
    return !p.HasExited && p.Id==id && p.StartTime.ToUniversalTime().Ticks==ticks &&
     String.Equals(p.MainModule.FileName,exe,StringComparison.OrdinalIgnoreCase) &&
     p.MainWindowHandle==h && window!=0 && IsWindow(h) &&
     GetWindowThreadProcessId(h,out owner)!=0 && owner==id &&
     GetWindowText(h,title,16)==6 && title.ToString()=="magpie";
   } catch { return false; }
  }
  public static void Capture(int id,long ticks,string exe,long window,string output) {
   using(var p=Process.GetProcessById(id)) {
    if(!Matches(p,id,ticks,exe,window)) throw new InvalidOperationException();
    RECT r; var h=new IntPtr(window);
    if(!GetWindowRect(h,out r)) throw new InvalidOperationException();
    long w=(long)r.Right-r.Left, height=(long)r.Bottom-r.Top;
    if(w<1 || height<1 || w>1920 || height>1200) throw new InvalidOperationException();
    using(var bitmap=new Bitmap((int)w,(int)height,PixelFormat.Format32bppArgb)) {
     using(var g=Graphics.FromImage(bitmap)) {
      g.Clear(Color.Black); var dc=g.GetHdc();
      try {
       // Synchronous target rendering request, NOT passive observation. Killing this
       // helper bounds our wait, not the target's completion of rendering.
       if(!Matches(p,id,ticks,exe,window) || !PrintWindow(h,dc,2)) throw new InvalidOperationException();
      } finally { g.ReleaseHdc(dc); }
     }
     if(!Matches(p,id,ticks,exe,window)) throw new InvalidOperationException();
     using(var bytes=new MemoryStream()) {
      bitmap.Save(bytes,ImageFormat.Png);
      if(bytes.Length<67 || bytes.Length>10485760) throw new InvalidOperationException();
      using(var file=new FileStream(output,FileMode.CreateNew,FileAccess.Write,FileShare.None)) {
       bytes.Position=0; bytes.CopyTo(file);
      }
     }
    }
    if(!Matches(p,id,ticks,exe,window)) throw new InvalidOperationException();
   }
  }
  public static bool ValidPng(byte[] bytes) {
   try {
    if(bytes.Length<67 || bytes.Length>10485760) return false;
    byte[] signature={137,80,78,71,13,10,26,10};
    for(int i=0;i<8;i++) if(bytes[i]!=signature[i]) return false;
    if(bytes[8]!=0 || bytes[9]!=0 || bytes[10]!=0 || bytes[11]!=13 ||
       bytes[12]!=73 || bytes[13]!=72 || bytes[14]!=68 || bytes[15]!=82) return false;
    long w=((long)bytes[16]<<24)|((long)bytes[17]<<16)|((long)bytes[18]<<8)|bytes[19];
    long h=((long)bytes[20]<<24)|((long)bytes[21]<<16)|((long)bytes[22]<<8)|bytes[23];
    if(w<1 || h<1 || w>1920 || h>1200) return false;
    using(var s=new MemoryStream(bytes,false)) using(var image=Image.FromStream(s,true,true)) {
     return image.RawFormat.Guid==ImageFormat.Png.Guid && image.Width==w && image.Height==h;
    }
   } catch { return false; }
  }
 }
}
'@
}
