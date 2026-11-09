package nativepreview

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "strings"
 "time"
)

func sendText(parent context.Context,spec peerSpec,model,text string)(Reply,error){
 spec.only167=true
 var answer string
 err:=withNativePeer(parent,spec,90*time.Second,textOverrides(),true,func(peer *nativePeer)error{
  effective,err:=peer.rpc("preview-send-config","config/read",map[string]any{"includeLayers":false,"cwd":spec.home})
  if err!=nil || !matchesTextGuard(effective,spec){return ErrUnavailable}
  models,err:=readModels(peer,spec)
  if err!=nil{return ErrUnavailable}
  found:=false;for _,candidate:=range models{if candidate.ID==model{found=true}}
  if !found{return ErrUnavailable}
  stream:=&textStream{spec:spec,model:model,items:map[string]string{},finished:map[string]bool{}}
  peer.notification=stream.notification
  started,err:=peer.rpc("preview-thread","thread/start",map[string]any{
   "model":model,"modelProvider":nativeProvider(spec),"cwd":spec.home,"ephemeral":true,
   "environments":[]any{},"dynamicTools":[]any{},"selectedCapabilityRoots":[]any{},"allowProviderModelFallback":false,
  })
  if err!=nil{return ErrUnavailable}
  var response struct{
   Thread json.RawMessage `json:"thread"`;Model string `json:"model"`;Provider string `json:"modelProvider"`;Cwd string `json:"cwd"`
   InstructionSources json.RawMessage `json:"instructionSources"`
  }
  if json.Unmarshal(started,&response)!=nil || response.Model!=model || response.Provider!=nativeProvider(spec) ||
   !sameOwnedDirectory(response.Cwd,spec.home) || !emptyArray(response.InstructionSources) || stream.bindThread(response.Thread)!=nil{return ErrUnavailable}
  // The server owns all execution. Empty environments is the actual native
  // no-environment contract, not a prompt or a tool-notification race.
  stream.turnRequested=true
  turn,err:=peer.rpc("preview-turn","turn/start",map[string]any{"threadId":stream.thread,"model":model,"environments":[]any{},
   "input":[]any{map[string]any{"type":"text","text":text}}})
  if err!=nil{return ErrUnavailable}
  var result struct{Turn json.RawMessage `json:"turn"`}
  if json.Unmarshal(turn,&result)!=nil || stream.bindTurn(result.Turn)!=nil{return ErrUnavailable}
  for !stream.completed{
   envelope,err:=peer.next()
   if err!=nil || peer.acceptNotification(envelope)!=nil{return ErrUnavailable}
  }
  if len(stream.parts)==0{return ErrUnavailable}
  answer=strings.Join(stream.parts,"\n\n")
  if len(answer)>65536 || strings.TrimSpace(answer)=="" || (spec.key!="" && strings.Contains(answer,spec.key)){return ErrUnavailable}
  return nil
 })
 if err!=nil{return Reply{},ErrUnavailable}
 return Reply{Text:answer},nil
}
func nativeProvider(spec peerSpec)string{if spec.provider=="aihub"{return "magpie_preview_aihub"};return "openai"}
func emptyArray(raw json.RawMessage)bool{
 if len(raw)==0 || bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return false}
 var values []json.RawMessage
 return json.Unmarshal(raw,&values)==nil && values!=nil && len(values)==0
}
func sameOwnedDirectory(got,want string)bool{
 if !safeStateParent(got) || !safeStateParent(want){return false}
 a,err:=os.Stat(got);if err!=nil{return false};b,err:=os.Stat(want)
 return err==nil && os.SameFile(a,b)
}
func shortID(s string)bool{return s!="" && len(s)<=256 && cleanText(s)}

