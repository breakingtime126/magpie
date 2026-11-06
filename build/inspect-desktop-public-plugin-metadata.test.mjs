import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import {
  PUBLIC_PLUGIN_LIMITS, TARGETS, analyzePublicPluginSource,
  inspectPublicPluginPackage, publicPluginKeysDigest, inspectPackage, readPublicManifest,
} from './inspect-desktop-static-metadata.mjs';

const JS = '.vite/build/public-plugin-fixture.js';
const OTHER_JS = '.vite/build/second-public-plugin-fixture.js';
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
function fixture(t) {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-public-plugin-'));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  const root = path.join(home, 'OpenAI.Codex_26.1103.3517.0_x64__abcdefghijklm');
  fs.mkdirSync(path.join(root, 'app', 'resources'), { recursive: true });
  return root;
}
function pack(entries) {
  const header = { files: {} }, buffers = [];
  let offset = 0;
  for (const [entry, data] of entries) {
    const parts = entry.split('/');
    let node = header;
    for (const part of parts.slice(0, -1)) {
      node.files[part] ??= { files: {} };
      node = node.files[part];
    }
    const bytes = Buffer.isBuffer(data) ? data : Buffer.from(data);
    node.files[parts.at(-1)] = { offset: String(offset), size: bytes.length };
    buffers.push(bytes); offset += bytes.length;
  }
  return packHeader(header, Buffer.concat(buffers));
}
function packHeader(header, body = Buffer.alloc(0)) {
  const json = Buffer.from(JSON.stringify(header));
  const padding = Buffer.alloc((4 - json.length % 4) % 4);
  const size = 8 + json.length + padding.length;
  const prefix = Buffer.alloc(16);
  prefix.writeUInt32LE(4, 0); prefix.writeUInt32LE(size, 4);
  prefix.writeUInt32LE(size - 4, 8); prefix.writeInt32LE(json.length, 12);
  return Buffer.concat([prefix, json, padding, body]);
}
function writeArchive(root, entries) {
  const bytes = pack(entries);
  fs.writeFileSync(path.join(root, 'app', 'resources', 'app.asar'), bytes);
  return bytes;
}
function manifest(root, directory, value, suffix = '.agents/plugins/marketplace.json') {
  const relative = 'app/resources/plugins/' + directory + '/' + suffix;
  const full = path.join(root, ...relative.split('/'));
  fs.mkdirSync(path.dirname(full), { recursive: true });
  const bytes = typeof value === 'string' ? value : JSON.stringify(value);
  fs.writeFileSync(full, bytes);
  return { relative, full, bytes };
}

test('whole static identities and keys include curated names without a guessed allowlist', () => {
  const source = 'const a="browser@openai-curated-remote"; const b="plugins.other@public-market.enabled=true";';
  const found = analyzePublicPluginSource(source);
  assert.equal(found.complete, true);
  assert.deepEqual(found.candidates, [
    { key: 'plugins.browser@openai-curated-remote.enabled', derivation: 'id_literal' },
    { key: 'plugins.browser@openai-curated-remote.auto_provision', derivation: 'id_literal' },
    { key: 'plugins.other@public-market.enabled', derivation: 'key_literal' },
  ]);
});

test('comments, regex, escaped or interpolated names never make public candidates', () => {
  const tick = String.fromCharCode(96);
  const source = '// "comment@market"\nconst r=/"regex@market"/;' +
    'const a="escaped\\u0040market"; const b=' + tick + 'dynamic@$' + '{market}' + tick + ';' +
    'const c="prefix actual@market suffix"; const d="plugins.x@market.enabled=SYNTHETIC_PRIVATE_VALUE";';
  assert.deepEqual(analyzePublicPluginSource(source), { complete: true, candidates: [] });
});

