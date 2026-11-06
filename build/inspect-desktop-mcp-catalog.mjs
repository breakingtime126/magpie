// Source contract: openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9.
// core-plugins/src/marketplace.rs: resolve_marketplace_plugin_entry and
// resolve_local_plugin_source_path (identity and root-relative source).
// utils/plugins/src/plugin_namespace.rs: native-recognized manifest locations.
// core-plugins/src/manifest.rs: resolve_manifest_mcp_servers.
// core-plugins/src/loader.rs: plugin_mcp_config_paths and policy lookup.
// codex-mcp/src/plugin_config.rs and agent_plugin_config.rs: declaration maps.
// Paths above are beneath codex-rs/. This is a declaration superset, not native
// precedence, valid transport, selected/loaded server or authorization evidence.
// Bounded public declaration parsing only. Never execute or expose commands,
// environment, URLs, credentials, or arbitrary source values.
export const MCP_PATH_LIMIT = 512;
export const MCP_MANIFEST_SUFFIXES = Object.freeze([
  'plugin.json', '.codex-plugin/plugin.json',
  '.claude-plugin/plugin.json', '.cursor-plugin/plugin.json',
]);
export const AGENT_PLUGIN_SCHEMA = 'https://agent-plugins.org/schemas/1.0.0/plugin.schema.json';
export const AGENT_MCP_SCHEMA = 'https://agent-plugins.org/schemas/1.0.0/mcp.schema.json';
const SEGMENT = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const COMPONENT = /^[A-Za-z0-9_.-]{1,64}$/;
export const isRecord = value => value !== null && typeof value === 'object' && !Array.isArray(value);

export function safeRelative(value, allowRoot = false, allowBare = false) {
  if (typeof value !== 'string') return null;
  if (allowRoot && (value === '.' || value === './')) return '';
  if (value.startsWith('./')) value = value.slice(2);
  else if (!allowBare) return null;
  const parts = value.split('/');
  if (parts.length < 1 || parts.length > 8 ||
      parts.some(part => part === '.' || part === '..' || part.endsWith('.') || !COMPONENT.test(part))) return null;
  return value;
}
export function publicPluginRoot(marketRoot, source, cursor = false) {
  let value = source;
  if (isRecord(source)) {
    if (source.source !== 'local' || typeof source.path !== 'string') return null;
    value = source.path;
  }
  const relative = safeRelative(value, true, cursor);
  if (relative === null) return null;
  const root = marketRoot + (relative ? '/' + relative : '');
  return root.length <= MCP_PATH_LIMIT ? root : null;
}
export function publicMCPPlan(suffix, manifest) {
  if (!isRecord(manifest)) return null;
  if (suffix === 'plugin.json') {
    if (manifest.$schema !== AGENT_PLUGIN_SCHEMA) return null;
    return { derivation: 'agent_mcp_default', reference: 'mcp.json', agent: true };
  }
  // A legacy location never grants Agent Plugins semantics.
  if (typeof manifest.$schema === 'string' &&
      manifest.$schema.startsWith('https://agent-plugins.org/schemas/')) return null;
  if (!Object.hasOwn(manifest, 'mcpServers')) {
    return { derivation: 'legacy_mcp_default', reference: '.mcp.json', agent: false };
  }
  if (isRecord(manifest.mcpServers)) {
    return { derivation: 'legacy_mcp_inline', inline: manifest.mcpServers, agent: false };
  }
  const reference = safeRelative(manifest.mcpServers);
  if (reference === null || !reference.endsWith('.json')) return null;
  return { derivation: 'legacy_mcp_path', reference, agent: false };
}
export function publicMCPNames(value, agent = false) {
  if (!isRecord(value)) return null;
  let declarations = value;
  if (agent) {
    if (value.$schema !== AGENT_MCP_SCHEMA || !isRecord(value.mcpServers) ||
        Object.keys(value).some(key => key !== '$schema' && key !== 'mcpServers')) return null;
    declarations = value.mcpServers;
  } else if (Object.hasOwn(value, 'mcpServers')) {
    if (!isRecord(value.mcpServers)) return null;
    declarations = value.mcpServers;
  }
  const names = [], entries = Object.entries(declarations);
  if (entries.length > 128) throw new Error('public_catalog_limit');
  let skipped = false;
  for (const [name, configuration] of entries) {
    // Name evidence is not transport validation or proof of native loadability.
    if (!SEGMENT.test(name) || !isRecord(configuration)) { skipped = true; continue; }
    names.push(name);
  }
  return { names, skipped };
}
export function publicMCPKey(identity, server) {
  const parts = identity.split('@');
  if (parts.length !== 2 || parts.some(part => !SEGMENT.test(part)) || !SEGMENT.test(server)) return null;
  const key = 'plugins.' + identity + '.mcp_servers.' + server + '.enabled';
  return key.length <= 128 ? key : null;
}

// JSON.parse establishes grammar. This bounded structural pass rejects duplicate
// decoded object keys (including escape aliases) without exposing their spelling.
export function parsePublicJSON(text) {
  if (typeof text !== 'string') throw new Error('public_manifest_invalid');
  let value;
  try { value = JSON.parse(text); } catch { throw new Error('public_manifest_invalid'); }
  const stack = [];
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    if (char === '"') {
      const start = i++;
      for (; i < text.length; i++) {
        if (text[i] === '\\') { i++; continue; }
        if (text[i] === '"') break;
      }
      const top = stack.at(-1);
      if (top?.object && top.key) {
        const key = JSON.parse(text.slice(start, i + 1));
        if (top.keys.has(key)) throw new Error('public_manifest_invalid');
        top.keys.add(key); top.key = false;
      }
    } else if (char === '{' || char === '[') {
      if (stack.length >= 64) throw new Error('public_manifest_limit');
      stack.push({ object: char === '{', key: char === '{', keys: new Set() });
    } else if (char === '}' || char === ']') {
      stack.pop();
    } else if (char === ',' && stack.at(-1)?.object) {
      stack.at(-1).key = true;
    }
  }
  return value;
}
