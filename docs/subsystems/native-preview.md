# Independent native provider preview

This is not the same-window Codex bridge. It does not import or directly edit
existing Codex home/configuration/history and never enters ordinary Magpie
migration paths. Native login still follows its effective managed credential
store and may clear/revoke authentication there; a fresh home is not a promise
that all pre-existing authentication is unaffected.
It uses the selected unmodified executable, with program-owned fresh contexts.
Selection hashes a regular local file; it is not signature/provenance validation.
Before native operations the digest and the reported version are checked. The
initial compatibility set is 0.166.0-alpha.6 and 0.167.0-alpha.3.2; a reported
version is not cryptographic identification and path replacement races remain.

## Explicit operations

- Automatic discovery: a read-only current-user Windows Get-AppxPackage query
  admits only a unique registered OpenAI.Codex_2p2nqsd0c76g0 package reporting
  Store signature kind and Ok status, with the previously observed relative
  app/resources/codex.exe path. It does not independently verify a signature,
  launch Codex, search all disks, elevate, bypass execution policy, or modify
  package registration. Missing/ambiguous/blocked results retain manual selection;
  they do not prove the application is absent. Non-Windows auto lookup is not
  implemented. Lookup has a 10-second context with the same cleanup caveat below.
- Select executable: no child process. The user must choose a trusted official
  executable, not assume the product has authenticated an arbitrary file.
- Configure AIHub: HTTPS endpoint and key stay in memory. The key is supplied
  only in the AIHub app-server child environment, never primary or version
  probing, argv, GUI state, application logs, or an application settings file.
  Native behavior, OS dumps and same-user processes are not a secrecy boundary.
- Discover: version probe, then a fresh stdio app-server; initialize, initialized,
  config/read (effective provider match), paginated model/list, EOF. It returns
  only native model identifier and display name. No full ModelInfo is exported
  or constructed. Discovery may refresh authentication, contact services and
  write native caches; it does not prove a key, subscription or inference works.
  An AIHub managed override of endpoint/auth provider fields causes rejection,
  not a rewrite of managed policy. No thread or tool request is sent.
- Login: each explicit attempt selects a new random dedicated primary HOME and
  CODEX_HOME. The original native `login` command owns opening the browser and
  callback; stdout/stderr are discarded, never parsed for OAuth codes/tokens.
  The native default/managed credential store is retained, not forced to file.
  Native login can clear/revoke authentication and write credentials according
  to that store. A fresh path is not proof of no keyring entry or OS isolation.
  A zero exit status is only a native command result, not account verification.
  Subsequent discovery uses this attempt's context even after failure/cancel.
- Send is a guarded, explicitly selected single short-text attempt on the reviewed
  0.167.0-alpha.3.2 protocol only. The 0.166 discovery/login path does not enable it.
  Discovery is not authentication or inference verification. See the boundary below.
  A prompt asking not to use tools is not a security control.

No operation runs automatically, retries, logs out, imports an old context, or
removes native authentication after failure. Closing clears the application's
key reference and cancels/waits for its active operation, but retains native
state; Go memory is not securely wiped. Reopening the preview creates another
fresh context and does not resume these retained credentials. This limited
preview is not a persistent account manager.

## Process and transport boundaries

Before Windows state creation, the selected temporary parent must be an
absolute local drive-letter path on a fixed drive, with directory attributes
and no reparse point on any ancestor. UNC/mapped remote/reparse locations fail
without writing state, changing ACLs or resolving string identity by case/8.3
comparison. Login and discovery recheck their owned state paths before native
use. This does not eliminate replacement races after the check or make the
same-user directory a security boundary. Unix fixture admission checks an
existing absolute directory, not a portable fixed-drive guarantee.


Discovery and version probing reuse strict proc tree ownership. Native login
must not use kill-on-close trees: a new browser could become a descendant.
Login cancels only its exact owned root process and waits for it; browser windows
remain user-owned. An inherited Windows job disables login rather than changing
job policy, requesting breakaway, or risking a browser being closed with a job.
The non-Windows login path is not desktop-browser acceptance evidence.

CODEX_WINDOWS_REGISTERED_CORE presence blocks operations; it is not silently
removed. Explicit proxy environment settings (upper/lower HTTP(S)/ALL/NO_PROXY)
and CA settings CODEX_CA_CERTIFICATE, SSL_CERT_FILE/DIR, CURL_CA_BUNDLE,
REQUESTS_CA_BUNDLE and NODE_EXTRA_CA_CERTS are retained unchanged. No direct-route
fallback or system network change is attempted. Other provider credentials and
caller-owned account/home variables are not inherited. This curated set is not
a guarantee of every enterprise transport mechanism; failures remain failures.

Version probing uses ten seconds, discovery a 30-second operation context and login five minutes. These
are not hard full-lifecycle time bounds: filesystem calls and parent Wait/cleanup
can block beyond them. Existing proc semantics do not surface every sweep/kill
failure; Windows job-close and Unix asynchronous process-group cleanup are not
proof that every descendant exited. Root-only login cancellation intentionally
does not kill browser descendants. No security setting or sandbox setup is run.

## Verification scope