test('all candidates in a lexically incomplete file are discarded', () => {
  for (const tail of ['const x="unterminated', 'if(x){} /ambiguous/.test(x);',
    'const r=/[[a] "hidden@market" /b]/v;', 'const r=/[a]/v;']) {
    const found = analyzePublicPluginSource('const a="earlier@market";' + tail);
    assert.equal(found.complete, false);
    assert.deepEqual(found.candidates, []);
  }
});

test('new package chunks are scanned and exact source hashes remain package-bound', t => {
  const root = fixture(t);
  const source = 'const x="browser@openai-curated-remote"; const secret="SYNTHETIC_PRIVATE_VALUE";';
  const before = writeArchive(root, [[JS, source], ['user/home/private.js', '"do-not-read@private"']]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'complete');
  assert.equal(report.archiveSha256, digest(before));
  assert.deepEqual(report.coverage, {
    jsFiles: 1, jsComplete: 1, jsSkipped: 0, sourceBytes: Buffer.byteLength(source), manifestFiles: 0,
  });
  assert.deepEqual(report.sources, [{ kind: 'asar_js', path: JS, sha256: digest(source) }]);
  assert.deepEqual(report.candidates.map(x => x.key), [
    'plugins.browser@openai-curated-remote.auto_provision',
    'plugins.browser@openai-curated-remote.enabled',
  ]);
  assert.equal(report.keysSha256, digest(report.candidates.map(x => x.key).join('\n')));
  assert.ok(report.candidates.every(x => x.source === 0 && x.derivation === 'id_literal'));
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE_VALUE'));
  assert.ok(!JSON.stringify(report).includes('do-not-read'));
  assert.deepEqual(fs.readFileSync(path.join(root, 'app/resources/app.asar')), before);
});

test('manifest identity combines only native name fields and ignores referenced resources', t => {
  const root = fixture(t);
  writeArchive(root, []);
  const file = manifest(root, 'public-package', {
    name: 'public-market', plugins: [
      { name: 'browser', source: '../../SYNTHETIC_PRIVATE_PATH', token: 'SYNTHETIC_PRIVATE_VALUE' },
      { name: 'browser', source: './plugins/browser' },
    ],
  });
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'complete');
  assert.equal(report.coverage.manifestFiles, 1);
  assert.deepEqual(report.sources, [{
    kind: 'marketplace_manifest', path: file.relative, sha256: digest(file.bytes),
  }]);
  assert.deepEqual(report.candidates.map(x => x.key), [
    'plugins.browser@public-market.auto_provision', 'plugins.browser@public-market.enabled',
  ]);
  assert.ok(report.candidates.every(x => x.derivation === 'manifest_identity'));
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
});

test('all four native manifest layouts are bounded public sources', t => {
  for (const suffix of ['.agents/plugins/marketplace.json', '.agents/plugins/api_marketplace.json',
    '.claude-plugin/marketplace.json', '.cursor-plugin/marketplace.json']) {
    const root = fixture(t);
    writeArchive(root, []);
    manifest(root, 'market', { name: 'market', plugins: [{ name: 'plugin' }] }, suffix);
    const report = inspectPublicPluginPackage(root);
    assert.equal(report.status, 'complete');
    assert.equal(report.candidates.length, 2);
  }
});

test('candidate deduplication is stable and preserves one actual source', t => {
  const root = fixture(t);
  writeArchive(root, [[OTHER_JS, 'const a="same@market";'], [JS, 'const a="same@market";']]);
  manifest(root, 'market', { name: 'market', plugins: [{ name: 'same' }] });
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.candidates.length, 2);
  assert.equal(report.sources.length, 1);
  assert.equal(report.sources[0].path, JS);
  assert.deepEqual(report.candidates.map(x => x.source), [0, 0]);
});

test('partial scanning names no key from an ambiguous file and does not claim absence', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const x="good@market";'],
    [OTHER_JS, 'const x="SYNTHETIC_PRIVATE@market"; if(x){} /ambiguous/;']]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'partial');
  assert.equal(report.coverage.jsFiles, 2);
  assert.equal(report.coverage.jsComplete, 1);
  assert.equal(report.coverage.jsSkipped, 1);
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
});

