// Node built-ins only. Exercise the actual page script with synthetic controls.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const elements = new Map();
function element(id) {
  if (!elements.has(id)) elements.set(id, {value:'',textContent:'',disabled:false,children:[],replaceChildren(){this.children=[];},append(x){this.children.push(x);},setAttribute(){}});
  return elements.get(id);
}
const state = {executableSelected:true,aiHubConfigured:true,canDiscover:true,canLogin:true,canSend:false};
let finishConfigure, finishLogin;
let failSend = false, sendCalls = 0;
const context = {
  document:{getElementById:element,querySelector:()=>({content:'public-test-token'}),querySelectorAll:()=>[],createElement:()=>element('new-button')},
  confirm:()=>true,
  fetch:async(url,options)=>{
    assert.equal(options.headers['X-Preview-Token'],'public-test-token');
    if(url==='/api/configure') await new Promise(resolve=>{finishConfigure=resolve;});
    if(url==='/api/login') await new Promise(resolve=>{finishLogin=resolve;});
    if(url==='/api/discover') return {ok:true,json:async()=>({models:[{id:'public-model',name:'Public model'}]})};
    if(url==='/api/send') { sendCalls++; return {ok:!failSend,json:async()=>({text:'Public completed response'})}; }
    return {ok:true,json:async()=>state};
  }
};
vm.runInNewContext(fs.readFileSync(path.join(__dirname,'assets/app.js'),'utf8'),context);
const tick=()=>new Promise(resolve=>setImmediate(resolve));
(async()=>{
  await tick();
  element('endpoint').value='https://public-fixture.invalid';element('key').value='PUBLIC-NOT-A-CREDENTIAL';
  element('configure').onclick();
  assert.equal(element('endpoint').disabled,true,'endpoint editable during submitted configuration');
  assert.equal(element('key').disabled,true,'key editable during submitted configuration');
  assert.equal(typeof finishConfigure,'function');
  finishConfigure();await tick();
  assert.equal(element('endpoint').disabled,false);assert.equal(element('key').value,'');
  element('models-openai').children.push('old-context-model');
  element('login').onclick();
  assert.equal(element('models-openai').children.length,0,'old-context models remain during fresh login');
  assert.equal(typeof finishLogin,'function');finishLogin();await tick();
  assert.equal(element('send').disabled,true,'unsupported send became enabled');
  state.canSend=true;
  element('discover-openai').onclick();await tick();
  assert.equal(element('models-openai').children.length,1);
  element('models-openai').children[0].onclick();
  assert.equal(element('send').disabled,false);
  element('prompt').value='Public test text';element('reply').textContent='Old completed reply';
  failSend=true;element('send').onclick();await tick();
  assert.equal(element('reply').textContent,'','failed new send displays stale reply');
  assert.equal(sendCalls,1,'failed send retried');
  failSend=false;element('send').onclick();await tick();
  assert.equal(element('reply').textContent,'Public completed response');assert.equal(sendCalls,2);
  element('models-openai').children[0].onclick();
  assert.equal(element('reply').textContent,'','new selection retains previous reply');
  console.log('PASS: configuration lock, fresh login, unsupported send, completed reply and no stale failure/retry');
})().catch(error=>{console.error(error);process.exitCode=1;});
