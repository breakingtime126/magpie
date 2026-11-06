// Read-only static metadata for one pinned official Desktop package.
// ASAR format: electron/asar at b48633488d28655cd8ff8ac1f420b411d411e953:
// README.md ("Format"), src/disk.ts (readArchiveHeaderSync/readFileSync),
// and src/pickle.ts (readString/readUInt32 and four-byte alignment).
// https://github.com/electron/asar/tree/b48633488d28655cd8ff8ac1f420b411d411e953
// No package code is imported, evaluated, executed, extracted, or modified.
import fs from 'node:fs';
import { createHash } from 'node:crypto';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { analyzePublicPluginAST, PublicPluginASTLimit, PLUGIN_METHODS, AST_BLOCK_CODES, AST_SKIP_CODES } from './inspect-desktop-public-plugin-ast.mjs';

import { MCP_PATH_LIMIT, MCP_MANIFEST_SUFFIXES, publicPluginRoot, publicMCPPlan,
  publicMCPNames, publicMCPKey, parsePublicJSON } from './inspect-desktop-mcp-catalog.mjs';

export const TARGETS = Object.freeze([
  '.vite/build/early-bootstrap-339-fe39e0119f57b81e.js',
  '.vite/build/early-bootstrap-824-d144f7c160c094ea.js',
  '.vite/build/early-bootstrap-715-fa9cbf44c39465d5.js',
]);
export const LIMITS = Object.freeze({
  archive: 2 * 1024 ** 3, header: 32 * 1024 ** 2, entries: 100000,
  depth: 64, entry: 16 * 1024 ** 2, total: 32 * 1024 ** 2,
  method: 65536, tokens: 4000000, anchors: 128, candidates: 128,
});
const METHODS = ['getConfigOverrides', 'startupCommand'];
const ANCHORS = [...METHODS, 'code_mode_host'];
const BT = String.fromCharCode(96);

export class InspectionError extends Error {
  constructor(code) { super(code); this.code = code; }
}
function fail(code) { throw new InspectionError(code); }
function record(x) { return x !== null && typeof x === 'object' && !Array.isArray(x); }
function own(x, k) { return Object.prototype.hasOwnProperty.call(x, k); }
function codeOf(error) {
  return error instanceof InspectionError ? error.code : 'io_or_inspection_failed';
}
function decode(bytes) {
  try { return new TextDecoder('utf-8', { fatal: true }).decode(bytes); }
  catch { fail('invalid_utf8'); }
}
function readExact(fd, count, position) {
  const bytes = Buffer.alloc(count);
  let done = 0;
  while (done < count) {
    const n = fs.readSync(fd, bytes, done, count - done, position + done);
    if (n === 0) fail('truncated_archive');
    done += n;
  }
  return bytes;
}

