// Public, exact-package contract evidence. Never import or evaluate ASAR code.
import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { readIndex, InspectionError } from './inspect-desktop-static-metadata.mjs';

export const ARCHIVE_SHA = 'fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa';
export const ANCHORS = Object.freeze([
  'codex_cli_command', 'CODEX_CLI_PATH', 'CODEX_APP_RUNTIME_CLI_PATH',
  'startupCommand', 'getConfigOverrides', '--print-startup-config', '--startup-client-info',
  'experimentalApi', 'clientInfo', 'capabilities:', 'getAuthStatus', 'account/read',
  'config/read', 'configRequirements/read', 'model/list', 'thread/list', 'environment/',
]);
const REQUIRED = ANCHORS.slice(0, 9);
export const MENU_ANCHORS = Object.freeze([
  'Legacy coding model', 'hasConfiguredModelCatalog', 'model_catalog_json', 'Select model',
  'modelProvider', 'model_provider', 'isCustomProvider', 'customProvider',
]);
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
class ContractReadError extends Error {
  constructor(code) { super('Public contract evidence is unavailable.'); this.code = code; }
}
const fail = (code = 'contract_evidence_unavailable') => { throw new ContractReadError(code); };
// Only codes emitted by the bounded shared index reader, never exception text.
const INDEX_FAILURE_CODES = new Set([
  'invalid_arguments', 'truncated_archive', 'archive_too_large', 'unsupported_asar_prefix',
  'invalid_header_size', 'header_too_large', 'truncated_header', 'invalid_header_pickle',
  'invalid_json_size', 'invalid_header_padding', 'invalid_utf8', 'invalid_header_json',
  'invalid_header_root', 'invalid_header_entry', 'invalid_unpacked_flag', 'invalid_link_entry',
  'invalid_directory', 'header_too_deep', 'too_many_entries', 'invalid_entry_path',
  'entry_path_too_long', 'invalid_file_size', 'invalid_file_offset', 'file_outside_archive',
]);
function menuEntryMetadata([name, entry]) {
  // Only validated public archive-relative paths, never machine paths or link destinations.
  return { path: name.length <= 192 && /^[A-Za-z0-9_./@+() -]+$/.test(name) ? name : null,
    pathCharacters: name.length, status: entry.status, bytes: entry.size ?? null };
}
function menuIndexMetadata(index) {
  const files = [...index.publicSources];
  const packed = files.filter(([, e]) => e.status === 'packed');
  const blocked = new Map([...index.menuLinks, ...files.filter(([, e]) => e.status !== 'packed' || e.size > 16 * 1024 ** 2)]);
  const candidates = new Map([...blocked, ...packed.sort((a, b) => b[1].size - a[1].size)]);
  return { entries: index.entries, ...index.sourceCoverage, rootLinkBlocked: index.publicRootBlocked,
    candidateFiles: files.length, packedFiles: packed.length,
    unpackedFiles: files.filter(([, e]) => e.status === 'unpacked_not_read').length,
    linkEntries: index.menuLinks.size, oversizedFiles: packed.filter(([, e]) => e.size > 16 * 1024 ** 2).length,
    packedBytes: packed.reduce((n, [, e]) => n + e.size, 0), maxPackedFileBytes: packed.reduce((n, [, e]) => Math.max(n, e.size), 0),
    candidateExamples: [...candidates].slice(0, 8).map(menuEntryMetadata), candidateExamplesOmitted: Math.max(0, candidates.size - 8),
    htmlEntries: index.menuHtml.size, htmlExamples: [...index.menuHtml].slice(0, 8).map(menuEntryMetadata),
    htmlExamplesOmitted: Math.max(0, index.menuHtml.size - 8), htmlContentsRead: false,
    rendererOwnershipProven: false };
}
// Only the static menu CLI uses this evidence envelope. A completed diagnostic
// serializes with exit 0, but status=blocked makes its PowerShell job fail.
// Legacy archive/schema CLI errors still use their unchanged nonzero path.
export function rendererMenuEvidence(file, expected = ARCHIVE_SHA) {
  const diagnostic = { archiveHashMatched: false, archiveIdentityUnchanged: false, indexValidated: false, index: null, currentSource: null };
  try { return inspectContractArchive(file, expected, true, diagnostic); }
  catch (error) {
    const code = error instanceof ContractReadError ? error.code :
      error instanceof InspectionError && INDEX_FAILURE_CODES.has(error.code) ? error.code : 'io_or_inspection_failed';
    return { schema: 1, scope: 'public-static-renderer-menu-excerpts', archiveSha256: expected,
      status: 'blocked', failure: { code, ...diagnostic }, menuContractProven: false };
  }
}
const utf8 = bytes => new TextDecoder('utf-8', { fatal: true }).decode(bytes);
function unchanged(a, b) { return a.dev === b.dev && a.ino === b.ino && a.size === b.size && a.mtimeMs === b.mtimeMs; }
function read(fd, count, position) {
  const bytes = Buffer.alloc(count); let done = 0;
  while (done < count) { const n = fs.readSync(fd, bytes, done, count - done, position + done); if (!n) fail(); done += n; }
  return bytes;
}
export function sourceExcerpts(source, sourcePath, sourceSha256, remaining, anchors = ANCHORS, radius = 1600, deduplicate = false) {
  const matches = [];
  for (const anchor of anchors) {
    let from = 0; let count = 0; let candidateWindowsDeduplicated = 0; const excerpts = [];
    while (true) {
      const offset = source.indexOf(anchor, from); if (offset < 0) break;
      count++; from = offset + anchor.length;
      if (remaining[anchor] > 0) {
        let start = Math.max(0, offset - radius); let end = Math.min(source.length, offset + anchor.length + radius);
        if (deduplicate) {
          // Source is valid UTF-8; trim inward if the UTF-16 context boundary
          // bisects a supplementary character. Never publish lone surrogates.
          if (start > 0 && source.charCodeAt(start) >= 0xdc00 && source.charCodeAt(start) <= 0xdfff) start++;
          if (end < source.length && source.charCodeAt(end - 1) >= 0xd800 && source.charCodeAt(end - 1) <= 0xdbff) end--;
        }
        if (deduplicate && excerpts.some(e => start < e.end && e.start < end)) { candidateWindowsDeduplicated++; continue; }
        excerpts.push({ offset, start, end, text: source.slice(start, end) }); remaining[anchor]--;
      }
    }
    if (count) matches.push({ anchor, count, excerpts, ...(deduplicate ? { candidateWindowsDeduplicated } : {}) });
  }
  return matches.length ? { path: sourcePath, sha256: sourceSha256, matches } : null;
}
// Publication budget is separate from the parser/JSON cap. Prefer one narrow
// window per anchor before second windows, and never repeat overlapping text.
export function trimMenuExcerpts(sources) {
  const candidates = new Map(MENU_ANCHORS.map(a => [a, []]));
  for (const source of sources) for (const match of source.matches) {
    for (const excerpt of match.excerpts) candidates.get(match.anchor).push({ source, match, excerpt });
    match.excerpts = [];
  }
  const ranges = new Map(); let textBytes = 0; let deduplicated = 0; let budgetOmitted = 0;
  for (let round = 0; round < 2; round++) for (const anchor of MENU_ANCHORS) {
    const entries = candidates.get(anchor);
    while (entries.length) {
      const { source, match, excerpt } = entries.shift();
      const seen = ranges.get(source.path) ?? [];
      if (seen.some(e => excerpt.start < e.end && e.start < excerpt.end)) { deduplicated++; continue; }
      const bytes = Buffer.byteLength(excerpt.text);
      if (textBytes + bytes > 16384) { budgetOmitted++; continue; }
      match.excerpts.push(excerpt); seen.push(excerpt); ranges.set(source.path, seen); textBytes += bytes;
      break;
    }
  }
  return { textBytes, textBudgetBytes: 16384, deduplicated, budgetOmitted,
    remainingCandidatesOmitted: [...candidates.values()].reduce((n, entries) => n + entries.length, 0) };
}
export function inspectContractArchive(file, expected = ARCHIVE_SHA, rendererMenu = false, diagnostic = null) {
  if (typeof rendererMenu !== 'boolean') fail();
  const anchors = rendererMenu ? MENU_ANCHORS : ANCHORS;
  if (!path.isAbsolute(file) || fs.realpathSync(file) !== file || !fs.lstatSync(file).isFile()) fail();
  const fd = fs.openSync(file, 'r'); let before;
  try {
    before = fs.fstatSync(fd); if (before.size > 2 * 1024 ** 3) fail('archive_too_large');
    const digest = createHash('sha256'); let position = 0;
    while (position < before.size) { const b = read(fd, Math.min(65536, before.size - position), position); digest.update(b); position += b.length; }
    if (digest.digest('hex') !== expected) fail('archive_hash_mismatch');
    if (diagnostic) diagnostic.archiveHashMatched = true;
    const index = readIndex(fd, before.size, true, rendererMenu ? 'desktop-4239' : '', rendererMenu);
    if (diagnostic) { diagnostic.index = menuIndexMetadata(index); diagnostic.indexValidated = true; }
    if (index.publicRootBlocked) fail('source_link_blocked');
    const entries = [...index.publicSources.entries()].sort(([a], [b]) => {
      // Preserve old ordering by default; menu review spends excerpt quotas on
      // sources outside the already-reviewed main-process directory first.
      if (rendererMenu && a.startsWith('.vite/build/') !== b.startsWith('.vite/build/')) return a.startsWith('.vite/build/') ? 1 : -1;
      return a < b ? -1 : a > b ? 1 : 0;
    });
    if (!entries.length) fail('source_candidates_missing');
    if (entries.length > 4096) fail('source_count_bound');
    const remaining = Object.fromEntries(anchors.map(a => [a, 8])); const counts = Object.fromEntries(anchors.map(a => [a, 0]));
    const sources = []; let scannedBytes = 0;
    for (const [name, entry] of entries) {
      if (diagnostic) diagnostic.currentSource = menuEntryMetadata([name, entry]);
      if (entry.status !== 'packed') fail(entry.status === 'unpacked_not_read' ? 'source_unpacked_blocked' : 'source_link_blocked');
      if (entry.size > 16 * 1024 ** 2) fail('source_file_bound');
      scannedBytes += entry.size; if (scannedBytes > 128 * 1024 ** 2) fail('source_total_bound');
      const bytes = read(fd, entry.size, entry.start);
      let source; try { source = utf8(bytes); } catch { fail('source_invalid_utf8'); }
      const result = sourceExcerpts(source, name, hash(bytes), remaining, anchors, rendererMenu ? 900 : 1600, rendererMenu);
      if (result) { sources.push(result); for (const match of result.matches) counts[match.anchor] += match.count; }
    }
    if (!unchanged(before, fs.fstatSync(fd)) || !unchanged(before, fs.statSync(file))) fail('archive_identity_changed');
    const publication = rendererMenu ? trimMenuExcerpts(sources) : null;
    const retained = anchor => rendererMenu ? sources.flatMap(s => s.matches).filter(m => m.anchor === anchor).reduce((n, m) => n + m.excerpts.length, 0) : 8 - remaining[anchor];
    const result = { schema: 1, scope: rendererMenu ? 'public-static-renderer-menu-excerpts' : 'public-static-source-excerpts', archiveSha256: expected,
      runtimeSelectionProven: false, requiredAnchorsPresent: (rendererMenu ? ['Legacy coding model', 'model_catalog_json'] : REQUIRED).every(anchor => counts[anchor] > 0),
      offsets: 'JavaScript UTF-16 code units', scannedFiles: entries.length, scannedBytes,
      anchors: anchors.map(anchor => ({ anchor, matches: counts[anchor], excerpts: retained(anchor), truncated: counts[anchor] > retained(anchor) })), sources,
      ...(rendererMenu ? { coverage: { ...index.sourceCoverage, candidateFiles: entries.length,
        outsideLegacyDirectory: entries.filter(([name]) => !name.startsWith('.vite/build/')).length,
        eligiblePackedJavaScriptScanComplete: true, allPackageFilesCovered: false,
        limits: { files: 4096, fileBytes: 16 * 1024 ** 2, totalBytes: 128 * 1024 ** 2, excerptsPerAnchor: 2, contextCharactersEachSide: 900, excerptTextBytes: 16384, outputBytes: 1024 ** 2 },
        negativeSearchProvesAbsence: false, publication } } : {}) };
    if (Buffer.byteLength(JSON.stringify(result)) >= 1024 ** 2) fail('source_output_bound');
    return result;
  } finally {
    if (diagnostic) {
      try { diagnostic.archiveIdentityUnchanged = Boolean(before && unchanged(before, fs.fstatSync(fd)) && unchanged(before, fs.statSync(file))); }
      catch { diagnostic.archiveIdentityUnchanged = false; }
      if (!diagnostic.archiveIdentityUnchanged) { diagnostic.indexValidated = false; diagnostic.index = null; diagnostic.currentSource = null; }
    }
    fs.closeSync(fd);
  }
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
    if (process.argv.length !== 4 || !['--archive', '--schemas', '--renderer-menu'].includes(process.argv[2])) fail();
    const result = process.argv[2] === '--schemas' ? validateSchemas(process.argv[3]) :
      process.argv[2] === '--renderer-menu' ? rendererMenuEvidence(process.argv[3]) : inspectContractArchive(process.argv[3]);
    process.stdout.write(JSON.stringify(result) + '\n');
  } catch { process.stderr.write('Public contract evidence is unavailable.\n'); process.exitCode = 1; }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
