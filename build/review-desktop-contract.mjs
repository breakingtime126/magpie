// Public, exact-package contract evidence. Never import or evaluate ASAR code.
import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { readIndex } from './inspect-desktop-static-metadata.mjs';

export const ARCHIVE_SHA = 'fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa';
export const ANCHORS = Object.freeze([
  'codex_cli_command', 'CODEX_CLI_PATH', 'CODEX_APP_RUNTIME_CLI_PATH',
  'startupCommand', 'getConfigOverrides', '--print-startup-config', '--startup-client-info',
  'experimentalApi', 'clientInfo', 'capabilities:', 'getAuthStatus', 'account/read',
  'config/read', 'configRequirements/read', 'model/list', 'thread/list', 'environment/',
]);
const REQUIRED = ANCHORS.slice(0, 9);
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const fail = () => { throw new Error('Public contract evidence is unavailable.'); };
const utf8 = bytes => new TextDecoder('utf-8', { fatal: true }).decode(bytes);
function unchanged(a, b) { return a.dev === b.dev && a.ino === b.ino && a.size === b.size && a.mtimeMs === b.mtimeMs; }
function read(fd, count, position) {
  const bytes = Buffer.alloc(count); let done = 0;
  while (done < count) { const n = fs.readSync(fd, bytes, done, count - done, position + done); if (!n) fail(); done += n; }
  return bytes;
}
export function sourceExcerpts(source, sourcePath, sourceSha256, remaining) {
  const matches = [];
  for (const anchor of ANCHORS) {
    let from = 0; let count = 0; const excerpts = [];
    while (true) {
      const offset = source.indexOf(anchor, from); if (offset < 0) break;
      count++; from = offset + anchor.length;
      if (remaining[anchor] > 0) {
        const start = Math.max(0, offset - 1600); const end = Math.min(source.length, offset + anchor.length + 1600);
        excerpts.push({ offset, start, end, text: source.slice(start, end) }); remaining[anchor]--;
      }
    }
    if (count) matches.push({ anchor, count, excerpts });
  }
  return matches.length ? { path: sourcePath, sha256: sourceSha256, matches } : null;
}
export function inspectContractArchive(file, expected = ARCHIVE_SHA) {
  if (!path.isAbsolute(file) || fs.realpathSync(file) !== file || !fs.lstatSync(file).isFile()) fail();
  const fd = fs.openSync(file, 'r');
  try {
    const before = fs.fstatSync(fd); if (before.size > 2 * 1024 ** 3) fail();
    const digest = createHash('sha256'); let position = 0;
    while (position < before.size) { const b = read(fd, Math.min(65536, before.size - position), position); digest.update(b); position += b.length; }
    if (digest.digest('hex') !== expected) fail();
    const index = readIndex(fd, before.size, true); if (index.publicRootBlocked) fail();
    const entries = [...index.publicSources.entries()].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0);
    if (!entries.length || entries.length > 4096) fail();
    const remaining = Object.fromEntries(ANCHORS.map(a => [a, 8])); const counts = Object.fromEntries(ANCHORS.map(a => [a, 0]));
    const sources = []; let scannedBytes = 0;
    for (const [name, entry] of entries) {
      if (entry.status !== 'packed' || entry.size > 16 * 1024 ** 2) fail();
      scannedBytes += entry.size; if (scannedBytes > 128 * 1024 ** 2) fail();
      const bytes = read(fd, entry.size, entry.start);
      const result = sourceExcerpts(utf8(bytes), name, hash(bytes), remaining);
      if (result) { sources.push(result); for (const match of result.matches) counts[match.anchor] += match.count; }
    }
    if (!unchanged(before, fs.fstatSync(fd)) || !unchanged(before, fs.statSync(file))) fail();
    const result = { schema: 1, scope: 'public-static-source-excerpts', archiveSha256: expected,
      runtimeSelectionProven: false, requiredAnchorsPresent: REQUIRED.every(anchor => counts[anchor] > 0),
      offsets: 'JavaScript UTF-16 code units', scannedFiles: entries.length, scannedBytes,
      anchors: ANCHORS.map(anchor => ({ anchor, matches: counts[anchor], excerpts: 8 - remaining[anchor], truncated: counts[anchor] > 8 })), sources };
    if (Buffer.byteLength(JSON.stringify(result)) >= 1024 ** 2) fail();
    return result;
  } finally { fs.closeSync(fd); }
}