// Only the documented two-Pickle layout is accepted. This intentionally does
// not support alternate Pickle headers, links, or reading unpacked resources.
export function readIndex(fd, size, includePublicSources = false) {
  if (!Number.isSafeInteger(size) || size < 16) fail('truncated_archive');
  if (size > LIMITS.archive) fail('archive_too_large');
  const prefix = readExact(fd, 16, 0);
  if (prefix.readUInt32LE(0) !== 4) fail('unsupported_asar_prefix');
  const headerSize = prefix.readUInt32LE(4);
  if (headerSize < 8 || headerSize % 4 !== 0) fail('invalid_header_size');
  if (headerSize > LIMITS.header) fail('header_too_large');
  if (headerSize > size - 8) fail('truncated_header');
  if (prefix.readUInt32LE(8) !== headerSize - 4) fail('invalid_header_pickle');
  const jsonSize = prefix.readInt32LE(12);
  if (jsonSize < 2 || 8 + Math.ceil(jsonSize / 4) * 4 !== headerSize) {
    fail('invalid_json_size');
  }
  const bytes = readExact(fd, headerSize - 8, 16);
  for (let i = jsonSize; i < bytes.length; i++) {
    if (bytes[i] !== 0) fail('invalid_header_padding');
  }
  let header;
  try { header = JSON.parse(decode(bytes.subarray(0, jsonSize))); }
  catch (error) {
    if (error instanceof InspectionError) throw error;
    fail('invalid_header_json');
  }
  if (!record(header) || !record(header.files)) fail('invalid_header_root');
  const selected = new Map();
  const publicSources = new Map();
  let publicRootBlocked = false;
  const stack = [{ node: header, relative: '', depth: 0, inheritedUnpacked: false }];
  let entries = 0;
  while (stack.length) {
    const { node, relative, depth, inheritedUnpacked } = stack.pop();
    if (!record(node)) fail('invalid_header_entry');
    if (own(node, 'unpacked') && typeof node.unpacked !== 'boolean') fail('invalid_unpacked_flag');
    const unpacked = inheritedUnpacked || node.unpacked === true;
    if (own(node, 'link')) {
      if (typeof node.link !== 'string' || node.link.length === 0) fail('invalid_link_entry');
      if (TARGETS.some(p => p === relative || p.startsWith(relative + '/'))) {
        selected.set(relative, { status: 'link_not_read' });
      }
      if (includePublicSources && (relative === '.vite' || relative === '.vite/build')) {
        publicRootBlocked = true;
      }
      if (includePublicSources && publicSourcePath(relative)) {
        publicSources.set(relative, { status: 'link_not_read' });
      }
      continue;
    }
    if (own(node, 'files')) {
      if (!record(node.files) || own(node, 'offset') || own(node, 'size')) fail('invalid_directory');
      if (depth >= LIMITS.depth) fail('header_too_deep');
      for (const [name, child] of Object.entries(node.files)) {
        entries++;
        if (entries > LIMITS.entries) fail('too_many_entries');
        if (name.length === 0 || name.length > 255 || name === '.' || name === '..' ||
            /[\/\\:\x00-\x1f\x7f]/.test(name) || /[. ]$/.test(name)) fail('invalid_entry_path');
        const childPath = relative ? relative + '/' + name : name;
        if (childPath.length > 1024) fail('entry_path_too_long');
        stack.push({ node: child, relative: childPath, depth: depth + 1, inheritedUnpacked: unpacked });
      }
      continue;
    }
    if (!Number.isSafeInteger(node.size) || node.size < 0) fail('invalid_file_size');
    if (unpacked) {
      if (TARGETS.includes(relative)) selected.set(relative, { status: 'unpacked_not_read' });
      if (includePublicSources && publicSourcePath(relative)) publicSources.set(relative, { status: 'unpacked_not_read' });
      continue;
    }
    if (typeof node.offset !== 'string' || !/^(0|[1-9][0-9]{0,19})$/.test(node.offset)) {
      fail('invalid_file_offset');
    }
    const offset = BigInt(node.offset);
    const start = BigInt(8 + headerSize) + offset;
    if (start + BigInt(node.size) > BigInt(size)) fail('file_outside_archive');
    if (TARGETS.includes(relative)) {
      selected.set(relative, { status: 'packed', start: Number(start), size: node.size });
    }
    if (includePublicSources && publicSourcePath(relative)) {
      publicSources.set(relative, { status: 'packed', start: Number(start), size: node.size });
    }
  }
  for (const target of TARGETS) {
    for (const [relative, entry] of selected) {
      if (entry.status === 'link_not_read' && target.startsWith(relative + '/')) {
        selected.set(target, { status: 'link_not_read' });
      }
    }
  }
  return { selected, entries, publicSources, publicRootBlocked };
}

