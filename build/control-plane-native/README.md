# Exact 0.167 native routing acceptance

This CI-only program exercises the compiled production adapter against the
unmodified Windows `codex-cli 0.167.0-alpha.3.2` executable bundled with the
verified Desktop package. It is separate from the retained Linux 0.166 tests.

## Inputs and invocation

Build `go build -trimpath -tags nogui -o control-plane-native.exe ./build/control-plane-native`.

The caller supplies the adapter, official executable, and the exact official
`codex-rs/models-manager/models.json` from commit
`c40729ea5cf8e947481b130c59807273eda2f8e9`.
The harness verifies native SHA-256
`40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`,
the native version, and catalog Git blob
`489ebe3f2483348641a16160ce8491dd077eabca` before use.
It selects the unchanged gpt-5.5 model record for native catalog loading; it
does not inject bridge model descriptors or invent native model metadata.

Dot-source `invoke.ps1`, then call:

```powershell
Invoke-ControlPlaneNative -HelperExecutable $helper -BridgeExecutable $bridge `
  -CodexExecutable $codex -CatalogPath $catalog `
  -ScratchParent $freshEmptyScratch -HelperHome $freshEmptyHelperHome
```

The two directory inputs must be empty, distinct, and non-overlapping. They
belong to the disposable CI job. Never use a user computer or credential store.
The wrapper does not download anything or start Desktop. The integrating job
owns preparation and deletion of its inputs. It must verify the original
signed package independently.

The wrapper clears the helper environment, suppresses stderr, bounds captured
stdout, and runs the `-supervise` watchdog. The watchdog uses
`internal/proc.StartTreeStrict` for its child and enforces 265 seconds.
The acceptance process enforces 240 seconds and also owns its adapter through
`StartTreeStrict`. Windows Job Object kill-on-close ownership propagates
cleanup when the wrapper terminates its owned watchdog at 270 seconds.
No executable-name or global process kill is used. The summary's
`adapter_clean_exit` means only the adapter exited with status zero after
frontend EOF. `Tree.Wait` sweeps its owned tree, and cleanup may forcibly end
owned descendants; this is not a claim of graceful native-process shutdown.

## Acceptance and boundaries

The logical sequence is initialize (with the actual separate control home),
config/read before catalog resolution, actual native model/list, config/read
with its resolved catalog default, two projectless threads selected by their
alpha/beta aliases, two interleaved assistant-text turns per thread, rejection
of opposite-backend model overrides, thread/read with logical providers, and
a final unchanged routing-default read. Each config phase makes separate
`includeLayers:false`, `true`, and `true` requests: nine reads in total, six
with layers, and three stable layer rereads. Every assistant completion is audited
at the reader boundary through adapter EOF. Exactly four distinct completions
must match the known threads, actual turn IDs and route-specific synthetic
answers. Extra, duplicate, unexplained or wrong-route completions fail even
when no waiter would have consumed them. The assistant count is derived from
that complete audit, not from successful waiter calls.

After the four completed turns, two nonempty `thread/list` queries exercise
the real adapter and both native backends. The first includes
`parentThreadId:null`; the second includes all three reviewed lineage keys
(`parentThreadId`, `ancestorThreadId`, `attachedToThreadId`) as null.
Both use `sourceKinds:["vscode"]`, `modelProviders:["magpie_bridge"]`,
`sortKey:"created_at"`, descending order and `limit:1`. Each must return
exactly the two native IDs obtained from `thread/start`, once each across two
one-row pages. The continuation request sends only the opaque frontend cursor,
and the second page must have `nextCursor:null`. Empty, missing, duplicate,
unknown or extra rows, wrong owner/model aliases, native providers and invalid
pagination fail. A present non-null model must match its known owner alias;
`model:null` is preserved as unavailable, as allowed by the pinned
[Thread schema](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread_data.rs).
The [loaded-thread enrichment](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/thread_enrichment.rs)
normally fills it from the live config, but this check does not require native
metadata availability or claim per-turn execution telemetry.
Each row must also retain source `vscode`, a nonempty
preview and `ephemeral:false`. No list error is retried or treated as empty.

