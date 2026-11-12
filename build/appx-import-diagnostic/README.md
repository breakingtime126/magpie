# Appx import-order diagnostic

This standalone hosted-Windows diagnostic tests a concrete difference between
Connect and its passing metadata gate. The earlier original query imported Appx first;
the candidate production query now imports Management and Utility first, as does
`build/original-installed-query.tests.ps1`. The diagnostic preserves both arms.
The diagnostic itself is not a production fix, package query, Desktop acceptance
or cold-cache proof. It does not change original acceptance workflow gates.

## Fixed experiment

Four fresh processes, in predeclared A, C, C, A order, each with a different
fresh owned home and empty `.codex` directory:

- A: Appx, Management, Utility.
- C: Management, Utility, Appx.

Both arms run all three imports and identical manifest/export identity checks.
Go AST extracts `installedQuery` from the checked-out source and requires its
full SHA-256 to equal
`2ae02210429155e929cd583c488dc09f415f91ed8e99cfc4c5008d88ed28b23e`
(the candidate differs from the query at `d6de6e03de5255922e0f32e856c076d5215a3124`
only by reordering the three assignment/marker blocks). After that exact binding,
the helper prefix and each complete import block are extracted independently of
production order; missing, duplicate or extra import content is refused. A always
remains Appx-first and C always remains Management/Utility-first. Only these three
import blocks are reordered. Both scripts end before the command binding and
invocation of the installed-package query. The helper inspects Get-AppxPackage's
exported CmdletInfo/implementing assembly but never executes that cmdlet.

Every child uses fixed System32 Windows PowerShell, `-NoProfile -NonInteractive
-Command`, and one total ten-second operation budget. The environment follows
the original GUI's allowlist: OS directory variables, preserved trust/proxy
settings, System32-only PATH, and fresh isolated profile/temp/cache/native-home
paths. PSModulePath remains absent. No ambient provider or GitHub credentials
are inherited, no cache is cleared, and no native account/state is accessed.
Windows environment names are compared case-insensitively; conflicting duplicate
names refuse the probe rather than choosing one. Working directory
is each fresh home, not the original GUI directory: this is an isolated test,
not identical GUI execution. The harness is a normal console executable
launched from CI PowerShell; production Magpie uses `-H windowsgui`. Console/
subsystem association may therefore differ despite matching child flags. A
completion here cannot rule out behavior specific to the GUI parent context.

`proc.StartTreeDrained` uses the same suspended startup, pre-execution Job
assignment and resume implementation as `StartTreeStrict`. It additionally
retains a Job handle until root reaping and zero active processes are observed.
It owns the sole underlying `Cmd.Wait`; repeated drain observation never waits
the command a second time. This wrapper is deliberately stronger cleanup
evidence than the production `Tree.Stop(one second)`/Wait path, not a claim of
byte-identical lifetime behavior. Both diagnostic arms use the same wrapper.

A deadline requests termination of that retained owned Job and allows a separate
three-second cleanup observation. This never extends import success eligibility.
Partial-start owners are also stopped/drained. Any uncertain startup/cleanup
stops later arms and exits unsuccessfully; fresh directories remain for runner
disposal. There is no unknown-PID cleanup, fallback launcher, production retry,
policy/trust override, or successful result from partial/late output.

Stdout is discarded. Stderr is matched byte-by-byte against fixed markers using
a bounded candidate mask; arbitrary bytes are never stored. Duplicate, malformed
or out-of-order protocol lines permanently invalidate evidence, and a matching
partial protocol tail suppresses completion. Unrelated stderr is discarded.
Each parser critical section handles at most 256 input bytes. Output contains only arm, fixed
outcome/progress, confirmed/unknown cleanup, and a conservative contrast enum.
Successful import requires the full ordered marker sequence, a successful root
exit, confirmed drainage and no expired/cancelled operation. Drainage alone is
not successful import. Marker receipt can lag execution.

Only A deadline, C complete, C complete, A deadline is `order-associated`.
Even that is not causation: it supports follow-up investigation of module-load
state. All complete is `not-reproduced`; other full sequences are `inconclusive`.
A completes after earlier failures may indicate cache/order sensitivity. This
harness makes no stronger claim. Deadlines are expected diagnostic outcomes;
a green job means the bounded experiment finished, not Connect acceptance.

## Separate hosted job and explicit opt-in

`.github/workflows/experimental-appx-import-order.yml` runs only on pushes to
`breakingtime126/magpie`'s experimental branch, changes to its own files, and
commit marker `[appx-import-order-v1]`. It uses `windows-2025`, matching the original failing GUI runner label, and
verifies hosted Windows, repository,
branch, exact checked-out/event SHA and marker before admitting the harness.
Checkout does not persist credentials. The job builds only this Go harness and
runs authored tests, never packages or runs the GUI. Its VM cannot warm the VM
of a separate original-GUI attempt. Hosted image caches and prior OS activity
are uncontrolled; neither job is claimed to be cold.

Publishing still reaches existing broad branch workflows. Without changing them,
use `[bridge-offline-only-v1]` alongside `[appx-import-order-v1]` to keep
any separately scheduled broad bridge jobs synthetic-only. Do not use native
Desktop or app-server activation markers for this diagnostic. Its new paths do
not match the original GUI workflow path filters. Existing workflow gates remain
unchanged; separate synthetic jobs are not evidence from this experiment.

## Focused checks

With Go 1.26.8, `GOTOOLCHAIN=local`, `GOTELEMETRY=off`, `GOFLAGS=-mod=readonly`:

- `go test -race -count=1 -timeout=2m ./build/appx-import-diagnostic`
- `go vet ./build/appx-import-diagnostic`
- `go build ./build/appx-import-diagnostic`
- Windows: execute the package tests and harness in the dedicated hosted job.
- Cross-compiling Windows tests only checks compilation, not Windows behavior.

Portable fixtures cover source/hash refusal, exact order, environment isolation,
bounded concurrent parsing, incomplete/nonzero/late results, partial-start
ownership, stop-on-uncertain cleanup and conservative classification. The
Windows-only authored subprocess exercises strict startup and successful drain
without importing any PowerShell module. No official import runs in tests.
