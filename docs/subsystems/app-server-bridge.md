# Experimental Codex app-server bridge

This opt-in command connects one JSONL client to **two unmodified, independently
configured official Codex app-server processes**. It is not Magpie's HTTP
Codex relay, provider gateway, or a replacement Codex implementation. There is
no HTTP client or listener in this package. Each official child performs its
own model requests using its own configuration and authentication.

**Status:** cloud implementation, offline fixtures and limited official-process
smoke checks only. The official
Codex Desktop model picker, actual GPT subscription/AIHub tool tasks, company
network policy, and Windows process behavior have not been accepted. A merged
`model/list` response does not prove those features work in Desktop. Native
account quota UI may disable Send independently of the selected custom model.

The CI-only [standalone native acceptance harness](../../build/control-plane-native/README.md#standalone-4239-app-server-profile)
has an explicit `app-server-4239` profile for the separately reviewed
26.1103.4239.0 native executable. Its omitted/empty profile remains pinned to
the original runtime; no production bridge default or native permission is
changed. The new profile attempts the same two-thread/four-turn synthetic
protocol, real thread-list and config/layer checks with a different fixed
binary hash. It is not a Desktop launch, UI acceptance or real-account test.
Adding the profile does not establish a successful native run.

The separate [self-authored model profile](../../build/control-plane-native/README.md#self-authored-synthetic-model-profile)
`synthetic-models-4239` uses the same reviewed native executable with two newly
written local test models, `test-alpha` and `test-beta`. It rejects catalog
input and does not download or copy a bundled model record. Each child loads
only its own record; actual model-list identities, original Responses slugs,
none-effort metadata and four fixed text turns must satisfy the existing
ownership/config/lifecycle oracles. Its report scope is distinct from legacy
acceptance. This is CI-only harness behavior, not a production bridge or
Desktop change, a permission-policy change or proof of a hosted model.

The standalone self-authored profile also uses two distinct **public synthetic**
authentication canaries to test native per-provider Bearer routing to its local
fixtures. Its self-authored standalone profile additionally performs one public
canary rotation, clean process restart and saved-thread resume; see the
[restart contract](../../build/control-plane-native/README.md#one-synthetic-credential-restart-and-saved-thread-resume).
It never reads real credentials or enables Desktop account support.
See the [canary contract and real-account boundary](../../build/control-plane-native/README.md#synthetic-provider-authentication-canaries-standalone-only).

## Sources and entry points

- [`app_server_bridge.go`](../../internal/cli/app_server_bridge.go):
  `magpie app-server-bridge --config /absolute/bridge.json`.
- [`cli.Main`](../../internal/cli/main.go) dispatches the command before normal
  startup, account discovery, settings migration, proxy installation, and
  session persistence. `run` has the same early dispatch for CLI tests.
- [`Config.Validate`](../../internal/appserverbridge/config.go): strict,
  bounded JSON, two backend identities, absolute native executable paths,
  and four pre-existing non-overlapping state directories. Symlink/file
  identities are checked. No directory, credential, config or login is created.
- [`Run`](../../internal/appserverbridge/run.go): starts each child with
  `app-server -c model_provider="<configured provider>"` using `proc`, not a
  shell. Scans bounded JSONL, serializes router state, limits stalled writes,
  fails the whole connection on child loss, and reaps its owned children.
- [`Router`](../../internal/appserverbridge/router.go) and
  [`protocol.go`](../../internal/appserverbridge/protocol.go): protocol scope,
  ownership, request correlation, catalog pagination and field validation.

Baseline: Magpie **v0.1.3035**, commit
`435f0415140ca58a8af49eab46754d4903d35da9`, verified 2026-11-05. MIT license
and existing third-party notices remain unchanged. Protocol shapes were checked
against schemas generated without credentials by an unmodified cloud binary,
`codex-cli 0.167.0-alpha.4`, plus
[official app-server documentation](https://learn.chatgpt.com/docs/app-server).
The access fields and environment read methods were also checked against
the exact [official 0.166.0-alpha.6 protocol](https://github.com/openai/codex/tree/rust-v0.166.0-alpha.6/codex-rs/app-server-protocol/src/protocol/v2).
Schema compatibility does not certify a Desktop/backend runtime combination.

## Routing contracts

1. `initialize` is sent independently to both children. By default the primary
   child's genuine result is returned only after both succeed. The separate
   [routing control plane](#exact-0167-owned-routing-control-plane) instead validates
   both native identities and reports its own explicit frontend `codexHome`.
   At that point the
   bridge is ready, before emitting the merged response; no client
   `initialized` notification is required. A real `initialized` notification
   is passed unchanged to both children when early, delayed, or repeated,
   and never grants readiness. Failed or partial initialization stays closed.
   Attestation and explicit gateway OAuth opt-ins are rejected by
   default. Separately configured pinned modes can preserve the true flags
   with the limited behavior documented below; neither grants login authority.
2. `model/list` exhausts each child's pages, or uses that backend's explicitly
   supplied `models` descriptors. It preserves unknown metadata, prefixes both
   `id` and `model` as `magpie/<backend>/<model>`, labels display names, and
   namespaces upgrade references. Only one visible default is exposed.
   Static descriptors are declared metadata, **not proof of capabilities,
   availability, entitlement or service compatibility**.
3. Models select a backend only when starting a **new thread**. Every known
   thread keeps its owner. A turn's model override must belong to that owner.
   There is no cross-provider migration, history replay, or fallback.
4. Thread IDs remain native UUIDs, not made-up namespaced IDs. Ownership is
   learned from that backend's thread response/list or verified parent
   relationship. A cross-backend ID collision invalidates the connection.
   Unknown IDs never select a default backend. Ownership is connection-local;
   rediscover saved threads with `thread/list` after reconnecting.
5. Frontend request IDs are replaced with unique transport IDs and restored
   exactly, including large numeric IDs. Backend approval/request IDs get a
   separate namespace. Replies return only to their origin. The related
   `serverRequest/resolved.requestId` is translated too. The last 256 resolved
   requests have tombstones so late UI responses are discarded safely. Failed
   fanouts release their payloads immediately and keep at most 256 transport-ID
   tombstones for a late sibling response.
6. Thread, turn and item notifications are forwarded only after checking
   ownership. `thread/started` may precede its request response. Model fields
   are translated at their defined protocol locations; user text, tool
   arguments and arbitrary JSON are never recursively rewritten.
7. Turn membership is scoped by backend **and thread**, so historical turn
   IDs shared by a fork remain valid. Interrupt/steer requests must name a
   known turn in that thread. Errors are returned without retrying another
   provider. A backend crash or malformed/unmatched response ends the bridge.

The handshake ordering is source-backed for both retained native profiles:
[0.166 initialize processor](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/src/request_processors/initialize_processor.rs)
and [0.167 initialize processor](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/initialize_processor.rs)
commit the native session before sending success. The corresponding
[0.166 message processor](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/src/message_processor.rs)
and [0.167 message processor](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/message_processor.rs)
only log client notifications; `initialized` does not advance native state.
The earlier bridge incorrectly required that notification after two successful
replies. This correction changes ordering only, not capability negotiation,
authentication authority, account RPC support, or policy comparison.
`getAuthStatus` remains unsupported (`-32601`) by default after readiness.
The separately selected [CI-only no-auth mode](#exact-0167-ci-only-no-auth-status)
and the [trusted stdio relay](#exact-0167-primary-trusted-stdio-auth-status) are
separate explicit exceptions; readiness alone never enables either.

Supported requests are enumerated in `Router.Client` and `validateParams`:
initialization, model listing, ordinary thread start/resume/fork/list/read,
loaded-thread listing, paginated turns/items, archive/unarchive/name/compact,
revert/legacy rollback/unsubscribe, turn start/interrupt/steer, and the primary
backend's account/rate-limit reads. Legacy `thread/rollback` keeps its
`numTurns` parameter. `thread/revert` takes `threadId` and `beforeTurnId`:
the named turn and every later turn are excluded from the replacement durable
history; local file changes are not reverted. These are distinct native
operations, not interchangeable method names. The child version determines
availability. Unsupported methods return `-32601`.

The revert boundary follows the native `ThreadRevertParams` contract, also
confirmed by the schema generated from Desktop `26.1103.4239.0`'s bundled
`0.167.0-alpha.3.2` binary. The bridge requires a non-empty string boundary
already known under the selected backend **and thread**, including turns
learned through `thread/turns/list`. Missing, null, malformed, unknown or foreign
boundaries return local `-32602` without a backend delivery. The mistaken
`turnId` spelling is rejected rather than silently aliased. Valid params go
unchanged to the thread's owner. Native errors remain errors without retrying
another provider; the bridge does not emulate history mutation.
The native response has updated thread metadata with empty `turns` and optional
backwards cursors for rehydration. Existing response ownership checks and cursor
pass-through apply. The native `thread/reverted` notification remains scoped to
that owner. Synthetic coverage is in
[`thread_revert_test.go`](../../internal/appserverbridge/thread_revert_test.go);
it does not execute a native revert or establish live history-mutation support.
The existing [`bridge-race` job](../../.github/workflows/experimental-bridge-windows.yml)
copies that test file onto exact pre-fix commit
`341298e1515298c6f8714509c78324f7f38dbe12` and requires the named boundary-routing
test's behavioral failure. Compilation or setup failure does not qualify.
On the triggering candidate it runs every `TestThreadRevert` test under
`-race -count=20`, then retains the whole existing bridge/proc/command suite.
Both runs use isolated homes and pinned Go caches; neither issues a native
history-mutation request.
The separate default-off [feature-page comparison](#exact-0167-shared-experimental-feature-pages)
adds only unscoped `experimentalFeature/list` under its pinned contract.

The separate experimental methods `environment/info` and `environment/status`
accept only `{ "environmentId": "local" }` in this bridge. The native local ID, when enabled,
belongs independently to each child's registry. Both children must return the
same complete result before it is exposed; different status, shell, cwd, or
other result fields produce an explicit ambiguity error, never a synthesized
`ready` or a last-model/primary-backend fallback. Combined results are bounded
to 64 KiB. Each child's own error, including experimental API gating, remains
an error. Initialization capabilities are not enabled or widened by the bridge.

`environment/info` reports the child's startup cwd. The two private homes
normally yield different values, so that query can remain blocked by an
ambiguity error even when both children are healthy. The bridge does not erase
cwd to hide the difference. An explicitly selected shared startup directory
can make the real cwd values agree, as described below. Arbitrary remote
environment queries and `environment/add` are unsupported: native environment
IDs are process-local and an info query may start or recover a connection.
There is no literal `environment/status/info` method in the checked protocol.

Unknown request fields, config/history/path overrides, collaboration modes,
provider fallbacks and delegated parent/root turn lineage fail locally with
`-32602`. The exact routing control mode has a separate
[null-only thread-list lineage exception](#null-only-thread-list-lineage).
Section-position thread sorting is unsupported. `created_at`,
`updated_at` and `recency_at` are merged in the requested direction. Parent
subagent IDs may be learned from verified same-backend relationships, but
cross-provider subagent dispatch is not implemented or required.

## Permission and environment selection

`permissions` names an opaque profile in the selected child's own config. It
is forwarded unchanged on thread start/resume/fork and turn start. It cannot
be combined with a non-null `sandbox` or `sandboxPolicy`. The bridge never
replaces an unavailable profile, copies a profile across backends, removes a
permission field, or retries a denial with weaker access. Effective permission
profile information and permission approval requests/responses are preserved.

`runtimeWorkspaceRoots` is supported on those same requests. `environments`
is supported only on thread start and turn start, as in the checked schemas.
Its IDs select environments inside the already chosen thread owner's child;
they never select or change a model provider. Environment order, native cwd
and nested roots are preserved. Omitted values, explicit null, and empty
arrays remain distinct on the wire. In particular, `environments: []` is not
replaced by a default, and empty roots are not expanded. The official child
validates profile availability, environment membership, absolute native paths,
and actual grants. Windows paths are not normalized by the bridge's host OS.
Unknown nested environment fields and invalid field types fail locally.

Thread environment connection notifications retain their native environment
ID and are checked against the thread's backend owner, so both children may
legitimately report `local` without transferring ownership of any thread.

## Limits and isolation

The runner starts no existing GUI, changes no global agent settings, imports
no current credentials, and neither chooses nor installs a desktop backend
hook. The first backend owns account display; account mutations, sign-in,
external token refresh and unscoped backend requests are unsupported except
for the explicitly enabled terminal attestation-error response described below.

Each child gets explicit `HOME`, `USERPROFILE`, `CODEX_HOME`, private XDG/app
paths and a minimal system PATH. Ambient tokens, proxy variables and runtime
injection variables are not inherited. Optional `inherit_env` names forward
only explicitly named variables to one child; store **names, never secret
values**, in the JSON. This facility is not used by the tests with real keys.
Reserved home/runtime/proxy names are rejected. Corporate policy requiring a
proxy or a particular process identity needs separate authorized validation;
this command does not bypass such policy.

Separate configuration directories are **not an OS security sandbox**. The
children run as the same user and can access that user's files and keychain
where the operating system permits it. Executable-path validation does not
certify that an arbitrary binary is an authentic official Codex release.
Likewise, a backend's own config may define endpoints. Check those configs and
binary provenance before any real acceptance test; never point OpenAI at
Magpie's gateway or AIHub.

The bridge uses `proc.StartTreeStrict`: a Unix session, or a strict Windows
kill-on-close job. Windows starts the process with its initial thread suspended,
assigns the exact owned process to the job, verifies its sole initial thread,
and resumes it once. This prevents the ordinary child-spawns-before-job-assignment
race in the general-purpose `StartTree` helper. Job denial, ambiguous threads,
conflicting creation flags or failed resume cause startup failure and owned-child
cleanup; the bridge never falls back to an uncontained Windows child. Other
Magpie callers retain the original helper and its existing behavior.

Shutdown closes stdin, gives an EOF cleanup grace period, then stops/reaps the
owned tree. A Unix descendant can deliberately create another session; hostile
external process injection and security-product interference are not contained
by this lifecycle helper. Conservative Windows thread checks can reject such an
environment rather than weaken policy. There is no process-name-wide kill,
breakaway flag, privilege escalation or change to OS security settings.

Limits: 16 MiB per JSONL message and combined list, at most 10,000 rows and
100 backend pages, 128 active frontend requests with 32 MiB of stored input,
4,096 tracked backend requests, and 64/32 MiB pagination snapshots. Expired
bridge cursors are rejected. Only one refresh per list method can run at once.
No message payload or child stderr is logged. A fatal protocol error prints
only a generic diagnostic and ends the connection.

## Configuration example

Copy [`app-server-bridge.example.json`](../examples/app-server-bridge.example.json)
to an isolated test directory and replace paths. Its static AIHub card is an
**illustrative declaration**, not a discovered model capability. Replace it
with reviewed metadata for the intended backend before enabling real use.
Create four separate directories outside the live Codex home and configure
both official backends independently. Authentication setup and AIHub endpoint
configuration are intentionally not performed by Magpie or this change.

### Optional shared startup directory

By default each child starts in its own private HOME, preserving the original
behavior. To deliberately choose a common startup context, add the optional
top-level setting `"working_directory": "/absolute/isolated/shared-start"`
beside `backends`. It must name an existing absolute directory outside all
four state directories, including their ancestors, descendants and symlink
aliases. The bridge neither creates this directory nor changes its own/global
cwd. A missing or invalid directory fails startup; it never falls back to a
different directory.

Both child processes then genuinely start in that same directory. Their
HOME/CODEX_HOME, provider selection, environment filtering and permission
configuration remain separate. No `environment/info` fields are rewritten,
and every full-result comparison remains in force: different shell, status or
other metadata still yields an error. The setting changes process startup
cwd only; it does not replace a client's thread/turn cwd or workspace roots.

This is a real configuration choice, **not a cosmetic compatibility toggle**.
Official Codex loads startup configuration in the context of cwd. Its project
trust lookup can affect default approval behavior; trusted project config,
hooks, execution policies, skills, instructions and default workspace roots
can also change. Untrusted project configuration is not an inert directory:
official startup warnings say skills still load. Choose an intentionally
reviewed neutral directory and review each child's effective configuration.
The bridge does not change trust settings, erase restrictions, or copy one
child's profile into the other.

Matching shell/cwd and `ready` attest only to those reported metadata fields
and connection status. They do **not** establish equal permissions, network
grants, account entitlement or successful tools. The checked public TUI uses
environment cwd for relative project paths and reads trust/config separately;
the proprietary Desktop's exact behavior is not verified here. Sources:
[official startup](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/app-server/src/lib.rs#L557),
[trust-dependent configuration](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/core/src/config/mod.rs#L3637),
and [public client use](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/tui/src/config_update.rs#L198).

The primary backend should be official Codex configured for OpenAI directly;
the secondary should be another instance of the same official executable
configured for its named AIHub Responses provider. The bridge does not read
those auth files or transform subscription authentication into API keys.

There is no automatically installed Desktop command override. Desktop
startup arguments, backend-selection hook, transport and single-instance
behavior must be reviewed for the exact installed version in a future
isolated acceptance stage. Unknown Desktop startup flags are rejected rather
than silently forwarded. Do not replace a production backend or modify the
current session to try this experimental command.

## Verification

Run with a fresh HOME and pinned Go caches as required by the code standards:

```sh
go test -race -count=20 ./internal/appserverbridge
go vet -tags nogui ./internal/appserverbridge ./internal/cli ./internal/proc
GOOS=windows go vet -tags nogui ./internal/appserverbridge ./internal/cli
go test -tags nogui ./...
go build -tags nogui .
GOOS=windows go build -tags nogui .
```

The package's fixtures are local synthetic processes. They check environment
isolation, explicit arguments, lifecycle, malformed streams, bounded writes,
request/list/notification routing, collision rejection, pagination, fork
history and provider invariance. CLI tests verify help/invalid configuration
bypass normal startup and leave agent files unchanged. See the delivery's
verification report for the actual checks run and any blocked whole-repo
checks; commands here are not evidence they passed.

### Official-process smoke check, 2026-11-05

The built bridge launched two unmodified `codex-cli 0.167.0-alpha.4`
app-server children with separate empty homes, no supplied credentials, and
two loopback-only synthetic Responses services. Both children initialized,
the bridge returned the two explicitly static test model cards, and it
forwarded the official `thread/start` error without changing providers.
Client EOF shut down the bridge and its children normally.

Thread creation was blocked before either synthetic service received an
HTTP request: the official filesystem sandbox helper rejected its reserved
app-server socket directory. The current cloud environment exposes that
directory with mode `000`, whereas the official check requires a user-owned
directory with mode `0700`. Fresh test homes with mode `0700` did not change
the result. The [official socket-directory implementation](https://github.com/openai/codex/blob/e7f32cf1928a35a9232a633a51c2a03b5a7e2c72/codex-rs/uds/src/daemon_directory.rs)
uses a fixed `/tmp/codex-daemon-<uid>` path, independent of `HOME`, `TMPDIR`
and `CODEX_HOME`. Its permissions and the sandbox were not changed.

Therefore real official-child Responses streaming, tool execution, follow-up
and interruption were **not reached** in this environment. The synthetic
process tests cover those bridge behaviors, but do not establish this missing
runtime integration. Optional syscall tracing was also denied and stopped;
configuration and local fixture logs are not kernel-level proof of no egress.
No real OpenAI/AIHub inference or Desktop acceptance is claimed.

A subsequent read-only smoke check of the access-field compatibility patch
used two fresh official `0.167.0-alpha.4` children with experimental capability
enabled explicitly by the test client. `environment/status` returned their
shared `ready` result; `environment/info` returned the bridge's expected
ambiguity error because their startup cwd values differed. The children exited
normally. This check made no thread, turn, or model-service request and does
not resolve the Desktop environment workflow or the earlier sandbox blocker.

A final read-only A/B check used the optional shared startup directory with
fresh official `0.167.0-alpha.4` children. With the setting omitted, local info
still failed with the expected cwd ambiguity. With an explicitly created,
reviewed neutral directory selected, both native replies agreed and the bridge
returned its actual `file:` cwd plus `bash` at `/bin/bash`. Both runs returned
shared status `ready` and exited normally. This verifies the narrow process-cwd
solution on cloud Linux without rewriting metadata. It makes no statement
about equal permission profiles and did not retry the blocked thread/tool path.
Desktop 26.1029.7803.0 with backend 0.166.0-alpha.6 remains untested.

Future real acceptance remains: authorized isolated Desktop instance, same
menu containing GPT and deepseek-flash-ft, both doing an unpredictable local
file/tool task and a follow-up, stop/error behavior, process/route evidence,
company policy compliance and reversible activation. A code build, HTTP 200,
model self-identification or catalog visibility cannot replace those checks.

### Experimental Windows artifact workflow

The fork's [experimental Windows workflow](../../.github/workflows/experimental-bridge-windows.yml)
checks out the exact triggering commit on
`experiment/app-server-bridge-v0.1.3035`, uses the Go version in `go.mod`,
and builds the `nogui` Windows amd64 command on a GitHub-hosted Windows runner.
It runs the native synthetic bridge tests, the bridge CLI isolation test,
Windows `go vet -tags nogui ./...`, and the built command's help smoke test.
It also repeats targeted Windows process-family cleanup and interruption tests,
and strict process-start tests. The Windows cases hold real process handles to
both fixture backends and their descendants before testing EOF, cancellation,
forced cleanup, malformed output and backend exit. Synthetic interruption cases
keep both providers active, reject unknown/foreign turn IDs, observe the intended
interruption while the sibling stays active, and start follow-up turns. The
Unix-only process-group test still skips on Windows. This is not the full
repository suite or official-child/Desktop acceptance.

The unsigned artifact includes the exact source commit/tree and run URL,
executable Go build information, unchanged MIT license and third-party
notices, an illustrative config, this design document, and SHA-256 checksums.
Its name includes the full source SHA and it expires after 14 days. The job
records the uploaded archive digest separately. Hashes provide integrity and
provenance context, not a trusted code signature or deployment approval.

Only this fork and experimental branch can run the job. It has read-only
repository permissions, does not retain checkout credentials, references
Actions by commit SHA, and never invokes the upstream release workflow,
creates tags/releases, changes `main`, installs software on a user's machine,
or bundles accounts or an official Codex backend. The `workflow_dispatch`
trigger is declared, but GitHub may not expose manual dispatch until a workflow
is on the default branch. Do not modify `main` merely to enable that UI; the
experimental branch's push trigger is the intended initial run.

This independent CI artifact is not a workaround for an organization's
blocked dependency downloads. Do not download, install or use it in a managed
environment without the applicable software/network approval. Actual Desktop
integration and deployment remain separate, unverified steps.

The strict-start regression samples the Windows thread suspension count before
job assignment and checks no fixture startup marker exists. A separate temporary
checkout removes `CREATE_SUSPENDED` and must produce a named regression failure;
the artifact is built only from the untouched triggering commit. Pre-resume
assignment and resume failure tests verify no fixture execution,
owned-child reaping and handle cleanup. Partial-resume failure tests deliberately
start the fixture and its descendant, then verify both are terminated and reaped.
These tests exercise native Windows processes and jobs with synthetic
children, not official Codex tools or a managed corporate Windows installation.

## Native stdio launcher adapter

[`cmd/magpie-codex-bridge`](../../cmd/magpie-codex-bridge/main.go) builds a
separate native executable. It imports the bridge directly and never enters
Magpie's ordinary CLI, account discovery, gateway or settings-migration startup.
This matters because the older Magpie executable's special early entry point is
`app-server-bridge`, while a Codex client normally launches `app-server`.
**Do not point a Desktop executable override at the ordinary Magpie binary.**

The adapter accepts the reviewed stdio forms `app-server`, `app-server --stdio`,
`app-server --listen stdio://`, and `app-server --listen=stdio://`. It also accepts
the installed Desktop's observed base argument array:

```text
-c features.code_mode_host=true app-server --analytics-default-enabled
```

The exact scalar feature toggle (and separately gated plugin leaf below) may
appear before or after `app-server`, using
`-c` or `--config`. The analytics default flag is server-only. Duplicate controls,
conflicting transports, false/structured/quoted toggle values and every unknown
argument fail before configuration or environment access and before any child
starts. Argument count and total bytes are bounded. Diagnostics never repeat
raw arguments or values. See the [official CLI definitions](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/cli/src/main.rs#L573-L632).

Set `MAGPIE_APP_SERVER_BRIDGE_CONFIG` in that process's environment to an
absolute bridge JSON file. `--check` validates it without starting any child;
help and version identify the adapter truthfully, not as an official Codex CLI.
Nothing sets a persistent environment variable, edits the registry, changes a
Desktop shortcut or installs this executable as a backend.

### Reviewed startup controls and policy preservation

`RunWithOptions` takes three typed controls, not a raw argument or TOML map. The
legacy `Run` path supplies no controls and retains its existing child command.
Both backends receive the same requested controls, followed by their own pinned
provider selection. Incoming provider, URL, authentication-storage, permission,
arbitrary config and all but the exact opt-in plugin leaf below remain rejected.

The scalar Desktop toggle is deliberately normalized to
`-c features.code_mode_host.enabled=true` for each official child. This preserves
the feature-enable intent without replacing an existing structured feature
table, including its `disable_in_process_fallback` setting. It is an explicit,
conservative translation, not byte-for-byte emulation of upstream scalar
replacement. In 0.166, `code_mode_host` accepts an `enabled` field but is absent
from the special scalar-to-structured merge list. See the [feature fields](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/features/src/feature_configs.rs#L69-L83)
and [merge implementation](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/config/src/merge.rs#L63-L154).
Synthetic policy fixtures cover the granular override contract; real official
backend policy merging remains an integration acceptance item.

`--analytics-default-enabled` is passed unchanged, never rewritten as
`analytics.enabled=true`. An explicit backend analytics opt-out still wins for
the official built-in metrics default. It is not a global analytics/network
switch, and isolated homes do not automatically inherit preferences from the
original Desktop home. See the [official metrics default resolution](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/core/src/otel_init.rs#L68-L79).

Conditional `features.prefer_mxc` and `features.windows_sandbox_service` overrides
remain rejected in this version. The former selects a Windows sandbox
implementation; the latter can enable provisioning through an installed service
when a later elevated setup request and other gates permit it. Neither is safe
to discard or infer automatically from a platform or version. Remote
`--code-mode-host URL`, daemon controls and additional unreviewed startup args
are rejected too. Their presence is an explicit compatibility blocker, not a
request to relax permission policy.

### Exact 0.167 bundled-plugin launch override

The signed Desktop `26.1103.3517.0`, with backend `0.167.0-alpha.3.2`,
was observed sending this additional normal-start argument:

```text
-c plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true
```

The metadata-only [runtime trace](https://github.com/breakingtime126/magpie/actions/runs/42213706926/job/127879528998)
and original signed package source chain distinguish it from a guessed
configuration spelling. The one-shot startup probe did **not** send it.
This evidence identifies the launch input, not successful Desktop integration.

Acceptance is independently **disabled by default**. The explicit top-level
setting `desktop_plugin_override: "codex-app-tools-v0.167.0-alpha.3.2"`
authorizes only that exact leaf, represented by
`LaunchOptions.CodexAppToolsMCPEnabled`. Both backend
`startup_probe_sha256` pins must be present and verified before either
JSONL child starts, including when only this mode is configured. Missing or
wrong pins, unknown/partial mode names and combinations with any legacy
0.166 startup, requirements or auth mode fail closed. Same-version modes remain
separate opt-ins. The operator must independently verify the exact binary
version and provenance; hashing operator-selected bytes does not check a
signature or prevent same-user executable replacement.

`-c` and `--config` accept the literal before or after
`app-server`. Duplicate controls, case/spacing/value changes, quoted
or structured alternatives, false, any other plugin/server name and every other
nested setting remain rejected before environment/configuration access.
Sign-out still takes precedence and starts nothing. This is not arbitrary
TOML parsing or plugin-config passthrough.

When requested and explicitly authorized, the exact unchanged leaf is appended
to **both** official backend command lines, followed by each backend's own
provider pin. Merely selecting the mode never synthesizes the override. The
scalar code-mode normalization and default-only analytics flag keep their
existing behavior. Both the CLI parser and the direct
`ProbeStartup` API reject the plugin launch option for one-shot
inspection. Selecting the mode in the shared config does not inject it into a
probe. The direct `RunWithOptions` API enforces the same explicit gate.

This is a real native plugin policy choice, not an inert compatibility label.
At pinned official source
`c40729ea5cf8e947481b130c59807273eda2f8e9`, the nested entry can
[activate an installed outer plugin](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/loader.rs#L174-L190)
and its true server override can
[replace a manifest's false enablement](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/loader.rs#L1572-L1618).
It applies only to a declared `codex_app` MCP server; native
[requirements](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core/src/config/mod.rs#L2312-L2342),
authentication and transport gates still apply. The server can have subprocess,
credential-use and network effects under the official backend's own policy. Review both independent
backend configurations and installed plugin contents before enabling real use.
The bridge does not install, download or copy plugins, populate plugin caches,
write configuration, discard permissions, synthesize MCP results or retry
failures with weaker settings. Missing/unavailable plugins remain native
behavior, never repaired by copying the Desktop cache. In an empty cache the
native [loader](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/loader.rs#L856-L908)
records `plugin is not installed` and returns before manifest/MCP/app/hook
loading. This leaf is not an installation command. The empty CI homes do not
contain the Desktop cache, so their results cannot certify installed-plugin
execution, IPC/credential dependencies or suitability for the secondary provider.
Other native startup paths can independently perform
[policy-permitted curated sync](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/manager.rs#L773-L789).
The missing-plugin return is not proof that all startup is offline. The Windows
Desktop startup trace is not network-none, and no zero-egress claim is made.
Actual signed Desktop/native startup may contact policy or curated services.
The separate Linux official-runtime harness uses a network-none container;
that containment does not apply to the Windows Desktop trace.

The sole launch mode also selects the exact 0.167
`requiredHumanApproval` rejection boundary. It enables no auth,
requirements-read or startup capability by itself and preserves native
permission/approval checks.

Implementation:
[`launch.go`](../../internal/appserverbridge/launch.go),
[`Config.Validate`](../../internal/appserverbridge/config.go),
[`RunWithOptions`](../../internal/appserverbridge/run.go),
[`probeStartup`](../../internal/appserverbridge/probe.go), and
[adapter argument parsing](../../cmd/magpie-codex-bridge/main.go).
The standalone
[`desktop_plugin_red_test.go`](../../cmd/magpie-codex-bridge/desktop_plugin_red_test.go)
uses only APIs present at base
`643cc5d060659c68c75e342e7e0fd71590b6993c`. Copying that file
alone to the base supports an assertion-level red for the exact launch input.
The companion adapter/runtime fixtures check all gates, both digest positions
before either child, no probe injection, no default injection, private
diagnostics, unchanged on-disk policy and exact argv delivered to two native
synthetic children through `internal/proc`. A synthetic native initialization
error remains an error without fallback, followed by EOF cleanup.
The compiled adapter case is `TestBuiltAdapterDesktopPluginOverride`.

At authoring no executor was available. Gofmt, build, vet, red/green tests,
race, the full suite and official installed-plugin runtime behavior were not
run for this change. The integrating CI record must state the actual results
at the exact tested head. Synthetic argv checks and empty-home Windows traces
do not establish real account, plugin tool, model inference or complete
Desktop acceptance.

### Installed Desktop launch evidence

An authorized read-only reinspection of Desktop 26.1029.7803.0 verified the
version-2 resolver extraction against the installed package. Its order is
internal `hostConfig.codex_cli_command`, then `CODEX_CLI_PATH`, then the official
backend. The host-config array's first item selects the executable and remaining
items supply its full arguments. The environment override selects an executable
but retains Desktop's default argument construction. `CODEX_APP_RUNTIME_CLI_PATH`
is a derived export, not a selection input. The internal host-config field is
not thereby a supported top-level `config.toml` field. This remains a
version-specific implementation finding, not an official long-term contract.

The base arguments above can be followed by `startupCommand.args` and additional
`-c` overrides from Desktop configuration, including conditional sandbox,
authentication-storage and plugin settings. The nonoverride Windows core path
may add `features.windows_sandbox_service=true`. These are possible code paths;
no private effective values were read. The launch uses Node `spawn` with piped
stdio, environment and working-directory inputs. This confirms construction
logic, not that a separate Desktop instance or bridge was launched successfully.
No `CODEX_CLI_PATH` setting is made by this project.

### Explicit, narrowly supported startup inspection

Desktop's startup inspection appends `--print-startup-config` and
`--startup-client-info` with a JSON client identity. The adapter now handles this
as a separate one-shot operation, never as a JSONL connection. It requires both
flags, a bounded client object with unique `name` and `version` string fields
and an optional string/null `title`, and no explicit `--stdio` or `--listen`.
Unknown, duplicate or malformed arguments fail before configuration access.

Inspection is **disabled by default**. After separately reviewing and authorizing
the effects on **both** configured backends, an operator can explicitly select
one `startup_probe` profile:

- `compare-unrestricted-v0.166` accepts only exact `0.166.0-alpha.6`.
- `compare-unrestricted-v0.167.0-alpha.3.2` accepts only exact
  `0.167.0-alpha.3.2`, the backend observed in Desktop `26.1103.3517.0`.

Each backend's `startup_probe_sha256` must be the SHA-256 of its separately
reviewed native executable. Neither profile is a range, automatic version
detection, or permission to enable another compatibility mode. Unknown profile
names fail configuration validation. The new 0.167 profile cannot be combined
with the legacy 0.166 requirements/auth modes. Only the separately selected,
exact 0.167 null-only requirements and auth modes below may accompany it.
Cross-version configurations
fail before either child starts, for one-shot and JSONL launches alike. Existing
0.166 combinations remain unchanged. The shipped example enables neither profile.
Config validation checks pin syntax; the probe hashes **both** binaries before starting either one. A pin
is not a signature, proof of an official binary, or protection against replacement
by another same-user process. Provision and review the executables independently.

The adapter invokes each configured official command sequentially, using the
same reviewed environment, working directory, normalized launch controls and
provider pin as the normal bridge. It adds only the two inspection arguments,
closes stdin, discards stderr and bounds stdout to 64 KiB. One 30-second context
budget covers both probes and digest checks; native filesystem and process
cleanup calls still depend on the OS completing them. Every started process uses
the bridge's strict owned-process-tree startup and cleanup. There is no retry,
JSONL initialization, fallback to a single authority, or cached permission grant.

Only this **complete** shape is supported, from both successful official exits,
with `appServerVersion` equal to the selected profile's exact version:

```json
{"appServerVersion":"0.166.0-alpha.6","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}
```

For the new profile the version is exactly `0.167.0-alpha.3.2`; all other fields
and restrictions are the same. Both children must report that version, not one
old and one new result. Client `name`, `title` and `version` values are forwarded
as validated, never replaced with a profile-dependent identity.

The result is validated before one normalized JSON object and newline are
returned. Every displayed field came from both validated responses. Missing,
unknown, case-variant or duplicate fields, another version, non-null requirements
(including an empty object), malformed UTF-8, extra JSON, excessive output,
nonzero exits and timeouts fail closed. The adapter does not redact a requirement
into null or merge unlike policies. A null result is an official startup snapshot,
not proof that no managed configuration exists, and cannot override either
backend's later permission checks. Managed/restricted configurations are
explicitly unsupported in this mode even when the two responses match.

Official [startup configuration](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/app-server/src/startup_config.rs#L28-L76)
is a one-shot JSON command that returns before normal app-server initialization.
The [reviewed success construction](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/app-server/src/startup_config.rs#L154-L174)
and [requirements mapping](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/app-server/src/request_processors/config_processor.rs#L445-L465)
are the source of the legacy exact version-scoped schema. The new profile is
separately reviewed against official source
[`c40729ea5cf8e947481b130c59807273eda2f8e9`](https://github.com/openai/codex/tree/c40729ea5cf8e947481b130c59807273eda2f8e9):
its [Cargo version](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/Cargo.toml)
is `0.167.0-alpha.3.2`, and its
[startup implementation](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/startup_config.rs)
is byte-identical to the legacy source at
`4f093f2748667f3b0e6a07e3d4055121cbd3ce64` (Git blob
`71dc648265e811e919854c3dd78f125ef7db5150`). The complete
[public response](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs)
still contains only `supportsIndependentSpeedModes` and `requirements` inside
`configRequirements`. The new
[mapper](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/config_processor.rs)
prefers `permissions.network` over legacy `network`, and the requirements schema
adds nested OTEL `log_sse_deltas`. Neither change widens this null-only profile:
**every** non-null requirements value still fails, even if both backends agree.
This one-shot profile is not a policy merge and does not enable the separately
reviewed JSONL requirements/auth modes described below.

Runtime/config failures emit a fixed bridge-specific JSON error and unsuccessful exit, with no backend
payload, token, `configLoad` classification or suggested login recovery. Argument
failures use stderr and empty stdout. This does not promise that Desktop will
accept the bridge's error handling or finish its bootstrap.

**Inspection has side effects.** It can load stored authentication (including
configured keyring/workload sources), refresh and persist credentials, fetch
account/enterprise policy and update caches. The official CLI preamble can load
its `.env` and prepare helper directories. Separate HOME/CODEX_HOME directories
are not an OS security boundary; system/project policy and supported credential
sources still apply. The bridge does not substitute an empty directory or alter
policy to manufacture an unrestricted result. Even a rejected, timed-out or
partial two-backend probe may already have changed authentication/cache state;
there is no transactional rollback or automatic retry. Enabling this code is
not authorization to run it against a user's real accounts.
See [managed startup loading](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/app-server/src/config_manager.rs#L417-L430),
[credential refresh](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/login/src/auth/manager.rs#L2495-L2512)
and [cloud policy loading](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/cloud-config/src/chatgpt.rs#L33-L56).
The Go profile tests use synthetic responses and native fixture processes.
They cover exact-version selection, unknown profiles, both pins before either
execution, complete strict envelopes, non-null restrictions, mixed-profile
rejection, unchanged client identity, native cleanup and both compiled-adapter
profiles. They do not themselves execute the real official one-shot command,
read account state, refresh credentials or query policy services. A successful
synthetic profile test is not Desktop GUI acceptance or official-runtime proof.

### Startup sign-out remains unsupported

A separate Desktop recovery call appends `--startup-sign-out`. The adapter
rejects this flag before environment/configuration access, regardless of other
argument order, and starts no child. It never forwards sign-out or attempts to
repair an account. Official [startup sign-out](https://github.com/openai/codex/blob/rust-v0.166.0-alpha.6/codex-rs/app-server/src/startup_config.rs#L86-L150)
can revoke tokens, delete configured authentication, clear policy caches and
update some provider settings before a later error occurs. Failure is not proof
that nothing changed. Supporting inspection does not authorize those actions.

### Explicit comparison of public config requirements

The JSONL `configRequirements/read` operation is disabled by default. After
reviewing and authorizing policy inspection of both configured authorities, set
`config_requirements_read` to `compare-safe-v0.166`. This is independent of
`startup_probe`; enabling either does not enable the other. Both backends must
have a separately reviewed `startup_probe_sha256` executable pin. That existing
field now also pins the requirements-read backend. The normal runner verifies
**both** binaries before starting either process when this mode is enabled.
A digest is not proof of official origin or protection against same-user
executable/configuration replacement.

This mode is scoped to Codex 0.166.0-alpha.6, source commit
`4f093f2748667f3b0e6a07e3d4055121cbd3ce64`. The source oracle is the complete
[experimental Rust response model](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L411-L461)
and [handler](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/src/request_processors/config_processor.rs#L445-L606).
The stable generated schema omits experimental fields and is not the complete
oracle for an experimental-enabled Desktop connection.

Client requests may omit params, use null, or use an empty object. Other params
are rejected. Empty objects from the inspected Desktop wrapper are adapted to
`params:null` for each backend: the official request type is `Option<()>`.
Every accepted read queries both children afresh, waits for both successful
results and compares their complete validated public views. No cache, fallback,
single-provider projection, or policy intersection is used.

Both results must contain exactly `supportsIndependentSpeedModes:true` and
`requirements`. Genuine null/null is accepted. A non-null requirements object
must contain all 34 reviewed fields, with no unknown or missing fields. The
validator accepts a deliberately narrow typed subset: login/approval/sandbox/
web-search constraints, supported boolean controls, independent speed-feature
requirements, feedback/browser-import controls, and reviewed agent/application
network rules. It preserves restrictive values such as false, an empty
`allowedLoginMethods` array, and an enabled application network with no allowed
hosts. Null, empty objects and empty arrays are never treated as interchangeable.

Network hosts are restricted to lowercase ASCII DNS labels; agent rules may
add the reviewed wildcard prefixes, application rules may not. The canonical
domain map and derived allowed/denied arrays must agree. Ports and controls are
type-checked. Unix-socket rules, application storage, arbitrary feature keys and
unsupported host formats are rejected rather than ignored.

Opaque or credential-bearing branches must remain null in this first mode:
model/provider overrides, custom ChatGPT URL, developer instructions, permission
profiles/defaults, computer/browser-use definitions, hooks, auto-review, models,
state/log/catalog paths and OTEL. A non-null unsupported branch fails even when
the two backends return identical values. In particular, ordinary
[RedactedString serialization](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/utils/redacted-string/src/lib.rs#L9-L69)
does not hide provider bearer tokens, header values or authentication arguments;
browser-extension request headers can likewise contain credentials. The bridge
does not forward them, replace them with null, or echo them in errors.

Raw envelopes/results are bounded to 64 KiB, nested containers to depth 12 and
256 entries each, and the combined accepted results to 64 KiB. Duplicate keys,
invalid UTF-8, malformed types and unsupported fields fail closed. Comparison
ignores object-key order but preserves array order and all accepted values.
Only the complete agreed result is returned. Errors are fixed bridge messages;
untrusted backend error/data bodies are not forwarded for this method. Failed
fanouts release retained policy payloads and keep bounded late-response state.

This comparison is not an atomic snapshot across accounts and does not prove
all hidden policy is identical. For example, the API intentionally omits core
network header injections. Each official child still enforces its own current
policy. An `account/updated` notification does not make an earlier result a
lasting grant: the next client read queries both children again.

**The read can have side effects.** It reloads configuration and publishes
application network policy; cloud-policy initialization may use authentication,
recover credentials and write caches. An already initialized cloud snapshot
does not necessarily make a new HTTP request on every read, but there is no
pure/no-refresh RPC option. Both authorities must be authorized before enabling
the mode. Failed or partial fanouts are not transactional and are not retried by
the bridge. No real policy service, credentials or account is used in the tests.

This JSONL mode does not widen either one-shot startup profile. Both still
reject non-null requirements, and the 0.167 profile cannot be combined with this
0.166 JSONL mode. Supporting a managed Desktop's full startup therefore requires
additional reviewed work, not simply enabling this read option.

### Observed Desktop bootstrap and remaining boundaries

A later authorized read-only inspection of installed Desktop 26.1029.7803.0
confirmed its ordinary local JSONL initialize uses client name `Codex Desktop`,
`experimentalApi:true`, MCP UI/form extensions, `requestAttestation:true` and
`explicitGatewayOauth:true`. This differs from the one-shot identity
`codex_desktop`. The inspected network-requirements startup gate runs after the
main connection becomes connected; unrecovered read failure can show an error
and exit the application. Account updates trigger another requirements read.
These are source-code construction and control-flow observations, not a runtime
trace or successful bridge/GUI acceptance.

**Default behavior still rejects those two true capability flags.** Two
independent, default-off modes provide deliberately limited behavior while
preserving the client's true values. They do not silently lower capabilities,
add invented acceptance fields, start login, or claim working Desktop access.
Both modes require reviewed `startup_probe_sha256` pins for both children,
verified before either starts. A client must actually declare the corresponding
true capability; configuration alone does not synthesize it.

#### Explicit attestation-unavailable mode

Set `attestation` to `unavailable-v0.166` only when deliberately choosing to
continue with attestation unavailable. This is not an implementation of proof
generation. For a valid child `attestation/generate` request the bridge returns
a real JSON-RPC `-32601` error, with its original typed ID, only to that child.
It never sends the request to Desktop, generates or parses a proof, returns a
successful token result, constructs an HTTP header, or redirects a request to
the other provider.

The pinned official protocol treats `requestAttestation:true` as a client
declaration permitting requests, not a guarantee every request succeeds. Its
[error handling](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/src/attestation.rs)
converts a genuine JSON-RPC error into its own token-free status-2 failure
envelope and locally continues the model transport. The upstream service can
still reject or restrict failed attestation. This mode must not be used to
evade an upstream denial, and synthetic success says nothing about upstream
acceptance.

**This mode is not Windows Desktop's normal failure behavior.** A later
authorized static inspection of Desktop 26.1029.7803.0 found that Windows
normally returns a successful JSON-RPC result containing an opaque token
wrapper whose inner `error_code` is 1. The wrapper can also contain an
application identifier and language, region, timezone, screen and application
session signals. No real values or proof were read during that inspection.
Unexpected asynchronous handler failures instead return a JSON-RPC error.

The official app-server does not decode the inner wrapper. Any string token,
including an empty string, becomes its outer status-0 envelope with that
string. A JSON-RPC error becomes outer status 2 without a token. Inner Windows
platform failure and outer RPC failure are therefore different protocol paths;
neither their service treatment nor interchangeability has been established.
The bridge does not emulate the Desktop wrapper, copy application/device
signals, substitute an empty token, or present a synthetic token as official.
Even an inner failure wrapper is not assumed safe to send to a second provider.

The inspected Desktop handler ignores request params and replies to the
requesting app-server connection without a provider/domain/account check.
Only the macOS ARM64 branch calls the native generator, without arguments.
Those call sites still do not establish the native token's cryptographic
audience, account or replay restrictions. This reinforces the decision to keep
real proof forwarding unsupported. The adapter's help and `--check` make the
Windows/RPC distinction explicit without starting either backend.

Only the reviewed JSONL envelope `id`, `method`, `params:{}` is accepted,
without a `jsonrpc` field. Raw duplicate keys, null/missing IDs or params,
extra payload, malformed Unicode, fractional/exponent IDs and mixed
request/response fields fail closed. Integer IDs must fit signed 64-bit;
decoded string IDs are at most 256 bytes; the envelope is at most 4 KiB.
The connection must be initialized and ready, but no thread binding is needed:
official prewarming can precede the thread-start response. Live ordinary
server-call ID collisions are rejected.

The response is terminal and uses no long-lived server-call mapping; there is
no `serverRequest/resolved` event to wait for. Identical IDs on separate
backends remain independent and completed IDs may be reused. A per-backend
token bucket limits bursts to 64 and refills at 32 requests per second;
sustained excess closes the connection instead of queuing. The official
deadline is 100 ms, which the bridge cannot guarantee under pipe backpressure.
Write failure or shutdown never triggers a different-provider retry.

Normal proof forwarding remains unsupported. Empty attestation params carry
no endpoint, audience, account, challenge or request digest. The app-server
treats the returned token as opaque, and its
[official integration test](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/tests/suite/v2/attestation.rs)
demonstrates forwarding a supplied test token to a custom provider URL.
This proves transport does not enforce an OpenAI recipient. It does not prove
whether the real Desktop generator cryptographically binds its tokens.
Reviewed generator, audience/account binding, replay and recipient evidence
would be required before implementing real forwarding.

#### Explicit primary gateway-read mode

Set `gateway_oauth` to `primary-read-only-v0.166` and
`gateway_oauth_provider_name` to the separately reviewed exact display name of
the first backend's provider. The name is bounded to 128 UTF-8 bytes and checked
against the response; free-text names are not assumed safe merely because they
came from a backend. The first backend remains the sole account authority,
matching `account/read`. This is not a dual-provider readiness view.

The mode preserves `explicitGatewayOauth:true` in both children. In the pinned
[initialize implementation](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/src/request_processors/initialize_processor.rs)
this is a sticky policy suppressing automatic gateway browser authorization.
It does not disable existing credential use or refresh. The flag is never
treated as permission to start a login.

Only `account/gatewayOAuth/read` is routed, exclusively to the primary
backend. Omitted/null/empty-object client params become `params:null`;
selectors or additional fields are rejected. Every read is fresh. The result
must contain exactly `providerId`, `providerName`, `required`, `status`,
and `error`, matching the configured provider and reviewed name.
`required:false,status:null` means OAuth is not configured, not authenticated.
When required is true, status is the actual `notReady`, `started`,
`succeeded` or `failed` value. Non-null error text, raw RPC errors, duplicate
keys, unknown fields, wrong types or oversized responses become a fixed bridge
error, never a fabricated success or a redacted replacement status.

Safe primary `account/gatewayOAuth/changed` notifications preserve their
actual status and require the exact reviewed shape with `authUrl:null` and
`error:null`. Secondary notifications are not shown as primary state.
Authorization URLs and opaque errors are never forwarded. Unsupported
notification content fails closed; the bridge does not act on it.

Both `account/gatewayOAuth/login` and `account/gatewayOAuth/cancel` remain
unsupported and send no request to either child. A complete login
implementation would need the official connection ownership, long-running
response, cancellation, credential persistence and private URL handoff
semantics. The second provider must not acquire login authority from the
currently selected model or the primary account display.

The readiness [handler](https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server/src/request_processors/account_processor/gateway_oauth.rs)
reloads configuration, obtains/stores its manager and reads credential
readiness. It can read the credential store, update caches and publish readiness
changes. That specific status operation does not itself refresh credentials or
open a browser, but it is not a pure or side-effect-free read. Other official
initialization/turn paths can still refresh credentials. Enabling these modes
for real accounts needs the applicable authorization and policy review.

The JSONL requirements/auth implementation and fixture assumptions above are
pinned to official source `4f093f2748667f3b0e6a07e3d4055121cbd3ce64`, Codex
`0.166.0-alpha.6`. The separately reviewed 0.167 one-shot profile does not
automatically enable these JSONL modes.
Tests use synthetic processes and synthetic statuses/errors only. They do not
run a real Desktop proof generator or validate OAuth, proof headers, accounts,
model inference or service acceptance.

Additional boundary regressions submit a visibly synthetic token-shaped client
reply after the bridge has terminally rejected attestation. With no outstanding
server-call mapping, it must produce no delivery to either backend. This does
not make claims about arbitrary data on an independently valid ordinary
server-call response.

A native synthetic composition fixture also enables all three explicit JSONL
modes together: the observed Desktop initialize construction subset, a complete
non-null requirements gate, model discovery, thread-start prewarming and primary
gateway readiness. It checks capability preservation and process cleanup.
This is based on static source construction, not a captured runtime exchange,
a complete Desktop notification-filter table, a one-shot startup probe, or
successful GUI acceptance. The ordinary startup probe's null-only policy scope
is unchanged.

The official pinned source includes opaque-token serialization and failure
envelope unit tests, plus a localhost WebSocket integration test using fake
authentication. Those Rust tests are not run by this repository's Go CI.
Expanding that official harness would require a separately pinned Rust build
and verified local-only runtime setup; source review is not an executed result.

### Isolated official-runtime black-box test

The experimental Linux CI also invokes
[`build/test-official-runtime.sh`](../../build/test-official-runtime.sh).
Preparation downloads the official full Codex `0.166.0-alpha.6` Linux musl
release, verifies archive SHA-256
`c9400b373a2c17c71f48a6af592e8c92b9b212295182a482bf0b351e137c8967`,
and verifies the model catalog's Git blob against the pinned official source.
The release tag is mutable, so the archive digest is required. The Python
official container image is pinned by its linux/amd64 manifest digest from
[Docker's image metadata](https://github.com/docker-library/repo-info/blob/master/repos/python/remote/3.13-slim-bookworm.md).
No official executable is added to the Windows artifact.

All Codex execution, including version inspection, runs inside an ephemeral
non-root container with `--network none`, dropped capabilities,
no-new-privileges, a read-only root and inputs, bounded resources, and a private
temporary filesystem. The runner inspects these settings before starting it.
Only the test inputs are mounted; no host account state, environment secrets
or Docker socket is supplied. The test checks that only the loopback network
interface exists. Preparation can download dependencies, but the test
processes cannot reach external services. This does not claim that the
official executable never attempts an external connection.

The stdlib-only [Python harness](../../build/test-official-runtime.py) uses
fresh synthetic authentication fixtures matching the official test helper,
local HTTP/WebSocket Responses endpoints and an unmodified official catalog
entry. These fixtures are not real login credentials, model responses or
proofs. The fresh fixture configuration disables optional Apps and plugins
using the release-supported `features.apps=false` and `features.plugins=false`
settings. It does not use debug-only startup hooks or relax sandbox/network
policy. Optional plugin inventory and Apps MCP startup are outside this test.
Direct official-process controls distinguish an actual JSON-RPC error
from a clearly labelled arbitrary test string. They do not construct a Windows
attestation wrapper, generate a device proof or test upstream acceptance.

The bridge case uses two real official child processes with separate state
and local providers. Direct controls now omit `initialized` before native
model listing, and the bridge omits it before `account/read` with
`refreshToken:false`, model listing and the actual null-policy requirements
read. Account data comes only from fresh synthetic authentication fixtures. Two delayed real notifications each precede
another requirements read, whose result must equal the first native result.
No notification or successful native response is synthesized. Its assertions
also cover model selection and the no-OAuth gateway
read response, unavailable-proof
handling without a frontend proof request, and two synthetic-response turns
per provider. Each endpoint must observe an actual WebSocket handshake;
SSE-only success is insufficient. Managed/non-null policy and both individual
policy reads are not independently observed by this black-box fixture.
An unsupported or changed runtime result is a failing test,
not a fabricated success or a silently skipped check. Directly launched
processes must exit normally; the fixture does not independently prove normal
exit of both official grandchildren. Container teardown bounds their lifetime,
and the container's final exit status must also succeed.

This black-box test is separate from the upstream Rust tests and the
native-synthetic Windows fixtures. It does not establish Windows Desktop GUI
acceptance, real GPT/AIHub inference, OAuth login, managed corporate policies,
tool execution, or equivalence between Windows error tokens and RPC failure.
The production defaults, real-proof forwarding prohibition and unsupported
methods are unchanged. CI failures or unavailable execution remain explicit
verification gaps, even when the source-based design looks feasible.

`getAuthStatus` remains unsupported by default. Earlier static inspection found the local
main connection defaulting to `ensureAuth:false`; that did not establish the
actual installed Desktop startup sequence. The signed Desktop 26.1103.3517.0 /
backend 0.167.0-alpha.3.2 trace in
[run 42217188118](https://github.com/breakingtime126/magpie/actions/runs/42217188118/job/127891199826)
observed `getAuthStatus`, `account/read` and `configRequirements/read` after
successful initialize without a client `initialized` notification. Correcting
that readiness gate does not authorize or implement `getAuthStatus`.
Even `includeToken:false,refreshToken:false`
can reach credential-refresh behavior, so the bridge does not fabricate status
or call it a guaranteed offline read. At this historical stage `config/read`,
gateway/account login mutations and external-token refresh remained unsupported.
The later independent routing-only view below does not enable authentication. The inspected
`config/read` wrapper waits for readiness but is not a fixed serial step in the
main initialization function. Returning only one backend's full configuration
would misrepresent the other backend's provider and permission context.

The new read-mode tests use synthetic peers with an ordinary fixture initialize;
they do not establish real Desktop compatibility. The auth modes in this
section are limited to unavailable-proof errors and primary readiness inspection.
The separate CI-only no-auth status mode below adds no real-account support.
Further acceptance needs reviewed attestation destination/binding evidence,
complete OAuth account ownership and handoff design,
any remaining sanitized request/notification dependency trace, and separately
authorized real Desktop testing. No cloud artifact is a workaround for a denied
local software, installation or network policy.

The Windows CI package includes both the ordinary Magpie binary and the separate
native adapter, with per-file checksums and build provenance. It runs the built
adapter itself against two native synthetic backend processes, verifies clean
JSONL initialization and EOF shutdown, and checks both backend exit markers.
The observed Desktop base argv is also exercised through the compiled adapter;
both synthetic children assert normalized controls and their own provider pins.
Synthetic one-shot tests additionally exercise the compiled adapter, both digest
pins, complete result validation, disabled mode and pre-config sign-out rejection.
These checks do not test Desktop's launch hook, official startup effects, model traffic, or native
OS-signal delivery to the adapter. Unsupported authentication/configuration flows remain covered by local-rejection
regressions. The explicitly enabled requirements mode adds complete-policy
validation, fresh dual-backend comparisons and native/compiled transport tests.
Independent auth compatibility fixtures cover capability preservation, terminal
same-backend attestation errors, primary-only gateway status and rejection of
login, private URLs, unsafe errors and cross-provider response IDs;
none of these are represented as actual Desktop startup acceptance.

### Exact 0.167 auth opt-ins

Two additional, independent default-off modes select the auth contract reviewed
against Codex `0.167.0-alpha.3.2`: `attestation: "unavailable-v0.167.0-alpha.3.2"` and
`gateway_oauth: "primary-read-only-v0.167.0-alpha.3.2"`. The gateway mode still
requires the reviewed provider display name. Neither mode enables the other,
a startup probe, login, proof generation or requirements inspection. Legacy
names keep their original scope. Partial mode names, cross-version mode combinations, the
legacy startup profile and legacy requirements-read mode are rejected before
either child starts. The exact 0.167 startup profile and null-only requirements
mode may accompany these modes but do not enable them. Both executables must pass their configured SHA256
pins before either JSONL child starts; naming a profile is not runtime version
detection or an independent guarantee that the selected binary is official.
The JSONL runner checks only the operator-supplied executable digests. The
operator must independently verify each binary's version and provenance;
these checks do not verify a signature.

The auth implementation is reused under the separately reviewed source
[`c40729ea5cf8e947481b130c59807273eda2f8e9`](https://github.com/openai/codex/tree/c40729ea5cf8e947481b130c59807273eda2f8e9).
Attestation still receives only an origin-specific genuine RPC failure, including
prewarming before a thread response. Gateway inspection still preserves fresh,
validated primary readiness and safe null-URL/null-error notifications.
The true authentication flags, experimental capability and notification opt-out
list remain unchanged. The new independent `requiredHumanApproval` opt-in is
not implemented: true or malformed values fail initialization locally, with no
child initialize request; absent or explicit false is preserved. This guard
also applies when the exact 0.167 startup profile alone is selected.

Official 0.167 gateway storage adds `gateway_oauth.credentials_store`
(`keyring` by default, or plaintext `CODEX_HOME/secrets/gateway_oauth.json`
for `file`). The bridge never changes this choice or reads a keychain.
The explicit gateway flag still prevents automatic browser authorization, not
credential reuse or refresh. Login, cancel and external-token refresh remain
unsupported. `getAuthStatus` is rejected unless the separately selected
[CI-only no-auth mode](#exact-0167-ci-only-no-auth-status) or
[trusted stdio relay](#exact-0167-primary-trusted-stdio-auth-status) is enabled; none of
these modes is an offline substitute.

The exact-mode tests reuse native synthetic prewarm and primary-read assertions
and add version combinations, both pin positions, capability preservation,
human-approval rejection and zero-child-request checks. These are synthetic
protocol tests, not official Desktop handshake, proof, OAuth or service-acceptance
evidence. The real Desktop startup launch-parameter investigation is separate.

### Exact 0.167 null-only requirements reads

The separate default-off `config_requirements_read:
"compare-null-v0.167.0-alpha.3.2"` mode supports only the exact native
unrestricted response for Codex `0.167.0-alpha.3.2`. It does not rename,
weaken or enable `compare-safe-v0.166`; the legacy complete non-null policy
validator remains scoped to its reviewed version. It may coexist with the exact
0.167 startup, unavailable-attestation and primary gateway-read modes, each
enabled independently. Cross-version combinations fail validation. Both
configured executable SHA-256 pins must pass before either JSONL child starts;
the operator still owns version and provenance review, as for the auth modes.

The source oracle is official
[`c40729ea5cf8e947481b130c59807273eda2f8e9`](https://github.com/openai/codex/tree/c40729ea5cf8e947481b130c59807273eda2f8e9).
The [request declaration](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/common.rs#L1527-L1530)
uses `Option<()>`, not an empty object. Absent, null and an explicitly empty
client object are accepted by the bridge's existing no-argument adapter; each
child always receives `params:null`. The bounded optional native `trace` carrier
is accepted without propagation, as described below. Selectors, overrides,
unknown envelope fields and duplicate decoded keys fail locally. With this exact mode selected, duplicate
top-level client routing keys are rejected before map decoding, even when the
last duplicate method tries to hide an earlier requirements read. Ordinary
valid thread/provider routing is unchanged.

Every read creates two fresh requests. The complete
[response](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L838-L846)
from each child must contain exactly `requirements:null` and
`supportsIndependentSpeedModes:true`. Both validated results are deeply
compared before the genuine primary result is returned, with JSON whitespace
compacted only. No startup snapshot, cached success, fabricated null, removed
restriction or single-backend fallback can satisfy a later read. Unknown,
missing or duplicate fields, wrong types, every non-null requirements value
(including an empty object and a matching legacy policy), child RPC errors,
malformed envelopes and disagreement fail closed. Backend error bodies are
not forwarded.

The fresh [handler](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/config_processor.rs#L140-L156)
calls `read_requirements()` and maps the current allowed login methods.
The [mapper](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/config_processor.rs#L445-L455)
returns null only when requirements are absent and allowed login methods are
exactly API and ChatGPT. Empty homes alone do not prove this condition; managed,
system or credential-derived restrictions can still apply. The same policy,
authentication and cache side-effect review required for requirements inspection
still applies. A successful earlier probe is neither authorization nor evidence
for a later successful read. Requirements-only mode also rejects the unreviewed
`requiredHumanApproval` capability and does not enable either auth capability.

Implementation:
[`requirements_167.go`](../../internal/appserverbridge/requirements_167.go),
[`Router.Client/Server`](../../internal/appserverbridge/router.go), and
[`RunWithOptions`](../../internal/appserverbridge/run.go).
[`requirements_167_test.go`](../../internal/appserverbridge/requirements_167_test.go)
covers exact shapes, raw duplicates, malformed envelopes, both response orders,
both pin positions, cross-version combinations, source-byte preservation and
unsupported routes.
[`requirements_167_red_test.go`](../../internal/appserverbridge/requirements_167_red_test.go)
uses only APIs present at base `e1c8fb52c61406baba1a6d712c0fcea83aacaf0d`;
copying that file alone to the base supports an assertion-level red check.
Its native synthetic processes check a successful earlier one-shot followed by
a changed non-null JSONL policy, fresh recovery to null, disagreement, no-argument
wire params and EOF cleanup. They run through the existing `internal/proc`
lifecycle helpers and do not contact a model service.

These are test definitions, not an executed acceptance record. At authoring,
no executor was available: gofmt, build, vet, the new behavioral red and green,
race checks, the full suite and official-runtime tests were not run for this
change. The integrating commit/CI record must name actual checks and the exact
tested head. No Desktop GUI startup, real account, proof, OAuth login, model
inference, tool execution or corporate policy acceptance is established.
`config/read` remains unsupported by these auth modes; the separate
[routing control plane](#exact-0167-owned-routing-control-plane) supplies only its
own routing view. Login/cancel and external-token refresh remain unsupported.
Requirements inspection does not enable `getAuthStatus`; only its separate
CI-only no-auth or trusted stdio relay mode can do so. Requirements inspection never produces a proof or grants
login authority.

### Native initialize readiness regression

[`initialize_test.go`](../../internal/appserverbridge/initialize_test.go)
checks immediate reads after both successful native-shaped fixture responses,
all supported native ID forms, response order, optional early/delayed/duplicate
notifications, exact primary result and native error preservation, unchanged
capabilities, and continued rejection before or after failed/partial
initialization. It also keeps pre-response authentication traffic and
unsupported RPCs behind their existing guards.
[`initialize_run_test.go`](../../internal/appserverbridge/initialize_run_test.go)
runs compiled synthetic children and checks the recorded child traffic:
the first requirements read precedes every `initialized`, and only the two
notifications actually sent by the test appear. Both policy profiles are
covered. These are fixtures, not official-process or Desktop acceptance.

The official-runtime omission/delay checks above extend the existing isolated
Linux harness without new endpoints, dependencies, privileges or network
access. Their pass/fail evidence must come from the exact-head CI run.
No tests, Go formatting, vet, builds, race checks, full suite, or official-runtime
checks were run while preparing this connector-only source change.
The Windows Desktop trace remains a separate, non-network-none diagnostic;
this fix makes no zero-egress or completed Desktop acceptance claim.

### Native 0.167 request trace envelope

The exact 0.167 requirements-read adapter accepts the optional native `trace`
field: null or an object containing only optional `traceparent` and
`tracestate` values, each a string or null. Empty objects and empty strings
remain valid shapes. A single JSON-escaped spelling of a known key retains
ordinary JSON semantics; duplicate decoded keys, unknown/case/suffix aliases,
wrong types, malformed UTF-8/surrogates and control characters are rejected.
The bridge imposes its own bounds of 16 KiB per encoded carrier and 1,024
UTF-8 bytes per decoded string, within the existing 64 KiB requirements request
limit. These are conservative bridge limits, not limits claimed by the native
schema. No W3C header pattern is inferred from a string's contents.

The pinned source is
[JSONRPCRequest.json](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/schema/json/JSONRPCRequest.json),
[rpc.rs](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/rpc.rs),
[W3cTraceContext](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/protocol/src/protocol.rs), and
[app_server_tracing.rs](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/app_server_tracing.rs).
Native request tracing attaches a telemetry span parent; invalid W3C contexts
are ignored by that tracing path. This carrier is not an authentication
credential, permission profile or policy requirement. Accepting its shape
does not authorize login, refresh, attestation, tools or any new method.

**Acceptance does not enable telemetry propagation.** `Router.send` remains
unchanged: it reconstructs child requests with transport ID, method and params,
without copying the unscoped trace to either provider. No trace value is stored
as routing state or returned to the client. Each requirements read still sends
two fresh `params:null` requests and returns only the genuine primary result
after both complete exact native null-policy results pass all existing gates.
The adapter does not remove, normalize, synthesize or weaken any policy field.
Default-off modes, executable pins, initialization readiness, launch opt-ins
and default `getAuthStatus` rejection (`-32601`) are unchanged by trace
acceptance. The separate CI-only no-auth mode below is independently enabled.

The test-only observer shares the pure
[`NativeTrace167Valid`](../../internal/appserverbridge/native_trace_167.go)
predicate. For an unambiguous, correlated client `getAuthStatus`,
`configRequirements/read`, `config/read`, or `experimentalFeature/list` request
containing trace, it records only
`tracePresent:true` and `traceValid:boolean`. Validity denotes the bounded
schema shape, not a usable W3C context. No IDs, state, lengths, hashes or raw
carrier values enter diagnostic records. Missing trace preserves the old
metadata shape; ambiguous outer envelopes, notifications, other directions,
duplicate IDs and exhausted correlations receive no trace metadata.
Diagnostic envelope IDs use the existing bridge string-or-int64 validator,
including its 256-byte string bound, without changing general correlation.

Only a valid auth envelope and valid trace (if present) can expose the existing
auth parameter flags. Params must still be an object with only the exact two
optional native fields: missing or null field values mean the native false
default, and only literal booleans are otherwise accepted. Missing/null params,
unknown fields, duplicate decoded keys and wrong values remain invalid.
This is request metadata, not observed authentication state or a guarantee
that false flags cannot refresh credentials. The observer's prior optional
`jsonrpc:"2.0"` auth spelling is retained only in diagnostics; it is not newly
accepted by production requirements reads. Observer forwarding remains
byte-for-byte identical, including rejected or oversized input.

The signed Desktop 26.1103.3517.0 / backend 0.167.0-alpha.3.2
[trace run 42221864689](https://github.com/breakingtime126/magpie/actions/runs/42221864689/job/127906694973)
showed successful initialize and account reads, unsupported `getAuthStatus`,
and rejected requirements reads. Its absent auth flag metadata proves only
that strict recognition did not complete; it does not prove trace was present
or caused either failure. A later actual run must establish that fact using
the new fixed metadata. No private request bytes were inspected to assert it.

[`native_trace_167_red_test.go`](../../internal/appserverbridge/native_trace_167_red_test.go)
and [`native_trace_red_test.go`](../../build/desktop-rpc-observer/native_trace_red_test.go)
use only APIs present at base `3c3d18fb877e8d376836459c498b1f99f501632d`
for assertion-level old-base reds. Other fixtures cover local size boundaries,
escaped keys, decoded duplicates, private canaries, dual-policy rejection,
unchanged child requests, byte-exact observation and exact PowerShell
publication schemas. Source preparation used connectors only: gofmt, Go tests,
behavioral reds, race/vet/build checks and PowerShell fixtures were not run
while authoring. Their result must come from exact-head CI. The Windows
Desktop diagnostic is not network-none; the separate Linux official-runtime
container's isolation does not establish Windows zero egress or Desktop GUI,
real-account, inference or corporate-policy acceptance.

The observer initially added 14 exact public startup method labels from the
pinned [ClientRequest.json](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/schema/json/ClientRequest.json);
the current diagnostic extends that list to the complete 183-name public
request vocabulary and adds boolean-only request-shape metadata for
`config/read` and `experimentalFeature/list`. The owning
[complete vocabulary and read-shape contract](desktop-cold-start.md#complete-pinned-rpc-vocabulary-and-read-request-shapes)
defines those exact fields and publication limits. This is diagnostic
classification, not generic routing, payload capture, request execution, or
evidence that Desktop actually sent a particular candidate. `config/read` remains
unsupported by default; its separately enabled
[routing control plane](#exact-0167-owned-routing-control-plane) is described below.
The separately enabled
[feature-page comparison](#exact-0167-shared-experimental-feature-pages) below
is the sole production exception for `experimentalFeature/list`; diagnostic
recognition alone enables nothing. Existing trace and auth-flag recognition
gates are not relaxed. The earlier
[`native_methods_red_test.go`](../../build/desktop-rpc-observer/native_methods_red_test.go)
continues to cover its assertion-level public-label regression.

### Exact 0.167 CI-only no-auth status

The independent default-off configuration property
`auth_status: "primary-no-auth-v0.167.0-alpha.3.2"` enables only the narrow
`getAuthStatus` diagnostic described here. It is for the explicitly reviewed
disposable CI launch profile, not real-account support. It is not enabled in
defaults, examples, baseline profiles, or by requirements inspection, startup
probing, attestation, gateway inspection, or trace acceptance.

Both configured executable SHA-256 pins must pass before either child starts,
even when this is the only selected mode. Exact 0.167 modes may coexist;
legacy 0.166 mode combinations and partial/future mode names fail validation.
A mode string plus operator-supplied digest is not independent version,
provenance, signature, or credential-isolation verification. The operator must
review those facts separately. Existing initialization readiness and
`requiredHumanApproval`/other capability gates remain in force.

The source oracle is the pinned
[0.167 account processor](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/account_processor.rs#L1097-L1168)
and its [v1 types](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v1.rs).
The handler defaults absent/null flags to false, calls its requested-refresh
helper, reloads configuration, then branches on the selected provider's
`requires_openai_auth`. When false, it returns the genuine triple
`{authMethod:null,authToken:null,requiresOpenaiAuth:false}` before
`auth().await` or `get_token()` in the credentialed branch.

**This is not a credential-isolation or offline guarantee.** The reviewed CI
profile uses disposable empty homes, no inherited secrets,
`requires_openai_auth=false`, and a loopback discard endpoint. That setup is
not established merely by setting `auth_status`. Native AuthManager startup
credential loads predate this RPC, and the configuration/refresh path still
exists. If this mode is misused against a credentialed backend, preserving
`includeToken:true` can cause the bridge to receive a token before it rejects
the response. Result validation prevents onward relay; it cannot prevent that
prior receipt or undo upstream side effects. Windows CI is not network-none,
and the separately isolated Linux runtime does not establish Windows zero
egress. No real credentials may be used in these fixtures.

A request must be an unambiguous bounded native envelope with only `id`,
`method`, `params`, and optional native `trace`. IDs use the existing bounded
string-or-int64 native profile. Params must be an object containing only
optional `includeToken` (absent, null, false, or true) and `refreshToken`
(absent, null, or false). Explicit true refresh, unknown fields, wrong types,
decoded duplicates, invalid IDs, and invalid trace are rejected locally.
Single escaped spellings of exact known keys keep their normal JSON meaning.
The existing 64 KiB/depth/container and native trace limits still apply.
The entire original params object, including true/false/null/absence, reaches
only the primary child with JSON whitespace compacted. No flag is rewritten,
no request is fanned out, and no failure triggers retry or secondary fallback.
The unscoped trace is accepted without propagation, as above; it is not
permission or authentication data. Each accepted call is fresh, not cached.

Success requires the actual primary response to contain exactly the complete
three-field native no-auth result above. Non-null auth methods/tokens, a
requires-auth flag other than false, missing/unknown/duplicate fields, malformed
envelopes, and mismatching values reject the entire result. No removed field,
synthetic null, fabricated success, or secondary result can satisfy this gate.
The genuine validated primary result bytes are returned, compacted only by
JSON encoding. An ordinary well-formed native error preserves its signed
64-bit code but replaces all source message/data with the fixed text
`primary no-auth status unavailable` and omits data. Invalid results/errors
receive `-32000` with the same fixed text. Malformed routing/correlation
envelopes may terminate the connection without a response. A bounded scoped
error-envelope validator permits valid discarded control escapes and Unicode
replacement characters, while rejecting malformed UTF-8/surrogate escapes,
decoded duplicates, and size/depth/container overflow. It does not relax
policy or trace validators and never emits arbitrary native text or data.

All backend messages now receive a top-level decoded-duplicate check before
correlation, including with this mode disabled. Otherwise a duplicate ID could
select another pending method through last-key-wins decoding and bypass the
appropriate result gate. This check does not add a global field allowlist,
field-count limit, text profile, or recursive restrictions on arbitrary
tool/user result data. Existing stricter method-specific validation remains.

Implementation and synthetic regressions:
- [`auth_status_167.go`](../../internal/appserverbridge/auth_status_167.go):
  strict primary-only requests and genuine no-auth response validation.
- [`auth_status_167_test.go`](../../internal/appserverbridge/auth_status_167_test.go):
  optional flags, unchanged true params, native IDs/trace bounds, response
  canaries, exact int64 error codes, opaque valid text, duplicates, wrong-backend
  responses, ID aliases, version/pin gates, and unchanged capability guards.
- [`auth_status_167_red_test.go`](../../internal/appserverbridge/auth_status_167_red_test.go):
  old-API assertion reds for the new mode and global duplicate-ID rejection.
  This file can be copied alone onto
  `4e75e65d04f36618fd05dcd858dd19ceb3bc0a4c` without production changes.
- [`auth_status_run_test.go`](../../internal/appserverbridge/auth_status_run_test.go)
  and the [compiled adapter fixture](../../cmd/magpie-codex-bridge/auth_status_test.go):
  full synthetic lifecycle, unchanged requests/results, rejected token/error
  sentinels, fresh recovery, sole-mode pin checks, disabled rejection, no
  secondary reads or startup probe, safe diagnostics, and both child EOF markers.
  These use copied fixture executables, not actual accounts.

Source preparation was connector-only. No gofmt, unit/integration test,
behavioral red, race/vet/build, PowerShell, or real Desktop run was executed
while authoring these blobs. Exact-head CI must establish the actual results,
including a real correlated Desktop `includeToken:true` request receiving the
validated native no-auth success. Synthetic protocol success alone is not
Desktop GUI acceptance, real authentication, model inference, tool execution,
or corporate policy acceptance.

### Exact 0.167 shared experimental feature pages

The independent default-off property
`experimental_feature_list: "compare-v0.167.0-alpha.3.2"` enables only
unscoped `experimentalFeature/list`. It is absent from examples and defaults.
Both configured executable SHA-256 pins must pass before either child starts,
including when this is the only selected mode. Partial/future names and
mixtures with the retained 0.166 modes fail validation. Other exact 0.167
modes may coexist but remain separate opt-ins. As with the other modes,
operator-supplied pins do not independently verify version, signature,
provenance, or credential isolation.

The source oracle is official
[`experimental_feature.rs`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/experimental_feature.rs),
[`catalog_processor.rs`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/catalog_processor.rs#L358-L459),
and the [feature registry](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/features/src/lib.rs).
The request requires a params object containing only optional `cursor`
(string or null), `limit` (uint32 or null), and `threadId` (null only).
Missing fields preserve their native defaults. A non-null thread ID, even a
known owned ID, is unsupported here. The native string-or-int64 ID and bounded
optional trace rules apply; duplicate decoded keys, unknown envelope/param
fields and malformed text/types fail locally. Trace is never propagated.

Each accepted call sends fresh requests to both children with the **actual
native params values**, including omitted fields and explicit nulls. Ordinary
JSON serialization may compact whitespace or escape equivalent string bytes;
there is no semantic transformation. Limit is never
discarded, capped, defaulted, or rewritten, including `0` and `4294967295`.
The native producer clamps zero to one, uses its own full size when limit is
absent/null, and interprets a cursor as an offset into its current registry.
A subsequent cursor request is another fresh paired read. The bridge neither
exhausts native pages, creates a snapshot, replaces a cursor with null, nor
uses the generic merged model/thread-list paginator.

Success requires both complete native pages to agree: every row field,
boolean, nullable metadata value, row order and `nextCursor`. Each row has
exactly `name`, `stage`, `displayName`, `description`, `announcement`,
`enabled`, and `defaultEnabled`. Stages are the five native enum values.
Beta metadata is string-valued (including empty strings); other stages have
three null metadata fields. Flags are literal booleans. Duplicate feature
names, missing/extra fields, wrong types, malformed Unicode and duplicate
decoded keys fail closed. Native non-null response cursors are canonical
positive decimal offsets. Only a validated genuine empty terminal page can
produce an empty success, never an error fallback.

After equality, the actual primary result is returned through the same JSON
serialization, without field reconstruction, flag replacement, union/intersection,
or a prior cached result. Any child error, malformed result or disagreement
returns only `-32000` and `shared experimental features unavailable`.
Native error message/data are not relayed. Correlation violations can end the
connection. Failures release only that pending group immediately and retain
the existing bounded transport-ID tombstones for its late siblings. Up to four
active bridge request groups may be pending concurrently for feature reads,
each with fresh paired native requests and independent result comparison. A half-completed pair still occupies one
slot. The fifth read fails locally with `-32000` and
`experimental feature list request limit reached`, without sending child
requests or disturbing active reads. Completion or group failure frees the slot;
an abandoned native sibling may still finish later and is discarded through its
tombstone. This cap bounds active bridge groups, not outstanding native work
across failed attempts. Identical params are not coalesced, and different pages may complete out of
order. Readiness and global active-input caps remain unchanged. Duplicate active
client IDs, including native numeric aliases `-0` and `0`, remain invalid,
including through another route.

These are local conservative budgets, not limits promised by the native
schema: 64 KiB per request, 256 decoded bytes per input cursor, 128 bytes per
feature name, 4,096 bytes per metadata string, 256 rows/container entries,
depth 12, and 128 KiB per complete child response envelope and separately
validated result. The two retained results for each read are capped at 256 KiB;
the four-read cap bounds their combined budget to 1 MiB. A result at
its standalone limit still needs envelope overhead when routed. No next-page
responses are automatically requested or retained. The pinned registry has
201 rows, a 39-byte longest key and a 90-byte longest literal; the source-derived
Windows-default fixture is 30,741 compact JSON bytes. That fixture reconstructs
`enabled` from defaults for testing and is **not an observed live policy**.
The larger response budget has a regression above 64 KiB and at the full
envelope boundary. The shared strict JSON reader preserves the previous
auth-error byte/container/depth checks; policy/trace validators are unchanged.

**A read-only API is not an offline guarantee.** Every unscoped native call
reloads the child's current configuration. This can consult or refresh managed,
system, credential-derived and network-backed state. Separate homes and page
equality do not prove credential isolation, equal underlying policies, or
zero egress. No feature is enabled or disabled by the bridge.
`experimentalFeature/enablement/set`, non-null thread-scoped feature reads,
and previously unsupported authentication routes remain unsupported.
This feature mode does not enable `config/read`; only its separate routing
control plane can supply the owned routing view described below. Existing capability guards, including human-approval rejection,
remain in force; this mode does not enable an auth capability.

Implementation:
[`experimental_feature_167.go`](../../internal/appserverbridge/experimental_feature_167.go),
[`native_json.go`](../../internal/appserverbridge/native_json.go), and the
existing configuration/router/runner.
[`experimental_feature_167_test.go`](../../internal/appserverbridge/experimental_feature_167_test.go)
covers native pagination inputs, source-byte preservation, full comparison,
strict requests/responses, large pages, malformed Unicode, ID aliases,
cleanup/tombstone caps, default-off and version/capability gates.
[`experimental_feature_167_concurrency_test.go`](../../internal/appserverbridge/experimental_feature_167_concurrency_test.go)
covers overlapping native pages, out-of-order completion, fresh identical reads,
isolated failures, duplicate IDs, half-completed pair counting and cap recovery.
It uses only APIs present at
`ace340842ea5d61818e8e416df1b5017bf7847ae`; copying that file alone and running
`TestExperimentalFeature167ConcurrentPages` gives an assertion-level behavioral
red for the old singleton guard.
[`experimental_feature_167_red_test.go`](../../internal/appserverbridge/experimental_feature_167_red_test.go)
uses only APIs from base `2ab7bcd946dba06d9467b9db2dfa5b799a24d87d`;
copying that file alone supports an assertion-level unknown-mode red.
[`experimental_feature_run_test.go`](../../internal/appserverbridge/experimental_feature_run_test.go)
and the [compiled adapter test](../../cmd/magpie-codex-bridge/experimental_feature_test.go)
exercise genuine subprocess lifecycle using copied synthetic executables:
fresh paired reads, supplied limits/cursors, unchanged source results,
sanitized failures, recovery, both pins before either child, default rejection,
and child EOF markers. The lifecycle test file plus its fixture dispatch hook
also compiles against that base and must fail its fresh-page assertion.

These source blobs were prepared with connectors only. Go formatting, unit/
lifecycle/compiled-adapter tests, assertion-level reds, race/vet/build checks,
the full suite and real Desktop behavior were **not run while authoring**.
The integrating CI record must give the exact tested head and actual outcomes.
No real account, model inference, tool execution, Windows zero-egress,
corporate-policy or completed Desktop GUI acceptance is claimed.

### Exact 0.167 owned routing control plane

The independent default-off setting
`control_plane: "routing-v0.167.0-alpha.3.2"` enables a **bridge-owned routing
defaults view**, not a native effective configuration or shared execution-policy
authority. It is absent from ordinary examples/defaults. It does not implement
projects, filesystem access, configuration writes, authentication or account
mutation. It does not inject or alter approval, sandbox, permission-profile,
workspace-root or environment settings.

Activation also requires `control_home`: an explicit, existing absolute directory
for the frontend server's own state namespace. The caller creates this directory;
the bridge never creates it, copies a native configuration into it or invents a
`config.toml`. It must not overlap either child's HOME or CODEX_HOME in either
direction. Existing canonical-path and file-identity checks reject symlink aliases.
A stray `control_home` with the mode disabled is rejected. It is a namespace
declaration, not a filesystem grant, broker allowlist or proof of OS isolation.

Both configured executable SHA-256 pins must match before either native child
starts, even when this is the sole selected compatibility mode. Partial/future
mode names and combinations with retained 0.166 modes are rejected. The native
provider identifier `magpie_bridge` is reserved and cannot be configured on a
child. Other exact 0.167 opt-ins remain independent. The existing 0.167
required-human-approval capability guard also applies; this adds no new auth-state
gate and enables no authentication capability. Operator-supplied pins bind the
selected files, not independently their version, signature or provenance.

#### Frontend initialization and native homes

This mode validates both complete native initialize responses before readiness.
Each must contain exactly `userAgent`, `codexHome`, `platformFamily` and
`platformOs`. The reported native home must match that child's configured home
or its canonical spelling resolved before launch. Windows drive/UNC
extended-length spelling and case differences are normalized lexically. No
child-supplied arbitrary path is resolved, opened or inspected.

The common user agent and platform metadata must agree. Only then does the bridge
return those genuine common metadata values with `codexHome` replaced by the
resolved, explicitly designated `control_home` and become ready. This is a
projected bridge response, not an unchanged native initialize response. Missing,
unexpected or differing metadata and native initialization failures produce a
sanitized failure and never grant readiness. Child environment variables and
CODEX_HOME paths remain unchanged. Initialization without this mode keeps its
previous behavior.

A Desktop attempt to read a nonexistent control-home config file is a real
unsupported dependency. Do not satisfy it by copying a private backend config,
returning fictional file bytes or expanding filesystem access.

#### Strict routing config request and fresh policy precondition

The `config/read` params must be an object containing only optional
`includeLayers` and `cwd`. The six accepted forms are `{}`,
`{"includeLayers":false}`, `{"cwd":null}`,
`{"includeLayers":false,"cwd":null}`, `{"includeLayers":true}` and
`{"includeLayers":true,"cwd":null}`. In the pinned native
[ConfigReadParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L390-L398),
omitted `includeLayers` defaults to false and absent/null `cwd` means no working
directory. All six forms select the same unscoped session settings and origins.
Only explicit true includes the one genuine bridge-owned session layer; false or
omission omits `layers`. Accepting null `cwd` does not enable directory-scoped
configuration.

Explicit null `includeLayers`, every string `cwd` (including empty),
wrong field types, missing/null/non-object params and unknown fields fail
locally. Native string-or-int64 IDs and the existing bounded optional trace
shape apply. The 64 KiB strict policy JSON reader rejects decoded duplicate keys,
invalid UTF-8/surrogate text, depth/container overflow and trailing content.
Trace metadata is not propagated.

After both children have initialized, every accepted call sends a fresh internal
`configRequirements/read` with native `params:null` to each child. It preserves
the existing exact 0.167 validator: success requires two genuine complete
`{"requirements":null,"supportsIndependentSpeedModes":true}` views. Non-null or
unsupported policy, malformed envelopes, errors and disagreement fail the entire
read with `bridge routing configuration unavailable`. Backend error text/data
never appears in the response. A prior success is not cached. Up to four
independent config reads may be outstanding, including mixed `includeLayers` values and repeated identical
params. Each owns a fresh native pair and its own frontend ID, requested shape,
policy results and byte budget; no reads are coalesced. A half-completed pair
still occupies one slot. The fifth read fails locally with a bounded request-limit
error. Completion or failure frees only that group's slot. A failed group's late
sibling cannot complete another read, including a newer one reusing its frontend
ID. The cap limits active bridge groups, not all native work across failed
attempts: an abandoned native sibling may still finish. Ordinary active-request
limits and bounded late-response tombstones apply.

These internal policy reads do not enable the public requirements route. They
also do not establish equal effective execution permissions or authorize a shared
policy claim. Native policy reloads may refresh credential-derived, managed or
network-backed state; this is not an offline or no-side-effects guarantee.

**No native `config/read` is ever issued.** The returned config is constructed
from the bridge's actual routing state rather than filtered native config.

#### Exact owned result and provenance

The result contains `config` and `origins`. With `includeLayers:true` it also
contains exactly one `layers` entry; otherwise `layers` is omitted, not null or
an empty array. The same published session snapshot supplies all three fields.
`config` contains:

- `model_provider: "magpie_bridge"`, the reserved frontend routing authority.
- Optional `model`, the actual alias in the current complete merged model catalog
  that the existing router would use for an omitted new-thread model. The first
  successful control-plane initialization has already committed an explicit
  catalog default; the initial private model-less snapshot is not exposed to the
  frontend. A later successful refresh with no default omits this field. It is
  never fabricated or set to null, and `config/read` itself does not read the catalog.
- `magpie_bridge`, an explicit extension with `version:1`,
  `view:"routing-defaults"`, `provider_routing:"fixed-per-thread"`,
  `execution_policy:"backend-owned-per-thread"` and `model_catalog:"model/list"`.
  Its `supports` object has exactly `config_writes:false`, `layer_reads:true` and
  `cwd_reads:false`. Its `provenance` object names
  `model_provider:"bridge-routing"` and, only when model is present,
  `model:"bridge-catalog-default"`.
- The extension's `backends` array contains exactly two entries, each with only
  `id`, configured native `provider`, `model_prefix` and `catalog_source`.
  A prefix is `magpie/<backend ID>/`. Catalog source is `native-model-list` or
  `configured-descriptors` according to that backend's existing configuration.
  The default alias therefore unambiguously identifies its catalog provenance.
  Configured descriptors are declared metadata, not native availability,
  entitlement or successful service use.

No approval, sandbox, permission, login, instruction, feature or native
`model_providers` fields are advertised. No path, endpoint, header, credential,
environment table or native storage location enters the owned config view.
The bridge materializes one real, in-memory, service-owned session layer when
the control-plane mode is enabled. It publishes a replacement only when a whole
successful model catalog commits. Routes, default and session layer are committed
together; incomplete or failed refreshes retain the previous complete snapshot.
An omitted-model `thread/start` consumes that published snapshot's model, not
a separately reconstructed config default. Explicit model selection and fixed
per-thread ownership remain unchanged.

The layer has exactly `name:{"type":"sessionFlags"}`, `version` and `config`.
Its config equals the returned effective owned config. It has no file path,
`profile`, `disabledReason`, writable-user claim or empty policy layer.
`origins` contains one entry for every primitive leaf, using dotted paths and
array indexes such as `magpie_bridge.backends.0.id`. Containers and empty arrays
have no origins. Each entry is exactly the same layer `name` and `version`.

This source uses the pinned native implementation's service-owned session
semantics, not merely the wire enum's older `-c/--config` comment.
[`SessionThreadConfig`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/thread_config.rs)
is documented as values owned by the service that starts or manages the session,
and converts them to `SessionFlags`. The native
[`ConfigLayerStack::with_session_plugins`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/state.rs)
also replaces such layers at runtime. The bridge applies this provenance to
its own actual routing authority only. It does not claim that either native
backend loaded this layer or that the layer supplies native execution settings.

The version follows native
[`version_for_toml` and `record_origins`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/fingerprint.rs):
`sha256:` followed by the lowercase SHA-256 of recursively key-sorted compact
UTF-8 JSON for the layer config. The owned schema uses strings, booleans,
integers, objects and arrays. Its serializer deliberately preserves literal
Unicode, HTML characters and U+2028/U+2029 as native serde_json does, while
escaping quotes, backslashes and control characters. Literal backslash-u text
is not confused with Unicode escapes. No filesystem timestamp or native
configuration version participates.

The mode caps the complete wire-serialized layered result at 64 KiB, including
both config copies, origins and JSON escaping. A config that already exceeds
this budget is rejected before fingerprinting; a final layered-result check
also catches duplication and HTML/Unicode escape expansion. An oversized
catalog-derived default fails the catalog refresh with the sanitized routing
error before publishing any new routes, default or session snapshot. The prior
complete state remains usable. Nothing is truncated, and the disabled mode is
unchanged. This conservative control-plane budget prevents a later output-frame
failure from terminating an otherwise usable bridge connection.

A config read selects the complete current snapshot after its second fresh
policy response. A catalog refresh completed while the policy pair is pending
is therefore reflected consistently in config, origins and layers. Reads do not
republish the session. Equal config content has the same version; default
addition, change or removal changes the version, while a catalog change that
does not alter owned routing settings need not. This in-memory read-only layer
needs no persistent file or write/CAS API. It does not change the control-home
filesystem boundary.

The source oracle is
[protocol/v2/config.rs](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs)
and its
[ConfigReadResponse JSON schema](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/schema/json/v2/ConfigReadResponse.json).
That JSON schema requires only config and origins; Config has no required
properties and accepts extensions. Generated TypeScript nevertheless describes
nullable policy fields and layers as required. This is therefore a deliberately
partial JSON-schema-valid owned view, **not proof of compatibility with every
native or Desktop consumer**. Never add invented null policy fields to hide that
difference.

#### Logical provider routing

Only known protocol fields are translated. On thread start/resume/fork, an
omitted/null provider or `modelProvider:"magpie_bridge"` is accepted. Other
explicit provider IDs are rejected in this mode. The selected model alias
chooses a new thread's owner; an existing thread's previously verified owner
chooses resume/fork. Only that owner's configured native provider is sent to the
child. The logical provider never becomes a native config value or model-service
destination.

For thread listing, `modelProviders:["magpie_bridge"]` becomes a single-element
filter for each child's own native provider on **every** native page. Omitted,
null and empty filters keep their existing distinct native semantics. Unsupported
provider entries are rejected before native reads or bridge pagination.

Every native Thread must pass the existing provider/ownership checks before its
frontend `modelProvider` is projected. This covers thread list, started
notifications and the supported Thread-returning response siblings:
start/resume/fork/read/unarchive/revert/legacy rollback. Top-level
start/resume/fork provider fields and `threadSettings.modelProvider` are also
validated against the sending owner before projection. Missing/null/foreign
required providers fail closed. Duplicate keys are rejected on the consumed
routing objects, including Thread-returning siblings.

Native IDs and ownership remain unchanged. Existing model aliases and
cross-provider rejection still apply. Arbitrary nested user/tool content is not
recursively scanned or rewritten. Existing native permission fields,
approval routing, account ownership and attestation rules are preserved.

#### Null-only thread-list lineage

Only the default-off exact routing mode accepts explicitly null
`parentThreadId`, `ancestorThreadId` and `attachedToThreadId` on `thread/list`.
Each is an ordinary `Option<String>` in the pinned
[native params](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs#L1497-L1511):
null and omission both deserialize to no lineage constraint. The native
[processor](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/thread_processor.rs#L2799-L2835)
checks mutual exclusion and selects lineage behavior only for non-null values.
All null combinations are therefore accepted, without inventing parent
ownership or enabling lookup of an unknown, known or foreign parent ID.

Every non-null value, including empty strings, arrays, objects and the string
`"null"`, fails locally before a backend send. Duplicate decoded parameter keys
remain invalid, even when both spellings contain null. Mode-off behavior and
the ordinary `validateParams` allowlist are unchanged. A validation-only copy
recognizes the three reviewed null fields; the original params are retained
and each explicit null is forwarded on **every page of both native lists**.
An omitted field remains omitted. Provider-filter translation, owner-specific
exclusions, collision checks, page/row/byte caps and snapshot pagination remain
unchanged. Cursor-only continuation still consumes the already collected
snapshot; this no-op filter extension creates no new sort or scope identity.

This exception does **not** include `projectId`. The pinned
[project and section filters](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs#L1464-L1484)
use double-option deserialization: omission is unrestricted, while null means
unassigned or unsectioned. `projectId:null` remains unsupported and is never
removed to make a request succeed. Existing `sectionId` behavior is not expanded
by this slice, and native section identifiers are not claimed as one shared
cross-backend namespace.

`section_position` remains rejected before any backend send, including with a
non-null `sectionId`. The pinned native implementation requires a non-null
[section filter](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/thread-store/src/local/list_threads.rs#L122-L165)
and SQLite. Its default direction is ascending, and it orders by the internal
nullable signed-64-bit section rank followed by the thread ID in the same
[direction](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/state/src/runtime/threads.rs#L1599-L1632).
Ranks and their allocation belong to each store's section. The public
[Thread record](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread_data.rs#L205-L259)
exposes `section` and `sectionEnteredAt`, **not the rank**. Native cursor
positions do not supply missing row ranks or establish shared section identity.
The bridge cannot truthfully merge this ordering and does not substitute
timestamps, zeroes, page offsets or backend order. No section comparator,
numeric-rank validation or section mutation support is claimed.

#### Implementation and acceptance boundary

Implementation lives in
[`control_plane_167.go`](../../internal/appserverbridge/control_plane_167.go)
and the actual session layer, version and origins in
[`control_plane_session_167.go`](../../internal/appserverbridge/control_plane_session_167.go),
with mode/path validation in [`config.go`](../../internal/appserverbridge/config.go),
launch wiring in [`run.go`](../../internal/appserverbridge/run.go), exact native-ID
collision handling in [`native_trace_167.go`](../../internal/appserverbridge/native_trace_167.go),
and the existing ownership paths in [`router.go`](../../internal/appserverbridge/router.go).

[`control_plane_167_test.go`](../../internal/appserverbridge/control_plane_167_test.go)
covers strict requests, genuine fresh policy preconditions, errors/recovery,
default provenance, routing projections, provider/filter rejection, native
pagination, duplicate routing objects, unchanged arbitrary payloads and
permissions, mode/pin/version gates, explicit control-home isolation and
initialize metadata projection.
[`control_plane_run_test.go`](../../internal/appserverbridge/control_plane_run_test.go)
uses copied synthetic native peers through real subprocess pipes: config read,
actual fixture model listing, two owned projectless threads and two text turns
each, policy failure/recovery, process pins and EOF cleanup.
[`control_plane_167_red_test.go`](../../internal/appserverbridge/control_plane_167_red_test.go)
uses only base APIs and can be copied alone to exact base
`70e603d8481c28b9f84cadf2f7183255044e11b4` for an assertion-level unknown-mode red.
The independent
[`control_plane_167_optional_red_test.go`](../../internal/appserverbridge/control_plane_167_optional_red_test.go)
copies alone to `b047d781d1c26a2ee2b65a6c64cf2a9b94a5c9dc` and asserts acceptance
of all four native-defaulted params forms. Its failure diagnostic is
`native defaulted config/read params must be accepted`; it does not change the
older unknown-mode red. The routing unit tests also verify that each form still
requires fresh paired native requirements, yields the same owned result and
rejects a new non-null policy after prior success.

[`control_plane_167_layers_test.go`](../../internal/appserverbridge/control_plane_167_layers_test.go)
adds six-form shape equivalence, fresh policy checks for true and false, exact
source/hash/leaf origins, stable rereads, atomic catalog publication and failed
refresh retention, read/refresh overlap, actual omitted-model routing, default
removal, duplicate IDs/keys, byte bounds, no native/private fields, and Unicode,
HTML/control-character/literal-backslash version oracles, detached snapshot
bytes, exact layered wire budgets, escape amplification and oversized-catalog
rollback. The standalone
[`control_plane_167_layers_red_test.go`](../../internal/appserverbridge/control_plane_167_layers_red_test.go)
copies alone to `66e39e44c2c26d23023278d1a0b8799d26f2b8bb` and must fail with
`native includeLayers:true config/read must be accepted`. It also requires two
fresh native policy replies, one coherent session layer and all 18 initial leaf
origins with the correct content version. These are assertion-level oracles,
not an execution record.

[`control_plane_167_lineage_test.go`](../../internal/appserverbridge/control_plane_167_lineage_test.go)
covers default-off behavior, all null/absent combinations, non-null and duplicate
rejections before sends, exact preservation across both native pagination paths,
provider/exclusion/parent ownership, collisions and cursor/page caps. It also
keeps project filters and section-position sorting rejected. The independent
[`control_plane_167_lineage_red_test.go`](../../internal/appserverbridge/control_plane_167_lineage_red_test.go)
copies alone to `02600624b069bd5a00a339953eea5b8348e5a467` and must fail with
`native null lineage thread/list filters must be accepted`. Its successful path
checks both backends' native continuation requests and cursor-only frontend
continuation. These are prepared behavioral oracles; gofmt, red/green, race,
vet/build and the real native/Desktop listing checks were not run during this
source-only authoring step and must be recorded at the integrated head.

[`control_plane_167_concurrency_test.go`](../../internal/appserverbridge/control_plane_167_concurrency_test.go)
covers mixed layered/unlayered overlaps, independent identical reads, out-of-order
responses, per-group policy failures and byte limits, late siblings after frontend
ID reuse, strict requests, duplicate native IDs, the four-group cap and fresh paired calls,
half-completed slot retention and recovery. It also checks reads completing on
either side of a whole catalog commit, failed oversized refresh retention and
default removal. This standalone file uses only APIs from exact base
`09b526eb76a1f6e033c51db57eba58ed80026e86`; copying it alone there and running
`TestControlPlane167ConcurrentReads` must fail with
`second overlapping native-equivalent config read must reach both backends`.
The assertion is a behavioral-red oracle, not an execution record.

These source blobs were authored with connectors only. Formatting, compilation,
unit/lifecycle tests, old-base red, race/vet/build/full-suite checks, official
native synthetic integration and signed Desktop consumer behavior were **not
run while authoring**. The integration record must identify the exact tested
head and actual outcomes.

The next acceptance slice is an isolated no-account official-native synthetic
text flow plus one actual signed Desktop batch. If Desktop rejects the logical
provider, requires omitted global policy/config fields, requests config writes
or needs an unsupported filesystem/project authority, report that boundary and
stop. Do not invent native values, route primary config as a shared authority,
or widen access to make the test green. No production account, credential/proof
isolation, inference entitlement, tool execution, zero-egress, corporate-policy
or completed same-window Desktop acceptance is claimed here.

### Restricted Windows readiness (0.167.0-alpha.3.2 x64 only)

`windowsSandbox/readiness` is **not generally read-only**: the pinned official
handler can refresh Registered Core package registrations and metadata ACLs.
The existing default-off `routing-v0.167.0-alpha.3.2` control plane admits this
method only through the validated Windows runner with both native executables
matching SHA256 `4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382`.
The bridge's incoming environment and each final explicit child environment must
both lack `CODEX_WINDOWS_REGISTERED_CORE`. Presence, even empty or `0`, rejects
admission; the bridge does not clear a caller selection to enable this method.
Other binaries, wrappers, platforms, and the user's older 0.166 backend remain
unsupported. Digest verification is a pre-start path check: it retains the
existing file-replacement TOCTOU window, does not lock the running image, and
does not provide an OS credential isolation boundary.

In that exact official source, absence at process startup keeps the OnceLock
Registered Core flag false; dotenv cannot introduce CODEX_* variables. The
remaining readiness branch reads per-home sandbox artifact versions and parsed
configuration, without setup, account login, ACL or service changes. Native
readiness diagnostics can write a debug log when `SBX_DEBUG=1`; this is neither
a zero-file-write guarantee for the RPC nor for the whole native process. Source oracle:
`openai/codex@c40729ea5cf8e947481b130c59807273eda2f8e9`,
`app-server/src/request_processors/windows_sandbox_processor.rs:32-89,318-354`,
`windows-sandbox-rs/src/app_package.rs:31-43`, `arg0/src/lib.rs:270-301`, and
`windows-sandbox-rs/src/identity.rs:74-157` (all under `codex-rs/`).

Only omitted/null unit params are accepted. Two bounded, fresh native reads must
return exactly the same genuine `ready`, `notConfigured`, or `updateRequired`
status; disagreement, native errors and unrecognized shapes return a fixed
private-data-free error. No primary/best status selection, fallback or synthetic
readiness is used. Four concurrent logical reads are allowed; failed siblings
are abandoned using the existing bounded late-response tombstones. Trace
metadata is validated but not propagated across providers. Setup, policy writes,
and security feature changes remain unsupported. Even a genuine `ready` does
not establish that a conversation or sandbox command succeeds.

### Initial control-plane catalog barrier

In `routing-v0.167.0-alpha.3.2`, the frontend `initialize` reply now waits for
both validated native initialize replies **and** a complete initial model
catalog. It uses the ordinary `model/list` collector, pagination limits,
descriptor validation and alias/default selection. A visible explicit catalog
default is required; an empty, hidden-default-only or default-less catalog
fails initialization instead of inventing a first model. Configured descriptors
remain declared metadata, not native entitlement.

Routes, the chosen default and the bridge-owned versioned routing layer are
committed together before the original initialize ID receives the original
validated frontend initialization result. Internal catalog payloads, errors
and cursors do not become that result. While pending, other frontend requests
remain not-initialized; early/delayed `initialized` notifications retain their
existing forwarding behavior and do not release the barrier. Correlated catalog
failure clears the owned group and retains only bounded late-response tombstones.
Outer protocol errors before request association, including malformed envelopes
or wrong-backend replies, retain the existing fatal connection-close behavior;
they are not converted into recoverable initialization errors. A stalled
native catalog holds one bounded initialization group until the existing
connection/process cancellation closes it; this adds no independent timeout.
Non-control initialization and later explicit catalog refreshes are unchanged.
An unchanged routing default keeps the layer version; a changed actual default
changes it. Every config read still checks fresh real requirements from both
backends and never copies native configuration or policies.

This moves real catalog work into initialization. The pinned official
`model/list` path may resolve/refresh gateway credentials and use
`OnlineIfUncached`; it is **not** a universal local-only or zero-network read.
This change does not log in, import credentials, select another provider or
relax authentication. Credential-free disposable tests and configured metadata
are not proof of account entitlement or production Desktop usability.

The concrete defect addressed is the bridge publishing model-less initial
routing config before a complete catalog, with no guarantee that a frontend
re-reads it. The observed `Custom Model Medium` Desktop label is a motivating
symptom, not a proven causal diagnosis or a claimed fix. Tests cover the
initial barrier, backend/pagination order, early requests, failure and late
responses, declared/native mixed catalogs, cancellation and version behavior.
Actual Desktop validation remains separate from these protocol tests.

## Isolated profile draft preview

[`scripts/bridge-profile-preview`](../../scripts/bridge-profile-preview/README.md)
is a standalone Windows Forms script, not normal Magpie GUI startup. It uses
native path selectors and a pure allowlisted JSON draft builder; it never loads
backend accounts or invokes migrations, backend processes, the gateway or agent
wiring. It does not save/import configuration, change environment/security state,
or claim executable, symlink, policy, authentication or Desktop compatibility
validation. Compatibility modes stay absent/default-off. This is a developer
preview slice, not a one-click installer or a production account connection UI.

## Backend shutdown ordering

Shutdown closes all backend stdin streams, gives the existing EOF cleanup
grace period, then stops/reaps each owned tree before closing backend stdout.
A concurrent Windows pipe Close must not gate the owned stop/reap path. The
bridge joins its stream readers before returning. This removes a confirmed
ordering hazard; it is not proof of the unique cause of the historical Windows
malformed-output CI hang. The deterministic owned-child close-gate regression
and original malformed/family cases require actual Windows execution.

### Explicit self-authored Desktop model-menu fixture

The test-only Desktop path can separately select
`-fixture-profile synthetic-models-4239` with the exact reviewed 4239 build.
It reuses the native acceptance's wholly self-authored `test-alpha`/`test-beta`
records without a catalog input, old catalog download or frontend file broker.
Every explicit `-catalog`/`NativeControlCatalog` occurrence is rejected in this
mode, including an empty value. Native approval, sandbox, auth, owned homes and
loopback limits are unchanged. Production bridge code is not involved in the
fixture-profile choice.

The existing policy-trace workflow requires its prior 4239 welcome/official-tool
opt-ins plus `[desktop-synthetic-models-v1]`. Readiness and final fixture audit
use `desktop-self-authored-fixtures-only`; readiness identifies the source as
`self-authored-test-models-v1` rather than claiming the old catalog digest.
The old profile remains unchanged but is not a fallback for a new-profile
failure. The post-Desktop legacy acceptance stays unrun in this welcome mode.

This is only a model-menu observation experiment through the existing owned
window and bounded official-tool path. Exact profile-specific labels are
`Synthetic Alpha [alpha]` and `Synthetic Beta [beta]`; the profile does not
activate model items or send chat messages, and successful fixture audit requires
zero model requests at both endpoints. Identity, visible/enabled/unique
control checks and protected-screen stop conditions remain required. The RPC
observer separately counts only complete known self-authored model tuples,
without publishing names, IDs or payloads or changing forwarded bytes.

A successful native two-thread/four-turn test is not a Desktop verdict; a
received two-model response is not proof that both menu items are visible.
Distinct original slugs are the next minimal experiment, not a diagnosis of
the earlier first-menu observation. The [fixture documentation](../../build/control-plane-native/README.md)
records the profile contract and evidence limits.

The menu-only follow-up recognizes the exact current-model Button label
`Synthetic Alpha [alpha] None` observed in Desktop run
[42413441847](https://github.com/breakingtime126/magpie/actions/runs/42413441847)
at `8c9bd8d617981b05c1a01cf8db1e3e0709742373`. This label is profile-local and
only identifies the same collapsed menu opener; `None` is not reported as
Medium. It does not expand model-item counts, submenu names or keyboard model
activation. Exact typed resolution, unique visible enabled Button identity and
collapsed-state checks still precede Expand. The preceding run stopped before
any menu action; this follow-up has not yet established two visible menu items.


### Optional immutable bridge-owned alias catalog

`bridge_model_catalog_json` is default-off and requires the routing control
plane and actual native model lists on both backends. It names a real absolute
regular file under the bridge's actual `control_home`, not either native home.
The startup loader in `owned_catalog.go` reads at most 64 KiB, rejects duplicate
JSON keys and unreviewed fields/subsets, requires two unique known-owner alias
records and binds file identity, size, modification time and SHA-256. This
version supports the reviewed self-authored text/none/disabled-shell subset,
not arbitrary hosted-model capabilities or policy overrides. Primary priority
must precede secondary; native defaults are verified, not inferred from the
file. The file is immutable for the life of this connection.

The complete initial/native refresh collector requests hidden entries as well
and validates every public Model field against the owned projection before
publishing routes/default/session state. Both backends must actually return one
matching raw model each, with their own native default true. Existing alias,
owner display suffix and aggregate-primary-default transformations are then
compared; no native field is overwritten to conceal a discrepancy. ModelInfo
instructions and non-public capabilities remain authored declarations, not
facts reconstructed or proved from model/list. The original child catalogs and
configuration are untouched.

After that barrier, existing fresh paired requirements validation still gates
config/read. The returned `config.model_catalog_json` is the loaded real path;
`magpie_bridge.provenance.model_catalog_json` is
`bridge-owned-alias-catalog`. The extension's `catalog` contains version 1,
source `bridge-owned-alias-catalog`, content `sha256` and validation
`complete-native-model-list`. Its existing bridge-owned session layer and
origins bind that config in the canonical version. No native config layer,
common home or file-read support is claimed.

A refresh revokes routes/default/config session/model pages until both complete
native results validate. Frontend requests cannot use this transient state.
Only three already-enabled, catalog-independent reads bypass this temporary
barrier: CI no-auth `getAuthStatus`, `experimentalFeature/list`, and public
`configRequirements/read`. Each retains its existing mode, initialization,
parameter/trace, request-count/byte, native comparison and response-validation
gates. Feature cursors/limits still belong to the native page producer; no
cached or synthesized feature/policy result is substituted. This does not enable
public requirements through the internal config/read policy check. Mode-off,
unknown methods, config/read, model/list and all other operations remain blocked
during refresh. Gateway, account, readiness and thread-read routes gain no
exception in this change. These native reads are not an offline or no-credential
side-effect guarantee; their existing documented limitations still apply.
Any catalog failure, file identity/hash change or mismatched result is sticky
fatal: close the bridge connection, run the existing owned-tree cleanup and
require restart, rather than retain a stale declaration or silently recover.
Already-bound thread owners are never cleared or rebound. The option does not
alter refresh behavior when absent, and does not expand auth, writes, setup,
readiness/environment, sandbox or `fs/readFile` admission.

The test-only `bridge-owned-catalog-4239` Desktop profile builds its file solely
from the existing original self-authored alpha/beta definitions. It maps only
raw slug to real alias and display name to the existing owner-labelled wire
name. Its menu-only path, no-selection/no-message scope, zero-request audit,
known-file publication booleans and independent GUI evidence are documented in
[the fixture README](../../build/control-plane-native/README.md#bridge-owned-alias-catalog-separate-menu-only-slice).
The prior 75908967 run observed Alpha only in the first popover; deeper
navigation was unverified. It did not prove full-picker filtering or a missing
filesystem contract. A positive new observation would establish this narrow
versioned contract's visibility only, not causality or successful chat.

### Optional persisted bridge-owned model defaults

`bridge_model_settings: "owned-file-v0.167.0-alpha.3.2"` is separately default-off
and requires the immutable owned alias catalog and routing control plane. It is
not enabled by menu-only or selection-only Desktop fixture profiles. The separate
explicit model-settings CI profile opts in to this mode. Its only writable
configuration is
the bridge's real `control_home/config.toml`, never either child's configuration.
The two allowed keys are `model` and `model_reasoning_effort`. Only the existing
text/`none` catalog subset is supported; `none` is already the verified native
model default. No native config override, authentication or execution-policy
setting is introduced to apply it.

The first complete catalog in this mode first checks fresh requirements from
both native children using the existing strict null-requirements validator. Only
after that policy pair and the entire native catalog succeed may an absent or
empty owned settings file be initialized with the catalog default and `none`.
Opening the store acquires an exclusive lifetime marker but does not create the
configuration file. Existing nonempty files must contain exactly the two known
string keys, and the selected alias must still belong to the validated catalog.

`config/batchWrite` accepts exactly one `replace` or `upsert` edit for each key, atomically.
Unknown, duplicate, missing, profile, permission, provider and credential edits
are rejected, as are null or non-string edit values. `reloadUserConfig` may be
omitted or a boolean; null/non-booleans are rejected. Both boolean values retain
existing thread owners: pinned c407 `config_processor.rs:168-191` skips mutation
cache clearing and thread reload for these session-default keys. Its
`config_manager_service.rs:708-778` gives `replace` and `upsert` the same scalar
insertion semantics for these two flat keys, not general object/array merging. Optional
`filePath` may be absent/null or the exact absolute owned path only. Optional
`expectedVersion` may be absent/null; if supplied, it must equal the real user
layer's semantic `sha256:` version. Writes serialize within the connection and
recheck fresh paired null requirements before committing. No native config-write
RPC is forwarded. Catalog refresh blocks writes; a refresh cannot reset a saved
selection to the catalog's priority default.

`config/read` exposes the real `user` file layer (`profile: null`) plus the
existing `sessionFlags` routing layer. Only the user layer supplies model and
effort; session flags do not override them. The write response contains the real
absolute path, semantic version, `status: "ok"` and null overridden metadata,
and is returned only after successful file publication and verification. Generic
`supports.config_writes` stays false; the separate `model_settings` extension
lists precisely these two writable keys. Catalog priority/default and persisted
new-thread selection are distinct. Existing thread/provider ownership is never
changed. A new thread without an explicit model uses the effective persisted
alias and is routed to its original native model/provider.

The store uses bounded TOML, a fixed file name, directory/file identity checks,
exclusive bridge-writer coordination and same-directory staged publication.
Unix uses exclusive link publication for the first file and rename thereafter;
Windows uses `MoveFileEx` without copy/delete fallback. Failed or uncertain
persistence/verification is fatal, not a claimed rollback or successful write.
Unexpected semantic edits or file-existence changes fail closed; stale locks
are not automatically removed. This is not isolation against a malicious writer
running as the same OS user, and does not promise power-loss durability on every
filesystem. External editors must respect the writer lock.

Public schema source: `openai/codex@c40729ea`,
`app-server-protocol/src/protocol/v2/config.rs`,
`app-server/src/config_manager_service.rs`, and `config/src/fingerprint.rs`.
Regression entry points are `TestOwnedSettings*`; tests are synthetic and do not
claim that the current Desktop sends an accepted batch shape. Actual Desktop
parameters and consumer compatibility require separate sanitized observation
before enabling this mode in any Desktop run.
The public `model/list` `isDefault` still describes the immutable catalog priority,
not the user's saved selection. Router restart tests prove retained config and
new-thread routing only; they do not establish that Desktop startup will prefer
that user layer over its own picker/default handling.


### Owned config reads during catalog refresh

In the owned-catalog mode, `config/read` keeps its existing strict parameters,
initialization gate, four-logical-read limit, duplicate-ID and active-byte limits.
The limit includes both native-policy-pending reads and reads whose policy pair
has completed while a catalog refresh is in progress. Each read obtains its own
real, validated, identical two-backend null-requirements responses. A completed
pair is retained while waiting; it is not discarded, synthesized or described as
a new policy read after catalog publication.

Only successful complete catalog publication releases held reads against the
current bridge-owned config session, including the persisted user defaults when
that mode is enabled. No invalidated or empty session is returned. A remaining
policy response arriving after publication follows the same validation before
completion. Policy failure rejects that read; catalog/file failure abandons all
tracked reads, releases payload accounting and tombstones pending siblings.
No new RPC, native configuration write, or general read bypass is introduced.

This is a bounded-memory wait, not a new wall-clock deadline. There is no new
request timer: if a native policy or catalog operation stalls, the bounded reads
can remain pending until connection cancellation or termination. Existing Run
EOF/error/cancellation handling still closes the connection and its children.


## Disposable Windows unelevated conversation experiment

The separately approved `unelevated-read-only-4239` CI test profile is default-off.
It does not add production `windowsSandbox/setupStart` routing or configure a
user installation. See [the native preflight and setup contract](../../build/control-plane-native/unelevated-setup.md)
and [the explicit test-only observer guard](../../build/desktop-rpc-observer/fixture-readonly-guard.md).

Both fresh backend homes must first prove the genuine built-in read-only profile
through the pinned official runtime, exact generated configuration layers and
unrestricted native requirements. Only then are the two official unelevated
setups attempted sequentially. Completion, persisted configuration, process
shutdown, native restart and readiness are separate required observations. A
failed or uncertain setting operation is never retried or changed to elevated.
This is an action-time-approved disposable test, not standing permission for
future security changes. The eighth CI marker is additionally restricted to a
first attempt and a reviewed base commit; the other seven markers remain required.

The later Desktop connection explicitly rejects unsafe or unknown permission
overrides before forwarding them; unlike the normal observer, that test mode is
not an unconditional byte-transparent relay. It preserves original bytes for
admitted messages and never fabricates a policy, readiness, proof or RPC success.
Registered Core or prefer-MXC selection is not silently removed to make it pass.
The source-derived no-ACL-change argument applies only to the strictly verified
read-only unelevated branch; normal configuration, logs and thread state can be
written. Separate homes and startup hashes are not an OS isolation or anti-TOCTOU
boundary, nor reproducible-build proof.

The optional GUI slice only attempts four fixed plaintext loopback turns: two in
one Alpha conversation, a genuine New chat action, then two in a Beta conversation.
It requires unique semantically named editable/send controls, actual value
readback and official UI Automation receipts; missing controls stop the test.
Evidence must agree across UI text, fixed-file defaults, guarded native
thread/turn ownership and the two independent local Responses services. Menu
visibility or UI text alone is not conversation acceptance. A completion under
this profile still does not prove hosted GPT/AIHub, real authentication, tools,
elevated sandbox, cross-Desktop-restart persistence or ordinary Start/taskbar
launch. Desktop restart is deliberately not attempted without a complete
process-family shutdown proof.

No successful Windows setup or conversation is claimed by adding this profile.
The ordinary settings-only profile retains its zero-model-request requirement
and original 180-second fixture lifetime; only the explicit chat profile uses
360 seconds and nonzero synthetic requests.


### Offline repair and the first completed native setup

A push marked `[bridge-offline-only-v1]` plus the existing
`[winapp-inspection-v0.7.1]` legacy-lane exclusion runs only offline observer /
bridge regressions and the separate Windows build. All other policy-trace jobs
are explicitly skipped. Live profile markers cannot be combined with this
mode. It does not renew permission to configure another disposable VM.

In commit `eda5edd5867cd4bc2c368e83d5f7263da17513cc`,
[run 42458635929](https://github.com/breakingtime126/magpie/actions/runs/42458635929)
recorded both genuine native setups completed, both post-restart read-only /
readiness checks passed, and four preparation peers exited cleanly. The real
Desktop screenshot no longer showed the Windows setup prompt. This is native
backend restart evidence, not Desktop restart acceptance. The Desktop chat
slice stopped at its first Alpha UI read without input, send, or local model
requests. A separate observer event-order bug prevented final trace validation;
the Desktop job failed. The independent legacy helper smoke also failed.
[Windows run 42458635987](https://github.com/breakingtime126/magpie/actions/runs/42458635987)
passed its race, Windows test and packaging jobs. Those results do not establish
actual chat or production compatibility.

The observer now emits its real public catalog provenance before its guard
activation record. Guard construction still precedes child startup and wire
forwarding; the parent ordering validator is unchanged. UI helper fixes must be
verified independently: a screenshot alone cannot identify the first parser
exception or prove that a semantically named send target is available.

The chat helper now records hidden/duplicate evidence for all fixed targets but
applies it only when that target is requested. A hidden future composer cannot
block closing an already-open model menu. The actual composer, send, new-chat
or model target must still be unique, visible and enabled. Promoted UI Automation
IDs use only the existing official read-only property lookup with a fixed name /
type, and must resolve back to the same identity and menu expansion state.
Anonymous buttons, changed identities and unavailable state remain rejected.
These are independently reproducible helper defects, not proof of the first
exception in the earlier real run, whose raw UI tree was not retained.

## Thread-history discovery: pinned 0.167 filter semantics

This section records the existing behavior, not a migration or new history /
section store. The public reference is Codex commit
`c40729ea5cf8e947481b130c59807273eda2f8e9`, matching the reviewed version
declaration `0.167.0-alpha.3.2`, not a reproducible-build proof.

For an ordinary native `thread/list` without a relation filter:

| `modelProviders` | Native meaning | Bridge control-plane behavior |
| --- | --- | --- |
| Omitted or `null` | Filter by that app-server's current configured provider. | Preserve omission/null separately on each backend and every collected page. |
| `[]` | Do not filter by provider inside that backend's history store. | Preserve `[]`; do not replace it with the configured provider. |
| Nonempty list | Return records matching the supplied provider names. | Accept only `["magpie_bridge"]`, translated on every native page to that backend's configured native provider. Raw, unknown, duplicate or multi-provider frontend filters are rejected. |

The native implementation also makes omitted/null provider filters unrestricted
when a relation filter is present. The current bridge rejects non-null relation
filters, so it must not advertise that native exception as supported. Null
lineage fields remain supported without creating a relation filter.

An empty provider filter is **not** a request for every thread: source-kind,
archived, cwd, exclusion and other supported filters still apply. In particular,
source kinds default to interactive sources and archived defaults to false.
Native `useStateDbOnly:false` can scan and repair stored metadata; a list request
is not a guarantee of zero filesystem writes. These observations come from the
[official protocol](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs#L1429-L1501),
[filter construction](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/thread_processor.rs#L5990-L6064),
and the official
[provider-filter test](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/tests/suite/v2/thread_list.rs#L676-L720).
Those native tests were inspected, not executed for this check.

The bridge collects both backend lists before publishing its bounded merged
pages. It learns each returned ID's backend ownership, projects the validated
native provider to the logical bridge provider, and qualifies model names with
the backend alias. Subsequent read, resume and turn requests route only to that
owner; choosing another backend's model does not migrate the thread. A new
bridge connection has to discover ownership again. It does not create, copy,
repair or delete native rollout files itself, or infer ownership from a model
name supplied for an unknown thread.

If `[]` causes a backend to return a record from a provider other than that
backend's configured provider, `bindThread` rejects the result and the running
bridge connection fails closed. Cross-backend thread-ID collisions also fail
closed rather than merging histories. This is a current compatibility limit:
all-provider native discovery does not grant permission or establish an
execution route for historical foreign-provider threads. Neither a filtered
view nor this rejection establishes that underlying data was deleted.

`thread_history_discovery_167_test.go` adds only in-memory synthetic router
regressions. They cover all four filter spellings, both backends and native
continuations, merged frontend pagination, owner-based read/resume/turn request
routing and synthetic resume-response projection, cross-provider override
rejection, rediscovery in a fresh router, foreign-provider results and ID
collisions. Mixed configured/legacy-provider rows in one native response are
checked for each filter, backend and row order. With `[]`, a legacy row can be
a legitimate native result; with the other filters it violates the pinned
native contract. Both cases fail closed without publishing a partial list;
the test does not keep using the failed router or assert ownership-map rollback. The fixtures do not execute Codex,
read a native store, complete a resumed turn, or test Desktop restart, grouping,
real account history retention or migration. No production routing or filter
behavior is changed. Use the existing explicit offline-only CI markers so this
check does not start Desktop, run the official runtime or configure a sandbox.

### Post-action model-popover observation

The `15b01977` disposable Desktop run completed both authorized unelevated
read-only setups and native restart checks, but sent zero messages. Its first
chat step received a successful `collapse` receipt, then observed the model
popover still open; the final screenshot corroborates that state. The receipt
is not proof of rendered closure, and this does not establish an asynchronous
rendering bug or a model/backend failure.

The chat helper now issues the close action only once, then performs up to eight
validated read-only observations. It proceeds only when the popover title is
absent and the same provider/model button is uniquely available and explicitly
collapsed. Every observation retains the identity, full-snapshot and protected-
screen checks. The two-second polling budget is checked between bounded tool
calls, not a strict two-second wall-clock guarantee; the overall chat budget is
also checked before and after those reads. No repeat close, blind key press,
input, send or next provider follows a persistent popover, changed identity,
invalid receipt or failed observation. Fixed failure reasons distinguish these
stops without publishing raw UI or error data.

Pure fixtures cover immediate and delayed closure, persistent visibility,
missing title with expanded state, protected/invalid observations, changed
identity and uncertain action receipts. They do not prove that waiting fixes the
real Desktop case. Any further native setup run requires a newly approved
single-VM experiment; offline-only CI must not consume that approval or rerun
setup automatically.

### Exact 0.167 primary trusted stdio auth status

`auth_status: "primary-trusted-stdio-v0.167.0-alpha.3.2"` is a new, independent,
**default-off credential relay capability**, not a relaxation disguised as the
old no-auth diagnostic. It requires `backends[0].provider` exactly `openai` and
both configured executable SHA-256 pins; existing legacy/current version
exclusion, digest checks, JSON bounds and both-child readiness remain. The
operator must verify the actual official executable version/provenance and
control the entire private stdio frontend and review primary account/endpoint
configuration. A provider label is not network destination authentication.
A mode name/digest does not itself
prove version, and `clientInfo.name` is not authentication. Do not enable this
mode on an untrusted/shared pipe or simply because a client calls itself Codex
Desktop. Examples, defaults and the older `primary-no-auth` mode are unchanged.

This is an explicit expansion: the primary's real API key or managed ChatGPT
bearer can reach that trusted frontend. Only primary receives `getAuthStatus`;
the validated response goes only to frontend stdout, never to secondary. No
response value is logged or cached by this path; native stderr remains discarded.
Tokens do pass through Go/JSON temporary buffers; there is no secure zeroing
guarantee and buffers may remain in memory until garbage collection.
Source result bytes are retained subject to normal JSON compaction, with the
original frontend ID. Wrong-backend replies fail correlation. No fallback,
retry, token substitution or synthetic successful identity is performed.

The exact request is the existing strict envelope and params object, with only
optional nullable booleans `includeToken` and `refreshToken`. Unlike the no-auth
diagnostic, explicit true refresh is allowed here. Absence/null/false/true are
preserved, not rewritten; trace metadata is not forwarded. A non-null token is
accepted only when the client explicitly requested `includeToken:true`, the
native result has `requiresOpenaiAuth:true`, `authMethod` is `apikey` or
`chatgpt`, and the token is a nonempty string. Without a token request, any
non-null token rejects the whole result rather than silently stripping it.

Results must contain exactly `authMethod`, `authToken`, `requiresOpenaiAuth`.
For the pinned handler, requires-auth is a boolean: false requires null method
and token; true allows null method/token or any known method with a null token.
Unknown methods, fields, missing fields, duplicate keys and malformed values
reject the whole result. The known metadata enums are `apikey`, `chatgpt`,
`chatgptAuthTokens`, `headers`, `agentIdentity`, `personalAccessToken`,
`bedrockApiKey`, `bedrockAccessKeys`. Recognition of metadata does not authorize
those other authentication workflows. Non-null tokens for every method except
`apikey`/`chatgpt` are deliberately rejected. This conservative subset is not
full native authentication compatibility, particularly not the internal
`chatgptAuthTokens` workflow.

The public [v1 schema](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v1.rs)
allows nullable requires-auth, but the fixed
[handler](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/account_processor.rs#L1097-L1168)
always emits a boolean. This mode follows that narrower behavior, not the
entire schema domain. The complete
[API enum](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/common.rs)
is a metadata oracle, not authorization to add internal login flows.

Ordinary well-formed native errors preserve only their signed int64 code and
return fixed `primary trusted stdio auth status unavailable`, omitting source
message/data. Invalid results/errors use -32000 with that text. Malformed or
uncorrelated routing envelopes may still close the connection. Nothing in
failure handling fabricates null/null identity success.

**Side effects and limits:** native explicit refresh can update authentication
state, contact services and write storage. Even `refreshToken:false` or omitted
can invoke `auth().await` and implicit refresh; `includeToken:false` is not a
no-network/no-refresh promise. Account/read and account/updated retain their
existing primary-only paths, not a new account model. This mode alone does not
enable attestation, proof generation, gateway OAuth, account login/logout,
external-token refresh or another mode's capabilities. Separately enabled modes
retain their own contracts. Homes/environment separation is not an OS secret
boundary: same-user processes and default read-only filesystem access may read
sibling credentials. No real account, model-tool sandbox isolation, corporate
network approval or Desktop compatibility is proven by enabling this option.

`auth_status_trusted_167_test.go` uses only public fake strings and existing
synthetic native-process fixtures. It checks explicit configuration/pins,
nullable flags, primary-only relay, conservative token export, result/error
validation, account-path preservation, unsupported auth capabilities, and
clean EOF of both children. These are not official runtime or real-login
experiments. Local Go/formatting/runtime checks were not available during
candidate preparation; static patch checks are not a test pass.

### Standalone save-and-check configuration helper

The [independent Windows helper](../../scripts/bridge-profile-preview/README.md)
now adds explicit new-file persistence and a bounded call to the fixed packaged
adapter's `--check`. It never invokes normal Magpie startup or agent wiring.
The ZIP includes `Open-Bridge-Profile.cmd`, the GUI script and its instructions;
the launcher preserves normal PowerShell/enterprise execution policy.
Only a new bridge-owned profile directory is written, outside all four backend
homes, with no overwrite. Saved JSON contains only the original five backend
fields. Checker identity is tied to the package manifest hash, not a user-selected
executable; this does not establish package trust. Check failure/timeout retains
an unchecked draft, and field changes invalidate the displayed result.

This is a configuration aid, not a ready-to-chat flow. It still requires two
program selections, provider IDs and four prepared directories. It does not
configure providers, credentials, compatibility opt-ins or normal Desktop
startup, and `--check` success establishes only the existing schema/path checks.
Pure temporary-file fixtures do not establish actual Windows GUI acceptance.
