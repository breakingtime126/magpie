import test from 'node:test';
import assert from 'node:assert/strict';
import { analyzeDesktopReadCallsites as scan, READ_CALL_LIMITS as limits } from './inspect-desktop-read-callers.mjs';

function good(source) {
  const result = scan(source);
  assert.equal(result.status, 'source_syntax_only');
  assert.equal(result.sourceParsedCompletely, true);
  assert.equal(result.runtimeBindingProven, false);
  assert.equal(result.responseValueProven, false);
  assert.equal(result.consumerCoverage, 'direct_await_and_sibling_const_only');
  return result;
}
function fields(call) { return call.responseFields.map(row => row.relation + ':' + row.path.join('.')); }
function absent(result, text) { assert.equal(JSON.stringify(result).includes(text), false); }

test('direct await and same-block const identify public config reads', () => {
  const result = good('async function f(){const r=await x.send("config/read",{});return r.config.model_provider;}');
  assert.deepEqual(fields(result.calls[0]), ['const_binding:config', 'const_binding:config.model_provider']);
  assert.equal(result.calls[0].relation, 'literal_first_argument_only');
  assert.equal(result.calls[0].offsetUtf16, 'async function f(){const r=await x.send('.length);
  const direct = good('async function f(){return(await x.send("config/read",{})).config.model_providers;}');
  assert.deepEqual(fields(direct.calls[0]), ['await_member:config', 'await_member:config.model_providers']);
});

test('same-block const destructuring retains only direct declared paths', () => {
  const result = good('async function f(){const {config:r}=await x("config/read",{});return r.model;}');
  assert.deepEqual(fields(result.calls[0]), ['const_binding:config', 'const_binding:config.model']);
  const nested = good('async function f(){const {config:{features:r}}=await x("config/read",{});return r;}');
  assert.deepEqual(fields(nested.calls[0]), ['const_binding:config.features']);
});

test('nested scopes, sibling receivers and mutable bindings are not associated', () => {
  const result = good('async function f(){const r=await x("config/read",{});function other(r){return r.config.model;}const cb=r=>r.config.features;{const r=other;use(r.config.layers)}if(ok){use(r.config.origins)}return unrelated.config.model_provider;}');
  assert.deepEqual(fields(result.calls[0]), []);
  const mutable = good('async function f(){let r=await x("config/read",{});return r.config.model;}');
  assert.deepEqual(fields(mutable.calls[0]), []);
  const promise = good('function f(){const r=x("config/read",{});return r.config.model;}');
  assert.deepEqual(fields(promise.calls[0]), []);
});

test('writes and delete do not masquerade as reading the assigned field', () => {
  const result = good('async function f(){const r=await x("config/read",{});r.config.model="CANARY";delete r.config.features;return r.config.model_provider;}');
  assert.deepEqual(fields(result.calls[0]), ['const_binding:config', 'const_binding:config.model_provider']);
  absent(result, 'CANARY');
});

test('unknown fields, local names and values never enter reports', () => {
  const result = good('async function SECRET_FUNCTION(){const SECRET_LOCAL=await SECRET_CLIENT.send("config/read",{credential:"SECRET_VALUE"});use(SECRET_LOCAL.config.SECRET_PROPERTY);return SECRET_LOCAL.config.model;}');
  assert.deepEqual(fields(result.calls[0]), ['const_binding:config', 'const_binding:config.model']);
  for (const canary of ['SECRET_FUNCTION', 'SECRET_LOCAL', 'SECRET_CLIENT', 'SECRET_VALUE', 'SECRET_PROPERTY', 'credential']) absent(result, canary);
});

test('fs evidence describes expression syntax and markers, never file authority', () => {
  const result = good('function f(){return x("fs/readFile",{path:join(SECRET_HOME,".codex","config.toml")});}');
  assert.equal(result.calls[0].pathSyntax, 'call');
  assert.deepEqual(result.calls[0].pathMarkers, ['config_toml', 'dot_codex']);
  assert.equal(result.calls[0].pathAuthorityProven, false);
  absent(result, 'SECRET_HOME');
  for (const [source, kind] of [
    ['"C:/SECRET_USER/auth.json"', 'literal_absolute'],
    ['"/SECRET_USER/AGENTS.md"', 'literal_absolute'],
    ['"relative/SKILL.md"', 'literal_relative'],
    ['some.path', 'member'], ['unknownPath', 'identifier'],
    ['ok?"config.toml":unknownPath', 'conditional'],
  ]) {
    const call = good('function f(){return x("fs/readFile",{path:' + source + '});}').calls[0];
    assert.equal(call.pathSyntax, kind);
    assert.equal(call.pathAuthorityProven, false);
    absent(call, 'SECRET_USER');
  }
});

