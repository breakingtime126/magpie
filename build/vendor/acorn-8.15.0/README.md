# Acorn 8.15.0: diagnostic-only third-party dependency

This directory vendors the unmodified ESM parser and MIT license from the
Node.js v24.11.0 source distribution. It is used only by the read-only Desktop
metadata inspector. It is not part of the production bridge and never executes
or resolves imports from inspected Desktop code. No npm installation, lifecycle
script, online test dependency, loose parser or Node-internal API is used.

## Provenance

Upstream Node commit: `3a6b2fa5cf599ae579eb07f4d2977592b4d64ce0`.
The GitHub tag object for v24.11.0 reports a valid release signature; the exact
commit and file digests, rather than a floating tag, identify these vendored bytes.

- [acorn.mjs](https://github.com/nodejs/node/blob/3a6b2fa5cf599ae579eb07f4d2977592b4d64ce0/deps/acorn/acorn/dist/acorn.mjs):
  229792 UTF-8 bytes; Git blob `74d5fc431be2b5062245e72937ef2032164b51cd`;
  SHA-256 `b4c8c70200e72bae33cf1085e0ecb1e792c1b6924ed50cab817caf14f51bb249`.
- [LICENSE](https://github.com/nodejs/node/blob/3a6b2fa5cf599ae579eb07f4d2977592b4d64ce0/deps/acorn/acorn/LICENSE):
  1099 bytes; Git blob `9d71cc63a35e7d878787e5159b5b163739486220`;
  SHA-256 `76a876cf886ff9be2a8b5e2e86514fed06223c8c9f0c1e9ee9606e93841e00b7`.
- Node's adjacent package.json identifies the parser as Acorn 8.15.0.
  Acorn's own release source is commit `6dc537416ad628b3959b3ff963fbdcfdb380e0a3`.
  [Parser API](https://github.com/acornjs/acorn/blob/6dc537416ad628b3959b3ff963fbdcfdb380e0a3/acorn/README.md)
  explicitly recommends full parse instead of tokenizer-only heuristics.

## Third-party notice

Acorn is Copyright (C) 2012-2022 by various contributors and distributed under
the MIT License. The complete notice and license are retained in LICENSE.
The dependency files must remain byte-identical. Offline tests check both their
Git blob IDs and SHA-256 digests; update provenance and tests together if the
reviewed upstream changes. The local bounded Parser subclass is outside the
vendored file.

## Parse goals and boundaries

Each selected .js file is parsed as a complete ECMAScript 2025 script, then as a
complete module only if the script attempt reports SyntaxError. These are genuine
Acorn grammar goals, not loose recovery. This handles CommonJS/Electron script
chunks and ESM chunks without guessing their mode from a filename. Neither goal
permits imports everywhere, top-level returns, or undeclared private fields.
If both goals fail, the file contributes no keys, method counts or constructions.
Counters and the 15-second file deadline are shared across both attempts.

Parser-time limits cover 4,000,000 tokens, 1,000,000 node allocations, guarded
recursive depth 512, and the existing 16 MiB file limit. Range errors fail closed.
The harness separately applies a 512 MiB V8 old-space limit and 120-second process
timeout; an exhausted process is a failed inspection, never an empty catalog.

Only unescaped whole ASCII literal identities/direct keys enter the catalog.
Complete public literals can occur anywhere in a successfully parsed file; that
is source evidence, not proof of runtime use. Dynamic expressions are never
resolved into identities. Named getConfigOverrides/startupCommand definitions
also yield fixed path-shape/form/value labels and counts. Those summaries do not
assert reachability, execution, successful coercion, or plugin identity. The
unresolved count concerns recognizable plugin-prefixed compositions not mapped
to the eight pinned native boolean shapes. Unsupported/oversized compositions
can remain unclassified. The legacy three-entry lexical report is separate.

## Fixed resource failure reasons and omitted files

AST resource classification uses only these fixed codes:
`public_ast_bytes_limit`, `public_ast_token_limit`, `public_ast_node_limit`,
`public_ast_parse_depth_limit`, `public_ast_walk_depth_limit`,
`public_ast_walk_node_limit`, `public_ast_deadline`, and
`public_ast_range_error`. No source location, expression, identifier, measured
value, exception text or stack is emitted. These labels do not change any cap or
parser algorithm. A valid long binary chain can exceed guarded Acorn recursion,
while a long member chain can parse iteratively and exceed AST walk depth.
Neither example proves which bound an official package encountered.

Exactly six explicit file-local guards (token, node, parse depth, walk depth,
walk node, deadline) may omit that entire file and produce a partial catalog.
The file contributes zero sources, candidates, method counts or construction
rows, including work collected before the failure. Its read bytes still count
toward the existing aggregate source budget. Only independently successful
sources and validated marketplace manifests contribute positive evidence.

Successful reports include analysis.skipped with nonzero fixed reason/count
rows in this order: syntax_error, public_ast_token_limit, public_ast_node_limit,
public_ast_parse_depth_limit, public_ast_walk_depth_limit,
public_ast_walk_node_limit, public_ast_deadline. Counts sum exactly to jsSkipped;
a complete report has no rows. Counts do not reveal file names or source data.

Byte/read/UTF-8/archive/hash/provenance/global source/catalog/row/process failures
and bare RangeError still block the entire inspection and clear all evidence.
RangeError does not prove that a particular measured depth guard was reached.
Unexpected parser defects remain a fixed inspection failure, not partial syntax.
A partial catalog supports exact positive matches only; no match does not rule
out any key in omitted files, other field shapes or dynamic construction.
