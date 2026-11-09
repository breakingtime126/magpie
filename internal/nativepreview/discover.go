package nativepreview

import (
 "bufio"
 "bytes"
 "context"
 "encoding/json"
 "io"
 "os"
 "path/filepath"
 "runtime"
 "strconv"
 "strings"
 "time"

 "github.com/yetone/magpie/internal/proc"
)

const maxLine = 4 << 20
const keyName = "MAGPIE_NATIVE_PREVIEW_KEY"
type peerSpec struct { command, home, codexHome, provider, endpoint, key string; digest [32]byte }
type peerEvent struct { data []byte; err error }

// discover does not start a thread or advertise experimental capabilities.
// Native discovery can refresh authentication or network caches; it is not an
// endpoint authentication test, inference test, or a zero-write operation.
func discover(parent context.Context, spec peerSpec) (models []Model, resultErr error) {
 ctx, cancel := context.WithTimeout(parent, 30*time.Second)
 defer cancel()
 // This presence changes native sandbox refresh semantics. Never clear it
 // silently to make a dedicated context appear safer.
 if _, present := os.LookupEnv("CODEX_WINDOWS_REGISTERED_CORE"); present { return nil, ErrUnavailable }
 if !safeStateParent(spec.home) || !safeStateParent(spec.codexHome) { return nil,ErrUnavailable }
 digest, err := executableDigest(spec.command)
 if err != nil || digest != spec.digest || ctx.Err() != nil { return nil, ErrUnavailable }
 if !nativeVersion(ctx, spec) { return nil, ErrUnavailable }
 args := []string{"app-server", "-c", `model_provider="openai"`}
 if spec.provider == "aihub" {
  args = []string{"app-server", "-c", `model_provider="magpie_preview_aihub"`, "-c",
   `model_providers.magpie_preview_aihub={name="AIHub preview",base_url=`+strconv.Quote(spec.endpoint)+`,env_key="`+keyName+`",wire_api="responses",requires_openai_auth=false}`}
 }
 // Parent cancellation is handled through the owned process tree, not an
 // early CommandContext parent kill. stderr is deliberately discarded.
 command := proc.CommandContext(context.WithoutCancel(ctx), spec.command, args...)
 command.Env, command.Dir = nativeEnvironment(spec), spec.home
 input, err := command.StdinPipe()
 if err != nil { return nil, ErrUnavailable }
 output, err := command.StdoutPipe()
 if err != nil { input.Close(); return nil, ErrUnavailable }
 if ctx.Err() != nil { input.Close(); output.Close(); return nil, ErrUnavailable }
 tree, err := proc.StartTreeStrict(command)
 if err != nil { input.Close(); output.Close(); return nil, ErrUnavailable }
 events := make(chan peerEvent, 1)
 readerDone := make(chan struct{})
 go func() {
  defer close(readerDone)
  scanner := bufio.NewScanner(output)
  scanner.Buffer(make([]byte,4096), maxLine+1)
  for scanner.Scan() {
   if len(scanner.Bytes()) > maxLine {
    select { case events <- peerEvent{err:ErrUnavailable}: case <-ctx.Done(): }; return
   }
   event := peerEvent{data:bytes.Clone(scanner.Bytes())}
   select { case events <- event: case <-ctx.Done(): return }
  }
  eof := io.EOF
  if scanner.Err() != nil { eof = ErrUnavailable }
  select { case events <- peerEvent{err:eof}: case <-ctx.Done(): }
 }()
 defer func() {
  cancel(); input.Close()
  waited := make(chan error,1)
  go func(){ waited <- tree.Wait() }()
  select {
  case err := <-waited:
   if err != nil { models, resultErr = nil, ErrUnavailable }
  case <-time.After(500*time.Millisecond):
   <-tree.Stop(time.Second); <-waited
   models, resultErr = nil, ErrUnavailable
  }
  output.Close(); <-readerDone
 }()
 rpc := func(id string, method string, params any) (json.RawMessage,error) {
  request, err := json.Marshal(map[string]any{"id":id,"method":method,"params":params})
  if err != nil { return nil, ErrUnavailable }
  if err := writePeer(ctx,input,request); err != nil { return nil, ErrUnavailable }
  select {
  case event := <-events:
   if event.err != nil || !strictJSON(event.data) { return nil, ErrUnavailable }
   var envelope map[string]json.RawMessage
   if json.Unmarshal(event.data,&envelope) != nil || len(envelope)!=2 { return nil, ErrUnavailable }
   var gotID string
   if json.Unmarshal(envelope["id"],&gotID)!=nil || gotID!=id || envelope["result"]==nil { return nil, ErrUnavailable }
   return envelope["result"],nil
  case <-ctx.Done(): return nil, ErrUnavailable
  }
 }
 init, err := rpc("preview-init","initialize", map[string]any{"clientInfo":map[string]string{"name":"magpie_native_preview","version":"1"},"capabilities":map[string]any{}})
 if err != nil { return nil, ErrUnavailable }
 var initialized map[string]json.RawMessage
 if json.Unmarshal(init,&initialized)!=nil { return nil, ErrUnavailable }
 var agent string
 if json.Unmarshal(initialized["userAgent"],&agent)!=nil || agent=="" { return nil, ErrUnavailable }
 // Protocol shape is checked, but userAgent is not a trustworthy binary pin.
 if writePeer(ctx,input,[]byte(`{"method":"initialized"}`))!=nil { return nil, ErrUnavailable }
 effective,err:=rpc("preview-config","config/read",map[string]any{"includeLayers":false})
 if err!=nil || !matchesProvider(effective,spec) { return nil,ErrUnavailable }
 var all []Model
 seenModels, seenCursors := map[string]bool{},map[string]bool{}
 cursor := ""
 for page:=0; page<8; page++ {
  params := map[string]any{"limit":100,"includeHidden":false}
  if cursor!="" { params["cursor"]=cursor }
  result, err := rpc("preview-models-"+strconv.Itoa(page),"model/list",params)
  if err!=nil { return nil, ErrUnavailable }
  var response struct { Data []json.RawMessage `json:"data"`; NextCursor *string `json:"nextCursor"` }
  if json.Unmarshal(result,&response)!=nil || response.Data==nil || len(response.Data)>100 { return nil, ErrUnavailable }
  for _, data:=range response.Data {
   var entry struct { ID string `json:"id"`; Model string `json:"model"`; Name string `json:"displayName"`; Hidden bool `json:"hidden"` }
   if json.Unmarshal(data,&entry)!=nil || entry.ID=="" || entry.Model=="" || entry.Name=="" || len(entry.Model)>256 || len(entry.Name)>256 || !cleanText(entry.Model) || !cleanText(entry.Name) || seenModels[entry.Model] { return nil, ErrUnavailable }
   if spec.key!="" && (strings.Contains(entry.Model,spec.key) || strings.Contains(entry.Name,spec.key)) { return nil,ErrUnavailable }
   seenModels[entry.Model]=true
   if !entry.Hidden { all=append(all,Model{ID:entry.Model,Name:entry.Name}) }
   if len(all)>200 { return nil, ErrUnavailable }
  }
  if response.NextCursor==nil {
   input.Close()
   select { case event:= <-events: if event.err!=io.EOF { return nil,ErrUnavailable }; case <-ctx.Done(): return nil,ErrUnavailable }
   if all==nil { all=[]Model{} }
   return all,nil
  }
  cursor=*response.NextCursor
  if cursor=="" || len(cursor)>4096 || seenCursors[cursor] { return nil,ErrUnavailable }
  seenCursors[cursor]=true
 }
 return nil,ErrUnavailable
}