type textStream struct{
 spec peerSpec
 model,thread,turn string
 completed,turnRequested bool
 textBytes int
 items map[string]string
 finished map[string]bool
 parts []string
}
func(s *textStream)bindThread(raw json.RawMessage)error{
 var thread struct{
  ID string `json:"id"`;Model string `json:"model"`;Provider string `json:"modelProvider"`;Ephemeral bool `json:"ephemeral"`
  Environments json.RawMessage `json:"environments"`;Parent json.RawMessage `json:"parentThreadId"`;Fork json.RawMessage `json:"forkedFromId"`
 }
 if json.Unmarshal(raw,&thread)!=nil || !shortID(thread.ID) || !thread.Ephemeral || thread.Model!=s.model || thread.Provider!=nativeProvider(s.spec) || !emptyArray(thread.Environments){return ErrUnavailable}
 for _,value:=range []json.RawMessage{thread.Parent,thread.Fork}{if len(value)!=0 && !bytes.Equal(bytes.TrimSpace(value),[]byte("null")){return ErrUnavailable}}
 if s.thread!="" && s.thread!=thread.ID{return ErrUnavailable};s.thread=thread.ID
 return nil
}
func(s *textStream)bindTurn(raw json.RawMessage)error{
 var turn struct{ID string `json:"id"`;Status string `json:"status"`;Error json.RawMessage `json:"error"`}
 if json.Unmarshal(raw,&turn)!=nil || !shortID(turn.ID) || (turn.Status!="inProgress" && turn.Status!="completed"){return ErrUnavailable}
 if len(turn.Error)>0 && !bytes.Equal(bytes.TrimSpace(turn.Error),[]byte("null")){return ErrUnavailable}
 if s.turn!="" && s.turn!=turn.ID{return ErrUnavailable};s.turn=turn.ID
 return nil
}
func(s *textStream)notification(method string,data json.RawMessage)error{
 var fields map[string]json.RawMessage
 if json.Unmarshal(data,&fields)!=nil || fields==nil{return ErrUnavailable}
 if method=="thread/started"{return s.bindThread(fields["thread"])}
 var thread string
 if json.Unmarshal(fields["threadId"],&thread)!=nil || s.thread=="" || thread!=s.thread{return ErrUnavailable}
 switch method{
 case "thread/status/changed":return nil
 case "thread/settings/updated":
  var settings struct{Model string `json:"model"`;Provider string `json:"modelProvider"`;Collaboration json.RawMessage `json:"collaborationMode"`}
  if json.Unmarshal(fields["threadSettings"],&settings)!=nil || settings.Model!=s.model || settings.Provider!=nativeProvider(s.spec){return ErrUnavailable}
  if len(settings.Collaboration)>0 && !bytes.Equal(bytes.TrimSpace(settings.Collaboration),[]byte("null")){
   var collaboration struct{Settings struct{Model string `json:"model"`} `json:"settings"`}
   if json.Unmarshal(settings.Collaboration,&collaboration)!=nil || collaboration.Settings.Model!=s.model{return ErrUnavailable}
  }
  return nil
 case "turn/started":if !s.turnRequested || s.completed{return ErrUnavailable};return s.bindTurn(fields["turn"])
 case "turn/completed":
  if !s.turnRequested || s.completed || s.bindTurn(fields["turn"])!=nil{return ErrUnavailable}
  var turn struct{Status string `json:"status"`;Error json.RawMessage `json:"error"`}
  if json.Unmarshal(fields["turn"],&turn)!=nil || turn.Status!="completed" || (len(turn.Error)>0 && !bytes.Equal(bytes.TrimSpace(turn.Error),[]byte("null"))){return ErrUnavailable}
  for id,kind:=range s.items{if kind=="agentMessage" && !s.finished[id]{return ErrUnavailable}}
  s.completed=true;return nil
 }
 var turn string
 if json.Unmarshal(fields["turnId"],&turn)!=nil || !s.turnRequested || !shortID(turn){return ErrUnavailable}
 if s.turn==""{s.turn=turn}
 if turn!=s.turn{return ErrUnavailable}
 switch method{
 case "thread/tokenUsage/updated":return nil
 case "item/started","item/completed":
  if s.completed{return ErrUnavailable}
  var item struct{ID string `json:"id"`;Type string `json:"type"`;Text string `json:"text"`;Phase *string `json:"phase"`}
  if json.Unmarshal(fields["item"],&item)!=nil || !shortID(item.ID) || (item.Type!="userMessage" && item.Type!="agentMessage" && item.Type!="reasoning"){return ErrUnavailable}
  if method=="item/started"{
   if _,exists:=s.items[item.ID];exists{return ErrUnavailable};s.items[item.ID]=item.Type;return nil
  }
  if previous:=s.items[item.ID];(previous!="" && previous!=item.Type) || s.finished[item.ID]{return ErrUnavailable};s.items[item.ID]=item.Type;s.finished[item.ID]=true
  if item.Type=="agentMessage"{
   if len(item.Text)>65536{return ErrUnavailable}
   if item.Phase!=nil && *item.Phase!="commentary" && *item.Phase!="final_answer"{return ErrUnavailable}
   if item.Phase==nil || *item.Phase=="final_answer"{s.textBytes+=len(item.Text);if s.textBytes>65536{return ErrUnavailable};s.parts=append(s.parts,item.Text)}
  }
  return nil
 case "item/agentMessage/delta","item/reasoning/summaryTextDelta","item/reasoning/textDelta","item/reasoning/summaryPartAdded":
  if s.completed{return ErrUnavailable}
  var id string;if json.Unmarshal(fields["itemId"],&id)!=nil || s.items[id]=="" || s.finished[id]{return ErrUnavailable};return nil
 default:return ErrUnavailable
 }
}
