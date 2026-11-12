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

This targets implicit command lookup as a hypothesis. The observed deadline
proved only that strict Wait did not finish in budget, potentially including
stdout draining; it did not prove the root process stayed alive or identify the
cause. Metadata gates may warm caches. A later passing run would demonstrate
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
