import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { ANCHORS, sourceExcerpts, inspectContractArchive, validateSchemas } from './review-desktop-contract.mjs';

// Synthetic safety fixtures, not captured Codex output or proof of its behavior.
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
function scratch(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-contract-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true })); return dir;
}
function pack(entries) {
  const header = { files: { '.vite': { files: { build: { files: {} } } } } }; const body = []; let offset = 0;
  for (const [name, text, extra] of entries) {
    const bytes = Buffer.from(text); header.files['.vite'].files.build.files[name] = { offset: String(offset), size: bytes.length, ...extra };
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
