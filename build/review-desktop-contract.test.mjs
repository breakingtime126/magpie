import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { ANCHORS, MENU_ANCHORS, trimMenuExcerpts, sourceExcerpts, inspectContractArchive, validateSchemas } from './review-desktop-contract.mjs';

// Synthetic safety fixtures, not captured Codex output or proof of its behavior.
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
function scratch(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-contract-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true })); return dir;
}
function pack(entries) {
  const header = { files: { '.vite': { files: { build: { files: {} } } } } }; const body = []; let offset = 0;
  for (const [name, text, extra] of entries) {
    const bytes = Buffer.from(text);
    const parts = name.includes('/') ? name.split('/') : ['.vite', 'build', name];
    let directory = header;
    for (const part of parts.slice(0, -1)) directory = (directory.files[part] ??= { files: {} });
    directory.files[parts.at(-1)] = { offset: String(offset), size: bytes.length, ...extra };
    body.push(bytes); offset += bytes.length;
  }
  const json = Buffer.from(JSON.stringify(header)); const padding = Buffer.alloc((4 - json.length % 4) % 4);
  const headerSize = 8 + json.length + padding.length; const prefix = Buffer.alloc(16);
  prefix.writeUInt32LE(4, 0); prefix.writeUInt32LE(headerSize, 4);
  prefix.writeUInt32LE(headerSize - 4, 8); prefix.writeInt32LE(json.length, 12);
  return Buffer.concat([prefix, json, padding, ...body]);
}
test('bounded excerpts preserve literal source and positions without executing it', () => {
  const source = 'throw new Error("NEVER_EXECUTED");hostConfig.codex_cli_command;CODEX_CLI_PATH;';
  const result = sourceExcerpts(source, '.vite/build/main.js', hash(source), Object.fromEntries(ANCHORS.map(a => [a, 8])));
  assert.equal(result.matches.length, 2);
  for (const match of result.matches) for (const e of match.excerpts) {
    assert.equal(source.slice(e.start, e.end), e.text); assert.equal(source.slice(e.offset, e.offset + match.anchor.length), match.anchor);
  }
});
test('exact archive hash, count limits, and honest missing/truncated anchors', t => {
  const dir = scratch(t); const file = path.join(dir, 'app.asar');
  const bytes = pack([['main.js', ('CODEX_CLI_PATH;').repeat(20)], ['bootstrap.js', 'codex_cli_command;']]); fs.writeFileSync(file, bytes);
  const result = inspectContractArchive(file, hash(bytes));
  const entry = result.anchors.find(a => a.anchor === 'CODEX_CLI_PATH');
  assert.deepEqual(entry, { anchor: 'CODEX_CLI_PATH', matches: 20, excerpts: 8, truncated: true });
  assert.equal(result.anchors.find(a => a.anchor === 'getAuthStatus').matches, 0);
  assert.equal(result.runtimeSelectionProven, false);
  assert.equal(result.requiredAnchorsPresent, false);
  assert.throws(() => inspectContractArchive(file, '0'.repeat(64)));
});
test('ASAR links, unpacked entries, invalid UTF8, and missing sources stop', t => {
  const dir = scratch(t); const file = path.join(dir, 'app.asar');
  for (const bytes of [pack([['main.js', 'CODEX_CLI_PATH', { link: 'other.js' }]]),
    pack([['main.js', 'CODEX_CLI_PATH', { unpacked: true }]]), pack([['main.js', Buffer.from([255])]]), pack([])]) {
    fs.writeFileSync(file, bytes); assert.throws(() => inspectContractArchive(file, hash(bytes)));
  }
});
test('all required anchor coverage is explicit without a runtime-selection claim', t => {
  const file = path.join(scratch(t), 'app.asar'); const bytes = pack([['main.js', ANCHORS.join(';')]]); fs.writeFileSync(file, bytes);
  const result = inspectContractArchive(file, hash(bytes)); assert.equal(result.requiredAnchorsPresent, true);
  assert.equal(result.runtimeSelectionProven, false);
});
function schemas(t, documents) {
  const root = scratch(t);
  for (const [name, value] of Object.entries(documents)) { const file = path.join(root, name); fs.mkdirSync(path.dirname(file), { recursive: true }); fs.writeFileSync(file, JSON.stringify(value)); }
  return root;
}
test('full schema manifest retains definitions and resolves local cross-file and escaped refs', t => {
  const root = schemas(t, { 'bundle.json': { definitions: { 'a/b': { type: 'string' }, X: { $ref: '#/definitions/a~1b' } }, $ref: 'v2/Request.json' },
    'v2/Request.json': { type: 'object', properties: { experimentalApi: { type: 'boolean' } }, $ref: '../bundle.json#/definitions/X' } });
  const result = validateSchemas(root); assert.equal(result.files.length, 2); assert.equal(result.referencesResolved, true);
  for (const entry of result.files) assert.equal(entry.sha256, hash(fs.readFileSync(path.join(root, entry.path))));
});
for (const [name, ref] of Object.entries({ remote: 'https://example.com/schema.json', parent: '../other.json',
  missingFile: 'other.json', missingDefinition: '#/definitions/missing', escapedTraversal: '%2e%2e/other.json',
  invalidEscape: '#/definitions/a~2b', backslash: 'v2\\Request.json' })) {
  test('reject schema reference '+name, t => assert.throws(() => validateSchemas(schemas(t, { 'one.json': { $ref: ref } })))) ;
}
test('reject malformed, non-object, empty and excessive-depth schema output', t => {
  for (const value of [null, [], 'PRIVATE_CANARY']) assert.throws(() => validateSchemas(schemas(t, { 'one.json': value })));
  assert.throws(() => validateSchemas(schemas(t, {})));
  const root = schemas(t, { 'one.json': {} }); fs.writeFileSync(path.join(root, 'one.json'), '{'); assert.throws(() => validateSchemas(root));
  let deep = {}; for (let i = 0; i < 130; i++) deep = { child: deep }; assert.throws(() => validateSchemas(schemas(t, { 'one.json': deep })));
});
test('reject unexpected files and symlinked generated output', t => {
  const root = schemas(t, { 'one.json': {} }); fs.writeFileSync(path.join(root, 'private.txt'), 'PRIVATE_CANARY'); assert.throws(() => validateSchemas(root));
  fs.unlinkSync(path.join(root, 'private.txt'));
  fs.symlinkSync(root, path.join(root, 'linked'), 'junction'); assert.throws(() => validateSchemas(root));
});
test('reject oversized generated file', t => {
  const root = schemas(t, { 'one.json': {} }); const fd = fs.openSync(path.join(root, 'one.json'), 'w');
  try { fs.ftruncateSync(fd, 8 * 1024 ** 2 + 1); } finally { fs.closeSync(fd); }
  assert.throws(() => validateSchemas(root));
});


