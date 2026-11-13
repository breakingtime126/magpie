package main
import (
 "bytes"
 "os"
 "path/filepath"
 "strings"
 "testing"
)
func TestReadOnlyProbe(t *testing.T) {
 root,err:=filepath.EvalSymlinks(t.TempDir())
 if err!=nil {t.Fatal(err)}
 dir:=filepath.Join(root,"Program Files","WindowsApps","OpenAI.Codex_26.1106.6628.0_x64__2p2nqsd0c76g0","app","resources")
 if err:=os.MkdirAll(dir,0700);err!=nil {t.Fatal(err)}
 p:=filepath.Join(dir,"codex.exe")
 payload:=[]byte("inert fixture, never executed")
 if err:=os.WriteFile(p,payload,0600);err!=nil {t.Fatal(err)}
 var out bytes.Buffer
 if code:=run([]string{p},&out);code!=0 {t.Fatal(code,out.String())}
 if strings.Contains(out.String(),root) {t.Fatal("path leaked")}
 data,err:=os.ReadFile(p)
 if err!=nil || !bytes.Equal(data,payload) {t.Fatal("fixture changed")}
 r:=inspect(filepath.Join(root,"absent","codex.exe"))
 if r.Stage!="runtime-path-resolution" || r.ErrorKind!="not-exist" {t.Fatal(r)}
 d:=filepath.Join(root,"codex.exe")
 if err:=os.Mkdir(d,0700);err!=nil {t.Fatal(err)}
 r=inspect(d)
 if r.Stage!="runtime-file-info" || r.Regular || r.Outcome!="failed" {t.Fatal(r)}
 if inspect("codex.exe").Stage!="input" {t.Fatal("relative input accepted")}
 if run(nil,&out)!=2 {t.Fatal("missing arg accepted")}
}
