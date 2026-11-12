# Original Magpie GUI: native Codex experimental build

This build uses Magpie's original main, tray, window, navigation, Agents page,
provider pages and controls. It is a partial integration, not a working
Codex Desktop same-window release.

The original Agents → Codex Connect/Disconnect path has a native-only launch
selection transaction. The desktop host owns a serialized lifecycle manager, constructed after the
local gateway starts and closed during application shutdown. Its production
supplier factory wires runtime preparation and the owned native launch host.
The supplier source reads the existing direct AIHub provider and its selected
or saved models. Missing configuration reports the existing Providers-page
setup step before creating transaction files or changing launch selection. The former **Native
backend test** action and separate account/test sheet are no longer offered in
the product UI. There is no replacement backend-ID or JSON setup form.

## Inspect the portable build

Extract the entire archive into a new, private folder, then open
Magpie-Native-Codex-Experimental.exe. Keep .portable and data beside it.
These are Magpie's existing portable-mode files, not a new storage mechanism.
The original provider/settings interface uses this new data folder; it does not
import an existing Magpie library. Account discovery is off in the supplied
settings. Other original pages and explicit actions retain their normal meaning.

The archive also contains magpie-codex-bridge.exe, an internal stdio adapter
built from the same source commit. It is not another GUI or a bundled official
Codex executable. BUILD.json and SHA256SUMS identify the packaged source and
files. CONNECT-STATUS.md describes preparation, recovery and remaining acceptance
limits.

Do not treat the available Connect control as a completed setup flow. Runtime
discovery, provenance checks and ownership of Magpie's local gateway are now
connected. The source produces an authored minimal native catalog from current ordinary
Magpie configuration; unknown optional fields use documented client defaults.
This can prepare future launch selection, but does not prove real chat or
Desktop acceptance. The reviewed shared-native adapter path is pinned to
0.167.0-alpha.3.2; this package does not install or update the official runtime.

The EXE is unsigned and requires an existing approved WebView2 Runtime. It
does not install WebView2 or change Windows sandbox/security settings. Do not
bypass company update, executable, proxy or certificate restrictions.

## What is protected, and what is not

The nativecodex build tag applies guards before normal startup. They stop
legacy Codex gateway wiring, automatic Codex configuration/rollout migrations,
and Codex-specific Library targets, including the WSL/profile family. Only the
owning desktop window's local Codex Connect/Disconnect path uses the new
selection transaction. Legacy ConnectHow and Sync remain guarded; ordinary
builds keep their old behavior.

The transaction has explicit journal-based recovery for changes to the two
launch-selection variables. SelectionConfigured means only that their values
were read back, not that a backend started or a chat succeeded. Disconnect
restores journal-matching previous values, refuses external changes, and does
not rewrite native authentication or history. Already running Codex windows
are not changed. See CONNECT-STATUS.md in the package, or
docs/subsystems/native-codex-connect.md in the repository, for the full
failure and recovery rules.

The experimental GUI does not install itself over an existing Magpie, refresh
shortcuts/autostart/link registration, take over another Magpie's gateway, or
self-update into an upstream release. These actions are unavailable here.
Other providers, account-discovery choices, explicit history edits, other
agents, shared Library inputs and normal gateway activity retain their
original side effects. The whole GUI is not read-only or offline.

The built-in ChatGPT account and legacy Codex gateway paths remain blocked in
this build. The intended route is official Codex GPT directly and AIHub through
Magpie's normal provider route, with existing gateway and native policy checks
intact. Separate folders are not a credential security boundary. This design
does not grant permission to use an account or transmit data to a supplier.

## Host lifetime and selection ownership

The original endpoint calls `nativeconnect.Manager`, not a separate preview.
The manager serializes Connect/Disconnect and keeps prepared launch resources
alive independently of the HTTP request. Its factory is in-process only and
must validate current configured route identity, runtime provenance and the exact owned gateway.
The production factory now composes the owned native endpoint, bootstrap and
profile packages. Its configured-source gate runs before side effects. Synthetic
adapter tests cover source extraction, generated catalog and launch selection;
production can prepare selection when its configured source, runtime and owned
gateway checks pass. That is not real Desktop or chat acceptance.

`Controller.Inspect` reads journal/selection evidence without creating a lock or
folder. An existing journal cannot be adopted as a live generation after a host
restart. Repeated Connect can reuse only the same still-valid unconsumed
in-memory generation and an unchanged regular installed profile whose bytes
match the journal SHA-256; it does not overwrite the original journal or prepare
another launch. Missing, tampered or linked profiles reject repeated Connect
without blocking Disconnect recovery. Unknown inspection or a partial selection write retains
resource ownership for explicit recovery. Missing supplier preparation remains
zero-write.

