package nativepreview

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestWindowsStateRejectsUNCRelativeAndReparseParents(t *testing.T){
 for _,path:=range []string{`\\server.invalid\share\preview`,`\\?\UNC\server.invalid\share`,`relative`,`C:relative`}{
  if safeStateParent(path){t.Fatal("unsafe state parent accepted")}
 }
 root:=t.TempDir()
 if !safeStateParent(root) || !safeStateParent(strings.ToUpper(root)){t.Fatal("ordinary local path or case variant rejected")}
 target:=filepath.Join(root,"target")
 if os.Mkdir(target,0700)!=nil{t.Fatal("create fixture")}
 link:=filepath.Join(root,"link")
 if err:=os.Symlink(target,link);err!=nil{t.Skip("symlink fixture unavailable; no privilege change attempted")}
 if safeStateParent(link){t.Fatal("reparse parent admitted")}
 child:=filepath.Join(link,"child")
 if os.Mkdir(child,0700)!=nil{t.Fatal("fixture child")}
 if safeStateParent(child){t.Fatal("reparse ancestor admitted")}
}
