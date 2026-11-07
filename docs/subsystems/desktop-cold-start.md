# Official Desktop cold-start CI

The experimental `Experimental Desktop cold start` workflow is a separate,
credential-free baseline on a fresh GitHub-hosted Windows 2025 VM. It never
installs or runs anything on a user's computer, changes `main`, publishes a
release, or imports account state.

It downloads the current x64 Store-signed package and offline license from the
[official Windows deployment page](https://learn.chatgpt.com/docs/enterprise/windows-deployment),
verifies trust with the Windows SDK signature tool, records SHA-256 and manifest
identity/version, and checks the existing runner's interactive desktop. The URL
is floating. The first run verified 26.1103.3517.0, SHA-256
`ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643`.
Subsequent runs stop if those bytes change. An explicitly selected
`InspectPackageIdentityOnly` diagnostic can inspect signed ZIP metadata before
that unchanged pin gate. It always stops before installation or package execution,
including if the floating URL returns the old reviewed bytes again. This is not a reproduction of
historical Desktop 26.1029.7803.0. The verified manifest has three applications;
the Desktop entry is selected by its exact `app/ChatGPT.exe` executable, and its
AUMID still comes from actual registration plus manifest application ID.

Only the documented licensed provisioning command is used. No unsigned-package
switch, license bypass, manifest change, certificate installation, security or
network policy change, new session, RDP, account login, or repackaged executable
is allowed. If provisioning does not register the application in the current
runner session, the probe stops and reports that limitation.

Activation uses the registered AUMID and Windows' application activation manager.
Process package identity, visible window geometry and a bounded screenshot are
separate evidence. Each launch is observed for 60 seconds without UI interaction. A screenshot can
be occluded and must be reviewed; a green
job means only that those baseline observations were obtained. It is not a
successful bridge connection, usable model picker, account login, model response,
or production acceptance.

The first phase deliberately does not set `CODEX_CLI_PATH`. Normal MSIX activation
has no per-launch environment-block parameter, so setting an environment variable
in PowerShell does not prove that Desktop receives it. The second job uses another fresh VM and the same SHA-pinned official package.
It launches only the newly registered `chatgpt-desktop.exe` alias declared under
that manifest's Desktop application. An identically named alias existing before
installation is a blocker. Directory-entry enumeration checks the documented
root alias location as well as the family-qualified location; the initial `Test-Path` check failed to discover the latter, while subsequent
directory enumeration found both. A separate fresh runner returned no entries
immediately after provisioning, so enumeration now waits approximately 60 seconds for
registration to materialize. It does not trigger activation, rewrite registration,
or retry access errors. Continued absence is a reported launcher blocker. The selected entry must have the documented
CreateProcess alias reparse tag `0x8000001B`. The probe never parses undocumented
APPEXECLINK payload strings or fabricates an alias. Actual runtime package identity
remains a separate mandatory check. Non-shell process
creation supplies a minimal, non-secret, process-local environment containing
the exact commit-built bridge path. These default baseline/direct-launch modes never write user/system environment.
The separate native-entry experiment below permits exactly two temporary USER
path variables on a fresh disposable VM, with mandatory rollback.

The pinned package contains both `app\\Codex.exe` and
`app\\resources\\codex.exe`. Only the latter resource backend is considered, and
its `--version` must identify it as `codex-cli` in a separate empty home with no
inherited authentication environment. This is an identity/version check, not a
model or authentication request.

The baseline/direct bridge configuration has two new empty homes and loopback discard endpoints,
no API key, and no startup, requirements, attestation, OAuth, auth-status or
feature-list opt-ins. The separate
instrumented follow-ups below record their separate startup and launch-compatibility
opt-ins. Startup inspection alone still selects only its original profile. The exact
commit-built production bridge uses a unique commit-derived filename and is
validated using `--check` before monitoring.
The probe requires a matching child process-start event under a package-verified
Desktop parent, and records file hashes before/after. Short-lived events may not
permit querying the running image path; this evidence is not proof of a successful
protocol or model request. Static adapter error markers may be detected in fresh
app log files; no raw log lines, auth files or arbitrary environment are emitted. Do not substitute naked extracted EXEs, unreviewed persistent environment changes or
debug injection to make this test appear to pass.

A screenshot of at most 1 MiB is also emitted as base64 in workflow logs for
remote visual review where supported. Workflow-log retention is independent of
the seven-day artifact retention; no successful image retrieval is assumed.

Artifacts contain only public package/signature metadata, sanitized test-process
facts and the fresh unauthenticated window image. They do not contain the MSIX,
offline license, app data, credentials, raw process command lines, or environment
values. Test-created activation is closed when possible; the disposable VM is
the final process and filesystem lifetime boundary.

## Bounded USER-environment native-entry experiment

[Experimental Desktop native entry](../../.github/workflows/experimental-desktop-native-entry.yml)
adds a separate, default-off `-NativeEntryCase` selector to the existing signed
package harness. The production bridge and existing baseline/policy workflows
retain their behavior; this is not an installer or a deployment recommendation.
The new case requires the exact experimental repository, branch, GitHub-hosted
runner and dedicated workflow path. It rejects separate startup/compatibility/UI switches. The entry selector itself
selects only the existing exact-version genuine startup comparison in the two
private native homes, so the bridge need not exit instantly at that boundary.
It selects no launch-compatibility, auth, attestation, routing or UI modes. The same pinned signed package, registered identity, original backend
version/hash, private bridge homes, window capture and sanitized process evidence
remain mandatory.

The original acceptance contract has four independent fresh-VM cases, all still
uncompleted. The workflow temporarily schedules only the single observation-only
case below until its result is interpreted; it does not repeat this four-case
matrix unchanged:

- `baseline`: first official AUMID activation with no overrides, requiring a
  live observed original native executable with the pinned image path/hash.
- `treatment`: write only `CODEX_CLI_PATH` and
  `MAGPIE_APP_SERVER_BRIDGE_CONFIG` to `HKCU\Environment` as two `REG_SZ`
  paths to this job's exact bridge/config, read them back, notify the shell,
  then make the first official AUMID activation.
- `restore-before-activation`: set/readback/notify the same two values, restore
  and verify their exact prior absence, notify again, and only then perform
  the first AUMID activation. It requires the original native image. This is
  an independent fresh-activation control, not a second launch of treatment's
  possibly still-running package family.
- `alias-control`: the existing verified application-execution alias with
  explicit process-local bridge paths, plus the new exact image evidence and
  root cleanup checks. This is a positive launch-selection control, never
  evidence for Start/taskbar or shell environment inheritance.

Preflight rejects either target name's presence in process, machine or USER
state, including an empty or differently typed USER value. USER presence is
checked before reading any prior value. The exact prior state is consequently
known absence, not a saved user configuration. Existing default `.codex/auth.json`
or `.codex/config.toml` files are presence-only blockers; credentials are never
read. An alternate process HOME/CODEX_HOME, any prior Desktop/native/bridge
process or existing registration blocks the experiment. No persistent HOME,
CODEX_HOME, APPDATA, machine environment, security/network setting, package
content, Explorer restart, logoff or new session is used.

The [helper](../../build/desktop-native-entry.ps1) marks mutation attempted
before its first write and restores both names independently in the harness's
`finally`, before activation-process cleanup. Even a partial/uncertain write,
notification failure or launch exception cannot skip the rollback attempt.
Readback must show both exact original absences. Values, raw environment,
arguments, credential files and runner tokens are not published. Scratch remains
private until VM disposal because whole-family exit is not established.

The harness's direct `--version` and `--check` children now use
`Complete-DesktopProbeProcess` in `finally` for every caller. A successful wait
is followed by confirmed exit and disposal of that same held process object;
an early error reaps only the process the harness itself started, confirms exit,
and disposes it. No freshness-scan match is a termination target. Releasing these
handles matters because an exited Windows process object can remain enumerable
until its last handle closes. Entry reports record only fixed before/after
visibility counts for those owned probe PIDs and exit/close booleans. Diagnostic
reads have a five-second CIM operation timeout (not an absolute wall-clock
guarantee), and their own enclosing `finally` cannot bypass owned cleanup. A PID
counter is diagnostic, never permission to ignore a process.

The final freshness guard still rejects every original package/name match,
including a remaining owned-probe PID candidate, an active unrelated match or
an unclassified package process. Its new metadata contains only bounded counters
for package, Desktop/native/bridge names, the two known probe PID candidates and
other matches. No arbitrary name, path, command line or process environment is
published. Failed enumeration remains an error, not an empty result.

The [first run at `46248d03`](https://github.com/breakingtime126/magpie/actions/runs/42310342651)
passed the offline safety gate in both hosts, but all four package cases stopped
at the freshness guard before environment writes or activation. Both owned
version/config checks had passed; their process objects were not disposed before
the scan. The report did not identify matching rows, so it does **not** prove that
retained exited probes caused that failure. The new Windows lifetime fixture
observes an exited child while holding its handle, closes it and waits for its
PID to disappear from CIM, while a separate stdin-held live child must remain
a guard blocker. Its fixed booleans can support or refute the lifetime hypothesis
on the actual runner without installing a package or changing environment.
The lifecycle fix and that new fixture still require exact-head Windows execution.

At `53ab904f`, both owned probes were confirmed exited and closed, with zero CIM
rows before and after close in every case. The Windows fixture likewise reported
`heldExitedVisibleBeforeClose=false`, `exitedVisibleAfterClose=false`, and
`liveNeighborStillBlocks=true` in both hosts. All four package cases still found
exactly one package-identity match, with zero Desktop/native/bridge-name matches
and zero owned-probe PID matches. Retained probe handles therefore do **not**
explain the observed blocker on these runners. The process's role is unknown;
package identity alone does not establish that the Desktop GUI was already open.
The bridge's `--check` reads configuration without starting children.

### One observation-only freshness pass

The temporary `freshness-observation` case uses baseline provisioning and checks,
then stops at the same strict predicate. It never sets USER variables or activates
Desktop, even if there are no matches. The original acceptance cases remain in
source, but the native-entry workflow now schedules only this one fresh VM after
the existing safety gate. No result automatically triggers another run or weakens
the guard.

[`desktop-entry-observe.ps1`](../../build/desktop-entry-observe.ps1) receives at
most four matching PIDs through a bounded private input file. A separate
allowlisted helper has a 30-second execution deadline and a 4 KiB output bound;
input is capped at 16 KiB. Each opened process handle requests only query-limited
and synchronize rights. Package identity and executable image
come from that same held handle. PIDs are reopened after the initial guard scan,
so these records describe the current process at each matching PID, not proof
of the original scanned generation. `originalGenerationVerified` is always
false; no causal attribution or exemption follows from PID reuse assumptions. An `observed` record requires the expected
package to be revalidated and the process to stay alive throughout the reads.
This minimal observation does not query CIM, parent PIDs, creation times or
lineage. Those fields were removed because identifying the exact current
package-associated image does not require them and cannot grant any exemption.

Only closed labels, booleans and counts leave the helper. Image labels compare
exact paths under the verified install root against the three pinned manifest
applications, the known native resource and package Codex executable, or exact
paths for RuntimeBroker, backgroundTaskHost, ApplicationFrameHost, svchost,
dllhost and Explorer under the runner's Windows root. These are image-path
classifications, not role/safety exemptions or independent binary-signature
proof. An unknown path becomes `other`; inaccessible or changed processes remain
unavailable. No arbitrary names, paths, arguments, environment, credentials,
PIDs or timestamps are published. The helper disposes only the query handles it
opened, never signals or terminates a matched process.

The full match count still governs the blocker; the four-record cap cannot omit
a blocker from the predicate. Even a verified RuntimeBroker label would only
identify the observed process. Deciding whether a package-associated broker can
coexist with a genuine fresh Desktop launch requires interpreting this one result,
not automatically excluding it. New fixtures cover exact-path classification,
manifest changes, raw-field rejection, portable Int32/Int64 JSON integers
(with bool/double/string rejected), forbidden parent/raw fields, query-only
native interop, the complete bounded helper against an owned non-package test
process, the observation cap and unconditional stops before activation
or environment writes. These new changes still need exact-head Windows execution.

At `062aaa9a`, the first PowerShell 7 safety invocation stopped in the bounded
helper's shared failure check before the observation VM could run. The old log
did not separate deadline, stream completion, overflow or nonzero exit, so its
roughly 37-second elapsed interval does not identify the failing operation.
The helper now emits only fixed supervision evidence: start state, wait/timer
deadline flags, stream completion, zero/nonzero/unavailable exit and confirmed
cleanup. The shared supervisor's overflow bit also covers stream read errors,
so it is explicitly named `overflowOrReadError`, not asserted to prove excess
bytes. No raw child output, stderr, exception text, process input or path is logged.
If supervisor construction throws before returning a held object, start and
cleanup remain uncertain and the exit-unconfirmed flag is set. The caller does
not infer a harmless pre-start failure or search for a PID to terminate.
Fixed fixture labels locate a failing safety check. Ordered observation phase
frames distinguish input validation, native interop, held-image/package reads and
report completion; only a strict frame grammar followed by the original
canonical validated JSON can succeed. Partial markers are not completed phases.
All deadlines and byte limits remain unchanged. An expired bounded wait cannot
pass because a process later exits zero before
the timer callback fires. No-input helpers now receive
explicit stdin EOF; this is lifecycle hygiene, not an established explanation
for the failed run. Removing the unnecessary parent/CIM reads reduces scope;
it does not establish that those reads caused the prior failure. Fixtures cover
EOF, nonzero exit, deadline and byte overflow,
redaction and malformed frames. Exact-head Windows execution remains required;
no observation, environment inheritance or successful activation is claimed.

At [`b4f704e4`](https://github.com/breakingtime126/magpie/actions/runs/42316096858/job/128228938689),
the owned-process fixture reached only `started`, then exceeded its 30-second
deadline. The report confirmed helper exit and completed streams, with no
overflow/read-error flag. This locates the failure before `input_validated`,
not in native process observation; the exact operation remains unknown.
Four new completed-phase frames separate file metadata validation, bounded byte
reading, JSON parsing plus stream disposal, and input-shape validation. The
existing `input_validated` frame still follows known-image-table construction.
The output parser requires every frame in order, and the diagnostic phase
scanner recognizes only complete fixed lines. No raw input or error is added.
A zero-record fixture exercises the same complete helper/input/interop path
without opening a process handle, before the existing owned-process fixture.
The helper still has the same 30-second deadline, 4 KiB output and 16 KiB input
bounds, cleared environment, stdin EOF and held-process cleanup. This is a
diagnostic change, not a confirmed timeout fix. Both PowerShell host runs and
the original package observation remain unverified for this change; no workflow
or 4239 native-entry run is enabled by these frames alone.

The subsequent [`71dd9e9b` single-VM run](https://github.com/breakingtime126/magpie/actions/runs/42342444847/job/128312877629)
failed in the Core-host zero-record case after 30 seconds, again at `started`
and now specifically before `input_file_validated`. Streams completed, no
overflow/read error was reported, and helper exit was confirmed. This narrows
the known stalled segment to `Get-Item` and its metadata validation; it does
not establish the provider's internal cause or prove module autoloading was
responsible. No Desktop provisioning, activation or USER mutation occurred.

`Assert-EntryInputFileMetadata` replaces that segment with direct
`File.GetAttributes` and `FileInfo.Length` calls. It rejects directories,
leaf reparse points, hidden files and system files. Hidden retains no-Force
filtering; System rejection deliberately tightens the private-input contract,
not a claim of exact Windows PowerShell 5.1 provider equivalence. It requires
1 through 16384 bytes and propagates failed metadata reads to the
existing fixed failure path. The bounded stream read still checks the actual
byte count independently. This neither closes a file-replacement race nor
adds ancestor-reparse validation. The helper deadline, output/input bounds,
cleared environment, phases and held-process cleanup are unchanged.

Adjacent fixtures use real zero-, one-, 16384- and 16385-byte files, a missing
path, a directory, read-only, hidden and system files with verified attribute
restoration, and an owned junction. Junction
rejection also exercises the directory flag, so it is not isolated proof of
the reparse branch. Cleanup deletes known leaves without recursive traversal.
The existing complete zero-record and owned-process cases remain mandatory
in both hosts. The previous Windows run is a behavioral failure; the metadata
replacement and new cases still require exact-commit Windows execution before
calling the stall fixed. Earlier native/bridge UI observations remain separate
evidence and are not rerun or strengthened by this change.

At [`2a8982e6`](https://github.com/breakingtime126/magpie/actions/runs/42343447135/job/128315953467),
the Core-host suite passed 627 assertions, including real metadata boundaries,
the complete zero-record helper (about 29.1 seconds), owned-process helper
(about 17.9 seconds) and owned-probe lifetime checks. The subsequent Desktop
host passed bounded normal output, then its decoded-stdin EOF case exited
nonzero after about 0.16 seconds. No deadline, overflow or read-error flag
was set, streams completed, and held helper exit was confirmed. This was not
another metadata timeout. Desktop provisioning and USER mutation were not reached.

The [.NET Framework Process reference implementation](https://github.com/microsoft/referencesource/blob/main/System/services/monitoring/system/diagnosticts/Process.cs#L2153-L2155) creates redirected stdin using
`Console.InputEncoding` and enables the writer's `AutoFlush` during
`Process.Start`. [`StreamWriter`](https://github.com/microsoft/referencesource/blob/main/mscorlib/system/io/streamwriter.cs#L293-L337) can emit an encoding preamble then, before
any explicit input or `CloseInput`. This is a source-backed mechanism, not
proof of the failed child's actual bytes or exact exit code. Changing only
the close operation cannot remove bytes emitted during construction.

The native helper wrapper now selects a preamble-free encoding before
construction. An already preamble-free encoding is unchanged; UTF-8 code page
65001 with exactly the EF-BB-BF preamble selects BOM-free UTF-8. Other
preamble-bearing encodings fail closed before starting a child. The serial
caller temporarily assigns only that input encoding and restores the original
in `finally`, including failed construction, with code-page/preamble readback.
This does not change the console code-page value, environment, registry,
user preferences or shared supervisor. The managed input reader can be reset
by the encoding setter; this private serial caller has no concurrent stdin reader.
This does not provide general concurrent-console or cross-thread isolation.
The existing held ownership, zero-input close, output bounds and deadlines
remain unchanged. Restoration failure fails the operation and still reaches
the existing held-process cleanup.

The original decoded-empty-input assertion is retained. An additional helper
requires raw `OpenStandardInput().ReadByte()` to return `-1`, without stripping
or accepting a BOM. Tests also exercise real memory-stream preamble emission,
encoding selection and original encoding restoration after helper success and
failure. Both parent hosts must pass the complete existing prerequisites;
the metadata and process-identity fixtures are not skipped. These new tests
and the encoding correction still require exact-commit Windows execution.

The [`a823a4cb` entry run](https://github.com/breakingtime126/magpie/actions/runs/42353998804/job/128347964132)
passed the Core 663, Desktop 667 and profile 1267 assertions, then stopped
inside the signed-package identity reader before returning identity metadata.
The downloaded 4239 package digest and signature checks matched; no activation
or USER write occurred. Its generic error does not identify a failed pin.

The reader now explicitly loads the installed `System.IO.Compression` assembly
before its first `ZipArchiveMode`/`ZipArchive` use. Loading only the separate
Framework `System.IO.Compression.FileSystem` assembly is not an explicit load
of those types. UI tool setup previously used `ZipFile.OpenRead` first, whereas
the entry path reaches this reader first. This dependency correction does not
by itself establish the cause of the earlier generic failure.

The existing native-entry safety suite adds two fresh Windows PowerShell
children under the same bounded helper, cleared environment and held cleanup.
Each AST-loads only the six required reader functions, writes the same static
645-byte synthetic ZIP, and calls the real reader without previously using a
ZIP API. The control removes only the new assembly-load line; the fixed child
must validate the synthetic package, manifest, backend and archive digests.
The control reports rejection or success rather than requiring an assumption
about assemblies preloaded by a particular host. Only an observed rejected
control plus passing fixed child supports the proposed dependency mechanism
for that synthetic path. Neither result establishes the prior real failure's
cause. Fixture signature arguments are supplied test values, not an official
package, an executable backend, a valid ASAR or real signature verification.

No workflow change is required: both existing host runs execute these cases
before the real treatment. No pin, size/time bound, freshness or USER rollback
rule changes. New fresh-host cases and the real signed-package path remain
unrun while authoring this change; local source/ZIP checks are not Windows proof.

The documented `WM_SETTINGCHANGE` / `Environment` notification uses a separate
allowlisted helper. `SendMessageTimeout`'s one-second timeout is per receiving
window, so the parent applies a separate ten-second helper deadline and verifies
that owned helper's exit after timeout. A `sent` notification does not prove
Explorer or an activation broker refreshed anything. Persistent restoration and
notification status are reported separately.

Normal activation runs in an allowlisted PowerShell helper with neither override
in its process environment. The helper itself checks their absence before
calling `IApplicationActivationManager::ActivateApplication` with no arguments.
It does not reconstruct USER values into the caller environment. The parent
also remains override-free. The returned Desktop PID must be a freshly created,
package-verified process in the interactive session; the harness holds its OS
handle and full-precision creation identity through exact-root cleanup.

This tests official AUMID API activation, **not a literal Start-menu/taskbar
click**. An event-driven bounded image watcher starts before activation and opens both
child and parent handles while alive, retaining exact image path and both
creation times. The main sweep also samples live children. Exact path/hash
evidence must be tied directly to the held activation parent and its full-precision
creation identity. Late or inaccessible events are counted as misses, never
upgraded from executable-name evidence. A uniquely named bridge process-start
event alone remains `bridge-start-event-only`; a short-lived child may escape live
image capture. No bridge observation remains unknown, even if an original native
image is seen. Both bridge and native image evidence can coexist after fallback.
Only exact bridge image evidence passes treatment/alias selection; only original
native image evidence without any bridge event passes the two native controls.
All successes also require capture success, exact persistent restoration and
confirmed exit of the held activation root. Cleanup failure fails the job.

Bridge selection demonstrates the backend executable choice only. It does not
independently establish that the bridge read the configured environment path,
started both native backends, completed startup, logged in or served a model.
The bridge may still stop at a later intentionally unsupported argument/protocol boundary.
There is no observer/wire trace in this entry-only slice, and no permission or
full launch-compatibility behavior is broadened to keep a process alive; the
separately documented genuine startup comparison is the only opt-in. Full package-family
exit and same-session runtime rollback after a running treatment remain unproven.

Offline [safety fixtures](../../build/desktop-native-entry.tests.ps1) use only mock
snapshots/callbacks. They cover dirty preflight, absent-versus-empty/type equality,
partial writes before/after errors, rollback of both names despite an individual
failure, notification failure, concurrent state changes, no token/override in the
activation caller, controls and failure precedence. Process fixtures exercise
normal output, byte overflow and timeout with confirmed helper exit; Windows
PowerShell also compiles and runs the image-event observer against an owned
stdin-held fixture, verifies exact child/parent creation identities and checks
callback shutdown before releasing the fixture. A deliberate missing-second-
rollback mutation must fail the behavior assertion. Parsing and fixtures run in
both PowerShell hosts before any of the four package jobs. No PowerShell runtime
or real Windows activation was available during source authoring; exact-commit
CI results must be recorded before any claim that this experiment passed.

Sources: [Windows process environments](https://learn.microsoft.com/en-us/windows/win32/procthread/environment-variables),
[WM_SETTINGCHANGE](https://learn.microsoft.com/en-us/windows/win32/winmsg/wm-settingchange),
[SendMessageTimeout](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendmessagetimeoutw),
and [AUMID activation](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-iapplicationactivationmanager-activateapplication).

### Independent 4239 single-treatment entry profile

[`experimental-desktop-native-entry-4239.yml`](../../.github/workflows/experimental-desktop-native-entry-4239.yml)
is a separate push-marker workflow, disabled unless an exact experimental-branch
push changes one of its listed paths and the head commit contains
`[native-entry-4239-v1]`. The push must not contain the winapp, bridge-UI,
package-identity, original-native-UI or native-contract-review markers. No
workflow-dispatch capability is needed. One Windows 2025 VM runs the unchanged missing-second-rollback mutation,
the full native-entry safety suite in both PowerShell hosts, and the default
and selected-profile fixtures before building or running one treatment. There
is no matrix, retry or second activation. The new marker suppresses all ten
jobs across the existing bridge-Windows, cold-start, old native-entry and
policy-trace workflows for that push. Their existing conditions and behavior
for other commits are preserved. This prevents the policy trace's default
live-welcome action from running beside the new entry treatment, even if a
conflicting marker makes the new lane decline the push. No winapp marker is
used as a routing workaround. The other seven workflows at the reviewed base
do not directly respond to this branch push. Pull-request or scheduled events
are separate triggers and are not covered by this push-only exclusion.

The zero-record and owned-process helper cases must both reach validated
`output_ready`, exit zero and confirm cleanup in each host. The optional
`DiagnosticPath` on the test suite writes only fixed status/fixture labels and
the existing eight-field supervisor evidence, including the last completed
input phase. A terminating fixture error saves failed status before propagating.
The workflow also saves a fixed prerequisite-gate result in `finally` and
uploads these reports on failure. No raw helper input, output, stderr, exception
or environment is added to those diagnostic artifacts. A failed prerequisite
stops the job before package provisioning or USER-state mutation. The 30-second
helper runtime, 4 KiB output, 16 KiB input, cleared environment, stdin EOF and
held-process cleanup are unchanged; the unresolved timeout is not relabeled fixed.

`-EnableReviewedEntry4239` is default-off and requires `-Mode bridge`,
`-NativeEntryCase treatment`, the exact dedicated workflow path and the
`MAGPIE_NATIVE_ENTRY_PROFILE=user-aumid-4239-v1` marker. It rejects every other
experiment opt-in, including `-EnableReviewedBuild4239`. The existing 4239 UI
profile's exclusive admission, static-reader propagation and alias-cleanup
distinctions are unchanged. The shared signed-package identity reader must
establish the [reviewed 4239 tuple](#reviewed-4239-bridge-observation-profile)
before installation. The installed native version/hash check and both live
and event-based child-image checks select that same native digest.

`New-DesktopBridgeSettings` has a separate entry selector which permits only
the existing genuine startup comparison with two exact native executables.
It does not enable launch compatibility, attestation, gateway OAuth, synthetic
auth, control-plane configuration, live fixtures or UI input. The old observer
fixture package pin and native inference slice remain unchanged because neither
is a 4239 treatment runtime path.

The actual treatment reuses the existing strict fresh-state predicate, two USER
variables, readback and bounded notification, override-free AUMID caller,
held activation identity and exact-root cleanup. Any prior package/name match
still blocks before USER writes. Rollback still attempts both original absences
and verifies them before process cleanup, including on failure. No broker label
waives freshness and no child/name-based kill or scratch deletion is added.

Success means only that the original AUMID activation selected the exact bridge
image and persistent USER state was restored, with the held activation root
confirmed exited. It is not literal Start/taskbar click evidence, config-path
consumption proof, full process-family exit, same-session runtime rollback,
successful account login or a dual-provider conversation. This new lane and its
Windows prerequisites have not been run while authoring the candidate.

## Policy-read diagnostic

The direct launch at commit `56c7e8b6a55be2374f8fdced2a47962e095af078`
reached the original login screen in the baseline job. The override job logged
`source=override`, started the uniquely named bridge child, and then displayed
“Organization settings could not be loaded.” The Desktop log classified the
failure as `phase=policy-read reason=rpc`. This does not identify an exact method,
error origin, successful handshake, or a network denial.
[Observed run](https://github.com/breakingtime126/magpie/actions/runs/42169742564).

The separate `Experimental Desktop policy trace` workflow adds an explicitly
instrumented `observe` mode. Its Windows-only test observer launches the exact
commit-built production bridge and forwards arguments and stdio bytes unchanged,
without issuing extra requests, retries, invented results, changed capabilities or
policy overrides. The bridge's separately selected compatibility modes are recorded
in the report; the observer itself does not grant or select them.
It is not installed or included in the production package.

Only fixed method/flag/config-key names, generated correlation numbers, error
codes and fixed classifications, four initialization booleans, the bounded
request booleans documented below, and process/EOF metadata can enter the trace. Raw IDs or their hashes, parameters, results,
messages, argument values, paths, arbitrary stderr, credentials and proof tokens
are never trace fields. Unknown values are classified without their text.
PowerShell validates the fixed metadata schema and publishes a validated snapshot,
not a recopy of a still-growing file.

One fresh directory and prefix allow at most 16 exclusive 64 KiB trace files.
Duplicate request IDs remain uncorrelatable, and exhausting correlation capacity
disables further matching. Missing child exit or output EOF, exhausted trace slots or truncated
summaries mean incomplete evidence, not proof that no failure occurred.
The helper forwards ordinary Windows exit codes and output EOF. Its blocked
stdin reader is released by immediate process exit after the child ends; it is
not a reusable in-process proxy and does not claim Unix signal transparency.

Race/redaction/forwarding tests and a real Windows helper-process test must pass
before the instrumented Desktop launch. The latter holds inherited stdin open
while the native fixture exits, checking byte-exact output, EOF and exit status.
No old 0.166 compatibility mode is enabled simply because the packaged backend
reports 0.167.0-alpha.3.2. The first correlated failure must be established before
considering a separately reviewed compatibility change.

## Exact-version startup follow-up

The instrumented run at `b4775be7ea17a2f34bee7aaef329748dd98610cf`
identified the first blocker as the one-shot startup command, not a JSONL RPC.
Desktop passed `--print-startup-config --startup-client-info`; the default-off
bridge emitted `startup_probe_unavailable` and exited 1. No initialize or
configRequirements/read request occurred in that complete invocation.
[Metadata and screenshot](https://github.com/breakingtime126/magpie/actions/runs/42173927946).

The separate `-EnableReviewedStartupProbe` switch enables only the exact-version
unrestricted startup profile for the verified bundled 0.167.0-alpha.3.2 binary. Its SHA-256 is
`40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`.
This does not enable any old authentication or JSONL requirements profile.
Both independently configured official commands must return the same complete,
actually unrestricted success result. A non-null requirement, different version,
unsupported field or mismatching backend result still stops startup. This is not
support for arbitrary organization policies or proof that different underlying
policies are equivalent.

The official one-shot command may refresh policy/auth state or write caches.
This CI uses newly created empty homes, no account credentials and no login or
inference requests. It does not claim zero attempted network activity, and it
does not change firewall, network security or machine policy to force a pass.
Linux race tests and Windows focused tests gate the actual Desktop attempt.
Later initialization, authentication and model-use gates remain unverified until
separately observed. A green observation job must not be described as a usable
Desktop integration.

A genuine one-shot success object has neither a JSONL `result` nor `error` key.
The transport observer deliberately classifies that shape as `unrecognized`
without recording its body. That label alone is not a startup failure. Interpret
it with the unchanged bytes, production validator, child exit code and complete
output EOF, and examine a later JSONL invocation as separate evidence.


## Explicit CI launch-compatibility follow-up

At `643cc5d060659c68c75e342e7e0fd71590b6993c`, the
[Windows policy trace](https://github.com/breakingtime126/magpie/actions/runs/42213706926)
identified the actual normal-launch override by its source-bound public catalog:
`plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true`.
Desktop was the signed, hash-verified 26.1103.3517.0 package and its backend was
0.167.0-alpha.3.2. The one-shot startup exited zero while normal startup still
exited 2 under the old argument parser. The positive spelling match does not prove
plugin installation, loadability, successful initialization or authentication.

The separate `-EnableReviewedLaunchCompatibility` switch in
[the cold-start probe](../../build/test-desktop-cold-start.ps1) is default-off.
It requires `-Mode observe` and the independent
`-EnableReviewedStartupProbe` switch. The existing exact repository, experimental
branch and GitHub-hosted-runner guards run before any probe filesystem or package
actions. Only
[the experimental policy-trace workflow](../../.github/workflows/experimental-desktop-policy-trace.yml)
selects the additional switch. Baseline/direct-launch configurations and
startup-only callers are not broadened. For the additional launch-compatibility
switch, the signed package must also have exact manifest version
`26.1103.3517.0` and SHA-256
`ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643`.
A changed floating download or mismatching version reports a package mismatch and
stops before provisioning. Existing signature and registered-identity checks remain
mandatory; startup-only and baseline gates are unchanged.

The CI profile keeps `startup_probe=compare-unrestricted-v0.167.0-alpha.3.2`,
the exact `codex-cli 0.167.0-alpha.3.2` version check and the
`40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39`
executable pin on both backend entries. It separately adds:

- `desktop_plugin_override=codex-app-tools-v0.167.0-alpha.3.2`;
- `attestation=unavailable-v0.167.0-alpha.3.2`;
- `gateway_oauth=primary-read-only-v0.167.0-alpha.3.2`;
- `gateway_oauth_provider_name="CI alpha launch-only"`;
- `config_requirements_read=compare-null-v0.167.0-alpha.3.2`;
- `auth_status=primary-no-auth-v0.167.0-alpha.3.2`;
- `experimental_feature_list=compare-v0.167.0-alpha.3.2`.

These are configured modes, not assertions that their runtime exchanges succeeded.
The native forwarding contract and fail-closed behavior are documented in
[the app-server bridge reference](app-server-bridge.md).
The probe does not install, copy, provision or link a plugin/cache, set a shared
backend `working_directory`, load credentials or add inference, authentication or
diagnostic requests. Each native child retains its separate empty HOME and
CODEX_HOME, the existing loopback discard model endpoint, and its
`features.apps=false` / `features.plugins=false` configuration. A successful
argument parse is not evidence that a plugin was loaded or that a model is usable.
No firewall, network policy or other security setting is changed; no zero-egress
claim is made.

Gateway OAuth reads remain primary-native and read-only. A false or null result is
not logged in. Attestation remains explicitly unavailable; a failure there is not
evidence of a Windows token or access-token problem. JSONL requirements reads must
obtain actual successful `requirements:null` results from both backends on each
read. Missing, malformed, failed, non-null or incompatible results fail closed,
never become invented null requirements. The one-shot startup comparison remains
independent and unchanged. Reports list the explicit profile and these limits
without recording response bodies, credentials, proof tokens or raw arguments.
A green observation still means bounded process/window/metadata evidence only.

[Offline PowerShell profile tests](../../build/test-desktop-ci-profile.ps1)
AST-load only the allowlisted gate/configuration, sanitized-shape and evidence helpers. They cover missing
opt-ins, baseline/direct modes, repository/branch/runner denials, unchanged default
and startup-only JSON, all exact compatibility values, both executable pins,
independent backend homes, unchanged caller inputs, exact Desktop version/package
hash rejection, and backend version/hash/count rejection.
They run on Linux and Windows alongside all prior catalog and observer regressions.
The standalone `TestDesktopPluginLiteralReachesLaunchOptions` fixture is also
copied onto the exact pre-change `643cc5d0` adapter; its named behavioral assertion
must fail, so compilation, setup errors and skips are not accepted as red evidence.
No fixture launches Desktop, contacts an account or tests a real plugin.

## Static override identification

At `f5e0a7e9f90876d583341b3fa7500797c28e809e`, the official one-shot
startup path exited zero. Desktop then launched normal JSONL mode with an extra
`-c` key outside the four initial metadata labels. The adapter rejected the
arguments and exited 2 before reading configuration or starting JSONL backends.
An initialize request seen by the observer is not proof that the adapter read it.
[Observed run](https://github.com/breakingtime126/magpie/actions/runs/42176293112).

A subsequent metadata-only refinement freezes 233 exact public config names
from the reviewed official schema at `c40729ea5cf8e947481b130c59807273eda2f8e9`,
blob `f5149c6efdf7ab9f1550aa0639dd5f2401205f25`. The finite feature names plus
selected boolean/enum keys are not a wildcard. Known literal booleans and allowed
per-key enum spellings become fixed classification labels. Objects, paths,
credentials and other values remain redacted; unknown or dynamic keys remain
unknown. Nothing is fetched from a schema during observation. This does not add
production argument support or authorize any setting change. The exact actual
override must be observed and its semantics reviewed before support is added.


The 233-name observation still classified the actual normal-launch override as
unknown at commit `05e3bd90a50bca2c2a3164148a748c17aa992cad`. It did not
identify the key or prove JSONL initialization reached the adapter.

The next instrumented CI pass reads only three fixed source entries in the
already signature-, hash- and registration-verified official package's
`app/resources/app.asar`. Entry names come from that exact package's recorded
failure stack. The stdlib-only Node reader neither loads nor executes package
code, extracts files, modifies the package, nor reads user data. The documented
Electron ASAR layout, sizes, offsets, links and redirects are checked; bounded
synthetic archive and lexer tests run on Linux and Windows first.

The output consists solely of static candidate key names, fixed classifications,
counts and explicit unsupported states. Raw source, values, command lines and
dynamic key portions are not output. PowerShell independently validates the
metadata schema before publication. The lexical reader is intentionally not a
full JavaScript parser or control-flow proof. Ambiguous syntax discards that
entry's candidates. Computed or escaped keys, returned object forms and other
unsupported definitions can remain unresolved. A candidate does not prove the
actual runtime override and must never automatically expand production argument
acceptance. The adapter and policy behavior remain unchanged.


The static inspection at `49a6aed41c71f8ac74d450d2a98200604ad0017d`
completed and found a direct `-c` candidate named `forced_login_method`.
The pinned official schema permits only `chatgpt` or `api`. A subsequent
observer-only change recognizes this one additional fixed key and those two enum
labels to determine whether it is the real extra override. Arbitrary values stay
redacted, and neither production acceptance nor login policy changes. Static
source also contains unrelated command flags, so candidates are never treated as
proven app-server launch parameters. Both Linux and Windows synthetic inspector
suites passed 16/16 without skips at the observed commit.


The `9c69ae37750e25545657f5260e0e903fad7c7810` runtime still reported an
unknown override after adding the forced-login candidate. This neither confirms
that candidate nor excludes whitespace/quoted key spellings. No production
forced-login support was added from this hypothesis.

The observer revision at `31f269b0a6acb9bd5bb7e8d676ca89b797e96e03`
froze all 830 explicitly named property paths in the same pinned schema,
following only explicit properties and local schema references/combinators.
It never admits dynamic map entries or wildcard paths. The original 234 value
classifications retain their types; every additional name has an opaque value.
The actual additional override still appeared as `other_key`/`other` and the
normal app-server invocation exited 2. That classification was not evidence that
the official CLI would reject the argument.

### Native literal-key diagnostic correction

Reviewing the pinned official
[CLI parser](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/utils/cli/src/config_override.rs)
and [override application](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/overrides.rs)
showed that the CLI splits at the first equals sign, trims the whole key, then
splits its path at literal dots. It does not unquote key segments or trim each
segment. The earlier observer's TOML-like normalization therefore could falsely
label a quoted spelling as a known setting. No production argument acceptance
was based on that diagnostic normalization.

The corrected observer recognizes only whole-key ASCII whitespace trimming and
fixed bare dotted names. Quoted keys, whitespace within a segment and other
unreviewed syntax remain unknown. One schema property has a literal dot within
its segment; its frozen quoted boundary remains unrecognized. No flattened
alias is invented. Non-ASCII whitespace may be accepted by the native parser
but remains conservatively unknown to this diagnostic.

Unknown config events include only fixed shape/family labels plus three bounded
classifications: an exact public root followed by a dot or `other`; a fixed,
ordered set of character classes found only before the first equals sign; and
whether the complete right-hand side is literally `true`, `false` or other.
No original key, dynamic suffix, raw value, length, position, count or hash is
published. The lexical root does not assert that the native parser accepts the
setting or that its value has any particular semantics. PowerShell validates
the fixed fields, labels, order and event relationship before publication.

All argument bytes remain unchanged. The production adapter still rejects
unsupported arguments, and authentication, permissions and organization policy
are not altered by this diagnostic. A recognized root or candidate remains an
investigation clue, not permission to apply a setting to either backend.

## Public bundled-plugin key identification

The actual `eed7071a22cc9fef22fdf1dc4def78d085f59cc0` Windows observation
identified the extra assignment as a `plugins.` key containing an at-sign and
the complete right-hand side `true`. The lifecycle trace was complete, but the
normal invocation still exited 2 before a successful initialize response.
[Observed run](https://github.com/breakingtime126/magpie/actions/runs/42190963869).
This rules out treating the earlier forced-login static candidate as the actual
parameter. It does not yet identify a plugin or the particular boolean field.

A further metadata-only lookup freezes the exact public identities named in
the same reviewed official source:
[bundled hook selection](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/plugin/src/bundled_hooks.rs#L28-L88)
and [plugin loader](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/loader.rs#L260-L274).
The identities are browser, chrome, chrome-dev, chrome-internal, computer-use,
unified-computer-use, sites and visualize, each at the openai-bundled marketplace.
Only their exact direct `enabled` and `auto_provision` property paths are
eligible. This is a 16-name investigation dictionary, not a claim to enumerate
every plugin shipped in Desktop. These source occurrences do not authenticate a
plugin or verify its signature. A different plugin, marketplace, nested field
or spelling remains redacted.

The lookup runs before the conservative bare-key reader because native plugin
identities contain an at-sign. It performs only whole-key ASCII whitespace
trimming and exact equality against frozen names, and emits only a matched
constant plus literal true/false/other. It never unquotes, extracts an arbitrary
identity, interprets a value, or changes the forwarded assignment. PowerShell's
fixed-label validator uses the same 16 additional names.

Recognition is not authorization to enable a plugin. Native `enabled` changes
installed-plugin activation and may expose skills, MCP servers, apps or hooks.
Native `auto_provision` is provisioning metadata, not authorization. Creating a
new entry can still make an installed plugin eligible for activation through
native defaults. It must not be forwarded as an inert or harmless setting. Neither field is passed through
by the production adapter in this diagnostic change. The actual exact match,
official behavior, both backends and remaining initialization gates must be
reviewed before any production compatibility change.


### Source-bound public catalog diagnostic

At `a54fd10a36483aaaf6498b3ce47682ae9f3415e9`, all three experimental
workflows passed, including
[the policy trace](https://github.com/breakingtime126/magpie/actions/runs/42193686360).
The verified 26.1103.3517.0 Desktop and its bundled
`codex-cli 0.167.0-alpha.3.2` still completed only the one-shot startup.
Normal JSONL startup rejected the extra assignment, and none of the 16 frozen
public plugin keys matched. A green diagnostic job did not establish a working
bridge.

The source-bound diagnostic obtains candidates only from the same verified
installed public package. The Node reader scans bounded packed JavaScript
under `.vite/build/` and four fixed marketplace-manifest layouts under
`app/resources/plugins/<marketdir>/`. It does not execute package code, read
private homes or environment values, search accounts, or fetch plugin catalogs.
The existing three-entry launcher inspection and its validation remain in place.

The added `publicPlugins` report is either a fixed blocked reason or a public
table with exact ASCII keys, source paths, SHA-256 values, derivation labels,
and explicit scan coverage. It scans at most 2,048 JavaScript files, at most
16 MiB each, and at most 64 unique JSON files (including at most 16 marketplace manifests)
of at most 256 KiB each, with a combined source
limit of 128 MiB. At most 64 supporting sources and 128 distinct keys fit in
the existing 64 KiB report bound. The Node inspection has a fixed two-minute
process deadline. Lexically unsupported JavaScript contributes
no keys. Such a scan is marked `partial`, and the coverage records its gap.
A complete scan still does not prove that its static candidates are runtime
arguments.

Before publishing any candidate text, PowerShell validates the complete report,
source/derivation relationships, exact key grammar, strict ordinal sorting,
bounds and key digest. It then opens the original downloaded MSIX read-only,
rechecks its already signature-verified fixed package hash, and streams SHA-256
over the unique exact `app/resources/app.asar` ZIP entry. That hash must match
the installed archive hash reported by Node. The complete archive binding
covers its internal JavaScript sources without executing or extracting them.
Each external manifest source must independently match its exact original ZIP
entry hash. Case aliases and duplicate ZIP entries, redirected package path
components, nonregular entry attributes, missing sources, oversized entries,
hash failures and a two-minute streaming deadline all fail closed.
No package is extracted, repacked or changed for this verification.

The handoff is the process-local `MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES` variable,
canonical compact JSON with ordered fields `schema`, `packageSha256`,
`archiveSha256`, `keysSha256`, and `keys`. The digest covers the ASCII-sorted
keys joined by exactly LF, without a trailing LF. Each key is at most 128
ASCII bytes and has the direct `plugins.<identity>@<marketplace>.enabled`
or `.auto_provision` form, or the source-linked MCP declaration form described
below. The payload is at most 16 KiB UTF-8; the complete
minimal Windows child environment is at most 30,000 characters including
entry separators and terminators. Existing presence checks reject an inherited
handoff. An empty, unsupported or invalid catalog stops before Desktop launch,
with an explicit unknown-identity result, rather than treating failure as no
matching plugin.

The observer preserves the original fixed lookup first. Only otherwise unknown
keys can match the validated dynamic table, by exact equality after the existing
whole-key ASCII trimming. A matched event contains only a one-based
`publicPluginRef` and `bool_true`, `bool_false` or `other`, never the key or a
runtime value. An independent `public_plugin_catalog` event identifies the table
by its key digest. PowerShell requires exactly one matching digest per observed
PID after observer start and before any argv or child event, even if no reference
matches. It checks the exact event shape and integer
bounds, and resolves references only against its independently verified public
table. Repeated argv references are allowed; duplicate catalog events, mixed
fixed-key/unknown fields, changed digests and out-of-range references are not.
Published match names come from the public table, never from argv. The observer
strips this variable before launching the unchanged production bridge.

This is positive-match evidence only. Partial coverage, unsupported syntax or
no match must not be described as proof that a plugin is absent. A match
identifies public spelling, not plugin authenticity, signature verification,
installed-plugin safety, successful initialization, or authority to enable or
provision anything. Neither `enabled` nor `auto_provision` is forwarded by this
change. Authentication, organization policy, production argument acceptance
and both private backend homes remain unchanged.

Offline PowerShell fixtures load only allowlisted validator function definitions
from the probe's AST. They use synthetic ZIPs and check field casing, unknown
fields, duplicate keys and source paths, coverage/provenance gaps, source and
handoff digests, source indices, per-PID catalog ordering, strict references,
ZIP aliases, file/descendant collisions and link attributes, and environment
bounds. A synthetic ASAR also exercises the actual Node report through the
PowerShell JSON and catalog validators on both platforms. The policy workflow
runs these tests, both Node suites and the observer/bridge suites on Linux and
Windows at the exact pushed head, while preserving prior behavioral red
fixtures. The new native-executable observer regression runs on Windows and must also
fail behaviorally against the pre-catalog `a54fd10a` code; its explicit Linux
skip does not count as red evidence. These tests never install Desktop or contact an
account; the separate fresh Windows observation retains the signed-package,
registration, GUI-session and no-account gates.


### Modern JavaScript parsing and construction diagnostics

The source-bound observation at
`e1ceddf7542163cfaacdc975a72387e51b2aeb82` completed with all three
experimental workflows green. Its
[policy trace](https://github.com/breakingtime126/magpie/actions/runs/42200500315)
still did not identify the normal-launch plugin override or obtain a successful
initialize response. One-shot startup exited zero; normal startup exited 2.
The original signed package, bundled backend and production restrictions were
unchanged.

The public table had 16 keys from one bundled marketplace manifest. Of 29
selected JavaScript files, 20 parsed and nine were skipped; all three fixed
startup chunks reported `unsupported_lexical_syntax`. The scan covered
12,431,638 source bytes. This establishes a parser coverage gap, not evidence
that the unidentified runtime key is absent from the package.


The public scanner now uses the pinned Acorn 8.15.0 full JavaScript parser,
vendored with its MIT license under
[build/vendor/acorn-8.15.0](../../build/vendor/acorn-8.15.0/README.md).
The offline tests verify the parser and license bytes against their reviewed
upstream Git blob and SHA-256 identities. No npm install, runtime download,
package import, dynamic code evaluation or execution of Desktop source is
introduced. The separate three-entry legacy launcher analysis remains labeled
by its existing lexical status; it is not silently reclassified as an AST
result.

The public analysis must finish parsing a whole source before retaining that
file's literal candidates or construction diagnostics. Syntax failure leaves
no partial candidates or constructions from the file. Parser-time token, node,
depth and deadline guards supplement the existing file and source-byte limits.
The inspector process also has a 512 MiB V8 old-space limit and the existing
120-second wall timeout; that heap setting is not a claim to bound total
process memory. Parser exceptions and stderr are captured and never published
as source excerpts or raw error messages. Source-reading, provenance, global resource and process failures block the
diagnostic rather than being interpreted as an empty catalog. The narrowly
classified file-local AST skips below discard all evidence from the affected
file and explicitly reduce coverage.

The additive `publicPlugins.analysis` object identifies the parser as
`acorn_8_15_0`. Its two ordered method rows describe `getConfigOverrides`
and `startupCommand`. `definitions` counts recognized function or method
definitions; `unresolved` counts maximal plugin-prefixed string, template or
concatenation expressions in those definitions whose paths cannot be classified.
Unresolved expressions can outnumber definitions. Both totals are independently
bounded at four million.

At most 128 coalesced construction rows carry only a verified ASAR source index,
one of those two method labels, `literal`/`template`/`concat` form, a fixed
path-shape label, `bool_true`/`bool_false`/`dynamic`/`other` value class,
and a positive count. Their total count is independently bounded at four
million. Sources can support these diagnostics without contributing a candidate
key. These are static syntax observations, not evidence that a method executes
or that any expression becomes a launch argument.

The eight shared shape labels are `direct_enabled`, `direct_auto_provision`,
`mcp_server_enabled`, `app_enabled`, `app_destructive_enabled`,
`app_open_world_enabled`, `app_default_tools_enabled` and `app_tool_enabled`.
They name only reviewed path forms. No plugin, marketplace, MCP server, app or
tool identity, source expression, arbitrary property name or constructed value
is emitted by a shape diagnostic. Shapes alone never become catalog candidates or extend production argument
support. The separately authenticated MCP declaration chain described below is
the only additional nested key form admitted to the diagnostic catalog.

For comparison, the observer can attach the optional `configPluginShape`
label to an existing unknown config argument. It remains unknown. Fixed known
keys and catalog-reference events never carry this label. PowerShell validates
the exact unknown-event shape and its relationship to the existing plugin root,
at-sign character classification and literal-value class. The key and original
argument bytes are never printed or changed. A shape match cannot establish
identity, plugin trust, runtime activation, authentication or successful startup.

PowerShell validates all new fields, scalar types, bounds, method order,
source kinds and indices, and duplicate construction tuples before the existing
original-MSIX provenance gate permits publication. Its tests cover valid forms,
aggregate boundaries, impossible method/count relationships, mixed runtime
event fields, malformed types and deliberate canary strings. A modern-syntax
synthetic ASAR exercises the actual Node report through PowerShell on Linux and
Windows. New behavioral red fixtures run against the exact `e1ceddf7` code
for both modern JavaScript parsing and unknown plugin path shapes; their expected
assertion messages must appear, so compile/setup failures do not count. Earlier
red fixtures and the guarded Windows native-exit handling remain intact.

The policy workflow watches the vendored parser, AST helper and standalone test
files explicitly. Changes to these diagnostics run their Linux/Windows tests
there without adding duplicate package installation to the separate baseline
cold-start workflow. All prior signature, archive/source binding, account-free
session, minimal-environment, per-PID catalog and production-policy boundaries
remain in force.


### Bounded partial parser coverage

The `af097bf4336559cb468ffb72b7458458845f2a9b`
[Windows diagnostic](https://github.com/breakingtime126/magpie/actions/runs/42204661575)
passed its 143 PowerShell and 52 Node checks, then stopped before Desktop launch
with the generic `public_source_limit` reason. The signed package was unchanged.
That observation does not identify which parser or package-reader bound was hit,
and it does not establish a runtime key.

The following diagnostic keeps every limit, but distinguishes six explicitly
identified file-local AST guards: token, allocated-node, parse-depth, walk-depth,
walk-node and per-file deadline. Like syntax errors, these discard the entire
file's analysis atomically. No candidate, source record, method count or
construction row from that file survives. Bytes already read still count toward
the unchanged aggregate source-byte budget.

The successful public analysis adds `skipped` rows containing only a fixed
`reason` and positive integer `count`. Nonzero rows appear once each, in this
exact order: `syntax_error`, `public_ast_token_limit`,
`public_ast_node_limit`, `public_ast_parse_depth_limit`,
`public_ast_walk_depth_limit`, `public_ast_walk_node_limit` and
`public_ast_deadline`. There are at most seven rows; their counts sum exactly
to `coverage.jsSkipped` and cannot exceed 2,048. A complete report has no skip
rows. Any skipped file requires `partial` status.

Source-byte limits, read or UTF-8 failures, archive/hash/provenance failures,
global source/catalog/construction-row caps, unclassified `RangeError` and
whole-process failures still block the entire diagnostic. In particular,
`public_ast_bytes_limit` and `public_ast_range_error` are never skip reasons.
The bounds remain 16 MiB per source, four million tokens, one million allocated
or walked nodes, depth 512 for guarded parsing or AST walking, and one shared
15-second deadline per source, in addition to the process heap and wall limits
above. Nothing is retried with larger limits or a less restrictive parser.

Independently completed JavaScript and marketplace-manifest evidence may remain
in a partial catalog. It still needs the same complete schema, digest and
original signed-MSIX source verification before publication or injection into
Desktop. An empty catalog still stops; a valid nonempty partial catalog can
support positive runtime matches and the fixed unknown-path-shape diagnostic.
Incomplete coverage and an unmatched runtime argument remain unknown, never
proof that a plugin or key is absent.

PowerShell validates skip ordering, uniqueness, exact field types and counts,
coverage arithmetic and the complete/partial relationship. Its fixtures reject
global failures disguised as skips, extra source/candidate fields, duplicates,
unknown or differently cased reasons and inconsistent totals. Blocked reports
retain the exact three-field shape with fixed reason labels and no retained
catalog, source excerpts or exception messages.

An additional offline regression file exercises the existing AST/package APIs
against exact `af097bf4` code. Both the typed parse-depth reason and retention
of independent complete evidence must fail with their intended assertions on
the old implementation. The current Linux and Windows suites run that same file;
the policy workflow explicitly watches its path and preserves every earlier
behavioral red check.


### Source-linked MCP declaration identification

At `e1c8fb52c61406baba1a6d712c0fcea83aacaf0d`, the
[Windows policy trace](https://github.com/breakingtime126/magpie/actions/runs/42207254261)
completed with 28 JavaScript sources fully analyzed and one whole-file
`public_ast_node_limit` skip. Its 22 direct catalog keys did not match the
normal-startup argument. The unknown argument had the fixed
`mcp_server_enabled` shape and literal true classification. No runtime
plugin, marketplace or server name was logged. Normal startup still exited 2;
one-shot startup exited zero. This shape is evidence for a narrower diagnostic,
not successful initialization or permission to enable a server.

The additional catalog form is only
`plugins.<identity>@<marketplace>.mcp_servers.<server>.enabled`.
All three variable segments retain the same ASCII alphanumeric/underscore/hyphen
grammar, first character alphanumeric, at most 64 bytes each; the complete key
still cannot exceed 128 bytes. App/tool paths, arbitrary nested fields and MCP
`auto_provision` are not admitted. The production adapter remains unchanged.

Candidates are derived from a connected declaration chain in the public package,
not a cross-product of unrelated names. The marketplace entry's name and the
marketplace document's name define the plugin identity, following the pinned
[native marketplace resolver](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/marketplace.rs#L530-L588).
Neither the directory name nor `plugin.json.name` substitutes for that identity.
Only bounded local sources are followed within that marketplace root. Native
recognized plugin manifest locations and MCP declarations are inspected
independently, conservatively recording public declarations rather than
asserting which source wins native precedence.

A nested candidate adds zero-based `marketplaceSource` and `pluginSource`
indices to its existing `source`, key and derivation. The fixed derivations
are `legacy_mcp_inline`, `legacy_mcp_default`, `legacy_mcp_path` and
`agent_mcp_default`. Inline declarations use the plugin manifest itself as
their source. Otherwise the source is the linked MCP JSON file; the default is
`.mcp.json` for a legacy manifest or `mcp.json` for a supported
Agent Plugins 1.0 root manifest. Explicit local MCP file references are bounded
to the same plugin root. An unsupported root schema is not reinterpreted as a
legacy manifest. The server-map key supplies the MCP policy segment before
native namespacing or app routing. No transport command, URL, environment value,
header, arbitrary JSON value or private metadata is published.

The new source kinds are `plugin_manifest` and `mcp_manifest`. All remain
inside `app/resources/plugins/<marketdir>/`, with safe ASCII components no
longer than 64 bytes and full source paths no longer than 512 bytes. Local
plugin-source paths and local MCP references have at most eight components,
including the MCP filename. Absolute paths, dot traversal, aliases, links,
variables and URI references are not followed. Every new source counts toward
the same 64-source and combined 128 MiB budgets. Each JSON file is at most
256 KiB; at most 64 distinct JSON files are read, including no more than 16
marketplace manifests. The 128-key, 16 KiB environment, 64 KiB report, parser and
process limits do not change.

The separate `manifestAnalysis` object contains only integer
`pluginEntries` (at most 2,048), `pluginManifests`, `mcpFiles`
(each at most 64), and `skippedEntries` (at most the entry count).
An entry with an unsupported or missing local source, manifest, schema, MCP
reference, file or server identity contributes once to the skipped count.
Valid sibling declarations can remain. The existing complete/partial status
still describes JavaScript coverage; manifest incompleteness is separately
visible and never means that other server keys are absent. Malformed or
duplicate JSON, read/provenance failures and global bounds still block the
whole catalog rather than yielding an empty result.

Before publication, PowerShell validates exact candidate shapes, derivation and
source-kind relationships, indices, shared marketplace/plugin roots, exact
default filenames and local path bounds. Every supporting marketplace, plugin
and MCP manifest must independently hash to its unique original signed-MSIX ZIP
entry on the same read-only verification stream. A terminal MCP file alone
cannot establish provenance. The trusted bounded extractor establishes JSON
relationships; PowerShell binds all supporting bytes and validates the emitted
relation, without executing or reparsing plugin configuration. The existing
whole-ASAR binding, ZIP ambiguity rejection, no-account/GUI-session gates,
per-PID digest ordering and reference-only runtime events are preserved.

This catalog identifies package-declared positive candidates only. It does not
authenticate a plugin, select native precedence, validate a transport, prove
that a server is installed or loadable, establish policy approval, or authorize
forwarding. A match is still just a public spelling match. Enabling an MCP
server can activate a connection or process; no enablement, provisioning,
credential, permission or production argument change is made here.

Offline tests cover every derivation, linked source kind/index/root, exact key
and path bounds, all-source ZIP hashes, aliases and links, and rejection of
unreviewed nested forms and raw values. An actual Node report from synthetic
marketplace/plugin/MCP files passes through PowerShell and original-ZIP binding,
with command/environment canaries absent from output. New scanner and observer
behavioral red tests use exact `e1c8fb52` APIs and require their named
assertion failures before the current Linux/Windows suites run. The workflow
watches the new MCP scanner tests explicitly and preserves all earlier reds.

The RPC metadata allowlist recognizes the exact method spelling
`getAuthStatus` and the reviewed public startup labels listed in the
[native trace-envelope reference](app-server-bridge.md#native-0167-request-trace-envelope).
Differently cased or suffixed spellings remain unknown. These are diagnostic
labels, not proof that Desktop sent a candidate. The bounded request diagnostic
below does not add a production handler or change routing, sign-in or policy behavior.

### Bounded getAuthStatus request flags

The signed Desktop `26.1103.3517.0` and bundled backend
`0.167.0-alpha.3.2`
[observation](https://github.com/breakingtime126/magpie/actions/runs/42217188118/job/127891199826)
showed `getAuthStatus`, `account/read` and `configRequirements/read` after
the initialize response. The original method-only trace did not reveal the
authentication request's flags. This motivates a narrowly scoped diagnostic,
not a claim that initialization or authentication succeeds.

Only an unambiguous client-to-bridge `getAuthStatus` request with ordinary
request correlation is eligible for the
[test-only parser](../../build/desktop-rpc-observer/auth_request_metadata.go). Its optional `authParamsValid` boolean is
true only when `params` is an object containing no keys except the exact
`includeToken` and `refreshToken` fields, each absent, null or a native JSON
boolean. For valid params, two additional booleans,
`includeTokenRequested` and `refreshTokenRequested`, report the native
requested flags, with absent/null fields defaulting to false. Those defaults
describe request syntax, not actual authentication state.

Missing/null/non-object params, unknown fields, wrong types and duplicate
parameter keys emit `authParamsValid:false` without requested flags. They never emit partial
requested flags or convert an invalid value to false. Escaped key spellings
are decoded before duplicate comparison. Nested data, arbitrary keys,
credentials, IDs, paths, response bodies and raw errors are never published.

The optional bounded native `trace` carrier is recognized without telemetry
propagation. A present carrier adds only `tracePresent:true` and
`traceValid:boolean` on an unambiguous client auth or requirements request;
invalid trace suppresses all auth flags. No trace IDs, state, lengths, hashes
or raw values are recorded. Diagnostic request IDs use the existing bounded
native string-or-int64 rule. See the
[shared contract, bounds and evidence limits](app-server-bridge.md#native-0167-request-trace-envelope)
rather than interpreting shape validity as authentication or W3C validity.
Absent trace retains the previous metadata shape and all old wire-byte bounds.

Ambiguous/duplicate envelopes, unknown envelope fields, invalid JSON-RPC version
fields, invalid IDs, notifications, responses, other directions/methods and
duplicate-ID or correlation-limit records carry none of these three fields.
Malformed, oversized and unterminated lines retain their existing generic
classification. Missing diagnostics mean unknown, never false. Observation
preserves every forwarded byte and all existing line/trace/correlation bounds.

PowerShell validates the exact event shape, native boolean types, direction,
method, request class and correlation before publication. A false validity
field forbids both requested flags; a true one requires both. The focused
public-catalog PowerShell fixtures exercise accepted and rejected shapes,
casing, unknown fields, invalid types and duplicate encoded aliases. The
self-contained
[`auth_request_metadata_red_test.go`](../../build/desktop-rpc-observer/auth_request_metadata_red_test.go)
checks flag combinations, invalid and nested canaries, ambiguous envelopes,
both directions, unchanged correlation, exact wire bytes and existing caps.
Copied alone onto `bcc80e7c31fa65f01e181c3eee08759862affa8a`,
`TestGetAuthStatusRequestedFlagsObserved` is the behavioral red target.
This is a test definition, not a claimed executed red/green result.

The default bridge still rejects `getAuthStatus`; the separately selected
[disposable no-auth status CI mode](#disposable-no-auth-status-ci-gate) below is
a narrow exception, not real-account support. Even when
`refreshTokenRequested:false`, the official implementation can perform
implicit credential refresh. These diagnostics do not establish a pure,
offline or side-effect-free read and do not authorize authentication changes,
login, token disclosure or a production compatibility mode.

### Complete pinned RPC vocabulary and read-request shapes

The observer and PowerShell sanitizer now share
[`rpc_method_facts.json`](../../build/desktop-rpc-observer/rpc_method_facts.json),
a finite, source-derived dictionary from official Codex
`c40729ea5cf8e947481b130c59807273eda2f8e9`. It contains 183 unique client
method names: 106 in the checked-in stable JSON schema, 74 experimental
declarations and three deprecated v1 methods. The checked-in JSON schema alone
is incomplete: the exporter removes experimental declarations and
`getConversationSummary`, `gitDiffToRemote`, and `getAuthStatus`.
The full [ClientRequest declaration](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/common.rs#L502-L1581)
is authoritative for this diagnostic vocabulary.

Generation independently recomputed every recorded upstream Git blob identity
and SHA-256 and reconciled all three disjoint name sets. Only public facts and
source identities are retained, not the upstream source bodies. Go embeds the
same JSON that PowerShell reads from this repository. Both verify its fixed
SHA-256 before using it. The JSON is one line without a trailing newline, so
Windows checkout line-ending conversion does not change its identity. No
normalization occurs before hashing. An invalid dictionary blocks diagnostic setup or
publication; it never becomes an empty or partially trusted table. Offline
tests verify the frozen artifact and provenance; they neither fetch upstream
nor independently regenerate upstream schemas during a test.

The existing `initialized` and `attestation/generate` labels remain separate
from the client dictionary. Exact decoded JSON spellings map to the fixed
dictionary value. Unknown names still yield only `unknown`, never a raw name,
sample, length or digest. The declared `mock/experimentalMethod` is included
as public test-method vocabulary, not supported behavior. This complete set
covers only the pinned public source, not undisclosed proprietary Desktop
extensions. Name recognition does not route a method or enable a capability.

Two additional request-only diagnostics expose native boolean fields after
the same bounded, unambiguous envelope, native ID, correlation and optional
native trace checks. Both methods retain native envelopes without `jsonrpc`.
Valid trace shapes retain `tracePresent:true,traceValid:true`; invalid trace
shapes expose those two booleans with `traceValid:false` and suppress all
parameter metadata. No trace value is recorded.

- `config/read` emits `configParamsValid`. When true, it also emits
  `configIncludeLayers` and `configCwdProvided`. Params must be an object
  containing only `includeLayers` and `cwd`. Omitted `includeLayers`
  defaults to false; explicit null is invalid. `cwd` is absent, null or a
  well-formed string. Only a non-null string, including empty, means provided.
- `experimentalFeature/list` emits `featureListParamsValid`. When true,
  `featureListCursorProvided`, `featureListLimitProvided` and
  `featureListThreadIdProvided` report only non-null field presence.
  Params must be an object containing only `cursor`, `limit` and
  `threadId`. Cursor/thread ID are optional nullable strings; limit is an
  optional nullable native uint32, including zero. Negative values, fractions,
  exponent tokens, overflow, booleans and numeric strings are invalid.

The exact source shapes are
[ConfigReadParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L390-L398)
and [ExperimentalFeatureListParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/experimental_feature.rs#L10-L22).
String validation rejects invalid UTF-8 and unpaired escaped surrogates rather
than accepting Go's replacement decoding. Ordinary escapes, valid surrogate
pairs and schema-valid empty strings retain their native shape semantics.
No path is resolved or read, and no cursor, limit or thread value is logged.
Shape validity does not establish a usable path, cursor, thread or effective
configuration.

Missing/null/non-object params, unknown keys, wrong types and duplicate decoded
keys emit validity=false alone, without partial companion fields. Ambiguous
envelopes, duplicate escaped aliases, wrong directions, responses, notifications,
duplicate IDs and exhausted correlation produce no new parameter metadata.
Existing generic malformed/oversized/unterminated classifications, stream
bytes, trace caps and correlation quarantine remain unchanged.

PowerShell checks exact per-method event fields, booleans, native-trace
relationships and invalid-only shapes before publication. It rejects mixed
metadata families, values, casing aliases and duplicate fields. The observer
tests exercise the whole vocabulary and pinned facts, both parameter shapes,
type boundaries, Unicode, nested canaries, both directions, trace validity,
correlation, malformed input, and exact forwarding. The existing narrow startup
method regression now expects only the explicitly added feature-list diagnostic
on that exact request; all other payload and response guarantees remain intact.

The self-contained
[`read_request_metadata_red_test.go`](../../build/desktop-rpc-observer/read_request_metadata_red_test.go)
can be copied alone onto `4e75e65d04f36618fd05dcd858dd19ceb3bc0a4c`.
`TestCompleteRPCVocabularyMetadataRed` must fail with
`source-declared client method missing from complete vocabulary`;
`TestReadRequestShapesMetadataRed` must fail with
`read-request shape boolean missing or incorrect`.
These are test definitions, not an executed verification record. Formatting,
tests, vet, race checks and real Windows observation need exact-head CI evidence.

No production `config/read` or `experimentalFeature/list` route is introduced
by this diagnostic. No auth/config result body, private identifier, credential,
native trace carrier or raw error is logged. The vocabulary/shape diagnostic itself does not alter launch opt-ins or
establish successful Desktop initialization, equivalent backend configuration,
offline authentication or service acceptance. The separately reviewed CI mode
below has its own explicit admission and result gates.

### Disposable no-auth status CI gate

The signed Desktop observation at
[4e75e65d](https://github.com/breakingtime126/magpie/actions/runs/42226093994)
established valid native trace envelopes, successful fresh dual
`configRequirements/read` replies, and actual `getAuthStatus` requests with
`includeToken:true` and `refreshToken:false`. It also showed unsupported
`config/read` and `experimentalFeature/list`. A method-only observation is
not a real-account or model-usage test.

Only the already guarded `-EnableReviewedLaunchCompatibility` disposable
GitHub profile selects
`auth_status:"primary-no-auth-v0.167.0-alpha.3.2"`. Baseline, startup-only and
default configurations do not select it. Existing repository/branch/hosted-VM,
signed Desktop package/version/hash and both exact backend digest gates remain
unchanged. Both homes are freshly created, no credential environment is
inherited, and both native providers use `requires_openai_auth=false` with
loopback discard endpoints. No user account or keyring is imported.

The bridge forwards the actual permitted params to the primary only, including
`includeToken:true`; it does not rewrite the request to false. Explicit
refresh is rejected before dispatch. Success is accepted only when the actual
native result is exactly `authMethod:null, authToken:null,
requiresOpenaiAuth:false`. A token, any extra/malformed field or native error
is a failure, never converted into this triple. See the
[bridge contract](app-server-bridge.md#exact-0167-ci-only-no-auth-status) for admission, pinning and envelope
checks.

This matches the official handler's genuine no-auth-provider branch,
[before token extraction](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server/src/request_processors/account_processor.rs#L1097-L1123).
It means that this fixture's provider does not require OpenAI authentication;
it does not mean that a user is logged in. The bridge cannot certify that an
arbitrary backend is credential-free. A response validator prevents onward
token relay, not earlier receipt if someone misuses the mode with credentials.
Native initialization and config refresh can access auth/config sources before
this call. This CI mode is not authorization or guidance to use real accounts,
credential stores, external auth commands or production services.

The runtime gate consumes the full already-sanitized metadata snapshots, never
auth result bodies. It requires every observed status request to have a
correlated validated response, including at least one actual
`includeToken:true` request. Errors, outstanding replies, reused IDs or
correlation saturation fail the diagnostic. The report contains counts only.
The evidence depends on the production exact-result validator and describes
only the observed window, not complete process lifetime or future requests.
Offline fixtures cover the profile's default-off boundaries and these
success/failure correlations. Exact-head CI must establish execution results.

The no-auth mode alone leaves config read unsupported: two fixed native
providers do not produce one identical effective config, and native config
serialization can contain provider secrets even without raw layers. The
separately selected bridge-owned routing view below does not read or merge those
native configurations, invent shared defaults, or weaken policy to make Desktop
continue.

### Genuine feature-page comparison CI gate

The subsequent signed Desktop
[run at 2ab7bcd9](https://github.com/breakingtime126/magpie/actions/runs/42233630209)
observed nine correlated successful `getAuthStatus` exchanges, six preserving
`includeToken:true`. Its actual `experimentalFeature/list` request had valid
native trace and parameter metadata, a non-null uint32 limit, absent/null cursor,
and absent/null thread scope. That feature request was still unsupported at
that source revision. These are observed request shapes, not a successful
feature page or usable GUI.

Only the same explicitly selected `-EnableReviewedLaunchCompatibility` profile
now adds `experimental_feature_list:"compare-v0.167.0-alpha.3.2"`. Baseline,
startup-only and default configurations remain unchanged. The existing exact
signed Desktop `26.1103.3517.0` package gate, backend
`0.167.0-alpha.3.2` version and both native executable digest pins still apply.
There are no additional diagnostic RPCs: the observer forwards Desktop's actual
requests unchanged.

Each accepted unscoped request obtains a fresh native page from both children.
The bridge preserves cursor and limit, including subsequent cursor-present
pages, and succeeds only when both entire validated native pages match. It does
not merge a feature catalog, replace a native error with an empty page, invent
shared flags, return a primary-only page, or enable features. A non-null
`threadId` remains unsupported. See the
[bridge reference](app-server-bridge.md) for the production admission, page
validation and comparison contract.

`Get-DesktopFeatureListEvidence` consumes the full already-sanitized
`$allEvents` snapshot, not the 256-record report preview or raw RPC bodies.
It requires actual, valid unscoped requests and correlated successful replies,
including at least one limit-present request. It checks native boolean shape
metadata, valid optional trace metadata and per-process correlation. Errors,
missing flags, reused correlations, duplicate/saturated correlation records
from any method, unmatched replies and outstanding requests fail the gate.
Cursor-present requests remain admissible and must have their own replies.
Only request/reply counts and limit/cursor-presence counts are reported, never
page bodies, cursor values, numeric limit values, thread IDs or trace carriers.

Observer success alone cannot independently establish equal native pages. These
counts rely on the enabled production whole-page validator and cover only the
observed snapshot, not the complete process lifetime or future requests.
Offline fixtures exercise multiple processes, out-of-order replies, optional
trace, pagination presence, invalid/missing shapes, wrong methods/directions/IDs,
native errors, ambiguous correlation and failures after the preview cutoff.
Existing auth evidence semantics and shared sanitizers are unchanged. New
fixtures and actual feature success require exact-head CI evidence; the earlier
run above is not verification of the new mode.

The same two fresh empty homes, no inherited credential environment, no imported
account/keyring and loopback discard model endpoints remain in force. This is
genuine read-only feature-page compatibility in the disposable fixture, not a
real-account or inference test, and it makes no zero-egress claim.
This feature-only mode does not add config, project or filesystem routes.
The separate bridge-owned routing view below now handles only its exact global
`config/read` shape; `project/list` and `fs/readFile` remain unsupported.
Different native provider configurations are never replaced with invented
shared configuration. GUI usability and complete Desktop compatibility remain
unverified.

### Optional full-file read-caller syntax metadata

The optional runtime pass is currently **not run**. The earlier package pass
exhausted its node limit, so repeating it supplies no new evidence for this
control-plane run. The cold-start report records
`status: "not-run", reason: "prior_node_limit_no_new_evidence"`. The original
mandatory public-plugin inspection and signed-MSIX provenance pass still run
under `-InspectStaticLauncher`. Source/parser/schema regressions for the optional
inspector remain in CI.

When invoked separately for source inspection, a captured Node process can run
[`inspect-desktop-read-callers.mjs`](../../build/inspect-desktop-read-callers.mjs)
after the existing public catalog has passed its original signed-MSIX provenance
check. It receives only the verified installation root and that independently
verified ASAR digest. The reader requires the installed archive to match before
analyzing selected complete `.vite/build/*.js` files. It neither executes
package code nor reads any target of a `fs/readFile` expression.

This optional pass has separate limits: 16 MiB per source, 32 MiB combined
source bytes, 2,048 selected files, four million parser tokens, two million
allocated nodes and two million visited nodes, depth 512, 30 seconds of shared
analysis time, 128 calls across all sources and a 64 KiB report. A separate
Node heap cap is 1,024 MiB and the process timeout is 60 seconds. The original
catalog, parser and observer budgets are unchanged. Both streams are captured;
stderr and raw exceptions are discarded. Timeout, unexpected output, invalid
metadata or a failure to establish provenance is unavailable evidence, never
a finding that there are no callers. Optional failures do not bypass earlier
mandatory checks or change Desktop launch/RPC behavior.

PowerShell validates canonical JSON, exact schemas, fixed method/field/marker
vocabularies, integer bounds, aggregate call limits, distinct ordered source
references and offsets, proof flags fixed to false, and the trusted archive
digest. It publishes only a sanitized projection in
`static-read-callers-metadata.json` and the report: public source digests with
ordinal references, bounded counts/offsets, fixed classifications and public
response-field components. Source filenames, source text, actual paths,
identifiers, values and raw errors are not published.

The only candidate methods are `config/read`, `experimentalFeature/list`,
`project/list` and `fs/readFile`. A literal first argument is a positive
syntax fact, not an established RPC transport binding. Response-member facts
cover immediate awaits and restricted same-block const references only; wrapper
calls, aliases, callbacks and broader consumer flow remain unresolved. Mutations
can affect values, so even a recognized response-member expression does not prove
its runtime value. Path classifications and fixed filename markers establish
neither an actual resolved path/root nor authority to read one. The explicit
`runtimeBindingProven`, `responseValueProven` and `pathAuthorityProven` flags
remain false.

A wholly parsed source may contribute positive facts. Syntax or local
analysis limits omit that whole source and mark the package report partial;
global bounds, read/provenance failures and unknown failures discard the whole
report. Complete means complete parsing within the selected source scope, not
complete consumer coverage. Zero reported facts are never proof of absence.
Offline PowerShell fixtures cover schema, bounds, digest mismatch, incomplete
coverage, fixed false proof flags, output framing/status, redaction and aggregate
call limits. Execution of the fixtures requires exact-head CI. The optional full-file
package pass remains uninvoked for the combined control-plane check. This diagnostic enables no auth/config route,
changes no defaults and introduces no additional Desktop launch or request.


### Bridge-owned routing view in the combined CI profile

Only `-EnableReviewedLaunchCompatibility`, with its existing separate startup
opt-in, selects `control_plane: "routing-v0.167.0-alpha.3.2"`. Default, baseline
and startup-only configurations do not add this mode. The same repository,
branch, hosted-runner, signed Desktop version/package digest and both native
executable version/digest checks remain mandatory. The helper configuration
adds a fresh, separate `control_home` outside the Desktop workspace and all
four native HOME/CODEX_HOME directories. It is actual bridge-owned state, not a
copy of native config. No synthetic `config.toml`, credential or origin record
is written there.

The production bridge view is intentionally narrower than native effective
config. It accepts global `config/read` with `includeLayers` absent, false or
true and no non-null cwd. It advertises the logical frontend provider
`magpie_bridge` and exposes only bridge-owned routing facts and its explicit
extension. A layers-enabled read returns one actual service-owned
`sessionFlags` layer with `name`, `version` and `config`, not a user or system
file layer. The sole layer's config equals the effective owned config, and each
primitive config leaf has an origin naming that layer and version. Container
objects have no origin entry. Layers-disabled reads omit `layers` but return
the same config and origins.

The version follows the pinned native fingerprint contract: `sha256:` plus the
SHA-256 of recursively sorted compact UTF-8 JSON for the layer config. Repeated
reads of the same snapshot retain the version. A successful full model-catalog
refresh publishes the routing default and its complete layer snapshot together;
failed or incomplete refreshes retain the previous snapshot. The default used
by omitted-model thread creation comes from that same snapshot. A read selects
one complete current snapshot only after both fresh native
`configRequirements/read` results genuinely contain `requirements:null`.
Native config is never read or merged, and no authentication, approval, policy
or sandbox setting is manufactured. No config file is created for this layer.
Config writes, non-null cwd reads, project and filesystem routes remain
unsupported. Logical provider values on the supported request and response paths
are translated only after ownership checks. See
[`control_plane_167.go`](../../internal/appserverbridge/control_plane_167.go)
and the [bridge contract](app-server-bridge.md) for the pinned native sources,
exact production schema, snapshot publication and failure behavior.

`Get-DesktopConfigReadEvidence` consumes the full sanitized `$allEvents`
snapshot, not the 256-record preview. Both global layers-enabled and
layers-disabled request shapes need correlated successful responses. The
reviewed runtime explicitly selects `-RequireLayers`: at least one actual
layers-enabled request must succeed, so a layers-disabled-only capture cannot
claim layer coverage. Offline callers without that switch can still validate
layers-disabled evidence. Invalid/missing native boolean metadata, non-null
cwd, errors, outstanding or unmatched replies, cross-process mismatches, reused
correlations, and any duplicate-ID or saturation record fail the gate.

The report retains only total request/validated-response counts and their
layers-enabled/layers-disabled splits. Replies inherit their request's flag
through the process-and-correlation key, even when interleaved. Result/config
bodies, cwd values, origin details, version hashes and trace carriers are never
inspected by this evidence helper or published. Counts rely on the production
view and its requirement validator; they are not independent proof of the
layer's contents, native configuration equivalence or Desktop consumer support.
Offline fixtures retain the previous failure guards and cover true-flag success,
errors, cwd, missing/invalid metadata, unknown fields, reused correlations,
unfinished replies, required true coverage and requests beyond the preview.
Their definitions are not an execution result.

Before strict auth, feature and config checks, `Get-DesktopRPCErrorEvidence`
counts all observed RPC errors in the full sanitized snapshot, split into
unsupported-method and other errors. It validates the fixed error labels,
optional correlation metadata, bounded integers and exact metadata-only shape
against the existing public method vocabulary. Its fixed `threadListErrors`
buckets count only correlated bridge-to-client `thread/list` errors by the
reviewed rejection classes below, plus `unknown` and `other`; existing total
counters are unchanged. It retains no request ID, code, message or result body. The report explicitly labels the selected scope as
auth status, feature pages and global owned config only, with
`fullDesktopCompatibility: "not-established"`. A green selected-profile
capture does not establish support for project, filesystem, config-write or
other unimplemented APIs. Zero observed errors is not full-lifecycle evidence.
Existing failures remain failures; unsupported consumers are not replaced with
guessed fallback config or another Desktop launch.

### Project and file-target classification in the same capture

The same single Desktop capture adds request-only metadata for `project/list`
and `fs/readFile`. It does not route either method or read a target.
[The observer contract](../../build/desktop-rpc-observer/read-target-contract.md)
defines the pinned native request oracle, complete enums, strict JSON/Unicode
rules, and lexical comparison limits.

PowerShell accepts only each method's exact shape, native booleans and fixed
labels under the existing envelope, correlation and optional trace guards:

- `projectListParamsValid`, then only when true cursor/limit-presence booleans
  and fixed sort-key/direction labels. A direction requires a sort key.
- `fsReadParamsShapeValid`, then only when true fixed path-form, basename and
  lexical-root labels. Shape validity is not native path validity or read
  authority. No path, arbitrary basename, cursor, limit value, hash or raw error
  is published.
- Invalid parameter shapes expose only false validity. Invalid trace exposes
  only the existing trace-presence/validity pair. Responses, notifications,
  wrong-direction and ambiguous-correlation records carry no target metadata.

The private `MAGPIE_CI_READ_TARGET_ROOTS` object is serialized from the
harness-created primary/secondary HOME and CODEX_HOME directories, disposable
workspace, verified installed package root, and the separate control home when
the control-plane profile is selected. Nothing is inferred from user
environment, requests or native config. The variable is passed only to the
observer launch and stripped before child launch. Its values are neither
printed nor hashed. Exact key/type/size and conservative Windows spelling
checks fail closed. These labels describe lexical relationships only; no
symlink, junction, file contents or physical containment is inspected. In
particular, `control_home` does not authorize reading a config file there.

Existing public-catalog/provenance tests remain intact. New synthetic
PowerShell fixtures cover all target enums, sort relationships, trace,
correlation, missing/wrong-cased/duplicate fields, invalid-only shapes,
redaction, explicit root provenance and root bounds. The source contract retains
one runtime declaration seam and one explicit live-fixture selector for welcome
or read-only inspection. Its four flag combinations must select exactly one
owned workspace; both enabled still fail the separate inspection option gate.
Source mutations cover welcome-only drift, unvalidated roots and duplicated
declarations. Their source definitions are not a claim that execution passed.

### Separate native end-to-end slice, one Windows job

The same job can invoke
[`build/control-plane-native/invoke.ps1`](../../build/control-plane-native/invoke.ps1)
once after a safely validated Desktop capture and confirmed activation-process
exit. A later strict Desktop auth, feature or config compatibility failure does
not suppress this independent native slice. It still remains a Desktop failure;
there is no synthetic replacement for Desktop evidence and no extra Desktop
launch. Read-only winapp inspection is explicitly excluded from this native-turn
slice, even with valid capture and cleanup; it reports `not-run` instead. The
profile source contract retains one guarded invocation after cleanup and tests
all 32 combinations of profile, inspection, capture, exit and cleanup flags.
Negative fixtures reject removal of the inspection exclusion or inner safety
decision without launching native processes.

The explicit `captureValidated` flag starts false. It becomes true only after
the signed-package, visible-window and identified-child checks and the complete
bounded trace-validation loop have succeeded: every collected snapshot is
canonical, sanitized, ordered, tied to its observed process/public catalog and
published from the validated in-memory records. The flag is set before the
strict compatibility evidence helpers. It does not derive from the 256-record
preview or from mere presence of `$allEvents`, and it does not imply
`completeLifecycleEvidence` or successful Desktop compatibility. Missing GUI
or child identity, invalid trace metadata or failed snapshot validation leave
the native stage not-run. Existing trace budgets and payload/redaction
boundaries are unchanged.

The reviewed profile retains the original process object and OS handle returned
by the activation launch, recording its exact full-precision creation time at
that point. Cleanup uses that same object and rechecks its PID, captured creation
time and verified package identity, including the recorded package observation,
before signaling it. WMI CreationDate remains report metadata and is never used
as a cleanup equality key; no timestamp tolerance is introduced. It requests
window close and waits up to five seconds, then, if
needed, terminates only that same handle-pinned process and waits another five
seconds. Missing or mismatched identity, process-operation errors without a
confirmed exit, or an unconfirmed survivor keep the native stage not-run. The
explicit `desktopActivationExited` boolean gates invocation independently
of the cleanup-error report. No reused PID, executable name or unverified
process is targeted. This is activation-process exit evidence only, not a claim
that all Desktop package descendants exited. Baseline and startup-only cleanup
behavior is unchanged.

`Get-DesktopNativeRunDecision` requires the existing explicit profile,
`captureValidated`, confirmed activation exit and no cleanup error. It does not
test the Desktop compatibility exit code. Before native execution the report
copies the original Desktop status, stage, reason and exit code into
`desktopOutcome`. `Get-DesktopCombinedOutcome` preserves all four original
fields exactly whenever Desktop already failed, including its nonzero exit
code. A native pass cannot turn that job green, and a native failure cannot
replace the original Desktop failure. The separately validated
`nativeControlPlane` summary reports the native result in either case. When
Desktop passed, native failure or a missing safety precondition still fails
the combined job. Only both completed successes produce a successful combined
result.

Synthetic PowerShell fixtures cover the eligibility truth table, both-success,
Desktop-failure/native-success, two distinct failures, invalid capture and
unknown cleanup not-run cases, immutable original outcomes, and the runtime
AST seams. They also verify that capture validity is set after full snapshot
sanitization and before strict compatibility helpers. These changes do not
broaden any auth, feature, config or filesystem route.

The workflow passes the exact commit-built adapter/helper and the already
verified official resource `codex.exe`, plus the official pinned model catalog.
The catalog source is
[`c40729ea…/codex-rs/models-manager/models.json`](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/models-manager/models.json),
Git blob `489ebe3f2483348641a16160ce8491dd077eabca`; the helper independently
checks native version/hash and catalog identity. Its temporary parent and
helper environment are fresh siblings, never borrowed Desktop/native state.
The wrapper clears inherited environment, keeps only its explicit OS/runtime
allowlist, captures bounded output, suppresses raw stderr, enforces a hard
deadline and uses an owned process-tree supervisor for cleanup.

The helper performs config-read, model-list and resolved routing checks, then
two projectless native threads with two text turns each against separate
loopback synthetic Responses fixtures. At each of the initial, resolved and
final config phases it reads without layers, with layers, then with layers
again. It checks exact config/layer equality, primitive-leaf origins, native
content fingerprint, stable rereads and the single initial-to-resolved version
change. It also checks ownership rejection and final config consistency, then
observes adapter exit and performs owned process-tree cleanup. The
`adapter_clean_exit` flag means the adapter exited with status zero;
it does not prove that every native descendant exited on its own. Owned cleanup
may forcibly stop remaining descendants through the kill-on-close job.
`cleanup_complete` records completion of that cleanup, not graceful descendant
shutdown. The main report accepts
only its exact pinned success schema: `config_reads=9`, `layer_reads=6`,
`stable_layer_rereads=3` and `layer_version_changes=1`, plus
`thread_list_queries=2`, `thread_list_pages=4`, `thread_list_items=4`, two catalog
models, two threads, four turns, four assistant messages, two thread reads, two
ownership rejections, two fixture requests per backend, zero fixture errors,
zero unexpected server requests and all verification/cleanup flags true.
The two list queries use limit one and require both known native IDs exactly
once per query, one item per page and a final null cursor. The native stage
`thread_list` and failure `thread_list_invalid` are fixed sanitized labels.
Failures never become empty or successful reports. Only fixed labels, exact
public pin and counts/booleans are retained, not RPC bodies, IDs, prompts,
answers, paths, credential data or raw errors.

This is explicitly `synthetic-loopback-only` evidence from the same compiled
adapter and actual official native app servers. It is separate from actual
Desktop evidence. It proves neither real-service acceptance nor real-account,
tool, corporate-policy or complete GUI compatibility, and makes no zero-egress
claim. The existing no-auth and genuine feature-page gates keep their previous
semantics.

The [pre-change run at `66e39e44`](https://github.com/breakingtime126/magpie/actions/runs/42261517920)
validated 297 Desktop records while previewing 256, but failed on the actual
`includeLayers:true` request. Its separate native slice passed the older
three-config-read fixture. That run does not verify the new layer support.

Source preparation used GitHub connectors without an available execution
environment. PowerShell parsing/fixtures, Go tests/vet/race/build and the
combined real Windows run were not executed during authoring. Exact-head CI
must report those results before this change is called verified.



### Thread-list filter shapes and exact rejection classes

The disposable observer adds five JSON-type labels on unambiguous client
`thread/list` requests: `threadListProjectIdShape`, `threadListParentIdShape`,
`threadListAncestorIdShape`, `threadListAttachedIdShape`, and
`threadListSectionIdShape`. Each is exactly
`absent`, `null`, `string`, or `invalid`; no string contents, identifiers,
provider names, cursor, search term, path or arbitrary field name are retained.
This is not whole-request validity or permission to route a filter. The
[pinned official ThreadListParams](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs#L1428-L1511)
distinguishes omission of `projectId` (every project) from explicit null
(unassigned threads), and a string selects a project. Null must not be silently
discarded or described as a harmless default. The other three labels describe
`parentThreadId`, `ancestorThreadId`, and `attachedToThreadId` only by JSON type,
not valid identities or supported ownership semantics. `sectionId` omission
means every section, null means unsectioned, and a string selects a section.

The sixth field, `threadListSortKey`, reports only `absent`, `null`, `created_at`,
`updated_at`, `recency_at`, `section_position`, `other-string`, or `invalid`,
from the [pinned ThreadSortKey enum](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs#L1567-L1575).
Known JSON-escaped enum spellings normalize to their fixed label. All other
valid strings, including empty, prefixed, suffixed or differently cased values,
share `other-string`; invalid Unicode and non-string/non-null values are
`invalid`. No arbitrary sort text is retained. These independent observations
do not enforce a relationship between sort and section, or enable sorting.
The [native section sorter](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/thread-store/src/local/list_threads.rs#L122-L138)
requires a non-null section filter and SQLite, and the
[public Thread result](https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread_data.rs#L205-L259)
does not expose the section position rank. This metadata does not establish
that any previously observed sort failure used `section_position`, nor does
it justify inventing or reconstructing that rank across backends.

[`thread_list_metadata.go`](../../build/desktop-rpc-observer/thread_list_metadata.go)
reuses the existing bounded reader, rejecting duplicate top-level envelope and
parameter keys (including JSON-escaped aliases), and checks native JSON-string
Unicode only for the five selected identifier fields and sortKey. An ambiguous
envelope or invalid trace emits none of the six fields, since its request
identity is not trustworthy. A non-object, missing, top-level-duplicate-key or
more-than-18-field parameter object emits six `invalid` labels. The bound is
the pinned 17 fields plus the bridge's legacy `currentAccountOnly`. Unrelated
parameter values remain opaque; the labels do not validate the full schema.
Malformed and oversized transport lines retain their existing fixed
`unrecognized`/`oversized` labels. Existing 64 KiB line/trace budgets and pending
request limits are unchanged.

For correlated bridge-to-client `thread/list` errors with integer code -32602,
strict envelope/error parsing recognizes only these exact existing bridge
messages, with no prefix, suffix, case or arbitrary-name matches:

- `unsupported thread/list field: projectId` → `thread_list_project_id`
- `unsupported thread/list field: parentThreadId` → `thread_list_parent_id`
- `unsupported thread/list field: ancestorThreadId` → `thread_list_ancestor_id`
- `unsupported thread/list field: attachedToThreadId` → `thread_list_attached_id`
- `unsupported thread sort key` → `thread_list_sort_key`
- `unsupported thread sort direction` → `thread_list_sort_direction`
- `unsupported logical provider filter` → `thread_list_provider_filter`
- `invalid bridge cursor` → `thread_list_cursor`

These strings come from the existing bridge's
[`validateParams`](../../internal/appserverbridge/protocol.go) and
[router list checks](../../internal/appserverbridge/router.go). Duplicate or
ambiguous top-level error/envelope fields, a wrong code, unknown message, unmatched reply,
wrong method or wrong direction cannot acquire a new rejection class. Neither
raw errors nor error data enter the trace. The PowerShell sanitizer enforces
exact string labels, all six metadata fields together, request/error scope and
integer error code before publication, including records beyond the preview.

The error buckets are diagnostic evidence only, collected before the existing
selected auth/feature/config gates. They do not waive those gates, establish
full Desktop compatibility, change production routes or select a backend.
Observer forwarding remains byte-for-byte unchanged and adds no filesystem,
authentication, account, network or diagnostic RPC access. The next already
required CI capture supplies evidence; this metadata change does not establish
the cause of the earlier 21 unclassified failures or require another launch.

The portable offline
[`thread_list_metadata_red_test.go`](../../build/desktop-rpc-observer/thread_list_metadata_red_test.go)
uses only observer APIs present at `09b526eb`. Copying that test file alone onto
that baseline and running `TestThreadListFilterShapesObserved` must fail with
`thread/list filter shape metadata missing or incorrect`, rather than a compile
failure. `TestThreadListRejectionClassesObserved` covers exact-message evidence;
neighbor tests cover prefix/suffix/case canaries, malformed and duplicate JSON,
18/19-field bounds, null-versus-omission, string validity, method/direction scope,
unchanged wire bytes and oversized lines. The existing CI profile fixtures cover
fixed counters, metadata types, scope and unknown-field rejection. These test
definitions are not a claim that the local environment executed Go or PowerShell.


The portable
[`thread_list_sort_section_red_test.go`](../../build/desktop-rpc-observer/thread_list_sort_section_red_test.go)
uses only APIs already present at `02600624b069bd5a00a339953eea5b8348e5a467`.
Copying it alone to that baseline and running
`TestThreadListSortSectionObserved` must fail behaviorally with
`thread/list sort and section metadata missing or incorrect`. It covers the
cross-product of the fixed sort observations and section shapes, escaped enum
and field spellings, malformed Unicode/types, duplicate keys, bounded parsing,
request scope and byte-identical request/response forwarding. PowerShell
fixtures independently check exact enum/type/shape acceptance and rejection.
The additional fields ride the already-needed combined CI capture; they add
no diagnostic-only launch or account/filesystem access. Definitions and source
review are not execution evidence.

### Initial welcome snapshot observation

The disposable [welcome helper](../../build/desktop-welcome-step.ps1) adds one
nullable, count-only summary to an initial classified zero-action stop. It
reuses the last already-owned ControlView snapshot and counts existing reads;
it makes no additional UIA call and does not change the strict action classifier,
shell-readiness predicate, deadlines, retries or production bridge APIs. The
[live contract](../../build/desktop-live-welcome.md) defines the fixed fields,
schema validation and stop condition. Unknown names and types never enter the
report. Normalized heading counts are diagnostic only, never action authority.
The parent continues to report a failed live slice. If one capture does not
identify a concrete harmless mismatch for separate review, stop this lane
rather than repeating diagnostics or expanding onboarding behavior.


## One-off official winapp read-only comparison

### Separate package identity diagnostic

`package-identity-only-v1` is a distinct static diagnostic, not a UI inspection.
For a push, the commit message must contain both `[package-identity-only-v1]`
and the existing `[winapp-inspection-v0.7.1]` exclusion marker, which keeps the
older cold-start/native-entry runtime jobs skipped. A package marker without
the exclusion marker fails the policy workflow's pre-checkout gate. An explicit
dispatch can instead select `package-identity-only-v1` with the exact expected
commit SHA. Disabled/default dispatch still fails before checkout.

The workflow selects only the package switch, never welcome or winapp mode.
There is no winapp download/setup, package installation, backend version call,
Desktop activation, UIA read or native-turn slice. The existing download/signature
gate is reused. Static reads additionally require valid Authenticode status and
the expected publisher subject, then rehash the original package while holding
it read-only against writes. Existing canonical ZIP-index and bounded stream
helpers read only `AppxManifest.xml`, `app/resources/codex.exe` and
`app/resources/app.asar`; nothing is extracted or executed.

Published metadata is limited to fixed identity/version/architecture/publisher
fields, sizes and SHA-256 digests. XML DTDs and external resolution are prohibited.
Bounds are 4 GiB for the input, 256 KiB for the manifest, 512 MiB for the backend
and 2 GiB for ASAR, with the existing shared 120-second cooperative streaming
budget. No manifest body, ASAR source or executable bytes are published.

`packageIdentity.status=observed` means only those static fields were read.
`runtimeAccepted` remains false, the overall job stops with exit 1, and the trusted
package/native pins stay unchanged. If the harness can prove no activation,
registration, observer, fixture or tool was started, its Desktop cleanup label is
`not-started`; this never sets activation-exit confirmation to true. The ordinary
activation cleanup path and existing winapp mode remain unchanged.

### HWND-requested UI diagnostic

A push to the existing experimental branch whose head commit message contains
`[winapp-inspection-v0.7.1]`, without either independent native-job marker below,
selects `EnableReadOnlyWinAppInspection` in the
policy-trace job. It is mutually exclusive with the welcome action helper and
native-entry cases. The older cold-start/native-entry jobs are skipped for that
marked push, not reported as passed. Existing policy-trace unit, profile,
redaction and bridge build checks remain. Unmarked behavior is unchanged.

An explicit dispatch of this policy-trace workflow also supports the diagnostic:
select `inspection_mode=winapp-inspection-v0.7.1` and supply `expected_sha` equal
to the workflow's full 40-character lowercase `github.sha`. The mode defaults
to `disabled`. The first observer-unit step checks these values before checkout,
using environment bindings rather than interpolating inputs into shell source.
A default, unknown-mode, missing-SHA or mismatched-SHA dispatch fails that step;
the dependent Windows job cannot run or fall through to welcome actions.
Dispatch selects only this existing workflow. Marked and unmarked push behavior,
repository/branch guards, permissions and all later checks are unchanged.

[desktop-winapp-inspect.ps1](../../build/desktop-winapp-inspect.ps1) downloads the
official Microsoft [winappCli v0.7.1 release](https://github.com/microsoft/winappCli/releases/tag/v0.7.1)
into fresh private scratch. Before any tool execution it requires the x64 ZIP's
published size 99,013,508 and SHA-256
`d925d1e32cdc320b6d271f653fd2cd3c05b5d7558deb20d1b548bead77900fcf`, then a valid
embedded Authenticode signature with Microsoft Corporation as signer
organization on `winapp.exe`. It records the actual executable digest and signer
thumbprint, and verifies all extracted file digests and the executable signature
again before and after inspection. An unavailable/invalid signature stops, with
no unsigned or alternate-tool fallback. The tool's source license is MIT; no
installer, init, update, run, debug, pack, global PATH or system-setting command
is used. Zip paths and expanded size are bounded before extraction.

The existing fresh signed Desktop package, native backend hashes, empty-account
checks, reviewed startup/profile gates, loopback fixture setup, held activation
process, screenshot and cleanup remain. The only UI command is exactly
`ui inspect -w <verified HWND> --depth 8 --json`. Held process start time,
PID, session, package, executable and current requested-window ownership are
rechecked before and after the invocation. This requests the existing HWND; it
is **not** strict proof of the inspected root or PID exclusion. The pinned
source can fall back to a same-PID root, and an unresolved HWND reuse race
remains. The explicit HWND avoids `-a PID`'s intentional extra owned-dialog
walk. Depth 8 bounds rendered output only: AutomationId promotion can read
full descendants. No subsequent input action is authorized by the result.

The shared bounded child supervisor imposes 30 seconds and 256 KiB per output
stream. Raw JSON is reduced only in private memory, and stderr is discarded.
One invocation is attempted, with no retry on missing, malformed, unavailable,
nonzero, timed-out, oversized, changed-identity or uncertain output. Cleanup
explicitly checks held-child exit; unconfirmed exit/disposal or constructor
failure after a possible start cannot succeed. The shared supervisor constructor
validates bounds before starting and transactionally stops/reaps only its own
held Process on any later constructor exception, disposing partial resources.
A fixture injects failure after stream readers start but before timer creation
and independently holds that same child to verify exit. Process termination/drain may
need up to five/two additional seconds; this is not extra inspection time.

Only fixed counts, booleans and enums enter `report.nativeInspection`: exact
and whitespace-normalized heading counts, the three target-label counts and
invokable-label counts, node/truncation counts, rendered depth, required-target
presence and UIA offscreen-based visibility, protected-label counts and supervisor outcome.
The visibility enum reflects UIA properties, not independent pixel validation. Missing target names
are `stopped/required_targets_missing`; offscreen targets are
`stopped/required_targets_offscreen`, never observed visible controls. Login,
permission, authorization, account, payment, terms, privacy and security markers
stop without an action. These fixed marker checks are not a complete semantic
classification of arbitrary UI. Unknown output schema also stops.

`WINAPP_UI_LOCK_DIRECTORY` and `WINAPP_CLI_CACHE_DIRECTORY` are set only in the
cleared child environment, to new nonexistent local scratch paths. Existing
runner profile directories are neither overridden nor repaired. The official
tool creates its own restricted coordination directories there. No
`WINAPP_UI_WORKFLOW_ID` is inherited. Exact `--json` bypasses first-run notice
and update checking; `WINAPP_CLI_TELEMETRY_OPTOUT=1` disables upload-tagged
telemetry, while local ETW and named session-event bookkeeping remain. This is
not a zero-egress or filesystem-read-only claim.

The original screenshot is retained; an after-inspection screenshot is taken
only when the tool result and requested-window identity checks passed. It uses
the existing screen-pixel capture, never winapp's focus/restore screenshot
command. Pixels may be occluded and require review. `inspectionFixture` reports
only the existing loopback helper lifetime; no welcome result is fabricated.
Independent native four-turn acceptance is `not-run/read_only_inspection_only`.
Earlier .NET UIA counts are historical, not a simultaneous same-window sample.
A completed observation is not onboarding, model selection or Desktop chat.

Offline verification: `pwsh -NoProfile -File build/desktop-winapp-inspect.tests.ps1`.
The `-BreakRedaction` mutation must fail exactly the raw-UI-text exclusion
assertion before the normal suite passes. Fixtures are explicitly
schema-derived synthetic data, not captured official-tool output. They exercise
option/signature/schema/label/privacy rules, injected cleanup uncertainty, and
real bounded child stdout/stderr/timeout behavior without downloading or
activating Desktop. Actual CLI serialization/signature/runtime compatibility
remains to be checked by the single marked Windows job. Focused checks never
replace the retained repository checks or actual Windows run.

### Independent original-native UI observation

A push with both `[native-ui-inspection-v1]` and
`[winapp-inspection-v0.7.1]` selects the independent `native-original-ui`
Windows job. It has no bridge-regression job dependency and runs only relevant
reader, package, registration, process-cleanup and mode-safety fixtures first.
The old observed-policy runtime job is skipped for either this marker or
`[native-contract-review-v1]`; the common winapp marker also keeps the other
old Desktop runtime lanes skipped. The native-contract evidence job may run
alongside this one, but neither job's success implies success of the other.
Package-only and native-UI selection cannot be combined.

`EnableNativeReadOnlyInspection` requires `Mode=baseline` and rejects every
bridge/startup/welcome/static-launcher/package-only/native-entry opt-in. It
checks for routing overrides and account environment/configuration, retains
the ordinary no-auth/no-existing-registration preflight, and never sets a
bridge override, starts synthetic fixtures or sends a service turn.

This original-app lane accepts the latest package only after the existing
signtool gate and `Valid` Authenticode with the expected Store publisher,
bounded `OpenAI.Codex`/x64 manifest identity, and one expected
`app/ChatGPT.exe` full-trust application entry. It records the actual package,
native payload and ASAR identities before installation. It does not add a
bridge-approved version or native hash: every existing bridge compatibility
pin remains unchanged. Package updates are observations, not protocol approval.

The existing licensed install, bounded registration wait and interactive-session
checks precede one AUMID activation. Its newly created Process object is held
and checked for creation time, package and session before the original owned
window is inspected. AUMID activation is not evidence of a literal Start/taskbar
click, persistent routing or bridge usability.

The same pinned Microsoft winapp executable gets one HWND-requested read-only
call, with the existing 30-second and 256-KiB-per-stream bounds, output-depth
limit, private raw output, and disclosed fallback/HWND races. A nonempty valid
native tree may be observed even when old welcome labels are absent. The fixed
counts and `requiredTargetStatus` remain explicit; this proves neither usable
controls nor clickability. Login, permission/protected and error labels stop
without any input. The initial screenshot can still document such a stop.

Only the exact retained activation process is closed/terminated during bounded
cleanup. Unknown exit or identity never becomes success, and whole-family exit
is not claimed. The native-only footer exits before independent synthetic turns,
reports bridge acceptance as `not-tested`, and accepts observation only with
confirmed activation exit and the validated after-capture. Offline fixtures are
synthetic safety tests, not evidence that the actual window read succeeded.


### Separate exact-package contract review

`build/review-desktop-contract.ps1` is a disposable, credential-free review lane,
independent of Desktop activation and the bridge regression lanes. It requires
`[native-contract-review-v1]` and the existing `[winapp-inspection-v0.7.1]` push
marker on this experimental branch. The combined workflow excludes the old
observed-policy runtime lane when this marker or the native-only UI marker is
present. The existing disabled dispatch default is unchanged.

This review alone admits the signed public 26.1103.4239.0 package SHA
`9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c`, backend SHA
`4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382`, and ASAR SHA
`fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa`. These are
review-input identities, not a change to any accepted bridge/runtime pin.
Package SHA, exact size, Windows signature, manifest identity and both entry
hashes must pass before the two exact entries are extracted into fresh scratch.
The existing bounded ZIP validators are imported as function definitions, never
by executing the cold-start harness. Nothing is installed or activated.

After shared trust and extraction, two independent evidence legs preserve
successful artifacts even if the other leg fails. The native leg runs only
`--version`, `app-server --help`, `app-server generate-json-schema --help`, then
`app-server generate-json-schema --experimental --out <new directory>`. The
actual help must expose both generation flags before generation. It does not
initialize app-server, inspect startup configuration, authenticate, or infer.
Each process has a different empty HOME/CODEX_HOME and cleared inherited
environment, using the existing `New-LiveStart` and `BoundedProcess` helpers.
A held child has a 30-second deadline, 256 KiB per stream, and explicit exit
confirmation. Fixed command labels, exit codes, timeout/overflow flags and
retained stdout byte counts are recorded; stderr is bounded/discarded by the
shared supervisor, which does not expose its byte count. The static Node readers have the same deadline and 1 MiB output
cap. An uncertain child exit stops the other leg. No command is retried.

Generated JSON is limited to 2048 files, 8 MiB each and 64 MiB total; directory
sizes are sampled while generation runs, not enforced by a filesystem quota.
All files are parsed, safe references must resolve within the generated root,
and the complete generated set including definitions is hashed and staged
before artifact publication. Safe `../` references are normalized and accepted
only when their resolved path remains inside that root. No remote schema is
fetched. Source command syntax was cross-checked against public OpenAI CLI blob
`a1462ee2a36535fba971389bf8ef8bda8ad9b8d5`; actual bundled help/schema remains
the version-specific evidence. Current public initialize/config/model/thread
schema samples were used to test reference validation, not presented as this
package's generated protocol.

The static leg uses the existing ASAR index reader without evaluating package
code. It hashes the full exact ASAR, scans only packed `.vite/build/*.js` sources
(16 MiB per file, 128 MiB total, at most 4096 files), and reports literal source
excerpts around fixed launch, startup probe, initialize-capability, auth,
config, model, thread and environment anchors. At most eight excerpts per
anchor and 1600 UTF-16 code units on each side are retained; all match counts
and truncation flags are explicit. Artifact size is capped at 1 MiB. Every
source has a path/hash and every excerpt has offsets. Missing anchors are not
success, and snippets are implementation evidence rather than proof that a
selection path ran or is a supported public configuration interface.

Only the fixed report, public help/version, validated generated schemas and
bounded public source excerpts are uploaded. Extracted executables/ASAR, raw
profiles, stderr and the downloaded MSIX stay private in the disposable VM.
`observed` means both evidence legs completed; `partial` preserves successful
artifacts without claiming full review. Neither means UI, login, model,
bridge, normal-entry or same-window success. Held-child exit does not prove
an entire descendant family exited.

Focused checks: `node --test build/review-desktop-contract.test.mjs` and
`pwsh -NoProfile -File build/review-desktop-contract.tests.ps1`. The Node fixtures
exercise preserved excerpt bytes/offsets, pin mismatch, missing/truncated
anchors, malformed/unpacked/link ASAR sources, complete local definitions,
root-contained relative references, missing/remote/escaping references,
malformed/oversized output and links. The PowerShell fixtures check exact CI
admission/identities, generated size bounds, unchanged trust-before-execute
ordering and no activation/startup probe. Synthetic checks are not runtime
evidence. Authoring local Node checks passed; PowerShell, Windows and actual
package/backend execution remain for CI at the reviewed commit.

### Reviewed 4239 bridge observation profile

The actual `native-contract-review` artifact from commit
`341298e1515298c6f8714509c78324f7f38dbe12` independently verified this input:

- Signed Desktop version: `26.1103.4239.0`.
- MSIX SHA-256: `9a5fdf536327ce471f9594b0ac6e369244ec3ebfd5e09ec5a66524575dbaa60c`.
- Native `app/resources/codex.exe` SHA-256:
  `4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382`.
- ASAR SHA-256: `fb47d6491dc3006b708cb1e14de479b882bf50f88b7daa81818f154c253266aa`.
- Actual version output: `codex-cli 0.167.0-alpha.3.2`.
- Full experimental schema: 470 files, 4,654,630 bytes; each matched its
  generated manifest hash and size. Version/help/schema commands exited zero
  within the retained limits and confirmed owned-process exit.

The unchanged version string does not prove unchanged behavior. Comparison of
21 selected old nonexperimental schemas found 16 structurally identical and
five differences consisting of experimental fields already present in old
source `openai/codex@c40729ea5cf8e947481b130c59807273eda2f8e9`. The old exporter
intentionally omitted legacy `getAuthStatus`; its absence from the new JSON
schema is not proof of removal. Actual runtime initialize, legacy no-auth,
requirements/config/catalog and response validators remain authoritative.

The bounded actual frontend excerpts support the retained Windows stdio
launch contract: `hostConfig.codex_cli_command` takes executable/arguments
precedence; `CODEX_CLI_PATH` is handled and sanitized before spawn, while
`CODEX_APP_RUNTIME_CLI_PATH` is propagated separately. Startup probing still
uses `--print-startup-config` and `--startup-client-info`. The separate sign-out
transport descriptor is not permission to invoke it. Actual initialize
construction retains experimental API, local gateway-OAuth capability,
attestation/form/extensions and notification opt-outs. This is source evidence,
not evidence that any unsupported capability was safely negotiated.

The helper-availability branch can emit either boolean for
`plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled`.
The bridge continues accepting only the previously reviewed exact `true`
leaf, not `false` or arbitrary config overrides. Refreshing credentials,
non-null requirements, non-null config cwd, unreviewed initialize fields,
and unsupported thread scopes still fail closed. No production bridge parser,
auth behavior, attestation behavior, policy projection or allowlist changes
are part of this profile.

`-EnableReviewedBuild4239` is default-off and exclusive to the existing
`observe` bridge mode with startup/launch-compatibility/read-only-inspection
opt-ins. Live welcome, original-native observation, package-only inspection
and native-entry cases are rejected when combined with it. The GitHub-hosted
repository/branch restriction remains unchanged. Before installation, the
existing signature and bounded ZIP reader must establish the exact tuple
above; any changed floating download stops. Both fixture backend records,
initial/final readiness, installed backend check and generated startup pins must then use
the selected new digest. The old profile remains the default, not a fallback
for a failed new profile.

To select one new bridge-context inspection, the substantive experimental
branch push must contain both `[bridge-ui-4239-v1]` and
`[winapp-inspection-v0.7.1]`, and no `[package-identity-only-v1]`,
`[native-ui-inspection-v1]` or `[native-contract-review-v1]` marker. The first
Linux step rejects conflicting/missing marker pairs before checkout. The
independent original-native and contract jobs explicitly exclude the 4239
marker, so an invalid mixed marker cannot rerun either diagnostic. The
Windows job depends on that step and its existing tests. Its offline Windows
checks also run the native-tree login-evidence fixtures, without activating
the original-native lane or performing input actions. Dispatch defaults
remain fail-closed; dispatch does not select the 4239 build. The common winapp
marker continues skipping the old cold-start/native-entry runtime lanes.
This does not repeat the completed original-native window observation.

The existing reader performs at most one HWND-requested inspection in the
new bridge context, with zero input actions, retained before/after ownership
checks, fixed summaries, and a screenshot. Same-PID fallback and an unresolved
HWND race remain disclosed; this is not strict root identity proof. Missing
controls, login/protected/error evidence, changed identity, timeout, overflow
or uncertain cleanup stop the diagnostic. Zero matched login names do not
prove absence of a visible login screen. Actual bridge/trace outcomes and
pixels must be reviewed separately from schema evidence and the completed
original-native sign-in-screen observation.

The independent four-turn native inference/tool-acceptance slice is not run
in read-only inspection mode and remains pinned to the old native hash.
Neither this profile nor schema review validates inference on the new binary,
Desktop chat usability, persistent normal-entry routing, model switching,
or a successful bridge-mediated UI workflow. Those remain separate work.

Offline tests cover mode exclusivity, crossed build tuples, unchanged default
pins and policy settings, actual fixture config/readiness serialization, file
hash checking, owned-root provenance and no native-turn admission. Authoring
checks are source/truth-table checks only; PowerShell, Go and actual new
bridge-context Windows execution were not run while authoring this change.

### Native window evidence and unknown login state

The actual native-original observation at commit
`341298e1515298c6f8714509c78324f7f38dbe12` completed one read-only winapp
inspection. Its report recorded 13 rendered nodes, zero input actions, one
inspection attempt, confirmed held-tool exit, and confirmed owned Desktop
activation exit. The published `official-window-after-inspection.png` visibly
shows "Sign in to ChatGPT", "Continue to sign in", "Sign in another way", and
"Sign up". This is a sign-in screen, not completed onboarding or bridge use.

The same report recorded `loginMarkerCount: 0` and `protectedMarkerCount: 0`.
This discrepancy does not prove a label-regex bug: the existing sign-in regex
already matches all three sign-in phrases when exposed in an element's `name`,
and the protected regex matches "Sign up". The private raw tree was not
published, so the evidence does not establish whether names were unavailable,
the CLI selected a fallback root, or the window changed between observations.
Do not reconstruct a captured UIA tree from the screenshot or claim one of
these possible causes was observed.

`ConvertTo-WinAppSummary` now includes the fixed `loginAssessment` enum.
`login-marker-observed` means a recognized name was present and inspection
stops as before. `unknown-requires-visual-review` means no sign-in name matched;
it never means signed in, no login screen, or permission to interact. A
protected marker such as "Sign up" still stops inspection independently.
Native-original `observed` continues to mean a valid nonempty bounded read,
not a UI safety clearance. Review the screenshot before drawing a UI-state
conclusion; this run must be described as stopped at the visible sign-in screen.
The original report remains unmodified historical evidence.

Offline fixtures in `build/desktop-winapp-inspect.native.tests.ps1` use the
four screenshot labels as synthetic names, not captured tool output. They
also cover empty names, omitted names, value-only labels, redaction, the
unknown assessment, and the unchanged zero-action/one-attempt stop behavior.
No raw UI text or values are added to the public summary. This correction
does not alter selectors, traversal, tool invocations, bridge admission,
workflow markers, login handling, or input permissions, and does not request
another UI run. PowerShell and runtime fixtures were not run while authoring.

#### 4239 static-reader profile propagation

The 67311fd8 bridge-context attempt verified the reviewed 4239 package tuple
and registration, then stopped before activation because the static reader
still required a 3517 package-directory name. This was a missed profile
propagation, not evidence of a Desktop or bridge process failure.

Only the already selected 4239 harness profile now appends
`--reviewed-build desktop-4239` to the static reader. It selects one exact
4239 directory version and the three source paths established by the actual
signed-ASAR contract artifact: `339-ce026819bf837ba7`,
`824-d144f7c160c094ea`, and `199-e3316cddb4ea6a6b` under
`.vite/build/early-bootstrap-*.js`. The outer reader, nested public-plugin
reader, ASAR index/selected-source pass and PowerShell output allowlist all
receive that selection. Omitting the option retains the 3517 directory/target set;
unknown options and crossed directory/profile pairs fail closed. Existing
three-argument `readIndex` and two-argument `inspectArchive` callers retain
their old semantics. The optional read-caller scan remains not run.

The path still comes only from verified registration after the exact package,
backend and ASAR tuple checks. Reparse/redirect protections, fixed relative
archive path, limits, installed/signed-ZIP source hashes, snapshot consistency
and output schema are unchanged. The profile does not accept arbitrary
versions, hashes, source paths or caller-selected targets.

A failure before any alias activation attempt and before fixture/subscription
creation can now record `desktopActivationCleanup: not-started` in the new
profile. An attempt is recorded before Process.Start, so an uncertain start
is not mislabeled. The old package-only proof is retained; neither branch
sets `desktopActivationExited` true. Other cleanup failures remain failures,
and the original static blocker stays the primary reason.

Regression coverage includes the original named Node failure, selected source
content rather than missing-entry success, nested catalog/profile cross-pairs,
path/source limits, snapshot swaps for both builds, CLI selection, both-build
Node-to-PowerShell provenance roundtrips, harness option/entry propagation and
prelaunch versus uncertain activation. Node checks can run locally; PowerShell
and actual Windows execution still require the reviewed CI commit. No new UI
read, input action or independent inference acceptance is added.

### Renderer accessibility, one changed launch input

The reviewed 4239 registered-alias attempt at `1c9437c8` displayed the welcome
page but its one winapp read reported 13 nodes and `required_targets_missing`,
with zero actions. The original-native sign-in-screen run also reported 13
nodes; equal counts do not establish equal trees or explain missing controls.

For the next 4239 alias observation, the existing policy job explicitly selects
`-EnableRendererAccessibility` together with `-EnableReviewedBuild4239`.
The harness switch is default-off and returns only the fixed
`--force-renderer-accessibility` argument. Enabling it without the reviewed
4239 zero-action bridge profile, or with native-entry selection, fails before
setup. The same two push markers, `[bridge-ui-4239-v1]` and
`[winapp-inspection-v0.7.1]`, now select this changed-input observation.
Unmarked/default jobs and the independent AUMID job are unchanged. No additional
run is selected and the old no-flag 4239 observation is not repeated.

[Chromium's accessibility overview](https://github.com/chromium/chromium/blob/d24bf1f96bd65780e7b93b95a0c6112510607cc0/docs/accessibility/overview.md#command-line-options)
documents the switch and Windows UI Automation support. Without a value it
initially requests complete accessibility but permits subsequent mode changes.
[Electron's accessibility guide](https://github.com/electron/electron/blob/ef2710fa6c10fe56365a9949fa783d26ca1499e5/docs/tutorial/accessibility.md)
describes automatic accessibility activation and its Chromium tree.
[Electron's switch reference](https://github.com/electron/electron/blob/ef2710fa6c10fe56365a9949fa783d26ca1499e5/docs/api/command-line-switches.md#chromium-flags)
warns that Chromium switches are version-dependent. These public sources justify
the experiment; they do not establish the bundled Electron/Chromium version or
prove this signed Codex build honors the argument.

The only launch change is `ProcessStartInfo.Arguments` on the existing verified
registered alias. The official executable and package are not modified. No
debug port, script injection, browser connection, security setting, persistent
environment change or input action is added. The fixed
`rendererAccessibility` report says `requested-at-launch` and
`effect: unverified`; that does not prove the flag reached or enabled Chromium.
Read the actual UI counts and screenshot to judge the outcome.

All signed package/backend/ASAR pins, clean child environment, held activation
process identity, HWND checks, screenshot procedure, single winapp call,
30-second/256-KiB-per-stream limits, login/protected checks and cleanup remain
unchanged. A missing tree or target, timeout, overflow or identity uncertainty
still stops; there is no fallback read, retry, coordinate click, or welcome
helper action. Even exposed controls would only inform a separately reviewed
next step, not prove onboarding, model switching or chat. This is alias-context
UI evidence, separate from normal AUMID inheritance and native inference tests.

The focused profile tests cover the disabled default, sole literal argument,
wrong profiles and the unique preflight-to-alias assignment. Local source/YAML
checks do not substitute for PowerShell execution or the actual Windows run.
