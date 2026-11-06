// Parse only public package source supplied by the bounded ASAR reader.
// This module never evaluates that source or resolves its imports.
import { Parser, version } from './vendor/acorn-8.15.0/acorn.mjs';

export const AST_LIMITS = Object.freeze({
  bytes: 16 * 1024 ** 2, tokens: 4000000, nodes: 1000000,
  depth: 512, milliseconds: 15000, candidates: 128, constructions: 128,
});
export const AST_BLOCK_CODES = Object.freeze([
  'public_ast_bytes_limit', 'public_ast_token_limit', 'public_ast_node_limit',
  'public_ast_parse_depth_limit', 'public_ast_walk_depth_limit',
  'public_ast_walk_node_limit', 'public_ast_deadline', 'public_ast_range_error',
]);
export const AST_SKIP_CODES = Object.freeze([
  'public_ast_token_limit', 'public_ast_node_limit', 'public_ast_parse_depth_limit',
  'public_ast_walk_depth_limit', 'public_ast_walk_node_limit', 'public_ast_deadline',
]);
export const PLUGIN_METHODS = Object.freeze(['getConfigOverrides', 'startupCommand']);
export const PLUGIN_SHAPES = Object.freeze([
  'direct_enabled', 'direct_auto_provision', 'mcp_server_enabled',
  'app_enabled', 'app_destructive_enabled', 'app_open_world_enabled',
  'app_default_tools_enabled', 'app_tool_enabled',
]);
const SEGMENT = '[A-Za-z0-9][A-Za-z0-9_-]{0,63}';
const ID = new RegExp('^' + SEGMENT + '@' + SEGMENT + '$');
const KEY = new RegExp('^plugins\\.' + SEGMENT + '@' + SEGMENT + '\\.(enabled|auto_provision)$');
const HOLE = '\0';

export class PublicPluginASTLimit extends Error {
  constructor(code) { super(code); this.code = code; }
}
function limit(code) { throw new PublicPluginASTLimit(code); }
function freshMethods() {
  return PLUGIN_METHODS.map(name => ({ name, definitions: 0, unresolved: 0 }));
}

// Counters and the deadline cover both parse attempts. Hooks bound work while
// Acorn is building the tree; traversal limits alone would be too late.
function parseComplete(source) {
  if (version !== '8.15.0' || typeof source !== 'string') throw new Error('invalid_parser_input');
  if (Buffer.byteLength(source, 'utf8') > AST_LIMITS.bytes) limit('public_ast_bytes_limit');
  let nodes = 0, tokens = 0, depth = 0, delimiters = 0;
  const deadline = Date.now() + AST_LIMITS.milliseconds;
  function check() { if (Date.now() > deadline) limit('public_ast_deadline'); }
  function node() { if (++nodes > AST_LIMITS.nodes) limit('public_ast_node_limit'); check(); }
  function enter() { if (++depth > AST_LIMITS.depth) limit('public_ast_parse_depth_limit'); check(); }
  class BoundedParser extends Parser {
    copyNode(...args) { node(); return super.copyNode(...args); }
    startNode() { node(); return super.startNode(); }
    startNodeAt(...args) { node(); return super.startNodeAt(...args); }
    parseStatement(...args) { enter(); try { return super.parseStatement(...args); } finally { depth--; } }
    parseMaybeAssign(...args) { enter(); try { return super.parseMaybeAssign(...args); } finally { depth--; } }
    parseMaybeUnary(...args) { enter(); try { return super.parseMaybeUnary(...args); } finally { depth--; } }
    parseExprAtom(...args) { enter(); try { return super.parseExprAtom(...args); } finally { depth--; } }
    parseExprOp(...args) { enter(); try { return super.parseExprOp(...args); } finally { depth--; } }
    parseBindingAtom(...args) { enter(); try { return super.parseBindingAtom(...args); } finally { depth--; } }
  }
  for (const sourceType of ['script', 'module']) {
    depth = 0; delimiters = 0;
    try {
      const ast = BoundedParser.parse(source, {
        ecmaVersion: 2025, sourceType,
        onToken(token) {
          if (++tokens > AST_LIMITS.tokens) limit('public_ast_token_limit');
          check();
          const label = token.type.label;
          if (['(', '[', '{', '$' + '{'].includes(label) && ++delimiters > AST_LIMITS.depth) limit('public_ast_parse_depth_limit');
          if ([')', ']', '}'].includes(label)) delimiters--;
        },
      });
      check();
      return { ast, check };
    } catch (error) {
      if (error instanceof PublicPluginASTLimit) throw error;
      if (error instanceof RangeError) limit('public_ast_range_error');
      if (!(error instanceof SyntaxError)) throw error;
    }
  }
  return null;
}

