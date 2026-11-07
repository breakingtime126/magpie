import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { TARGETS, LIMITS, readIndex, inspectArchive, inspectPackage, inspectPublicPluginPackage, parsePackageArguments, analyzeSource } from './inspect-desktop-static-metadata.mjs';

function fixture(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-static-metadata-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}
function pack(header, body = Buffer.alloc(0)) {
  const json = Buffer.from(JSON.stringify(header));
  const padding = Buffer.alloc((4 - json.length % 4) % 4);
  const headerSize = 8 + json.length + padding.length;
  const prefix = Buffer.alloc(16);
  prefix.writeUInt32LE(4, 0); prefix.writeUInt32LE(headerSize, 4);
  prefix.writeUInt32LE(headerSize - 4, 8); prefix.writeInt32LE(json.length, 12);
  return Buffer.concat([prefix, json, padding, body]);
}
function tree(entries) {
  const header = { files: {} };
  for (const [entryPath, value] of entries) {
    const parts = entryPath.split('/');
    let node = header;
    for (const part of parts.slice(0, -1)) {
      node.files[part] ??= { files: {} }; node = node.files[part];
    }
    node.files[parts.at(-1)] = value;
  }
  return header;
}
function archive(t, bytes) {
  const file = path.join(fixture(t), 'app.asar');
  fs.writeFileSync(file, bytes); return file;
}
function sourceArchive(t, source) {
  const data = Buffer.from(source);
  return archive(t, pack(tree([[TARGETS[0], { offset: '0', size: data.length }]]), data));
}
function rejected(code) { return e => e && e.code === code && e.message === code; }
function index(file, size = fs.statSync(file).size) {
  const fd = fs.openSync(file, 'r');
  try { return readIndex(fd, size); } finally { fs.closeSync(fd); }
}
function packageRoot(t) {
  const root = path.join(fixture(t), 'OpenAI.Codex_26.1103.3517.0_x64__abcdefghijklm');
  fs.mkdirSync(path.join(root, 'app', 'resources'), { recursive: true });
  return root;
}

test('documented offsets read selected packed source without modifying bytes', t => {
  const file = sourceArchive(t, 'class A{getConfigOverrides(){return ["extra_key=true"];}}');
  const before = fs.readFileSync(file), report = inspectArchive(file);
  assert.equal(report.runtimeKeyProven, false);
  assert.equal(report.entries[0].status, 'static_candidates_only');
  assert.deepEqual(report.entries[0].candidates.map(x => x.key), ['extra_key']);
  assert.equal(report.entries[1].status, 'missing_entry');
  assert.equal(report.entries[2].status, 'missing_entry');
  assert.deepEqual(fs.readFileSync(file), before);
});

test('wrong, truncated and oversized archive prefixes fail before allocation', t => {
  for (const n of [0, 7, 15]) {
    assert.throws(() => index(archive(t, Buffer.alloc(n))), rejected('truncated_archive'));
  }
  assert.throws(() => index(archive(t, Buffer.alloc(16))), rejected('unsupported_asar_prefix'));
  const good = pack({ files: {} }), file = archive(t, good);
  assert.throws(() => index(file, LIMITS.archive + 1), rejected('archive_too_large'));
  const oversize = Buffer.from(good); oversize.writeUInt32LE(LIMITS.header + 4, 4);
  assert.throws(() => index(archive(t, oversize), LIMITS.header + 16), rejected('header_too_large'));
  assert.throws(() => index(archive(t, good.subarray(0, 16))), rejected('truncated_header'));
});

test('invalid Pickle payload, length, padding and JSON fail closed', t => {
  const good = pack({ files: {} });
  for (const [offset, value, code] of [
    [4, 7, 'invalid_header_size'], [8, 0, 'invalid_header_pickle'], [12, 0xffffffff, 'invalid_json_size'],
  ]) {
    const bad = Buffer.from(good); bad.writeUInt32LE(value, offset);
    assert.throws(() => index(archive(t, bad)), rejected(code));
  }
  const badJSON = Buffer.from(good); badJSON[16] = 0x21;
  assert.throws(() => index(archive(t, badJSON)), rejected('invalid_header_json'));
  const badUTF8 = Buffer.from(good); badUTF8[16] = 0xff;
  assert.throws(() => index(archive(t, badUTF8)), rejected('invalid_utf8'));
  let padded;
  for (let n = 0; n < 4; n++) {
    padded = pack({ files: {}, extra: 'x'.repeat(n) });
    if (padded.readInt32LE(12) % 4 !== 0) break;
  }
  padded[padded.length - 1] = 1;
  assert.throws(() => index(archive(t, padded)), rejected('invalid_header_padding'));
});

