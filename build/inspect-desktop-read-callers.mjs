// Test-only positive syntax facts from completely parsed, unexecuted Desktop source.
// This is NOT a transport, call-graph, runtime-value, path-authority or consumer-absence
// proof. Callers must bind source bytes to the original signed package before
// publishing. No imports, target files, package code or expressions are executed.
import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { readIndex } from './inspect-desktop-static-metadata.mjs';
import { Parser, version } from './vendor/acorn-8.15.0/acorn.mjs';

export const READ_CALL_LIMITS = Object.freeze({
  bytes: 16 * 1024 ** 2, totalBytes: 32 * 1024 ** 2, sources: 2048,
  tokens: 4000000, nodes: 2000000, reportBytes: 65536,
  depth: 512, milliseconds: 30000, calls: 128, localNodes: 20000, fieldsPerCall: 32,
});
export const READ_METHODS = Object.freeze([
  'config/read', 'experimentalFeature/list', 'project/list', 'fs/readFile',
]);
// These are public protocol/config field spellings, not arbitrary source names.
export const READ_RESPONSE_FIELDS = Object.freeze([
  'config', 'origins', 'layers', 'model', 'model_provider', 'model_providers', 'features',
  'data', 'nextCursor', 'id', 'name', 'roots', 'metadata', 'position',
  'createdAt', 'updatedAt', 'recencyAt', 'dataBase64',
]);
export const READ_PATH_MARKERS = Object.freeze([
  'config_toml', 'agents_md', 'skill_md', 'auth_json', 'dot_codex',
]);
export const READ_CALL_FAILURES = Object.freeze([
  'input_limit', 'token_limit', 'depth_limit', 'deadline', 'call_limit',
  'node_limit', 'local_node_limit', 'field_limit', 'syntax_error', 'analysis_failed',
  'source_limit', 'source_read_failed', 'archive_mismatch', 'archive_changed',
  'package_path', 'report_limit', 'invalid_arguments', 'disposable_windows_ci_required',
]);
class Bound extends Error {
  constructor(code) { super(code); this.code = code; }
}
function fail(code) { throw new Bound(code); }
function fn(node) {
  return node && ['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression',
    'ClassDeclaration', 'ClassExpression'].includes(node.type);
}
function property(node) {
  if (!node.computed && node.property?.type === 'Identifier') return node.property.name;
  if (node.computed && node.property?.type === 'Literal' && typeof node.property.value === 'string') return node.property.value;
  return null;
}
function key(node) {
  if (node.computed) return null;
  if (node.key?.type === 'Identifier') return node.key.name;
  if (node.key?.type === 'Literal' && typeof node.key.value === 'string') return node.key.value;
  return null;
}
function children(node) {
  const result = [];
  for (const [name, value] of Object.entries(node)) {
    if (['start', 'end', 'loc', 'raw', 'regex'].includes(name)) continue;
    for (const item of Array.isArray(value) ? value : [value]) {
      if (item && typeof item === 'object' && typeof item.type === 'string') result.push(item);
    }
  }
  return result;
}
function unwrap(node) {
  return node?.type === 'ChainExpression' ? node.expression : node;
}
function methodCall(node) {
  if (node?.type !== 'CallExpression' || node.optional || node.callee.type === 'Super') return null;
  const first = node.arguments[0];
  if (first?.type !== 'Literal' || !READ_METHODS.includes(first.value)) return null;
  return { method: first.value, anchor: first.start, call: node, params: node.arguments[1] };
}
function pathFacts(params, check) {
  const result = { pathSyntax: 'unresolved', pathMarkers: [], pathAuthorityProven: false };
  if (params?.type !== 'ObjectExpression') return result;
  const props = params.properties;
  if (props.some(p => p.type !== 'Property' || p.computed || p.kind !== 'init') ||
      props.filter(p => key(p) === 'path').length !== 1) return result;
  const node = props.find(p => key(p) === 'path').value;
  const kinds = { TemplateLiteral: 'template', CallExpression: 'call', Identifier: 'identifier',
    MemberExpression: 'member', BinaryExpression: 'binary', ConditionalExpression: 'conditional' };
  if (node.type === 'Literal' && typeof node.value === 'string') {
    result.pathSyntax = /^(?:[A-Za-z]:[\\/]|[\\/])/.test(node.value) ? 'literal_absolute' : 'literal_relative';
  } else result.pathSyntax = kinds[node.type] ?? 'other';
  const found = new Set(), stack = [node];
  while (stack.length) {
    check();
    const part = stack.pop();
    let value = null;
    if (part.type === 'Literal' && typeof part.value === 'string') value = part.value;
    if (part.type === 'TemplateElement') value = part.value.cooked;
    if (typeof value === 'string') {
      const segments = value.split(/[\\/]/);
      for (const [name, marker] of [['config.toml', 'config_toml'], ['AGENTS.md', 'agents_md'],
        ['SKILL.md', 'skill_md'], ['auth.json', 'auth_json'], ['.codex', 'dot_codex']]) {
        if (segments.includes(name)) found.add(marker);
      }
    }
    // Do not take markers from unrelated nested function bodies.
    if (!fn(part)) stack.push(...children(part));
  }
  result.pathMarkers = READ_PATH_MARKERS.filter(item => found.has(item));
  return result;
}
function readAccess(node, parents) {
  let current = node;
  for (let parent = parents.get(current); parent; parent = parents.get(current)) {
    if (parent.type === 'AssignmentExpression') {
      return !(parent.operator === '=' && parent.left === current);
    }
    if (parent.type === 'UnaryExpression' && parent.operator === 'delete' && parent.argument === current) return false;
    if (['ForOfStatement', 'ForInStatement'].includes(parent.type) && parent.left === current) return false;
    // Destructuring destinations are writes even through pattern containers.
    // A MemberExpression object prefix is not such a container: r.config is
    // still read to find the object when r.config.model is the write target.
    const target = parent.type === 'ChainExpression' && parent.expression === current ||
      parent.type === 'Property' && parent.value === current &&
        parents.get(parent)?.type === 'ObjectPattern' ||
      parent.type === 'ObjectPattern' && parent.properties.includes(current) ||
      parent.type === 'ArrayPattern' && parent.elements.includes(current) ||
      parent.type === 'AssignmentPattern' && parent.left === current ||
      parent.type === 'RestElement' && parent.argument === current;
    if (!target) return true;
    current = parent;
  }
  return true;
}
function responsePath(node, call, binding = null) {
  node = unwrap(node);
  if (binding && node?.type === 'Identifier' && node.name === binding.name) return binding.prefix;
  if (node?.type === 'AwaitExpression' && unwrap(node.argument) === call) return [];
  if (node?.type === 'MemberExpression') {
    const base = responsePath(node.object, call, binding), name = property(node);
    if (base && READ_RESPONSE_FIELDS.includes(name) && base.length < 3) return [...base, name];
  }
  return null;
}
function directBindings(pattern, prefix, out, check) {
  check();
  if (pattern?.type === 'Identifier') { out.push({ name: pattern.name, prefix }); return; }
  if (pattern?.type !== 'ObjectPattern') return;
  for (const p of pattern.properties) {
    if (p.type !== 'Property' || p.computed || p.kind !== 'init') continue;
    const name = key(p);
    if (!READ_RESPONSE_FIELDS.includes(name) || prefix.length >= 3) continue;
    directBindings(p.value, [...prefix, name], out, check);
  }
}
function analyzeCall(hit, parents, rows, checkDeadline, budget) {
    let localNodes = 0;
    const check = () => {
      checkDeadline();
      if (++localNodes > READ_CALL_LIMITS.localNodes) fail('local_node_limit');
      if (++budget.walked > READ_CALL_LIMITS.nodes) fail('node_limit');
    };
    const fields = new Map();
    function add(path, relation) {
      if (!path?.length) return;
      const id = relation + ':' + path.join('.');
      if (!fields.has(id) && fields.size >= READ_CALL_LIMITS.fieldsPerCall) fail('field_limit');
      fields.set(id, { relation, path });
    }
    // Only an immediately awaited call establishes a syntactic result root.
    let expression = hit.call, parent = parents.get(expression);
    if (parent?.type === 'ChainExpression') { expression = parent; parent = parents.get(parent); }
    if (parent?.type === 'AwaitExpression') {
      expression = parent;
      while (true) {
        parent = parents.get(expression);
        if (parent?.type === 'MemberExpression' && parent.object === expression) {
          expression = parent;
          if (readAccess(expression, parents)) add(responsePath(expression, hit.call), 'await_member');
        } else if (parent?.type === 'ChainExpression') expression = parent;
        else break;
      }
      // Restrict named references to a direct const declaration and sibling
      // simple statements in the SAME block. No nested scope, branch, loop,
      // callback, alias hop or cross-function/name-based resolution is attempted.
      if (parent?.type === 'VariableDeclarator' && parent.init === expression) {
        const declaration = parents.get(parent), block = parents.get(declaration);
        const prefix = responsePath(expression, hit.call);
        if (prefix && declaration?.type === 'VariableDeclaration' && declaration.kind === 'const' &&
            ['BlockStatement', 'Program'].includes(block?.type)) {
          const bindings = [];
          directBindings(parent.id, prefix, bindings, check);
          for (const binding of bindings) {
            add(binding.prefix, 'const_binding');
            for (const statement of block.body) {
              if (!['ExpressionStatement', 'ReturnStatement', 'ThrowStatement', 'VariableDeclaration'].includes(statement.type)) continue;
              const pending = [{ node: statement, parent: block }];
              while (pending.length) {
                check();
                const { node: item, parent: owner } = pending.pop();
                parents.set(item, owner);
                if (fn(item) || item.type === 'BlockStatement') continue;
                if (item.type === 'MemberExpression' && readAccess(item, parents)) add(responsePath(item, hit.call, binding), 'const_binding');
                for (const child of children(item)) pending.push({ node: child, parent: item });
              }
            }
          }
        }
      }
    }
    rows.push({
      method: hit.method, offsetUtf16: hit.anchor, relation: 'literal_first_argument_only',
      responseFields: [...fields.values()].sort((a, b) => {
        const left = a.relation + a.path.join('.'), right = b.relation + b.path.join('.');
        return left < right ? -1 : left > right ? 1 : 0;
      }),
      ...(hit.method === 'fs/readFile' ? pathFacts(hit.params, check) : {}),
    });
}
function analyzeAST(ast, check, budget) {
  const rows = [], stack = [{ node: ast, kids: null, next: 0 }];
  while (stack.length) {
    check();
    const frame = stack.at(-1);
    if (frame.kids === null) {
      if (++budget.walked > READ_CALL_LIMITS.nodes) fail('node_limit');
      if (stack.length > READ_CALL_LIMITS.depth) fail('depth_limit');
      const hit = methodCall(frame.node);
      if (hit) {
        if (++budget.calls > READ_CALL_LIMITS.calls) fail('call_limit');
        // Only the current ancestry is retained. Do not build a whole-file
        // parent map or retain a second list of every AST node.
        const parents = new WeakMap();
        for (let i = 1; i < stack.length; i++) parents.set(stack[i].node, stack[i - 1].node);
        analyzeCall(hit, parents, rows, check, budget);
      }
      frame.kids = children(frame.node);
    }
    if (frame.next === frame.kids.length) stack.pop();
    else stack.push({ node: frame.kids[frame.next++], kids: null, next: 0 });
  }
  return rows.sort((a, b) => a.offsetUtf16 - b.offsetUtf16);
}
function newBudget() {
  return { deadline: Date.now() + READ_CALL_LIMITS.milliseconds, tokens: 0, nodes: 0,
    walked: 0, calls: 0, bytes: 0 };
}
function analyzeReadSource(source, budget) {
  try {
    if (version !== '8.15.0' || typeof source !== 'string' ||
        Buffer.byteLength(source, 'utf8') > READ_CALL_LIMITS.bytes) fail('input_limit');
    const check = () => { if (Date.now() > budget.deadline) fail('deadline'); };
    let depth = 0;
    const allocate = () => {
      check();
      if (++budget.nodes > READ_CALL_LIMITS.nodes) fail('node_limit');
    };
    const enter = () => { check(); if (++depth > READ_CALL_LIMITS.depth) fail('depth_limit'); };
    class BoundedParser extends Parser {
      copyNode(...args) { allocate(); return super.copyNode(...args); }
      startNode() { allocate(); return super.startNode(); }
      startNodeAt(...args) { allocate(); return super.startNodeAt(...args); }
      parseStatement(...args) { enter(); try { return super.parseStatement(...args); } finally { depth--; } }
      parseMaybeAssign(...args) { enter(); try { return super.parseMaybeAssign(...args); } finally { depth--; } }
      parseMaybeUnary(...args) { enter(); try { return super.parseMaybeUnary(...args); } finally { depth--; } }
      parseExprAtom(...args) { enter(); try { return super.parseExprAtom(...args); } finally { depth--; } }
      parseExprOp(...args) { enter(); try { return super.parseExprOp(...args); } finally { depth--; } }
      parseBindingAtom(...args) { enter(); try { return super.parseBindingAtom(...args); } finally { depth--; } }
    }
    let ast;
    // The shared budgets cover both attempts and the complete traversal.
    // No partial tree, lexical window or larger retry survives a failure.
    for (const sourceType of ['script', 'module']) {
      depth = 0;
      let delimiters = 0;
      try {
        ast = BoundedParser.parse(source, { ecmaVersion: 2025, sourceType,
          onToken(token) {
            check();
            if (++budget.tokens > READ_CALL_LIMITS.tokens) fail('token_limit');
            const label = token.type.label;
            if (['(', '[', '{', '${'].includes(label) && ++delimiters > READ_CALL_LIMITS.depth) fail('depth_limit');
            if ([')', ']', '}'].includes(label)) delimiters--;
          },
        });
        break;
      } catch (error) {
        if (error instanceof Bound) throw error;
        if (!(error instanceof SyntaxError)) throw error;
      }
    }
    if (!ast) fail('syntax_error');
    const calls = analyzeAST(ast, check, budget);
    check();
    return { schema: 1, status: 'source_syntax_only', sourceParsedCompletely: true,
      runtimeBindingProven: false, responseValueProven: false, consumerCoverage: 'direct_await_and_sibling_const_only', calls };
  } catch (error) {
    // Never expose exceptions, identifiers, values, source excerpts or paths.
    return { schema: 1, status: 'unavailable',
      reason: error instanceof Bound ? error.code : 'analysis_failed' };
  }
}

