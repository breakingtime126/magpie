# Native Codex test inside the original Agents page

This addition is not a new window, provider page or Desktop integration. When the
host explicitly sets `bootPrefs.nativeCodexPreview=true`, the existing Codex row
gets **Native backend test**. Its ordinary state, fields and Connect switch are
not rewritten by this module. Connect still has its existing gateway meaning;
its host-level restrictions and labeling belong to the startup-mode guard.

The action uses the original `openModal`, `confirmAction`, `input`, `el`, `t`
and editor/row/chip styles. Opening reads controller state only. Program selection,
native login, model discovery and a one-turn text test are separate user actions.
Discovery, login, AIHub configuration and sending require the app's confirmation
dialog. The service key is never persisted by the sheet, is cleared before a
configuration request and is erased when the sheet closes. Fields are locked
while submitting, so a pending response cannot confirm a newly typed endpoint.
A new login clears the old official-context models even if login fails.

`codexnative.js` posts only to `/api/codex-native/{state,auto,choose,configure,
discover,login,send,cancel}` using the private `bootPrefs.nativeCodexToken` in
`X-Preview-Token`. The controller's response shapes and capability flags are
unchanged. Names and replies render as text. Errors use fixed UI text, not native
error bodies. Closing the sheet requests cancellation of an in-flight operation,
without retry or undoing native side effects. Provider configuration, native
state, process cleanup and token/origin admission remain server responsibilities.
This module never invokes existing provider/test, Connect/Disconnect, Sync,
account-switch or import APIs, and does not read existing provider credentials.

The host must register the namespace and inject the explicit boot flag/token;
these are not implemented by this UI-only patch. With the flag absent the script
adds no action and makes no native API requests. It must not be enabled in a
remote/browser-sharing endpoint without a separately reviewed trust boundary.

This is connectivity testing, not same-window Codex Desktop model switching,
quota verification, persistent account management or historical conversation
acceptance. A discovered model is labelled unverified. A successful native login
exit is not proof that chat works. Sending is allowed only when the controller
advertises it and after confirmation of possible charges. Cancellation cannot
undo a sent request, charge, login or managed-store update. Native transport may
itself retry; the UI does not automatically repeat operations.

## Verification

`node --test internal/gui/tests/codex-native.test.cjs` uses the original page and
fixtures only, with English/Chinese runs in Chromium/WebKit. It covers opt-in vs
default, unchanged Connect state, state-only opening, confirmation before native
operations, one-turn reply rendering, stale-model removal after failed login,
and the AIHub submission input lock. No real programs or credentials are used.
New strings also have Traditional Chinese, Japanese and German translations.

Local preparation passed JavaScript syntax and exact-base patch checks. The full
browser test still requires the complete repository assets and the corresponding
Playwright browser installations; it has not been claimed as passed locally.