test('renderer mode derives nonlegacy JS paths and exposes honest excluded coverage', t => {
  const file = path.join(scratch(t), 'app.asar');
  const source = 'throw new Error("NEVER_EVALUATE");' + MENU_ANCHORS.join(';') + ';function filterModels(){return hasConfiguredModelCatalog ? allModels : currentModel;}';
  const bytes = pack([['ui/assets/menu.js', source], ['deps/node_modules/private/index.js', 'PRIVATE_DEPENDENCY_CANARY'],
    ['ui/assets/locale.json', '{"PRIVATE_NON_JS_CANARY":1}'], ['main.js', 'modelProvider;']]);
  fs.writeFileSync(file, bytes);
  const result = inspectContractArchive(file, hash(bytes), true);
  assert.equal(result.scope, 'public-static-renderer-menu-excerpts');
  assert.equal(result.requiredAnchorsPresent, true);
  assert.equal(result.scannedFiles, 2);
  assert.equal(result.coverage.outsideLegacyDirectory, 1);
  assert.equal(result.coverage.dependencyFilesExcluded, 1);
  assert.equal(result.coverage.nonJavaScriptFilesExcluded, 1);
  assert.equal(result.coverage.allPackageFilesCovered, false);
  assert.equal(result.coverage.negativeSearchProvesAbsence, false);
  assert.ok(result.sources[0].matches.some(m => m.excerpts.some(e => e.text.includes('function filterModels()'))));
  assert.ok(!JSON.stringify(result).includes('PRIVATE_'));
  const legacy = inspectContractArchive(file, hash(bytes));
  assert.equal(legacy.scannedFiles, 1);
  assert.ok(!legacy.sources.some(e => e.path === 'ui/assets/menu.js'));
  for (const entry of result.sources) for (const match of entry.matches) for (const e of match.excerpts) {
    const original = entry.path === 'ui/assets/menu.js' ? source : 'modelProvider;';
    assert.equal(original.slice(e.start, e.end), e.text);
    assert.equal(original.slice(e.offset, e.offset + match.anchor.length), match.anchor);
  }
});
test('renderer quotas prioritize newly covered files and disclose missing/truncated anchors', t => {
  const file = path.join(scratch(t), 'app.asar');
  const bytes = pack([['main.js', 'modelProvider;'.repeat(20)], ['ui/menu.js', 'modelProvider;'.repeat(12)]]);
  fs.writeFileSync(file, bytes);
  const result = inspectContractArchive(file, hash(bytes), true);
  assert.equal(result.sources[0].path, 'ui/menu.js');
  assert.equal(result.requiredAnchorsPresent, false);
  assert.deepEqual(result.anchors.find(a => a.anchor === 'modelProvider'), { anchor: 'modelProvider', matches: 32, excerpts: 2, truncated: true });
  assert.ok(result.sources.some(s => s.matches.some(m => m.candidateWindowsDeduplicated > 0)));
  assert.ok(Buffer.byteLength(JSON.stringify(result)) < 1024 ** 2);
});
test('renderer archive pin, links, unpacked files, invalid UTF8/path and file cap fail closed', t => {
  const file = path.join(scratch(t), 'app.asar');
  for (const bytes of [pack([['ui/menu.js', 'model_catalog_json', { link: 'elsewhere.js' }]]),
    pack([['ui/menu.js', 'model_catalog_json', { unpacked: true }]]),
    pack([['ui/linked', '', { link: 'elsewhere' }]]), pack([['ui/menu.js', Buffer.from([255])]]),
    pack([['../menu.js', 'model_catalog_json']]), pack([['ui/menu.js', Buffer.alloc(16 * 1024 ** 2 + 1)]])]) {
    fs.writeFileSync(file, bytes); assert.throws(() => inspectContractArchive(file, hash(bytes), true));
  }
  const bytes = pack([['ui/menu.js', 'model_catalog_json']]); fs.writeFileSync(file, bytes);
  assert.throws(() => inspectContractArchive(file, '0'.repeat(64), true));
  assert.throws(() => inspectContractArchive(file, hash(bytes), 'true'));
});
test('renderer candidate count cap is not silently sampled', t => {
  const file = path.join(scratch(t), 'app.asar');
  const bytes = pack(Array.from({ length: 4097 }, (_, i) => [`ui/f${i}.js`, 'model_catalog_json']));
  fs.writeFileSync(file, bytes); assert.throws(() => inspectContractArchive(file, hash(bytes), true));
});