function plainString(node, source) {
  if (node?.type === 'Literal' && typeof node.value === 'string') {
    const raw = source.slice(node.start, node.end);
    if ((raw[0] === '"' || raw[0] === "'") && raw.at(-1) === raw[0] &&
        raw.slice(1, -1) === node.value && !raw.includes('\\')) return node.value;
  }
  if (node?.type === 'TemplateLiteral' && node.expressions.length === 0 &&
      node.quasis.length === 1 && node.quasis[0].value.raw === node.quasis[0].value.cooked &&
      !node.quasis[0].value.raw.includes('\\')) return node.quasis[0].value.raw;
  return null;
}
function functionNode(node) {
  return node && ['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression'].includes(node.type);
}
function propertyName(node, computed = false) {
  if (!computed && node?.type === 'Identifier') return node.name;
  if (node?.type === 'Literal' && typeof node.value === 'string') return node.value;
  return null;
}
function namedDefinition(node) {
  if (node.type === 'FunctionDeclaration') return { name: node.id?.name, fn: node };
  if (node.type === 'VariableDeclarator' && node.id?.type === 'Identifier' && functionNode(node.init)) {
    return { name: node.id.name, fn: node.init };
  }
  if ((node.type === 'MethodDefinition' || node.type === 'Property' || node.type === 'PropertyDefinition') &&
      functionNode(node.value)) return { name: propertyName(node.key, node.computed), fn: node.value };
  if (node.type === 'AssignmentExpression' && node.operator === '=' &&
      node.left.type === 'MemberExpression' && functionNode(node.right)) {
    return { name: propertyName(node.left.property, node.left.computed), fn: node.right };
  }
  return null;
}
function composition(node) {
  return node?.type === 'TemplateLiteral' ||
    (node?.type === 'BinaryExpression' && node.operator === '+');
}

// Unknown expressions become anonymous holes, never values or identifier names.
// A literal first fragment establishes string concatenation. No calls, bindings,
// property reads, imported constants or template tags are executed/resolved.
function pattern(root, source) {
  const work = [root], fragments = [];
  let count = 0, length = 0;
  while (work.length) {
    if (++count > 256) return null;
    const node = work.pop();
    const text = plainString(node, source);
    if (text !== null) {
      if (text.includes(HOLE)) return null;
      fragments.push(text);
    }
    else if (node?.type === 'Literal' && typeof node.value === 'boolean') fragments.push(String(node.value));
    else if (node?.type === 'BinaryExpression' && node.operator === '+') {
      work.push(node.right, node.left); continue;
    } else if (node?.type === 'TemplateLiteral') {
      if (node.quasis.some(q => q.value.raw !== q.value.cooked || q.value.raw.includes('\\') || q.value.raw.includes(HOLE))) return null;
      const parts = [];
      for (let i = 0; i < node.quasis.length; i++) {
        parts.push({ text: node.quasis[i].value.raw });
        if (i < node.expressions.length) parts.push({ hole: true });
      }
      for (const part of parts) fragments.push(part.hole ? HOLE : part.text);
    } else fragments.push(HOLE);
    length = fragments.reduce((sum, fragment) => sum + fragment.length, 0);
    if (length > 512) return null;
  }
  return fragments.join('');
}
function shapeOf(key) {
  const parts = key.split('.');
  if (parts[0] !== 'plugins') return null;
  const segment = part => typeof part === 'string' && part.length >= 1 && part.length <= 128 &&
    /^[A-Za-z0-9_\-\0]+$/.test(part) &&
    (part.includes(HOLE) || /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(part));
  const identity = parts[1];
  if (identity !== HOLE) {
    const pair = identity?.split('@');
    if (!pair || pair.length !== 2 || !pair.every(segment)) return null;
  }
  if (parts.length === 3 && parts[2] === 'enabled') return 'direct_enabled';
  if (parts.length === 3 && parts[2] === 'auto_provision') return 'direct_auto_provision';
  if (parts.length === 5 && parts[2] === 'mcp_servers' && segment(parts[3]) && parts[4] === 'enabled') {
    return 'mcp_server_enabled';
  }
  if (parts[2] !== 'apps' || !segment(parts[3])) return null;
  if (parts.length === 5) {
    if (parts[4] === 'enabled') return 'app_enabled';
    if (parts[4] === 'destructive_enabled') return 'app_destructive_enabled';
    if (parts[4] === 'open_world_enabled') return 'app_open_world_enabled';
    if (parts[4] === 'default_tools_enabled') return 'app_default_tools_enabled';
  }
  if (parts.length === 7 && parts[4] === 'tools' && segment(parts[5]) && parts[6] === 'enabled') {
    return 'app_tool_enabled';
  }
  return null;
}
function construction(root, source) {
  const text = pattern(root, source);
  if (text === null || !text.startsWith('plugins.')) return null;
  const equals = text.indexOf('='), key = equals < 0 ? text : text.slice(0, equals);
  const shape = shapeOf(key);
  if (!shape) return { unresolved: true };
  const rhs = equals < 0 ? '' : text.slice(equals + 1);
  return {
    form: root.type === 'TemplateLiteral' ? 'template' : root.type === 'BinaryExpression' ? 'concat' : 'literal',
    shape,
    value: rhs === 'true' ? 'bool_true' : rhs === 'false' ? 'bool_false' :
      rhs.includes(HOLE) ? 'dynamic' : 'other',
  };
}

