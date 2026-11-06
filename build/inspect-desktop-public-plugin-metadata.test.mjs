import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { Parser } from './vendor/acorn-8.15.0/acorn.mjs';
import { AST_BLOCK_CODES, AST_SKIP_CODES, PublicPluginASTLimit } from './inspect-desktop-public-plugin-ast.mjs';
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

test('all candidates in a syntactically incomplete file are discarded', () => {
  for (const tail of ['const x="unterminated', 'const broken=;',
    'const r=/[[a] "hidden@market" /b]/v;', 'const duplicate=1;const duplicate=2;']) {
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

test('partial scanning names no key from an invalid file and does not claim absence', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const x="good@market";'],
    [OTHER_JS, 'const x="SYNTHETIC_PRIVATE@market"; const broken=;']]);
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

test('Annex B HTML comments parse without exposing their text', () => {
  for (const html of ['<!-- "comment@market"\nconst y=1;', '\n--> "comment@market"\nconst y=1;']) {
    const found = analyzePublicPluginSource('const x="earlier@market";\n' + html);
    assert.equal(found.complete, true);
    assert.deepEqual(found.candidates.map(item => item.key), [
      'plugins.earlier@market.enabled', 'plugins.earlier@market.auto_provision',
    ]);
  }
  assert.equal(analyzePublicPluginSource('const x="<!--"; const y="actual@market";').complete, true);
});

test('Unicode identifier boundaries never expose regex contents after labeled control flow', () => {
  for (const [keyword, end] of [['break', '\u2028'], ['continue', '\u2029'], ['break', '\r']]) {
    const source = 'const p="earlier@market"; outer: for (;;) { ' + keyword +
      ' outer' + end + '/"regex@market"/.test(""); }';
    const found = analyzePublicPluginSource(source);
    assert.equal(found.complete, true);
    assert.deepEqual(found.candidates.map(item => item.key), [
      'plugins.earlier@market.enabled', 'plugins.earlier@market.auto_provision',
    ]);
  }
});

test('ordinary decrement followed by greater-than is not an HTML comment', () => {
  const found = analyzePublicPluginSource('let i=2;while(i-->0){} const p="actual@market";');
  assert.equal(found.complete, true);
  assert.equal(found.candidates.length, 2);
});

test('construction-only files have bound source records without invented identities', t => {
  const root = fixture(t), tick = String.fromCharCode(96);
  const source = 'class C{getConfigOverrides(){return [' + tick +
    'plugins.$' + '{SYNTHETIC_PRIVATE_ID}.apps.$' + '{app}.enabled=true' + tick + ']}}';
  const before = writeArchive(root, [[JS, source]]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'complete');
  assert.equal(report.archiveSha256, digest(before));
  assert.deepEqual(report.candidates, []);
  assert.deepEqual(report.sources, [{ kind: 'asar_js', path: JS, sha256: digest(source) }]);
  assert.deepEqual(report.analysis, {
    parser: 'acorn_8_15_0',
    methods: [{ name: 'getConfigOverrides', definitions: 1, unresolved: 0 },
      { name: 'startupCommand', definitions: 0, unresolved: 0 }],
    constructions: [{ source: 0, method: 'getConfigOverrides', form: 'template',
      shape: 'app_enabled', value: 'bool_true', count: 1 }],
    skipped: [],
  });
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE_ID'));
  assert.deepEqual(fs.readFileSync(path.join(root, 'app/resources/app.asar')), before);
});

test('a malformed method source contributes neither definitions nor construction rows', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const p="good@market";'],
    [OTHER_JS, 'function getConfigOverrides(){return "plugins.secret@market.enabled=true"} ???']]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'partial');
  assert.ok(report.analysis.methods.every(item => item.definitions === 0 && item.unresolved === 0));
  assert.deepEqual(report.analysis.constructions, []);
  assert.ok(!JSON.stringify(report).includes('secret'));
});

test('construction rows in different files retain separate source provenance', t => {
  const root = fixture(t), tick = String.fromCharCode(96);
  const source = 'function startupCommand(){return ' + tick + 'plugins.$' + '{id}.enabled=true' + tick + '}';
  writeArchive(root, [[JS, source], [OTHER_JS, source]]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.sources.length, 2);
  assert.equal(report.analysis.methods[1].definitions, 2);
  assert.deepEqual(report.analysis.constructions.map(item => item.source), [0, 1]);
  assert.ok(report.analysis.constructions.every(item => item.count === 1));
});

test('non-skippable later AST failures clear all earlier public evidence', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const x="earlier-public@market";'],
    [OTHER_JS, 'const x="later-public@market";']]);
  const parse = Parser.prototype.parse;
  let reason, calls = 0;
  t.mock.method(Parser.prototype, 'parse', function (...args) {
    if (++calls === 2) throw new PublicPluginASTLimit(reason);
    return parse.apply(this, args);
  });
  for (reason of AST_BLOCK_CODES.filter(code => !AST_SKIP_CODES.includes(code))) {
    calls = 0;
    const report = inspectPublicPluginPackage(root);
    assert.equal(calls, 2);
    assert.deepEqual(report, { schema: 1, status: 'blocked', reason });
    assert.ok(!JSON.stringify(report).includes('earlier-public'));
    assert.ok(!JSON.stringify(report).includes('later-public'));
  }
});

