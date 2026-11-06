import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { analyzePublicPluginAST } from './inspect-desktop-public-plugin-ast.mjs';
import { inspectPublicPluginPackage } from './inspect-desktop-static-metadata.mjs';

// Both public APIs existed at af097bf4336559cb468ffb72b7458458845f2a9b.
test('explicit AST guard preserves its fixed reason', () => {
  let failure;
  try { analyzePublicPluginAST('const x=' + Array(600).fill('1').join('+') + ';'); }
  catch (error) { failure = error; }
  assert.equal(failure?.code, 'public_ast_parse_depth_limit',
    'explicit parse-depth guard must retain its fixed resource reason');
});

test('limited source preserves independent public evidence', t => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-ast-limit-red-'));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  const root = path.join(home, 'OpenAI.Codex_26.1103.3517.0_x64__abcdefghijklm');
  const resources = path.join(root, 'app', 'resources');
  fs.mkdirSync(resources, { recursive: true });
  const contents = [
    'function getConfigOverrides(){return "plugins.DISCARDED@market.enabled=true"}' +
      'const x=root' + '.field'.repeat(600) + ';',
    'const id="retained@market";',
  ].map(value => Buffer.from(value));
  const header = { files: { '.vite': { files: { build: { files: {
    'a-limited.js': { offset: '0', size: contents[0].length },
    'b-complete.js': { offset: String(contents[0].length), size: contents[1].length },
  } } } } } };
  const json = Buffer.from(JSON.stringify(header));
  const padding = Buffer.alloc((4 - json.length % 4) % 4);
  const prefix = Buffer.alloc(16), size = 8 + json.length + padding.length;
  prefix.writeUInt32LE(4, 0); prefix.writeUInt32LE(size, 4);
  prefix.writeUInt32LE(size - 4, 8); prefix.writeInt32LE(json.length, 12);
  fs.writeFileSync(path.join(resources, 'app.asar'), Buffer.concat([prefix, json, padding, ...contents]));
  const market = path.join(resources, 'plugins', 'market', '.agents', 'plugins');
  fs.mkdirSync(market, { recursive: true });
  fs.writeFileSync(path.join(market, 'marketplace.json'),
    JSON.stringify({ name: 'market', plugins: [{ name: 'manifest' }] }));
  const report = inspectPublicPluginPackage(root);
  assert.equal(report.status, 'partial',
    'a file-local AST limit must preserve only complete independent public evidence');
  assert.equal(report.coverage.jsComplete, 1);
  assert.equal(report.coverage.jsSkipped, 1);
  assert.deepEqual(report.analysis.skipped, [{ reason: 'public_ast_walk_depth_limit', count: 1 }]);
  assert.ok(report.analysis.methods.every(item => item.definitions === 0 && item.unresolved === 0));
  assert.deepEqual(report.analysis.constructions, []);
  assert.deepEqual(report.candidates.map(item => item.key), [
    'plugins.manifest@market.auto_provision', 'plugins.manifest@market.enabled',
    'plugins.retained@market.auto_provision', 'plugins.retained@market.enabled',
  ]);
  assert.equal(report.sources.length, 2);
  assert.ok(!JSON.stringify(report).includes('DISCARDED'));
  assert.ok(!report.sources.some(item => item.path.includes('a-limited')));
});
