'use strict';
const $ = id => document.getElementById(id);
const token = document.querySelector('meta[name="preview-token"]').content;
let state = {}, selection = null, busy = false, aiHubDirty = false;
const knownBlockers = {discover_models_first:'先发现并选择模型。每次测试是新的独立短对话。',native_version_does_not_support_guarded_text:'当前官方后台版本不支持本预览的安全短文本路径，不能发送；不会绕过版本限制。',guarded_short_text_preview:'可请求一次短文本测试；发送前还会核实原生有效配置，冲突时停止。',text_requires_proven_tool_boundary:'短文本测试等待本地工具禁用边界确认，不会模拟成功。',windows_job_login_unavailable:'当前进程环境不支持安全的浏览器登录交接，不会绕过限制。',text_requires_proven_tool_boundary_login_requires_native_handoff:'短文本测试等待本地工具禁用边界确认，不会模拟成功。',tools_not_disabled:'本版本尚不能证明禁用所有本地工具，短文本测试未开放。',unsupported:'这项能力尚未实现，不会模拟成功。'};
async function api(path, body = {}) {
  const response = await fetch('/api/'+path,{method:'POST',headers:{'Content-Type':'application/json','X-Preview-Token':token},body:JSON.stringify(body),cache:'no-store'});
  if (!response.ok) throw new Error('操作未完成。本预览没有自动重试；原生副作用不会自动撤销。');
  return response.json();
}
function render() {
  $('choose').disabled=busy;
  $('auto').disabled=busy;
  $('endpoint').disabled=busy;
  $('key').disabled=busy;
  $('configure').disabled=busy;
  $('login').disabled=busy || !state.canLogin;
  $('discover-openai').disabled=busy || !state.canDiscover || !state.executableSelected;
  $('discover-aihub').disabled=busy || !state.canDiscover || !state.aiHubConfigured || aiHubDirty;
  $('send').disabled=busy || !state.canSend || !selection || (selection.provider==='aihub' && aiHubDirty);
  $('cancel').disabled=!busy;
  $('openai-status').textContent=state.executableSelected?(state.canLogin?'可以主动进行一次原生登录；账号和模型仍需分别验证。':'程序已选择；当前环境未开放原生登录。'):'尚未选择程序。';
  $('aihub-status').textContent=aiHubDirty?'输入已改动，请先使用此服务。':state.aiHubConfigured?'本次进程已配置；连接尚未验证。':'尚未配置。';
  if (!state.canSend && !selection) $('selection').textContent=knownBlockers[state.blocker] || '短文本测试尚未开放。模型发现不等于聊天可用。';
}
async function refresh(){state=await api('state');render();}
async function action(fn){if(busy)return;busy=true;render();$('operation').textContent='正在执行一次操作…';try{await fn();}catch(e){$('operation').textContent=e.message;}finally{busy=false;try{await refresh();}catch{$('operation').textContent='状态读取失败，不能确认操作结果。';render();}}}
$('auto').onclick=()=>action(async()=>{await api('auto');$('executable').textContent='已选择当前用户安装包中的程序；不代表服务目的地已验证。';selection=null;for(const p of ['openai','aihub'])$('models-'+p).replaceChildren();$('operation').textContent='自动查找完成；登录与模型请求仍需你主动操作。';});
$('choose').onclick=()=>action(async()=>{const r=await api('choose');if(!r.cancelled){$('executable').textContent='已选择：'+r.name;selection=null;for(const p of ['openai','aihub'])$('models-'+p).replaceChildren();}$('operation').textContent=r.cancelled?'已取消选择。':'程序已选择，不代表来源或版本已验证。';});
$('configure').onclick=()=>{const endpoint=$('endpoint').value.trim(),key=$('key').value;if(!endpoint||!key){$('operation').textContent='请填写可信任的服务地址和密钥。';return;}if(!confirm('服务地址：'+endpoint+'\n将这个密钥用于此地址，仅本次进程使用，不默认保存。请确认它属于你信任的服务。'))return;action(async()=>{try{await api('configure',{endpoint,key,confirm:true});aiHubDirty=false;selection=null;$('models-aihub').replaceChildren();$('operation').textContent='AIHub 已配置，但尚未验证连接。';}finally{$('key').value='';}});};
for(const id of ['endpoint','key'])$(id).oninput=()=>{aiHubDirty=true;if(selection?.provider==='aihub')selection=null;$('models-aihub').replaceChildren();render();};
$('login').onclick=()=>{if(!confirm('启动你选择的 Codex 原生登录，由你在浏览器中完成。每次使用新专用状态，不导入旧账号。原生程序遵循实际 managed 凭据存储策略，可能联网、保存/清理/撤销认证；不同目录不是系统隔离，不能保证系统密钥环毫无影响。取消不会回滚这些变化，也不自动重试。继续一次？'))return;action(async()=>{if(selection?.provider==='openai')selection=null;$('models-openai').replaceChildren();$('openai-status').textContent='本次登录使用新专用上下文；旧模型列表已清除。';await api('login',{confirm:true});$('operation').textContent='原生登录进程已正常退出。未确认账号、额度或聊天能力；可继续发现模型。';});};
for(const provider of ['openai','aihub'])$('discover-'+provider).onclick=()=>{if(!confirm('从所选供应商发现模型。会启动所选后台，可能读取或刷新专用凭据、联网及更新原生状态；这不是聊天测试。继续一次？'))return;action(async()=>{const r=await api('discover',{provider,confirm:true});const target=$('models-'+provider);target.replaceChildren();selection=null;for(const model of r.models||[]){const b=document.createElement('button');b.textContent=(model.name||model.id)+' · 发现，未验证';b.onclick=()=>{$('reply').textContent='';selection={provider,model:model.id};document.querySelectorAll('.models button').forEach(x=>x.setAttribute('aria-pressed','false'));b.setAttribute('aria-pressed','true');$('selection').textContent='选择：'+(provider==='openai'?'官方 Codex':'AIHub')+' / '+(model.name||model.id)+'，尚未验证可用。';render();};target.append(b);}$('operation').textContent='模型发现已完成。没有发送聊天请求。';});};
$('send').onclick=()=>{if(!selection||!state.canSend||(selection.provider==='aihub'&&aiHubDirty))return;const text=$('prompt').value.trim();if(!text){$('operation').textContent='请输入一条短测试文本。';return;}if(!confirm('向 '+selection.provider+' / '+selection.model+' 发送这条文本一次，可能产生服务费用。每次是新的临时对话，不继承上次内容；会先核实原生有效配置与空环境。本地敏感工具动作不开放，策略冲突时停止。失败或取消仍可能已发送请求并产生费用，本预览不自动重试，原生传输或模型循环仍可能重试。继续？'))return;action(async()=>{$('reply').textContent='';const reply=await api('send',{...selection,text,confirm:true});$('reply').textContent=reply.text;$('operation').textContent='本次实际回复已返回。不是 Desktop 接入或完整对话验收。';});};
$('cancel').onclick=async()=>{try{await api('cancel');$('operation').textContent='已请求取消，等待当前操作结束；原生副作用不会回滚。';}catch{$('operation').textContent='取消结果不确定。没有自动重试。';}};
refresh().catch(()=>{$('operation').textContent='预览控制器不可用；没有启动后台。';});