test('unexpected parser failures publish only a fixed inspection failure', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const x="public@market";']]);
  t.mock.method(Parser.prototype, 'parse', () => { throw new Error('SYNTHETIC_PRIVATE_FAILURE'); });
  assert.deepEqual(inspectPublicPluginPackage(root), {
    schema: 1, status: 'blocked', reason: 'io_or_inspection_failed',
  });
});

test('local guards before or after valid files retain only complete independent evidence', t => {
  const root = fixture(t);
  const paths = ['.vite/build/a-first.js', '.vite/build/b-middle.js', '.vite/build/c-last.js'];
  const normal = [
    'function getConfigOverrides(){return "plugins.first@market.enabled=true"}',
    'function getConfigOverrides(){return "plugins.middle@market.enabled=true"}',
    'function getConfigOverrides(){return "plugins.last@market.enabled=true"}',
  ];
  const parse = Parser.prototype.parse;
  let failureAt = 0, reason, calls = 0;
  t.mock.method(Parser.prototype, 'parse', function (...args) {
    const result = parse.apply(this, args);
    if (++calls === failureAt + 1) throw new PublicPluginASTLimit(reason);
    return result;
  });
  for (reason of AST_SKIP_CODES) {
    for (failureAt of [0, 1, 2]) {
      calls = 0;
      const sources = normal.map((source, i) => i === failureAt ?
        'function getConfigOverrides(){return "plugins.SYNTHETIC_PRIVATE@market.enabled=true"}' : source);
      writeArchive(root, paths.map((name, i) => [name, sources[i]]));
      const report = inspectPublicPluginPackage(root);
      assert.equal(calls, 3);
      assert.equal(report.status, 'partial');
      assert.equal(report.coverage.jsFiles, 3);
      assert.equal(report.coverage.jsComplete, 2);
      assert.equal(report.coverage.jsSkipped, 1);
      assert.equal(report.coverage.sourceBytes, sources.reduce((sum, value) => sum + Buffer.byteLength(value), 0));
      assert.deepEqual(report.analysis.skipped, [{ reason, count: 1 }]);
      assert.equal(report.analysis.methods[0].definitions, 2);
      assert.equal(report.analysis.methods[0].unresolved, 0);
      assert.equal(report.candidates.length, 2);
      assert.equal(report.sources.length, 2);
      assert.deepEqual(report.sources.map(item => item.path), paths.filter((_, i) => i !== failureAt));
      assert.equal(report.analysis.constructions.length, 2);
      assert.ok(report.analysis.constructions.every(item => item.count === 1));
      assert.ok(report.candidates.every(item => item.source >= 0 && item.source < 2));
      assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
    }
  }
});

test('actual late walk-depth guard discards earlier local candidates and method rows', t => {
  const root = fixture(t);
  const limited = 'function getConfigOverrides(){return "plugins.SYNTHETIC_PRIVATE@market.enabled=true"}' +
    'const x=root' + '.member'.repeat(600) + ';';
  writeArchive(root, [[JS, limited], [OTHER_JS, 'const p="good@market";']]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'partial');
  assert.deepEqual(report.analysis.skipped, [{ reason: 'public_ast_walk_depth_limit', count: 1 }]);
  assert.ok(report.analysis.methods.every(item => item.definitions === 0 && item.unresolved === 0));
  assert.deepEqual(report.analysis.constructions, []);
  assert.deepEqual(report.sources.map(item => item.path), [OTHER_JS]);
  assert.deepEqual(report.candidates.map(item => item.key), [
    'plugins.good@market.auto_provision', 'plugins.good@market.enabled',
  ]);
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
});

test('skip counts are canonical, aggregate, and equal the omitted file count', t => {
  const root = fixture(t);
  writeArchive(root, [
    ['.vite/build/a-depth.js', 'const x=root' + '.x'.repeat(600) + ';'],
    ['.vite/build/b-syntax.js', '???'],
    ['.vite/build/c-depth.js', 'const x=root' + '.x'.repeat(600) + ';'],
  ]);
  manifest(root, 'market', { name: 'market', plugins: [{ name: 'public' }] });
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'partial');
  assert.deepEqual(report.analysis.skipped, [
    { reason: 'syntax_error', count: 1 }, { reason: 'public_ast_walk_depth_limit', count: 2 },
  ]);
  assert.equal(report.coverage.jsSkipped, report.analysis.skipped.reduce((sum, item) => sum + item.count, 0));
  assert.equal(report.candidates.length, 2);
  assert.ok(report.candidates.every(item => item.derivation === 'manifest_identity'));
});

test('bare RangeError after a valid file still blocks the entire package', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const p="earlier@market";'], [OTHER_JS, 'const q=1;']]);
  const parse = Parser.prototype.parse;
  let calls = 0;
  t.mock.method(Parser.prototype, 'parse', function (...args) {
    if (++calls === 2) throw new RangeError('SYNTHETIC_PRIVATE_STACK');
    return parse.apply(this, args);
  });
  assert.deepEqual(inspectPublicPluginPackage(root), {
    schema: 1, status: 'blocked', reason: 'public_ast_range_error',
  });
  assert.equal(calls, 2);
});

test('complete public analysis has no skip reasons', t => {
  const root = fixture(t);
  writeArchive(root, [[JS, 'const p="public@market";']]);
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'complete');
  assert.equal(report.coverage.jsSkipped, 0);
  assert.deepEqual(report.analysis.skipped, []);
});