// A conservative lexical reader, not a JavaScript parser. It skips comments,
// quoted strings, regex literals and whole template expressions. Ambiguous
// slash syntax fails closed. Only complete supported method definitions and
// direct adjacent "-c", "literal.key=..." forms create candidates.
class Lexer {
  constructor(source, start = 0, end = source.length) {
    this.source = source; this.i = start; this.end = end; this.previous = null;
    this.parens = []; this.count = 0; this.nesting = 0; this.previous2 = null;
  }
  next() {
    if (++this.count > LIMITS.tokens) fail('token_limit');
    const s = this.source;
    while (this.i < this.end) {
      if (/\s/.test(s[this.i])) { this.i++; continue; }
      if (s.startsWith('//', this.i)) {
        const end = s.slice(this.i + 2, this.end).search(/[\r\n\u2028\u2029]/);
        this.i = end < 0 ? this.end : this.i + 3 + end; continue;
      }
      // File mode is not proven module-only. Annex B HTML comments are not
      // supported; their text must never become literal identity evidence.
      const closingHTMLComment = s.startsWith('-->', this.i) && (!this.previous ||
        /[\r\n\u2028\u2029]/.test(s.slice(this.previous.end, this.i)));
      if (s.startsWith('<!--', this.i) || closingHTMLComment) fail('unsupported_lexical_syntax');
      if (s.startsWith('/*', this.i)) {
        const end = s.indexOf('*/', this.i + 2);
        if (end < 0 || end + 2 > this.end) fail('unterminated_comment');
        this.i = end + 2; continue;
      }
      break;
    }
    if (this.i >= this.end) return null;
    const start = this.i, c = s[this.i], previous = this.previous;
    let kind = 'punct', value = c, dynamic = false, controlClose = false;
    if (c === '"' || c === "'") {
      kind = 'literal'; this.i++;
      let closed = false;
      while (this.i < this.end) {
        if (s[this.i] === '\\') { this.i += 2; continue; }
        if (s[this.i] === c) { this.i++; closed = true; break; }
        if (s[this.i] === '\n' || s[this.i] === '\r') fail('unterminated_string');
        this.i++;
      }
      if (!closed) fail('unterminated_string');
      value = s.slice(start + 1, this.i - 1);
    } else if (c === BT) {
      kind = 'template'; this.i++;
      let firstEnd = -1, closed = false;
      while (this.i < this.end) {
        if (s[this.i] === '\\') { this.i += 2; continue; }
        if (s[this.i] === BT) { if (firstEnd < 0) firstEnd = this.i; this.i++; closed = true; break; }
        if (s[this.i] === '$' && s[this.i + 1] === '{') {
          if (firstEnd < 0) firstEnd = this.i;
          dynamic = true; this.i += 2;
          if (++this.nesting > 32) fail('template_depth_limit');
          const outerPrevious = this.previous, outerParens = this.parens, outerPrevious2 = this.previous2;
          this.previous = null; this.previous2 = null; this.parens = [];
          let depth = 1;
          while (depth) {
            const t = this.next();
            if (!t) fail('unterminated_template');
            if (t.kind === 'punct' && t.value === '{') depth++;
            if (t.kind === 'punct' && t.value === '}') depth--;
          }
          this.previous = outerPrevious; this.previous2 = outerPrevious2; this.parens = outerParens; this.nesting--;
          continue;
        }
        this.i++;
      }
      if (!closed) fail('unterminated_template');
      value = s.slice(start + 1, firstEnd);
    } else if (/[A-Za-z_$]/.test(c) || c.charCodeAt(0) > 127 ||
               (c === '#' && /[A-Za-z_$]/.test(s[this.i + 1] ?? ''))) {
      kind = 'identifier'; this.i++;
      while (this.i < this.end && (/[A-Za-z0-9_$]/.test(s[this.i]) ||
        (s.charCodeAt(this.i) > 127 && !/\s/.test(s[this.i])))) this.i++;
      value = s.slice(start, this.i);
    } else if (/[0-9]/.test(c)) {
      kind = 'number'; this.i++;
      while (this.i < this.end && /[A-Za-z0-9_.]/.test(s[this.i])) this.i++;
      value = '';
    } else if (c === '/') {
      // A right brace may end a block or an expression. Do not guess.
      if (previous?.kind === 'punct' && previous.value === '}') fail('ambiguous_slash');
      const keyword = previous?.kind === 'identifier' &&
        /^(return|throw|case|delete|void|typeof|new|in|of|instanceof|yield|await|else|do|default|break|continue|debugger|extends)$/.test(previous.value);
      const expressionEnd = previous && !keyword && !previous.controlClose &&
        (['identifier', 'number', 'literal', 'template', 'regex'].includes(previous.kind) ||
         [')', ']', '++', '--'].includes(previous.value));
      if (expressionEnd && /[\r\n\u2028\u2029]/.test(s.slice(previous.end, start))) fail('ambiguous_slash');
      if (expressionEnd) {
        this.i += s[this.i + 1] === '=' ? 2 : 1; value = '/';
      } else {
        kind = 'regex'; this.i++; let inClass = false, closed = false;
        while (this.i < this.end) {
          const r = s[this.i++];
          if (r === '\\') { this.i++; continue; }
          if (/[\r\n\u2028\u2029]/.test(r)) fail('unterminated_regex');
          if (r === '[') {
            if (inClass) fail('unsupported_lexical_syntax');
            inClass = true;
          }
          if (r === ']') inClass = false;
          if (r === '/' && !inClass) { closed = true; break; }
        }
        if (!closed) fail('unterminated_regex');
        const flagsStart = this.i;
        while (this.i < this.end && /[A-Za-z]/.test(s[this.i])) this.i++;
        if (s.slice(flagsStart, this.i).includes('v')) fail('unsupported_lexical_syntax');
        value = '';
      }
    } else {
      const op = ['===', '!==', '...', '=>', '==', '!=', '&&', '||', '??', '?.', '++', '--',
        '**', '+=', '-=', '*=', '<=', '>='].find(op => s.startsWith(op, this.i));
      value = op ?? c; this.i += value.length;
      if (value === '(') this.parens.push(previous?.kind === 'identifier' &&
        (/^(if|for|while|switch|catch|with)$/.test(previous.value) ||
         (previous.value === 'await' && this.previous2?.value === 'for')));
      if (value === ')') controlClose = this.parens.pop() === true;
      if (c === '\\' || c === '#') fail('unsupported_lexical_syntax');
    }
    const token = { kind, value, start, end: this.i, dynamic, controlClose };
    this.previous2 = previous; this.previous = token;
    return token;
  }
}
function literal(token) { return token && (token.kind === 'literal' || token.kind === 'template'); }
function candidate(token, after) {
  if (!literal(token)) return null;
  const match = /^([a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*){0,8})\s*=(?![=>])/.exec(token.value);
  if (!match || match[1].length > 128) return null;
  const rest = token.value.slice(match[0].length).trim();
  let type = 'static_other';
  if (token.dynamic || after?.value === '+') type = 'dynamic_expression';
  else if (/^(true|false)$/.test(rest)) type = 'boolean_literal';
  else if (/^[+-]?(?:[0-9]+(?:\.[0-9]+)?)(?:[eE][+-]?[0-9]+)?$/.test(rest)) type = 'number_literal';
  else if (/^["']/.test(rest) || /^\\["']/.test(rest)) type = 'string_shaped_literal';
  else if (rest === '') type = 'empty_literal';
  return { key: match[1], type };
}
function occurrences(source, needle) {
  let count = 0, at = 0;
  while ((at = source.indexOf(needle, at)) !== -1) { count++; at += needle.length; }
  return count;
}

export function analyzeSource(source) {
  const rawOccurrences = Object.fromEntries(ANCHORS.map(a => [a, occurrences(source, a)]));
  const anchors = [];
  const direct = [];
  const lexer = new Lexer(source);
  const prior = [];
  let lexicalStatus = 'complete', candidatesLimited = false;
  try {
    for (let t; (t = lexer.next());) {
      if (t.kind === 'identifier' && METHODS.includes(t.value)) {
        if (anchors.length >= LIMITS.anchors) fail('anchor_limit');
        anchors.push(t);
      }
      // Delay one token so concatenated values are classified as dynamic.
      if (prior.length === 4 && literal(prior[1]) && prior[1].value === '-c' &&
          prior[2].value === ',' && ['[', '(', ','].includes(prior[0].value)) {
        const c = candidate(prior[3], t);
        if (c) {
          if (direct.length >= LIMITS.candidates) fail('candidate_limit');
          direct.push({ ...c, evidence: 'adjacent_c_literal', method: null });
        }
      }
      prior.push(t); if (prior.length > 4) prior.shift();
    }
  } catch (error) { lexicalStatus = codeOf(error); candidatesLimited = lexicalStatus === 'candidate_limit'; }

  const methods = Object.fromEntries(METHODS.map(name => [name, {
    lexicalOccurrences: anchors.filter(t => t.value === name).length,
    completeDefinitions: 0, unsupportedDefinitions: 0,
  }]));
  const found = lexicalStatus === 'complete' ? [...direct] : [];
  for (const anchor of lexicalStatus === 'complete' ? anchors : []) {
    const state = methods[anchor.value];
    const l = new Lexer(source, anchor.end, Math.min(source.length, anchor.end + LIMITS.method));
    try {
      let t = l.next();
      if (t?.value !== '(') continue;
      let depth = 1, signatureTokens = 0;
      while (depth) {
        t = l.next();
        if (!t || ++signatureTokens > 64) fail('unsupported_method_signature');
        if (t.kind === 'punct' && t.value === '(') depth++;
        if (t.kind === 'punct' && t.value === ')') depth--;
      }
      t = l.next();
      if (t?.value !== '{') continue; // A call/property reference is not a definition.
      depth = 1;
      const body = [];
      while (depth) {
        t = l.next();
        if (!t) fail('method_limit_or_truncated');
        if (t.kind === 'punct' && t.value === '{') depth++;
        if (t.kind === 'punct' && t.value === '}') depth--;
        if (depth) body.push(t);
      }
      const pending = [];
      for (let i = 0; i < body.length; i++) {
        const c = candidate(body[i], body[i + 1]);
        if (c) pending.push({ ...c, evidence: 'literal_assignment_in_method', method: anchor.value });
      }
      if (found.length + pending.length > LIMITS.candidates) {
        candidatesLimited = true;
      } else {
        found.push(...pending);
      }
      state.completeDefinitions++;
    } catch {
      state.unsupportedDefinitions++;
    }
  }
  const grouped = new Map();
  for (const c of found) {
    const identity = [c.key, c.type, c.evidence, c.method].join('|');
    if (grouped.has(identity)) grouped.get(identity).occurrences++;
    else grouped.set(identity, { ...c, occurrences: 1 });
  }
  return {
    status: 'static_candidates_only', lexicalStatus, rawOccurrences, methods,
    candidatesLimited, candidates: [...grouped.values()].sort((a, b) =>
      a.key.localeCompare(b.key) || a.evidence.localeCompare(b.evidence)),
  };
}


export const PUBLIC_PLUGIN_LIMITS = Object.freeze({
  jsFiles: 2048, entry: 16 * 1024 ** 2, total: 128 * 1024 ** 2,
  manifestFiles: 16, jsonFiles: 64, manifest: 256 * 1024, marketplaceDirectories: 64,
  sources: 64, candidates: 128, key: 128,
});
export const PUBLIC_PLUGIN_BLOCK_CODES = Object.freeze([
  'public_source_limit', 'public_source_not_packed', 'public_manifest_invalid',
  'public_manifest_limit', 'public_catalog_limit', 'public_source_path',
  'public_archive_changed', ...AST_BLOCK_CODES,
]);
const MARKETPLACE_SUFFIXES = Object.freeze([
  '.agents/plugins/marketplace.json', '.agents/plugins/api_marketplace.json',
  '.claude-plugin/marketplace.json', '.cursor-plugin/marketplace.json',
]);
const PUBLIC_SEGMENT = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const PUBLIC_ID = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}@[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;

function publicSourcePath(entry) {
  return /^\.vite\/build\/[A-Za-z0-9][A-Za-z0-9_.-]{0,190}\.js$/.test(entry);
}
function asciiOrder(a, b) { return a < b ? -1 : a > b ? 1 : 0; }
function sha256(bytes) { return createHash('sha256').update(bytes).digest('hex'); }
export function publicPluginKeysDigest(keys) { return sha256(keys.join('\n')); }

function publicKeysForIdentity(identity) {
  if (!PUBLIC_ID.test(identity)) return [];
  const keys = ['plugins.' + identity + '.enabled', 'plugins.' + identity + '.auto_provision'];
  if (keys.some(key => key.length > PUBLIC_PLUGIN_LIMITS.key)) fail('public_catalog_limit');
  return keys;
}

// Public-source extraction requires a complete, bounded AST. The legacy three
// fixed-entry analyzer above remains separate and is not the catalog parser.
function analyzePublicPluginMetadata(source) {
  try { return analyzePublicPluginAST(source); }
  catch (error) {
    if (error instanceof PublicPluginASTLimit) fail(error.code);
    throw error;
  }
}
export function analyzePublicPluginSource(source) {
  const { complete, candidates } = analyzePublicPluginMetadata(source);
  return { complete, candidates };
}

function hashDescriptor(fd, size) {
  const hash = createHash('sha256');
  for (let at = 0; at < size;) {
    const count = Math.min(1024 * 1024, size - at);
    hash.update(readExact(fd, count, at));
    at += count;
  }
  return hash.digest('hex');
}
function samePath(a, b) {
  return process.platform === 'win32' ? a.toLowerCase() === b.toLowerCase() : a === b;
}
function packageNode(root, relative, leafKind, optional = false) {
  let current = root;
  const parts = relative.split('/');
  for (let i = 0; i < parts.length; i++) {
    current = path.join(current, parts[i]);
    let stat;
    try { stat = fs.lstatSync(current); }
    catch (error) {
      if (optional && error.code === 'ENOENT') return null;
      throw error;
    }
    if (stat.isSymbolicLink() || !samePath(fs.realpathSync(current), current)) fail('public_source_path');
    const kind = i === parts.length - 1 ? leafKind : 'directory';
    if (kind === 'directory' ? !stat.isDirectory() : !stat.isFile()) fail('public_source_path');
  }
  return current;
}
function readPublicJSONFile(file, remainingSourceBytes) {
  if (!Number.isSafeInteger(remainingSourceBytes) || remainingSourceBytes < 0) fail('public_source_limit');
  const before = fs.lstatSync(file);
  if (!before.isFile() || before.isSymbolicLink()) fail('public_source_path');
  if (before.size > PUBLIC_PLUGIN_LIMITS.manifest) fail('public_manifest_limit');
  const fd = fs.openSync(file, 'r');
  try {
    const opened = fs.fstatSync(fd);
    if (!unchanged(before, opened)) fail('public_archive_changed');
    if (opened.size > remainingSourceBytes) fail('public_source_limit');
    const bytes = readExact(fd, opened.size, 0);
    if (!unchanged(opened, fs.fstatSync(fd)) || !unchanged(opened, fs.lstatSync(file))) {
      fail('public_archive_changed');
    }
    let value;
    try { value = parsePublicJSON(decode(bytes)); }
    catch (error) {
      if (error instanceof InspectionError) throw error;
      fail(error.message === 'public_manifest_limit' ? 'public_manifest_limit' : 'public_manifest_invalid');
    }
    return { size: bytes.length, hash: sha256(bytes), value };
  } finally { fs.closeSync(fd); }
}

function marketplaceData(result) {
  const manifest = result.value;
  if (!record(manifest) || typeof manifest.name !== 'string' || !PUBLIC_SEGMENT.test(manifest.name) ||
      !Array.isArray(manifest.plugins)) fail('public_manifest_invalid');
  if (manifest.plugins.length > PUBLIC_PLUGIN_LIMITS.candidates) fail('public_catalog_limit');
  const keys = [], names = new Set();
  for (const plugin of manifest.plugins) {
    if (!record(plugin) || typeof plugin.name !== 'string' || !PUBLIC_SEGMENT.test(plugin.name) ||
        names.has(plugin.name)) fail('public_manifest_invalid');
    names.add(plugin.name);
    keys.push(...publicKeysForIdentity(plugin.name + '@' + manifest.name));
  }
  return { ...result, keys, name: manifest.name, plugins: manifest.plugins };
}
export function readPublicManifest(file, remainingSourceBytes) {
  return marketplaceData(readPublicJSONFile(file, remainingSourceBytes));
}

// The caller separately verifies each external manifest and app.asar against
// the exact original signed MSIX entry before publishing or using this report.
// This function reads only bounded native-recognized public declarations and
// validated local JSON references. It never loads code, follows links or reads
// user state. Declarations do not establish native selection or loadability.
export function inspectPublicPluginPackage(packageRoot) {
  try {
    if (typeof packageRoot !== 'string' || !path.isAbsolute(packageRoot) ||
        !/^OpenAI\.Codex_26\.1103\.3517\.0_x64__[a-z0-9]{13}$/i.test(path.basename(packageRoot))) {
      fail('public_source_path');
    }
    const root = path.resolve(packageRoot);
    if (!samePath(fs.realpathSync(root), root) || fs.lstatSync(root).isSymbolicLink()) {
      fail('public_source_path');
    }
    const archive = packageNode(root, 'app/resources/app.asar', 'file');
    const before = fs.lstatSync(archive), fd = fs.openSync(archive, 'r');
    const sources = [], candidates = new Map(), sourceIndices = new Map();
    const jsonCache = new Map(), jsonCasePaths = new Map();
    const manifestAnalysis = { pluginEntries: 0, pluginManifests: 0, mcpFiles: 0, skippedEntries: 0 };
    const analysis = {
      parser: 'acorn_8_15_0',
      methods: PLUGIN_METHODS.map(name => ({ name, definitions: 0, unresolved: 0 })),
      constructions: [], skipped: [],
    };
    const skipped = new Map();
    const coverage = { jsFiles: 0, jsComplete: 0, jsSkipped: 0, sourceBytes: 0, manifestFiles: 0 };
    function skipFile(reason) {
      coverage.jsSkipped++;
      skipped.set(reason, (skipped.get(reason) ?? 0) + 1);
    }
    function ensureSource(kind, sourcePath, hash) {
      if (sourceIndices.has(sourcePath)) {
        const index = sourceIndices.get(sourcePath), old = sources[index];
        if (old.kind !== kind || old.sha256 !== hash) fail('public_archive_changed');
        return index;
      }
      if (sources.length >= PUBLIC_PLUGIN_LIMITS.sources) fail('public_catalog_limit');
      const index = sources.length;
      sources.push({ kind, path: sourcePath, sha256: hash });
      sourceIndices.set(sourcePath, index);
      return index;
    }
    function addSource(kind, sourcePath, hash, found, constructions = []) {
      const fresh = found.filter(item => !candidates.has(item.key));
      if (fresh.length === 0 && constructions.length === 0) return;
      if (candidates.size + fresh.length > PUBLIC_PLUGIN_LIMITS.candidates) fail('public_catalog_limit');
      const sourceIndex = ensureSource(kind, sourcePath, hash);
      for (const item of fresh) candidates.set(item.key, { ...item, source: sourceIndex });
      if (analysis.constructions.length + constructions.length > 128) fail('public_catalog_limit');
      for (const item of constructions) analysis.constructions.push({ source: sourceIndex, ...item });
    }
    function readJSON(relative, kind) {
      if (relative.length > MCP_PATH_LIMIT) fail('public_source_path');
      const folded = relative.toLowerCase();
      if (jsonCasePaths.has(folded) && jsonCasePaths.get(folded) !== relative) fail('public_source_path');
      jsonCasePaths.set(folded, relative);
      if (jsonCache.has(relative)) {
        const cached = jsonCache.get(relative);
        if (cached && cached.kind !== kind) fail('public_source_path');
        return cached;
      }
      const file = packageNode(root, relative, 'file', true);
      if (!file) { jsonCache.set(relative, null); return null; }
      const count = coverage.manifestFiles + manifestAnalysis.pluginManifests + manifestAnalysis.mcpFiles;
      if (count >= PUBLIC_PLUGIN_LIMITS.jsonFiles) fail('public_manifest_limit');
      if (kind === 'marketplace_manifest') {
        if (++coverage.manifestFiles > PUBLIC_PLUGIN_LIMITS.manifestFiles) fail('public_manifest_limit');
      } else if (kind === 'plugin_manifest') manifestAnalysis.pluginManifests++;
      else manifestAnalysis.mcpFiles++;
      const result = { ...readPublicJSONFile(file, PUBLIC_PLUGIN_LIMITS.total - coverage.sourceBytes), kind };
      coverage.sourceBytes += result.size;
      jsonCache.set(relative, result);
      return result;
    }
    function addMCPDeclarations(marketRelative, marketplacePath, market) {
      for (const plugin of market.plugins) {
        if (++manifestAnalysis.pluginEntries > 2048) fail('public_manifest_limit');
        let omitted = false, manifestFound = false;
        const pluginRoot = publicPluginRoot(marketRelative, plugin.source,
          marketplacePath.endsWith('/.cursor-plugin/marketplace.json'));
        if (!pluginRoot || !packageNode(root, pluginRoot, 'directory', true)) {
          manifestAnalysis.skippedEntries++; continue;
        }
        for (const suffix of MCP_MANIFEST_SUFFIXES) {
          const pluginPath = pluginRoot + '/' + suffix;
          if (pluginPath.length > MCP_PATH_LIMIT) { omitted = true; continue; }
          const manifest = readJSON(pluginPath, 'plugin_manifest');
          if (!manifest) continue;
          manifestFound = true;
          const plan = publicMCPPlan(suffix, manifest.value);
          if (!plan) { omitted = true; continue; }
          const mcpPath = plan.inline ? pluginPath : pluginRoot + '/' + plan.reference;
          if (mcpPath.length > MCP_PATH_LIMIT) { omitted = true; continue; }
          const mcp = plan.inline ? manifest : readJSON(mcpPath, 'mcp_manifest');
          if (!mcp) { omitted = true; continue; }
          let declaration;
          try { declaration = publicMCPNames(plan.inline ?? mcp.value, plan.agent); }
          catch (error) {
            if (error.message === 'public_catalog_limit') fail('public_catalog_limit');
            throw error;
          }
          if (!declaration) { omitted = true; continue; }
          if (declaration.skipped) omitted = true;
          for (const server of declaration.names) {
            const key = publicMCPKey(plugin.name + '@' + market.name, server);
            if (!key) { omitted = true; continue; }
            if (candidates.has(key)) continue;
            if (candidates.size >= PUBLIC_PLUGIN_LIMITS.candidates) fail('public_catalog_limit');
            const marketplaceSource = ensureSource('marketplace_manifest', marketplacePath, market.hash);
            const pluginSource = ensureSource('plugin_manifest', pluginPath, manifest.hash);
            const source = plan.inline ? pluginSource : ensureSource('mcp_manifest', mcpPath, mcp.hash);
            candidates.set(key, { key, derivation: plan.derivation, source, marketplaceSource, pluginSource });
          }
        }
        if (!manifestFound || omitted) manifestAnalysis.skippedEntries++;
      }
    }
    let archiveSha256;
    try {
      const opened = fs.fstatSync(fd);
      if (!unchanged(before, opened)) fail('public_archive_changed');
      const index = readIndex(fd, opened.size, true);
      if (index.publicRootBlocked) fail('public_source_not_packed');
      const entries = [...index.publicSources].sort(([a], [b]) => asciiOrder(a, b));
      coverage.jsFiles = entries.length;
      if (entries.length > PUBLIC_PLUGIN_LIMITS.jsFiles) fail('public_source_limit');
      archiveSha256 = hashDescriptor(fd, opened.size);
      for (const [entry, info] of entries) {
        if (info.status !== 'packed') fail('public_source_not_packed');
        if (info.size > PUBLIC_PLUGIN_LIMITS.entry ||
            coverage.sourceBytes + info.size > PUBLIC_PLUGIN_LIMITS.total) fail('public_source_limit');
        const bytes = readExact(fd, info.size, info.start);
        coverage.sourceBytes += bytes.length;
        let analyzed;
        try { analyzed = analyzePublicPluginMetadata(decode(bytes)); }
        catch (error) {
          // Only measured file-local guards may omit a file. No contribution
          // has been committed yet; all I/O, global and unknown errors block.
          if (error instanceof InspectionError && AST_SKIP_CODES.includes(error.code)) {
            skipFile(error.code);
            continue;
          }
          throw error;
        }
        if (!analyzed.complete) { skipFile('syntax_error'); continue; }
        coverage.jsComplete++;
        for (let i = 0; i < analysis.methods.length; i++) {
          analysis.methods[i].definitions += analyzed.methods[i].definitions;
          analysis.methods[i].unresolved += analyzed.methods[i].unresolved;
        }
        addSource('asar_js', entry, sha256(bytes), analyzed.candidates, analyzed.constructions);
        for (const total of [
          analysis.methods.reduce((sum, item) => sum + item.definitions, 0),
          analysis.methods.reduce((sum, item) => sum + item.unresolved, 0),
          analysis.constructions.reduce((sum, item) => sum + item.count, 0),
        ]) if (total > LIMITS.tokens) fail('public_source_limit');
      }
      if (!unchanged(opened, fs.fstatSync(fd)) || !unchanged(opened, fs.lstatSync(archive))) {
        fail('public_archive_changed');
      }
    } finally { fs.closeSync(fd); }

    const marketplacesRoot = packageNode(root, 'app/resources/plugins', 'directory', true);
    if (marketplacesRoot) {
      const directories = fs.readdirSync(marketplacesRoot, { withFileTypes: true })
        .sort((a, b) => asciiOrder(a.name, b.name));
      if (directories.length > PUBLIC_PLUGIN_LIMITS.marketplaceDirectories) fail('public_manifest_limit');
      for (const directory of directories) {
        if (!PUBLIC_SEGMENT.test(directory.name) || directory.isFile()) continue;
        const marketRelative = 'app/resources/plugins/' + directory.name;
        const marketRoot = packageNode(root, marketRelative, 'directory');
        if (!marketRoot) fail('public_source_path');
        for (const suffix of MARKETPLACE_SUFFIXES) {
          const relative = marketRelative + '/' + suffix;
          const raw = readJSON(relative, 'marketplace_manifest');
          if (!raw) continue;
          const manifest = marketplaceData(raw);
          addSource('marketplace_manifest', relative, manifest.hash,
            manifest.keys.map(key => ({ key, derivation: 'manifest_identity' })));
          addMCPDeclarations(marketRelative, relative, manifest);
        }
      }
    }
    analysis.skipped = ['syntax_error', ...AST_SKIP_CODES]
      .filter(reason => skipped.has(reason))
      .map(reason => ({ reason, count: skipped.get(reason) }));
    const ordered = [...candidates.values()].sort((a, b) => asciiOrder(a.key, b.key));
    return {
      schema: 1, status: coverage.jsSkipped ? 'partial' : 'complete',
      archiveSha256, keysSha256: publicPluginKeysDigest(ordered.map(item => item.key)),
      coverage, sources, candidates: ordered, analysis, manifestAnalysis,
    };
  } catch (error) {
    return { schema: 1, status: 'blocked', reason: codeOf(error) };
  }
}

function unchanged(a, b) {
  return a.dev === b.dev && a.ino === b.ino && a.size === b.size && a.mtimeMs === b.mtimeMs;
}
export function inspectArchive(archivePath, includeSnapshot = false) {
  const before = fs.lstatSync(archivePath);
  if (before.isSymbolicLink() || !before.isFile()) fail('archive_not_regular');
  const fd = fs.openSync(archivePath, 'r');
  try {
    const opened = fs.fstatSync(fd);
    if (!unchanged(before, opened)) fail('archive_changed');
    const index = readIndex(fd, opened.size);
    let total = 0;
    const entries = TARGETS.map(entry => {
      const info = index.selected.get(entry);
      if (!info) return { entry, status: 'missing_entry' };
      if (info.status !== 'packed') return { entry, status: info.status };
      if (info.size > LIMITS.entry) return { entry, status: 'entry_too_large' };
      total += info.size;
      if (total > LIMITS.total) return { entry, status: 'source_total_limit' };
      try { return { entry, ...analyzeSource(decode(readExact(fd, info.size, info.start))) }; }
      catch (error) { return { entry, status: codeOf(error) }; }
    });
    const archiveSha256 = includeSnapshot ? hashDescriptor(fd, opened.size) : null;
    if (!unchanged(opened, fs.fstatSync(fd)) || !unchanged(opened, fs.lstatSync(archivePath))) {
      fail('archive_changed');
    }
    const report = { schema: 1, status: 'static_candidates_only', runtimeKeyProven: false, archiveEntries: index.entries, entries };
    return includeSnapshot ? { report, archiveSha256 } : report;
  } finally { fs.closeSync(fd); }
}

// Call only with Get-AppxPackage's InstallLocation after the existing
// test-desktop-cold-start.ps1 verifies the downloaded hash/signature and the
// registered name/version/publisher/status. This reader is NOT a signer verifier.
export function inspectPackage(packageRoot) {
  if (typeof packageRoot !== 'string' || !path.isAbsolute(packageRoot)) fail('package_root_not_absolute');
  if (!/^OpenAI\.Codex_26\.1103\.3517\.0_x64__[a-z0-9]{13}$/i.test(path.basename(packageRoot))) {
    fail('unexpected_package_directory');
  }
  const root = path.resolve(packageRoot);
  const equalPath = (a, b) => process.platform === 'win32' ? a.toLowerCase() === b.toLowerCase() : a === b;
  if (!equalPath(fs.realpathSync(root), root) || fs.lstatSync(root).isSymbolicLink()) {
    fail('package_root_redirected');
  }
  let current = root;
  for (const part of ['app', 'resources', 'app.asar']) {
    current = path.join(current, part);
    let stat;
    try { stat = fs.lstatSync(current); }
    catch (error) {
      if (error.code === 'ENOENT') fail(part === 'app.asar' ? 'archive_missing' : 'package_path_missing');
      throw error;
    }
    if (stat.isSymbolicLink() || !equalPath(fs.realpathSync(current), current)) fail('package_path_redirected');
    if (part !== 'app.asar' && !stat.isDirectory()) fail('package_path_not_directory');
  }
  const legacy = inspectArchive(current, true);
  const publicPlugins = inspectPublicPluginPackage(root);
  // Only successful public reports are eligible for the harness's package-hash
  // verification. Bind their legacy metadata to the exact same archive bytes.
  if (publicPlugins.status !== 'blocked' && legacy.archiveSha256 !== publicPlugins.archiveSha256) {
    fail('archive_changed');
  }
  return { ...legacy.report, publicPlugins };
}
function main() {
  try {
    if (process.platform !== 'win32' || process.env.GITHUB_ACTIONS !== 'true' ||
        process.env.RUNNER_OS !== 'Windows') fail('disposable_windows_ci_required');
    if (process.argv.length !== 4 || process.argv[2] !== '--package-root') fail('invalid_arguments');
    const report = inspectPackage(process.argv[3]);
    const text = JSON.stringify(report);
    if (Buffer.byteLength(text) > 65536) fail('report_limit');
    process.stdout.write('DESKTOP_STATIC_METADATA=' + text + '\n');
  } catch (error) {
    // Do not print exception messages/stacks, argv, absolute paths or source.
    process.stdout.write('DESKTOP_STATIC_METADATA=' + JSON.stringify({
      schema: 1, status: 'blocked', reason: codeOf(error),
    }) + '\n');
    process.exitCode = 2;
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