Disconnect restores selection but retains launch resources because a launch may
already be consuming them. Shutdown cancels preparation before waiting for it,
then explicitly closes owned resources with a bounded deadline. Cleanup errors
are reported and ownership retained for retry; a timeout is not successful
drain. Shutdown does not automatically restore registry selection. The manager
is retained until process exit, which cannot guarantee another cleanup attempt.
Configured supplier metadata has a semantic revision and launch-time admission
checks. A local watcher cancels its host lifetime after a route/model change,
without another Connect. It does not poll suppliers or demand optional metadata. Catalog bytes are pinned in the
prepared profile and checked again before use. These contracts are covered by
synthetic generations, not authenticated native processes. Handshake and chat
status remain false.

## Evidence and remaining gap

Synthetic tests additionally exercise original endpoint dispatch, bounded host
cleanup, concurrent/repeated Connect, cancellation, partial-start ownership,
uncertain selection retention and cleanup retry. They cover missing-input
zero-write behavior, launch-selection
journaling and restoration, interrupted-write recovery, external-change
conflicts, original-page behavior and native-mode protections. Windows
packaging builds the original GUI and adapter, then captures the GUI in a
temporary synthetic home. Capturing a window is not acceptance of its controls.

An earlier internal short-text harness exercised the pinned official native
program against a local Responses fixture. That is not the removed product
entry and does not establish real-account or same-window usability.

The current source no longer requires complete supplier-native metadata. It
supports direct ordinary AIHub configuration and uses documented client defaults
for optional unknown fields. No real provider request or credential test is part
of that mapping. Official Desktop handshake, real GPT/AIHub chat, tool execution
and restored-start acceptance remain unverified. Existing-thread provider
switching is unsupported and explicitly rejected; choose another provider in a
new thread. Do not present a successful build or selection transaction as a
completed same-window release.