test('unsafe offsets, sizes and outside-archive ranges are rejected', t => {
  for (const offset of ['-1', '1.5', '1e2', '90071992547409920000', 0]) {
    const file = archive(t, pack(tree([[TARGETS[0], { offset, size: 1 }]]), Buffer.alloc(1)));
    const code = offset === '90071992547409920000' ? 'file_outside_archive' : 'invalid_file_offset';
    assert.throws(() => index(file), rejected(code));
  }
  for (const size of [-1, 1.5, Number.MAX_SAFE_INTEGER + 1, '1']) {
    const file = archive(t, pack(tree([[TARGETS[0], { offset: '0', size }]])));
    assert.throws(() => index(file), rejected('invalid_file_size'));
  }
  const outside = archive(t, pack(tree([[TARGETS[0], { offset: '1', size: 1 }]]), Buffer.alloc(1)));
  assert.throws(() => index(outside), rejected('file_outside_archive'));
});

test('invalid path components and excessive depth reject without reflecting names', t => {
  for (const name of ['', '.', '..', 'a/b', 'a\\b', 'c:drive', 'bad\u0000name', 'trailing.', 'trailing ']) {
    const file = archive(t, pack({ files: { [name]: { offset: '0', size: 0 } } }));
    assert.throws(() => index(file), rejected('invalid_entry_path'));
  }
  const header = { files: {} }; let node = header;
  for (let i = 0; i <= LIMITS.depth; i++) { node.files.x = { files: {} }; node = node.files.x; }
  assert.throws(() => index(archive(t, pack(header))), rejected('header_too_deep'));
});

test('ASAR links, unpacked files and unpacked ancestors are never followed', t => {
  const header = tree([
    [TARGETS[0], { link: '../../outside-private-source.js' }],
    [TARGETS[1], { size: 999, unpacked: true }],
  ]);
  const report = inspectArchive(archive(t, pack(header)));
  assert.equal(report.entries[0].status, 'link_not_read');
  assert.equal(report.entries[1].status, 'unpacked_not_read');
  assert.ok(!JSON.stringify(report).includes('outside-private-source'));
  const parentLink = { files: { '.vite': { link: '../../outside' } } };
  assert.ok(inspectArchive(archive(t, pack(parentLink))).entries.every(x => x.status === 'link_not_read'));
  const inherited = tree([[TARGETS[0], { size: 999 }]]);
  inherited.files['.vite'].unpacked = true;
  assert.equal(inspectArchive(archive(t, pack(inherited))).entries[0].status, 'unpacked_not_read');
});

test('oversized source is reported before reading; invalid UTF-8 is not replaced', t => {
  const bytes = pack(tree([[TARGETS[0], { offset: '0', size: LIMITS.entry + 1 }]]));
  const file = archive(t, bytes); fs.truncateSync(file, bytes.length + LIMITS.entry + 1);
  assert.equal(inspectArchive(file).entries[0].status, 'entry_too_large');
  const invalid = archive(t, pack(tree([[TARGETS[0], { offset: '0', size: 1 }]]), Buffer.from([255])));
  assert.equal(inspectArchive(invalid).entries[0].status, 'invalid_utf8');
});

test('package reader checks root name, absolute path and missing fixed archive', t => {
  assert.throws(() => inspectPackage('relative'), rejected('package_root_not_absolute'));
  assert.throws(() => inspectPackage(fixture(t)), rejected('unexpected_package_directory'));
  const root = packageRoot(t);
  assert.throws(() => inspectPackage(root), rejected('archive_missing'));
  fs.writeFileSync(path.join(root, 'app', 'resources', 'app.asar'), pack({ files: {} }));
  assert.ok(inspectPackage(root).entries.every(x => x.status === 'missing_entry'));
});

test('package-directory redirects are rejected before source access', t => {
  const root = packageRoot(t), resources = path.join(root, 'app', 'resources');
  const elsewhere = path.join(fixture(t), 'outside');
  fs.mkdirSync(elsewhere); fs.rmdirSync(resources);
  try { fs.symlinkSync(elsewhere, resources, process.platform === 'win32' ? 'junction' : 'dir'); }
  catch (e) {
    if (['EPERM', 'EACCES', 'ENOTSUP'].includes(e.code)) {
      t.skip('OS did not permit the synthetic redirect fixture'); return;
    }
    throw e;
  }
  assert.throws(() => inspectPackage(root), rejected('package_path_redirected'));
});

test('explicit 4239 profile reaches both bounded package readers', t => {
  const root = path.join(fixture(t), 'OpenAI.Codex_26.1103.4239.0_x64__abcdefghijklm');
  fs.mkdirSync(path.join(root, 'app', 'resources'), { recursive: true });
  fs.writeFileSync(path.join(root, 'app', 'resources', 'app.asar'), pack({ files: {} }));
  const report = inspectPackage(root, 'desktop-4239');
  assert.equal(report.status, 'static_candidates_only');
  assert.notEqual(report.publicPlugins.status, 'blocked');
  assert.deepEqual(report.entries.map(x => x.entry), [
    '.vite/build/early-bootstrap-339-ce026819bf837ba7.js',
    '.vite/build/early-bootstrap-824-d144f7c160c094ea.js',
    '.vite/build/early-bootstrap-199-e3316cddb4ea6a6b.js',
  ]);
});

