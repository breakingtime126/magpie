import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { inspectPublicPluginPackage } from './inspect-desktop-static-metadata.mjs';

function setup(t, source = './plugins/storage-directory') {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'magpie-mcp-catalog-'));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  const root = path.join(home, 'OpenAI.Codex_26.1103.3517.0_x64__abcdefghijklm');
  const resources = path.join(root, 'app/resources');
  fs.mkdirSync(resources, { recursive: true });
  const json = Buffer.from('{"files":{}}');
  const padding = Buffer.alloc((4 - json.length % 4) % 4);
  const prefix = Buffer.alloc(16), size = 8 + json.length + padding.length;
  prefix.writeUInt32LE(4, 0); prefix.writeUInt32LE(size, 4);
  prefix.writeUInt32LE(size - 4, 8); prefix.writeInt32LE(json.length, 12);
  fs.writeFileSync(path.join(resources, 'app.asar'), Buffer.concat([prefix, json, padding]));
  const market = 'app/resources/plugins/storage-market';
  const marketplace = market + '/.agents/plugins/marketplace.json';
  function write(relative, value) {
    const full = path.join(root, relative);
    fs.mkdirSync(path.dirname(full), { recursive: true });
    fs.writeFileSync(full, typeof value === 'string' || Buffer.isBuffer(value) ? value : JSON.stringify(value));
    return relative;
  }
  write(marketplace, { name: 'actual-market', plugins: [{ name: 'actual-plugin', source }] });
  return { root, market, marketplace, plugin: market + '/plugins/storage-directory', write };
}
const KEY = 'plugins.actual-plugin@actual-market.mcp_servers.public-server.enabled';

import {
  AGENT_PLUGIN_SCHEMA, AGENT_MCP_SCHEMA, parsePublicJSON, publicPluginRoot, publicMCPKey,
} from './inspect-desktop-mcp-catalog.mjs';