test('invalid UTF8, truncated ASAR and unsafe offsets block without retained candidates', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, Buffer.from([255])]]);
  assert.deepEqual(inspectPublicPluginPackage(root), {
    schema: 1, status: 'blocked', reason: 'invalid_utf8',
  });
  fs.writeFileSync(path.join(root, 'app/resources/app.asar'), Buffer.alloc(0));
  assert.equal(inspectPublicPluginPackage(root).reason, 'truncated_archive');
  const header = { files: { '.vite': { files: { build: { files: {
    'bad.js': { offset: '1', size: 10 },
  } } } } } };
  fs.writeFileSync(path.join(root, 'app/resources/app.asar'), packHeader(header));
  assert.equal(inspectPublicPluginPackage(root).reason, 'file_outside_archive');
});

test('ASAR links and unpacked public sources block without following them', t => {
  for (const node of [{ link: '../../outside' }, { unpacked: true, files: {
    build: { files: { 'candidate.js': { size: 1 } } },
  } }]) {
    const root = fixture(t);
    fs.writeFileSync(path.join(root, 'app/resources/app.asar'), packHeader({ files: { '.vite': node } }));
    assert.deepEqual(inspectPublicPluginPackage(root), {
      schema: 1, status: 'blocked', reason: 'public_source_not_packed',
    });
  }
});

test('malformed, oversized and invalid-name manifests block rather than look empty', t => {
  for (const value of ['{', { name: 'market', plugins: 'invalid' },
    { name: 'market', plugins: [{ name: '../private' }] },
    { name: 'bad@market', plugins: [] }]) {
    const root = fixture(t);
    writeArchive(root, []);
    manifest(root, 'market', value);
    assert.deepEqual(inspectPublicPluginPackage(root), {
      schema: 1, status: 'blocked', reason: 'public_manifest_invalid',
    });
  }
  const root = fixture(t);
  writeArchive(root, []);
  manifest(root, 'market', ' '.repeat(PUBLIC_PLUGIN_LIMITS.manifest + 1));
  assert.equal(inspectPublicPluginPackage(root).reason, 'public_manifest_limit');
});

test('candidate and source-file limits discard the entire catalog', t => {
  const root = fixture(t);
  const many = Array.from({ length: 65 }, (_, i) => 'const a' + i + '="plugin' + i + '@market";').join('');
  writeArchive(root, [[JS, many]]);
  assert.deepEqual(inspectPublicPluginPackage(root), {
    schema: 1, status: 'blocked', reason: 'public_catalog_limit',
  });
  writeArchive(root, Array.from({ length: PUBLIC_PLUGIN_LIMITS.jsFiles + 1 },
    (_, i) => ['.vite/build/file' + i + '.js', '']));
  assert.equal(inspectPublicPluginPackage(root).reason, 'public_source_limit');
});

test('package root and manifest directory redirects are rejected', t => {
  assert.equal(inspectPublicPluginPackage('relative').reason, 'public_source_path');
  const root = fixture(t);
  writeArchive(root, []);
  const outside = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-public-outside-'));
  t.after(() => fs.rmSync(outside, { recursive: true, force: true }));
  const target = path.join(root, 'app/resources/plugins');
  try { fs.symlinkSync(outside, target, process.platform === 'win32' ? 'junction' : 'dir'); }
  catch (error) {
    if (['EPERM', 'EACCES', 'ENOTSUP'].includes(error.code)) {
      t.skip('OS did not permit the synthetic redirect fixture'); return;
    }
    throw error;
  }
  assert.equal(inspectPublicPluginPackage(root).reason, 'public_source_path');
});

test('empty catalog digest is explicit and fixed-target inspection remains separate', t => {
  const root = fixture(t);
  writeArchive(root, [[TARGETS[0], 'const harmless=1;']]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'complete');
  assert.deepEqual(report.candidates, []);
  assert.deepEqual(report.sources, []);
  assert.equal(report.keysSha256, publicPluginKeysDigest([]));
  assert.equal(report.keysSha256, digest(''));
});

