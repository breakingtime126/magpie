import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { createHash } from 'node:crypto';
import { Parser } from './vendor/acorn-8.15.0/acorn.mjs';
import { AST_BLOCK_CODES, AST_SKIP_CODES, AST_LIMITS, PLUGIN_SHAPES, PublicPluginASTLimit, analyzePublicPluginAST } from './inspect-desktop-public-plugin-ast.mjs';

const BT = String.fromCharCode(96);
const template = contents => BT + contents + BT;
const limits = code => error => error instanceof PublicPluginASTLimit && error.code === code;

test('vendored parser and license match pinned official bytes offline', () => {
  for (const [file, git, sha256] of [
    ['acorn.mjs', '74d5fc431be2b5062245e72937ef2032164b51cd', 'b4c8c70200e72bae33cf1085e0ecb1e792c1b6924ed50cab817caf14f51bb249'],
    ['LICENSE', '9d71cc63a35e7d878787e5159b5b163739486220', '76a876cf886ff9be2a8b5e2e86514fed06223c8c9f0c1e9ee9606e93841e00b7'],
  ]) {
    const bytes = fs.readFileSync(new URL('./vendor/acorn-8.15.0/' + file, import.meta.url));
    assert.equal(createHash('sha1').update('blob ' + bytes.length + '\0').update(bytes).digest('hex'), git);
    assert.equal(createHash('sha256').update(bytes).digest('hex'), sha256);
  }
});

test('modern grammar is parsed fully without treating regex or comments as strings', () => {
  const sources = [
    'class C { #id="public@market"; static { this.field=1; } #get(){return this.#id} }',
    'const a=/[[a]--[b]]/v;const p="public@market";',
    'const a=(()=>({field:1}))()/2;const p="public@market";',
    '<!-- "comment@market"\nconst p="public@market";',
    '\n--> "comment@market"\nconst p="public@market";',
    'outer:for(;;){break outer\u2028/"regex@market"/.test("");}const p="public@market";',
    'outer:for(;;){continue outer\u2029/"regex@market"/.test("");}const p="public@market";',
    'export const p="public@market";',
  ];
  for (const source of sources) {
    const report = analyzePublicPluginAST(source);
    assert.equal(report.complete, true);
    assert.deepEqual(report.candidates.map(item => item.key), [
      'plugins.public@market.enabled', 'plugins.public@market.auto_provision',
    ]);
  }
});

test('escaped strings, template tags, computed identities and unrelated values remain excluded', () => {
  const source = 'const a="escaped\\u0040market"; const b="private"+"@market";' +
    'const c=tag' + template('tagged@market') + ';const d=' + template('dynamic@$' + '{market}') + ';' +
    'const v="SYNTHETIC_PRIVATE_VALUE";const w="plugins.x@market.enabled=SYNTHETIC_PRIVATE_VALUE";';
  const report = analyzePublicPluginAST(source);
  assert.equal(report.complete, true);
  assert.deepEqual(report.candidates, []);
  assert.deepEqual(report.constructions, []);
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
});

test('all eight native boolean construction shapes contain fixed metadata only', () => {
  const suffixes = [
    '.enabled', '.auto_provision', '.mcp_servers.$' + '{server}.enabled',
    '.apps.$' + '{app}.enabled', '.apps.$' + '{app}.destructive_enabled',
    '.apps.$' + '{app}.open_world_enabled', '.apps.$' + '{app}.default_tools_enabled',
    '.apps.$' + '{app}.tools.$' + '{tool}.enabled',
  ];
  const expressions = suffixes.map(suffix => template('plugins.$' + '{SYNTHETIC_PRIVATE_ID}' + suffix + '=true'));
  const report = analyzePublicPluginAST('class C{getConfigOverrides(){return [' + expressions.join(',') + ']}}');
  assert.equal(report.complete, true);
  assert.deepEqual(report.candidates, []);
  assert.deepEqual(report.methods, [
    { name: 'getConfigOverrides', definitions: 1, unresolved: 0 },
    { name: 'startupCommand', definitions: 0, unresolved: 0 },
  ]);
  assert.deepEqual(report.constructions.map(item => item.shape).sort(), [...PLUGIN_SHAPES].sort());
  assert.ok(report.constructions.every(item => item.method === 'getConfigOverrides' &&
    item.form === 'template' && item.value === 'bool_true' && item.count === 1));
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
  for (const value of ['server', 'tool']) assert.ok(!Object.hasOwn(report, value));
});

