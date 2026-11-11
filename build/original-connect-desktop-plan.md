# Original Connect Desktop acceptance

The default package job remains a zero-action original-window capture. The
separate `[original-connect-desktop-v1]` commit marker opts its disposable
GitHub-hosted Windows runner into the implemented action sequence. It is
exclusive with `[original-connect-discovery-v1]`; neither marker grants live
account access or makes the package a user-ready release.

## Runtime evidence before this action implementation

At `f96e5ee9642ffbc5d2eb595de12fc5615f564fb5`, the original Magpie screenshot
showed the Codex row and its off switch. The bounded RawView read observed one
exact **Configure Codex launch selection** element, visible and enabled,
ControlType.Button with TogglePattern and no InvokePattern. ControlView found
none. Restore and confirmation controls were absent in that disconnected state.
Discovery performed zero actions. The same original-window and browser suites
also passed at `f26b2c6469a13677c654cf3099e7e8f40558498b`.

The action implementation below has offline behavior and deliberate mutation
tests. Actual original UI Connect, official AUMID activation and original Restore
remain unverified until the opted-in Windows report establishes each stage.

## Owned execution and official preparation

`package-native-magpie-windows.ps1 -AcceptOriginalConnect` builds the unchanged
original GUI entry and same-source adapter with the existing compiled pins. It
then invokes `original-desktop-acceptance.ps1` in Windows PowerShell Desktop.
That one coordinator owns all retained package/service/activation handles and
its fresh copied Magpie process. It does not use the monolithic cold-start
harness's direct registry writer.

`original-desktop-package.ps1` separates exact signed official 26.1103.4239.0
package verification, official licensed provisioning, registration readiness,
held installed-image/service verification and freshness from activation. Its
bounded verifier functions are copied verbatim from the reviewed cold-start
script and parity-tested. A changed floating download stops before installation.
No package patch, signature workaround, execution-policy change, service stop,
reinstall, session workaround or account action is supplied. The pre-existing
4239 native-entry workflow remains treatment-only and unchanged.

The disposable Magpie copy uses English, no account discovery, a newly empty
native home and one ordinary explicitly selected AIHub fixture model. Its key
is the public inert string `PUBLIC-INERT-NOT-A-CREDENTIAL`; its ordinary HTTPS
endpoint is unchanged. There is no account, Decide endpoint, plugin, automatic
model refresh, model Fetch or provider turn. Source checks in provider/models.go,
decide.go and autolist.go establish those narrow startup exclusions, not a
network audit. Catalog refresh, telemetry and official Desktop background
traffic are not proven absent. Existing inherited proxy/trust settings remain.

## Original UI transaction and recovery

`original-connect-action.ps1` accepts only the retained owned Magpie process,
expected executable/adapter, exact journal path, a private baseline snapshot
and a fresh attempt directory. The Desktop-compatible supervisor runs its fixed
Core worker with a minimal environment, 60-second deadline and bounded streams.
The worker uses a 45-second inner budget, held PID generation/executable/HWND
identity checks and bounded RawView traversal. Before the first action, a
15-second read-only readiness phase waits only for a missing, hidden or disabled
exact Configure control. Duplicate controls, wrong role/pattern/state, failed
reads and owner changes stop. Fixed readiness counts and reason labels preserve
the last complete read; unknown text is not exported. The final fresh uniqueness
and runtime-element check still precedes the one-shot action.

Connect requires a unique exact visible/enabled Button with TogglePattern Off.
It makes one UIA Toggle call, then checks the actual journal's previous values,
installed adapter/profile paths, exact profile hash and registry readback.
Restore discovers the current original restore control and its supported
Toggle On or recovery Invoke pattern, opens the original confirmation, verifies
its exact explanatory text and unique confirmation button, then invokes once.
Success additionally requires original Configure Off, removed journal and exact
baseline registry presence, type and unexpanded values. During Connect readback,
a bounded read-only sample once per second records the first and last complete
counts of the two exact controls and allowlisted native errors, including while
the journal is absent. Failed reads retain earlier complete observations and
stop fail-closed. Busy/error observations never establish completion, authorize
recovery, or change the single-action and deadline rules.

Each mode is exclusively claimed on disk. Every attempted action is durably
marked before entering UIA. A lost output, timeout, identity change or uncertain
result is never retried. A distinct first Restore may recover a valid journal
after uncertain Connect. A clean registry snapshot with no journal does not
prove an asynchronous uncertain Connect has settled; that case retains the
original process and recovery state. External registry drift is never overwritten.

`original-desktop-sequence.ps1` runs Connect, a single normal AUMID activation,
and original Restore in finally. Activation retains existing pins/freshness and
uses persistent selection, never process-injected launch overrides. A bounded
read-only image observation records exact direct-child adapter/native starts;
it does not establish bootstrap completion or backend handshake. No Desktop
UI action, login, setup or policy navigation occurs.