func writePeer(ctx context.Context, output io.Writer, data []byte) error {
 done:=make(chan error,1)
 go func(){ line:=append(data,'\n');n,err:=output.Write(line);if err==nil && n!=len(line){err=io.ErrShortWrite};done<-err }()
 select { case err:= <-done:return err; case <-ctx.Done():return ErrUnavailable }
}

func nativeEnvironment(spec peerSpec) []string {
 env:=[]string{"HOME="+spec.home,"USERPROFILE="+spec.home,"CODEX_HOME="+spec.codexHome,
  "APPDATA="+filepath.Join(spec.home,"AppData","Roaming"),"LOCALAPPDATA="+filepath.Join(spec.home,"AppData","Local"),
  "XDG_CONFIG_HOME="+filepath.Join(spec.home,".config"),"XDG_CACHE_HOME="+filepath.Join(spec.home,".cache"),
  "XDG_DATA_HOME="+filepath.Join(spec.home,".local","share"),"XDG_STATE_HOME="+filepath.Join(spec.home,".local","state"),
  "TEMP="+spec.home,"TMP="+spec.home,"TMPDIR="+spec.home}
 if runtime.GOOS=="windows" {
  if root:=os.Getenv("SystemRoot");root!="" { env=append(env,"SystemRoot="+root,"PATH="+filepath.Join(root,"System32")) }
 } else { env=append(env,"PATH=/usr/bin:/bin") }
 // Preserve explicit transport routing and trust configuration, including
 // enterprise proxies/CA settings. Never rewrite these to bypass a denial.
 for _, name := range []string{"HTTP_PROXY","HTTPS_PROXY","ALL_PROXY","NO_PROXY","http_proxy","https_proxy","all_proxy","no_proxy","CODEX_CA_CERTIFICATE","SSL_CERT_FILE","SSL_CERT_DIR","CURL_CA_BUNDLE","REQUESTS_CA_BUNDLE","NODE_EXTRA_CA_CERTS"} {
  if value,present:=os.LookupEnv(name);present { env=append(env,name+"="+value) }
 }
 if spec.provider=="aihub" && spec.key!="" { env=append(env,keyName+"="+spec.key) }
 return env
}

