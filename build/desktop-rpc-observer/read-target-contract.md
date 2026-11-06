# Test-only project and file-read request metadata

This observer extension accompanies the next meaningful CONTROLPLANE config/read
CI run. It does not introduce a standalone diagnostic run, route either method,
read a target file, or establish Desktop, real-account, inference, tool, or
corporate-policy acceptance. Production bridge routing is unchanged.

## Pinned evidence and boundaries

The request source oracle is openai/codex
`c40729ea5cf8e947481b130c59807273eda2f8e9`:

- [ProjectListParams and ProjectSortKey](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/project.rs).
- [SortDirection](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs).
- [FsReadFileParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/fs.rs).

The target remains the signed no-account Desktop 26.1103.3517.0 with backend
0.167.0-alpha.3.2 in disposable cloud CI. This source evidence is not evidence
that Desktop actually sent a candidate request. Windows CI is not network-none.

The new implementation is [read_target_metadata.go](read_target_metadata.go),
called only from [observer.line](main.go) for a correlated `to_bridge` request.
It reuses the existing strict envelope, decoded-duplicate, native ID, JSON
string and native trace guards in
[auth_request_metadata.go](auth_request_metadata.go) and
[read_request_metadata.go](read_request_metadata.go).
Responses, notifications, wrong directions, ambiguous envelopes or IDs,
duplicate/quarantined IDs, and disabled correlation receive no new request
metadata. A malformed trace suppresses the parameter metadata and retains only
the existing fixed `tracePresent:true, traceValid:false` pair. Missing trace
adds no trace fields. Invalid params in an otherwise eligible request produce
only the method's false validity field, never partially recognized values.

## Exact project/list publication schema

An eligible request has strict object params with only `cursor`, `limit`,
`sortKey`, and `sortDirection`. Decoded duplicate keys, unknown keys, invalid
Unicode, and wrong types fail classification. A single escaped spelling of a
known field or enum keeps normal JSON semantics.

- `projectListParamsValid`: boolean.
- Only when true: `projectListCursorProvided` and
  `projectListLimitProvided`: booleans.
- Only when true: `projectListSortKey`: exactly `absent`, `position`,
  or `recencyAt`.
- Only when true: `projectListSortDirection`: exactly `absent`, `asc`,
  or `desc`.

Cursor and limit absent/null mean not provided. Cursor must otherwise be a
native-valid JSON string. Limit must otherwise be an integer token in
0..4294967295; negatives, negative zero, fractions, exponents, strings and
overflow are rejected. No cursor or numeric limit value, length or digest is
published. Sort absent/null uses the fixed `absent` label rather than guessing
a resolved default. A non-null direction requires a non-null sort key,
following the pinned request contract.

## Exact fs/readFile publication schema

Params must be exactly one `path` field whose value is a native-valid JSON
string with at most 4096 decoded UTF-8 bytes.

- `fsReadParamsShapeValid`: boolean. True means only this bounded JSON shape.
  It does **not** mean native AbsolutePathBuf validity, filesystem existence,
  ownership, permission, safe access, or an authorized read.
- Only when true: `fsReadPathForm`: exactly `empty`, `drive_absolute`,
  `unc_absolute`, `drive_relative`, `rooted`, `relative`, or `device`.
- Only when true: `fsReadBasename`: exactly `config.toml`, `AGENTS.md`,
  `SKILL.md`, `auth.json`, or `other`.
- Only when true: `fsReadLexicalRoot`: exactly `unconfigured`,
  `unclassified`, `outside`, `ambiguous`, `primary_home`,
  `primary_codex_home`, `secondary_home`, `secondary_codex_home`, `control_home`,
  `workspace`, or `verified_package`.

Path form is a spelling category. Empty, relative, malformed absolute and
device-like strings can pass JSON shape classification without matching a
root. Basename compares only the last slash/backslash-separated component,
using ASCII case folding and a fixed output vocabulary. It does not trim,
resolve dot components, remove streams, or publish an unknown basename.

## Observer-only root input

