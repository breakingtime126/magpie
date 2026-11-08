# Exact 0.167 native routing acceptance

This CI-only program exercises the compiled production adapter against the
unmodified Windows `codex-cli 0.167.0-alpha.3.2` executable bundled with the
verified Desktop package. It is separate from the retained Linux 0.166 tests.

## Self-authored synthetic-model profile

`-acceptance-profile synthetic-models-4239` is a separate, explicit model
fixture mode on the already reviewed 4239 native pin. It neither changes the
omitted/default profile nor enables Desktop. It rejects every `-catalog`
argument, including an explicitly empty value. The PowerShell wrapper likewise
rejects any supplied `-CatalogPath` before file access. Unknown mode strings
fail rather than choosing the legacy catalog path.

[`selfAuthoredCatalog`](synthetic_models.go) writes a new single-record catalog
for each backend: alpha owns `test-alpha`, beta owns `test-beta`. Each record
and its short instruction are authored for this loopback test from public
`ModelInfo` / `ModelMessages` types at OpenAI Codex commit
`c40729ea5cf8e947481b130c59807273eda2f8e9`; no bundled catalog, complete hosted
model record, old candidate or catalog hash/golden is needed by this mode.
The two files are separate, created exclusively in this invocation's fresh
owned root, and passed as absolute `model_catalog_json` paths.

The records describe text input, disabled shell, empty experimental tools and
service tiers, and only the public `none` reasoning effort. Apps/skills/plugin
instruction insertion, verbosity, search and reasoning summaries are disabled
in this fixture metadata. `supported_in_api=true` means only that our local
Responses endpoint accepts the record's slug; it is not an entitlement or
hosted-model capability claim. No guardian, approval-coverage or permission
policy is supplied by the records. Native configuration retains the exact
existing isolated `read-only` / `never` test settings, changing only the model
selection and the independently generated catalog path.

The existing compiled-adapter acceptance sequence is reused. It checks actual
native model/list IDs, aliases, display names, explicit visibility, sole alpha
default and the exact none-effort option. Config/session-layer fingerprints,
two projectless threads, interleaved two turns per thread, real thread/list,
owner-switch rejection, assistant completion auditing and owned cleanup remain
required. New-mode thread listings must contain the exact original owner's
model rather than an unavailable/null model.

Each loopback endpoint verifies its own original slug and short self-authored
instruction, rejects other reasoning efforts and nontext input items, and
returns only the existing fixed assistant-text SSE. Tool declarations in a
native request are inert, not proof of execution; the fixture never emits a
tool call. Request bodies, native instructions, environment values and native
errors are not logged or persisted. A schema match alone does not prove
runtime selection, Desktop visibility, real-model behavior or zero egress.

