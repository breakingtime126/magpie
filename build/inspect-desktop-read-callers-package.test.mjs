
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { inspectDesktopReadPackage, READ_CALL_LIMITS } from './inspect-desktop-read-callers.mjs';

function sha(bytes) { return createHash('sha256').update(bytes).digest('hex'); }
function fixture(t, contents) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'desktop-read-callers-'));
  t.after(() => fs.rmSync(tmp, { recursive: true, force: true }));
  const root = path.join(tmp, 'OpenAI.Codex_26.1103.3517.0_x64__abcdefghijklm');
  fs.mkdirSync(path.join(root, 'app', 'resources'), { recursive: true });
  const files = {}, bodies = []; let offset = 0;
  for (const [name, source] of Object.entries(contents)) {
    const bytes = Buffer.from(source);
    files[name] = { size: bytes.length, offset: String(offset) };
    offset += bytes.length; bodies.push(bytes);
  }
  const json = Buffer.from(JSON.stringify({ files: { '.vite': { files: { build: { files } } } } }));
  const headerSize = 8 + Math.ceil(json.length / 4) * 4;
  const header = Buffer.alloc(8 + headerSize);
  header.writeUInt32LE(4, 0); header.writeUInt32LE(headerSize, 4);
  header.writeUInt32LE(headerSize - 4, 8); header.writeInt32LE(json.length, 12);
  json.copy(header, 16);
  const asar = Buffer.concat([header, ...bodies]);
  const file = path.join(root, 'app', 'resources', 'app.asar');
  fs.writeFileSync(file, asar);
  return { root, file, asar, expected: sha(asar) };
}

test('real bounded ASAR reader binds complete source facts to the expected original identity', t => {
  const f = fixture(t, { 'one.js': 'async function f(){return(await x("config/read",{})).config.model_provider;}' });
  const result = inspectDesktopReadPackage(f.root, f.expected);
  assert.equal(result.status, 'complete');
  assert.equal(result.archiveSha256, f.expected);
  assert.equal(result.filesComplete, 1);
  assert.equal(result.sources[0].sha256, sha(Buffer.from('async function f(){return(await x("config/read",{})).config.model_provider;}')));
  assert.equal(result.sources[0].path, '.vite/build/one.js');
  assert.deepEqual(result.sources[0].analysis.calls[0].responseFields.map(x => x.path),
    [['config'], ['config', 'model_provider']]);
});

test('only wholly parsed sources survive a partial report', t => {
  const f = fixture(t, { 'one.js': 'x("project/list",{});', 'two.js': 'x("config/read",{}); const = 1;' });
  const result = inspectDesktopReadPackage(f.root, f.expected);
  assert.equal(result.status, 'partial');
  assert.equal(result.filesComplete, 1);
  assert.equal(result.filesSelected, 2);
  assert.equal(result.sources[0].analysis.calls.length, 1);
  assert.deepEqual(result.sources[1].analysis, { schema: 1, status: 'unavailable', reason: 'syntax_error' });
});

test('wrong expected archive identity never produces source facts', t => {
  const f = fixture(t, { 'one.js': 'x("config/read",{});' });
  assert.deepEqual(inspectDesktopReadPackage(f.root, '0'.repeat(64)),
    { schema: 1, status: 'unavailable', reason: 'archive_mismatch' });
});

test('call budget is shared across files rather than reset per source', t => {
  const f = fixture(t, {
    'one.js': 'x("project/list",{});'.repeat(64),
    'two.js': 'x("project/list",{});'.repeat(READ_CALL_LIMITS.calls - 63),
  });
  assert.deepEqual(inspectDesktopReadPackage(f.root, f.expected),
    { schema: 1, status: 'unavailable', reason: 'call_limit' });
});

test('source bytes budget is shared across selected files', t => {
  const source = '/*' + 'x'.repeat(12 * 1024 ** 2) + '*/';
  const f = fixture(t, { 'one.js': source, 'two.js': source, 'three.js': source });
  assert.deepEqual(inspectDesktopReadPackage(f.root, f.expected),
    { schema: 1, status: 'unavailable', reason: 'source_limit' });
});

test('invalid package roots and source decoding fail without raw paths or content', t => {
  const f = fixture(t, { 'one.js': 'x("config/read",{});' });
  assert.equal(inspectDesktopReadPackage(path.dirname(f.root), f.expected).reason, 'package_path');
  const bytes = Buffer.from(f.asar); bytes[bytes.length - 1] = 255;
  fs.writeFileSync(f.file, bytes);
  assert.deepEqual(inspectDesktopReadPackage(f.root, sha(bytes)),
    { schema: 1, status: 'unavailable', reason: 'source_read_failed' });
});