test('construction forms, values, duplicate counts and unresolved counts are separate', () => {
  const report = analyzePublicPluginAST('function startupCommand(){return [' +
    '"plugins.public@market.enabled=true","plugins.public@market.enabled=true",' +
    '"plugins."+id+".auto_provision="+false,' +
    template('plugins.$' + '{id}.apps.$' + '{app}.enabled=$' + '{flag}') + ',' +
    '"plugins.public@market.apps.app.enabled=PRIVATE_RHS",' +
    template('plugins.$' + '{id}.$' + '{unknown}=true') + ',' +
    '"plugins.public@market.mcp_servers.server.tools.tool.enabled=true",' +
    '"plugins.public@market.apps.app.enabled.extra=true"' +
    ']};const getConfigOverrides=()=>0;');
  assert.equal(report.complete, true);
  assert.equal(report.methods[1].definitions, 1);
  assert.equal(report.methods[1].unresolved, 3);
  assert.deepEqual(report.constructions, [
    { method: 'startupCommand', form: 'literal', shape: 'direct_enabled', value: 'bool_true', count: 2 },
    { method: 'startupCommand', form: 'concat', shape: 'direct_auto_provision', value: 'bool_false', count: 1 },
    { method: 'startupCommand', form: 'template', shape: 'app_enabled', value: 'dynamic', count: 1 },
    { method: 'startupCommand', form: 'literal', shape: 'app_enabled', value: 'other', count: 1 },
  ]);
  assert.ok(!JSON.stringify(report).includes('PRIVATE_RHS'));
});

test('method calls and arbitrary functions are not named method definitions', () => {
  const report = analyzePublicPluginAST('getConfigOverrides("plugins.public@market.apps.app.enabled=true");' +
    'function unrelated(){return "plugins.public@market.apps.app.enabled=true"}');
  assert.equal(report.complete, true);
  assert.ok(report.methods.every(item => item.definitions === 0 && item.unresolved === 0));
  assert.deepEqual(report.constructions, []);
});

test('late syntax failure discards candidates, definitions and construction counts from that file', () => {
  const report = analyzePublicPluginAST('const id="public@market";class C{getConfigOverrides(){return ' +
    template('plugins.$' + '{id}.enabled=true') + '}} ???');
  assert.deepEqual(report, {
    complete: false, candidates: [],
    methods: [{ name: 'getConfigOverrides', definitions: 0, unresolved: 0 },
      { name: 'startupCommand', definitions: 0, unresolved: 0 }],
    constructions: [],
  });
});

test('source, candidate and recursive grammar limits fail closed', () => {
  assert.throws(() => analyzePublicPluginAST(' '.repeat(AST_LIMITS.bytes + 1)), limits('public_ast_bytes_limit'));
  assert.throws(() => analyzePublicPluginAST(Array.from({ length: 65 },
    (_, i) => 'const p' + i + '="p' + i + '@market";').join('')),
    error => error instanceof PublicPluginASTLimit && error.code === 'public_catalog_limit');
  for (const source of [
    'const x=' + '('.repeat(600) + '1' + ')'.repeat(600) + ';',
    'const x=' + '!'.repeat(600) + 'true;',
    'const x=' + 'x?1:'.repeat(600) + '0;',
    'let x; x=' + 'x='.repeat(600) + '0;',
  ]) assert.throws(() => analyzePublicPluginAST(source), limits('public_ast_parse_depth_limit'));
});

test('allocation and token budgets are enforced during parsing', () => {
  assert.throws(() => analyzePublicPluginAST('const a=[' + '0,'.repeat(AST_LIMITS.nodes + 1) + '];'), limits('public_ast_node_limit'));
  // Elisions allocate no literal nodes, so this reaches the separate token guard.
  assert.throws(() => analyzePublicPluginAST('const a=[' + ','.repeat(AST_LIMITS.tokens + 1) + '];'), limits('public_ast_token_limit'));
});