The self-authored request oracle follows the pinned c407
[`core/src/client.rs:1000-1019,1075-1085,1156-1163`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core/src/client.rs#L1000):
the exact owned sentence appears once as a developer message's sole input_text
part, not in a top-level instructions field. The other endpoint's sentence,
wrong roles, duplicates and appended text are rejected. This fixture explicitly
sets none effort and disables summaries, so its request reasoning must be
exactly `{"effort":"none"}`; missing/null reasoning and any summary field are
not alternatives. Other ordinary native context text remains allowed without
logging it. Existing opaque/tool/media rejection is unchanged; no additional
output annotations or logprobs forms are admitted. Pure tests cover these wire
forms, but the corrected oracle still requires actual native acceptance.

Reports retain the existing fixed field/count schema, but their scope is
`self-authored-models-loopback-only`; old-profile consumers reject that scope,
and this profile rejects legacy-scope reports. The outer preparation report
uses profile `synthetic-models-4239` and scope `synthetic-models-4239-only`.
All unknown RPCs, native errors, missing/changed metadata, fixture violations
and cleanup failures remain errors. Nothing falls back to the old catalog.

The existing standalone workflow selects this path only when the authorized
push message contains both `[app-server-4239-v1]` and
`[synthetic-models-v1]`. The first marker retains the existing exclusion from
unrelated Desktop jobs. The native step passes the new explicit profile to
`ci-4239.ps1`: it verifies/extracts the same signed official runtime but skips
the legacy catalog download entirely. Without the second marker, the old
standalone profile and its behavior remain unchanged.

Offline coverage includes a named old-parser behavioral red, self-authored
record/alias/request negative tests, untouched native-config comparison, scope
separation and PowerShell catalog-presence/CI-branch guards. At preparation,
only source/static checks were available: Go, gofmt, PowerShell and official
Windows native execution were not run locally. Actual CI results at the exact
published commit are required before claiming four-turn native success.

## Separately reviewed Desktop fixture build

The independent four-turn acceptance defaults to the
original native SHA-256 `40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`.
The separate, explicit [standalone 4239 profile](#standalone-4239-app-server-profile)
below is the only additional acceptance pin. Neither path accepts an arbitrary
build or hash.

Only `-desktop-fixtures` accepts the explicit option
`-reviewed-build desktop-4239`. It selects native SHA-256
`4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382`
from the independently reviewed signed Desktop 26.1103.4239.0 package. An empty
option retains the old pin; every other value fails before fixture setup.
The helper verifies file bytes before `--version`, retains the exact
`codex-cli 0.167.0-alpha.3.2` check, and includes the selected hash in both
backend configuration records and readiness. The caller must separately
verify the package/native/ASAR tuple and require the same selected pin when
reading these records. No caller-supplied hash is accepted as a profile.

This option only prepares the existing private loopback fixtures for an
explicit zero-input Desktop bridge observation. It does not exercise the
independent four-turn inference or tool-acceptance sequence, establish new
binary inference compatibility, or broaden any bridge protocol/policy gate.
See [the Desktop build review](../../docs/subsystems/desktop-cold-start.md#reviewed-4239-bridge-observation-profile)
for the exact opt-ins and limits.

## Inputs and invocation

Build `go build -trimpath -tags nogui -o control-plane-native.exe ./build/control-plane-native`.

The caller supplies the adapter, official executable, and the exact official
`codex-rs/models-manager/models.json` from commit
`c40729ea5cf8e947481b130c59807273eda2f8e9`.
By default the harness verifies native SHA-256
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

### Standalone 4239 app-server profile

Only an explicit `-AcceptanceProfile app-server-4239` on the wrapper (or
`-acceptance-profile app-server-4239` on the helper and its supervisor) selects
the separately reviewed Desktop 26.1103.4239.0 native executable. Its exact
SHA-256 is `4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382`.
The caller still independently verifies the signed package identity before
supplying the native file. The version and public catalog pins stay unchanged.

```powershell
Invoke-ControlPlaneNative -HelperExecutable $helper -BridgeExecutable $bridge `
  -CodexExecutable $codex4239 -CatalogPath $catalog `
  -ScratchParent $freshEmptyScratch -HelperHome $freshEmptyHelperHome `
  -AcceptanceProfile app-server-4239
```

An omitted or empty option preserves the original pin. Every other nonempty
value, including a hash, case variant or Desktop-only `desktop-4239`, is
rejected before runtime setup. Conversely the Desktop fixture parser does not
accept `-acceptance-profile`. The selected pin controls both file checks,
both native backend startup pins and both supervisor/wrapper summary checks.
The fixed summary schema and every acceptance count are unchanged; a report
for the other pin is invalid, even with the same native version string.
Default summary consumers, including the existing Desktop gate, still accept
only the old pin. Keep this standalone report separate from Desktop evidence.

This profile enables an attempt, not a compatibility assertion. The exact
same four-turn, thread-list, config/layer, isolation, no-auth fixture and
cleanup checks below must pass on the selected executable. A native denial,
unsupported request or semantic difference stops the attempt. Do not change
permissions, sandbox mode or expected responses to make it pass. No Desktop
process, UI navigation, account, real provider or tool task is accepted by
this interface-only result. The profile was prepared locally; native 4239
acceptance and GUI compatibility are not established by this change.

The separate [standalone CI workflow](../../.github/workflows/experimental-control-plane-native-4239.yml)
is push-only on `experiment/app-server-bridge-v0.1.3035`. Its paths are limited
to that workflow, `build/control-plane-native/**` and the focused synthetic
`internal/appserverbridge/experimental_feature_run_test.go` fixture; its job also requires
the explicit commit marker `[app-server-4239-v1]` and rejects combined Desktop
or inspection markers. No marker means no standalone acceptance. Do not add
the marker until this exact attempt is wanted. A future repeat must touch a
watched path; an empty marker-only commit does not satisfy the path filter.
The workflow checks out the triggering SHA, requires an exact-base behavioral
red and offline synthetic tests, then uses a fresh Windows runner.

`ci-4239.ps1` requires the exact profile and that CI context. It imports only
the existing signed-package identity function definitions, downloads bounded
public package/catalog inputs, verifies real SDK and Authenticode signatures,
and checks the fixed package/version/native/ASAR tuple. It then rehashes the
locked package and extracts only `app/resources/codex.exe`. There is no package
registration, service setup, Desktop launch, registry edit or security-setting
change. Missing dependencies or an updated floating package stop the attempt.
The selected binary and catalog are reverified by the native harness.
The preparation directory is deleted after the owned acceptance processes stop.

Only `app-server-4239-report/report.json` is published. It wraps the strictly
validated native summary with fixed preparation status, source commit,
`package_verified`, `preparation_cleanup_complete` and `gui_verified:false`.
No package, native homes, raw errors, protocol/config or screenshots are uploaded.
This workflow never runs the old-pinned acceptance as a substitute. The existing
policy-trace, cold-start and generic bridge/native-runtime jobs explicitly skip this
marker on push only; their dispatch behavior stays unchanged. The remaining
Desktop workflows' path filters do not match this patch. Static fixtures guard
those exclusions and reject every conflicting Desktop/inspection marker.

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

The logical sequence is initialize (with the actual separate control home and
complete initial native catalog), config/read with the genuine catalog default,
an explicit native model/list refresh, config/read with the same default, two projectless threads selected by their
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

The actual `model/list` response must have exactly two rows, an explicit null
`nextCursor`, and distinct exact `id`/`model` pairs
`magpie/alpha/gpt-5.5` and `magpie/beta/gpt-5.5`. Both must explicitly report
`hidden:false`, with exactly one genuine boolean `isDefault:true`. Their
distinct display names must be `GPT-5.5 [alpha]` and `GPT-5.5 [beta]`: the
[pinned native record](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/models-manager/models.json)
supplies `GPT-5.5`, and the [existing bridge merger](../../internal/appserverbridge/router.go)
appends each owner. No returned label or metadata is changed by this check.
Missing/null/mistyped fields, duplicate JSON keys, collapsed IDs or labels,
hidden rows, unknown aliases and incomplete pages fail. These assertions prove
interface identities and routability, not that Desktop rendered both choices.

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
It is not a native backend layer or an invented file. The first visible config
already includes the default supplied by the completed model catalog. Refreshing
the unchanged default and completing the four routed turns must preserve all
fields and fingerprints. A genuinely changed catalog default changes the layer
version, as covered by separate bridge unit tests.
Every read still requires fresh paired native requirements replies.

The strict success summary now requires `config_reads:9`, `layer_reads:6`,
`stable_layer_rereads:3` and `layer_version_changes:0`. The existing two catalog
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
test vector and fixed routing-view fingerprints. The unchanged native refresh must retain
the initial resolved layer version; bridge unit tests separately verify that a
changed real default changes the version.
Thread-list tests independently check exact query/null filters, cursor-only
continuations, known IDs, owner/model aliases, nonempty row eligibility,
missing/duplicate/unknown rows, invalid cursors and immediate native-error
propagation. Summary tests reject missing or weakened list counts.
The real acceptance is never silently skipped by these tests; it is the
separate explicit Windows CI invocation above.

`go test -race ./build/control-plane-native -run TestStandalone` covers exact
default/explicit profile selection, invalid options and cross-profile or
weakened summaries. `acceptance_profile_red_test.go` uses only the original
base APIs; run that file alone on commit
`5a9e2f9e49bc00bd87bffd1385d7f4469389f838` to require the named opt-in assertion
to fail, not compilation. `pwsh -NoProfile -NonInteractive -File
build/control-plane-native/acceptance-profile.tests.ps1` exercises only the
pure wrapper validator and source argument wiring. It starts no process.
Existing Desktop profile fixtures must still pass without a profile argument.
The CI also runs `catalog_identity_red_test.go` alone on the exact old base and
requires its collapsed-ID/label assertion to fail behaviorally. Catalog identity
mutations and workflow routing/static path exclusions then run with the full
synthetic helper suite on the candidate.

The same isolated CI run also verifies the synthetic feature-response fixture
race correction without changing production routing. It copies the candidate
fixture into the exact old checkout and applies the two-anchor
`mutate-shared-response.py` mutation there only. The delayed native-child test
must fail its generation assertion, not compilation. The unmutated candidate
then repeats fresh/failure pages, delayed-sibling delivery and immutable
generation tests under Linux `-race -count=5` and Windows `-count=20`. Both use
fresh isolated homes with tool caches resolved before HOME changes. These are
synthetic subprocess tests, not additional official native or Desktop evidence.

The 4239 profile and CI extension were prepared locally without a Go toolchain
or PowerShell. gofmt, unit/race/vet/build, PowerShell fixtures and exact native
runtime acceptance were not run for this candidate. Record the exact
integrated head and real CI outcomes before claiming success. Earlier results
for another pinned executable do not establish 4239 acceptance.


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

### Self-authored Desktop model-menu experiment

The preceding independent native acceptance passed at exact commit
`25a7517ef35495ea8d5549f854e2ecbed1cda9e0` in
[run 42411936151](https://github.com/breakingtime126/magpie/actions/runs/42411936151),
artifact `13589902596`. Its scope is `self-authored-models-loopback-only`:
2 models, 2 threads, 4 turns, alpha/beta requests 2/2, 2 owner-switch rejections,
0 fixture errors, and `adapter_clean_exit`/`cleanup_complete` true. This is
native synthetic evidence only. That native result does not establish GUI visibility or chat. The later
Desktop observation is bounded as described below.

The separate Desktop fixture flag `-fixture-profile synthetic-models-4239`
requires `-reviewed-build desktop-4239` and rejects any `-catalog` occurrence,
including an explicit empty value. It reuses `selfAuthoredCatalog` verbatim for
independent `test-alpha` and `test-beta` records. Each native child receives only
its own fresh catalog path and model slug. No legacy catalog is read/downloaded,
no frontend catalog or config file is invented, and native permission/auth
configuration is unchanged. The old Desktop profile remains separately opt-in.

Readiness uses scope `desktop-self-authored-fixtures-only` and
`catalog_source: self-authored-test-models-v1`, not a legacy catalog digest.
The final fixture audit uses the same distinct scope. The self-authored
Responses oracle also requires the route's actual model, self-written
instructions in the reviewed developer/input-text position and only supported
`none` effort through the same `selfAuthoredRequest` helper. Existing Desktop
plaintext, no-auth-header and request-count bounds remain. Successful menu-only
audit additionally requires zero Responses requests at both endpoints.

The existing policy-trace workflow selects this mode only with the additional
`[desktop-synthetic-models-v1]` marker and the existing 4239 welcome/official-tool
markers. The mode skips the legacy catalog download, rejects a supplied
`NativeControlCatalog`, and does not run the separate legacy native acceptance
at the end. Native four-turn evidence and actual Desktop evidence remain
separate.

This experiment retains the existing owned-window welcome and model-menu
observation path. Its new profile recognizes only `Synthetic Alpha [alpha]`
and `Synthetic Beta [beta]` labels; only the existing menu-control keyboard
activation is permitted. It does not select a model, submit a message, complete
additional setup, change security settings or prove real-provider access.
Hidden, disabled, ambiguous, login/protected or unreviewed UI remains a stop,
not permission to guess another action. Screenshots and exact bounded counts
must be reviewed before any next action is proposed.

The test-only RPC observer recognizes the two exact self-authored
id/model/display-name tuples and emits only existing fixed counts. Other
identities remain unknown and every forwarded byte remains unchanged. These
counts describe a received `model/list` response, not visible or usable UI.
Distinct source slugs are a minimal follow-up experiment, not a proven cause
of the earlier first-menu observation. No Desktop run is authorized merely by
preparing this source; first require the separate new-model native acceptance.

The menu-only follow-up recognizes the exact current-model Button label
`Synthetic Alpha [alpha] None` observed in Desktop run
[42413441847](https://github.com/breakingtime126/magpie/actions/runs/42413441847)
at `8c9bd8d617981b05c1a01cf8db1e3e0709742373`. This label is profile-local and
only identifies the same collapsed menu opener; `None` is not reported as
Medium. It does not expand model-item counts, submenu names or keyboard model
activation. Exact typed resolution, unique visible enabled Button identity and
collapsed-state checks still precede Expand. The preceding run stopped before
any menu action; this follow-up has not yet established two visible menu items.


### Bridge-owned alias catalog: separate menu-only slice

`-fixture-profile bridge-owned-catalog-4239` is a separate explicit opt-in with
`-reviewed-build desktop-4239`. It rejects any `-catalog` occurrence, including
an empty one. Only `selfAuthoredCatalog` supplies its definitions. The two native
catalogs, raw model slugs, endpoints, permission/auth configuration and private
homes remain unchanged. The fixture additionally creates the actual
`control-home/bridge-model-catalog.json` and sets `bridge_model_catalog_json` in
its private bridge configuration. Only `slug` and `display_name` are mapped to
the public aliases and existing owner-labelled names; every other authored
field, including the short original instructions, stays unchanged. No bundled,
legacy, downloaded or recovered model record is an input.

This is a real bridge startup contract, not a placeholder path for the UI.
The bridge loads a bounded strict two-record file, binds its file identity,
size and SHA-256, and validates all 19 public native Model fields after both
complete native catalogs arrive. Routes, default and the owned session layer
publish together only on agreement. The path, digest and explicit bridge-owned
provenance then appear in actual `config/read` responses, still gated by both
fresh native requirements checks. This does not claim native configuration or
that model/list verified instruction/tool fields not exposed by that RPC.
File changes or failed/mismatched catalog refreshes close the connection and
require restart. Existing thread ownership is never rebound. `fs/readFile`,
config writes, authentication/setup and permission boundaries are unchanged.

Readiness has seven fixed fields with scope
`desktop-bridge-owned-catalog-fixtures-only`, catalog source
`bridge-owned-self-authored-test-models-v1` and the actual
`owned_catalog_sha256`. It proves file creation, not native validation.
The test-only observer receives an explicitly declared private path/hash;
correlated config responses emit only path-match, digest-match and validated
provenance booleans. The observer never opens that file. An fs/readFile request
can record only whether its path matches this known file; the bridge still
rejects the RPC. The main harness independently checks the created file and
keeps config publication evidence separate from GUI evidence.

The existing policy-trace workflow requires `[bridge-owned-catalog-v1]` plus
`[desktop-synthetic-models-v1] [bridge-welcome-4239-v1] [bridge-ui-4239-v1]
[winapp-inspection-v0.7.1]`. It uses the existing welcome/menu-only path and
requires zero Responses requests. It does not select a model, send a message,
operate setup, add filesystem RPCs or run the legacy catalog path. An unknown,
hidden, disabled, ambiguous or protected target remains a stop.

Evidence boundary: at `75908967cf18bd43fa912b8462244ea3869a1e63`, Expand
opened only the first observed Select model popover, where Alpha was observed.
The later script phase named `submenu` did not establish a real second level;
no expandable target was found, Tab did not advance focus, deeper navigation
was unverified, and no model was selected. That is not proof that the complete
picker excludes Beta. Public catalog documentation and an independent reported
remote-to-local catalog workaround motivate this separate contract experiment,
not a causal diagnosis. If this slice does not obtain two-label evidence it
stops. If it does, it shows only improved visibility under this exact version
and contract, not the prior root cause or successful Desktop conversation.

Local preparation of this slice did not run Go, gofmt, PowerShell, Windows or
native/Desktop acceptance. Its exact commit must pass CI before any result is
claimed. The default and earlier explicit profiles remain separate.

### Explicit current-draft model-selection follow-up

The `af72a101a0c1e018fe3ef9138b8b7c4ede8fc1ac` Desktop run
[42420558493](https://github.com/breakingtime126/magpie/actions/runs/42420558493)
actually displayed both original Alpha/Beta labels in the same popover, one
visible interactive instance each, and the pixels showed Alpha checked. The
fixture audit recorded zero requests/errors. The overall report was blocked by
an overly narrow config-read evidence validator; that result is retained, not
rewritten as a full pass. A separately reviewed report-only fix admits the
existing fixed owned-catalog response metadata under its explicit profile.
Neither observation proves SelectionItem support, persistence or conversation.

A further, independent PowerShell option
`-DesktopModelSelectionProfile bridge-owned-model-selection-4239` requires the
existing `bridge-owned-catalog-4239` fixture, all its markers and the additional
`[bridge-model-selection-v1]`. Empty/default leaves the earlier menu-only path
unchanged. No new Go/native/production mode is introduced and no external
catalog is accepted. Only after a confirmed captured two-label menu can this
separate stage run, with its own bounded 30-second budget, at most two explicit
SelectionItem `select` attempts and at most two ExpandCollapse `expand` calls.
No action is retried. The sequence is fixed Beta then Alpha.

Every model target must be the exact unique visible enabled RadioButton or
MenuItem and have a valid runtime selector, or obtain one through an exact typed readonly lookup.
Before any selection, a live `IsSelected` read must return precisely the string
`True` or `False` with the same identity. The pinned official tool obtains this
property from SelectionItemPattern; missing support can return null even with
exit code zero and is a stop, never false-by-default. The explicit select calls
only that pattern. No automatic invocation, ancestor fallback, keyboard input
or coordinate click is used. Held window/package/process/foreground checks
surround each command, retaining the existing disclosed non-atomic identity
limitations rather than claiming an action-instant proof.

A successful action receipt is insufficient. Any observed model-settings error
stops the slice even if the checkmark and current label change. A fresh tree must show the exact
current-model Button (`Synthetic Beta [beta] None`, then
`Synthetic Alpha [alpha] None`). For this read-only label check the Button need not be enabled or invokable.
If collapsed, that unique enabled, directly invokable Button may be reopened once through explicit Expand; if already expanded it is not acted
on. Another fresh pair of IsSelected reads must show the chosen model true and
the other false, followed by a verified screenshot. The Beta button spelling is
an exact prospective assertion, not a previously established UI fact. Any
unsupported type/pattern, hidden/disabled/duplicate target, stale identity,
unchanged label/state, visible protected/error screen, timeout or uncertain
receipt stops the slice. Fixed result observations distinguish target/type,
selector and selected-state availability without exporting arbitrary UI text.

The result scope is `bridge-owned-current-draft-selection-only`. It establishes
only current draft selection, not saved configuration, thread migration or a
message. The existing zero-request fixture audit remains mandatory. Composer,
send, Windows setup, authentication and permissions are untouched. Production
write and other RPC denials remain unchanged; existing terminal sanitized RPC
traces record them without claiming they were caused by a selection. There is
no runtime trace polling or new observation framework. Menu evidence, selection
evidence and the separate native four-turn acceptance remain distinct.

#### Selection snapshot stop and narrow role compatibility

Run [42423425288](https://github.com/breakingtime126/magpie/actions/runs/42423425288)
at `9ed91ec7849aa07e97c2f501008cd0ee8edc3691` again captured both menu labels,
but selection stopped after its first successful tree read: zero selections,
zero reopens, and no target or IsSelected read. Its default target observation
is not evidence of a missing RadioButton or missing SelectionItemPattern. Equal
stdout byte counts in consecutive reads do not prove identical content, and the
raw tree was not retained. The precise failure remains unknown.

The bounded follow-up distinguishes the read, common schema, fixed-name scan,
pending-target guard, and scene stages. Only counts for the four already-known
names leave the helper: total, hidden, disabled, direct-invokable and fixed
RadioButton/MenuItem/Button/Text/other counts, plus heading/protected flags.
Incomplete statistics are explicitly marked, not interpreted as absent targets.
The first snapshot gates only the two model targets; a current-model opener is
not an action target at that point. A hidden/disabled/duplicate current opener
therefore does not prevent model-state reads. Actual collapsed-button reopening
still requires its exact unique visible enabled directly invokable identity and
explicit Expand. An already-expanded button is only read for its label, including
when disabled; it is never acted on in that state.

The public pinned official tool selects through SelectionItemPattern rather than
requiring one UIA control type. This profile therefore admits an actual unique
RadioButton or MenuItem, preserves that exact type/name/runtime slug through
state reads, and requires literal IsSelected True/False before the same explicit
Select. A MenuItem without the pattern is still rejected. Only two fixed typed
MenuItem resolvers are added; the legacy welcome/menu-only command path cannot
use MenuItem selected/select commands. There is no Invoke, keyboard, coordinate,
ancestor or action-retry fallback. This is a source-justified compatibility fix,
not a claim that either role or the opener guard caused the observed stop.

Pure fixtures cover both roles, promoted identities, absent patterns, changed
type/slug, unavailable pending targets, non-action opener variants, actual Expand
rejection, an expanded disabled opener, bounded diagnostics and two-selection
limits. Local PowerShell/Windows/GUI execution remains unavailable; these tests
must run in CI before any new acceptance is claimed. Other evidence and the
original failed report are unchanged.

#### Application-error stop after model selection

At `39a73bf4bca222188fd4c91c796847ba2c6787f7`, the real isolated Desktop
performed Beta then Alpha through RadioButton SelectionItemPattern; selected
states and current labels changed. Both screenshots also show the red public
toast **Couldn’t update model settings**, and the trace records rejected
`config/batchWrite` requests (`-32601`). The helper incorrectly reported its UI
selection stage as observed and continued to Alpha after the Beta error. That
was a test-oracle defect. The original report and screenshots remain unchanged:
they prove control operability, not successful application settings or persistence.
The current run's `... None` count of two (one Button, one other role) justifies
keeping display labels separate from action-target uniqueness; it does not
establish the cause of an older run without its raw tree.

The selection snapshot now rejects the exact public error sentence with either
its straight or typographic apostrophe. It exports only a bounded fixed count
and `selection_model_settings_error`, not the toast text or arbitrary UI content.
A second snapshot after each screenshot checks for errors that arrived after
the first post-select read, before another selection or an observed result.
This remains a bounded observation, not an atomic absence-of-errors guarantee.
The first error stops the sequence. If Beta was attempted, only its existing
fixed screenshot path is captured for the failure; no Alpha selection follows.
Capture failure remains explicit and does not erase the application error.
Previously proven UI state/label counts may be retained as operability evidence,
but an error cannot produce an observed selection outcome.

Pure negative fixtures cover both exact spellings, an error already present,
an error after Beta despite changed labels, a toast arriving after the first
capture, and failure to save error pixels. They require zero or one selection
and no Alpha attempt; successful no-error control fixtures remain distinct.
No production write/auth/setup boundary is changed, and there is no retry,
sleep, timeout extension or standalone GUI run. This candidate needs CI fixture
validation; local PowerShell/Windows/GUI tests were not run. Any future real
configuration-write behavior needs a separate explicit contract, not more
selection-only GUI repetitions.

### Explicit current-VM owned preference-write slice

The separate `-DesktopModelSettingsProfile bridge-owned-model-settings-4239`
requires the existing owned catalog and model-selection profiles plus all seven
push markers: `[bridge-ui-4239-v1]`, `[winapp-inspection-v0.7.1]`,
`[bridge-welcome-4239-v1]`, `[desktop-synthetic-models-v1]`,
`[bridge-owned-catalog-v1]`, `[bridge-model-selection-v1]`, and
`[bridge-model-settings-v1]`. Missing, unknown or mixed authority stops before
launch. Without this additional opt-in, menu-only and prior selection modes
retain their existing write rejection. The original signed-package, runtime
pin, window identity, process supervision, cleanup and UI action limits remain.

The main test harness sets only
`bridge_model_settings: "owned-file-v0.167.0-alpha.3.2"` in its reconstructed
bridge configuration. The fixture helper, original seven-field readiness and
zero-request audit are unchanged. Readiness proves fixture creation, not settings
activation. The frontend `control-home/config.toml` must initially be absent;
only the production bridge may create it after its genuine native policy and
complete catalog checks. Native homes, configurations, auth, setup and policy
are not modified by this option.

The same bounded Beta-then-Alpha UI path, including the exact settings-error
toast stop, is reused without new actions, retries or keyboard fallbacks. A
single bounded read of the fixed frontend file checks the initial Alpha value
and each captured Beta/Alpha value. It accepts only the two known flat string
fields, independent of quote style and order; missing, unknown, duplicate,
changed, oversized or unexpected contents stop the slice. There is no sleep or
retry if an asynchronous write has not committed at that observation. A second
failure screenshot does not repeat the file read. No file contents or paths
are published.

Before truncating the public record preview, the main harness summarizes the
complete validated `$allEvents` snapshot. Exactly one Beta and one Alpha
`model` + `model_reasoning_effort: none` flat scalar batch must each have its own
matching `(pid, correlation)` success message, in that order. The request
oracle matches the reviewed production subset: `replace`/`upsert` for both
scalar edits and omitted/boolean `reloadUserConfig`; this does not reload
existing threads or authorize other fields. Other startup
batches are counted separately, never attributed to these selections or made
supported. Unknown selected shapes, RPC errors, missing or duplicate matches
cannot count as success. This correlation evidence does **not** inspect or
validate the `ConfigWriteResponse` body.

A successful result establishes only the two UI states, no observed settings
error toast and the actual frontend preference values at those file-read
instants in this disposable VM. It does not verify GUI user-layer versions,
CAS, restart persistence, thread migration, inference, real accounts or the
absence of all background failures. Version/provenance/CAS behavior belongs to
the separate production regressions. Existing cleanup evidence retains its
original scope; a file read is not proof that the entire process tree stopped.
