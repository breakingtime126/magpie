# Original Magpie GUI: native Codex experimental build

This build uses Magpie's original `main`, tray, window, navigation, Agents page,
provider pages and controls. It does not open the separate connectivity-preview
window. The original Agents → Codex row has a **Native backend test** action in
the existing editor. This action uses the reviewed official `app-server` path;
it is not the old HTTP gateway Connect switch and does not claim that Codex
Desktop's same-window multi-provider integration is complete.

## Try the portable build

Extract the entire archive into a new, private folder, then open
`Magpie-Native-Codex-Experimental.exe`. Keep `.portable` and `data` beside it.
These are Magpie's existing portable-mode files, not a new storage mechanism.
The original provider/settings interface uses this new data folder; it does not
import an existing Magpie library. Account discovery is off in the supplied
settings. Other original pages and explicit actions retain their normal meaning.

Open **Agents → Codex → Native backend test**. Find the installed official
program automatically, or choose an executable you already trust. Use its
explicit browser login for the official account, or explicitly configure your
trusted AIHub HTTPS address/key. Discover a model, select it and confirm one
short-text test. You never need to enter backend IDs, internal homes or JSON.
Native-discovered models are not entitlement or service-capability proof.

The short-text path currently requires the reviewed native
`0.167.0-alpha.3.2`. `0.166.0-alpha.6` is limited to login/discovery; unknown
versions stop. Do not bypass company update or executable restrictions.
The EXE is unsigned and requires an existing approved WebView2 Runtime. It
does not install WebView2 or change Windows sandbox/security settings.

## What is protected, and what is not

The `nativecodex` build tag sets the constant in `internal/buildvariant` before
normal startup. Agent-level guards stop legacy Codex gateway wiring, automatic
Codex configuration/rollout migrations, and Codex-specific Library targets,
including the WSL/profile family. Existing Codex Connect/Disconnect is not a
native connection and is rejected in this mode, rather than silently performing
the old gateway migration. Ordinary builds keep the old behavior. Synthetic
fixtures exercise unchanged files; they are not a claim about a user's data.

The experimental GUI does not install itself over an existing Magpie, refresh
shortcuts/autostart/link registration, take over another Magpie's gateway, or
self-update into an upstream release. These actions are unavailable here.
It still is the original application: other providers, account-discovery choices,
explicit history edits, other agents, shared Library inputs and normal gateway
activity retain their original side effects. This is not a promise that the
whole GUI is read-only, offline, or that every file visible to Codex is frozen.

The new native action does not import the old Magpie account store. Each preview
uses dedicated native contexts, but all processes run as the same OS user;
separate folders are not a credential security boundary. Native managed auth,
proxy and certificate rules continue to apply. Login/discovery can contact
services and write authentication state. Exact provider ID checks do not by
themselves establish the complete endpoint provenance of managed configuration.
Use only trusted executables and service addresses.

Each Send is a new ephemeral thread with empty environments and reviewed tool
guards, not a continuing developer session. It may incur costs. Native
transports can retry, so it is not a fixed-cost or single-HTTP-request guarantee.
Cancel/close cannot retract a transmitted request. Closing cancels the owned
operation, but does not close the login browser, revoke/logout credentials or
delete native authentication state. Deleting the portable app folder removes
its portable settings, not any native login context elsewhere. The normal
Codex launch selection is not changed by this test action.

## Evidence and remaining gap

The existing scoped test has run the signed, pinned official native program
against a wholly original local Responses fixture through guarded discovery,
thread start, streaming and final text. No real account or GPT/AIHub service was
used. That evidence does not establish real-account usability, zero egress,
all Windows security modes, or official Desktop same-window chat.

This original-GUI integration must separately pass its original-page browser
tests, Go protections and Windows build/window checks at the published commit.
A captured window is not proof of account login or native-button completion.
The same-window production design still has separate model-catalog ownership,
credential/proof isolation and Windows sandbox-ownership requirements; this
native test must not be presented as solving them.
