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
   result is returned only after both succeed. Client `initialized` then goes
   to both. Unsupported attestation and explicit gateway OAuth capability
   opt-ins are rejected before initialization.
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
external token refresh and unscoped backend requests are unsupported.

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

`proc.StartTree` owns a Unix session or Windows job. Shutdown closes stdin,
gives an EOF cleanup grace period, then stops/reaps the owned tree. A process
that deliberately starts another Unix session can escape the group, and the
existing Windows helper can fall back when job assignment fails. These are
not absolute containment guarantees. There is no process-name-wide kill.

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
