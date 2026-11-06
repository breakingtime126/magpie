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

1. `initialize` is sent independently to both children. The primary child's
   genuine result is returned only after both succeed. At that point the
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
`getAuthStatus` remains unsupported (`-32601`) after readiness.

Supported requests are enumerated in `Router.Client` and `validateParams`:
initialization, model listing, ordinary thread start/resume/fork/list/read,
loaded-thread listing, paginated turns/items, archive/unarchive/name/compact,
revert/legacy rollback/unsubscribe, turn start/interrupt/steer, and the primary
backend's account/rate-limit reads. `thread/rollback` is the older protocol
spelling, forwarded unchanged; `thread/revert` is the newer spelling. The child
version determines availability. Unsupported methods return `-32601`.

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
`-32602`. Section-position thread sorting is unsupported. `created_at`,
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

`getAuthStatus` remains unsupported. Earlier static inspection found the local
main connection defaulting to `ensureAuth:false`; that did not establish the
actual installed Desktop startup sequence. The signed Desktop 26.1103.3517.0 /
backend 0.167.0-alpha.3.2 trace in
[run 42217188118](https://github.com/breakingtime126/magpie/actions/runs/42217188118/job/127891199826)
observed `getAuthStatus`, `account/read` and `configRequirements/read` after
successful initialize without a client `initialized` notification. Correcting
that readiness gate does not authorize or implement `getAuthStatus`.
Even `includeToken:false,refreshToken:false`
can reach credential-refresh behavior, so the bridge does not fabricate status
or call it a guaranteed offline read. `config/read`, gateway/account login
mutations and external-token refresh remain unsupported. The inspected
`config/read` wrapper waits for readiness but is not a fixed serial step in the
main initialization function. Returning only one backend's full configuration
would misrepresent the other backend's provider and permission context.

The new read-mode tests use synthetic peers with an ordinary fixture initialize;
they do not establish real Desktop compatibility. The default-off auth modes
are limited to unavailable-proof errors and primary readiness inspection.
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
credential reuse or refresh. `getAuthStatus`, login, cancel and external-token
refresh remain unsupported; they are not offline substitutes.

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
`getAuthStatus`, `config/read`, login/cancel and external-token refresh remain
unsupported. This mode never produces a proof or grants login authority.

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
and production `getAuthStatus` rejection (`-32601`) are unchanged.

The test-only observer shares the pure
[`NativeTrace167Valid`](../../internal/appserverbridge/native_trace_167.go)
predicate. For an unambiguous, correlated client `getAuthStatus` or
`configRequirements/read` request containing trace, it records only
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

The observer also names 14 exact public startup request methods from the pinned
[ClientRequest.json](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/schema/json/ClientRequest.json):
`modelProvider/capabilities/read`, `experimentalFeature/list`,
`permissionProfile/list`, `windowsSandbox/readiness`,
`mcpServerStatus/list`, `skills/list`, `hooks/list`, `plugin/list`,
`plugin/installed`, `app/list`, `app/installed`,
`externalAgentConfig/detect`, `account/workspaceMessages/read`, and
`account/usage/read`. This is an exact diagnostic allowlist only, with no
payload fields or RPC execution; all remain unsupported by production routing.
It neither identifies the previously unknown observed RPC nor claims that
Desktop actually sent any of these methods. The separate
[`native_methods_red_test.go`](../../build/desktop-rpc-observer/native_methods_red_test.go)
uses old-base APIs for an assertion-level label regression.
