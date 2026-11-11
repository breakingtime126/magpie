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