Only inert copied test executables and public canaries are used in automated
coverage: separate homes, key noninheritance, fixed request sequence, native
metadata projection, malformed/unsolicited response rejection, cancellation,
transport preservation, admission and fresh login contexts. Fake login does not
open a browser or authenticate. No real credential/service/user device was used
while implementing this candidate. Local Go/gofmt/runtime checks were unavailable;
source/apply verification is not executed acceptance. Real installed native,
browser handoff, managed storage, endpoint discovery and graphical acceptance
remain separate checks, not consequences of CI compilation.

Public source anchors for the narrow contract:
- c407 `app-server/src/config_manager_service.rs:132-203` serializes effective
  ConfigToml including exact requirements into config/read; native provider
  defaults are defined in `model-provider-info/src/lib.rs`.
- c407 `login/src/server.rs:208-210` opens the native browser; storage naming is
  in `login/src/auth/storage.rs:307-321`.
- c407 `http-client/src/custom_ca.rs` defines CODEX_CA_CERTIFICATE and
  SSL_CERT_FILE precedence. These values are preserved, not rewritten.
- Existing authorized package harness `build/test-desktop-cold-start.ps1`
  observes registered OpenAI.Codex and app/resources/codex.exe. Auto lookup is
  limited to that known path/identity and does not infer new package layouts.


## Guarded single-text boundary (next candidate)

Each explicit Send requires a provider-owned discovered model and re-discovers it
in the actual native process that will execute the request. The controller checks
that process's effective config for the reviewed disabled tool/extension features,
web search, agents, cloud skills and orchestrator MCP; MCP entries must be absent
from the reported map or individually disabled, with no configured hooks/notify.
Missing or conflicting required effective values fail closed, not silently replace
managed requirements. The config/read check is **not** isolation before startup:
native initialization, auth/cache handling or configured integrations may already
have effects. This does not promise zero network or zero local native-state writes.

A new ephemeral thread explicitly selects empty environments, dynamic tools and
capability roots. Its native receipt must match the requested provider/model, own
working directory, empty environment selection and instruction sources, with no
parent/fork. No resume, sandbox mode, permission or approval-policy change is sent.
The turn again selects empty environments. Public c407 execution handlers require
a selected environment before filesystem/command access; the separately checked
feature gates close non-environment extensions. A nonempty tool registry is not
itself execution. Internal clock/plan/user-question events are not arbitrary local
or third-party actions; this minimal reader may reject unsupported events instead
of completing them. No server-originated request is answered or approved.

Only a matching completed turn with no error and completed agent-message text
can produce Reply. Native errors, wrong IDs, unknown/tool item kinds, excess data
or ambiguous completion return the fixed failure, never a partial success. This
notification rejection is additional protocol validation, **not** the tool-action
safety gate. Every Send creates a new thread; this preview does not provide
conversation history, resume or multi-turn state. Input is at most 4 KiB and final
text at most 64 KiB. The controller never retries a failed operation, falls back or switches provider.
Native transport/model-loop behavior, including its own configured retries, is
not replaced by this controller; a single explicit test is not a one-HTTP-request
or fixed-cost guarantee.

Cancel closes this owned native operation using the existing process-tree helper;
it does not retract an already sent request, undo native effects or guarantee no
charge. The 90-second exchange context is not a hard total lifecycle deadline:
cleanup has a further grace period and existing Stop/Wait limitations. Login still
uses its separate root-only browser-preserving lifecycle. No real account, model
endpoint or official runtime was invoked while writing this candidate. Synthetic
coverage is not acceptance of real installed native/managed-policy compatibility.

Fixed public source: c40729ea5cf8e947481b130c59807273eda2f8e9,
`app-server-protocol/src/protocol/v2/thread.rs` (empty environments, thread receipt),
`core/src/session/session.rs:1507-1522` and
`core/src/environment_selection.rs:1097-1119` (empty snapshot),
`core/src/tools/handlers/mod.rs:200-223` (no executor fallback),
`core/src/tools/spec_plan.rs` and `features/src/lib.rs` (tool feature gates).
Only these reviewed settings and narrow message projections are used; no upstream
model catalog records are copied or generated.


Code-mode is not assumed to share the empty-environment execution gate. Its
startup `features.code_mode_host` must be the literal boolean false (not an
object containing enabled=false), and code_mode, code_mode_only and prewarm are
also requested and verified false. On c407 this boolean host form resolves
`disable_in_process_fallback=false`; ThreadManager therefore installs the disabled
session provider before serving threads. That provider rejects create_session,
so even a model-metadata-selected CodeModeOnly task cannot execute a runtime cell.
Metadata can override requested tool mode, so such a model may fail this preview;
we do not rewrite model metadata to make it work. References:
`core/src/config/mod.rs:2840-2869`, `core/src/thread_manager.rs:681-690`,
`code-mode/src/remote_session.rs:103-116`, and
`core/src/tools/code_mode/mod.rs:150-152,241-257` at the same fixed c407 commit.
Browser/computer configuration tables are not on/off controls. In this fixed
standalone app-server there is no independent built-in browser/computer handler;
MCP, dynamic and injected host-tool sources remain excluded by this contract.