test('spread, duplicate path, computed key and accessor remain unresolved', () => {
  for (const params of ['{...other,path:"config.toml"}', '{path:"config.toml",path:other}',
    '{["path"]:"config.toml"}', '{get path(){return "config.toml"}}', 'params']) {
    const call = good('function f(){return x("fs/readFile",' + params + ');}').calls[0];
    assert.equal(call.pathSyntax, 'unresolved');
    assert.deepEqual(call.pathMarkers, []);
  }
});

test('nested callbacks cannot supply fs markers', () => {
  const call = good('function f(){return x("fs/readFile",{path:helper(()=>".codex/config.toml")});}').calls[0];
  assert.equal(call.pathSyntax, 'call');
  assert.deepEqual(call.pathMarkers, []);
});

test('comments, regex, plain literals and wrong method positions are not calls', () => {
  const result = good('const r=/config\\/read/; /* x("fs/readFile",{}) */ const s="project/list"; x("other","config/read");');
  assert.equal(result.calls.length, 0);
  const templates = good('const t=`x("config/read",{})`;');
  assert.deepEqual(templates.calls, []);
});

test('all four exact methods are recognized, with no transport assertion', () => {
  const result = good('x("config/read",{});x("experimentalFeature/list",{});x("project/list",{});x("fs/readFile",{path:p});x("Config/read",{});');
  assert.deepEqual(result.calls.map(x => x.method), ['config/read', 'experimentalFeature/list', 'project/list', 'fs/readFile']);
  assert.equal(result.runtimeBindingProven, false);
});

test('public fs response spelling is recognized but content is never emitted', () => {
  const call = good('async function f(){return(await x("fs/readFile",{path:p})).dataBase64;}').calls[0];
  assert.deepEqual(fields(call), ['await_member:dataBase64']);
  assert.equal(call.pathSyntax, 'identifier');
});

test('a complete source above the prior whole-AST node cap remains bounded', () => {
  const source = '0;'.repeat(510000) + 'async function f(){return(await x("config/read",{})).config.model_provider;}';
  const result = good(source);
  assert.deepEqual(fields(result.calls[0]), ['await_member:config', 'await_member:config.model_provider']);
});

test('source, call and nesting caps discard all partial evidence', () => {
  for (const [source, reason] of [
    ['x("config/read",{});'.repeat(limits.calls + 1), 'call_limit'],
    ['x'.repeat(limits.bytes + 1), 'input_limit'],
    ['('.repeat(limits.depth + 1) + '0' + ')'.repeat(limits.depth + 1), 'depth_limit'],
  ]) assert.deepEqual(scan(source), { schema: 1, status: 'unavailable', reason });
});

test('local consumer traversal is bounded and never leaves a partial row', () => {
  const source = 'async function f(){const r=await x("config/read",{}); const a=[' + '0,'.repeat(21000) + '0];return r.config.model;}';
  assert.deepEqual(scan(source), { schema: 1, status: 'unavailable', reason: 'local_node_limit' });
});

test('whole-source syntax error discards an earlier valid caller', () => {
  assert.deepEqual(scan('function f(){return x("config/read",{})} const = 1;'),
    { schema: 1, status: 'unavailable', reason: 'syntax_error' });
});

test('lexical errors never retain earlier positive syntax facts or raw exceptions', () => {
  const result = scan('x("config/read",{}); "UNTERMINATED_SECRET');
  assert.deepEqual(result, { schema: 1, status: 'unavailable', reason: 'syntax_error' });
});