The original Connect factory now opts into bridge-owned native picker defaults.
It persists only the model/effort pair in its isolated generation control home,
using genuine complete native model lists and matched null native requirements.
This adds no original GUI controls and does not establish same-window acceptance.
See [Native picker defaults](native-codex-connect.md#native-picker-defaults) for
restart scope, refusal behavior and the unchanged fixed-per-thread provider rule.

## Disposable original-UI acceptance harness

The default Windows packaging job still captures the original GUI without
operating controls. An exclusive `[original-connect-desktop-v1]` experiment
marker now admits `build/original-desktop-acceptance.ps1`: pinned official package
preparation, the actual original Configure Toggle, one normal AUMID activation,
and the original restore dialog/confirmation in finally. It uses only a fresh
portable copy, empty native home and public inert provider fixture. No live
account, inference, Desktop setup or permission adjustment is part of this run.

The [environment fixture](../../build/original-desktop-environment.ps1) separates
the fresh hosted Windows session identity from the isolated native state root.
Magpie preserves that session's verified `USERPROFILE` and, only if present and
matching, `HOME`. Normal AUMID Desktop activation does not inherit Magpie's
private `ProcessStartInfo`; giving only Magpie a synthetic identity makes the
bridge's unchanged shared-home check reject before either startup-probe child
when Desktop retains the session identity. This source-level mismatch does not
by itself prove the exact cause of a historical runtime failure. `CODEX_HOME`
remains the explicitly fresh empty
owned directory, and Magpie's portable data, APPDATA and temporary directories
remain isolated. No process/user environment is rewritten and no Desktop launch
override is injected. The native bridge still validates its own inherited
environment, including all existing home, policy, transport and certificate
checks.

This fixture is gated to the explicitly opted-in disposable GitHub-hosted
Windows job, never a user's computer or profile. Session identity paths are
checked as metadata only; preexisting ambient `.codex`, unexpected native state
overrides or nonempty isolated state block the run. Account discovery remains
disabled. Fixed report booleans describe fixture construction, not account,
Desktop or backend acceptance. The precondition observer receives launch
identity and owned native/data roots separately and still runs after saved
recovery. Authored construction and probe tests cover the previous mismatched
identity rejection and a matching identity with a separate native state root;
actual Windows results remain a separate acceptance check.

The action supervisor holds the original process identity and gives a bounded
Core worker one attempt per action. The initial Configure wait is read-only,
bounded to 15 seconds, and waits only on a missing, hidden or disabled exact
control. Unknown reads, duplicates or changed control identity stop. Exact registry/journal readback establishes
selection and restoration separately from UI appearance. Uncertain actions are
not retried; clean snapshots alone cannot settle an asynchronous Connect.
Unknown recovery retains the journal and running owner. Acceptance screenshots
follow restoration. Separate bounded, diagnostic-only original-window capture
and tray reads run only after the recovery checks return and the report is saved;
their results cannot authorize cleanup or change uncertainty. The first complete
allowlisted error snapshot is retained even if the UI clears it. Capture reports
validated fixed phases without extending its deadline. A separate read-only
observer reuses retained package metadata and the original environment to report
bounded path preconditions; it performs no package query or native execution and
does not prove the original version probe or host preparation. Tray discovery is
not a Quit action, and window close only hides this tray app.

The [cleanup reporter](../../build/original-desktop-cleanup.ps1) separates each
retained root's exit from graceful shutdown, descendant drain and resource
release. It records already-exited, exit-without-force and forced-exit outcomes,
and retains partial success if the other root cannot be confirmed. A failed
identity check never authorizes termination. Unknown recovery still permits no
root action; an attempted but unbound Desktop activation remains unknown, not
absent. Magpie's main window is no longer closed as a substitute for Quit.
After a bounded wait, the existing forced-cleanup fallback targets only the
same checked retained root. Official Desktop retains its prior window-close
request before that fallback. Neither operation establishes graceful Quit.

`ownedRootExitsConfirmed` can therefore be true while cleanup is blocked.
`cleanupResourcesDisposed` reports query-handle/lock release separately, and
one disposal failure does not skip the other or erase an observed root exit.
`cleanup.originalQuit` distinguishes no start, an already-exited process,
unknown recovery, unavailable identity and the lack of a verified original
Quit control. Both `gracefulShutdownVerified` and `descendantExitVerified`
remain false. The coordinator preserves an earlier operation's `failureStage`
alongside cleanup evidence, and never promotes these root facts to full
acceptance. The strict/drained Job helpers in `internal/proc` own only trees
enrolled before resume; they cannot retroactively prove AUMID descendants.

The official helper acquires the exact reviewed 26.1103.4239.0 release from
the versioned official URL, not the rolling download alias. Its source and
unchanged package hash match the [Microsoft winget manifest](https://github.com/microsoft/winget-pkgs/blob/0bdf2be4abb5704747b89410a59fa40ecd9fe35a/manifests/o/OpenAI/ChatGPT/26.1103.4239.0/OpenAI.ChatGPT.installer.yaml).
Signed package pins, licensed installation, registration, service identity and
freshness checks remain separate from the original UI transaction. No test registry writer can substitute for Connect or Restore.
The pre-existing 4239 treatment-only entry guard is unchanged. See
[the acceptance contract](../../build/original-connect-desktop-plan.md) for the
exact evidence, host boundary, source exclusions and offline mutation tests.
Until the real Windows report passes its stages, this remains an unverified
acceptance implementation, not a usable package or working chat claim.

A stage-only run can additionally opt into the compile-only
[fixed handshake observer](../../build/original-backend-handshake.md) with
`[original-backend-handshake-v1]`. Both packaged binaries and BUILD.json must
carry the exact corresponding diagnostic tags. It preserves a preliminary
post-recovery snapshot and a separately labeled post-cleanup snapshot, without
turning bootstrap or initialization milestones into backend/desktop acceptance.
The existing 15-second image-observation budget is used in full in this mode;
no deadline is extended. Missing or incomplete traces remain unknown.

The disposable Windows job precompiles the unchanged capture C# helper once,
before starting cleared-environment children. The runtime only loads that
fixed-path DLL; missing, corrupt or other-origin helpers fail closed, with no
compiler fallback. Initialization fixtures and real capture retain the 8-second
limit. The separate build step has a 2-minute limit and proves compilation only,
not successful window capture. See the [capture contract](../../build/original-desktop-window-capture.md).
The post-sequence precondition observer guards optional Windows `HOME` access
before indexing the environment under PowerShell strict mode. A missing `HOME`
uses the existing `USERPROFILE` identity rule, while empty or mismatched values
remain rejected.

The diagnostic-only handshake observer distinguishes client, primary and secondary
stream termination, primary exit before bridge cancellation, and cancellation of
the bridge loop. These fixed labels contain no backend output or error messages
and do not change stream ownership, deadlines, retry behavior or acceptance.

## Shared-state initialization ordering

Only the explicitly shared-native route starts the primary first and defers the
secondary until the router has accepted the primary's genuine initialize result.
The original secondary initialize delivery and any early initialized notifications
are retained in order, bounded to 128 deliveries and 32 MiB. A rejected response,
stream failure or client cancellation does not authorize secondary startup.
Ordinary isolated-backend mode retains parallel startup. Neither native home,
history storage nor the existing Magpie proxy is changed.

This removes concurrent first-start initialization as a source of interference;
it is not proof of the exact underlying database failure or chat readiness.
Both native replies and the existing policy/catalog checks are still required.
The diagnostic reader accepts both the earlier parallel stage sequence and this
ordered sequence, but does not infer a successful handshake from startup alone.

Protocol failures record only fixed source, envelope shape, pending-operation
category and error class before the existing failure stage. Unknown content is
reported as Other; payloads, request IDs, method names and raw errors are never
written. These labels do not relax validation or confer handshake readiness.
