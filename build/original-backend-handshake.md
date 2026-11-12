# Original backend handshake observation

This is a diagnostic, not a new launch path or an accepted Desktop integration.
The `dea499dc` original-Connect run verified selection, AUMID activation and
Restore. Its image observer reported no exact direct-child image and one missed
event. That is unknown, not evidence of absent invocation or failed handshake.
No screenshot or native protocol trace was collected in that stage-only run;
welcome/setup gating is therefore also unproven.

The later `03c1f9e1` run observed an authenticated startup-probe admission and
then `probe-failed`, without a successful owner acknowledgement or JSONL launch.
Its preliminary and post-cleanup snapshots matched. Those fixed stages predate
the environment milestones below and do not establish the exact failure cause.
The matching-home/separate-native-home fixtures reproduce a supported admission
path; they do not substitute for the next actual Windows result.

## Compile-time and path boundary

The independent `nativehandshakediag` build tag enables fixed milestone files
only in the freshly copied original acceptance executable directory. Ordinary
builds have no process observer. There is no environment override or runtime
permission switch. The package coordinator still owns exact source/adapter
pins and the hosted isolated action opt-in. Diagnostic binaries are not a
user-ready package.

The writer accepts only the exact host or adapter basename under
`original-desktop-acceptance-<32 lowercase hex>/app`, with an unchanged resolved
path. It creates `native-handshake-host.NN.log` or
`native-handshake-bridge.NN.log` exclusively, with at most 16 slots per role,
64 fixed records per file and each stage recorded once. It retains the opened
descriptor. Existing files are never opened or overwritten. A missing slot,
failed write or exhausted budget loses evidence and cannot change launch
authorization, protocol replies or cleanup. No raw protocol, request ID,
arguments, model data, errors, paths, credentials or environment is serialized.
Help/version/sign-out and the build-time identity check emit nothing. Invalid
argument parsing can also fail before any record, so absence remains unknown.

## Milestones, not inferred success

`nativehost` records existing listener creation, acquired probe lease, successful
probe acknowledgement and acquired JSONL lease separately. None proves a
native backend initialized. `nativelaunch` records successful authenticated
profile checks and endpoint allocation, preserving all existing failure gates.
A validated probe result is not released until the existing deferred cleanup
succeeds; the adapter records `probe-output-written` only after its actual
complete output write. This is not the JSONL handshake.

The owned probe also records `probe-environment-admitted` only after both
backends' existing shared-environment checks pass, or `probe-environment-rejected`
at their actual failure return. These fixed stages expose no environment values
or error details. Admission precedes native executable verification and either
probe command; it is not a native result. Rejection does not identify a specific
environment predicate, such as a different home identity, by itself.

The bridge records actual child starts, received initialize, completed writes
to each child and correlated native replies/errors. Native replies are not
assumed valid: control-plane initialization still waits for its genuine
configuration, policy and complete model-catalog admission. Local capability
rejections are observed without weakening negotiation. Optional `initialized`
notifications do not establish readiness.

`initialization-ready` means the existing Router reached readiness.
`initialize-output-written` requires that transition, the original request ID,
a matching successful result delivery, and the actual `writeMessage` returning
success after a complete write. Short, failed, held or cancelled writes cannot
claim it. The ID stays only in memory. A completed pipe write does not establish
that Desktop read or accepted the response, and does not prove account/chat
readiness, direct Desktop ancestry, successful catalog UI, or zero egress.

## Collection and next Windows run

`Get-OriginalBackendHandshake` runs only after the original recovery decisions
and report are saved. It validates the same fresh owned run/start/one-Connect
binding as the preparation collector, then reads only 32 fixed possible files.
Each snapshot is bounded to 16 KiB, strict UTF-8, complete newline, contiguous
sequence, unique known stages and the correct host/bridge role. Before/after
metadata, reparse/path checks and a retained file handle reject changed or
uncertain reads. This is not a hostile same-user filesystem security boundary.

The output keeps every file separate. `initializeOutputWrittenObserved` needs
authenticated JSONL launch, both native starts/writes/replies, router readiness
and the successful client write in one correctly ordered file. Probe files or
partial evidence from different invocations cannot be combined into it.
Missing or malformed observations remain unknown. All backend-handshake,
Desktop-provenance and recovery/action authority flags remain false.
An observed milestone from one valid file is retained even if another file is
invalid, but the overall snapshot stays `unknown`. This never combines partial
files into an acknowledged initialize. Fixed role and slot identify each file;
the role comes from the exact executable basename, not a protocol parameter.

Run one new hosted original Connect/normal-AUMID/Restore attempt with this tag,
the existing stage-only opt-in and unchanged 15-second observation bound. In
diagnostic mode retain the full existing image window rather than stopping on
the first image. Save the post-recovery observation, then a separate final
read-only snapshot after existing owner-close attempts to catch later records.
Neither snapshot requires screenshots or authorizes setup, login, another
activation, a policy workaround, a real provider turn, or stronger cleanup.
Inspect the observed stage at which evidence stops before choosing another
operation. A direct independently initiated probe would not preserve Desktop
request provenance, so it is not substituted for this run.
The final wrapper is explicitly `after-cleanup-attempts`; it preserves separate
root-exit evidence and keeps descendant exit and trace completeness unverified.
It does not promise a final trace when cleanup or recovery is uncertain. Missing
final evidence remains unknown, never proof that no invocation occurred.

## Verification

Run `go test -race -tags nogui,nativecodex -run Handshake` for
`internal/nativehandshake`, `internal/appserverbridge`, `internal/nativehost`,
`internal/nativelaunch` and `cmd/magpie-codex-bridge`, then the same packages with
`nativehandshakediag` added. The actual bridge Run tests use copied synthetic
test processes, hold the client writer, and cover complete/short/error/cancelled
writes. Router tests cover the catalog barrier, one native reply, native and
local rejection, malformed input and unrelated response IDs. Host and launcher
tests exercise their real composition with authored owner/endpoint fixtures.
The sink tests cover no-op default, owned paths, exclusive slot exhaustion,
fixed grammar, concurrent duplicates and failed sinks. The PowerShell collector
suite checks same-file prerequisites, partial/repeated/malformed/oversized
evidence, role separation, no authority and source dictionary/grammar parity.
These are synthetic tests, not actual official Windows/backend acceptance.

### Cloud verification on 2026-11-12

- Go 1.26.8: all five packages' `Handshake` tests passed with `-race -count=1`
  in both ordinary and diagnostic builds, under a temporary HOME and pinned
  caches. No official executable, account or real provider was used.
- PowerShell 7.5.4: `original-backend-handshake.tests.ps1` passed, including
  the actual temporary-file reader and aggregation fixtures.
- An overlay-only mutation moved the acknowledgement observation before the
  real client write. `TestHandshakeActualRunRequiresCompletedOutputWrite/complete`
  failed behaviorally with `ready or pending write claimed delivery`. The
  unmodified source passed; the working source was never mutated for this check.
- `gofmt -l` on all changed Go files and `git diff --check` printed nothing.
  Native Windows execution, Desktop interpretation, real providers, the full
  repository suite and aggregate packaging are separate checks, not claimed
  by these focused results.
