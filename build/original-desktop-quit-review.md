# Original Magpie quit: source review, not acceptance

Reviewed parent: `26dcefa169ad0291b478389c49e8e3d33262414d`.
This review and its read-only discovery helper add no quit action, Magpie launch, UI interaction or cleanup claim.

## Existing cleanup cannot establish graceful quit

`internal/gui/lightweight.go` cancels WindowClosing and hides the window.
`windowsOptions` in `internal/gui/app.go` disables quit on last window close.
The existing acceptance `CloseMainWindow` then five-second wait therefore is
not the original tray app's Quit operation. A subsequent retained-root kill
still cannot establish graceful host or descendant drain.

## Supported panel route has an additional startup lifetime

`internal/cli/gui_on.go:runPanel` and `openPanel` support the `panel` argument.
`internal/gui/app.go:singleInstance` keys the identity on settings.Dir(), with
the native build marker, and dispatches the second instance to togglePanel.
The panel title is `magpie panel`. The original index.html has a `Quit`
button with id `quit`; app.js binds it to the original window/quit route and
removes it in main-window mode. These are source labels, not runtime UIA
observations.

Before the handoff, `internal/cli/main.go:run` calls startUp. This makes dirs,
migrates settings, settles/refreshes plugins, folds provider copies and runs
legacy-agent migrations. In gui.Run, stats.Run and startBackend precede
application.New/SingleInstance. startBackend starts a gateway watcher and
catalog/provider work. Native guards prevent several legacy Codex writes,
update settlement and gateway displacement, but do not make this command a
side-effect-free IPC request.

The acceptance's fresh portable data, isolated environment, noAccountDiscovery
and inert provider narrow this substantially. This review did not demonstrate
access to real user data, nor establish that every concurrent startup activity
and descendant ends with the helper. Do not describe the candidate as harmful
to user settings, but do not infer safe cleanup from helper root exit either.
No second launch was performed. Its additional lifetime remains unverified.

## Read-only tray discovery is source-feasible

The repository pins Wails v3.0.0-beta.24 with no replacement. Exact-tag sources:

- [systemtray_windows.go](https://github.com/wailsapp/wails/blob/v3.0.0-beta.24/v3/pkg/application/systemtray_windows.go):
  windowsSystemTray.run creates a **message-only** window using HWND_MESSAGE,
  the app's Windows.WndClass, no title, and sets uid from parent.id.
  newNotifyIconData uses that HWND and UID, without NIF_GUID. bounds calls
  GetSystrayBounds with that exact pair.
- [application.go](https://github.com/wailsapp/wails/blob/v3.0.0-beta.24/v3/pkg/application/application.go):
  an empty Windows.WndClass defaults to `WailsWebviewWindow`; systemTrayID
  starts at Go's zero value. Magpie windowsOptions does not override WndClass.
- [system_tray_manager.go](https://github.com/wailsapp/wails/blob/v3.0.0-beta.24/v3/pkg/application/system_tray_manager.go):
  New calls getNextID, which increments before returning. Magpie app.go has
  one production SystemTray.New call, so the original tray UID is **1**.
- [shell32.go](https://github.com/wailsapp/wails/blob/v3.0.0-beta.24/v3/pkg/w32/shell32.go):
  GetSystrayBounds constructs NOTIFYICONIDENTIFIER with its correct native
  size, the HWND, UID and zero GUID; calls Shell_NotifyIconGetRect and requires
  S_OK. That is the supported read-only geometry route, not Explorer memory.
- [popupmenu_windows.go](https://github.com/wailsapp/wails/blob/v3.0.0-beta.24/v3/pkg/application/popupmenu_windows.go):
  NewPopupMenu retains the tray HWND as parent. ShowAt sets that parent
  foreground and passes it to TrackPopupMenuEx. This establishes intended
  native ownership, not an observed UIA popup identity.

A bounded read-only worker can enumerate message-only windows with
FindWindowEx(HWND_MESSAGE, previous, `WailsWebviewWindow`, null), check each
returned HWND with GetWindowThreadProcessId against the retained original
process generation/executable, and query UID 1. Require exactly one successful,
nonempty rectangle; zero, multiple, failures or identity drift are blocked.
Cap enumeration and runtime in a retained subprocess supervisor. Do not use
EnumWindows alone: message-only windows are not ordinary top-level windows.
Only emit fixed counts/booleans; keep HWND/geometry internal to the probe.

A successful rectangle does **not** prove the icon is visible, unobscured, in
the main tray rather than hidden overflow, or safe to click. It is discovery
evidence only. No coordinate click follows from this report. Do not send
synthetic WM_COMMAND or a tray callback message as a substitute for original
UI input. Do not match an Explorer icon by tooltip or process basename.

## Remaining action review

After discovery, a separately reviewed one-shot original right-click could
open the menu only after visible hit-target identity is established. Its menu
must then be checked against the original retained PID/native owner and exact
original label `Quit magpie` (English fixture), with a unique enabled UIA
MenuItem. These menu observations have not happened. Source-only knowledge
of TrackPopupMenuEx does not prove a future popup's UIA owner chain.

Both tray Quit and panel Quit call AskQuit. It may show a confirmation sheet;
a second Quit within one minute bypasses the sheet. Never retry Quit, click
`Disconnect and quit`, or toggle `Don't ask again`. An unexpected sheet or
failed root-exit observation is uncertain and retains ownership.

Any eventual action requires caller-confirmed restored selection (or positively
blocked zero-action Connect), independent exact baseline re-read and journal
absence. No Desktop quit belongs in this helper. Root exit alone does not
prove all descendants drained or the native shutdown hook succeeded.

## Discovery-only API

Dot-source `original-desktop-quit-discovery.ps1` in the existing acceptance
caller (Windows PowerShell Desktop or Core), opt in with
`MAGPIE_ORIGINAL_QUIT_DISCOVERY=read-only-v1`, and call
`Invoke-OriginalQuitDiscovery -OwnedProcess $process -ExpectedExecutable $exe`.
The worker requires the same hosted Windows experiment push context. It uses
installed PowerShell 7 and an allowlisted environment. It never acts on Magpie.
Parent retains the Process object. Catch exceptions as an unavailable probe,
not as cleanup success. Do not replace existing cleanup with this function.

The supervisor deadline is 15 seconds, with a further bounded three seconds
for helper termination. Worker checks a ten-second cooperative deadline and
256-window enumeration limit. Both output streams are capped at 8192 chars.
Only the retained helper can be killed; Magpie is never signaled. Reports have
fixed fields/counts, UID 1, and geometryValid. visibilityVerified and quitVerified
are always false: a rectangle in the virtual-screen bounds is not visibility.
No HWND, raw title, coordinates, settings or environment are exported.

Validation: PowerShell parser checks and offline reducer/report failures pass.
Four mutation checks are caught (geometry admission, visibility claim,
virtual-screen bounds, zero-action contract). The first mutation run exposed
ineffective mutation choices, which were replaced with meaningful cases.
No Windows native calls, UI observation, action or end-to-end cleanup were
executed here. No product code, app.js, or existing helper was changed.


## Optional read-only hit-test delta

A Shell rectangle can now be sampled at its center with UIA FromPoint twice,
under the same supervisor deadline and retained-owner checks. The fixed
`hitTest` field is `unavailable`, `mismatch`, or `matched`. A match requires
stable nonempty runtime identity, exact case-sensitive name `magpie`, Button
role, enabled and not offscreen, InvokePattern support, and all four bounding
edges equal to the shell rectangle, in both samples. No ancestor search,
focus, Invoke, or coordinate input is performed. Failed UIA initialization,
null/error reads and changing identities give unavailable. Nonmatching
properties give mismatch. Multiple native candidates cannot export matched.

The UIA provider can belong to Explorer. A coincident hit is not proof of icon
ownership or future click safety, and a dynamic tooltip can legitimately yield
mismatch. `visibilityVerified` and `quitVerified` stay false even for matched.
No raw names, runtime IDs, coordinates, provider PID or error text is exported.
The source-owned HWND/UID rectangle remains the independently checked input;
UIA does not replace that ownership check. No click is authorized by this data.

Additional offline fixtures cover null/error reads, case mismatch, private
unknown labels, wrong role, disabled/offscreen/non-invokable hits, unequal
rectangles, changing runtime identity, ambiguous native windows, and strict
report vocabulary/redaction. Two additional mutation cases remove name and
runtime-identity comparisons. Windows UIA execution remains untested.