The source selection is pinned, not inferred from the executable name:
the official [CLI app-server launch](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/cli/src/main.rs)
passes `SessionSource::VSCode`, and the [source filter](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/filters.rs)
maps `vscode` to it. The `appServer` filter instead selects `Mcp`.
The [native list processor](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/thread_processor.rs)
treats the three null lineage filters as no relation filter; the
[local store](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/thread-store/src/local/list_threads.rs)
uses ordinary scan-and-repair listing when `useStateDbOnly` is omitted.
Ordinary non-archived [state queries](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/state/src/runtime/threads.rs)
exclude empty previews and attached threads. Consequently the harness lists
only after real user turns and validates nonempty persisted-row eligibility;
it never reads the DB or manufactures rollout records to make listing pass.

The config checker owns an independent, fixed routing-view oracle. Its literal
JSON is recursively sorted and compact before SHA-256, following the pinned
[official fingerprint source](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/fingerprint.rs).
Its explicit primitive-leaf origin list includes decimal array indices and
excludes container origins. It calls no production bridge config, origin or
fingerprint helper. Each true response must have exactly one `sessionFlags`
layer, with `name`, `version` and `config`, no `disabledReason`, and config
identical to the effective owned routing view. All origins must identify that
same source and version. False responses must omit `layers` entirely while
retaining identical config and origins. Missing, null, extra and duplicate
fields, wrong sources, private/native fields and incorrect fingerprints fail.

This is the bridge's actual frontend session routing authority, consistent with
the pinned native
[service-owned session source](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/thread_config.rs).
It is not a native backend layer or an invented file. The fingerprint must
change exactly once when the actual complete model catalog supplies its default.
All fields and fingerprints must remain stable through the four routed turns.
Every read still requires fresh paired native requirements replies.

The strict success summary now requires `config_reads:9`, `layer_reads:6`,
`stable_layer_rereads:3` and `layer_version_changes:1`. The existing two catalog
models, two threads, four turns, four audited assistant completions, two thread
reads and two ownership rejections remain required. Listing additionally requires
`thread_list_queries:2`, `thread_list_pages:4` and `thread_list_items:4`:
two distinct known threads in each of two queries, not four distinct threads.
These add four frontend `thread/list` requests and no model requests.
No raw config, origin,
fingerprint, thread ID or response is written to the summary.

Native HOME, CODEX_HOME, control_home, and cwd are freshly created and separate.
The control home remains empty through the final read and has no fabricated config.toml. The fixture writes no auth.json,
copies no credentials, uses file credential storage and sets
requires_openai_auth=false for both distinct native providers.
Both model endpoints are numeric loopback HTTP SSE fixtures. Optional Apps,
plugins, cloud skills, updates, and analytics are disabled using ordinary
configuration. No model tool output, command, platform proof, account or
entitlement is synthesized. Every server-initiated RPC is a failure.

The SSE fixture reuses the working event sequence from
`../test-official-runtime.py`: response.created,
response.output_item.done (assistant output_text), and response.completed.
Each endpoint accepts exactly its two expected synthetic prompts, rejects
authentication/cookie/proof headers, unexpected methods/routes, oversized
bodies and cross-provider input, and retains only counts.

The program prints only the fixed summary schema, booleans, bounded counts,
known status/failure labels and the verified binary hash. It never prints
native diagnostics, configs, payloads, IDs, prompts, tokens or private paths.
Native errors and semantic differences fail acceptance; they are not retried
with weaker permissions or replaced with synthetic success.

This proves a narrow native protocol/assistant-text slice only. It does not
establish real account access, provider entitlement, company-policy support,
OS-enforced zero egress, or completed Desktop GUI compatibility. The parent
job keeps this report separate from its Desktop capture.

## Verification

`go test -race ./build/control-plane-native` covers local synthetic fixtures,
environment and input guards, exact pins, strict child lifecycle, unreviewed
RPC/tool rejection, bounded protocol and summary privacy. Config tests include
source, shape, primitive-origin and digest mutations, wrong fingerprints applied
consistently to all metadata, a changed later read, omission versus null, and
duplicate/trailing JSON. The hash oracle includes the native source's fixed
test vector and fixed initial/resolved routing-view fingerprints.
Thread-list tests independently check exact query/null filters, cursor-only
continuations, known IDs, owner/model aliases, nonempty row eligibility,
missing/duplicate/unknown rows, invalid cursors and immediate native-error
propagation. Summary tests reject missing or weakened list counts.
The real acceptance is never silently skipped by these tests; it is the
separate explicit Windows CI invocation above.

Source was prepared without an available executor. gofmt, unit/race/vet/build,
the full suite, and exact native runtime acceptance were not run during
authoring. Record the exact integrated head and real CI outcomes before
claiming success.