test('method literals and direct -c pairs have separate evidence', () => {
  const report = analyzeSource('class A{#private=1;getConfigOverrides(){return ["features.code_mode_host.enabled=true","count_key=17"];}startupCommand(){return ["-c","extra_key=false"];}}');
  assert.equal(report.lexicalStatus, 'complete');
  assert.equal(report.methods.getConfigOverrides.completeDefinitions, 1);
  assert.equal(report.methods.startupCommand.completeDefinitions, 1);
  assert.ok(report.candidates.some(x => x.key === 'extra_key' && x.evidence === 'adjacent_c_literal'));
  assert.ok(report.candidates.some(x => x.key === 'count_key' && x.type === 'number_literal'));
});

test('comments, string contents and calls do not manufacture definitions', () => {
  const source = 'const s="getConfigOverrides(){return [\\"bad_key=true\\"]}"; /* getConfigOverrides(){return ["comment_key=true"]} */ const r=/getConfigOverrides\\(\\)\\{bad_regex=true\\}/; this.getConfigOverrides(); function other(){return ["other_key=true"];}';
  const report = analyzeSource(source);
  assert.equal(report.methods.getConfigOverrides.completeDefinitions, 0);
  assert.deepEqual(report.candidates, []);
  assert.equal(report.rawOccurrences.getConfigOverrides, 4);
});

test('regex expression prefixes including instanceof and for-await hide their contents', () => {
  const re = '/getConfigOverrides(){return "fake.key=true"}/';
  for (const source of [
    'x instanceof ' + re,
    'export default ' + re,
    'async function f(){for await(x of y) ' + re + ';}',
    'function f(){return ' + re + ';}',
    'if(x) ' + re + '.test(y);',
  ]) {
    const report = analyzeSource(source);
    assert.equal(report.lexicalStatus, 'complete');
    assert.deepEqual(report.candidates, []);
  }
});

test('templates and concatenations emit key prefixes and dynamic type only', () => {
  const tick = String.fromCharCode(96);
  const source = 'class A{getConfigOverrides(){return [' + tick + 'first_key=$' +
    '{{x:' + tick + 'inner=$' + '{f()}' + tick + '}.x}' + tick +
    ',"second_key=" + privateValue];}}';
  assert.deepEqual(analyzeSource(source).candidates.map(x => [x.key, x.type]), [
    ['first_key', 'dynamic_expression'], ['second_key', 'dynamic_expression'],
  ]);
});

test('ambiguous slash, labeled break and truncated methods cannot yield candidates', () => {
  const ambiguous = analyzeSource('class A{getConfigOverrides(){if(x){} /abc/.test(x);return ["hidden_key=true"];}}');
  assert.equal(ambiguous.lexicalStatus, 'ambiguous_slash');
  assert.deepEqual(ambiguous.candidates, []);
  const labeled = analyzeSource('function f(){loop:while(x){break loop\n/getConfigOverrides(){return "fake.key=true"}/}}');
  assert.equal(labeled.lexicalStatus, 'ambiguous_slash');
  assert.deepEqual(labeled.candidates, []);
  const truncated = analyzeSource('class A{getConfigOverrides(){return ["hidden_key=true"];');
  assert.equal(truncated.methods.getConfigOverrides.unsupportedDefinitions, 1);
  assert.deepEqual(truncated.candidates, []);
});

test('reports omit values, source, arbitrary strings and paths and bound candidates', () => {
  const secret = 'SYNTHETIC_PRIVATE_VALUE_DO_NOT_PRINT';
  const report = analyzeSource('class A{getConfigOverrides(){const opaque="' + secret +
    '";return ["safe_key=\\\"' + secret + '\\\"","path_key=C:\\\\outside\\\\private","not_an_assignment"];}}');
  const output = JSON.stringify(report);
  for (const forbidden of [secret, 'outside', 'not_an_assignment', 'getConfigOverrides(){']) {
    assert.ok(!output.includes(forbidden));
  }
  assert.ok(report.candidates.some(x => x.key === 'safe_key' && x.type === 'string_shaped_literal'));
  const tooMany = 'class A{getConfigOverrides(){return [' +
    Array.from({ length: LIMITS.candidates + 1 }, (_, i) => '"key_' + i + '=true"').join(',') + '];}}';
  assert.equal(analyzeSource(tooMany).candidatesLimited, true);
  assert.ok(JSON.stringify(analyzeSource(tooMany)).length < 65536);
});

