# Read-only, single-pass freshness observation. Dot-sourcing without input only defines helpers.
[CmdletBinding()]
param([string]$InputPath = '')
function Get-EntryKnownImages([string]$PackageRoot, [string]$WindowsRoot, $Applications) {
    $manifestPaths=[ordered]@{
        App='app/ChatGPT.exe'
        CodexChromeNativeHost='app/resources/plugins/openai-bundled/plugins/chrome/extension-host/windows/x64/extension-host.exe'
        CodexCoreCommandRunner='app/resources/codex-command-runner.exe'
    }
    if (@($Applications).Count -ne 3) { throw 'Unexpected manifest application count.' }
    $seen=@{}
    foreach ($appEntry in $Applications) {
        if ($manifestPaths.Keys -cnotcontains $appEntry.id -or $seen.ContainsKey($appEntry.id) -or
            $appEntry.executable.Replace('\','/') -cne $manifestPaths[$appEntry.id]) { throw 'Unexpected manifest application.' }
        $seen[$appEntry.id]=$true
    }
    if (-not [IO.Path]::IsPathRooted($PackageRoot) -or -not [IO.Path]::IsPathRooted($WindowsRoot)) {
        throw 'Invalid verified image roots.'
    }
    $images=[ordered]@{
        package_desktop=Join-Path $PackageRoot $manifestPaths.App
        package_chrome_host=Join-Path $PackageRoot $manifestPaths.CodexChromeNativeHost
        package_command_runner=Join-Path $PackageRoot $manifestPaths.CodexCoreCommandRunner
        package_native_backend=Join-Path $PackageRoot 'app/resources/codex.exe'
        package_codex_executable=Join-Path $PackageRoot 'app/Codex.exe'
        windows_runtime_broker=Join-Path $WindowsRoot 'System32/RuntimeBroker.exe'
        windows_background_task_host=Join-Path $WindowsRoot 'System32/backgroundTaskHost.exe'
        windows_application_frame_host=Join-Path $WindowsRoot 'System32/ApplicationFrameHost.exe'
        windows_service_host=Join-Path $WindowsRoot 'System32/svchost.exe'
        windows_com_surrogate=Join-Path $WindowsRoot 'System32/dllhost.exe'
        windows_explorer=Join-Path $WindowsRoot 'explorer.exe'
    }
    return $images
}
function Get-EntryImageLabel([string]$Path, $Images) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return 'unavailable' }
    foreach ($label in $Images.Keys) {
        # Exact known path, never basename, prefix or arbitrary relative-path disclosure.
        if ($Path -ieq $Images[$label].Replace('/','\')) { return [string]$label }
    }
    return 'other'
}
function Get-EntryParentRelation([long]$ChildCreated, [long]$ParentCreated, [bool]$ChildAlive,
    [bool]$ParentAlive, [int]$ParentId, [int]$HarnessId, [long]$HarnessCreated) {
    if (-not $ChildAlive -or -not $ParentAlive -or $ParentCreated -le 0 -or
        $ChildCreated -le 0 -or $ParentCreated -ge $ChildCreated) { return 'unverified' }
    if ($ParentId -eq $HarnessId -and $ParentCreated -eq $HarnessCreated) { return 'harness' }
    return 'held-parent-predates-child'
}
function Initialize-EntryHeldObservation {
    if ('DesktopEntryReadOnly.Held' -as [type]) { return }
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Text;
namespace DesktopEntryReadOnly {
 public sealed class Held : IDisposable {
  IntPtr handle;
  [DllImport("kernel32.dll",SetLastError=true)] static extern IntPtr OpenProcess(uint access,bool inherit,uint id);
  [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr value);
  [DllImport("kernel32.dll")] static extern uint WaitForSingleObject(IntPtr value,uint milliseconds);
  [DllImport("kernel32.dll",SetLastError=true)] static extern bool GetProcessTimes(IntPtr value,out long created,out long exited,out long kernel,out long user);
  [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)]
  static extern bool QueryFullProcessImageName(IntPtr value,uint flags,StringBuilder path,ref uint size);
  [DllImport("kernel32.dll",CharSet=CharSet.Unicode)]
  static extern int GetPackageFullName(IntPtr value,ref uint size,StringBuilder name);
  public Held(int id) {
   if(id<=0)throw new InvalidOperationException();
   // Query-limited + synchronize only: no terminate, write, VM-read or injection rights.
   handle=OpenProcess(0x00101000,false,(uint)id);
   if(handle==IntPtr.Zero)throw new InvalidOperationException();
  }
  public bool Alive {get {return handle!=IntPtr.Zero && WaitForSingleObject(handle,0)==258;}}
  public long CreatedTicks {get {
   long created,exited,kernel,user;
   if(!GetProcessTimes(handle,out created,out exited,out kernel,out user))throw new InvalidOperationException();
   return DateTime.FromFileTimeUtc(created).Ticks;
  }}
  public string ImagePath {get {
   uint size=32768;var path=new StringBuilder((int)size);
   if(!QueryFullProcessImageName(handle,0,path,ref size) || size==0)throw new InvalidOperationException();
   return path.ToString();
  }}
  public string PackageRelation(string expected) {
   uint size=0;int result=GetPackageFullName(handle,ref size,null);
   if(result==15700)return "none";
   if(result!=122 || size==0 || size>32768)return "unavailable";
   var name=new StringBuilder((int)size);
   if(GetPackageFullName(handle,ref size,name)!=0)return "unavailable";
   return String.Equals(name.ToString(),expected,StringComparison.Ordinal)?"expected":"other";
  }
  public void Dispose() {if(handle!=IntPtr.Zero){CloseHandle(handle);handle=IntPtr.Zero;}}
 }
}
'@
}
function Read-EntryMatchedProcess([int]$ProcessId, [string]$Package, $Images, [int]$HarnessId, [long]$HarnessCreated) {
    $record=[ordered]@{status='unavailable';image='unavailable';package='unavailable';liveThroughout=$false;
        parentImage='unavailable';parentPackage='unavailable';parentRelation='unverified'}
    $child=$null; $parent=$null
    try {
        $child=[DesktopEntryReadOnly.Held]::new($ProcessId)
        if (-not $child.Alive) { throw 'Process observation unavailable.' }
        $record.image=Get-EntryImageLabel $child.ImagePath $Images
        $record.package=$child.PackageRelation($Package)
        # Read parent ID while the child handle is pinned and confirmed alive before/after.
        $rows=@(Get-CimInstance Win32_Process -Filter ("ProcessId="+$ProcessId) -OperationTimeoutSec 3 -ErrorAction Stop)
        if ($rows.Count -ne 1 -or -not $child.Alive) { throw 'Process observation unavailable.' }
        try {
            $parent=[DesktopEntryReadOnly.Held]::new([int]$rows[0].ParentProcessId)
            $relation=Get-EntryParentRelation $child.CreatedTicks $parent.CreatedTicks $child.Alive $parent.Alive `
                ([int]$rows[0].ParentProcessId) $HarnessId $HarnessCreated
            if ($relation -cne 'unverified') {
                $record.parentImage=Get-EntryImageLabel $parent.ImagePath $Images
                $record.parentPackage=$parent.PackageRelation($Package)
                # Recheck liveness after all metadata reads; no cached PID-only identity.
                if ($child.Alive -and $parent.Alive) { $record.parentRelation=$relation }
            }
        } catch { }
        $record.package=$child.PackageRelation($Package)
        $record.liveThroughout=$child.Alive
        if (-not $record.liveThroughout) { $record.parentRelation='unverified' }
        if ($record.liveThroughout -and $record.package -ceq 'expected') { $record.status='observed' }
    } catch { }
    finally {
        try { if ($null -ne $parent) { $parent.Dispose() } }
        finally { if ($null -ne $child) { $child.Dispose() } }
    }
    if ($record.status -cne 'observed' -or $record.parentRelation -ceq 'unverified') {
        $record.parentRelation='unverified'; $record.parentImage='unavailable'; $record.parentPackage='unavailable'
    }
    return $record
}
function Test-EntryIntegralRange($Value, [long]$Minimum, [long]$Maximum) {
    return (($Value -is [int] -or $Value -is [long]) -and $Value -ge $Minimum -and $Value -le $Maximum)
}
function Assert-EntryObservation($Value, [int]$ExpectedMatches) {
    function Shape($Item,[string[]]$Names) {
        if ($null -eq $Item -or $Item -isnot [pscustomobject] -or @($Item.PSObject.Properties).Count -ne $Names.Count) { throw 'Invalid observation shape.' }
        foreach ($property in $Item.PSObject.Properties) { if ($Names -cnotcontains $property.Name) { throw 'Invalid observation field.' } }
    }
    Shape $Value @('schema','scope','matches','truncated','originalGenerationVerified','records')
    if (-not (Test-EntryIntegralRange $Value.schema 1 1) -or $Value.scope -isnot [string] -or $Value.scope -cne 'freshness-observation-only' -or
        -not (Test-EntryIntegralRange $Value.matches 0 65536) -or $Value.matches -ne $ExpectedMatches -or
        $Value.originalGenerationVerified -isnot [bool] -or $Value.originalGenerationVerified -or
        $Value.truncated -isnot [bool] -or $Value.truncated -ne ($ExpectedMatches -gt 4) -or
        $Value.records -isnot [array] -or $Value.records.Count -ne [Math]::Min(4,$ExpectedMatches)) { throw 'Invalid observation counts.' }
    $labels=@('unavailable','other','package_desktop','package_chrome_host','package_command_runner','package_native_backend',
        'package_codex_executable','windows_runtime_broker','windows_background_task_host','windows_application_frame_host',
        'windows_service_host','windows_com_surrogate','windows_explorer')
    foreach ($record in $Value.records) {
        Shape $record @('status','image','package','liveThroughout','parentImage','parentPackage','parentRelation')
        foreach ($field in @('status','image','package','parentImage','parentPackage','parentRelation')) {
            if ($record.$field -isnot [string]) { throw 'Invalid observation label type.' }
        }
        if (($record.parentRelation -ceq 'unverified' -and
            ($record.parentImage -cne 'unavailable' -or $record.parentPackage -cne 'unavailable')) -or
            ($record.parentRelation -cne 'unverified' -and
            ($record.status -cne 'observed' -or -not $record.liveThroughout -or $record.parentImage -ceq 'unavailable'))) {
            throw 'Unverified parent metadata cannot imply a verified relationship.'
        }
        if (@('observed','unavailable') -cnotcontains $record.status -or $labels -cnotcontains $record.image -or
            $labels -cnotcontains $record.parentImage -or @('expected','other','none','unavailable') -cnotcontains $record.package -or
            @('expected','other','none','unavailable') -cnotcontains $record.parentPackage -or
            @('unverified','harness','held-parent-predates-child') -cnotcontains $record.parentRelation -or
            $record.liveThroughout -isnot [bool] -or ($record.status -ceq 'observed' -and (-not $record.liveThroughout -or $record.package -cne 'expected' -or $record.image -ceq 'unavailable'))) {
            throw 'Invalid fixed observation labels.'
        }
    }
}
function Invoke-EntryFreshnessObservation($ProcessIds, [int]$Matches, [string]$Package, [string]$PackageRoot,
    $Applications, [string]$Work) {
    $inputFile=Join-Path $Work 'freshness-observation-input.json'
    $inputValue=[ordered]@{schema=1;processIds=@($ProcessIds | ForEach-Object { [int]$_ });matches=$Matches;package=$Package;
        packageRoot=$PackageRoot;windowsRoot=$env:SystemRoot;applications=@($Applications | ForEach-Object {
            [ordered]@{id=$_.id;executable=$_.executable}
        });harnessId=$PID;harnessCreated=(Get-Process -Id $PID).StartTime.ToUniversalTime().Ticks}
    [IO.File]::WriteAllText($inputFile,($inputValue | ConvertTo-Json -Depth 6 -Compress),(New-Object Text.UTF8Encoding($false)))
    $script=Join-Path $PSScriptRoot 'desktop-entry-observe.ps1'
    if ($inputFile.Contains('"')) { throw 'Invalid owned observation input.' }
    $start=New-EntryActivationStart $script 'OpenAI.Codex_test!Test'
    $start.Arguments='-NoProfile -NonInteractive -File "'+$script+'" -InputPath "'+$inputFile+'"'
    $text=Invoke-EntryBoundedHelper $start 30000 4096
    $value=$text | ConvertFrom-Json
    Assert-EntryObservation $value $Matches
    if (($value | ConvertTo-Json -Depth 6 -Compress) -cne $text) { throw 'Noncanonical observation output.' }
    return $value
}
if ([string]::IsNullOrEmpty($InputPath)) { return }
$ErrorActionPreference='Stop'
try {
    $file=Get-Item -LiteralPath $InputPath
    if ($file.PSIsContainer -or ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $file.Length -lt 1 -or $file.Length -gt 16384) { throw 'input' }
    $stream=[IO.File]::OpenRead($InputPath)
    try {
        $bytes=New-Object byte[] 16385; $count=0
        while ($count -lt $bytes.Length) {
            $read=$stream.Read($bytes,$count,$bytes.Length-$count)
            if ($read -eq 0) { break }; $count+=$read
        }
        if ($count -lt 1 -or $count -gt 16384) { throw 'input' }
        $inputValue=[Text.Encoding]::UTF8.GetString($bytes,0,$count) | ConvertFrom-Json
    } finally { $stream.Dispose() }
    if (-not (Test-EntryIntegralRange $inputValue.schema 1 1) -or $inputValue.package -cne 'OpenAI.Codex_26.1103.3517.0_x64__2p2nqsd0c76g0' -or
        $inputValue.windowsRoot -ine $env:SystemRoot -or -not (Test-EntryIntegralRange $inputValue.matches 0 65536) -or
        $inputValue.processIds -isnot [array] -or
        $inputValue.processIds.Count -ne [Math]::Min(4,$inputValue.matches) -or
        -not (Test-EntryIntegralRange $inputValue.harnessId 1 2147483647) -or
        -not (Test-EntryIntegralRange $inputValue.harnessCreated 1 ([DateTime]::MaxValue.Ticks))) { throw 'input' }
    foreach ($processId in $inputValue.processIds) { if (-not (Test-EntryIntegralRange $processId 1 2147483647)) { throw 'input' } }
    $images=Get-EntryKnownImages $inputValue.packageRoot $inputValue.windowsRoot $inputValue.applications
    Initialize-EntryHeldObservation
    $records=@(foreach ($processId in $inputValue.processIds) {
        Read-EntryMatchedProcess $processId $inputValue.package $images $inputValue.harnessId $inputValue.harnessCreated
    })
    $result=[ordered]@{schema=1;scope='freshness-observation-only';matches=$inputValue.matches;
        truncated=($inputValue.matches -gt 4);originalGenerationVerified=$false;records=$records}
    $result | ConvertTo-Json -Depth 6 -Compress
    exit 0
} catch { Write-Output 'freshness-observation-unavailable'; exit 1 }
