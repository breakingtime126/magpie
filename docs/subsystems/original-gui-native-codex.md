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