export function analyzeDesktopReadCallsites(source) {
  return analyzeReadSource(source, newBudget());
}
function digest(bytes) { return createHash('sha256').update(bytes).digest('hex'); }
function unchanged(a, b) {
  return a.dev === b.dev && a.ino === b.ino && a.size === b.size && a.mtimeMs === b.mtimeMs;
}
function samePath(a, b) { return process.platform === 'win32' ? a.toLowerCase() === b.toLowerCase() : a === b; }
function exactRead(fd, count, position) {
  const bytes = Buffer.alloc(count);
  let done = 0;
  while (done < count) {
    const read = fs.readSync(fd, bytes, done, count - done, position + done);
    if (read === 0) fail('source_read_failed');
    done += read;
  }
  return bytes;
}
function archiveDigest(fd, size) {
  const hash = createHash('sha256');
  for (let at = 0; at < size;) {
    const count = Math.min(1024 * 1024, size - at);
    hash.update(exactRead(fd, count, at)); at += count;
  }
  return hash.digest('hex');
}
// expectedArchiveSha256 MUST come from the harness's original signed-MSIX
// verification, not from the installed file or an unverified report. This
// reader compares bytes to that identity; it does not itself verify a signer.
export function inspectDesktopReadPackage(packageRoot, expectedArchiveSha256) {
  let fd;
  try {
    if (typeof packageRoot !== 'string' || !path.isAbsolute(packageRoot) ||
        !/^OpenAI\.Codex_26\.1103\.3517\.0_x64__[a-z0-9]{13}$/i.test(path.basename(packageRoot)) ||
        typeof expectedArchiveSha256 !== 'string' || !/^[a-f0-9]{64}$/.test(expectedArchiveSha256)) fail('package_path');
    const root = path.resolve(packageRoot);
    let current = root;
    for (const part of ['', 'app', 'resources', 'app.asar']) {
      if (part) current = path.join(current, part);
      const stat = fs.lstatSync(current);
      if (stat.isSymbolicLink() || !samePath(fs.realpathSync(current), current) ||
          (part === 'app.asar' ? !stat.isFile() : !stat.isDirectory())) fail('package_path');
    }
    const before = fs.lstatSync(current);
    fd = fs.openSync(current, 'r');
    const opened = fs.fstatSync(fd);
    if (!unchanged(before, opened)) fail('archive_changed');
    const index = readIndex(fd, opened.size, true);
    if (index.publicRootBlocked) fail('source_read_failed');
    const entries = [...index.publicSources].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0);
    if (entries.length > READ_CALL_LIMITS.sources) fail('source_limit');
    const archiveSha256 = archiveDigest(fd, opened.size);
    if (archiveSha256 !== expectedArchiveSha256) fail('archive_mismatch');
    const budget = newBudget(), sources = [];
    for (const [entry, info] of entries) {
      if (Date.now() > budget.deadline) fail('deadline');
      if (info.status !== 'packed') fail('source_read_failed');
      if (info.size > READ_CALL_LIMITS.bytes || (budget.bytes += info.size) > READ_CALL_LIMITS.totalBytes) fail('source_limit');
      const bytes = exactRead(fd, info.size, info.start);
      let source;
      try { source = new TextDecoder('utf-8', { fatal: true }).decode(bytes); }
      catch { fail('source_read_failed'); }
      const analysis = analyzeReadSource(source, budget);
      // Syntax and local consumer complexity may omit one whole source. Global
      // resource, unknown, I/O or provenance failures discard the whole report.
      if (analysis.status === 'unavailable' &&
          !['syntax_error', 'local_node_limit', 'field_limit'].includes(analysis.reason)) fail(analysis.reason);
      sources.push({ path: entry, sha256: digest(bytes), analysis });
    }
    if (!unchanged(opened, fs.fstatSync(fd)) || !unchanged(opened, fs.lstatSync(current))) fail('archive_changed');
    const complete = sources.filter(source => source.analysis.status === 'source_syntax_only').length;
    const report = { schema: 1, status: complete === sources.length ? 'complete' : 'partial',
      archiveSha256, filesSelected: entries.length, filesComplete: complete,
      sourceBytes: budget.bytes, sources };
    if (Buffer.byteLength(JSON.stringify(report), 'utf8') > READ_CALL_LIMITS.reportBytes) fail('report_limit');
    return report;
  } catch (error) {
    return { schema: 1, status: 'unavailable',
      reason: error instanceof Bound ? error.code : 'source_read_failed' };
  } finally { if (fd !== undefined) fs.closeSync(fd); }
}
function main() {
  let report;
  try {
    if (process.platform !== 'win32' || process.env.GITHUB_ACTIONS !== 'true' ||
        process.env.RUNNER_OS !== 'Windows') fail('disposable_windows_ci_required');
    if (process.argv.length !== 6 || process.argv[2] !== '--package-root' ||
        process.argv[4] !== '--expected-asar-sha256') fail('invalid_arguments');
    report = inspectDesktopReadPackage(process.argv[3], process.argv[5]);
  } catch (error) {
    report = { schema: 1, status: 'unavailable', reason: error instanceof Bound ? error.code : 'analysis_failed' };
  }
  process.stdout.write('DESKTOP_READ_CALLERS_METADATA=' + JSON.stringify(report) + '\n');
  if (report.status === 'unavailable') process.exitCode = 2;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
