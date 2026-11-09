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
- Send is deliberately unavailable pending a proven native tool-action boundary.
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
