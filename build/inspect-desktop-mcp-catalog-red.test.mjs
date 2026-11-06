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

test('public MCP declarations produce source-bound nested candidates', t => {
  const f = setup(t);
  const plugin = f.write(f.plugin + '/.codex-plugin/plugin.json', { name: 'different-namespace' });
  const mcp = f.write(f.plugin + '/.mcp.json', { mcpServers: {
    'public-server': { command: 'SYNTHETIC_PRIVATE_COMMAND', env: { SECRET: 'SYNTHETIC_PRIVATE_VALUE' } },
  } });
  const report = inspectPublicPluginPackage(f.root);
  const candidate = report.candidates?.find(item => item.key === KEY);
  assert.ok(candidate, 'source-bound nested MCP declaration key missing');
  assert.equal(candidate.derivation, 'legacy_mcp_default');
  assert.equal(report.sources[candidate.marketplaceSource].path, f.marketplace);
  assert.equal(report.sources[candidate.pluginSource].path, plugin);
  assert.equal(report.sources[candidate.source].path, mcp);
  assert.ok(!JSON.stringify(report).includes('SYNTHETIC_PRIVATE'));
});
