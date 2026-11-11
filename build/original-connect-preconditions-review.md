# Read-only original Connect preconditions

This optional observer is not an acceptance stage or a recovery authority. It
adds no package query or child process. Existing official preparation already
retains `Context.App`, the exact installed native path and verified streams.
The observer reuses that metadata to compare the original factory's stricter
family and `SignatureKind=Store` predicates instead of repeating registration,
download, signature, installed-file hashing or native version work.

## Integration point (parent applies separately)

Dot-source `original-connect-preconditions.ps1` beside the other acceptance
helpers. Add `connectPreconditions=$null` to the report schema. Only after
`Invoke-OriginalDesktopSequence` has returned, its recovery flags have been
copied and `Save-AcceptanceReport` has succeeded, call:

```powershell
try {
    $report.connectPreconditions=Get-OriginalConnectPreconditions `
        -PackageContext $context -OriginalStartInfo $start `
        -OwnedRun $run -OwnedHome $homeRoot -Sequence $report.sequence `
        -RecoveryChecksCompleted -SequenceReportSaved
} catch {
    $report.connectPreconditions=New-OriginalPreconditionReport @{}
    $report.connectPreconditions.status='unavailable'
}
Save-AcceptanceReport
```

Use the retained original `$start`, not a newly reconstructed environment.
Place this before the existing diagnostic capture block, not inside a UI worker,
Connect callback, recovery callback, shutdown handler or preflight path. Do not
call it if the sequence did not return or saving the report failed. Never use
its return value to assign `$safeCleanup`, change sequence/selection status,
clear uncertainty, delete artifacts, retry Connect, Restore or activate Desktop.
An unexpected exception must be caught as a fixed diagnostic-unavailable result
without disrupting the existing recovery/retention path. No authoritative caller
has been changed in this patch.

## Evidence boundaries

`inspected` values are passed/failed/unknown; artifact values are
present/absent/unknown. No input paths, exception text, UI text, package metadata
strings, environment values, file contents or logs are serialized. A snapshot
means only observation completed, not acceptance. Invalid context or IO failure
returns unavailable, missing saved recovery evidence refuses observation, and
non-Windows execution returns unsupported-platform.

Package predicates describe the already-prepared registration snapshot, not a
new same-environment Get-AppxPackage execution. Neither retained package version
nor matching native file location proves the original `--version` probe ran.
The report explicitly leaves original discovery execution, CLI-version probe,
across-probe runtime identity, embedded launcher pins, owned gateway and host
preparation unproven. Adapter presence is not another hash or trust verdict.

Metadata inspection uses only supplied owned paths and the original SystemRoot
PowerShell location. It requires canonical local fixed-drive paths and checks
ancestors for reparse points before reading descendant attributes. Enumeration
is immediate-only: at most 128 entries per directory, eight generation folders,
and 64 path components; no recursive scan or file-content read occurs. This is
not an atomic filesystem snapshot or protection against a hostile concurrent
same-user path replacement. Transient disappearance does not establish that a
stage never ran: failed preparation may clean up its own generation.

## Offline verification

Run `original-connect-preconditions.tests.ps1`, then
`original-connect-preconditions.mutations.ps1` using PowerShell 5.1 or 7. Tests
exercise independently authored prepared metadata, exact family/signature
refusal, missing/closed context, fixed-schema redaction, honest unproven fields,
sequence gating and unchanged recovery input. AST/source checks constrain the
read-only command surface and metadata bounds. Nine mutations must fail a real
assertion (signature, family, unknown mapping, recovery authority, bound, and
automatic-variable direct, parameter, loop and scoped/typed writes). The AST
guard scans implementation, tests and mutation helpers on every platform,
including dormant Windows branches. HOME and PROFILE are never used as locals.
These portable tests do not exercise Windows filesystem IO or demonstrate a
real acceptance-run diagnosis; Windows execution remains a separate check.
