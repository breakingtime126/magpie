package nativepreview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/nativeprepdiag"
	"github.com/yetone/magpie/internal/proc"
)

// This identity and relative backend path were observed by the existing signed
// official package harness. No ASAR inspection, package edit or broad search.
const installedQuery = `$ErrorActionPreference='Stop'
$OutputEncoding=[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
function Import-QueryModule([string]$moduleName) {
    if ($moduleName -cnotin @('Appx','Microsoft.PowerShell.Management','Microsoft.PowerShell.Utility')) { throw 'native query module unavailable' }
    $moduleRoot=[IO.Path]::Combine($PSHOME,'Modules',$moduleName)
    $manifest=[IO.Path]::Combine($moduleRoot,$moduleName+'.psd1')
    $loaded=@(Microsoft.PowerShell.Core\Import-Module -Name $manifest -PassThru -ErrorAction Stop)
    if ($loaded.Count -ne 1 -or $loaded[0] -isnot [System.Management.Automation.PSModuleInfo] -or $loaded[0].Name -cne $moduleName) { throw 'native query module unavailable' }
    $sameBase=[string]::Equals([IO.Path]::GetFullPath($loaded[0].ModuleBase),[IO.Path]::GetFullPath($moduleRoot),[StringComparison]::OrdinalIgnoreCase)
    if ($moduleName -cne 'Appx') { $sameBase=$sameBase -or [string]::Equals([IO.Path]::GetFullPath($loaded[0].ModuleBase),[IO.Path]::GetFullPath($PSHOME),[StringComparison]::OrdinalIgnoreCase) }
    if (-not $sameBase) { throw 'native query module unavailable' }
    if ($moduleName -ceq 'Appx') {
        $appxCommand=Get-QueryCommand $loaded[0] 'Get-AppxPackage'
        $assembly=$appxCommand.ImplementingType.Assembly
        if ($null -eq $assembly -or $assembly.GetName().Name -cne 'Microsoft.Windows.Appx.PackageManager.Commands' -or
            [string]::IsNullOrEmpty($loaded[0].Path) -or [string]::IsNullOrEmpty($assembly.Location) -or
            -not [IO.Path]::IsPathRooted($loaded[0].Path) -or -not [IO.Path]::IsPathRooted($assembly.Location) -or
            -not [string]::Equals([IO.Path]::GetPathRoot($loaded[0].Path),[IO.Path]::GetPathRoot([IO.Path]::GetFullPath($loaded[0].Path)),[StringComparison]::OrdinalIgnoreCase) -or
            -not [string]::Equals([IO.Path]::GetPathRoot($assembly.Location),[IO.Path]::GetPathRoot([IO.Path]::GetFullPath($assembly.Location)),[StringComparison]::OrdinalIgnoreCase) -or
            -not [string]::Equals([IO.Path]::GetFullPath($loaded[0].Path),[IO.Path]::GetFullPath($assembly.Location),[StringComparison]::OrdinalIgnoreCase)) { throw 'native query module unavailable' }
    } elseif (-not [string]::Equals([IO.Path]::GetFullPath($loaded[0].Path),[IO.Path]::GetFullPath($manifest),[StringComparison]::OrdinalIgnoreCase)) { throw 'native query module unavailable' }
    return $loaded[0]
}
function Get-QueryCommand($moduleInfo,[string]$commandName) {
    $commandInfo=$moduleInfo.ExportedCommands[$commandName]
    if ($commandInfo -isnot [System.Management.Automation.CmdletInfo] -or $commandInfo.Name -cne $commandName -or
        $commandInfo.CommandType -ne [System.Management.Automation.CommandTypes]::Cmdlet) { throw 'native query command unavailable' }
    return $commandInfo
}
$appxModule=Import-QueryModule 'Appx'
$managementModule=Import-QueryModule 'Microsoft.PowerShell.Management'
$utilityModule=Import-QueryModule 'Microsoft.PowerShell.Utility'
$getPackage=Get-QueryCommand $appxModule 'Get-AppxPackage'
$joinPath=Get-QueryCommand $managementModule 'Join-Path'
$testPath=Get-QueryCommand $managementModule 'Test-Path'
$toJSON=Get-QueryCommand $utilityModule 'ConvertTo-Json'
$p=@(& $getPackage -Name 'OpenAI.Codex' -ErrorAction Stop | Microsoft.PowerShell.Core\Where-Object { $_.PackageFamilyName -ceq 'OpenAI.Codex_2p2nqsd0c76g0' -and [string]$_.SignatureKind -ceq 'Store' -and [string]$_.Status -ceq 'Ok' })
if($p.Count -ne 1){exit 2}
$f=& $joinPath $p[0].InstallLocation 'app\resources\codex.exe'
if(-not (& $testPath -LiteralPath $f -PathType Leaf)){exit 3}
@{path=$f;family=[string]$p[0].PackageFamilyName;signatureKind=[string]$p[0].SignatureKind}|& $toJSON -Compress`

func installedExecutable(parent context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	trace := nativeprepdiag.Track(ctx, nativeprepdiag.QueryEnvironment)
	defer trace.End()
	root := os.Getenv("SystemRoot")
	if !filepath.IsAbs(root) {
		return "", ErrUnavailable
	}
	powershell := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := proc.CommandContext(context.WithoutCancel(ctx), powershell, "-NoProfile", "-NonInteractive", "-Command", installedQuery)
	trace.Complete()
	return runInstalledQuery(ctx, command, func(cmd *exec.Cmd) (queryProcess, error) {
		return proc.StartTreeStrict(cmd)
	})
}
