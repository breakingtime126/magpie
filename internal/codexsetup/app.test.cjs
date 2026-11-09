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
const context = {
  document:{getElementById:element,querySelector:()=>({content:'public-test-token'}),querySelectorAll:()=>[],createElement:()=>element('new-button')},
  confirm:()=>true,
  fetch:async(url,options)=>{
    assert.equal(options.headers['X-Preview-Token'],'public-test-token');
    if(url==='/api/configure') await new Promise(resolve=>{finishConfigure=resolve;});
    if(url==='/api/login') await new Promise(resolve=>{finishLogin=resolve;});
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
  console.log('PASS: pending configuration locks inputs; fresh login clears old models; unsupported send stays disabled');
})().catch(error=>{console.error(error);process.exitCode=1;});