// Keep every generated schema, including referenced definitions. No remote refs
// or traversal are accepted; schema metadata such as $schema is not fetched.
export function validateSchemas(root) {
  if (!path.isAbsolute(root) || fs.realpathSync(root) !== root || !fs.lstatSync(root).isDirectory()) fail();
  const pending = ['']; const files = []; let bytes = 0; let nodes = 0; const documents = new Map();
  while (pending.length) {
    const dir = pending.pop();
    for (const entry of fs.readdirSync(path.join(root, dir), { withFileTypes: true })) {
      if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,190}$/.test(entry.name)) fail();
      const relative = dir ? dir + '/' + entry.name : entry.name; if (relative.length > 512) fail();
      const full = path.join(root, relative); const info = fs.lstatSync(full);
      if (info.isSymbolicLink()) fail();
      if (info.isDirectory()) { if (relative.split('/').length > 4) fail(); pending.push(relative); continue; }
      if (!info.isFile() || !relative.endsWith('.json') || info.size < 2 || info.size > 8 * 1024 ** 2) fail();
      bytes += info.size; if (bytes > 64 * 1024 ** 2 || files.length >= 2048) fail();
      const data = fs.readFileSync(full); const value = JSON.parse(utf8(data));
      if (!value || typeof value !== 'object' || Array.isArray(value)) fail();
      documents.set(relative, value); files.push({ path: relative, bytes: data.length, sha256: hash(data) });
    }
  }
  if (!files.length) fail();
  for (const [file, value] of documents) {
    const work = [{ value, depth: 0 }];
    while (work.length) {
      const node = work.pop(); if (++nodes > 2000000 || node.depth > 128) fail();
      if (node.value === null || typeof node.value !== 'object') continue;
      for (const [key, child] of Object.entries(node.value)) {
        if (key === '$ref') {
          if (typeof child !== 'string' || child.length > 2048 || child.includes('\\') || child.includes('%')) fail();
          const [target, fragment, extra] = child.split('#'); if (extra !== undefined) fail();
          if (target && (!/^[A-Za-z0-9_./-]+\.json$/.test(target) || target.startsWith('/') || target.includes('//'))) fail();
          const targetPath = target ? path.posix.normalize(path.posix.join(path.posix.dirname(file), target)) : file;
          if (targetPath === '..' || targetPath.startsWith('../') || path.posix.isAbsolute(targetPath)) fail();
          let pointed = documents.get(targetPath); if (!pointed) fail();
          if (fragment) {
            if (!fragment.startsWith('/')) fail();
            for (const part of fragment.slice(1).split('/')) {
              if (/~(?![01])/.test(part)) fail(); const token = part.replace(/~1/g, '/').replace(/~0/g, '~');
              if (pointed === null || typeof pointed !== 'object' || !Object.hasOwn(pointed, token)) fail(); pointed = pointed[token];
            }
          }
        }
        work.push({ value: child, depth: node.depth + 1 });
      }
    }
  }
  return { schema: 1, scope: 'full-experimental-generated-protocol', files: files.sort((a, b) => a.path < b.path ? -1 : 1), bytes, referencesResolved: true };
}
function main() {
  try {
    if (process.argv.length !== 4 || !['--archive', '--schemas'].includes(process.argv[2])) fail();
    const result = process.argv[2] === '--archive' ? inspectContractArchive(process.argv[3]) : validateSchemas(process.argv[3]);
    process.stdout.write(JSON.stringify(result) + '\n');
  } catch { process.stderr.write('Public contract evidence is unavailable.\n'); process.exitCode = 1; }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
