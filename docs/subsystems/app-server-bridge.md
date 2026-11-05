# Experimental Codex app-server bridge

This opt-in command connects one JSONL client to **two unmodified, independently
configured official Codex app-server processes**. It is not Magpie's HTTP
Codex relay, provider gateway, or a replacement Codex implementation. There is
no HTTP client or listener in this package. Each official child performs its
own model requests using its own configuration and authentication.

**Status:** cloud implementation and offline fixtures only. The official
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
This does not certify the user's older installed Desktop/backend version.

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

Unknown request fields, config/history/path overrides, collaboration modes,
provider fallbacks and delegated parent/root turn lineage fail locally with
`-32602`. Section-position thread sorting is unsupported. `created_at`,
`updated_at` and `recency_at` are merged in the requested direction. Parent
subagent IDs may be learned from verified same-backend relationships, but
cross-provider subagent dispatch is not implemented or required.

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

Future real acceptance remains: authorized isolated Desktop instance, same
menu containing GPT and deepseek-flash-ft, both doing an unpredictable local
file/tool task and a follow-up, stop/error behavior, process/route evidence,
company policy compliance and reversible activation. A code build, HTTP 200,
model self-identification or catalog visibility cannot replace those checks.