test('source is parsed without executing statements or dynamic code', () => {
  delete globalThis.__desktopReadCanary;
  const result = good('globalThis.__desktopReadCanary=true; eval("SECRET_EVAL");function f(){return x("fs/readFile",{path:"config.toml"});}');
  assert.equal(globalThis.__desktopReadCanary, undefined);
  absent(result, 'SECRET_EVAL');
});

test('unrecognized consumers are not guessed from wrapper names or aliases', () => {
  const result = good('async function read(){return await x("config/read",{});}async function use(){const r=await read();return r.config.model_provider;}');
  assert.deepEqual(result.calls[0].responseFields, []);
  const alias = good('async function f(){const r=await x("config/read",{});const a=r.config;return a.model_provider;}');
  assert.deepEqual(fields(alias.calls[0]), ['const_binding:config']);
});

test('complete parser handles regex and keyword-property contexts without fragment fallbacks', () => {
  const source = 'if(1){({function:0,class:0})} /{x("config\\/read",{})}/; async function f(){return(await x("config/read",{})).config.model;}';
  const result = good(source);
  assert.equal(result.calls.length, 1);
  assert.deepEqual(fields(result.calls[0]), ['await_member:config', 'await_member:config.model']);
});

test('a chain wrapper does not double count its actual call node', () => {
  const result = good('async function f(){return(await x?.send("config/read",{})).config.model;}');
  assert.equal(result.calls.length, 1);
  assert.deepEqual(fields(result.calls[0]), ['await_member:config', 'await_member:config.model']);
});

test('destructuring assignment destinations are writes, including defaults and rest', () => {
  for (const target of ['({value:r.config}={});', '[r.config]=[];',
    '({value:r.config={}}={});', '[...r.config]=[];', '({...r.config}={});']) {
    const result = good('async function f(){const r=await x("config/read",{});' + target + '}');
    assert.deepEqual(fields(result.calls[0]), []);
  }
  const deeper = good('async function f(){const r=await x("config/read",{});({value:r.config.model}={});}');
  assert.deepEqual(fields(deeper.calls[0]), ['const_binding:config']);
  const keyRead = good('async function f(){const r=await x("config/read",{});({[r.config.model]:target}={});}');
  assert.deepEqual(fields(keyRead.calls[0]), ['const_binding:config', 'const_binding:config.model']);
});

test('member syntax after mutation never claims native response-value provenance', () => {
  const result = good('async function f(){const r=await x("config/read",{});r.config=replacement;return r.config.model;}');
  assert.equal(result.responseValueProven, false);
  assert.deepEqual(fields(result.calls[0]), ['const_binding:config', 'const_binding:config.model']);
});

test('a destructuring default RHS is read, unlike its assignment destination', () => {
  const result = good('async function f(){const r=await x("config/read",{});({value:target=r.config.model}={});}');
  assert.deepEqual(fields(result.calls[0]), ['const_binding:config', 'const_binding:config.model']);
});
test('deadline failure retains no earlier source facts', () => {
  const now = Date.now;
  let checks = 0, result;
  try {
    Date.now = () => ++checks === 1 ? 0 : limits.milliseconds + 1;
    result = scan('x("config/read",{});');
  } finally { Date.now = now; }
  assert.deepEqual(result, { schema: 1, status: 'unavailable', reason: 'deadline' });
});

test('optional-chain delete targets are writes through transparent chain wrappers', () => {
  const result = good('async function f(){const r=await x("config/read",{});delete r?.config;}');
  assert.deepEqual(fields(result.calls[0]), []);
  const deeper = good('async function f(){const r=await x("config/read",{});delete r.config?.model;}');
  assert.deepEqual(fields(deeper.calls[0]), ['const_binding:config']);
  const awaited = good('async function f(){delete (await x("config/read",{}))?.config;}');
  assert.deepEqual(fields(awaited.calls[0]), []);
});

test('direct awaited for-of and for-in destinations are writes, preserving object-prefix reads', () => {
  for (const operator of ['of', 'in']) {
    const result = good('async function f(){for((await x("config/read",{})).config ' + operator + ' arr){}}');
    assert.deepEqual(fields(result.calls[0]), []);
    const deeper = good('async function f(){for((await x("config/read",{})).config.model ' + operator + ' arr){}}');
    assert.deepEqual(fields(deeper.calls[0]), ['await_member:config']);
  }
});