test('published renderer source has a separate global text budget and overlap deduplication', () => {
  const sources = MENU_ANCHORS.map((anchor, i) => ({ path: `ui/${i}.js`, matches: [{ anchor, count: 8,
    excerpts: Array.from({ length: 8 }, (_, j) => ({ offset: j * 10000 + 900, start: j * 10000, end: j * 10000 + 1800, text: 'x'.repeat(1800) })) }] }));
  const publication = trimMenuExcerpts(sources);
  const excerpts = sources.flatMap(s => s.matches).flatMap(m => m.excerpts);
  assert.ok(publication.textBytes <= 16384);
  assert.equal(publication.textBytes, excerpts.reduce((n, e) => n + Buffer.byteLength(e.text), 0));
  assert.ok(publication.budgetOmitted > 0);
  assert.ok(sources.every(s => s.matches[0].excerpts.length >= 1 && s.matches[0].excerpts.length <= 2));
});

test('renderer excerpts keep valid Unicode and count UTF-8 text bytes', () => {
  const anchor = 'model_provider'; const source = '😀'.repeat(1000) + 'x' + anchor + 'y' + '😀'.repeat(1000);
  const found = sourceExcerpts(source, 'ui/menu.js', hash(source), { [anchor]: 8 }, [anchor], 900, true);
  const e = found.matches[0].excerpts[0];
  assert.equal(e.text.isWellFormed(), true);
  assert.equal(e.text, source.slice(e.start, e.end));
  const publication = trimMenuExcerpts([found]);
  assert.equal(publication.textBytes, Buffer.byteLength(e.text, 'utf8'));
  assert.ok(publication.textBytes > e.text.length);
});
