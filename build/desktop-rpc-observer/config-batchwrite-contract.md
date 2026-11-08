# Test-only config/batchWrite request metadata

This observer addition describes an RPC request seen in a disposable synthetic
Desktop test. It neither admits, changes nor performs the write. The pump still
observes before forwarding the original bytes: a record is not proof of receipt,
a successful write, durable selection, or successful inference. In the 39a73bf4
run, both UI selections changed the draft UI state while the application showed
“Couldn't update model settings”; the existing RPC evidence records rejection.
The complete request parameters were not captured, so their shape remains unknown.

## Public schema and fixed observations

The source is the public Apache-2.0 protocol definition, not Desktop renderer code
or an upstream model record:
[ConfigBatchWriteParams / ConfigEdit](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L1191-L1211)
and [MergeStrategy](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L341-L344).

Only an existing correlated `to_bridge` request for exactly `config/batchWrite`
can receive `configBatchWrite` metadata. Duplicate/ambiguous JSON envelope fields,
invalid traces, notifications, reverse-direction requests and duplicate IDs do
not gain a shape. This does not change existing correlation or routing.

Schema 1 has `status` equal to `observed`, `unavailable`, or `limit`. Only
`observed` carries `shape`. “Observed” means a bounded shape was classified, not
that parameters conform to the protocol or that the write was accepted.

- `edits`: 0–8; `items` has exactly that many entries, preserving edit order.
- `duplicateKeyPath`: whether two decoded string key paths are exactly equal,
  including unknown key paths. No key-path text is retained in the output.
- `unknownTopLevel`: whether params contains a key outside the four public keys.
- Each item has `key`: `model`, `model_reasoning_effort`, or `other`;
  `valueType`: `missing`, `null`, `string`, `boolean`, `number`, `array`, `object`;
  `valueMatch`: `alpha`, `beta`, `none`, `other`; `mergeStrategy`: `missing`,
  `replace`, `upsert`, `other`; and boolean `unknownFields`.
- `alpha` and `beta` mean exact equality to the existing self-authored aliases
  `magpie/alpha/test-alpha` and `magpie/beta/test-beta`. `none` is the exact string
  `none`. Match labels are independent of the edit key and never imply admission.
- `filePath`: `missing`, `null`, `control_config`, `other`. The sole match uses the
  existing declared `control_home` plus `config.toml`, with the existing bounded
  ASCII Windows lexical comparison (slash/case normalization, no dot segments).
  This is not file identity, symlink resolution, existence, or permission. No new
  path is read, opened, hashed, or published. Without a declared root, no match
  is claimed.
- `expectedVersion`: `missing`, `null`, `string`, `other`. No version value,
  digest, equality claim, or additional session cache is introduced.
- `reloadUserConfig`: `missing`, `true`, `false`, `other`. Missing is not rewritten
  to false; a malformed value is not interpreted as a boolean.

Unknown keys/values/paths/versions are never copied into an observation or an
error. Each edit object is bounded to eight fields, params to sixteen fields;
duplicate envelope/params/edit-object JSON properties or a malformed object/edits
array produce only
`unavailable`. More than eight edits produce only `limit`, with no partial list.
The existing 64 KiB input-line, 64 KiB per-trace, 16-trace and 4096-character public
record caps are unchanged. The publication validator accepts only this nested
fixed schema in the exact request context, with bounded arrays, integers,
booleans and case-sensitive enums. No response, payload or raw error is added.

## Verification boundary

New offline Go tests cover exact enums, unknown-value canaries, duplicate JSON
fields and key paths, edit caps, optional fields, lexical path classification,
correlation/direction exclusions, and byte-identical forwarding even for malformed
write shapes. PowerShell fixtures cover publication shape/context/type rejection.
No new workflow, GUI action, RPC, write mode, permissions or process is introduced.
Go/gofmt, PowerShell and Windows were unavailable locally at candidate creation;
these test cases require actual CI execution and are not claimed to have passed.
