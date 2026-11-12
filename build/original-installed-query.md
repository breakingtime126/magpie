# Bound original installed-package query

`installedQuery` imports fixed manifests beneath the running Windows PowerShell
`$PSHOME`, captures `-PassThru` module objects, and invokes their validated exported
`CmdletInfo` objects. It performs no fallback command lookup. Exact module name,
official base shape are required: Appx's module directory;
Management/Utility's module directory or `$PSHOME` binary-assembly directory.
Management/Utility retain exact manifest Path checks. Appx alone requires its
returned absolute Path to equal Get-AppxPackage's implementing assembly Location,
with exact assembly name `Microsoft.Windows.Appx.PackageManager.Commands`.
Exported nested cmdlets are not required to have the manifest's ModuleBase.
The Desktop metadata gate observed this Appx binary-root shape; see Microsoft's
[module manifest documentation](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_Module_Manifests).
The fixed system manifest import remains the provenance anchor; assembly identity
matching does not claim an independent signature verification.

Layout references: [Windows PowerShell 5.1 Get-Module, example 6](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/get-module?view=powershell-5.1#example-6-group-all-modules-by-name)
shows Appx's fixed manifest; [supporting Microsoft Q&A launch example](https://learn.microsoft.com/en-us/answers/questions/1074830/windows-server-2019-event-id-1000-and-event-id-102)
illustrates Management/Utility's explicit `$PSHOME` manifest paths. Portable tests
verified the official Core Management/Utility manifest Path with ModuleBase at
`$PSHOME`. The Desktop-only metadata gate verifies all three actual fixed imports
and exported command types before packaging, without calling Get-AppxPackage.

The exact package/family/signature/status filters, unique count, literal leaf
check, exit2/3 and JSON fields remain unchanged. Launcher, inherited environment,
policy/trust/proxy settings, ten-second budget and strict full Wait/drain remain
unchanged. Missing or blocked imports/exports fail closed. No alternate query,
retry, WaitDelay, cache clearing or leaked-descendant success is introduced.

The fixed-module binding targeted implicit command lookup as a hypothesis.
A later isolated Windows Connect run observed `query-root-running` before
`query-deadline`: the original root was still alive at sampling. This rules out
an already-exited root at that sample, but does not identify the blocking call
or prove the cause. Metadata gates may warm caches. A later passing run would demonstrate
practical success, not establish cold-start causality.

## Deadline root observation

After the existing context-cancellation branch wins, the query records one of
`query-root-running`, `query-root-exited`, or `query-root-unknown` immediately
before the original cancellation/deadline failure and Stop/Wait cleanup. It is
an observation at sampling time, not an exact-deadline timestamp. Concurrent
completion may report exited even when the Wait result became ready meanwhile;
the selected cancellation branch still refuses the query.

The Windows observer captures a noninheritable SYNCHRONIZE-only duplicate of
the exact owned process handle after strict startup and before the sole Wait.
This retains a kernel process-object reference until that Wait has completed;
it does not extend process execution, change Job ownership, or grant termination
authority. Go 1.26.8 releases its original handle before waiting for stdout
copying, so sampling only through that released handle would lose this case.
No PID is reopened and ProcessState is never read concurrently with Cmd.Wait.
Capture/sample failures remain unknown. The extra observation handle is closed
on both success and failure after the original Wait result has been received.

An exited snapshot narrows the investigation but does not prove that pipe
copying caused the deadline: Wait and job cleanup may be completing concurrently.
No output bytes, stderr, paths, PIDs or error text enter these fixed records.
The snapshot is completed, not failed, so the original query-deadline or
query-cancelled remains the first failure. Observations authorize no retry,
cleanup shortcut, partial-output acceptance or timeout change.

Authored Windows subprocess tests hold the stdout writer behind a deterministic
barrier while the root exits and Go releases its original handle. They cover
running/exited/closed observations and ensure the duplicate is noninheritable
and lacks termination access. Portable fixtures cover missing observation,
invalid state, every cancellation classification, success/error cleanup order,
and failure precedence. Cross-compilation alone does not execute these Windows
tests or establish actual Connect success.

## Query progress observation

The original script writes ten fixed ASCII lines to stderr, separately from
its unchanged JSON stdout: `MAGPIE_QUERY_V1:entry`, then `:setup`, `:appx-import`,
`:appx-return`, `:appx`, `:management`,
`:utility`, `:packages`, `:path`, and `:json` with the same prefix. Entry is the
first script statement; setup follows the preference/encoding setup and helper
definitions. The Appx-only import marker immediately precedes the actual fixed
Import-Module call inside the helper; return immediately follows that call,
before module/identity validation. Neither reports another module. The appx,
management and utility markers follow successful fixed import/identity
validation, packages follows enumeration/filtering before unique-count admission,
path follows the literal leaf check, and json follows the original JSON pipeline.
The snapshot names the last complete marker received, not the exact current
PowerShell phase: pipe/copy scheduling can lag script execution. A marker names
a reached boundary, not a successful Connect or a proven cause.

The command's new stderr observation pipe is owned by Go's `exec.Cmd`: Start
creates it, Go's copy goroutine feeds a bounded discard parser, and the original
sole Wait drains and closes it. Startup failure uses Cmd's startup cleanup.
Cancellation still invokes the strict owner's Stop(one second) before receiving
the original Wait result; there is no second Wait, WaitDelay, or early pipe close.
This adds a pipe/drain dependency, not cleanup or launch authority. A retained
writer could delay draining just as with stdout; no partial output is accepted.

The parser retains no arbitrary stderr bytes. It recognizes only complete,
ordered fixed marker lines, discards other bytes, and synchronizes the enum
snapshot with concurrent writes. Missing, partial, corrupt, or out-of-order
marker evidence must not invent a later completed boundary. At the existing
post-start cancellation branch, one completed `query-progress-*` stage reports
that snapshot before Stop, followed by the independent root snapshot and original
query-deadline/query-cancelled failure. Unknown remains a completed observation
of missing evidence, never query success. No raw stdout/stderr, error text,
paths, PIDs, credentials or native records are exported.

The source-binding gate removes only those ten exact literal marker lines
(including the two fixed Appx conditionals) and requires the original reviewed query hash. It also verifies exact boundary
placement, authored package refusals, unchanged JSON, and separated stderr.
Portable Go tests exercise parser corruption/partial input and concurrent
snapshotting; Windows runtime tests and the original isolated GUI gate remain
necessary. This is diagnostic instrumentation, not a timeout fix.
