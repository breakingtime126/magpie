# Original Connect fixed-stage observations

This observer does not establish Connect, Desktop activation, restoration, or
cleanup success. Its result cannot authorize any action or change recovery.
The coordinator collects it only after the original sequence's recovery checks
finish and `Save-AcceptanceReport` persists their result. It adds `connectStages`
to `original-desktop-acceptance.json`; no raw log is uploaded.

## Source and binding

The only production read is the fresh portable original app's
`<OwnedRun>/data/logs/app.log`. `OwnedRun` must be this coordinator's
`RUNNER_TEMP/original-desktop-acceptance-<32 lowercase hex>/app` directory and
match the original process start-info executable and working directory. The log
must have been created no earlier than that process's recorded start. All path
components must exist on a local fixed drive and be non-reparse directories;
the final component must be a non-reparse regular file. Missing and failed reads
remain unknown. These checks are not an atomic defense against an adversarial
same-user filesystem writer.

The saved sequence must contain the original UI worker's exact Connect scope,
mode, schema and one attempted action. Unknown action count cannot bind a trace.
Only process-local attempt `1` is accepted in this fresh process. A different or
second attempt is unknown, even if one attempt appears complete.

The existing cli.runWindow → runGUI → startLog("app") startup calls
applog.Start before GUI.Run. applog.Start changes the standard logger output
only, not its flags or prefix. The parser rejects any unexpected prefix.

The Go observer uses the existing application log, not a new credential or
configuration file. It buffers fixed events during Connect and flushes only
after Connect returns, outside manager/controller locks. Thus a hung/crashed
Connect may yield no records, not an observable last-entered stage. Buffered
logging does not make a synchronous log sink immune to blocking.

## Wire format

Every record has the normal Go `YYYY/MM/DD HH:MM:SS ` prefix, followed by exactly:

`native-connect-preparation-v1 attempt=1 sequence=N stage=STAGE outcome=OUTCOME`

The positive decimal sequence starts at 1, is contiguous, and is at most 128.
The first record is `connect entered`, and the final record closes that same
outer Connect with `completed` or `failed`. Entered stages and their matching
terminal outcomes are LIFO nested; a stage may nest inside another occurrence
of itself. Unknown fields, stages, outcomes, malformed protocol-looking lines,
sequence gaps, mismatched nesting, a second Connect and truncated final lines
invalidate the snapshot. Ordinary application log lines are ignored, never
exported. Timestamp values, attempt identifiers and sequence numbers are not
included in the structured result.

The fixed stage names are owned by `internal/nativeprepdiag` and mirrored by
`Get-OriginalConnectStageNames` in the collector. They describe preparation
observations, not authorization or an explanation of a vendor error. Nested
failures may produce both child and parent failed counts.

## Bounds and result meaning

The collector reads at most 1 MiB, 4096 lines, 4096 characters per line and 128
protocol records. It checks size and file metadata before and after the read and
rejects detected change. Existing `app.old.log` is checked by metadata only
and makes the result unknown; its contents are never read. The fixed rotation
truncation notice also invalidates a snapshot. The Windows read handle does not permit ordinary delete
or rename rotation while open. There is no unbounded directory enumeration,
process launch, retry, UI operation or network operation. These are bounded local
filesystem operations, not a hard wall-clock guarantee for a stalled OS/filesystem.

A complete trace reports fixed per-stage entered/completed/failed counts and the
observed Connect terminal outcome. `firstFailedStage` retains the first failed
record in source order only for a complete valid trace; it is unknown otherwise
(and when no failure was observed). Later parent or cleanup failures do not
replace that fixed stage name. A missing, oversized, changing, unreadable,
malformed, multiple-attempt or partial trace is `unknown`, with no stage failure
counts. A well-formed incomplete trace may retain the fixed last-entered stage
as a diagnostic hint; an entered stage is never labelled failed. No completion
in these logs can override the actual journal/registry/UI recovery checks.

The pure parser and mutation fixtures run in both Windows PowerShell and Core
before live acceptance. Windows-only fixtures exercise the real bounded file
reader, old-file rejection, exclusive-lock failure and a junction ancestor.
Local Linux Core results do not establish those Windows filesystem checks or
actual Connect acceptance.