Acceptance screenshots and root cleanup follow verified original recovery.
After the sequence finally returns and its report is saved, separate diagnostic
helpers may capture the original Magpie window on an unverified sequence and
inspect its owned tray geometry. Capture uses one separately bounded child and
publishes a validated PNG under `original-magpie-uncertain.png`; private partial
files are retained. PrintWindow requests target rendering; the child deadline
does not prove that target rendering finished. No focus, activation, input,
desktop-wide capture or fallback occurs. Capture success never changes the
original failure, uncertainty, cleanup authority or preserved recovery state.
Its eight-second worker deadline has additional bounded startup-error, stream
drain and disposal waits, so it is not an eight-second whole-call guarantee.

Tray discovery is read-only, externally bounded and fixed-field only. Its
geometry and optional coincident UIA hit are not visibility, input ownership or
authority to click. See [the source review](original-desktop-quit-review.md).
Normal window close hides this tray app and cannot prove graceful Quit. The
existing root-cleanup failure gate remains until an original Quit route is
independently observed and reviewed. No second app launch or Quit action is
added by these diagnostics. Forced root termination blocks clean
shutdown acceptance. Root exits do not prove descendant cleanup; the report
explicitly marks descendant exit unverified. On uncertain recovery the GUI,
journal and generation are left intact for the disposable runner's remaining
lifetime; there is no alternate rollback writer or deletion.

## Reports, tests and limits

Published evidence contains fixed stage/boolean/count fields and owned-window
screenshots only. Preparation retains schema/status/stage/cleanup and nullable
fixed failure and download records even when failure occurs before a package
context is returned. Cleanup does not overwrite the failed operation stage. Separate path, open, digest and
hash-match stages distinguish unavailable input from a mismatched pin. The
versioned official package URL changes acquisition only; all admission pins and
trust checks remain mandatory. Failure evidence contains only a fixed category,
integer HResult and WebException status, bounded HTTP status, and source line.
Unknown values remain unknown; they do not authorize retries or a different
client. Each official download uses the same Windows PowerShell
Invoke-WebRequest in a retained owned child with a 180-second worker deadline,
plus bounded supervision and cleanup. Progress display is suppressed; transport
settings, proxy and trust environment are inherited unchanged. Only the two
fixed official URLs and fresh owned destinations are admitted. Timeout, overflow,
nonzero exit, invalid result or unknown cleanup blocks the next preparation
operation. Partial files are retained, never retried or reused. Download evidence
reports only fixed status/deadline/timeout/exit fields; timeout byte progress is
unavailable. Baseline registry data, journal/profile bytes, attempt files,
arbitrary UI text, process command lines and private paths stay out of artifacts.
The report always leaves backend handshake, chat, restored Desktop startup and
zero-egress claims false. The bounded sequence is not same-window GPT/AIHub
conversation acceptance or proof of future authenticated operation.

Run the offline `original-connect-action.tests.ps1`, its `.mutations.ps1`,
`original-desktop-package.tests.ps1`, `original-desktop-sequence.tests.ps1` and
`original-desktop-acceptance.tests.ps1`. The Windows gate runs compatible
fixtures in both Core and Desktop hosts before any live action. The sequence's
`-BreakRestoration` test must fail on missing registry/journal cleanup. Tests
cover exact selectors, duplicate/disabled/hidden controls, deadlines, identity
drift, one-shot attempts, external-value refusal, installed-source parity,
activation freshness/pins and recovery-read failure. The tray discovery and
window-capture fixture/mutation suites run before live acceptance; Windows
capture fixtures use Desktop PowerShell for native PNG checks. Offline fixtures do not
substitute for the real opted-in Windows run.

The post-action report retains the first complete known-error observation as
well as first/last control snapshots, so a transient allowlisted error is not
lost. Capture reports only validated fixed phase names under its existing
stream and deadline bounds; a phase never proves rendering or action success.
The optional read-only Connect precondition observer runs after saved recovery
evidence. It reuses retained official registration metadata and the original
launch environment, inspects bounded owned path metadata, and performs no new
package query, native execution or file-content read. Its fixed predicates do
not prove the original discovery, CLI-version probe, embedded pins or host
preparation succeeded. Unknown observations stay unknown and cannot change
recovery decisions. See [its contract](original-connect-preconditions-review.md).


## Fixed-stage Connect log observer

The bounded [fixed-stage collector](original-connect-stages.md) reads only the
fresh original app's `data/logs/app.log`, after the sequence's recovery checks and
saved report. It emits fixed structured diagnostics only. Missing, partial or
malformed traces are unknown, never proof that a stage failed or did not run.
The collector adds no action, retry, cleanup authority or acceptance claim.

### Stage-only investigation

A fresh hosted push can additionally opt in with the exact commit marker
`[original-connect-stages-only-v1]` and matching `stage-diagnostics-v1` environment
value. The existing `[original-connect-desktop-v1]`, exact source, repository,
branch, isolated fixture, original UI and transaction/recovery guards still apply.
This mode runs the same Connect/activation/Restore sequence and fixed stage
collector, but imports or invokes no screenshot helper and takes no successful
window captures. It always reports `diagnostic-only`, never full acceptance,
and cannot publish the portable package. Structured evidence is uploaded even
when the transaction is uncertain. Capture fixtures run honestly in their own
independent job; their failure neither blocks this diagnostic transaction nor
permits acceptance. Full mode retains its capture prerequisite and behavior.