// Reject duplicate keys at every level before interpreting IDs or metadata.
func strictJSON(data []byte) bool {
 decoder:=json.NewDecoder(bytes.NewReader(data));decoder.UseNumber()
 var value func(int) bool
 value=func(depth int) bool {
  if depth>32 { return false }
  token,err:=decoder.Token();if err!=nil { return false }
  delimiter,ok:=token.(json.Delim);if !ok { return true }
  switch delimiter {
  case '{':
   seen:=map[string]bool{}
   for decoder.More() { key,err:=decoder.Token();s,ok:=key.(string);if err!=nil || !ok || seen[s] { return false };seen[s]=true;if !value(depth+1){return false} }
   end,err:=decoder.Token();return err==nil && end==json.Delim('}')
  case '[':
   for decoder.More(){if !value(depth+1){return false}}
   end,err:=decoder.Token();return err==nil && end==json.Delim(']')
  }
  return false
 }
 if !value(0){return false};_,err:=decoder.Token();return err==io.EOF && !strings.HasPrefix(strings.TrimSpace(string(data)),"null")
}

// A supported reported version is a compatibility check, not provenance. The
// executable digest is rechecked by discovery but remains subject to path races.
func nativeVersion(parent context.Context, spec peerSpec) bool {
 ctx,cancel:=context.WithTimeout(parent,10*time.Second);defer cancel()
 if ctx.Err()!=nil{return false}
 spec.key = "" // Version reporting does not need the provider credential.
 command:=proc.CommandContext(context.WithoutCancel(ctx),spec.command,"--version")
 command.Env,command.Dir=nativeEnvironment(spec),spec.home
 output:=&boundedVersion{}
 command.Stdout=output
 tree,err:=proc.StartTreeStrict(command)
 if err!=nil{return false}
 done:=make(chan error,1);go func(){done<-tree.Wait()}()
 select {
 case err:= <-done:
  version:=strings.TrimSpace(output.String())
  return err==nil && !output.exceeded && (version=="codex-cli 0.167.0-alpha.3.2" || version=="codex-cli 0.166.0-alpha.6")
 case <-ctx.Done():
  <-tree.Stop(time.Second);<-done;return false
 }
}

type boundedVersion struct { bytes.Buffer; exceeded bool }
func (b *boundedVersion) Write(data []byte)(int,error){
 n:=len(data)
 if b.Len()+n>256 {b.exceeded=true;return n,nil}
 _,_=b.Buffer.Write(data);return n,nil
}

func matchesProvider(data json.RawMessage,spec peerSpec) bool {
 var response struct{Config map[string]json.RawMessage `json:"config"`}
 if json.Unmarshal(data,&response)!=nil || response.Config==nil{return false}
 var provider string
 if json.Unmarshal(response.Config["model_provider"],&provider)!=nil{return false}
 if spec.provider=="openai"{return provider=="openai"}
 if provider!="magpie_preview_aihub"{return false}
 var providers map[string]map[string]json.RawMessage
 if json.Unmarshal(response.Config["model_providers"],&providers)!=nil{return false}
 selected:=providers["magpie_preview_aihub"]
 if selected==nil{return false}
 // ConfigRead serializes native ModelProviderInfo defaults too. Admit only
 // the fixed fields below plus inert known null/false defaults, never assume
 // an unknown provider field is harmless or drop a managed auth override.
 inertNull:=map[string]bool{"model_catalog_url":true,"env_key_instructions":true,"experimental_bearer_token":true,"auth":true,"gateway_oauth":true,"aws":true,"project_id":true,"location":true,"credential_file":true,"capabilities":true,"query_params":true,"http_headers":true,"env_http_headers":true,"request_max_retries":true,"stream_max_retries":true,"stream_idle_timeout_ms":true,"websocket_connect_timeout_ms":true}
 for key,value:=range selected {
  switch key {
  case "name","base_url","env_key","wire_api","requires_openai_auth":
  case "supports_websockets","supports_standalone_web_search":
   if !bytes.Equal(bytes.TrimSpace(value),[]byte("false")){return false}
  default:
   if !inertNull[key] || !bytes.Equal(bytes.TrimSpace(value),[]byte("null")){return false}
  }
 }
 for key,expected:=range map[string]string{"name":"AIHub preview","base_url":spec.endpoint,"env_key":keyName,"wire_api":"responses"}{
  var got string;if json.Unmarshal(selected[key],&got)!=nil || got!=expected{return false}
 }
 return bytes.Equal(bytes.TrimSpace(selected["requires_openai_auth"]),[]byte("false"))
}