## Live Desktop fixture service (explicit opt-in)

`control-plane-native.exe -desktop-fixtures -codex <absolute pinned exe>
-catalog <absolute pinned catalog> -scratch-parent <fresh empty directory>`
prepares `bridge.json` inside that directory and serves two loopback Responses
fixtures for at most 180 seconds. Its stdout first emits a fixed `ready` JSON
record, then a final count-only audit. Keep stdin open while Desktop runs;
close it after stopping the owned Desktop/bridge tree. Any stdin byte, timeout,
or rejected request fails the audit. Zero requests is valid idle observation,
not conversation acceptance. `stopped` is never a Desktop pass verdict.

Run with the same minimal environment as the independent harness. The helper
verifies the exact executable hash/version and pinned public catalog, reuses
`nativeConfig` and `responseEvents`, and writes only fresh owned homes. The
caller owns process-tree supervision, bounds stdout/stderr, and deletes the
scratch directory after all children stop. Do not publish scratch, native
homes, bridge.json or raw protocol content as artifacts.

The internal bridge.json contains only the native backend entries, shared empty
workspace, separate control home and existing routing control-plane mode. The
integrator adds its explicitly reviewed Desktop startup/auth/feature opt-ins.
Use gateway_oauth_provider_name `Synthetic alpha_provider`, matching the actual
primary config display name. The native approval `never` and sandbox `read-only`
settings are retained. If the null-only startup profile rejects these settings,
stop and report the real incompatibility; do not replace its policy snapshot,
remove safeguards, fake authentication or bypass the UI.

Desktop, not this helper, sends initialize/model/thread/turn RPCs through the
production bridge. This helper does not drive onboarding, submit a turn, or
assert native thread IDs/assistant completions. A separate Desktop/bridge audit
must establish any claimed UI or conversation result. Unsupported RPCs remain
honest errors.

User messages must match `synthetic-alpha-[A-Za-z0-9 -]{1,80}` or the corresponding
beta prefix at their own endpoint, with no predetermined order. Only gpt-5.5 is
accepted, at most 16 requests per endpoint. History accepts only text messages;
assistant history must match that route's fixture answer. Native system and
developer text is permitted; tool declarations may be sent by native, but tool
call/results and image/URL input types are rejected. User prompts cannot contain
URLs. Native-generated system text is not an external URL fetch instruction to
the helper, which never fetches URLs. It retains no request content and emits
only the existing text-only SSE response. This is not OS-enforced zero egress.

Strict native input validation can expose an additional native context message
that is not yet reviewed; rejection is a blocker, not permission to broaden the
allowlist. The original independent harness and its exact turn/count assertions
remain unchanged. New source tests cover unsequenced synthetic prompts, route
and input guards, auth headers and request bounds. Go formatting, compilation,
race tests and live Desktop startup have not been run during source authoring.


The live fixture's input-message allowlist is sourced from pinned
[`protocol/src/models.rs`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/protocol/src/models.rs):
`ResponseItem::Message`, `ResponseInputItem::Message`, `ContentItem` and
`MessagePhase`. Unknown message/content keys fail closed. Optional `id`/`status`
are bounded strings or null, and phase is absent/null or one of the three native
variants. Encrypted content and internal passthrough metadata must be absent or
null. Output-text annotations/logprobs must be absent/null/empty. Nonempty native
metadata is deliberately unsupported pending review, not silently dropped.
User/system/developer content is input_text; assistant history is output_text.
The model fixture's own answers need no opaque metadata.

Native system/developer text may include URLs or instructions. These strings
are not verified synthetic and are never logged, relayed, fetched or executed
by this helper. The prohibition is on URLs in synthetic user prompts and on
nontext/tool input payloads, including unknown fields hidden alongside text.
Native tool declarations remain inert. Counts do not prove a wholly synthetic
native envelope or its origin.

Paths are deterministic directly under ScratchParent: alpha-home, alpha-codex,
beta-home, beta-codex, workspace, control-home, version-home, version-codex,
models.json and bridge.json. Readiness also contains fixed native_sha256 and
catalog_blob pins. No command/path authority is emitted in readiness. Stop the
helper only after verified Desktop activation exit; that event alone does not
prove all native descendants exited. The external process owner must supervise
its complete tree and report locked-file cleanup failures rather than mask them.
