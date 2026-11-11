# Isolated bounded capture child ownership. No Magpie process is stopped here.
function Initialize-OriginalCaptureSupervisor {
    if ('OriginalCaptureSupervisor.BoundedProcess' -as [type]) { return }
    Add-Type -TypeDefinition @'
using System;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
namespace OriginalCaptureSupervisor {
 public sealed class BoundedProcess : IDisposable {
  readonly object gate=new object(); readonly StringBuilder output=new StringBuilder();
  readonly Process process; readonly int limit; readonly Timer timer;
  readonly Task stdout, stderr; volatile bool timedOut, overflow, errorOutput; bool disposed;
  public bool ErrorOutput { get { return errorOutput; } }
  public bool TimedOut { get { return timedOut; } } public bool Overflow { get { return overflow; } }
  public bool Exited { get { try { return process.HasExited; } catch { return false; } } }
  public int ExitCode { get { return process.ExitCode; } }
  public string Output { get { lock(gate) return output.ToString(); } }
  public bool Drained { get { return stdout.IsCompleted && stderr.IsCompleted; } }
  public BoundedProcess(ProcessStartInfo start, int maxBytes, int milliseconds)
   : this(start,maxBytes,milliseconds,null) {}
  public sealed class ConstructionFailure : InvalidOperationException {
   public bool ExitConfirmed { get; private set; }
   public ConstructionFailure(bool confirmed) : base(confirmed
    ? "Bounded process construction failed; owned process exit confirmed."
    : "Bounded process construction failed; owned process exit unconfirmed.") { ExitConfirmed=confirmed; }
  }
  // Fault at the start-call boundary: launch may associate the retained Process,
  // then throw BEFORE the constructor receives a return value from that call.
  // The fixture owns and finally reaps the same object even under a red mutation.
  public static bool[] TestFailureDuringStart(ProcessStartInfo start, bool associate) {
   Process retained=null;
   try {
    using(var unexpected=new BoundedProcess(start,8192,30000,p=> {
     retained=p;
     if(associate && !p.Start()) throw new InvalidOperationException();
     throw new InvalidOperationException("Injected start-call exception before return.");
    })) { throw new InvalidOperationException("Fixture did not fail."); }
   } catch(ConstructionFailure failure) {
    bool exited=false;
    try { exited=retained.HasExited; } catch {}
    return new[]{failure.ExitConfirmed,exited};
   } finally {
    if(retained!=null) {
     try { if(!retained.HasExited) retained.Kill(); retained.WaitForExit(5000); } catch {}
     retained.Dispose();
    }
   }
  }
  BoundedProcess(ProcessStartInfo start, int maxBytes, int milliseconds, Func<Process,bool> startOperation) {
   if(start==null || maxBytes<=0 || milliseconds<=0) throw new ArgumentException("Invalid bounded process inputs.");
   limit=maxBytes; process=new Process(); process.StartInfo=start; bool attempted=false;
   try {
    attempted=true;
    if(!(startOperation==null ? process.Start() : startOperation(process))) throw new InvalidOperationException("Process did not start.");
    var held=process.Handle;
    stdout=Task.Run(()=>Drain(process.StandardOutput,true)); stderr=Task.Run(()=>Drain(process.StandardError,false));
    timer=new Timer(_=>{ timedOut=true; Stop(); },null,milliseconds,Timeout.Infinite);
   } catch {
    bool reaped=false;
    try { if(timer!=null) timer.Dispose(); } catch {}
    if(attempted) {
     try { if(!process.HasExited) process.Kill(); reaped=process.WaitForExit(5000); } catch { reaped=false; }
    }
    if(reaped) {
     try { if(stdout!=null) stdout.Wait(2000); } catch {}
     try { if(stderr!=null) stderr.Wait(2000); } catch {}
    }
    // No association after a throwing Start is UNKNOWN, never proof no child ran.
    // Only the retained object may be inspected/stopped; never look up a PID.
    if(startOperation==null) { try { process.Dispose(); } catch {} }
    throw new ConstructionFailure(reaped);
   }
  }
  void Drain(StreamReader reader,bool retain) {
   try { var buffer=new char[1]; int count,total=0;
    while((count=reader.Read(buffer,0,buffer.Length))>0) {
     if(!retain) errorOutput=true;
     total+=Encoding.UTF8.GetByteCount(buffer,0,count);
     if(total>limit) { overflow=true; Stop(); return; }
     if(retain) lock(gate) output.Append(buffer,0,count);
    }
   } catch { overflow=true; }
  }
  public void Deadline(int milliseconds) { timer.Change(milliseconds,Timeout.Infinite); }
  public void CloseInput() { try { process.StandardInput.Close(); } catch {} }
  public void Stop() { try { if(!process.HasExited) process.Kill(); } catch {} }
  public bool Wait(int milliseconds) { return process.WaitForExit(milliseconds); }
  public void Dispose() { if(disposed)return; disposed=true; timer.Dispose(); Stop();
   if(process.WaitForExit(5000)) { try { Task.WaitAll(new[]{stdout,stderr},2000); } catch {} process.Dispose(); }
  }
 }
}
'@
}
