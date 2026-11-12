# Original-window diagnostic capture

Invoke `Invoke-OriginalDesktopWindowCapture` only after the original sequence's
finally and fixed report have completed. Its result is separate diagnostic
metadata: it cannot change uncertainty, cleanup permission or recovery decisions.
The original owned PID/start/executable/window checks and one capture attempt
remain mandatory. No activation, input, Desktop launch or recovery is provided.

## Fixed phase protocol

The ordered stdout records are `worker-entered`, `helper-loaded`,
`context-checked`, `fixture-checked`, `native-compile`, `target-identity`,
`print-window`, `render-returned`, `png-encode`, `private-write`,
`capture-complete`, followed by the exact success JSON. Each phase is flushed.
The complete successful transcript is 195 ASCII bytes within the existing
256-byte per-stream bound. Skipped, duplicate, partial, unknown or reordered
records do not form a valid prefix. Phase-only completion never means success.

The added markers separate helper script loading, environment checks, fixture
path/ancestor checks, and output-path checks. A last observed phase records a
boundary, not the cause of a timeout. In particular, absence of the next marker
does not identify which intervening filesystem, module-loading or OS operation
stalled. `PrintWindow` is synchronous target rendering, not passive observation;
killing its child does not prove target rendering has finished.

The child deadline remains 8 seconds. This is not a whole-call bound: explicit
exit/drain/finally/Dispose waits and constructor-error cleanup can add time, as
listed in `Invoke-OriginalCaptureChild`. Only the retained capture child is
stopped. All stderr, overflow/read failures, unknown exits and invalid images
still block publication; partial images remain private.

## Offline checks

In the disposable Windows CI job, first run
`powershell -NoProfile -File build/original-desktop-window-capture-build.ps1`.
It compiles the reviewed `.cs` source once with the installed .NET Framework
compiler into the fixed sibling helper DLL. No compiled helper is committed or
included in the user-facing Magpie package. Then run
`original-desktop-window-capture.tests.ps1` and
`original-desktop-window-capture.mutations.ps1`. Use Windows Desktop PowerShell
for native tests; Linux Core runs portable checks with telemetry disabled.

The main test automatically invokes `original-desktop-window-capture-prefix.tests.ps1`
on Windows. It uses the actual production host/environment builder with fresh
dummy fixture paths, the same helper loading and real context/path gates, then
loads the precompiled native definitions without invoking a target/window method. A second
inert child verifies rejection of a disallowed branch. This is test-only encoded
code, not a mode or alternate success path in the production worker. There is no
new target/window operation. CI prepares the DLL before these checks. Fixed `PREFIX_PROBE` facts never include
raw errors, paths or arbitrary child text. The earlier inherited-environment
load-only probe remains a narrower comparison, not production startup parity.

## Fixed path loading

The child prefix uses `IO.Path.Combine` for both script paths, the expected
executable/evidence paths, and the private output path. These are filesystem
roots plus fixed literal suffixes, so they do not require PowerShell provider
resolution or first-use `Join-Path` module discovery. Existing absolute-path,
ancestor/reparse-point, path-shape, existence, context and identity guards remain.
Parent-only setup/publication path calls are outside this cleared-child prefix
and are unchanged.

The inert prefix fixture reports an additional test-only `script-path-ready`
boundary between path composition and helper dot-sourcing. This does not change
the production phase protocol. Portable tests trap `Join-Path` during actual
helper loading, real path validation and the inert worker; mutations restore
those calls and must fail. Removing this dependency is source-supported, but
Windows CI is still required to establish whether it resolves the observed stall.

## Precompiled native helper

The C# window identity, capture and PNG validation implementation lives in
`original-desktop-window-capture-native.cs`. Its code is unchanged from the
previous inline definition. Runtime initialization uses only the fixed sibling
DLL after the existing absolute-path and ancestor/reparse-point checks; a
missing or invalid DLL, or a type already loaded from another path, is rejected.
There is no source compilation, search-path fallback or environment expansion in
the cleared screenshot child. The compatibility phase name `native-compile`
now marks initialization by DLL loading, not an invocation of a compiler.

The independent build step has a two-minute CI bound and runs before cleared
children. Both initialization fixtures again use the actual eight-second child
budget. This moves variable cold compilation work out of each screenshot attempt;
it does not prove a specific cause of the earlier Add-Type stalls or bypass OS
security checks. The supervisor and successful/failed image publication rules
are unchanged. Native mutation fixtures copy this same built DLL and the source
alongside their scripts, so missing build input cannot masquerade as a caught
mutation. Missing/corrupt/helper-origin negatives run in fresh processes.
