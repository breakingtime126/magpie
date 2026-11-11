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

Run `original-desktop-window-capture.tests.ps1` and
`original-desktop-window-capture.mutations.ps1`. Use Windows Desktop PowerShell
for native tests; Linux Core runs portable checks with telemetry disabled.

The main test automatically invokes `original-desktop-window-capture-prefix.tests.ps1`
on Windows. It uses the actual production host/environment builder with fresh
dummy fixture paths, the same helper loading and real context/path gates, then
compiles native definitions without invoking a target/window method. A second
inert child verifies rejection of a disallowed branch. This is test-only encoded
code, not a mode or alternate success path in the production worker. There is no
new coordinator/workflow invocation. Fixed `PREFIX_PROBE` facts never include
raw errors, paths or arbitrary child text. The earlier inherited-environment
compile-only probe remains a narrower comparison, not production startup parity.