function nested(report) { return report.candidates?.filter(item => item.key.includes('.mcp_servers.')) ?? []; }
function blocked(report, reason) {
  assert.deepEqual(report, { schema: 1, status: 'blocked', reason });
}
test('each native declaration form binds exact identities and all source hashes', t => {
  for (const form of ['legacy_mcp_inline', 'legacy_mcp_default', 'legacy_mcp_path', 'agent_mcp_default']) {
    const f = setup(t, { source: 'local', path: './plugins/storage-directory' });
    const configuration = { 'public-server': { command: 'SYNTHETIC_PRIVATE_COMMAND', env: {
      SECRET: 'SYNTHETIC_PRIVATE_VALUE' }, url: 'https://SYNTHETIC_PRIVATE_URL' } };
    let plugin, mcp;
    if (form === 'agent_mcp_default') {
      plugin = f.write(f.plugin + '/plugin.json', { $schema: AGENT_PLUGIN_SCHEMA, name: 'not-config-identity' });
      mcp = f.write(f.plugin + '/mcp.json', { $schema: AGENT_MCP_SCHEMA, mcpServers: configuration });
    } else {
      const declaration = { name: 'not-config-identity' };
      if (form === 'legacy_mcp_inline') declaration.mcpServers = configuration;
      if (form === 'legacy_mcp_path') declaration.mcpServers = './config/declared.json';
      plugin = f.write(f.plugin + '/.codex-plugin/plugin.json', declaration);
      mcp = form === 'legacy_mcp_inline' ? plugin :
        f.write(f.plugin + (form === 'legacy_mcp_path' ? '/config/declared.json' : '/.mcp.json'),
          form === 'legacy_mcp_path' ? configuration : { mcpServers: configuration });
    }
    const report = inspectPublicPluginPackage(f.root);
    assert.equal(report.status, 'complete');
    assert.equal(nested(report).length, 1);
    const candidate = nested(report)[0];
    assert.equal(candidate.key, KEY); assert.equal(candidate.derivation, form);
    assert.equal(report.sources[candidate.marketplaceSource].path, f.marketplace);
    assert.equal(report.sources[candidate.pluginSource].path, plugin);
    assert.equal(report.sources[candidate.source].path, mcp);
    for (const source of report.sources) {
      const bytes = fs.readFileSync(path.join(f.root, source.path));
      assert.equal(source.sha256, createHash('sha256').update(bytes).digest('hex'));
    }
    assert.deepEqual(report.manifestAnalysis, {
      pluginEntries: 1, pluginManifests: 1, mcpFiles: form === 'legacy_mcp_inline' ? 0 : 1, skippedEntries: 0,
    });
    assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
    assert.ok(!JSON.stringify(report).includes('not-config-identity'));
  }
});
test('recognized locations enumerate declarations without claiming selected precedence', t => {
  const f = setup(t);
  for (const [i, suffix] of ['.codex-plugin/plugin.json', '.claude-plugin/plugin.json',
    '.cursor-plugin/plugin.json'].entries()) {
    f.write(f.plugin + '/' + suffix, { mcpServers: { ['server-' + i]: { command: 'ignored' } } });
  }
  const report = inspectPublicPluginPackage(f.root);
  assert.equal(nested(report).length, 3);
  assert.equal(report.manifestAnalysis.pluginManifests, 3);
  assert.equal(report.manifestAnalysis.skippedEntries, 0);
});
test('entry-source relationship never pairs other plugin IDs or plugin manifest names', t => {
  const f = setup(t);
  f.write(f.marketplace, { name: 'actual-market', plugins: [
    { name: 'actual-plugin', source: './plugins/storage-directory' },
    { name: 'other', source: './elsewhere' },
  ] });
  f.write(f.plugin + '/.codex-plugin/plugin.json', { name: 'unrelated-name', mcpServers: {
    'public-server': { command: 'ignored' },
  } });
  f.write(f.market + '/elsewhere/.codex-plugin/plugin.json', { mcpServers: {
    'second-server': { command: 'ignored' },
  } });
  const report = inspectPublicPluginPackage(f.root);
  assert.deepEqual(nested(report).map(item => item.key), [
    KEY, 'plugins.other@actual-market.mcp_servers.second-server.enabled',
  ]);
  assert.ok(!JSON.stringify(report).includes('unrelated-name'));
});
test('unsupported references do not read outside or invent candidates', t => {
  for (const source of ['../outside', '/absolute', 'C:/private', './a/../b', './folder.', './folder./child', './a\\b',
    { source: 'url', url: 'https://SYNTHETIC_PRIVATE_URL' }, './' + 'a/'.repeat(8) + 'b']) {
    const f = setup(t, source);
    const report = inspectPublicPluginPackage(f.root);
    assert.equal(report.status, 'complete');
    assert.deepEqual(nested(report), []);
    assert.equal(report.manifestAnalysis.skippedEntries, 1);
    assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
  }
  for (const reference of ['../private.json', '/private.json', './a/../private.json', './folder./private.json',
    './$HOME/private.json', './file%2ejson', './a\\private.json', './not-json.txt']) {
    const f = setup(t);
    f.write(f.plugin + '/.codex-plugin/plugin.json', { mcpServers: reference });
    const report = inspectPublicPluginPackage(f.root);
    assert.deepEqual(nested(report), []);
    assert.equal(report.manifestAnalysis.skippedEntries, 1);
    assert.equal(report.manifestAnalysis.mcpFiles, 0);
  }
});
test('root and Cursor local source resolution remain literal and bounded', () => {
  const root = 'app/resources/plugins/market';
  assert.equal(publicPluginRoot(root, '.'), root);
  assert.equal(publicPluginRoot(root, './'), root);
  assert.equal(publicPluginRoot(root, 'plugins/p'), null);
  assert.equal(publicPluginRoot(root, 'plugins/p', true), root + '/plugins/p');
  assert.equal(publicPluginRoot(root, './plugins/p'), root + '/plugins/p');
  assert.equal(publicPluginRoot(root, './folder.'), null);
  assert.equal(publicPluginRoot(root, './folder./child'), null);
  assert.equal(publicMCPKey('p@m', 's'.repeat(64)).length <= 128, true);
  assert.equal(publicMCPKey('p'.repeat(64) + '@m', 's'.repeat(64)), null);
  for (const server of ['s.x', 's@x', 's x', '"s"', 's\n', 's/other']) {
    assert.equal(publicMCPKey('p@m', server), null);
  }
});
test('unsupported Agent schemas never acquire legacy meaning', t => {
  for (const suffix of ['plugin.json', '.codex-plugin/plugin.json']) {
    const f = setup(t);
    f.write(f.plugin + '/' + suffix, { $schema: 'https://agent-plugins.org/schemas/9/plugin.schema.json',
      mcpServers: { 'public-server': { command: 'ignored' } } });
    const report = inspectPublicPluginPackage(f.root);
    assert.deepEqual(nested(report), []);
    assert.equal(report.manifestAnalysis.skippedEntries, 1);
  }
  const f = setup(t);
  f.write(f.plugin + '/plugin.json', { $schema: AGENT_PLUGIN_SCHEMA });
  f.write(f.plugin + '/mcp.json', { $schema: 'unsupported', mcpServers: { 'public-server': {} } });
  assert.deepEqual(nested(inspectPublicPluginPackage(f.root)), []);
});
test('missing and unsupported declarations expose only bounded incompleteness', t => {
  const f = setup(t);
  f.write(f.plugin + '/.codex-plugin/plugin.json', { mcpServers: {
    'public-server': { command: 'ignored' }, 'private.name': { token: 'SYNTHETIC_PRIVATE' }, invalid: 3,
  } });
  const report = inspectPublicPluginPackage(f.root);
  assert.equal(nested(report).length, 1);
  assert.equal(report.manifestAnalysis.skippedEntries, 1);
  assert.ok(!JSON.stringify(report).includes('private.name'));
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
});
test('malformed and duplicate JSON clear earlier independent declarations', t => {
  for (const invalid of ['{', '{"mcpServers":{},"mcpServers":{}}',
    '{"mcpServers":{"x":{},"\\u0078":{}}}', Buffer.from([255])]) {
    const f = setup(t);
    f.write(f.plugin + '/.codex-plugin/plugin.json', { mcpServers: { 'public-server': {} } });
    f.write(f.plugin + '/.claude-plugin/plugin.json', invalid);
    const report = inspectPublicPluginPackage(f.root);
    blocked(report, Buffer.isBuffer(invalid) ? 'invalid_utf8' : 'public_manifest_invalid');
  }
  const f = setup(t);
  f.write(f.marketplace, { name: 'actual-market', plugins: [
    { name: 'duplicate', source: '.' }, { name: 'duplicate', source: '.' },
  ] });
  blocked(inspectPublicPluginPackage(f.root), 'public_manifest_invalid');
});
test('duplicate-key scan is nested, escape-aware, bounded and accepts ordinary JSON', () => {
  assert.deepEqual(parsePublicJSON('{"a":[{"b":"comma, brace} quote\\\""}],"c":true}'),
    { a: [{ b: 'comma, brace} quote"' }], c: true });
  for (const text of ['{"a":1,"a":2}', '{"a":1,"\\u0061":2}', '{"a":[{"x":0,"x":1}]}']) {
    assert.throws(() => parsePublicJSON(text), /public_manifest_invalid/);
  }
  assert.throws(() => parsePublicJSON('['.repeat(65) + '0' + ']'.repeat(65)), /public_manifest_limit/);
});
test('manifest file bytes, map size and source count fail closed', t => {
  const f = setup(t);
  f.write(f.plugin + '/.codex-plugin/plugin.json', ' '.repeat(256 * 1024 + 1));
  blocked(inspectPublicPluginPackage(f.root), 'public_manifest_limit');
  const second = setup(t);
  second.write(second.plugin + '/.codex-plugin/plugin.json', { mcpServers:
    Object.fromEntries(Array.from({ length: 129 }, (_, i) => ['s' + i, {}])) });
  blocked(inspectPublicPluginPackage(second.root), 'public_catalog_limit');
  const third = setup(t);
  third.write(third.marketplace, { name: 'actual-market', plugins:
    Array.from({ length: 22 }, (_, i) => ({ name: 'p' + i, source: './p' + i })) });
  for (let i = 0; i < 22; i++) for (const suffix of ['.codex-plugin/plugin.json',
    '.claude-plugin/plugin.json', '.cursor-plugin/plugin.json']) {
    third.write(third.market + '/p' + i + '/' + suffix, { mcpServers: {} });
  }
  blocked(inspectPublicPluginPackage(third.root), 'public_manifest_limit');
});
test('symlinked and case-ambiguous declaration files never contribute', t => {
  const f = setup(t);
  f.write(f.plugin + '/.codex-plugin/plugin.json', { mcpServers: './config.json' });
  f.write(f.plugin + '/real.json', { mcpServers: { 'public-server': {} } });
  try { fs.symlinkSync(path.join(f.root, f.plugin, 'real.json'), path.join(f.root, f.plugin, 'config.json')); }
  catch (error) { if (error.code === 'EPERM') { t.skip('symlink permission unavailable'); return; } throw error; }
  blocked(inspectPublicPluginPackage(f.root), 'public_source_path');
});
