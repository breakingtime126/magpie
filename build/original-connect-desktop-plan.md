# Original Connect Desktop acceptance: staged candidate

No runtime execution or GUI acceptance is established by these files. The
read-only discovery helper has a default-off package-job caller. Publish and run
only after its current source has been reviewed and authorized.

## Implemented first slice

The existing package script opts in with `-DiscoverOriginalConnect` only on an
explicit `[original-connect-discovery-v1]` push marker and hosted-runner context.
It runs offline helper fixtures, changes only the disposable copy to English,
and calls discovery after its screenshot but before owned-process cleanup.
A blocked read writes fixed evidence then fails the job; a completed tree read
may contain zero matching controls and does not claim action acceptance. The
deliverable retains Chinese settings and the existing nativeActions=0 report.

The caller dot-sources `original-connect-discovery.ps1` in PowerShell Core. It
launches the original packaged Magpie GUI in a fresh copied portable directory
with a fresh synthetic native home (the existing inert config fixture) and
`lang=en`, `noAccountDiscovery=true`.
Then call `Invoke-OriginalConnectDiscovery` with that retained Process object
and its verified executable path. The caller owns the Magpie lifetime, screenshot,
package hashes, source provenance, fixture creation and cleanup. Do not substitute
a process found by its basename or inspect a user's existing window.

Both processes require a hosted Windows GitHub runner on the one experimental
branch and `MAGPIE_ORIGINAL_CONNECT_DISCOVERY=read-only-v1`. The supervisor starts
only its fixed sibling worker, supplies a small environment allowlist, imposes a
15-second deadline and 8192-character limit per stream, and kills only that worker
if necessary. UIA calls may hang despite inner checks; hence the separate process.
The worker holds the target process handle and checks PID generation, executable,
window handle and exact title around a 2048-node/depth-20 traversal. This is not
an atomic proof against concurrent same-user window changes.

Only three English labels are counted: Configure Codex launch selection,
Disconnect and restore Codex launch selection, and Disconnect and restore.
Counts include visibility/enabled state, control role and Invoke/Toggle pattern
availability. Names are source-defined by the recovery UI patch, not yet observed
on Windows. Unknown strings, UI trees, errors, runtime selectors and paths are not
exported. Incomplete traversal returns blocked with no partial counts. A successful
read remains zero-action discovery, not a verified selector or recovery transaction.

The PowerShell offline fixtures cover exact labels versus case/prefix/suffix and
protected-screen controls, syntax and no-action/limit source contracts. They have
not been executed locally because PowerShell is unavailable. They are not a mock
of the Windows UIA implementation or proof that a native pattern invokes the
correct browser event. Real discovery remains necessary.

## Next action slice, not implemented or enabled

1. Verify signed official 26.1103.4239.0 package and existing complete pins through
   the established installer path in a disposable VM. Do not expand accepted
   builds, alter package contents or suppress service/freshness checks.
2. Make synthetic provider configuration through its existing file format:
   ordinary AIHub HTTPS endpoint, public inert key, explicitly saved model choice.
   No model Fetch, account discovery, real account or provider turn. Keep the
   production endpoint unchanged; no DNS, proxy or transport rewrite.
3. Start the verified original patched Magpie GUI from its copied package. Its
   native home must be newly empty and its two launch-selection registry values
   read before the test, without writing them. Capture the owned window and run
   discovery. Reject duplicate/disabled/hidden controls or unsupported patterns.
4. Before connecting, establish a supported recovery action using discovery and
   current GUI source; a source-derived name alone is not runtime evidence.
   One explicit UIA action may then target the exact unique Configure control.
   No external HTTP request may forge ownWindow, no console shortcut may call
   Connect, and no replacement registry writer may simulate success.
5. Observe the new restore control, exact journal and installed registry values.
   Do not infer backend success from appearance or SelectionConfigured alone.
   Launch normal official AUMID only after existing fresh-process/package checks.
   Stop inference/navigation at login, onboarding, setup or policy barriers.
6. Use the actual original restore control, inspect its confirmation, and invoke
   the exact confirm control once. Verify original registry presence/types/values
   and journal removal. Never treat repeat Connect as Disconnect. If outcome is
   uncertain, stop, retain the journal and report the exact observed stage.
7. Cleanup only owned handles/processes. Never erase a journal needed for recovery.
   Neither this sequence nor registry readback verifies a later restored Desktop
   start; the existing treatment-only 4239 guard remains unchanged.

Actual original GUI action, official activation, journal assertions and fixture
caller are intentionally not implemented until discovery supplies runtime control
identities/patterns. No inference, tool call, auth, setup, permission adjustment,
real-provider request or same-window conversation is part of this first slice.
Absence of a chat action is not proof of zero background network traffic. Review
Magpie background tasks before claiming even zero provider requests; if startup
can contact the configured supplier, the no-request slice needs a supported
existing configuration that prevents it, not transport interception.

## Background-source check before running

`internal/gui/backend.go:startBackend` runs `provider.FetchNew` automatically.
`internal/provider/models.go:newDue` skips an ordinary provider when Account is
nil and no decision endpoint is due. `internal/provider/decide.go:Decides` requires
an explicit Decide endpoint. `internal/provider/autolist.go:refreshDueLists`
skips RefreshInterval zero (empty/off). Thus the proposed fixture must have no
Account, Decide, plugin, subscription, auto-refresh or account discovery. These
source checks are narrower than an executed network audit. Catalog refresh and
application telemetry may still perform unrelated background network activity.

The existing Desktop installer/launcher is monolithic; this candidate does not
silently invoke its launch side effects as package preparation. The future caller
must preserve its pinned install/registration checks and separately prove the
original Magpie action. Current discovery by itself installs no Desktop package,
creates no provider or registry state, and cannot establish original AUMID launch.

## Future same-run action admission

A separate manually inspected discovery run is not a continuing permission
requirement. Once implemented and reviewed, the bounded action stage can read the
current owned window, require a unique exact visible/enabled target, verify its
actual pattern and expected state, and invoke that retained element in the same
run. Recheck identity immediately before and state/journal/readback immediately
after. Unexpected screens or uncertain outcomes stop without an action retry.
The initial read-only run establishes which Wails UIA roles/patterns actually
exist; it is an engineering prerequisite, not an imposed manual handoff. No
Invoke/Toggle action has been added by this candidate.
