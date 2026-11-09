package nativepreview

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "time"

 "github.com/yetone/magpie/internal/proc"
)

// This identity and relative backend path were observed by the existing signed
// official package harness. No ASAR inspection, package edit or broad search.
const installedQuery = `$ErrorActionPreference='Stop'; $OutputEncoding=[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); $p=@(Get-AppxPackage -Name 'OpenAI.Codex' -ErrorAction Stop | Where-Object { $_.PackageFamilyName -ceq 'OpenAI.Codex_2p2nqsd0c76g0' -and [string]$_.SignatureKind -ceq 'Store' -and [string]$_.Status -ceq 'Ok' }); if($p.Count -ne 1){exit 2}; $f=Join-Path $p[0].InstallLocation 'app\resources\codex.exe'; if(-not (Test-Path -LiteralPath $f -PathType Leaf)){exit 3}; @{path=$f;family=[string]$p[0].PackageFamilyName;signatureKind=[string]$p[0].SignatureKind}|ConvertTo-Json -Compress`

func installedExecutable(parent context.Context)(string,error){
 ctx,cancel:=context.WithTimeout(parent,10*time.Second);defer cancel()
 root:=os.Getenv("SystemRoot")
 if !filepath.IsAbs(root){return "",ErrUnavailable}
 powershell:=filepath.Join(root,"System32","WindowsPowerShell","v1.0","powershell.exe")
 command:=proc.CommandContext(context.WithoutCancel(ctx),powershell,"-NoProfile","-NonInteractive","-Command",installedQuery)
 output:=&queryOutput{}
 command.Stdout=output
 // Keep the host environment for the read-only Windows package query. No
 // AIHub key has ever been put in it; no network/security setting is changed.
 if ctx.Err()!=nil{return "",ErrUnavailable}
 tree,err:=proc.StartTreeStrict(command)
 if err!=nil{return "",ErrUnavailable}
 done:=make(chan error,1);go func(){done<-tree.Wait()}()
 select{
 case err:= <-done:
  if err!=nil || output.exceeded{return "",ErrUnavailable}
 case <-ctx.Done():<-tree.Stop(time.Second);<-done;return "",ErrUnavailable
 }
 if !strictJSON(output.data){return "",ErrUnavailable}
 var found struct{Path string `json:"path"`;Family string `json:"family"`;SignatureKind string `json:"signatureKind"`}
 if json.Unmarshal(output.data,&found)!=nil || !filepath.IsAbs(found.Path) || found.Family!="OpenAI.Codex_2p2nqsd0c76g0" || found.SignatureKind!="Store"{return "",ErrUnavailable}
 return found.Path,nil
}

type queryOutput struct{data []byte;exceeded bool}
func(q *queryOutput)Write(data []byte)(int,error){n:=len(data);if len(q.data)+n>16384{q.exceeded=true;return n,nil};q.data=append(q.data,data...);return n,nil}
