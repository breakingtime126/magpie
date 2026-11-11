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
const installedQuery = `$ErrorActionPreference='Stop'; $OutputEncoding=[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); $p=@(Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction Stop | Where-Object { $_.PackageFamilyName -ceq 'OpenAI.Codex_2p2nqsd0c76g0' -and [string]$_.SignatureKind -ceq 'Store' -and [string]$_.Status -ceq 'Ok' }); if($p.Count -ne 1){exit 2}; $f=Join-Path $p[0].InstallLocation 'app\resources\codex.exe'; if(-not (Test-Path -LiteralPath $f -PathType Leaf)){exit 3}; @{path=$f;family=[string]$p[0].PackageFamilyName;signatureKind=[string]$p[0].SignatureKind}|ConvertTo-Json -Compress`

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