test('repeated occurrences aggregate and computed or escaped key names are omitted', () => {
  const tick = String.fromCharCode(96);
  const report = analyzeSource('class A{getConfigOverrides(){return ["same_key=true","same_key=true",' +
    tick + '$' + '{dynamicKey}=true' + tick + ',"escaped\\\\key=true"]}}');
  assert.equal(report.candidates.length, 1);
  assert.equal(report.candidates[0].occurrences, 2);
  assert.equal(report.candidates[0].key, 'same_key');
});


test('4239 sources are read and build profiles cannot cross-pair', t => {
  const root = path.join(fixture(t), 'OpenAI.Codex_26.1103.4239.0_x64__abcdefghijklm');
  fs.mkdirSync(path.join(root, 'app', 'resources'), { recursive: true });
  const target = '.vite/build/early-bootstrap-199-e3316cddb4ea6a6b.js';
  const data = Buffer.from('class A{getConfigOverrides(){return ["selected_4239_key=true"];}}');
  const bytes = pack(tree([[target, { offset: '0', size: data.length }]]), data);
  const file = path.join(root, 'app', 'resources', 'app.asar');
  fs.writeFileSync(file, bytes);
  const report = inspectPackage(root, 'desktop-4239');
  assert.equal(report.entries[2].status, 'static_candidates_only');
  assert.equal(report.entries[2].entry, target);
  assert.deepEqual(report.entries[2].candidates.map(x => x.key), ['selected_4239_key']);
  assert.deepEqual(fs.readFileSync(file), bytes);
  assert.ok(inspectArchive(file).entries.every(x => x.status === 'missing_entry'));
  assert.throws(() => inspectPackage(root), rejected('unexpected_package_directory'));
  const oldRoot = packageRoot(t);
  assert.throws(() => inspectPackage(oldRoot, 'desktop-4239'), rejected('unexpected_package_directory'));
  assert.deepEqual(inspectPublicPluginPackage(root), { schema:1, status:'blocked', reason:'public_source_path' });
  assert.deepEqual(inspectPublicPluginPackage(oldRoot, 'desktop-4239'), { schema:1, status:'blocked', reason:'public_source_path' });
  for (const profile of ['4239', 'desktop-4240', 'desktop-4239 ', null, {}]) {
    assert.throws(() => inspectPackage(root, profile), rejected('invalid_arguments'));
    assert.deepEqual(inspectPublicPluginPackage(root, profile), { schema:1, status:'blocked', reason:'invalid_arguments' });
  }
});

test('4239 targets retain link, unpacked, size and redirected-path guards', t => {
  const target = '.vite/build/early-bootstrap-199-e3316cddb4ea6a6b.js';
  for (const [value, status] of [[{link:'PRIVATE_TARGET'},'link_not_read'], [{unpacked:true,size:1},'unpacked_not_read']]) {
    const file = archive(t, pack(tree([[target,value]])));
    assert.equal(inspectArchive(file, false, 'desktop-4239').entries[2].status, status);
  }
  const file = archive(t, pack(tree([[target,{offset:'0',size:LIMITS.entry+1}]])));
  fs.truncateSync(file, fs.statSync(file).size + LIMITS.entry + 1);
  assert.equal(inspectArchive(file, false, 'desktop-4239').entries[2].status, 'entry_too_large');
  const root = path.join(fixture(t), 'OpenAI.Codex_26.1103.4239.0_x64__abcdefghijklm');
  fs.mkdirSync(path.join(root, 'app'), { recursive:true });
  try { fs.symlinkSync(fixture(t), path.join(root,'app','resources'), process.platform === 'win32' ? 'junction' : 'dir'); }
  catch (e) { if (['EPERM','EACCES','ENOTSUP'].includes(e.code)) { t.skip('OS did not permit synthetic redirect'); return; } throw e; }
  assert.throws(() => inspectPackage(root, 'desktop-4239'), rejected('package_path_redirected'));
});

test('CLI selection accepts only the explicit fixed 4239 profile', () => {
  assert.deepEqual(parsePackageArguments(['--package-root','fixture']), { packageRoot:'fixture', reviewedBuild:'' });
  assert.deepEqual(parsePackageArguments(['--package-root','fixture','--reviewed-build','desktop-4239']),
    { packageRoot:'fixture', reviewedBuild:'desktop-4239' });
  for (const args of [[], ['--package-root'], ['--package-root',''], ['--package-root','fixture','--reviewed-build',''],
    ['--package-root','fixture','--reviewed-build','desktop-4240'], ['--package-root','fixture','--reviewed-build','desktop-4239','extra'],
    ['--reviewed-build','desktop-4239','--package-root','fixture']]) assert.throws(() => parsePackageArguments(args), rejected('invalid_arguments'));
});
