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
identity checks and bounded RawView traversal. Unknown text is not exported.

Connect requires a unique exact visible/enabled Button with TogglePattern Off.
It makes one UIA Toggle call, then checks the actual journal's previous values,
installed adapter/profile paths, exact profile hash and registry readback.
Restore discovers the current original restore control and its supported
Toggle On or recovery Invoke pattern, opens the original confirmation, verifies
its exact explanatory text and unique confirmation button, then invokes once.
Success additionally requires original Configure Off, removed journal and exact
baseline registry presence, type and unexpanded values.

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

Only after verified original recovery does the coordinator capture the owned
windows and close retained process roots. Forced root termination blocks clean
shutdown acceptance. Root exits do not prove descendant cleanup; the report
explicitly marks descendant exit unverified. On uncertain recovery the GUI,
journal and generation are left intact for the disposable runner's remaining
lifetime; there is no alternate rollback writer or deletion.

## Reports, tests and limits

Published evidence contains fixed stage/boolean/count fields and owned-window
screenshots only. Baseline registry data, journal/profile bytes, attempt files,
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
activation freshness/pins and recovery-read failure. Offline fixtures do not
substitute for the real opted-in Windows run.