test('legacy and public metadata must come from the same archive snapshot', t => {
  const root = fixture(t), archive = path.join(root, 'app/resources/app.asar');
  writeArchive(root, [[TARGETS[0], 'class A{getConfigOverrides(){return ["legacy_key=true"];}}']]);
  const replacement = pack([[TARGETS[0], 'const x="replacement@market";']]);
  const originalOpen = fs.openSync, originalClose = fs.closeSync;
  const archiveFDs = new Set();
  let swapped = false, archiveReads = 0;
  t.mock.method(fs, 'openSync', function (name, ...args) {
    const fd = originalOpen.call(fs, name, ...args);
    if (name === archive && args[0] === 'r') { archiveFDs.add(fd); archiveReads++; }
    return fd;
  });
  t.mock.method(fs, 'closeSync', function (fd) {
    originalClose.call(fs, fd);
    if (archiveFDs.delete(fd) && !swapped) {
      swapped = true;
      fs.writeFileSync(archive, replacement);
    }
  });
  assert.throws(() => inspectPackage(root), error => error.code === 'archive_changed');
  assert.equal(swapped, true);
  assert.equal(archiveReads, 2);
});

test('a later unreadable source discards earlier valid public candidates', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const x="valid-first@market";'], [OTHER_JS, Buffer.from([255])]]);
  const report = inspectPublicPluginPackage(root);
  assert.deepEqual(report, { schema: 1, status: 'blocked', reason: 'invalid_utf8' });
  assert.ok(!JSON.stringify(report).includes('valid-first'));
});

test('manifest source budget is checked before any content read', t => {
  const root = fixture(t);
  const file = manifest(root, 'market', { name: 'market', plugins: [{ name: 'plugin' }] });
  const originalRead = fs.readSync;
  let reads = 0;
  t.mock.method(fs, 'readSync', function (...args) {
    reads++;
    return originalRead.apply(fs, args);
  });
  assert.throws(() => readPublicManifest(file.full, Buffer.byteLength(file.bytes) - 1),
    error => error.code === 'public_source_limit');
  assert.equal(reads, 0);
  assert.equal(readPublicManifest(file.full, Buffer.byteLength(file.bytes)).keys.length, 2);
  assert.ok(reads > 0);
});

test('line comments stop at every JavaScript line terminator', () => {
  for (const end of ['\n', '\r', '\r\n', '\u2028', '\u2029']) {
    const found = analyzePublicPluginSource('// "comment@market"' + end + 'const x="actual@market";');
    assert.equal(found.complete, true);
    assert.deepEqual(found.candidates.map(item => item.key), [
      'plugins.actual@market.enabled', 'plugins.actual@market.auto_provision',
    ]);
  }
});

test('unsupported Annex B HTML comments invalidate all file candidates', () => {
  for (const html of ['<!-- "comment@market"\nconst x=1;', '\n--> "comment@market"\nconst x=1;']) {
    const found = analyzePublicPluginSource('const x="earlier@market";\n' + html);
    assert.deepEqual(found, { complete: false, candidates: [] });
  }
  assert.equal(analyzePublicPluginSource('const x="<!--"; const y="actual@market";').complete, true);
});

test('Unicode identifier boundaries never expose regex contents after labeled control flow', () => {
  for (const [keyword, end] of [['break', '\u2028'], ['continue', '\u2029'], ['break', '\r']]) {
    const source = 'const p="earlier@market"; outer: for (;;) { ' + keyword +
      ' outer' + end + '/"regex@market"/.test(""); }';
    assert.deepEqual(analyzePublicPluginSource(source), { complete: false, candidates: [] });
  }
});

test('ordinary decrement followed by greater-than is not an HTML comment', () => {
  const found = analyzePublicPluginSource('let i=2;while(i-->0){} const p="actual@market";');
  assert.equal(found.complete, true);
  assert.equal(found.candidates.length, 2);
});
