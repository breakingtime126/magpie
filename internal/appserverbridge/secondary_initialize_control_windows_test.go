//go:build windows

package appserverbridge

import (
 "bufio"
 "context"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
 "time"

 "github.com/yetone/magpie/internal/nativecatalog"
 "github.com/yetone/magpie/internal/nativeendpoint"
 "github.com/yetone/magpie/internal/proc"
)

// Disposable official-runtime diagnostic only. No thread, turn, login or
// inference request is sent. A completed experiment is not chat acceptance.
func TestOfficialSecondaryInitializeControl(t *testing.T) {
 if os.Getenv("MAGPIE_PREVIEW_OFFICIAL_TEXT_TEST") != "1" { t.Skip("explicit hosted official-runtime opt-in required") }
 exe := os.Getenv("MAGPIE_PREVIEW_OFFICIAL_NATIVE")
 if !filepath.IsAbs(exe) { t.Fatal("verified official executable unavailable") }
 outcomes := make([]string, 0, 2)
 for _, explicit := range []bool{false, true} {
  outcomes = append(outcomes, secondaryInitializeControl(t, exe, explicit))
 }
 fmt.Printf("native-preview-diagnostic stage=initialize-control-%s-%s class=passed\n", outcomes[0], outcomes[1])
}

func secondaryInitializeControl(t *testing.T, exe string, explicit bool) (outcome string) {
 t.Helper()
 // Each case gets fresh equivalent state; the primary is deliberately not
 // launched. This tests secondary initialization, not dual-process integration.
 root := t.TempDir()
 home, state := filepath.Join(root, "home"), filepath.Join(root, "state")
 for _, dir := range []string{home, state} {
  if os.Mkdir(dir, 0700) != nil { t.Fatal("control directory unavailable") }
 }
 const model = "original-ui-fixture/original-ui-fixture-model"
 catalog, err := nativecatalog.Render([]nativecatalog.Model{{ID:model, Name:"original-ui-fixture-model"}})
 if err != nil { t.Fatal("control catalog unavailable") }
 path := filepath.Join(root, "catalog.json")
 if os.WriteFile(path, catalog, 0600) != nil { t.Fatal("control catalog write failed") }
 ep, err := nativeendpoint.Listen()
 if err != nil { t.Fatal("control endpoint unavailable") }
 defer func() {
  ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
  defer stop()
  if ep.Shutdown(ctx) != nil { t.Error("control descendant cleanup unverified") }
 }()
 primary := Backend{ID:"openai", Provider:"openai", Home:home, CodexHome:state, Command:exe}
 secondary := Backend{ID:"aihub", Provider:sharedSecondaryProvider167, Home:home, CodexHome:state, Command:exe}
 cfg := Config{Backends:[]Backend{primary,secondary}, SharedNativeRoute:&SharedNativeRoute167{
  Mode:SharedNativeHome167, GatewayURL:"http://"+ep.Addr().String()+"/v1",
  GatewayEnvKey:"MAGPIE_INITIALIZE_CONTROL_GATEWAY", GatewayAuth:SharedNativeLocalGateway167, SecondaryCatalog:path,
 }}
 // Preserve transport/trust variables through the production filter. Only
 // homes are replaced with this experiment's own fresh directories.
 inherited := append([]string{}, os.Environ()...)
 filtered := inherited[:0]
 for _, entry := range inherited {
  key := entry
  for i, c := range entry { if c == '=' { key=entry[:i]; break } }
  if strings.EqualFold(key,"HOME") || strings.EqualFold(key,"USERPROFILE") || strings.EqualFold(key,"CODEX_HOME") { continue }
  filtered=append(filtered,entry)
 }
 filtered=append(filtered,"HOME="+home,"USERPROFILE="+home,"CODEX_HOME="+state)
 environment, err := cfg.sharedNativeEnvironment167(secondary,filtered)
 if err != nil { t.Fatal("control environment unavailable") }
 args := secondaryInitializeControlArgs(cfg, secondary, explicit, model)
 cmd := proc.Command(exe,args...)
 cmd.Env, cmd.Dir = environment, home
 // Null stderr, as in production: no raw native content or private errors.
 input, output, closeChild, err := ownedCommandPipes(cmd)
 if err != nil { t.Fatal("control pipes unavailable") }
 defer input.Close()
 defer output.Close()
 if err := ep.Start(cmd); err != nil { closeChild(); t.Fatal("control child unavailable") }
 closeChild()
 ctx, stop := context.WithTimeout(context.Background(), 35*time.Second)
 defer stop()
 written := make(chan error,1)
 go func() { _,err:=input.Write([]byte("{\"id\":1,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"magpie_initialize_control\",\"version\":\"1\"},\"capabilities\":{\"experimentalApi\":true}}}\n")); written<-err }()
 select { case err:=<-written: if err!=nil { return "write" }; case <-ctx.Done(): return "timeout" }
 result:=make(chan string,1)
 readDone:=make(chan struct{})
 defer func(){ output.Close(); <-readDone }()
 go func() {
  defer close(readDone)
  scanner:=bufio.NewScanner(output)
  scanner.Buffer(make([]byte,64<<10),MaxMessageBytes+1)
  for scanner.Scan() {
   var envelope map[string]json.RawMessage
   if json.Unmarshal(scanner.Bytes(),&envelope)!=nil { result<-"invalid";return }
   if string(envelope["id"])!="1" { continue }
   if _,ok:=envelope["result"];ok { if _,bad:=envelope["error"];bad { result<-"invalid" } else { result<-"reply" };return }
   if _,ok:=envelope["error"];ok { result<-"error";return }
   result<-"invalid";return
  }
  if scanner.Err()!=nil { result<-"invalid" } else { result<-"eof" }
 }()
 select { case value:=<-result:return value;case <-ctx.Done():return "timeout" }
}

func secondaryInitializeControlArgs(cfg Config, b Backend, explicit bool, model string) []string {
 args := cfg.nativeArgs167(LaunchOptions{}, b)
 if explicit { args=append(args,"-c","model="+strconv.Quote(model)) }
 return args
}

func TestSecondaryInitializeControlChangesOnlyModel(t *testing.T) {
 cfg:=Config{SharedNativeRoute:&SharedNativeRoute167{GatewayURL:"http://127.0.0.1:1234/v1",GatewayEnvKey:"MAGPIE_INITIALIZE_CONTROL_GATEWAY",SecondaryCatalog:"catalog.json"}}
 b:=Backend{Provider:sharedSecondaryProvider167}
 base:=secondaryInitializeControlArgs(cfg,b,false,"fixture/model")
 picked:=secondaryInitializeControlArgs(cfg,b,true,"fixture/model")
 expected:=cfg.nativeArgs167(LaunchOptions{},b)
 if len(base)!=len(expected) || len(picked)!=len(base)+2 {t.Fatal("control changed argument shape")}
 for i:=range base {if base[i]!=expected[i] || picked[i]!=base[i] {t.Fatal("control changed a production argument")}}
 if picked[len(base)]!="-c" || picked[len(base)+1]!="model="+strconv.Quote("fixture/model") {t.Fatal("control changed more than explicit model")}
}