export function analyzePublicPluginAST(source) {
  const parsed = parseComplete(source);
  if (!parsed) return { complete: false, candidates: [], methods: freshMethods(), constructions: [] };
  const { ast, check } = parsed;
  const found = new Map(), rows = new Map(), methods = freshMethods();
  const functionNames = new WeakMap(), counted = new WeakSet();
  function add(key, derivation) {
    if (key.length > 128 || found.size >= AST_LIMITS.candidates && !found.has(key)) {
      throw new PublicPluginASTLimit('public_catalog_limit');
    }
    if (!found.has(key)) found.set(key, derivation);
  }
  const stack = [{ node: ast, parent: null, method: null, depth: 0, composed: false }];
  let visited = 0;
  while (stack.length) {
    check();
    const { node, parent, method: inherited, depth, composed } = stack.pop();
    if (++visited > AST_LIMITS.nodes) limit('public_ast_walk_node_limit');
    if (depth > AST_LIMITS.depth) limit('public_ast_walk_depth_limit');
    const definition = namedDefinition(node);
    if (definition && PLUGIN_METHODS.includes(definition.name)) {
      functionNames.set(definition.fn, definition.name);
      if (!counted.has(definition.fn)) {
        counted.add(definition.fn);
        methods.find(item => item.name === definition.name).definitions++;
      }
    }
    const method = functionNames.get(node) ?? inherited;
    const tagged = node.type === 'TemplateLiteral' && parent?.type === 'TaggedTemplateExpression';
    const value = tagged ? null : plainString(node, source);
    if (value !== null) {
      if (ID.test(value)) {
        add('plugins.' + value + '.enabled', 'id_literal');
        add('plugins.' + value + '.auto_provision', 'id_literal');
      }
      const equals = value.indexOf('='), key = equals < 0 ? value : value.slice(0, equals);
      const rhs = equals < 0 ? '' : value.slice(equals + 1);
      if (KEY.test(key) && (equals < 0 || rhs === '' || rhs === 'true' || rhs === 'false')) add(key, 'key_literal');
    }
    if (method && !composed && !tagged && (value !== null || composition(node))) {
      const item = construction(node, source);
      if (item?.unresolved) methods.find(state => state.name === method).unresolved++;
      else if (item) {
        const identity = [method, item.form, item.shape, item.value].join('|');
        const previous = rows.get(identity);
        if (previous) previous.count++;
        else {
          if (rows.size >= AST_LIMITS.constructions) throw new PublicPluginASTLimit('public_catalog_limit');
          rows.set(identity, { method, ...item, count: 1 });
        }
      }
    }
    // Enumerate AST children only. Literal values (including regexp objects)
    // are not walked; arbitrary source data never becomes a report object.
    for (const [key, child] of Object.entries(node).reverse()) {
      if (['value', 'regex', 'loc', 'range', 'raw', 'start', 'end'].includes(key) &&
          !(key === 'value' && child && typeof child.type === 'string')) continue;
      const children = Array.isArray(child) ? child : [child];
      for (let i = children.length - 1; i >= 0; i--) {
        const next = children[i];
        if (next && typeof next === 'object' && typeof next.type === 'string') {
          stack.push({ node: next, parent: node, method, depth: depth + 1,
            composed: composed || composition(node) && (key === 'left' || key === 'right' || key === 'expressions') });
        }
      }
    }
  }
  return {
    complete: true,
    candidates: [...found].map(([key, derivation]) => ({ key, derivation })),
    methods, constructions: [...rows.values()],
  };
}