test('parser checks the elapsed deadline inside its work', t => {
  let tick = 0;
  t.mock.method(Date, 'now', () => tick++ * AST_LIMITS.milliseconds);
  assert.throws(() => analyzePublicPluginAST('const id="public@market";'), limits('public_ast_deadline'));
});

test('prototype-like unknown fields and literal NULs are never shape labels', () => {
  const source = 'function getConfigOverrides(){return [' +
    '"plugins.public@market.apps.app.toString=true",' +
    '"plugins.public@market.apps.app.__proto__=true",' +
    template('plugins.\0.enabled=true') + ']}';
  const report = analyzePublicPluginAST(source);
  assert.equal(report.complete, true);
  assert.deepEqual(report.constructions, []);
  assert.deepEqual(report.candidates, []);
  assert.equal(report.methods[0].unresolved, 2);
});

test('valid long chains distinguish guarded parse depth from iterative AST depth', () => {
  const cases = [
    ['const x=' + Array(600).fill('1').join('+') + ';', 'public_ast_parse_depth_limit'],
    ['const x=root' + '.x'.repeat(600) + ';', 'public_ast_walk_depth_limit'],
  ];
  for (const [source, reason] of cases) {
    // The unmodified pinned parser accepts both; local fixed bounds still stop them.
    assert.equal(Parser.parse(source, { ecmaVersion: 2025, sourceType: 'script' }).type, 'Program');
    assert.throws(() => analyzePublicPluginAST(source), limits(reason));
  }
});

test('explicit allocation errors are not collapsed or retried as syntax failures', t => {
  let calls = 0;
  t.mock.method(Parser.prototype, 'parse', function () {
    calls++;
    throw new PublicPluginASTLimit('public_ast_node_limit');
  });
  assert.throws(() => analyzePublicPluginAST('const x=1;'), limits('public_ast_node_limit'));
  assert.equal(calls, 1);
});

test('native RangeError stays separate from measured parse depth', t => {
  let calls = 0;
  t.mock.method(Parser.prototype, 'parse', function () {
    calls++;
    throw new RangeError('SYNTHETIC_PRIVATE_STACK');
  });
  assert.throws(() => analyzePublicPluginAST('const x=1;'), limits('public_ast_range_error'));
  assert.equal(calls, 1);
});

test('unexpected parser defects are not partial syntax or a measured bound', t => {
  const failure = new Error('SYNTHETIC_PRIVATE_FAILURE');
  let calls = 0;
  t.mock.method(Parser.prototype, 'parse', function () { calls++; throw failure; });
  assert.throws(() => analyzePublicPluginAST('const x=1;'), error => error === failure);
  assert.equal(calls, 1);
  assert.throws(() => analyzePublicPluginAST(null),
    error => !(error instanceof PublicPluginASTLimit));
});

test('iterative traversal has its own exact visit-count bound', t => {
  // Controlled AST injection isolates the walk budget from parser allocation.
  const empty = { type: 'EmptyStatement', start: 0, end: 0 };
  t.mock.method(Parser.prototype, 'parse', function () {
    return { type: 'Program', start: 0, end: 0, body: Array(AST_LIMITS.nodes).fill(empty) };
  });
  assert.throws(() => analyzePublicPluginAST(''), limits('public_ast_walk_node_limit'));
});

test('AST blocked reasons form a closed fixed set distinct from package limits', () => {
  assert.deepEqual(AST_BLOCK_CODES, [
    'public_ast_bytes_limit', 'public_ast_token_limit', 'public_ast_node_limit',
    'public_ast_parse_depth_limit', 'public_ast_walk_depth_limit',
    'public_ast_walk_node_limit', 'public_ast_deadline', 'public_ast_range_error',
  ]);
  assert.ok(!AST_BLOCK_CODES.includes('public_source_limit'));
});

test('only measured file-local guards are eligible to omit a file', () => {
  assert.deepEqual(AST_SKIP_CODES, [
    'public_ast_token_limit', 'public_ast_node_limit', 'public_ast_parse_depth_limit',
    'public_ast_walk_depth_limit', 'public_ast_walk_node_limit', 'public_ast_deadline',
  ]);
  assert.ok(!AST_SKIP_CODES.includes('public_ast_bytes_limit'));
  assert.ok(!AST_SKIP_CODES.includes('public_ast_range_error'));
});