`MAGPIE_CI_READ_TARGET_ROOTS` is an optional strict JSON object of at most
16 KiB, with one to seven of these exact keys:

- `primaryHome` -> `primary_home`
- `primaryCodexHome` -> `primary_codex_home`
- `secondaryHome` -> `secondary_home`
- `secondaryCodexHome` -> `secondary_codex_home`
- `controlHome` -> `control_home`
- `workspace` -> `workspace`
- `verifiedPackage` -> `verified_package`

Each value is an explicit harness-generated absolute Windows drive or UNC
root string, at most 4096 decoded UTF-8 bytes. Omit roots the harness has not
explicitly established. The control home is the harness-created frontend state
root, separate from both native child homes, both native CODEX_HOME roots, and
the workspace. Nothing is inferred from HOME, CODEX_HOME, cwd, argv,
the registry, or a target request. The harness must serialize the object as
JSON rather than concatenate unescaped paths, pass it only to the test
observer, and never print it. The label `verifiedPackage` records the harness's
declaration; the observer performs no package verification.

Absent input leaves roots unconfigured. Present empty, malformed, oversized,
unknown-field, duplicate-field, wrong-type, or unsafe-root input fails observer
setup with the existing fixed diagnostic and exit 125. No path, value, length,
digest, parser error, or exception is printed. The variable, including any
case-insensitive spelling, is removed from the child's environment before
launch. Child argv and wire streams are unchanged.

## Lexical comparison, not filesystem authority

Comparison is deliberately conservative and platform-independent:

1. Forward slash and backslash are equivalent, and ASCII A-Z folds to a-z.
   Non-ASCII bytes are excluded from root matching rather than approximating
   Windows Unicode casing. JSON string shape can still be true for Unicode.
2. Only ordinary drive-absolute and UNC spellings participate. UNC requires
   server and share components. One terminal root separator is accepted;
   repeated separators and terminal separators on file targets are rejected.
3. Device/extended prefixes, drive-relative/root-relative paths, dot or parent
   traversal, empty components, alternate data streams, wildcard/reserved
   punctuation, control bytes, trailing dots/spaces, tilde/short-name spellings
   and reserved DOS devices are excluded from matching.
4. A target equals a root or begins with that root at the next component
   boundary. Prefix siblings do not match. The longest matching root wins,
   so a codex home nested in a home is labeled as the codex home. If multiple
   equally specific declared roots match, the result is `ambiguous`,
   independent of input/map iteration order.
5. An unsafe or unsupported spelling produces `unclassified`, even without
   configured roots. A comparable target with no roots gives `unconfigured`;
   a comparable target matching none of the supplied roots gives `outside`.

No symlink, junction, hardlink, mount, short-name or actual filesystem identity
is resolved. Even a lexical root match or `outside` result says nothing about
the physical target, ownership, authorization, or real containment. No target
I/O, stat, realpath, content read or target/root hashing occurs.

## Verification

[read_target_metadata_red_test.go](read_target_metadata_red_test.go) uses only
observer APIs available at base
`70e603d8481c28b9f84cadf2f7183255044e11b4`. Copy that file alone to the base and
run `TestProjectListMetadataRed`, `TestFSReadTargetMetadataRed`, and
`TestReadTargetEnvironmentMetadataRed` for assertion-level regressions, then
run them with the implementation.

[read_target_metadata_test.go](read_target_metadata_test.go) covers strict JSON,
decoded duplicate aliases, Unicode, trace and ID guards, optional values and
u32 bounds, path/config byte bounds, all fixed labels, prefix siblings,
overlapping roots, reserved devices and path aliases, private canary omission,
unchanged chunked wire bytes and caps, and an owned native fixture child that
checks root-env stripping and byte-exact stdout/stderr.

Authoring used GitHub connectors only. Go formatting, unit/behavioral-red tests,
race/vet/build checks and actual Windows execution were **not run while
preparing these source blobs** because no execution environment was available.
Run the applicable checks in the existing exact-head CI under disposable HOME
with pinned Go caches. This change creates no extra diagnostic workflow/run.
